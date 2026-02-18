package bot

import "context"

// Bot is the interface for platform-specific message senders.
type Bot interface {
	// Send sends a message to the target destination.
	Send(ctx context.Context, msg string) error
	// Platform returns the platform identifier (e.g. "bilibili").
	Platform() string
	// Name returns the bot's display name.
	Name() string
	// Available returns whether the bot is ready to send.
	Available() bool
}
