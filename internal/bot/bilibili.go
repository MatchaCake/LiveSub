package bot

import (
	"context"
	"log/slog"
	"sync"
	"time"

	dm "github.com/MatchaCake/bilibili_dm_lib"
)

// BilibiliBot sends danmaku to a Bilibili live room.
type BilibiliBot struct {
	name       string
	roomID     int64
	sessdata   string
	biliJCT    string
	uid        int64
	danmakuMax int

	mu     sync.Mutex
	sender *dm.Sender
}

// NewBilibiliBot creates a new Bilibili danmaku bot.
func NewBilibiliBot(name string, roomID int64, sessdata, biliJCT string, uid int64, danmakuMax int) *BilibiliBot {
	if danmakuMax <= 0 {
		danmakuMax = 20
	}
	b := &BilibiliBot{
		name:       name,
		roomID:     roomID,
		sessdata:   sessdata,
		biliJCT:    biliJCT,
		uid:        uid,
		danmakuMax: danmakuMax,
	}
	b.sender = dm.NewSender(
		dm.WithSenderCookie(sessdata, biliJCT),
		dm.WithMaxLength(danmakuMax),
		dm.WithCooldown(2*time.Second),
	)
	return b
}

func (b *BilibiliBot) Platform() string   { return "bilibili" }
func (b *BilibiliBot) Name() string       { return b.name }
func (b *BilibiliBot) Available() bool     { return b.sessdata != "" }
func (b *BilibiliBot) RoomID() int64      { return b.roomID }
func (b *BilibiliBot) DanmakuMax() int    { return b.danmakuMax }
func (b *BilibiliBot) MaxMessageLen() int  { return b.danmakuMax }
func (b *BilibiliBot) SESSDATA() string   { return b.sessdata }
func (b *BilibiliBot) BiliJCT() string    { return b.biliJCT }

// SetRoomID updates the target room for this bot.
func (b *BilibiliBot) SetRoomID(roomID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roomID = roomID
}

// Send sends a danmaku message to the specified room. Long messages are split into chunks.
// If roomID is 0, falls back to the bot's default roomID.
func (b *BilibiliBot) Send(ctx context.Context, roomID int64, msg string) error {
	b.mu.Lock()
	sender := b.sender
	if roomID == 0 {
		roomID = b.roomID
	}
	b.mu.Unlock()

	err := sender.Send(ctx, roomID, msg)
	if err != nil {
		slog.Warn("danmaku send failed", "bot", b.name, "room", roomID, "error", err)
	}
	return err
}

// UpdateCredentials replaces the bot's credentials and rebuilds the sender.
func (b *BilibiliBot) UpdateCredentials(sessdata, biliJCT string, uid int64, danmakuMax int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessdata = sessdata
	b.biliJCT = biliJCT
	b.uid = uid
	if danmakuMax > 0 {
		b.danmakuMax = danmakuMax
	}
	b.sender = dm.NewSender(
		dm.WithSenderCookie(sessdata, biliJCT),
		dm.WithMaxLength(b.danmakuMax),
		dm.WithCooldown(2*time.Second),
	)
}
