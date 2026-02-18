package danmaku

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	dm "github.com/MatchaCake/bilibili_dm_lib"
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
// Uses bilibili_dm_lib under the hood.
type BilibiliSender struct {
	RoomID    int64
	MaxLength int // default max chars per danmaku (20=default, 30=UL20+)

	mu       sync.RWMutex
	accounts []Account
	current  int // index of current account
	senders  []*dm.Sender // one dm.Sender per account
}

func NewBilibiliSender(roomID int64, sessdata, biliJCT string, uid int64) *BilibiliSender {
	s := &BilibiliSender{
		RoomID:    roomID,
		MaxLength: 20,
	}
	if sessdata != "" {
		s.addAccountLocked(Account{
			Name:     "默认",
			SESSDATA: sessdata,
			BiliJCT:  biliJCT,
			UID:      uid,
		})
	}
	return s
}

// buildSender creates a dm.Sender for the given account.
func (s *BilibiliSender) buildSender(a Account) *dm.Sender {
	maxLen := a.DanmakuMax
	if maxLen <= 0 {
		maxLen = s.MaxLength
	}
	if maxLen <= 0 {
		maxLen = 20
	}
	// Set lib maxLength to full maxLen — we handle 【】 splitting ourselves
	return dm.NewSender(
		dm.WithSenderCookie(a.SESSDATA, a.BiliJCT),
		dm.WithMaxLength(maxLen),
		dm.WithCooldown(5*time.Second),
	)
}

// addAccountLocked appends an account (no lock, caller must hold lock or be in constructor).
func (s *BilibiliSender) addAccountLocked(a Account) {
	s.accounts = append(s.accounts, a)
	s.senders = append(s.senders, s.buildSender(a))
}

// SetAccounts replaces all accounts.
func (s *BilibiliSender) SetAccounts(accounts []Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts = make([]Account, len(accounts))
	s.senders = make([]*dm.Sender, len(accounts))
	for i, a := range accounts {
		s.accounts[i] = a
		s.senders[i] = s.buildSender(a)
	}
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

// Send sends a danmaku message to the live room, wrapped in 【】.
// Long messages are split into chunks, each wrapped in 【】.
// Uses the currently selected account's dm.Sender.
func (s *BilibiliSender) Send(msg string) error {
	s.mu.RLock()
	if len(s.senders) == 0 {
		s.mu.RUnlock()
		return fmt.Errorf("no danmaku account configured")
	}
	sender := s.senders[s.current]
	acct := s.accounts[s.current]
	s.mu.RUnlock()

	maxLen := acct.DanmakuMax
	if maxLen <= 0 {
		maxLen = s.MaxLength
	}
	if maxLen <= 0 {
		maxLen = 20
	}

	wrapped := "【" + msg + "】"
	if len([]rune(wrapped)) <= maxLen {
		err := sender.Send(context.Background(), s.RoomID, wrapped)
		if err != nil {
			slog.Warn("danmaku send failed", "room", s.RoomID, "error", err)
		}
		return err
	}

	// Split inner content, wrap each chunk
	contentRunes := []rune(msg)
	chunkSize := maxLen - 2 // for 【】
	if chunkSize < 1 {
		chunkSize = 1
	}
	for i := 0; i < len(contentRunes); i += chunkSize {
		end := i + chunkSize
		if end > len(contentRunes) {
			end = len(contentRunes)
		}
		chunk := "【" + string(contentRunes[i:end]) + "】"
		if err := sender.Send(context.Background(), s.RoomID, chunk); err != nil {
			slog.Warn("danmaku send failed", "room", s.RoomID, "error", err)
			return err
		}
	}
	return nil
}
