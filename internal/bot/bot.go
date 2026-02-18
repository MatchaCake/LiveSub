package bot

import "context"

// Bot is the interface for platform-specific message senders.
type Bot interface {
	// Send sends a message to the specified room.
	// roomID is platform-specific (e.g. Bilibili room number).
	Send(ctx context.Context, roomID int64, msg string) error
	// Platform returns the platform identifier (e.g. "bilibili").
	Platform() string
	// Name returns the bot's display name.
	Name() string
	// Available returns whether the bot is ready to send.
	Available() bool
	// MaxMessageLen returns the max rune length per message (0 = no limit).
	MaxMessageLen() int
}
