package command

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	dm "github.com/MatchaCake/bilibili_dm_lib"
	"github.com/christian-lee/livesub/internal/bot"
	"github.com/christian-lee/livesub/internal/controller"
)

// Handler listens for danmaku commands in a live room and executes them.
type Handler struct {
	roomID      int64
	allowedUIDs map[int64]bool
	client      *dm.Client
	pool        *bot.Pool
	replyBot    string // bot name for sending replies

	mu   sync.RWMutex
	ctrl *controller.Controller
}

// New creates a command handler. The dm.Client should already be started.
func New(roomID int64, allowedUIDs []int64, client *dm.Client, opts ...HandlerOption) *Handler {
	allowed := make(map[int64]bool, len(allowedUIDs))
	for _, uid := range allowedUIDs {
		allowed[uid] = true
	}
	h := &Handler{
		roomID:      roomID,
		allowedUIDs: allowed,
		client:      client,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithPool sets the bot pool for sending replies.
func WithPool(p *bot.Pool, replyBot string) HandlerOption {
	return func(h *Handler) {
		h.pool = p
		h.replyBot = replyBot
	}
}

// SetController sets or clears the active controller (called when stream starts/stops).
func (h *Handler) SetController(ctrl *controller.Controller) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ctrl = ctrl
}

// UpdateUIDs replaces the allowed UID whitelist (for hot reload).
func (h *Handler) UpdateUIDs(uids []int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.allowedUIDs = make(map[int64]bool, len(uids))
	for _, uid := range uids {
		h.allowedUIDs[uid] = true
	}
}

// Run starts listening for commands. Blocks until ctx is cancelled.
func (h *Handler) Run(ctx context.Context) {
	slog.Info("command handler started", "room", h.roomID, "allowed_uids", len(h.allowedUIDs))

	events := h.client.Subscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.RoomID != h.roomID || ev.Type != dm.EventDanmaku {
				continue
			}
			d, ok := ev.Data.(*dm.Danmaku)
			if !ok || d == nil {
				continue
			}
			h.handleDanmaku(d)
		}
	}
}

func (h *Handler) handleDanmaku(d *dm.Danmaku) {
	text := strings.TrimSpace(d.Content)
	if !strings.HasPrefix(text, "/") {
		return
	}

	h.mu.RLock()
	allowed := h.allowedUIDs[d.UID]
	ctrl := h.ctrl
	h.mu.RUnlock()

	if !allowed {
		slog.Debug("command rejected: uid not in whitelist", "uid", d.UID, "user", d.Sender, "cmd", text)
		return
	}

	if ctrl == nil {
		slog.Info("command received but no active stream", "uid", d.UID, "user", d.Sender, "cmd", text, "room", h.roomID)
		return
	}

	cmd := strings.ToLower(text)
	// Check for per-output commands first: /暂停 outputName
	parts := strings.SplitN(text, " ", 2)
	action := strings.ToLower(parts[0])

	if len(parts) == 2 {
		target := strings.TrimSpace(parts[1])
		switch action {
		case "/暂停", "/pause", "/off":
			h.pauseOutput(ctrl, target, true, d)
		case "/恢复", "/resume", "/on":
			h.pauseOutput(ctrl, target, false, d)
		default:
			slog.Debug("unknown command", "uid", d.UID, "cmd", text)
		}
		return
	}

	switch cmd {
	case "/暂停", "/pause", "/off":
		h.pauseAll(ctrl, true, d)
	case "/恢复", "/resume", "/on":
		h.pauseAll(ctrl, false, d)
	case "/help", "/帮助":
		h.sendHelp(ctrl, d)
	case "/list", "/列表":
		h.sendList(ctrl, d)
	default:
		slog.Debug("unknown command", "uid", d.UID, "cmd", text)
	}
}

func (h *Handler) sendHelp(ctrl *controller.Controller, d *dm.Danmaku) {
	slog.Info("command: help", "uid", d.UID, "user", d.Sender, "room", h.roomID)
	h.reply(context.Background(), "/off 暂停全部, /off 名称 暂停指定, /on 同理, /list 列表")
}

func (h *Handler) sendList(ctrl *controller.Controller, d *dm.Danmaku) {
	slog.Info("command: list", "uid", d.UID, "user", d.Sender, "room", h.roomID)
	states := ctrl.OutputStates()
	if len(states) == 0 {
		h.reply(context.Background(), "当前无输出")
		return
	}
	var parts []string
	for _, s := range states {
		status := "▶"
		if s.Paused {
			status = "⏸"
		}
		parts = append(parts, status+s.Name)
	}
	h.reply(context.Background(), strings.Join(parts, " | "))
}

func (h *Handler) reply(ctx context.Context, msg string) {
	if h.pool == nil || h.replyBot == "" {
		return
	}
	b := h.pool.Get(h.replyBot)
	if b == nil {
		return
	}
	if err := b.Send(ctx, h.roomID, msg); err != nil {
		slog.Warn("command reply failed", "err", err)
	}
}

func (h *Handler) pauseAll(ctrl *controller.Controller, paused bool, d *dm.Danmaku) {
	states := ctrl.OutputStates()
	for _, s := range states {
		ctrl.SetPaused(s.Name, paused)
	}
	action := "paused"
	if !paused {
		action = "resumed"
	}
	slog.Info("command executed",
		"action", action,
		"scope", "all",
		"uid", d.UID,
		"user", d.Sender,
		"room", h.roomID,
	)
}

func (h *Handler) pauseOutput(ctrl *controller.Controller, name string, paused bool, d *dm.Danmaku) {
	states := ctrl.OutputStates()
	var matched string
	for _, s := range states {
		if s.Name == name {
			matched = s.Name
			break
		}
	}
	if matched == "" {
		for _, s := range states {
			if strings.EqualFold(s.Name, name) {
				matched = s.Name
				break
			}
		}
	}
	if matched == "" {
		slog.Warn("command: output not found", "name", name, "uid", d.UID)
		return
	}

	ctrl.SetPaused(matched, paused)
	action := "paused"
	if !paused {
		action = "resumed"
	}
	slog.Info("command executed",
		"action", action,
		"output", matched,
		"uid", d.UID,
		"user", d.Sender,
		"room", h.roomID,
	)
}
