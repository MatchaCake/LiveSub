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
	ID        int64  `json:"id"`
	Text      string `json:"text"`
	SendAt    int64  `json:"send_at"`     // unix ms
	RemainSec int    `json:"remain_sec"`  // computed at read time
}

// OutputState tracks per-output status for the web UI.
type OutputState struct {
	Name       string       `json:"name"`
	Platform   string       `json:"platform"`
	TargetLang string       `json:"target_lang"`
	BotName    string       `json:"bot_name"`
	BotNames   []string     `json:"bot_names"`  // account pool names
	RoomID     int64        `json:"room_id"`
	Paused     bool         `json:"paused"`
	ShowSeq    bool         `json:"show_seq"`
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

	sendDelay  time.Duration // delay before sending (default 3s)
	onChange   func()        // called when pending/recent changes
	rrIndex    map[string]int // output name → round-robin index for account pool
	ch         chan Translation
	done       chan struct{}
	wg         sync.WaitGroup
}

// OnChange registers a callback fired when output state changes (pending/sent).
func (c *Controller) OnChange(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onChange = fn
}

func (c *Controller) notifyChange() {
	c.mu.RLock()
	fn := c.onChange
	c.mu.RUnlock()
	if fn != nil {
		go fn()
	}
}

// New creates a Controller with the given bot pool and output configuration.
// streamerRoomID is the room being monitored; used as fallback when output room_id=0.
func New(pool *bot.Pool, outputs []config.OutputConfig, tlog *transcript.Logger, streamerRoomID int64) *Controller {
	states := make(map[string]*OutputState)
	paused := make(map[string]bool)
	for _, o := range outputs {
		accts := o.AccountPool()
		botName := o.Account
		if len(accts) > 0 {
			botName = accts[0]
		}
		states[o.Name] = &OutputState{
			Name:       o.Name,
			Platform:   o.Platform,
			TargetLang: o.TargetLang,
			BotName:    botName,
			BotNames:   accts,
			RoomID:     o.RoomID,
			ShowSeq:    o.ShowSeq,
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
		rrIndex:        make(map[string]int),
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

// UpdateOutput syncs an output's config to the running controller.
func (c *Controller) UpdateOutput(cfg config.OutputConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.outputs {
		if c.outputs[i].Name == cfg.Name {
			c.outputs[i] = cfg
			break
		}
	}
	if s, ok := c.outputStates[cfg.Name]; ok {
		accts := cfg.AccountPool()
		s.Platform = cfg.Platform
		s.TargetLang = cfg.TargetLang
		s.BotName = cfg.Account
		s.BotNames = accts
		s.RoomID = cfg.RoomID
		s.ShowSeq = cfg.ShowSeq
	}
}

// SyncOutputs replaces the full output list, preserving pause state for existing outputs.
func (c *Controller) SyncOutputs(outputs []config.OutputConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outputs = outputs

	// Build new states, preserve existing pause/pending/recent
	newStates := make(map[string]*OutputState)
	newPaused := make(map[string]bool)
	for _, o := range outputs {
		accts := o.AccountPool()
		if existing, ok := c.outputStates[o.Name]; ok {
			// Update fields from config
			existing.Platform = o.Platform
			existing.TargetLang = o.TargetLang
			existing.BotName = o.Account
			existing.BotNames = accts
			existing.RoomID = o.RoomID
			existing.ShowSeq = o.ShowSeq
			newStates[o.Name] = existing
			newPaused[o.Name] = c.paused[o.Name]
		} else {
			// New output — default paused
			newStates[o.Name] = &OutputState{
				Name:       o.Name,
				Platform:   o.Platform,
				TargetLang: o.TargetLang,
				BotName:    o.Account,
				BotNames:   accts,
				RoomID:     o.RoomID,
				ShowSeq:    o.ShowSeq,
			}
			newPaused[o.Name] = true
		}
	}
	c.outputStates = newStates
	c.paused = newPaused
}

// SetShowSeq updates the show_seq flag for an output.
func (c *Controller) SetShowSeq(outputName string, showSeq bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.outputs {
		if c.outputs[i].Name == outputName {
			c.outputs[i].ShowSeq = showSeq
			break
		}
	}
	if s, ok := c.outputStates[outputName]; ok {
		s.ShowSeq = showSeq
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
			now := time.Now().UnixMilli()
			cp.Pending = make([]PendingMsg, len(s.Pending))
			for i, p := range s.Pending {
				cp.Pending[i] = p
				remain := int((p.SendAt - now + 999) / 1000) // ceil
				if remain < 0 {
					remain = 0
				}
				cp.Pending[i].RemainSec = remain
			}
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
					c.notifyChange()
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
			c.notifyChange()
			continue
		}
		if isPaused {
			slog.Info("paused at send time, dropping", "output", dm.output, "text", dm.text)
			c.notifyChange()
			continue
		}

		c.sendMessage(ctx, dm)
		c.notifyChange()
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

	// Pick bot via round-robin from account pool
	accts := o.AccountPool()
	if len(accts) == 0 {
		slog.Warn("no accounts for output", "output", dm.output)
		return
	}

	targetRoom := o.RoomID
	if targetRoom == 0 {
		targetRoom = c.streamerRoomID
	}

	prefix := o.Prefix
	if o.ShowSeq {
		prefix += seqEmojis[dm.seqNum%len(seqEmojis)]
	}

	// Use minimum maxLen across all pool bots so chunks fit any bot
	minMax := 0
	for _, name := range accts {
		if pb := c.pool.Get(name); pb != nil {
			if ml := pb.MaxMessageLen(); ml > 0 && (minMax <= 0 || ml < minMax) {
				minMax = ml
			}
		}
	}

	chunks := splitWithWrap(dm.text, prefix, o.Suffix, minMax)
	for _, chunk := range chunks {
		// Round-robin: pick next bot for each chunk
		c.mu.Lock()
		idx := c.rrIndex[dm.output] % len(accts)
		c.rrIndex[dm.output] = (idx + 1) % len(accts)
		c.mu.Unlock()

		b := c.pool.Get(accts[idx])
		if b == nil {
			slog.Warn("bot not found", "output", dm.output, "bot", accts[idx])
			continue
		}
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
