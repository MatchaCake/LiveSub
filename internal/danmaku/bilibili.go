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
	"sync"
	"time"
)

// Account represents a Bilibili account for sending danmaku.
type Account struct {
	Name       string
	SESSDATA   string
	BiliJCT    string
	UID        int64
	DanmakuMax int // per-account max chars (0=use sender default)
}

// BilibiliSender sends danmaku messages to a Bilibili live room.
// Supports multiple accounts with runtime switching.
type BilibiliSender struct {
	RoomID    int64
	MaxLength int // max chars per danmaku (20=default, 30=UL20+)

	mu       sync.RWMutex
	accounts []Account
	current  int // index of current account

	client   *http.Client
	lastSend time.Time
	cooldown time.Duration
}

func NewBilibiliSender(roomID int64, sessdata, biliJCT string, uid int64) *BilibiliSender {
	s := &BilibiliSender{
		RoomID:    roomID,
		MaxLength: 20,
		client:    &http.Client{Timeout: 10 * time.Second},
		cooldown:  5 * time.Second,
	}
	// Add the default account
	if sessdata != "" {
		s.accounts = append(s.accounts, Account{
			Name:     "默认",
			SESSDATA: sessdata,
			BiliJCT:  biliJCT,
			UID:      uid,
		})
	}
	return s
}

// AddAccount appends an account (deduplicates by name).
func (s *BilibiliSender) AddAccount(a Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.accounts {
		if existing.Name == a.Name {
			s.accounts[i] = a // update
			return
		}
	}
	s.accounts = append(s.accounts, a)
}

// SetAccounts replaces all accounts. Keeps current index valid.
func (s *BilibiliSender) SetAccounts(accounts []Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts = accounts
	if s.current >= len(s.accounts) {
		s.current = 0
	}
}

// SwitchAccount switches to the account at the given index.
func (s *BilibiliSender) SwitchAccount(index int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.accounts) {
		return false
	}
	s.current = index
	slog.Info("switched danmaku account", "room", s.RoomID, "account", s.accounts[index].Name)
	return true
}

// CurrentAccount returns the index and name of the current account.
func (s *BilibiliSender) CurrentAccount() (int, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.accounts) == 0 {
		return -1, ""
	}
	return s.current, s.accounts[s.current].Name
}

// AccountNames returns all account names.
func (s *BilibiliSender) AccountNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, len(s.accounts))
	for i, a := range s.accounts {
		names[i] = a.Name
	}
	return names
}

func (s *BilibiliSender) getCredentials() (sessdata, biliJCT string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.accounts) == 0 {
		return "", ""
	}
	a := s.accounts[s.current]
	return a.SESSDATA, a.BiliJCT
}

// getMaxLength returns the effective max length for the current account.
func (s *BilibiliSender) getMaxLength() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.accounts) > 0 {
		if m := s.accounts[s.current].DanmakuMax; m > 0 {
			return m
		}
	}
	if s.MaxLength > 0 {
		return s.MaxLength
	}
	return 20
}

// Send sends a danmaku message to the live room, wrapped in 【】.
// Long messages are split into multiple danmaku.
func (s *BilibiliSender) Send(msg string) error {
	wrapped := "【" + msg + "】"
	runes := []rune(wrapped)

	maxLen := s.getMaxLength()

	if len(runes) <= maxLen {
		return s.sendOne(wrapped)
	}

	contentRunes := []rune(msg)
	chunkSize := maxLen - 2
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
	if elapsed := time.Since(s.lastSend); elapsed < s.cooldown {
		time.Sleep(s.cooldown - elapsed)
	}

	sessdata, biliJCT := s.getCredentials()
	if sessdata == "" {
		return fmt.Errorf("no danmaku account configured")
	}

	form := url.Values{
		"bubble":     {"0"},
		"msg":        {msg},
		"color":      {"16777215"},
		"mode":       {"1"},
		"fontsize":   {"25"},
		"rnd":        {strconv.FormatInt(time.Now().Unix(), 10)},
		"roomid":     {strconv.FormatInt(s.RoomID, 10)},
		"csrf":       {biliJCT},
		"csrf_token": {biliJCT},
	}

	req, err := http.NewRequest("POST",
		"https://api.live.bilibili.com/msg/send",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", fmt.Sprintf("SESSDATA=%s; bili_jct=%s", sessdata, biliJCT))
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
		slog.Warn("danmaku rejected", "room", s.RoomID, "msg", msg, "code", result.Code, "error", errMsg)
		return nil
	}

	s.lastSend = time.Now()
	slog.Info("danmaku sent", "room", s.RoomID, "msg", msg)
	return nil
}
