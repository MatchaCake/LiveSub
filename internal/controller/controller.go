package controller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/christian-lee/livesub/internal/bot"
	"github.com/christian-lee/livesub/internal/config"
	"github.com/christian-lee/livesub/internal/transcript"
	"github.com/christian-lee/livesub/internal/translate"
)

// Translation is a multi-language translation result from the Agent.
type Translation struct {
	Seq        int               // sequence number for ordering
	SourceText string            // original STT text
	SourceLang string            // detected language code
	Texts      map[string]string // target_lang → translated text (empty key = source text)
}

// PendingMsg is a message waiting to be sent (with delay for review).
type PendingMsg struct {
	ID   int64  `json:"id"`
	Text string `json:"text"`
	// SendAt is unix milliseconds when the message will be sent.
	SendAt int64 `json:"send_at"`
}

// OutputState tracks per-output status for the web UI.
type OutputState struct {
	Name       string       `json:"name"`
	Platform   string       `json:"platform"`
	TargetLang string       `json:"target_lang"`
	BotName    string       `json:"bot_name"`
	RoomID     int64        `json:"room_id"`
	Paused     bool         `json:"paused"`
	LastText   string       `json:"last_text"`
	Pending    []PendingMsg `json:"pending"` // messages waiting to send
	Recent     []string     `json:"recent"`  // last N sent messages
}

const maxRecent = 5

// Controller receives translations from the Agent and routes them to bots.
type Controller struct {
	pool           *bot.Pool
	outputs        []config.OutputConfig
	tlog           *transcript.Logger
	streamerRoomID int64

	mu           sync.RWMutex
	paused       map[string]bool // output name → paused
	outputStates map[string]*OutputState
	skipSet      map[int64]bool // pending msg IDs to skip
	nextMsgID    int64

	sendDelay time.Duration // delay before sending (default 3s)
	ch        chan Translation
	done      chan struct{}
	wg        sync.WaitGroup
}

// New creates a Controller with the given bot pool and output configuration.
// streamerRoomID is the room being monitored; used as fallback when output room_id=0.
func New(pool *bot.Pool, outputs []config.OutputConfig, tlog *transcript.Logger, streamerRoomID int64) *Controller {
	states := make(map[string]*OutputState)
	paused := make(map[string]bool)
	for _, o := range outputs {
		states[o.Name] = &OutputState{
			Name:       o.Name,
			Platform:   o.Platform,
			TargetLang: o.TargetLang,
			BotName:    o.Account,
			RoomID:     o.RoomID,
		}
		paused[o.Name] = false
	}

	return &Controller{
		pool:           pool,
		outputs:        outputs,
		tlog:           tlog,
		streamerRoomID: streamerRoomID,
		paused:         paused,
		outputStates:   states,
		skipSet:        make(map[int64]bool),
		sendDelay:      3 * time.Second,
		ch:             make(chan Translation, 100),
		done:           make(chan struct{}),
	}
}

// Start begins processing translations. Call Stop to shut down.
func (c *Controller) Start(ctx context.Context) {
	c.wg.Add(1)
	go c.run(ctx)
}

// Submit sends a translation to the controller for routing.
func (c *Controller) Submit(t Translation) {
	c.ch <- t
}

// Stop gracefully shuts down the controller.
func (c *Controller) Stop() {
	close(c.ch)
	c.wg.Wait()
}

// TogglePause toggles pause state for an output. Returns new paused state.
func (c *Controller) TogglePause(outputName string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused[outputName] = !c.paused[outputName]
	if s, ok := c.outputStates[outputName]; ok {
		s.Paused = c.paused[outputName]
	}
	return c.paused[outputName]
}

// SetPaused sets pause state for an output.
func (c *Controller) SetPaused(outputName string, paused bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused[outputName] = paused
	if s, ok := c.outputStates[outputName]; ok {
		s.Paused = paused
	}
}

// IsPaused returns whether an output is paused.
func (c *Controller) IsPaused(outputName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.paused[outputName]
}

// IsAnyPaused returns true if ALL outputs are paused (gates STT).
func (c *Controller) IsAnyPaused() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, p := range c.paused {
		if !p {
			return false
		}
	}
	return len(c.paused) > 0
}

// SkipPending marks a pending message to be skipped (not sent).
func (c *Controller) SkipPending(msgID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.skipSet[msgID] = true
	// Also remove from pending in outputStates for UI feedback
	for _, st := range c.outputStates {
		for i, p := range st.Pending {
			if p.ID == msgID {
				st.Pending = append(st.Pending[:i], st.Pending[i+1:]...)
				break
			}
		}
	}
}

// OutputStates returns the current state of all outputs in config order.
func (c *Controller) OutputStates() []OutputState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]OutputState, 0, len(c.outputs))
	for _, o := range c.outputs {
		if s, ok := c.outputStates[o.Name]; ok {
			cp := *s
			cp.Pending = make([]PendingMsg, len(s.Pending))
			copy(cp.Pending, s.Pending)
			cp.Recent = make([]string, len(s.Recent))
			copy(cp.Recent, s.Recent)
			out = append(out, cp)
		}
	}
	return out
}

// delayedMsg is a message in the per-output delay queue.
type delayedMsg struct {
	id     int64
	text   string
	sendAt time.Time
	output string // output name
	seqNum int    // seqCounter value for emoji
}

func (c *Controller) run(ctx context.Context) {
	defer c.wg.Done()

	// Per-output ordered sender
	type outputSender struct {
		nextSeq    int
		seqCounter int
		pending    map[int]string // seq → text to send
	}
	senders := make(map[string]*outputSender)
	for _, o := range c.outputs {
		senders[o.Name] = &outputSender{pending: make(map[int]string)}
	}

	// Delay queue: messages waiting to be sent
	var delayQueue []delayedMsg

	// Ticker to check delay queue
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case t, ok := <-c.ch:
			if !ok {
				// Channel closed — flush remaining
				c.flushDelayQueue(ctx, delayQueue)
				return
			}
			for _, o := range c.outputs {
				var text string
				if o.TargetLang == "" {
					text = t.SourceText
				} else {
					text = t.Texts[o.TargetLang]
					if text == "" {
						if isLangMatch(t.SourceLang, o.TargetLang) {
							text = t.SourceText
						}
					}
				}

				// Log transcript
				if c.tlog != nil && text != "" {
					targetLang := o.TargetLang
					if targetLang == "" {
						targetLang = t.SourceLang
					}
					c.tlog.Write(t.SourceLang, t.SourceText, targetLang, text)
				}

				// Buffer for ordered sending
				s := senders[o.Name]
				s.pending[t.Seq] = text

				// Flush in order → push to delay queue
				for {
					txt, ok := s.pending[s.nextSeq]
					if !ok {
						break
					}
					delete(s.pending, s.nextSeq)
					s.nextSeq++

					if txt == "" {
						continue
					}

					c.mu.Lock()
					isPaused := c.paused[o.Name]
					c.mu.Unlock()

					if isPaused {
						slog.Info("paused, dropping", "output", o.Name, "text", txt)
						continue
					}

					// Assign message ID and push to delay queue
					c.mu.Lock()
					msgID := c.nextMsgID
					c.nextMsgID++
					sendAt := time.Now().Add(c.sendDelay)
					// Add to pending in output state for UI
					if st, ok := c.outputStates[o.Name]; ok {
						st.Pending = append(st.Pending, PendingMsg{
							ID:     msgID,
							Text:   txt,
							SendAt: sendAt.UnixMilli(),
						})
						st.LastText = txt
					}
					c.mu.Unlock()

					delayQueue = append(delayQueue, delayedMsg{
						id:     msgID,
						text:   txt,
						sendAt: sendAt,
						output: o.Name,
						seqNum: s.seqCounter,
					})
					s.seqCounter++
				}
			}

		case <-ticker.C:
			// Send messages whose delay has expired
			delayQueue = c.processDelayQueue(ctx, delayQueue)

		case <-ctx.Done():
			return
		}
	}
}

func (c *Controller) processDelayQueue(ctx context.Context, queue []delayedMsg) []delayedMsg {
	now := time.Now()
	remaining := queue[:0]
	for _, dm := range queue {
		if now.Before(dm.sendAt) {
			remaining = append(remaining, dm)
			continue
		}

		// Check if skipped
		c.mu.Lock()
		skipped := c.skipSet[dm.id]
		if skipped {
			delete(c.skipSet, dm.id)
		}
		// Remove from pending
		if st, ok := c.outputStates[dm.output]; ok {
			for i, p := range st.Pending {
				if p.ID == dm.id {
					st.Pending = append(st.Pending[:i], st.Pending[i+1:]...)
					break
				}
			}
		}
		// Check if paused at send time
		isPaused := c.paused[dm.output]
		c.mu.Unlock()

		if skipped {
			slog.Info("skipped by user", "output", dm.output, "text", dm.text)
			continue
		}
		if isPaused {
			slog.Info("paused at send time, dropping", "output", dm.output, "text", dm.text)
			continue
		}

		c.sendMessage(ctx, dm)
	}
	return remaining
}

func (c *Controller) flushDelayQueue(ctx context.Context, queue []delayedMsg) {
	for _, dm := range queue {
		c.mu.Lock()
		skipped := c.skipSet[dm.id]
		if skipped {
			delete(c.skipSet, dm.id)
		}
		c.mu.Unlock()
		if !skipped {
			c.sendMessage(ctx, dm)
		}
	}
}

func (c *Controller) sendMessage(ctx context.Context, dm delayedMsg) {
	// Find output config
	var o *config.OutputConfig
	for i := range c.outputs {
		if c.outputs[i].Name == dm.output {
			o = &c.outputs[i]
			break
		}
	}
	if o == nil {
		return
	}

	b := c.pool.Get(o.Account)
	if b == nil {
		slog.Warn("bot not found", "output", dm.output, "bot", o.Account)
		return
	}

	targetRoom := o.RoomID
	if targetRoom == 0 {
		targetRoom = c.streamerRoomID
	}

	seqEmoji := seqEmojis[dm.seqNum%len(seqEmojis)]
	chunks := splitWithWrap(dm.text, o.Prefix+seqEmoji, o.Suffix, b.MaxMessageLen())
	for _, chunk := range chunks {
		slog.Info("sending", "output", dm.output, "bot", b.Name(), "room", targetRoom, "text", chunk)
		if err := b.Send(ctx, targetRoom, chunk); err != nil {
			slog.Error("send failed", "output", dm.output, "bot", b.Name(), "err", err)
			break
		}
	}

	// Add to recent
	c.mu.Lock()
	if st, ok := c.outputStates[dm.output]; ok {
		st.Recent = append(st.Recent, dm.text)
		if len(st.Recent) > maxRecent {
			st.Recent = st.Recent[len(st.Recent)-maxRecent:]
		}
	}
	c.mu.Unlock()
}

// splitWithWrap splits text into chunks where each chunk is wrapped with prefix+suffix
// and fits within maxLen runes. If maxLen <= 0, returns a single wrapped string.
// For text containing spaces (e.g. English), splits at word boundaries.
func splitWithWrap(text, prefix, suffix string, maxLen int) []string {
	wrapped := prefix + text + suffix
	if maxLen <= 0 || len([]rune(wrapped)) <= maxLen {
		return []string{wrapped}
	}

	prefixRunes := len([]rune(prefix))
	suffixRunes := len([]rune(suffix))
	contentMax := maxLen - prefixRunes - suffixRunes
	if contentMax <= 0 {
		return []string{wrapped}
	}

	runes := []rune(text)
	var chunks []string
	i := 0
	for i < len(runes) {
		end := i + contentMax
		if end >= len(runes) {
			chunks = append(chunks, prefix+string(runes[i:])+suffix)
			break
		}
		breakAt := end
		for j := end - 1; j > i+contentMax/2; j-- {
			if runes[j] == ' ' || runes[j] == '、' || runes[j] == '，' || runes[j] == '。' {
				breakAt = j + 1
				break
			}
		}
		chunks = append(chunks, prefix+string(runes[i:breakAt])+suffix)
		i = breakAt
	}
	return chunks
}

// seqEmojis are number emojis 0-10 for message sequence display.
var seqEmojis = []string{"0️⃣", "1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣", "8️⃣", "9️⃣", "🔟"}

// isLangMatch checks if detected language matches a target language code.
func isLangMatch(detected, target string) bool {
	if detected == "" || target == "" {
		return false
	}
	if len(detected) >= 2 && len(target) >= 2 {
		if detected[:2] == target[:2] {
			return true
		}
	}
	if len(detected) >= 3 && detected[:3] == "cmn" && len(target) >= 2 && target[:2] == "zh" {
		return true
	}
	return false
}

// TranslateAndSubmit handles the translation fan-out for a single STT result.
func TranslateAndSubmit(ctx context.Context, ctrl *Controller, translator *translate.GeminiTranslator, seq int, sourceText, sourceLang string, outputs []config.OutputConfig) {
	needed := make(map[string]bool)
	for _, o := range outputs {
		if o.TargetLang != "" && !isLangMatch(sourceLang, o.TargetLang) {
			needed[o.TargetLang] = true
		}
	}

	texts := make(map[string]string)

	if len(needed) == 0 {
		ctrl.Submit(Translation{
			Seq:        seq,
			SourceText: sourceText,
			SourceLang: sourceLang,
			Texts:      texts,
		})
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for lang := range needed {
		wg.Add(1)
		go func(targetLang string) {
			defer wg.Done()
			translated, err := translator.Translate(ctx, sourceText, sourceLang, targetLang)
			if err != nil {
				slog.Error("translate error", "lang", targetLang, "err", err)
				return
			}
			mu.Lock()
			texts[targetLang] = translated
			mu.Unlock()
		}(lang)
	}
	wg.Wait()

	ctrl.Submit(Translation{
		Seq:        seq,
		SourceText: sourceText,
		SourceLang: sourceLang,
		Texts:      texts,
	})
}
