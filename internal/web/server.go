package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"path/filepath"

	"github.com/christian-lee/livesub/internal/auth"
	"github.com/christian-lee/livesub/internal/danmaku"
	"github.com/christian-lee/livesub/internal/transcript"
)

// RoomState tracks the state of a single room
type RoomState struct {
	RoomID         int64    `json:"room_id"`
	Name           string   `json:"name"`
	Live           bool     `json:"live"`
	Paused         bool     `json:"paused"`
	STTText        string   `json:"stt_text"`
	Accounts       []string `json:"accounts"`
	CurrentAccount int      `json:"current_account"`
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

func (rc *RoomControl) SetSender(roomID int64, sender *danmaku.BilibiliSender) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.senders[roomID] = sender
}

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

// GetFiltered returns rooms filtered by allowed room IDs and accounts.
func (rc *RoomControl) GetFiltered(roomIDs []int64, accountNames []string) []RoomState {
	all := rc.GetAll()
	roomSet := make(map[int64]bool)
	for _, id := range roomIDs {
		roomSet[id] = true
	}
	acctSet := make(map[string]bool)
	for _, n := range accountNames {
		acctSet[n] = true
	}

	var filtered []RoomState
	for _, r := range all {
		if !roomSet[r.RoomID] {
			continue
		}
		// Filter accounts to only allowed ones
		var visibleAccounts []string
		newCurrent := 0
		for _, a := range r.Accounts {
			if acctSet[a] {
				visibleAccounts = append(visibleAccounts, a)
			}
		}
		// Adjust current account index
		if len(visibleAccounts) > 0 {
			currentName := ""
			if r.CurrentAccount < len(r.Accounts) {
				currentName = r.Accounts[r.CurrentAccount]
			}
			for i, a := range visibleAccounts {
				if a == currentName {
					newCurrent = i
					break
				}
			}
		}
		r.Accounts = visibleAccounts
		r.CurrentAccount = newCurrent
		filtered = append(filtered, r)
	}
	return filtered
}

// session stores user info
type session struct {
	UserID int64
	Expiry time.Time
}

// Server serves the control panel with SQLite-based authentication
type Server struct {
	rc              *RoomControl
	port            int
	store           *auth.Store
	sessions        sync.Map // token → session
	onAccountChange func()   // called when bili accounts change
	onStreamChange  func()   // called when streams added/removed
	transcriptDir   string   // directory for transcript CSVs
}

func NewServer(rc *RoomControl, port int, store *auth.Store, transcriptDir string) *Server {
	return &Server{
		rc:            rc,
		port:          port,
		store:         store,
		transcriptDir: transcriptDir,
	}
}

// OnAccountChange registers a callback when bilibili accounts change.
func (s *Server) OnAccountChange(fn func()) {
	s.onAccountChange = fn
}

// OnStreamChange registers a callback when streams are added/removed.
func (s *Server) OnStreamChange(fn func()) {
	s.onStreamChange = fn
}

func (s *Server) Start() {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("/login", s.handleLoginPage)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)

	// Authenticated
	mux.HandleFunc("/", s.requireAuth(s.handleIndex))
	mux.HandleFunc("/api/rooms", s.requireAuth(s.handleRooms))
	mux.HandleFunc("/api/toggle", s.requireAuth(s.handleToggle))
	mux.HandleFunc("/api/account", s.requireAuth(s.handleSwitchAccount))
	mux.HandleFunc("/api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("/api/transcripts", s.requireAuth(s.handleTranscripts))
	mux.HandleFunc("/api/transcripts/download", s.requireAuth(s.handleTranscriptDownload))

	// Admin only
	mux.HandleFunc("/admin", s.requireAdmin(s.handleAdminPage))
	mux.HandleFunc("/api/admin/users", s.requireAdmin(s.handleAdminUsers))
	mux.HandleFunc("/api/admin/user", s.requireAdmin(s.handleAdminUser))
	mux.HandleFunc("/api/admin/all-rooms", s.requireAdmin(s.handleAdminAllRooms))
	mux.HandleFunc("/api/admin/all-accounts", s.requireAdmin(s.handleAdminAllAccounts))
	mux.HandleFunc("/api/admin/audit", s.requireAdmin(s.handleAdminAudit))
	mux.HandleFunc("/api/admin/streams", s.requireAdmin(s.handleAdminStreams))
	mux.HandleFunc("/api/admin/stream", s.requireAdmin(s.handleAdminStream))
	mux.HandleFunc("/api/admin/bili-accounts", s.requireAdmin(s.handleBiliAccounts))
	mux.HandleFunc("/api/admin/bili-account", s.requireAdmin(s.handleBiliAccount))
	mux.HandleFunc("/api/admin/bili-qr/generate", s.requireAdmin(s.handleBiliQRGenerate))
	mux.HandleFunc("/api/admin/bili-qr/poll", s.requireAdmin(s.handleBiliQRPoll))

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

func (s *Server) getSession(r *http.Request) *session {
	cookie, err := r.Cookie("livesub_token")
	if err != nil {
		return nil
	}
	val, ok := s.sessions.Load(cookie.Value)
	if !ok {
		return nil
	}
	sess := val.(*session)
	if time.Now().After(sess.Expiry) {
		s.sessions.Delete(cookie.Value)
		return nil
	}
	return sess
}

func (s *Server) getUser(r *http.Request) *auth.User {
	sess := s.getSession(r)
	if sess == nil {
		return nil
	}
	u, _ := s.store.GetUser(sess.UserID)
	return u
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.getSession(r) == nil {
			if len(r.URL.Path) > 4 && r.URL.Path[:5] == "/api/" {
				http.Error(w, `{"error":"unauthorized"}`, 401)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.getUser(r)
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if !u.IsAdmin {
			http.Error(w, `{"error":"forbidden"}`, 403)
			return
		}
		next(w, r)
	}
}

// --- Auth handlers ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.ParseForm()
	username := r.FormValue("username")
	password := r.FormValue("password")

	u, err := s.store.Authenticate(username, password)
	if err != nil || u == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]string{"error": "用户名或密码错误"})
		return
	}

	token := s.generateToken()
	s.sessions.Store(token, &session{UserID: u.ID, Expiry: time.Now().Add(24 * time.Hour)})

	http.SetCookie(w, &http.Cookie{
		Name:     "livesub_token",
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	s.store.Log(u.ID, u.Username, "登录", "", ip)
	slog.Info("user logged in", "username", username, "admin", u.IsAdmin, "ip", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "is_admin": u.IsAdmin})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("livesub_token")
	if err == nil {
		s.sessions.Delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "livesub_token", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(u)
}

// --- Room handlers (filtered by user permissions) ---

func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	var rooms []RoomState
	if u.IsAdmin {
		rooms = s.rc.GetAll()
	} else {
		allowedRooms, _ := s.store.GetUserRooms(u.ID)
		allowedAccounts, _ := s.store.GetUserAccounts(u.ID)
		rooms = s.rc.GetFiltered(allowedRooms, allowedAccounts)
	}
	if rooms == nil {
		rooms = []RoomState{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rooms)
}

func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	roomID, _ := strconv.ParseInt(r.URL.Query().Get("room"), 10, 64)
	if !s.userCanAccessRoom(u, roomID) {
		http.Error(w, `{"error":"forbidden"}`, 403)
		return
	}
	paused := s.rc.Toggle(roomID)
	if paused {
		s.audit(r, "暂停翻译", fmt.Sprintf("房间 %d", roomID))
	} else {
		s.audit(r, "恢复翻译", fmt.Sprintf("房间 %d", roomID))
	}
	slog.Info("room toggled", "room", roomID, "paused", paused, "user", u.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"room_id": roomID, "paused": paused})
}

func (s *Server) handleSwitchAccount(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	roomID, _ := strconv.ParseInt(r.URL.Query().Get("room"), 10, 64)
	idx, _ := strconv.Atoi(r.URL.Query().Get("index"))

	if !s.userCanAccessRoom(u, roomID) {
		http.Error(w, `{"error":"forbidden"}`, 403)
		return
	}

	sender := s.rc.GetSender(roomID)
	if sender == nil {
		http.Error(w, "room not found", 404)
		return
	}

	// Check if user can use this account
	names := sender.AccountNames()
	if idx < 0 || idx >= len(names) {
		http.Error(w, "invalid index", 400)
		return
	}
	if !u.IsAdmin {
		allowed, _ := s.store.GetUserAccounts(u.ID)
		acctSet := make(map[string]bool)
		for _, a := range allowed {
			acctSet[a] = true
		}
		if !acctSet[names[idx]] {
			http.Error(w, `{"error":"forbidden"}`, 403)
			return
		}
	}

	sender.SwitchAccount(idx)
	newIdx, name := sender.CurrentAccount()
	s.audit(r, "切换账号", fmt.Sprintf("房间 %d → %s", roomID, name))
	slog.Info("account switched", "room", roomID, "account", name, "user", u.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"room_id": roomID, "account_index": newIdx, "account_name": name})
}

func (s *Server) userCanAccessRoom(u *auth.User, roomID int64) bool {
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	rooms, _ := s.store.GetUserRooms(u.ID)
	for _, r := range rooms {
		if r == roomID {
			return true
		}
	}
	return false
}

// --- Admin handlers ---

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		users, err := s.store.ListUserDetails()
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, 500)
			return
		}
		if users == nil {
			users = []auth.UserDetail{}
		}
		json.NewEncoder(w).Encode(users)

	case "POST":
		var req struct {
			Username string  `json:"username"`
			Password string  `json:"password"`
			IsAdmin  bool    `json:"is_admin"`
			Rooms    []int64 `json:"rooms"`
			Accounts []string `json:"accounts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, 400)
			return
		}
		if req.Username == "" || req.Password == "" {
			http.Error(w, `{"error":"username and password required"}`, 400)
			return
		}
		u, err := s.store.CreateUser(req.Username, req.Password, req.IsAdmin)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, 400)
			return
		}
		if req.Rooms != nil {
			s.store.SetUserRooms(u.ID, req.Rooms)
		}
		if req.Accounts != nil {
			s.store.SetUserAccounts(u.ID, req.Accounts)
		}
		detail, _ := s.store.GetUserDetail(u.ID)
		s.audit(r, "创建用户", req.Username)
		slog.Info("user created", "username", req.Username, "admin", req.IsAdmin)
		json.NewEncoder(w).Encode(detail)

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleAdminUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, 400)
		return
	}

	switch r.Method {
	case "PUT":
		var req struct {
			Password *string  `json:"password"`
			Rooms    *[]int64 `json:"rooms"`
			Accounts *[]string `json:"accounts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, 400)
			return
		}
		if req.Password != nil && *req.Password != "" {
			s.store.UpdatePassword(id, *req.Password)
		}
		if req.Rooms != nil {
			s.store.SetUserRooms(id, *req.Rooms)
		}
		if req.Accounts != nil {
			s.store.SetUserAccounts(id, *req.Accounts)
		}
		detail, _ := s.store.GetUserDetail(id)
		s.audit(r, "编辑用户", fmt.Sprintf("ID=%d %s", id, detail.Username))
		json.NewEncoder(w).Encode(detail)

	case "DELETE":
		if err := s.store.DeleteUser(id); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, 500)
			return
		}
		s.audit(r, "删除用户", fmt.Sprintf("ID=%d", id))
		slog.Info("user deleted", "id", id)
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleAdminAllRooms(w http.ResponseWriter, r *http.Request) {
	rooms := s.rc.GetAll()
	type roomInfo struct {
		RoomID int64  `json:"room_id"`
		Name   string `json:"name"`
	}
	var infos []roomInfo
	for _, r := range rooms {
		infos = append(infos, roomInfo{RoomID: r.RoomID, Name: r.Name})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(infos)
}

func (s *Server) handleAdminAllAccounts(w http.ResponseWriter, r *http.Request) {
	// Collect unique account names from senders + DB
	seen := make(map[string]bool)
	var names []string
	for _, room := range s.rc.GetAll() {
		for _, a := range room.Accounts {
			if !seen[a] {
				seen[a] = true
				names = append(names, a)
			}
		}
	}
	// Also include DB accounts not yet synced to senders
	if dbAccounts, err := s.store.ListBiliAccountSummaries(); err == nil {
		for _, a := range dbAccounts {
			if !seen[a.Name] {
				seen[a.Name] = true
				names = append(names, a.Name)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(names)
}

// --- Transcripts ---

func (s *Server) handleTranscripts(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if u.IsAdmin {
		// Admin sees all
		files, err := transcript.ListFiles(s.transcriptDir)
		if err != nil {
			json.NewEncoder(w).Encode([]transcript.FileInfo{})
			return
		}
		if files == nil {
			files = []transcript.FileInfo{}
		}
		json.NewEncoder(w).Encode(files)
		return
	}

	// Regular user: only their assigned rooms
	allowedRooms, _ := s.store.GetUserRooms(u.ID)
	var allFiles []transcript.FileInfo
	for _, roomID := range allowedRooms {
		files, _ := transcript.ListFilesForRoom(s.transcriptDir, roomID)
		allFiles = append(allFiles, files...)
	}
	if allFiles == nil {
		allFiles = []transcript.FileInfo{}
	}
	json.NewEncoder(w).Encode(allFiles)
}

func (s *Server) handleTranscriptDownload(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	if u == nil {
		http.Error(w, "unauthorized", 401)
		return
	}

	filename := r.URL.Query().Get("file")
	if filename == "" || filepath.Base(filename) != filename {
		http.Error(w, "invalid filename", 400)
		return
	}

	// Permission check: admin can download all, users only their rooms
	if !u.IsAdmin {
		allowed := false
		rooms, _ := s.store.GetUserRooms(u.ID)
		for _, roomID := range rooms {
			prefix := fmt.Sprintf("%d_", roomID)
			if len(filename) > len(prefix) && filename[:len(prefix)] == prefix {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, "forbidden", 403)
			return
		}
	}

	path := filepath.Join(s.transcriptDir, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.Error(w, "not found", 404)
		return
	}

	s.audit(r, "下载字幕", filename)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	http.ServeFile(w, r, path)
}

// --- Stream Management ---

func (s *Server) handleAdminStreams(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	streams, err := s.store.ListStreams()
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, 500)
		return
	}
	if streams == nil {
		streams = []auth.StreamInfo{}
	}
	json.NewEncoder(w).Encode(streams)
}

func (s *Server) handleAdminStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "POST":
		var req struct {
			Name       string `json:"name"`
			RoomID     int64  `json:"room_id"`
			SourceLang string `json:"source_lang"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Name == "" || req.RoomID == 0 {
			http.Error(w, `{"error":"name and room_id required"}`, 400)
			return
		}
		si, err := s.store.AddStream(req.Name, req.RoomID, req.SourceLang)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, 400)
			return
		}
		s.audit(r, "添加直播间", fmt.Sprintf("%s (#%d)", req.Name, req.RoomID))
		if s.onStreamChange != nil {
			s.onStreamChange()
		}
		json.NewEncoder(w).Encode(si)

	case "DELETE":
		idStr := r.URL.Query().Get("id")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		s.store.DeleteStream(id)
		s.audit(r, "删除直播间", fmt.Sprintf("ID=%d", id))
		if s.onStreamChange != nil {
			s.onStreamChange()
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

// --- Bilibili Account Management ---

func (s *Server) handleBiliAccounts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	accounts, err := s.store.ListBiliAccountSummaries()
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, 500)
		return
	}
	if accounts == nil {
		accounts = []auth.BiliAccountSummary{}
	}
	json.NewEncoder(w).Encode(accounts)
}

func (s *Server) handleBiliAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)

	switch r.Method {
	case "PUT":
		var req struct {
			DanmakuMax *int `json:"danmaku_max"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.DanmakuMax != nil {
			s.store.UpdateBiliAccountDanmakuMax(id, *req.DanmakuMax)
		}
		s.notifyAccountChange()
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})

	case "DELETE":
		s.store.DeleteBiliAccount(id)
		s.audit(r, "删除B站账号", fmt.Sprintf("ID=%d", id))
		s.notifyAccountChange()
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleBiliQRGenerate(w http.ResponseWriter, r *http.Request) {
	qr, err := auth.GenerateQRCode()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(qr)
}

func (s *Server) handleBiliQRPoll(w http.ResponseWriter, r *http.Request) {
	qrcodeKey := r.URL.Query().Get("key")
	if qrcodeKey == "" {
		http.Error(w, `{"error":"missing key"}`, 400)
		return
	}

	result, err := auth.PollQRCode(qrcodeKey)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if result.Status == "confirmed" {
		// Get username from Bilibili
		name := fmt.Sprintf("UID_%d", result.UID)
		if uname, err := auth.GetBiliUserInfo(result.SESSDATA); err == nil {
			name = uname
		}

		// Save to DB
		acc, err := s.store.SaveBiliAccount(name, result.SESSDATA, result.BiliJCT, result.UID, 20, "")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}

		s.audit(r, "添加B站账号", fmt.Sprintf("%s (UID: %d)", name, result.UID))
		s.notifyAccountChange()

		json.NewEncoder(w).Encode(map[string]any{
			"status": "confirmed",
			"name":   name,
			"uid":    result.UID,
			"id":     acc.ID,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": result.Status})
}

func (s *Server) notifyAccountChange() {
	if s.onAccountChange != nil {
		s.onAccountChange()
	}
}

// --- Audit ---

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 200
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
		limit = n
	}
	entries, err := s.store.GetAuditLog(limit)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, 500)
		return
	}
	if entries == nil {
		entries = []auth.AuditEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (s *Server) audit(r *http.Request, action, detail string) {
	u := s.getUser(r)
	if u == nil {
		return
	}
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	s.store.Log(u.ID, u.Username, action, detail, ip)
}

// --- Pages ---

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.getSession(r) != nil {
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

func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, adminHTML)
}
