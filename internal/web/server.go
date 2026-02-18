package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
)

// RoomState tracks the state of a single room
type RoomState struct {
	RoomID  int64  `json:"room_id"`
	Name    string `json:"name"`
	Live    bool   `json:"live"`
	Paused  bool   `json:"paused"`
	STTText string `json:"stt_text"` // last STT text for display
}

// RoomControl manages per-room pause/resume state
type RoomControl struct {
	mu    sync.RWMutex
	rooms map[int64]*RoomState
}

func NewRoomControl() *RoomControl {
	return &RoomControl{
		rooms: make(map[int64]*RoomState),
	}
}

func (rc *RoomControl) Register(roomID int64, name string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.rooms[roomID] = &RoomState{RoomID: roomID, Name: name}
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
		states = append(states, *r)
	}
	return states
}

// Server serves the control panel
type Server struct {
	rc   *RoomControl
	port int
}

func NewServer(rc *RoomControl, port int) *Server {
	return &Server{rc: rc, port: port}
}

func (s *Server) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/rooms", s.handleRooms)
	mux.HandleFunc("/api/toggle", s.handleToggle)

	addr := fmt.Sprintf(":%d", s.port)
	slog.Info("web control panel started", "addr", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			slog.Error("web server error", "err", err)
		}
	}()
}

func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.rc.GetAll())
}

func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request) {
	roomStr := r.URL.Query().Get("room")
	roomID, err := strconv.ParseInt(roomStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid room", 400)
		return
	}
	paused := s.rc.Toggle(roomID)
	state := "▶️ 翻译中"
	if paused {
		state = "⏸ 已暂停"
	}
	slog.Info("room toggled", "room", roomID, "paused", paused)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"room_id": roomID, "paused": paused, "state": state})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LiveSub 控制面板</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #1a1a2e; color: #eee; min-height: 100vh; padding: 20px; }
  h1 { text-align: center; margin-bottom: 30px; font-size: 24px; color: #e94560; }
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
  .btn { width: 100%; padding: 12px; border: none; border-radius: 8px; font-size: 16px; cursor: pointer; font-weight: bold; transition: all 0.2s; }
  .btn-pause { background: #e94560; color: #fff; }
  .btn-resume { background: #4ecca3; color: #000; }
  .btn:hover { opacity: 0.85; transform: scale(1.02); }
  .btn:active { transform: scale(0.98); }
</style>
</head>
<body>
<h1>🎙️ LiveSub 控制面板</h1>
<div class="rooms" id="rooms"></div>
<script>
async function fetchRooms() {
  const res = await fetch('/api/rooms');
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

fetchRooms();
setInterval(fetchRooms, 2000);
</script>
</body>
</html>`
