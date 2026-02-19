package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/christian-lee/livesub/internal/auth"
	"github.com/christian-lee/livesub/internal/bot"
	"github.com/christian-lee/livesub/internal/config"
	"github.com/christian-lee/livesub/internal/controller"
	"github.com/christian-lee/livesub/internal/transcript"
)

// StreamerState tracks per-streamer state for the web UI.
type StreamerState struct {
	RoomID   int64                    `json:"room_id"`
	Name     string                   `json:"name"`
	Live     bool                     `json:"live"`
	Outputs  []controller.OutputState `json:"outputs"`
}

// StatusResponse is the /api/status response.
type StatusResponse struct {
	Streamers []StreamerState `json:"streamers"`
	BotNames  []string        `json:"bot_names"`
}

// session stores user info
type session struct {
	UserID int64
	Expiry time.Time
}

// streamerRuntime tracks runtime state for a single streamer.
type streamerRuntime struct {
	live   bool
	ctrl   *controller.Controller
	paused map[string]bool // output name → paused (persists across streams)
}

// Server serves the control panel with SQLite-based authentication
type Server struct {
	pool            *bot.Pool
	port            int
	store           *auth.Store
	cfg             *config.Config
	cfgPath         string
	sessions        sync.Map // token → session
	onAccountChange  func()
	onStreamerChange func()
	transcriptDir   string

	mu        sync.RWMutex
	streamers map[string]*streamerRuntime // streamer name → runtime state

	// WebSocket clients for live status push
	wsMu      sync.Mutex
	wsConns   map[*websocket.Conn]bool
	wsBroadch chan struct{} // coalesce rapid broadcasts
}

func NewServer(pool *bot.Pool, port int, store *auth.Store, transcriptDir string, cfg *config.Config, cfgPath string) *Server {
	s := &Server{
		pool:          pool,
		port:          port,
		store:         store,
		cfg:           cfg,
		cfgPath:       cfgPath,
		transcriptDir: transcriptDir,
		streamers:     make(map[string]*streamerRuntime),
		wsConns:       make(map[*websocket.Conn]bool),
		wsBroadch:     make(chan struct{}, 1),
	}
	// Load persisted sessions
	s.store.CleanExpiredSessions()
	if saved, err := s.store.LoadSessions(); err == nil {
		for token, sess := range saved {
			s.sessions.Store(token, &session{UserID: sess.UserID, Expiry: sess.Expiry})
		}
		if len(saved) > 0 {
			slog.Info("restored sessions", "count", len(saved))
		}
	}
	// Init runtime state — outputs with auto_start enabled start unpaused
	for _, sc := range cfg.Streamers {
		p := make(map[string]bool)
		for _, o := range sc.Outputs {
			p[o.Name] = !o.AutoStart
		}
		s.streamers[sc.Name] = &streamerRuntime{
			paused: p,
		}
	}
	return s
}

// OnAccountChange registers a callback when bilibili accounts change.
func (s *Server) OnAccountChange(fn func()) {
	s.onAccountChange = fn
}

// UpdateConfig replaces the server's config pointer (called on hot reload).
func (s *Server) UpdateConfig(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	// Init runtime for any new streamers
	for _, sc := range cfg.Streamers {
		if _, ok := s.streamers[sc.Name]; !ok {
			p := make(map[string]bool)
			for _, o := range sc.Outputs {
				p[o.Name] = true // new outputs default paused
			}
			s.streamers[sc.Name] = &streamerRuntime{paused: p}
		}
	}
}

// OnStreamerChange registers a callback when streamer config changes.
func (s *Server) OnStreamerChange(fn func()) {
	s.onStreamerChange = fn
}

// SetController sets the active controller for a streamer.
func (s *Server) SetController(streamerName string, ctrl *controller.Controller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.getOrCreateRuntime(streamerName)
	rt.ctrl = ctrl
	if ctrl != nil {
		for name, paused := range rt.paused {
			ctrl.SetPaused(name, paused)
		}
	}
}

// SetLive updates live status for a streamer.
func (s *Server) SetLive(streamerName string, live bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.getOrCreateRuntime(streamerName)
	rt.live = live
}

func (s *Server) getOrCreateRuntime(name string) *streamerRuntime {
	rt, ok := s.streamers[name]
	if !ok {
		rt = &streamerRuntime{paused: make(map[string]bool)}
		s.streamers[name] = rt
	}
	return rt
}

func (s *Server) Start() {
	go s.runWSBroadcast()
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("/login", s.handleLoginPage)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)

	// Authenticated
	mux.HandleFunc("/", s.requireAuth(s.handleIndex))
	mux.HandleFunc("/api/status", s.requireAuth(s.handleStatus))
	mux.HandleFunc("/ws/status", s.handleWS)
	mux.HandleFunc("/api/toggle", s.requireAuth(s.handleToggle))
	mux.HandleFunc("/api/toggle-seq", s.requireAuth(s.handleToggleSeq))
	mux.HandleFunc("/api/toggle-autostart", s.requireAuth(s.handleToggleAutoStart))
	mux.HandleFunc("/api/skip", s.requireAuth(s.handleSkip))
	mux.HandleFunc("/api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("/api/transcripts", s.requireAuth(s.handleTranscripts))
	mux.HandleFunc("/api/transcripts/download", s.requireAuth(s.handleTranscriptDownload))
	mux.HandleFunc("/api/my/streamer-outputs", s.requireAuth(s.handleMyStreamerOutputs))
	mux.HandleFunc("/api/my/accounts", s.requireAuth(s.handleMyAccounts))
	// /settings removed — merged into /admin

	// Admin only
	mux.HandleFunc("/admin", s.requireAuth(s.handleAdminPage))
	mux.HandleFunc("/api/admin/users", s.requireAdmin(s.handleAdminUsers))
	mux.HandleFunc("/api/admin/user", s.requireAdmin(s.handleAdminUser))
	mux.HandleFunc("/api/admin/all-accounts", s.requireAdmin(s.handleAdminAllAccounts))
	mux.HandleFunc("/api/admin/audit", s.requireAdmin(s.handleAdminAudit))
	mux.HandleFunc("/api/admin/bili-accounts", s.requireAdmin(s.handleBiliAccounts))
	mux.HandleFunc("/api/admin/bili-account", s.requireAdmin(s.handleBiliAccount))
	mux.HandleFunc("/api/admin/bili-qr/generate", s.requireAdmin(s.handleBiliQRGenerate))
	mux.HandleFunc("/api/admin/bili-qr/poll", s.requireAdmin(s.handleBiliQRPoll))
	mux.HandleFunc("/api/admin/streamers", s.requireAdmin(s.handleAdminStreamers))
	mux.HandleFunc("/api/admin/streamer-outputs", s.requireAdmin(s.handleAdminStreamerOutputs))

	addr := fmt.Sprintf(":%d", s.port)
	slog.Info("web control panel started", "addr", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			slog.Error("web server error", "err", err)
		}
	}()
}

func (s *Server) generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(b), nil
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
		s.store.DeleteSession(cookie.Value)
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
			if strings.HasPrefix(r.URL.Path, "/api/") {
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"bad request"}`, 400)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	u, err := s.store.Authenticate(username, password)
	if err != nil || u == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]string{"error": "用户名或密码错误"})
		return
	}

	token, err := s.generateToken()
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, 500)
		slog.Error("generate session token", "err", err)
		return
	}
	expiry := time.Now().Add(7 * 24 * time.Hour)
	s.sessions.Store(token, &session{UserID: u.ID, Expiry: expiry})
	s.store.SaveSession(token, u.ID, expiry)

	http.SetCookie(w, &http.Cookie{
		Name:     "livesub_token",
		Value:    token,
		Path:     "/",
		MaxAge:   604800, // 7 days
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
		s.store.DeleteSession(cookie.Value)
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

// --- Status handler (multi-streamer) ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	// Get user's assigned rooms for filtering
	var userRooms map[int64]bool
	if !u.IsAdmin {
		rooms, _ := s.store.GetUserRooms(u.ID)
		if len(rooms) > 0 {
			userRooms = make(map[int64]bool)
			for _, rid := range rooms {
				userRooms[rid] = true
			}
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var streamers []StreamerState
	for _, sc := range s.cfg.Streamers {
		// Filter by user permissions
		if userRooms != nil && !userRooms[sc.RoomID] {
			continue
		}

		state := StreamerState{
			RoomID: sc.RoomID,
			Name:   sc.Name,
		}

		rt := s.streamers[sc.Name]
		if rt != nil {
			state.Live = rt.live
			if rt.ctrl != nil {
				state.Outputs = rt.ctrl.OutputStates()
			}
		}

		// If no controller, show configured outputs with server-level pause state
		if state.Outputs == nil {
			state.Outputs = make([]controller.OutputState, len(sc.Outputs))
			for i, o := range sc.Outputs {
				paused := false
				if rt != nil {
					paused = rt.paused[o.Name]
				}
				state.Outputs[i] = controller.OutputState{
					Name:       o.Name,
					Platform:   o.Platform,
					TargetLang: o.TargetLang,
					BotName:    o.Account,
					BotNames:   o.AccountPool(),
					Paused:     paused,
					ShowSeq:    o.ShowSeq,
					AutoStart:  o.AutoStart,
				}
			}
		}

		streamers = append(streamers, state)
	}

	if streamers == nil {
		streamers = []StreamerState{}
	}

	resp := StatusResponse{
		Streamers: streamers,
		BotNames:  s.pool.Names(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	streamerName := r.URL.Query().Get("streamer")
	outputName := r.URL.Query().Get("output")
	if streamerName == "" || outputName == "" {
		http.Error(w, `{"error":"streamer and output name required"}`, 400)
		return
	}

	s.mu.Lock()
	rt := s.getOrCreateRuntime(streamerName)
	rt.paused[outputName] = !rt.paused[outputName]
	paused := rt.paused[outputName]
	ctrl := rt.ctrl
	s.mu.Unlock()

	if ctrl != nil {
		ctrl.SetPaused(outputName, paused)
	}
	if paused {
		s.audit(r, "暂停翻译", fmt.Sprintf("%s / %s", streamerName, outputName))
	} else {
		s.audit(r, "恢复翻译", fmt.Sprintf("%s / %s", streamerName, outputName))
	}
	slog.Info("output toggled", "streamer", streamerName, "output", outputName, "paused", paused, "user", u.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"streamer": streamerName, "output": outputName, "paused": paused})
}

func (s *Server) handleToggleSeq(w http.ResponseWriter, r *http.Request) {
	streamerName := r.URL.Query().Get("streamer")
	outputName := r.URL.Query().Get("output")

	s.mu.Lock()
	// Toggle show_seq in config
	for i := range s.cfg.Streamers {
		if s.cfg.Streamers[i].Name == streamerName {
			for j := range s.cfg.Streamers[i].Outputs {
				if s.cfg.Streamers[i].Outputs[j].Name == outputName {
					s.cfg.Streamers[i].Outputs[j].ShowSeq = !s.cfg.Streamers[i].Outputs[j].ShowSeq
					newVal := s.cfg.Streamers[i].Outputs[j].ShowSeq
					rt := s.streamers[streamerName]
					s.mu.Unlock()
					if rt != nil && rt.ctrl != nil {
						rt.ctrl.SetShowSeq(outputName, newVal)
					}
					config.Save(s.cfgPath, s.cfg)
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "show_seq": newVal})
					return
				}
			}
		}
	}
	s.mu.Unlock()
	http.Error(w, `{"error":"not found"}`, 404)
}

func (s *Server) handleToggleAutoStart(w http.ResponseWriter, r *http.Request) {
	streamerName := r.URL.Query().Get("streamer")
	outputName := r.URL.Query().Get("output")

	s.mu.Lock()
	for i := range s.cfg.Streamers {
		if s.cfg.Streamers[i].Name == streamerName {
			for j := range s.cfg.Streamers[i].Outputs {
				if s.cfg.Streamers[i].Outputs[j].Name == outputName {
					s.cfg.Streamers[i].Outputs[j].AutoStart = !s.cfg.Streamers[i].Outputs[j].AutoStart
					newVal := s.cfg.Streamers[i].Outputs[j].AutoStart
					// Update runtime pause state if not live
					rt := s.streamers[streamerName]
					if rt != nil && !rt.live {
						rt.paused[outputName] = !newVal
					}
					// Update controller state if active
					if rt != nil && rt.ctrl != nil {
						if st, ok := rt.ctrl.GetOutputState(outputName); ok {
							st.AutoStart = newVal
						}
					}
					s.mu.Unlock()
					config.Save(s.cfgPath, s.cfg)
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "auto_start": newVal})
					return
				}
			}
		}
	}
	s.mu.Unlock()
	http.Error(w, `{"error":"not found"}`, 404)
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("ws upgrade failed", "err", err)
		return
	}
	s.wsMu.Lock()
	s.wsConns[conn] = true
	s.wsMu.Unlock()

	// Keep connection alive, remove on close
	defer func() {
		s.wsMu.Lock()
		delete(s.wsConns, conn)
		s.wsMu.Unlock()
		conn.Close()
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// BroadcastStatus signals that status should be pushed to WS clients.
// Non-blocking; rapid calls are coalesced.
func (s *Server) BroadcastStatus() {
	select {
	case s.wsBroadch <- struct{}{}:
	default: // already pending
	}
}

// runWSBroadcast is the single goroutine that writes to all WS connections.
func (s *Server) runWSBroadcast() {
	for range s.wsBroadch {
		s.doBroadcast()
	}
}

func (s *Server) doBroadcast() {
	s.wsMu.Lock()
	conns := make([]*websocket.Conn, 0, len(s.wsConns))
	for c := range s.wsConns {
		conns = append(conns, c)
	}
	s.wsMu.Unlock()

	if len(conns) == 0 {
		return
	}

	s.mu.RLock()
	var streamers []StreamerState
	for _, sc := range s.cfg.Streamers {
		state := StreamerState{RoomID: sc.RoomID, Name: sc.Name}
		rt := s.streamers[sc.Name]
		if rt != nil {
			state.Live = rt.live
			if rt.ctrl != nil {
				state.Outputs = rt.ctrl.OutputStates()
			}
		}
		if state.Outputs == nil {
			state.Outputs = make([]controller.OutputState, len(sc.Outputs))
			for i, o := range sc.Outputs {
				paused := false
				if rt != nil {
					paused = rt.paused[o.Name]
				}
				state.Outputs[i] = controller.OutputState{
					Name: o.Name, Platform: o.Platform, TargetLang: o.TargetLang,
					BotName: o.Account, BotNames: o.AccountPool(), Paused: paused, ShowSeq: o.ShowSeq,
				}
			}
		}
		streamers = append(streamers, state)
	}
	s.mu.RUnlock()

	data, _ := json.Marshal(StatusResponse{Streamers: streamers})
	for _, c := range conns {
		if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
			s.wsMu.Lock()
			delete(s.wsConns, c)
			s.wsMu.Unlock()
			c.Close()
		}
	}
}

func (s *Server) handleSkip(w http.ResponseWriter, r *http.Request) {
	streamerName := r.URL.Query().Get("streamer")
	msgIDStr := r.URL.Query().Get("id")
	msgID, _ := strconv.ParseInt(msgIDStr, 10, 64)

	s.mu.RLock()
	rt, ok := s.streamers[streamerName]
	var ctrl *controller.Controller
	if ok {
		ctrl = rt.ctrl
	}
	s.mu.RUnlock()

	if ctrl != nil {
		ctrl.SkipPending(msgID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "skipped": msgID})
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
			Username string   `json:"username"`
			Password string   `json:"password"`
			IsAdmin  bool     `json:"is_admin"`
			Rooms    []int64  `json:"rooms"`
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
			if err := s.store.SetUserRooms(u.ID, req.Rooms); err != nil {
				slog.Error("set user rooms", "user", u.ID, "err", err)
			}
		}
		if req.Accounts != nil {
			if err := s.store.SetUserAccounts(u.ID, req.Accounts); err != nil {
				slog.Error("set user accounts", "user", u.ID, "err", err)
			}
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
			Password *string   `json:"password"`
			Rooms    *[]int64  `json:"rooms"`
			Accounts *[]string `json:"accounts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, 400)
			return
		}
		if req.Password != nil && *req.Password != "" {
			if err := s.store.UpdatePassword(id, *req.Password); err != nil {
				slog.Error("update password", "user", id, "err", err)
			}
		}
		if req.Rooms != nil {
			if err := s.store.SetUserRooms(id, *req.Rooms); err != nil {
				slog.Error("set user rooms", "user", id, "err", err)
			}
		}
		if req.Accounts != nil {
			if err := s.store.SetUserAccounts(id, *req.Accounts); err != nil {
				slog.Error("set user accounts", "user", id, "err", err)
			}
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

func (s *Server) handleAdminAllAccounts(w http.ResponseWriter, r *http.Request) {
	names := s.pool.Names()
	if dbAccounts, err := s.store.ListBiliAccountSummaries(); err == nil {
		seen := make(map[string]bool)
		for _, n := range names {
			seen[n] = true
		}
		for _, a := range dbAccounts {
			if !seen[a.Name] {
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

	files, err := transcript.ListFiles(s.transcriptDir)
	if err != nil {
		json.NewEncoder(w).Encode([]transcript.FileInfo{})
		return
	}
	if files == nil {
		files = []transcript.FileInfo{}
	}

	// Non-admin: filter to assigned rooms only
	if !u.IsAdmin {
		rooms, _ := s.store.GetUserRooms(u.ID)
		if len(rooms) > 0 {
			roomSet := make(map[string]bool)
			for _, rid := range rooms {
				roomSet[fmt.Sprintf("%d_", rid)] = true
			}
			var filtered []transcript.FileInfo
			for _, f := range files {
				for prefix := range roomSet {
					if len(f.Name) > len(prefix) && f.Name[:len(prefix)] == prefix {
						filtered = append(filtered, f)
						break
					}
				}
			}
			files = filtered
			if files == nil {
				files = []transcript.FileInfo{}
			}
		}
	}

	json.NewEncoder(w).Encode(files)
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

	// Non-admin: check room access
	if !u.IsAdmin {
		rooms, _ := s.store.GetUserRooms(u.ID)
		allowed := false
		for _, rid := range rooms {
			prefix := fmt.Sprintf("%d_", rid)
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, 400)
			return
		}
		if req.DanmakuMax != nil {
			if err := s.store.UpdateBiliAccountDanmakuMax(id, *req.DanmakuMax); err != nil {
				slog.Error("update danmaku max", "id", id, "err", err)
			}
		}
		s.notifyAccountChange()
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})

	case "DELETE":
		if err := s.store.DeleteBiliAccount(id); err != nil {
			slog.Error("delete bili account", "id", id, "err", err)
		}
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
		name := fmt.Sprintf("UID_%d", result.UID)
		if uname, err := auth.GetBiliUserInfo(result.SESSDATA); err == nil {
			name = uname
		}

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

// --- Admin Streamer Management ---

// handleAdminStreamers handles GET (list), POST (add/update), DELETE (remove) streamers.
func (s *Server) handleAdminStreamers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		json.NewEncoder(w).Encode(s.cfg.Streamers)

	case "POST":
		var req config.StreamerConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, 400)
			return
		}
		if req.Name == "" || req.RoomID == 0 {
			http.Error(w, `{"error":"name and room_id required"}`, 400)
			return
		}
		if req.SourceLang == "" {
			req.SourceLang = "ja-JP"
		}
		if req.Outputs == nil {
			req.Outputs = []config.OutputConfig{}
		}
		// Update existing or add new
		found := false
		for i, sc := range s.cfg.Streamers {
			if sc.Name == req.Name {
				s.cfg.Streamers[i] = req
				found = true
				break
			}
		}
		if !found {
			s.cfg.Streamers = append(s.cfg.Streamers, req)
		}
		if err := config.Save(s.cfgPath, s.cfg); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}
		s.mu.Lock()
		s.getOrCreateRuntime(req.Name)
		s.mu.Unlock()
		action := "add_streamer"
		if found {
			action = "update_streamer"
		}
		s.audit(r, action, fmt.Sprintf("name=%s room=%d", req.Name, req.RoomID))
		if s.onStreamerChange != nil {
			go s.onStreamerChange()
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})

	case "DELETE":
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, `{"error":"name required"}`, 400)
			return
		}
		newStreamers := make([]config.StreamerConfig, 0)
		for _, sc := range s.cfg.Streamers {
			if sc.Name != name {
				newStreamers = append(newStreamers, sc)
			}
		}
		s.cfg.Streamers = newStreamers
		if err := config.Save(s.cfgPath, s.cfg); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}
		s.mu.Lock()
		delete(s.streamers, name)
		s.mu.Unlock()
		s.audit(r, "delete_streamer", name)
		if s.onStreamerChange != nil {
			go s.onStreamerChange()
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})

	default:
		http.Error(w, `{"error":"method not allowed"}`, 405)
	}
}

// handleAdminStreamerOutputs manages outputs for a specific streamer.
func (s *Server) handleAdminStreamerOutputs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	streamerName := r.URL.Query().Get("streamer")
	if streamerName == "" {
		http.Error(w, `{"error":"streamer name required"}`, 400)
		return
	}

	// Find streamer
	var sc *config.StreamerConfig
	for i := range s.cfg.Streamers {
		if s.cfg.Streamers[i].Name == streamerName {
			sc = &s.cfg.Streamers[i]
			break
		}
	}
	if sc == nil {
		http.Error(w, `{"error":"streamer not found"}`, 404)
		return
	}

	switch r.Method {
	case "GET":
		json.NewEncoder(w).Encode(sc.Outputs)

	case "POST", "PUT":
		var req config.OutputConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, 400)
			return
		}
		if req.Name == "" {
			http.Error(w, `{"error":"name required"}`, 400)
			return
		}
		if req.Platform == "" {
			req.Platform = "bilibili"
		}
		found := false
		for i, o := range sc.Outputs {
			if o.Name == req.Name {
				sc.Outputs[i] = req
				found = true
				break
			}
		}
		if !found {
			sc.Outputs = append(sc.Outputs, req)
		}
		if err := config.Save(s.cfgPath, s.cfg); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}
		// Sync full output list to controller
		{
			rt := s.getOrCreateRuntime(streamerName)
			rt.paused[req.Name] = true
			if rt.ctrl != nil {
				rt.ctrl.SyncOutputs(sc.Outputs)
			}
		}
		action := "add_output"
		if found {
			action = "update_output"
		}
		s.audit(r, action, fmt.Sprintf("%s / %s lang=%s", streamerName, req.Name, req.TargetLang))
		json.NewEncoder(w).Encode(map[string]any{"ok": true})

	case "DELETE":
		outputName := r.URL.Query().Get("name")
		if outputName == "" {
			http.Error(w, `{"error":"output name required"}`, 400)
			return
		}
		newOutputs := make([]config.OutputConfig, 0)
		for _, o := range sc.Outputs {
			if o.Name != outputName {
				newOutputs = append(newOutputs, o)
			}
		}
		sc.Outputs = newOutputs
		if err := config.Save(s.cfgPath, s.cfg); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}
		s.audit(r, "delete_output", fmt.Sprintf("%s / %s", streamerName, outputName))
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
		// Sync to controller
		if rt := s.streamers[streamerName]; rt != nil && rt.ctrl != nil {
			rt.ctrl.SyncOutputs(sc.Outputs)
		}

	default:
		http.Error(w, `{"error":"method not allowed"}`, 405)
	}
}

// handleMyAccounts returns accounts available to the current user.
// Admin gets all accounts; regular users get their assigned ones.
func (s *Server) handleMyAccounts(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if u.IsAdmin {
		// Admin: all accounts (same as all-accounts)
		names := s.pool.Names()
		if dbAccounts, err := s.store.ListBiliAccountSummaries(); err == nil {
			seen := make(map[string]bool)
			for _, n := range names {
				seen[n] = true
			}
			for _, a := range dbAccounts {
				if !seen[a.Name] {
					names = append(names, a.Name)
				}
			}
		}
		json.NewEncoder(w).Encode(names)
		return
	}

	accts, _ := s.store.GetUserAccounts(u.ID)
	if accts == nil {
		accts = []string{}
	}
	json.NewEncoder(w).Encode(accts)
}

// handleMyStreamerOutputs lets authenticated users manage outputs for their assigned rooms.
// Admins can access all rooms.
func (s *Server) handleMyStreamerOutputs(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	streamerName := r.URL.Query().Get("streamer")
	if streamerName == "" {
		http.Error(w, `{"error":"streamer name required"}`, 400)
		return
	}

	var sc *config.StreamerConfig
	for i := range s.cfg.Streamers {
		if s.cfg.Streamers[i].Name == streamerName {
			sc = &s.cfg.Streamers[i]
			break
		}
	}
	if sc == nil {
		http.Error(w, `{"error":"streamer not found"}`, 404)
		return
	}

	// Check permission: admin or assigned room
	if !u.IsAdmin {
		rooms, _ := s.store.GetUserRooms(u.ID)
		allowed := false
		for _, rid := range rooms {
			if rid == sc.RoomID {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, `{"error":"forbidden"}`, 403)
			return
		}
	}

	// Filter available accounts for non-admin
	var allowedAccounts map[string]bool
	if !u.IsAdmin {
		accts, _ := s.store.GetUserAccounts(u.ID)
		allowedAccounts = make(map[string]bool)
		for _, a := range accts {
			allowedAccounts[a] = true
		}
	}

	switch r.Method {
	case "GET":
		json.NewEncoder(w).Encode(sc.Outputs)

	case "POST", "PUT":
		var req config.OutputConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, 400)
			return
		}
		if req.Name == "" {
			http.Error(w, `{"error":"name required"}`, 400)
			return
		}
		if req.Platform == "" {
			req.Platform = "bilibili"
		}
		// Non-admin can only use their assigned accounts
		if allowedAccounts != nil && req.Account != "" && !allowedAccounts[req.Account] {
			http.Error(w, `{"error":"account not assigned to you"}`, 403)
			return
		}
		found := false
		for i, o := range sc.Outputs {
			if o.Name == req.Name {
				sc.Outputs[i] = req
				found = true
				break
			}
		}
		if !found {
			sc.Outputs = append(sc.Outputs, req)
		}
		if err := config.Save(s.cfgPath, s.cfg); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}
		// Sync full output list to controller
		{
			rt := s.getOrCreateRuntime(streamerName)
			rt.paused[req.Name] = true
			if rt.ctrl != nil {
				rt.ctrl.SyncOutputs(sc.Outputs)
			}
		}
		action := "add_output"
		if found {
			action = "update_output"
		}
		s.audit(r, action, fmt.Sprintf("%s / %s lang=%s", streamerName, req.Name, req.TargetLang))
		json.NewEncoder(w).Encode(map[string]any{"ok": true})

	case "DELETE":
		outputName := r.URL.Query().Get("name")
		if outputName == "" {
			http.Error(w, `{"error":"output name required"}`, 400)
			return
		}
		newOutputs := make([]config.OutputConfig, 0)
		for _, o := range sc.Outputs {
			if o.Name != outputName {
				newOutputs = append(newOutputs, o)
			}
		}
		sc.Outputs = newOutputs
		if err := config.Save(s.cfgPath, s.cfg); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}
		s.audit(r, "delete_output", fmt.Sprintf("%s / %s", streamerName, outputName))
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
		if rt := s.streamers[streamerName]; rt != nil && rt.ctrl != nil {
			rt.ctrl.SyncOutputs(sc.Outputs)
		}

	default:
		http.Error(w, `{"error":"method not allowed"}`, 405)
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
