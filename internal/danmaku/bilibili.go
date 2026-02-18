package danmaku

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BilibiliSender sends danmaku messages to a Bilibili live room.
type BilibiliSender struct {
	RoomID    int64
	SESSDATA  string
	BiliJCT   string // csrf token
	UID       int64
	MaxLength int    // max chars per danmaku (20=default, 30=UL20+)

	client   *http.Client
	lastSend time.Time
	cooldown time.Duration // min interval between messages
}

func NewBilibiliSender(roomID int64, sessdata, biliJCT string, uid int64) *BilibiliSender {
	return &BilibiliSender{
		RoomID:    roomID,
		SESSDATA:  sessdata,
		BiliJCT:   biliJCT,
		UID:       uid,
		MaxLength: 20, // default, set to 30 if UL20+
		client:    &http.Client{Timeout: 10 * time.Second},
		cooldown:  5 * time.Second,
	}
}

// Send sends a danmaku message to the live room, wrapped in 【】.
// Long messages are split into multiple danmaku.
func (s *BilibiliSender) Send(msg string) error {
	wrapped := "【" + msg + "】"
	runes := []rune(wrapped)

	// Split into chunks if too long (max 20 chars default, 30 for UL20+)
	maxLen := s.MaxLength
	if maxLen == 0 {
		maxLen = 20
	}

	if len(runes) <= maxLen {
		return s.sendOne(wrapped)
	}

	// Split into chunks, each wrapped in 【】
	contentRunes := []rune(msg)
	chunkSize := maxLen - 2 // reserve for 【】
	if chunkSize < 1 {
		chunkSize = 1
	}

	for i := 0; i < len(contentRunes); i += chunkSize {
		end := i + chunkSize
		if end > len(contentRunes) {
			end = len(contentRunes)
		}
		chunk := "【" + string(contentRunes[i:end]) + "】"

		if err := s.sendOne(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (s *BilibiliSender) sendOne(msg string) error {
	// Respect cooldown
	if elapsed := time.Since(s.lastSend); elapsed < s.cooldown {
		time.Sleep(s.cooldown - elapsed)
	}

	form := url.Values{
		"bubble":   {"0"},
		"msg":      {msg},
		"color":    {"16777215"}, // white
		"mode":     {"1"},        // scroll
		"fontsize": {"25"},
		"rnd":      {strconv.FormatInt(time.Now().Unix(), 10)},
		"roomid":   {strconv.FormatInt(s.RoomID, 10)},
		"csrf":     {s.BiliJCT},
		"csrf_token": {s.BiliJCT},
	}

	req, err := http.NewRequest("POST",
		"https://api.live.bilibili.com/msg/send",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", fmt.Sprintf("SESSDATA=%s; bili_jct=%s", s.SESSDATA, s.BiliJCT))
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) livesub/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send danmaku: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("danmaku API %d: %s", resp.StatusCode, string(body))
	}

	// Parse response to check for errors
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &result); err == nil && result.Code != 0 {
		errMsg := result.Message
		if errMsg == "" {
			errMsg = result.Msg
		}
		// Log but don't return error for content-related failures (sensitive words etc.)
		// These are not retryable
		slog.Warn("danmaku rejected", "room", s.RoomID, "msg", msg, "code", result.Code, "error", errMsg)
		return nil
	}

	s.lastSend = time.Now()
	slog.Info("danmaku sent", "room", s.RoomID, "msg", msg)
	return nil
}
