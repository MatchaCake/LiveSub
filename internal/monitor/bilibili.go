package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// LiveStatus represents the state of a Bilibili live room.
type LiveStatus int

const (
	StatusOffline  LiveStatus = 0 // 未开播
	StatusLive     LiveStatus = 1 // 直播中
	StatusRotation LiveStatus = 2 // 轮播
)

// RoomState tracks a room's live/offline transitions.
type RoomState struct {
	RoomID   int64
	Status   LiveStatus
	Title    string
	WasLive  bool
	LiveTime string
}

// BilibiliMonitor watches multiple rooms and reports live/offline transitions.
type BilibiliMonitor struct {
	client   *http.Client
	interval time.Duration

	mu    sync.Mutex
	rooms map[int64]*RoomState
}

func NewBilibiliMonitor(interval time.Duration) *BilibiliMonitor {
	return &BilibiliMonitor{
		client:   &http.Client{Timeout: 10 * time.Second},
		interval: interval,
		rooms:    make(map[int64]*RoomState),
	}
}

// RoomEvent is emitted when a room goes live or offline.
type RoomEvent struct {
	RoomID int64
	Live   bool // true=just went live, false=just went offline
	Title  string
}

// AddRooms registers new room IDs for monitoring (skips already tracked).
func (m *BilibiliMonitor) AddRooms(roomIDs []int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range roomIDs {
		if _, exists := m.rooms[id]; !exists {
			m.rooms[id] = &RoomState{RoomID: id}
			slog.Info("monitor: added room", "room", id)
		}
	}
}

// RemoveRooms stops monitoring the given rooms and emits offline events for any that were live.
func (m *BilibiliMonitor) RemoveRooms(roomIDs []int64, events chan<- RoomEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range roomIDs {
		if state, exists := m.rooms[id]; exists {
			if state.WasLive {
				events <- RoomEvent{RoomID: id, Live: false}
			}
			delete(m.rooms, id)
			slog.Info("monitor: removed room", "room", id)
		}
	}
}

// Watch monitors the given rooms and sends events on transitions.
// Blocks until context is cancelled.
func (m *BilibiliMonitor) Watch(ctx context.Context, roomIDs []int64, events chan<- RoomEvent) error {
	m.AddRooms(roomIDs)

	slog.Info("bilibili monitor started", "rooms", len(roomIDs), "interval", m.interval)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	m.checkAll(ctx, events)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.checkAll(ctx, events)
		}
	}
}

func (m *BilibiliMonitor) checkAll(ctx context.Context, events chan<- RoomEvent) {
	m.mu.Lock()
	// Snapshot room IDs to check (hold lock briefly, not during HTTP calls)
	roomIDs := make([]int64, 0, len(m.rooms))
	for id := range m.rooms {
		roomIDs = append(roomIDs, id)
	}
	m.mu.Unlock()

	for _, id := range roomIDs {
		if err := m.checkRoom(ctx, id, events); err != nil {
			slog.Warn("check room failed", "room", id, "err", err)
		}
	}
}

func (m *BilibiliMonitor) checkRoom(ctx context.Context, roomID int64, events chan<- RoomEvent) error {
	info, err := m.getRoomInfo(ctx, roomID)
	if err != nil {
		return err
	}

	isLive := info.LiveStatus == 1

	// Hold lock while reading/writing RoomState to avoid data race
	m.mu.Lock()
	state, exists := m.rooms[roomID]
	if !exists {
		m.mu.Unlock()
		return nil // room was removed while we were checking
	}

	var event *RoomEvent
	if isLive && !state.WasLive {
		slog.Info("room went LIVE", "room", roomID, "title", info.Title)
		state.Title = info.Title
		event = &RoomEvent{RoomID: roomID, Live: true, Title: info.Title}
	}
	if !isLive && state.WasLive {
		slog.Info("room went OFFLINE", "room", roomID)
		event = &RoomEvent{RoomID: roomID, Live: false}
	}

	state.WasLive = isLive
	state.Status = LiveStatus(info.LiveStatus)
	m.mu.Unlock()

	// Send event outside lock to avoid blocking while holding mu
	if event != nil {
		events <- *event
	}
	return nil
}

type roomInfoResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		LiveStatus int    `json:"live_status"`
		Title      string `json:"title"`
		LiveTime   string `json:"live_time"`
		UID        int64  `json:"uid"`
	} `json:"data"`
}

type roomInfo struct {
	LiveStatus int
	Title      string
	LiveTime   string
}

func (m *BilibiliMonitor) getRoomInfo(ctx context.Context, roomID int64) (*roomInfo, error) {
	url := fmt.Sprintf("https://api.live.bilibili.com/room/v1/Room/get_info?room_id=%d", roomID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) livesub/1.0")
	req.Header.Set("Referer", "https://live.bilibili.com/")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var r roomInfoResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	if r.Code != 0 {
		return nil, fmt.Errorf("API error %d: %s", r.Code, r.Message)
	}

	return &roomInfo{
		LiveStatus: r.Data.LiveStatus,
		Title:      r.Data.Title,
		LiveTime:   r.Data.LiveTime,
	}, nil
}
