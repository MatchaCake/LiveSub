package controller

import (
	"context"
	"log/slog"
	"strings"
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
	AutoStart  bool         `json:"auto_start"`
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
	stopOnce   sync.Once
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

// newOutputState creates an OutputState from config.
func newOutputState(o config.OutputConfig) *OutputState {
	accts := o.AccountPool()
	botName := o.Account
	if len(accts) > 0 {
		botName = accts[0]
	}
	return &OutputState{
		Name:       o.Name,
		Platform:   o.Platform,
		TargetLang: o.TargetLang,
		BotName:    botName,
		BotNames:   accts,
		RoomID:     o.RoomID,
		ShowSeq:    o.ShowSeq,
		AutoStart:  o.AutoStart,
	}
}

// syncOutputState updates an existing OutputState from config.
func syncOutputState(s *OutputState, o config.OutputConfig) {
	s.Platform = o.Platform
	s.TargetLang = o.TargetLang
	s.BotName = o.Account
	s.BotNames = o.AccountPool()
	s.RoomID = o.RoomID
	s.ShowSeq = o.ShowSeq
}

// New creates a Controller with the given bot pool and output configuration.
// streamerRoomID is the room being monitored; used as fallback when output room_id=0.
func New(pool *bot.Pool, outputs []config.OutputConfig, tlog *transcript.Logger, streamerRoomID int64) *Controller {
	states := make(map[string]*OutputState)
	paused := make(map[string]bool)
	for _, o := range outputs {
		states[o.Name] = newOutputState(o)
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
// Safe to call concurrently with Stop: drops the message once shut down rather
// than panicking on a closed channel (in-flight translations race with Stop on
// every stream teardown).
func (c *Controller) Submit(t Translation) {
	select {
	case c.ch <- t:
	case <-c.done:
	}
}

// Stop gracefully shuts down the controller. Idempotent.
func (c *Controller) Stop() {
	c.stopOnce.Do(func() { close(c.done) })
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
	// Copy-on-write: run() snapshots c.outputs under RLock and then reads the
	// elements WITHOUT the lock; mutating elements in place would race with
	// those reads (torn string headers). Replacing the slice is safe.
	newOutputs := make([]config.OutputConfig, len(c.outputs))
	copy(newOutputs, c.outputs)
	for i := range newOutputs {
		if newOutputs[i].Name == cfg.Name {
			newOutputs[i] = cfg
			break
		}
	}
	c.outputs = newOutputs
	if s, ok := c.outputStates[cfg.Name]; ok {
		syncOutputState(s, cfg)
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
		if existing, ok := c.outputStates[o.Name]; ok {
			syncOutputState(existing, o)
			newStates[o.Name] = existing
			newPaused[o.Name] = c.paused[o.Name]
		} else {
			newStates[o.Name] = newOutputState(o)
			newPaused[o.Name] = true
		}
	}
	c.outputStates = newStates
	c.paused = newPaused
}

// SetAutoStart updates the auto_start flag for an output.
func (c *Controller) SetAutoStart(outputName string, autoStart bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.outputStates[outputName]; ok {
		s.AutoStart = autoStart
	}
}

// SetShowSeq updates the show_seq flag for an output.
func (c *Controller) SetShowSeq(outputName string, showSeq bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Copy-on-write for the same reason as UpdateOutput.
	newOutputs := make([]config.OutputConfig, len(c.outputs))
	copy(newOutputs, c.outputs)
	for i := range newOutputs {
		if newOutputs[i].Name == outputName {
			newOutputs[i].ShowSeq = showSeq
			break
		}
	}
	c.outputs = newOutputs
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

// IsAllPaused returns true if ALL outputs are paused (gates STT audio gating).
func (c *Controller) IsAllPaused() bool {
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
	// Only mark IDs that are actually still pending: a skip racing with the
	// send (message already delivered) would otherwise leave a skipSet entry
	// nobody ever consumes — a slow leak over a long stream.
	found := false
	for _, st := range c.outputStates {
		for i, p := range st.Pending {
			if p.ID == msgID {
				st.Pending = append(st.Pending[:i], st.Pending[i+1:]...)
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if found {
		c.skipSet[msgID] = true
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
		nextSeq      int
		seqCounter   int
		pending      map[int]string // seq → text to send
		waitingSince time.Time      // when we started waiting for nextSeq (zero = not waiting)
	}
	senders := make(map[string]*outputSender)
	c.mu.RLock()
	for _, o := range c.outputs {
		senders[o.Name] = &outputSender{pending: make(map[int]string)}
	}
	c.mu.RUnlock()

	// If nextSeq never arrives (translation goroutine panicked, Submit dropped
	// at shutdown race, ...), skip ahead instead of stalling this output for the
	// rest of the stream with pending growing unbounded.
	const seqGapTimeout = 10 * time.Second

	// Delay queue: messages waiting to be sent
	var delayQueue []delayedMsg

	// flushSender drains s.pending in seq order into the delay queue and
	// maintains the seq-gap wait timer. Shared by the translation path and the
	// gap-recovery path.
	flushSender := func(name string, s *outputSender) {
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
			isPaused := c.paused[name]
			c.mu.Unlock()

			if isPaused {
				slog.Info("paused, dropping", "output", name, "text", txt)
				continue
			}

			// Assign message ID and push to delay queue
			c.mu.Lock()
			msgID := c.nextMsgID
			c.nextMsgID++
			sendAt := time.Now().Add(c.sendDelay)
			// Add to pending in output state for UI
			if st, ok := c.outputStates[name]; ok {
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
				output: name,
				seqNum: s.seqCounter,
			})
			s.seqCounter++
			c.notifyChange()
		}

		// Track whether this output is stuck waiting for a missing seq.
		if len(s.pending) > 0 {
			if s.waitingSince.IsZero() {
				s.waitingSince = time.Now()
			}
		} else {
			s.waitingSince = time.Time{}
		}
	}

	// Ticker to check delay queue
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			// Stop() called — flush remaining and exit.
			c.flushDelayQueue(ctx, delayQueue)
			return
		case t := <-c.ch:
			// Snapshot outputs under lock (SyncOutputs may replace the slice concurrently)
			c.mu.RLock()
			outputs := c.outputs
			c.mu.RUnlock()

			// Log transcript once per STT result
			if c.tlog != nil && t.SourceText != "" {
				logged := false
				for _, o := range outputs {
					if c.IsPaused(o.Name) {
						continue
					}
					targetLang := o.TargetLang
					if targetLang == "" {
						targetLang = t.SourceLang
					}
					var text string
					if o.TargetLang == "" {
						text = t.SourceText
					} else {
						text = t.Texts[o.TargetLang]
						if text == "" && isLangMatch(t.SourceLang, o.TargetLang) {
							text = t.SourceText
						}
					}
					if text != "" {
						c.tlog.Write(t.SourceLang, t.SourceText, targetLang, text)
						logged = true
						break
					}
				}
				// All paused or no translation available: log source text only
				if !logged {
					c.tlog.Write(t.SourceLang, t.SourceText, "", "")
				}
			}

			for _, o := range outputs {
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

				// Buffer for ordered sending (lazily init for outputs added via SyncOutputs)
				s := senders[o.Name]
				if s == nil {
					// Start ordering from the first seq this output sees — upstream
					// seq is already high, so nextSeq=0 would wait forever and grow
					// pending unbounded.
					s = &outputSender{nextSeq: t.Seq, pending: make(map[int]string)}
					senders[o.Name] = s
				}
				s.pending[t.Seq] = text

				// Flush in order → push to delay queue
				flushSender(o.Name, s)
			}

		case <-ticker.C:
			// Seq-gap recovery: if an output has waited too long for a missing
			// seq, jump to the smallest buffered seq so it doesn't stall forever.
			for name, s := range senders {
				if len(s.pending) == 0 || s.waitingSince.IsZero() || time.Since(s.waitingSince) < seqGapTimeout {
					continue
				}
				minSeq := -1
				for seq := range s.pending {
					if minSeq == -1 || seq < minSeq {
						minSeq = seq
					}
				}
				slog.Warn("seq gap timeout, skipping ahead", "output", name, "from", s.nextSeq, "to", minSeq, "buffered", len(s.pending))
				s.nextSeq = minSeq
				s.waitingSince = time.Time{}
				flushSender(name, s) // drain immediately — a quiet stream may not deliver another translation for a while
			}

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
		// Respect pause on the shutdown flush too — otherwise messages the user
		// paused (or is still reviewing in the delay window) blast out the moment
		// the stream ends.
		isPaused := c.paused[dm.output]
		c.mu.Unlock()
		if !skipped && !isPaused {
			c.sendMessage(ctx, dm)
		}
	}
}

func (c *Controller) sendMessage(ctx context.Context, dm delayedMsg) {
	// Find output config (snapshot under lock to avoid race with SyncOutputs)
	c.mu.RLock()
	var oCopy config.OutputConfig
	found := false
	for i := range c.outputs {
		if c.outputs[i].Name == dm.output {
			oCopy = c.outputs[i]
			found = true
			break
		}
	}
	c.mu.RUnlock()
	if !found {
		return
	}
	o := &oCopy

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
		err := b.Send(ctx, targetRoom, chunk)
		// Bilibili rate limit (10031 频率过快) is transient — retry instead of
		// dropping the chunk (and everything after it).
		for retry := 1; err != nil && isRateLimited(err) && retry <= 2; retry++ {
			wait := time.Duration(retry) * 2 * time.Second
			slog.Warn("danmaku rate limited, retrying", "output", dm.output, "bot", b.Name(), "retry", retry, "wait", wait)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return
			}
			err = b.Send(ctx, targetRoom, chunk)
		}
		if err != nil {
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

// isRateLimited reports whether err is Bilibili's danmaku frequency limit (code 10031).
func isRateLimited(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "10031") || strings.Contains(err.Error(), "频率过快"))
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
