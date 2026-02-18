package controller

import (
	"context"
	"log/slog"
	"sync"

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

// OutputState tracks per-output status for the web UI.
type OutputState struct {
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	TargetLang string `json:"target_lang"`
	BotName    string `json:"bot_name"`
	RoomID     int64  `json:"room_id"`
	Paused     bool   `json:"paused"`
	LastText   string `json:"last_text"`
}

// Controller receives translations from the Agent and routes them to bots.
type Controller struct {
	pool           *bot.Pool
	outputs        []config.OutputConfig
	tlog           *transcript.Logger
	streamerRoomID int64

	mu           sync.RWMutex
	paused       map[string]bool // output name → paused
	outputStates map[string]*OutputState

	ch   chan Translation
	done chan struct{}
	wg   sync.WaitGroup
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
		ch:           make(chan Translation, 100),
		done:         make(chan struct{}),
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

// IsAnyPaused returns true if any output is paused (used to gate STT).
func (c *Controller) IsAnyPaused() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// All paused = pause STT
	for _, p := range c.paused {
		if !p {
			return false
		}
	}
	return len(c.paused) > 0
}

// OutputStates returns the current state of all outputs in config order.
func (c *Controller) OutputStates() []OutputState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]OutputState, 0, len(c.outputs))
	for _, o := range c.outputs {
		if s, ok := c.outputStates[o.Name]; ok {
			out = append(out, *s)
		}
	}
	return out
}

func (c *Controller) run(ctx context.Context) {
	defer c.wg.Done()

	// Per-output ordered sender: buffer out-of-order results, send in sequence
	type outputSender struct {
		nextSeq int
		pending map[int]string // seq → text to send
	}
	senders := make(map[string]*outputSender)
	for _, o := range c.outputs {
		senders[o.Name] = &outputSender{pending: make(map[int]string)}
	}

	for t := range c.ch {
		for _, o := range c.outputs {
			// Determine text for this output
			var text string
			if o.TargetLang == "" {
				// No translation — send source text
				text = t.SourceText
			} else {
				text = t.Texts[o.TargetLang]
				if text == "" {
					// Check if source lang matches target (direct pass-through)
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

			// Flush in order
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

				// Update output state
				c.mu.Lock()
				if st, ok := c.outputStates[o.Name]; ok {
					st.LastText = txt
				}
				isPaused := c.paused[o.Name]
				c.mu.Unlock()

				if isPaused {
					slog.Info("paused, dropping", "output", o.Name, "text", txt)
					continue
				}

				// Send via bot to output's room (0 = streamer's room)
				b := c.pool.Get(o.Account)
				if b == nil {
					slog.Warn("bot not found for output", "output", o.Name, "bot", o.Account)
					continue
				}

				targetRoom := o.RoomID
				if targetRoom == 0 {
					targetRoom = c.streamerRoomID
				}

				// Split text into chunks, each wrapped with prefix/suffix
				chunks := splitWithWrap(txt, o.Prefix, o.Suffix, b.MaxMessageLen())
				for _, chunk := range chunks {
					slog.Info("sending", "output", o.Name, "bot", b.Name(), "room", targetRoom, "text", chunk)
					if err := b.Send(ctx, targetRoom, chunk); err != nil {
						slog.Error("send failed", "output", o.Name, "bot", b.Name(), "err", err)
						break
					}
				}
			}
		}
	}
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
		// Try to break at a space (for languages with word boundaries)
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

// isLangMatch checks if detected language matches a target language code.
func isLangMatch(detected, target string) bool {
	if detected == "" || target == "" {
		return false
	}
	// Simple prefix match: "ja" matches "ja-JP", "zh" matches "zh-CN"
	if len(detected) >= 2 && len(target) >= 2 {
		if detected[:2] == target[:2] {
			return true
		}
	}
	// Handle cmn → zh mapping
	if len(detected) >= 3 && detected[:3] == "cmn" && len(target) >= 2 && target[:2] == "zh" {
		return true
	}
	return false
}

// TranslateAndSubmit handles the translation fan-out for a single STT result.
// It translates to all needed languages and submits to the controller.
func TranslateAndSubmit(ctx context.Context, ctrl *Controller, translator *translate.GeminiTranslator, seq int, sourceText, sourceLang string, outputs []config.OutputConfig) {
	// Collect unique target languages that need translation
	needed := make(map[string]bool)
	for _, o := range outputs {
		if o.TargetLang != "" && !isLangMatch(sourceLang, o.TargetLang) {
			needed[o.TargetLang] = true
		}
	}

	texts := make(map[string]string)

	if len(needed) == 0 {
		// No translation needed — all outputs use source text
		ctrl.Submit(Translation{
			Seq:        seq,
			SourceText: sourceText,
			SourceLang: sourceLang,
			Texts:      texts,
		})
		return
	}

	// Translate to each needed language (can be parallelized later)
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
