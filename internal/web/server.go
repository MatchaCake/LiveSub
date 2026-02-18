package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/christian-lee/livesub/internal/danmaku"
)

// RoomState tracks the state of a single room
type RoomState struct {
	RoomID         int64    `json:"room_id"`
	Name           string   `json:"name"`
	Live           bool     `json:"live"`
	Paused         bool     `json:"paused"`
	STTText        string   `json:"stt_text"`
	Accounts       []string `json:"accounts"`        // available account names
	CurrentAccount int      `json:"current_account"`  // index of active account
}

// RoomControl manages per-room pause/resume state
type RoomControl struct {
	mu      sync.RWMutex
	rooms   map[int64]*RoomState
	senders map[int64]*danmaku.BilibiliSender
}

func NewRoomControl() *RoomControl {
	return &RoomControl{
		rooms:   make(map[int64]*RoomState),
		senders: make(map[int64]*danmaku.BilibiliSender),
	}
}

// SetSender associates a danmaku sender with a room.
func (rc *RoomControl) SetSender(roomID int64, sender *danmaku.BilibiliSender) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.senders[roomID] = sender
}

// GetSender returns the sender for a room.
func (rc *RoomControl) GetSender(roomID int64) *danmaku.BilibiliSender {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.senders[roomID]
}

func (rc *RoomControl) Register(roomID int64, name string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.rooms[roomID] = &RoomState{RoomID: roomID, Name: name}
}

func (rc *RoomControl) Unregister(roomID int64) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	delete(rc.rooms, roomID)
}

func (rc *RoomControl) SetLive(roomID int64, live bool) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if r, ok := rc.rooms[roomID]; ok {
		r.Live = live
	}
}

func (rc *RoomControl) SetLastText(roomID int64, text string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if r, ok := rc.rooms[roomID]; ok {
		r.STTText = text
	}
}

func (rc *RoomControl) IsPaused(roomID int64) bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	if r, ok := rc.rooms[roomID]; ok {
		return r.Paused
	}
	return false
}

func (rc *RoomControl) Toggle(roomID int64) bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if r, ok := rc.rooms[roomID]; ok {
		r.Paused = !r.Paused
		return r.Paused
	}
	return false
}

func (rc *RoomControl) GetAll() []RoomState {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	states := make([]RoomState, 0, len(rc.rooms))
	for _, r := range rc.rooms {
		s := *r
		if sender, ok := rc.senders[r.RoomID]; ok {
			s.Accounts = sender.AccountNames()
			s.CurrentAccount, _ = sender.CurrentAccount()
		}
		states = append(states, s)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].RoomID < states[j].RoomID
	})
	return states
}

// Server serves the control panel with authentication
type Server struct {
	rc       *RoomControl
	port     int
	mu       sync.RWMutex
	username string
	password string
	sessions sync.Map // token → expiry time
}

func NewServer(rc *RoomControl, port int, username, password string) *Server {
	return &Server{
		rc:       rc,
		port:     port,
		username: username,
		password: password,
	}
}

// UpdateAuth updates credentials (hot reload)
func (s *Server) UpdateAuth(username, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.username = username
	s.password = password
	slog.Info("auth credentials updated")
}

func (s *Server) Start() {
	mux := http.NewServeMux()

	s.mu.RLock()
	hasAuth := s.username != "" && s.password != ""
	s.mu.RUnlock()

	if hasAuth {
		// Auth enabled
		mux.HandleFunc("/login", s.handleLoginPage)
		mux.HandleFunc("/api/login", s.handleLogin)
		mux.HandleFunc("/api/logout", s.handleLogout)
		mux.HandleFunc("/", s.requireAuth(s.handleIndex))
		mux.HandleFunc("/api/rooms", s.requireAuth(s.handleRooms))
		mux.HandleFunc("/api/toggle", s.requireAuth(s.handleToggle))
		mux.HandleFunc("/api/account", s.requireAuth(s.handleSwitchAccount))
		slog.Info("web auth enabled", "username", s.username)
	} else {
		// No auth
		mux.HandleFunc("/", s.handleIndex)
		mux.HandleFunc("/api/rooms", s.handleRooms)
		mux.HandleFunc("/api/toggle", s.handleToggle)
		mux.HandleFunc("/api/account", s.handleSwitchAccount)
		slog.Info("web auth disabled (no username/password configured)")
	}

	addr := fmt.Sprintf(":%d", s.port)
	slog.Info("web control panel started", "addr", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			slog.Error("web server error", "err", err)
		}
	}()
}

func (s *Server) generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) isValidSession(r *http.Request) bool {
	cookie, err := r.Cookie("livesub_token")
	if err != nil {
		return false
	}
	expiry, ok := s.sessions.Load(cookie.Value)
	if !ok {
		return false
	}
	if time.Now().After(expiry.(time.Time)) {
		s.sessions.Delete(cookie.Value)
		return false
	}
	return true
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.isValidSession(r) {
			next(w, r)
			return
		}
		// API calls get 401, page requests redirect to login
		if len(r.URL.Path) > 4 && r.URL.Path[:4] == "/api" {
			http.Error(w, `{"error":"unauthorized"}`, 401)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.ParseForm()
	user := r.FormValue("username")
	pass := r.FormValue("password")

	s.mu.RLock()
	validUser := s.username
	validPass := s.password
	s.mu.RUnlock()

	if user != validUser || pass != validPass {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]string{"error": "用户名或密码错误"})
		return
	}

	token := s.generateToken()
	s.sessions.Store(token, time.Now().Add(24*time.Hour))

	http.SetCookie(w, &http.Cookie{
		Name:     "livesub_token",
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	slog.Info("user logged in", "username", user, "ip", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("livesub_token")
	if err == nil {
		s.sessions.Delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "livesub_token",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.rc.GetAll())
}

func (s *Server) handleSwitchAccount(w http.ResponseWriter, r *http.Request) {
	roomStr := r.URL.Query().Get("room")
	roomID, err := strconv.ParseInt(roomStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid room", 400)
		return
	}
	idxStr := r.URL.Query().Get("index")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		http.Error(w, "invalid index", 400)
		return
	}
	sender := s.rc.GetSender(roomID)
	if sender == nil {
		http.Error(w, "room not found", 404)
		return
	}
	ok := sender.SwitchAccount(idx)
	if !ok {
		http.Error(w, "invalid account index", 400)
		return
	}
	newIdx, name := sender.CurrentAccount()
	slog.Info("account switched via web", "room", roomID, "account", name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"room_id": roomID, "account_index": newIdx, "account_name": name})
}

func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request) {
	roomStr := r.URL.Query().Get("room")
	roomID, err := strconv.ParseInt(roomStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid room", 400)
		return
	}
	paused := s.rc.Toggle(roomID)
	slog.Info("room toggled", "room", roomID, "paused", paused)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"room_id": roomID, "paused": paused})
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.isValidSession(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, loginHTML)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

const loginHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LiveSub 登录</title>
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>🎙️</text></svg>">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #1a1a2e; color: #eee; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
  .login-box { background: #16213e; border-radius: 16px; padding: 40px; width: 360px; }
  h1 { text-align: center; margin-bottom: 30px; color: #e94560; font-size: 22px; }
  .field { margin-bottom: 20px; }
  label { display: block; margin-bottom: 6px; font-size: 14px; color: #aaa; }
  input { width: 100%; padding: 12px; border: 1px solid #333; border-radius: 8px; background: #0f3460; color: #eee; font-size: 16px; outline: none; }
  input:focus { border-color: #e94560; }
  .btn { width: 100%; padding: 14px; border: none; border-radius: 8px; background: #e94560; color: #fff; font-size: 16px; font-weight: bold; cursor: pointer; }
  .btn:hover { opacity: 0.9; }
  .error { color: #e94560; text-align: center; margin-top: 15px; font-size: 14px; display: none; }
</style>
</head>
<body>
<div class="login-box">
  <h1>🎙️ LiveSub</h1>
  <form id="loginForm">
    <div class="field">
      <label>用户名</label>
      <input type="text" name="username" id="username" autocomplete="username" required>
    </div>
    <div class="field">
      <label>密码</label>
      <input type="password" name="password" id="password" autocomplete="current-password" required>
    </div>
    <button type="submit" class="btn">登录</button>
    <div class="error" id="error"></div>
  </form>
</div>
<script>
document.getElementById('loginForm').onsubmit = async (e) => {
  e.preventDefault();
  const form = new FormData(e.target);
  const res = await fetch('/api/login', { method: 'POST', body: new URLSearchParams(form) });
  if (res.ok) {
    window.location.href = '/';
  } else {
    const data = await res.json();
    const el = document.getElementById('error');
    el.textContent = data.error || '登录失败';
    el.style.display = 'block';
  }
};
</script>
</body>
</html>`

const indexHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LiveSub 控制面板</title>
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>🎙️</text></svg>">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #1a1a2e; color: #eee; min-height: 100vh; padding: 20px; }
  .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 30px; }
  h1 { font-size: 24px; color: #e94560; }
  .logout { padding: 8px 16px; border: 1px solid #555; border-radius: 6px; background: transparent; color: #aaa; cursor: pointer; font-size: 13px; text-decoration: none; }
  .logout:hover { border-color: #e94560; color: #e94560; }
  .rooms { display: flex; flex-wrap: wrap; gap: 20px; justify-content: center; }
  .room { background: #16213e; border-radius: 12px; padding: 20px; min-width: 300px; max-width: 400px; flex: 1; }
  .room-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
  .room-name { font-size: 18px; font-weight: bold; }
  .room-id { font-size: 12px; color: #888; }
  .status { display: flex; gap: 8px; align-items: center; margin-bottom: 12px; }
  .badge { padding: 3px 10px; border-radius: 12px; font-size: 12px; font-weight: bold; }
  .badge-live { background: #e94560; }
  .badge-offline { background: #444; }
  .badge-translating { background: #0f3460; }
  .badge-paused { background: #e9a045; color: #000; }
  .last-text { font-size: 13px; color: #aaa; min-height: 40px; margin-bottom: 15px; word-break: break-all; }
  .account-row { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
  .account-row label { font-size: 13px; color: #aaa; white-space: nowrap; }
  .account-select { flex: 1; padding: 6px 10px; border: 1px solid #333; border-radius: 6px; background: #0f3460; color: #eee; font-size: 13px; outline: none; cursor: pointer; }
  .account-select:focus { border-color: #e94560; }
  .btn { width: 100%; padding: 12px; border: none; border-radius: 8px; font-size: 16px; cursor: pointer; font-weight: bold; transition: all 0.2s; }
  .btn-pause { background: #e94560; color: #fff; }
  .btn-resume { background: #4ecca3; color: #000; }
  .btn:hover { opacity: 0.85; transform: scale(1.02); }
  .btn:active { transform: scale(0.98); }
</style>
</head>
<body>
<div class="header">
  <h1>🎙️ LiveSub 控制面板</h1>
  <a href="/api/logout" class="logout">退出登录</a>
</div>
<div class="rooms" id="rooms"></div>
<script>
async function fetchRooms() {
  const res = await fetch('/api/rooms');
  if (res.status === 401) { window.location.href = '/login'; return; }
  const rooms = await res.json();
  const el = document.getElementById('rooms');
  el.innerHTML = rooms.map(r => ` + "`" + `
    <div class="room">
      <div class="room-header">
        <span class="room-name">${r.name || '直播间'}</span>
        <span class="room-id">#${r.room_id}</span>
      </div>
      <div class="status">
        <span class="badge ${r.live ? 'badge-live' : 'badge-offline'}">${r.live ? '🔴 直播中' : '⚫ 未开播'}</span>
        <span class="badge ${r.paused ? 'badge-paused' : 'badge-translating'}">${r.paused ? '⏸ 已暂停' : '▶️ 翻译中'}</span>
      </div>
      <div class="last-text">${r.stt_text || '等待语音...'}</div>
      ${r.accounts && r.accounts.length > 1 ? ` + "`" + `
      <div class="account-row">
        <label>🔑 账号:</label>
        <select class="account-select" onchange="switchAccount(${r.room_id}, this.value)">
          ${r.accounts.map((a, i) => ` + "`" + `<option value="${i}" ${i === r.current_account ? 'selected' : ''}>${a}</option>` + "`" + `).join('')}
        </select>
      </div>
      ` + "`" + ` : ''}
      <button class="btn ${r.paused ? 'btn-resume' : 'btn-pause'}" onclick="toggle(${r.room_id})">
        ${r.paused ? '▶️ 恢复翻译' : '⏸ 暂停翻译'}
      </button>
    </div>
  ` + "`" + `).join('');
}

async function toggle(roomId) {
  await fetch('/api/toggle?room=' + roomId);
  fetchRooms();
}

async function switchAccount(roomId, index) {
  await fetch('/api/account?room=' + roomId + '&index=' + index);
  fetchRooms();
}

fetchRooms();
setInterval(fetchRooms, 2000);
</script>
</body>
</html>`
