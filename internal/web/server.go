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

	"github.com/christian-lee/livesub/internal/auth"
	"github.com/christian-lee/livesub/internal/bot"
	"github.com/christian-lee/livesub/internal/config"
	"github.com/christian-lee/livesub/internal/controller"
	"github.com/christian-lee/livesub/internal/transcript"
)

// RoomState tracks the overall state for the web UI.
type RoomState struct {
	RoomID   int64                    `json:"room_id"`
	Name     string                   `json:"name"`
	Live     bool                     `json:"live"`
	Outputs  []controller.OutputState `json:"outputs"`
	BotNames []string                 `json:"bot_names"`
}

// session stores user info
type session struct {
	UserID int64
	Expiry time.Time
}

// Server serves the control panel with SQLite-based authentication
type Server struct {
	pool            *bot.Pool
	port            int
	store           *auth.Store
	cfg             *config.Config
	cfgPath         string
	sessions        sync.Map // token → session
	onAccountChange func()
	transcriptDir   string

	mu   sync.RWMutex
	ctrl *controller.Controller
	live bool
}

func NewServer(pool *bot.Pool, port int, store *auth.Store, transcriptDir string, cfg *config.Config, cfgPath string) *Server {
	return &Server{
		pool:          pool,
		port:          port,
		store:         store,
		cfg:           cfg,
		cfgPath:       cfgPath,
		transcriptDir: transcriptDir,
	}
}

// OnAccountChange registers a callback when bilibili accounts change.
func (s *Server) OnAccountChange(fn func()) {
	s.onAccountChange = fn
}

// SetController sets the active controller (when stream goes live).
func (s *Server) SetController(ctrl *controller.Controller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctrl = ctrl
}

// SetLive updates live status.
func (s *Server) SetLive(live bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live = live
}

func (s *Server) Start() {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("/login", s.handleLoginPage)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)

	// Authenticated
	mux.HandleFunc("/", s.requireAuth(s.handleIndex))
	mux.HandleFunc("/api/status", s.requireAuth(s.handleStatus))
	mux.HandleFunc("/api/toggle", s.requireAuth(s.handleToggle))
	mux.HandleFunc("/api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("/api/transcripts", s.requireAuth(s.handleTranscripts))
	mux.HandleFunc("/api/transcripts/download", s.requireAuth(s.handleTranscriptDownload))

	// Admin only
	mux.HandleFunc("/admin", s.requireAdmin(s.handleAdminPage))
	mux.HandleFunc("/api/admin/users", s.requireAdmin(s.handleAdminUsers))
	mux.HandleFunc("/api/admin/user", s.requireAdmin(s.handleAdminUser))
	mux.HandleFunc("/api/admin/all-accounts", s.requireAdmin(s.handleAdminAllAccounts))
	mux.HandleFunc("/api/admin/audit", s.requireAdmin(s.handleAdminAudit))
	mux.HandleFunc("/api/admin/bili-accounts", s.requireAdmin(s.handleBiliAccounts))
	mux.HandleFunc("/api/admin/bili-account", s.requireAdmin(s.handleBiliAccount))
	mux.HandleFunc("/api/admin/bili-qr/generate", s.requireAdmin(s.handleBiliQRGenerate))
	mux.HandleFunc("/api/admin/bili-qr/poll", s.requireAdmin(s.handleBiliQRPoll))
	mux.HandleFunc("/api/admin/streamer", s.requireAdmin(s.handleAdminStreamer))
	mux.HandleFunc("/api/admin/outputs", s.requireAdmin(s.handleAdminOutputs))

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

// --- Status handler (replaces old rooms handler) ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	live := s.live
	ctrl := s.ctrl
	s.mu.RUnlock()

	state := RoomState{
		RoomID:   s.cfg.Streamer.RoomID,
		Name:     s.cfg.Streamer.Name,
		Live:     live,
		BotNames: s.pool.Names(),
	}

	if ctrl != nil {
		state.Outputs = ctrl.OutputStates()
	} else {
		// Show configured outputs even when offline
		state.Outputs = make([]controller.OutputState, len(s.cfg.Outputs))
		for i, o := range s.cfg.Outputs {
			state.Outputs[i] = controller.OutputState{
				Name:       o.Name,
				Platform:   o.Platform,
				TargetLang: o.TargetLang,
				BotName:    o.Account,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request) {
	u := s.getUser(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	outputName := r.URL.Query().Get("output")
	if outputName == "" {
		http.Error(w, `{"error":"output name required"}`, 400)
		return
	}

	s.mu.RLock()
	ctrl := s.ctrl
	s.mu.RUnlock()

	if ctrl == nil {
		http.Error(w, `{"error":"no active stream"}`, 400)
		return
	}

	paused := ctrl.TogglePause(outputName)
	if paused {
		s.audit(r, "暂停翻译", fmt.Sprintf("输出 %s", outputName))
	} else {
		s.audit(r, "恢复翻译", fmt.Sprintf("输出 %s", outputName))
	}
	slog.Info("output toggled", "output", outputName, "paused", paused, "user", u.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"output": outputName, "paused": paused})
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
	// Also include DB accounts
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

// handleAdminStreamer GET returns streamer config, POST/PUT updates it.
func (s *Server) handleAdminStreamer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "GET" {
		json.NewEncoder(w).Encode(s.cfg.Streamer)
		return
	}
	if r.Method != "POST" && r.Method != "PUT" {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		return
	}
	var req struct {
		Name       string `json:"name"`
		RoomID     int64  `json:"room_id"`
		SourceLang string `json:"source_lang"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, 400)
		return
	}
	if req.RoomID == 0 {
		http.Error(w, `{"error":"room_id required"}`, 400)
		return
	}
	s.cfg.Streamer.Name = req.Name
	s.cfg.Streamer.RoomID = req.RoomID
	if req.SourceLang != "" {
		s.cfg.Streamer.SourceLang = req.SourceLang
	}
	if err := config.Save(s.cfgPath, s.cfg); err != nil {
		slog.Error("save config failed", "err", err)
		http.Error(w, `{"error":"save failed"}`, 500)
		return
	}
	s.store.Log(0, "admin", "update_streamer", fmt.Sprintf("room=%d name=%s", req.RoomID, req.Name), r.RemoteAddr)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// handleAdminOutputs GET returns outputs, POST adds, PUT updates, DELETE removes.
func (s *Server) handleAdminOutputs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "GET" {
		json.NewEncoder(w).Encode(s.cfg.Outputs)
		return
	}
	if r.Method == "DELETE" {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, `{"error":"name required"}`, 400)
			return
		}
		newOutputs := make([]config.OutputConfig, 0)
		for _, o := range s.cfg.Outputs {
			if o.Name != name {
				newOutputs = append(newOutputs, o)
			}
		}
		s.cfg.Outputs = newOutputs
		if err := config.Save(s.cfgPath, s.cfg); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}
		s.store.Log(0, "admin", "delete_output", name, r.RemoteAddr)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		return
	}
	if r.Method == "POST" || r.Method == "PUT" {
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
		// Update existing or add new
		found := false
		for i, o := range s.cfg.Outputs {
			if o.Name == req.Name {
				s.cfg.Outputs[i] = req
				found = true
				break
			}
		}
		if !found {
			s.cfg.Outputs = append(s.cfg.Outputs, req)
		}
		if err := config.Save(s.cfgPath, s.cfg); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}
		action := "add_output"
		if found {
			action = "update_output"
		}
		s.store.Log(0, "admin", action, fmt.Sprintf("name=%s platform=%s lang=%s room=%d", req.Name, req.Platform, req.TargetLang, req.RoomID), r.RemoteAddr)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		return
	}
	http.Error(w, `{"error":"method not allowed"}`, 405)
}
