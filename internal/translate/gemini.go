package translate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

const (
	// defaultAttemptTimeout bounds a single model call so a hung model falls
	// through to the next one in the chain instead of stalling the subtitle.
	defaultAttemptTimeout = 10 * time.Second
	// defaultCooldown is how long a model that just failed is skipped before
	// being tried again first.
	defaultCooldown = 30 * time.Second
)

// DefaultFallbackModels is used when the config does not list any fallback.
// Rolling alias maintained by Google — pinned versions get retired and 404.
var DefaultFallbackModels = []string{"gemini-flash-latest"}

// generateFunc performs one model call. withThinkingOff asks the model to skip
// reasoning (thinkingBudget=0). Abstracted so the fallback logic is testable.
type generateFunc func(ctx context.Context, model, prompt string, withThinkingOff bool) (string, error)

// GeminiTranslator translates text using the Gemini API with an ordered model
// chain: the primary model first, then each fallback in order. A model that
// fails (error, timeout, rate limit, unavailable/unknown model) is put on a
// short cooldown and the next model is tried for the same line.
type GeminiTranslator struct {
	models         []string // primary first, then fallbacks in order
	generate       generateFunc
	attemptTimeout time.Duration
	cooldown       time.Duration
	now            func() time.Time

	mu         sync.Mutex
	coolUntil  map[string]time.Time
	noThinkOff map[string]bool // models that reject thinkingBudget=0
	lastServed string          // model that produced the last good translation
}

// NewGeminiTranslator creates a translator. model is the primary model;
// fallbacks are tried in order when the primary fails. An empty fallback list
// uses DefaultFallbackModels.
func NewGeminiTranslator(ctx context.Context, apiKey, model string, fallbacks []string, opts ...TranslatorOption) (*GeminiTranslator, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}

	gen := func(ctx context.Context, model, prompt string, thinkingOff bool) (string, error) {
		var cfg *genai.GenerateContentConfig
		if thinkingOff {
			// 3.x flash models are reasoning models; without this a single
			// subtitle line costs 5-20s of thinking. Live subs need ~1-2s.
			zero := int32(0)
			cfg = &genai.GenerateContentConfig{
				ThinkingConfig: &genai.ThinkingConfig{ThinkingBudget: &zero},
			}
		}
		resp, err := client.Models.GenerateContent(ctx, model, genai.Text(prompt), cfg)
		if err != nil {
			return "", err
		}
		return resp.Text(), nil
	}

	return newTranslator(gen, model, fallbacks, opts...), nil
}

func newTranslator(gen generateFunc, model string, fallbacks []string, opts ...TranslatorOption) *GeminiTranslator {
	if len(fallbacks) == 0 {
		fallbacks = DefaultFallbackModels
	}
	t := &GeminiTranslator{
		models:         dedupe(append([]string{model}, fallbacks...)),
		generate:       gen,
		attemptTimeout: defaultAttemptTimeout,
		cooldown:       defaultCooldown,
		now:            time.Now,
		coolUntil:      make(map[string]time.Time),
		noThinkOff:     make(map[string]bool),
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// TranslatorOption configures a GeminiTranslator.
type TranslatorOption func(*GeminiTranslator)

// WithAttemptTimeout overrides the per-model call timeout.
func WithAttemptTimeout(d time.Duration) TranslatorOption {
	return func(t *GeminiTranslator) { t.attemptTimeout = d }
}

// WithCooldown overrides how long a failed model is skipped.
func WithCooldown(d time.Duration) TranslatorOption {
	return func(t *GeminiTranslator) { t.cooldown = d }
}

// Models returns the model chain in priority order.
func (t *GeminiTranslator) Models() []string {
	return append([]string(nil), t.models...)
}

// Translate translates text from sourceLang to targetLang.
func (t *GeminiTranslator) Translate(ctx context.Context, text, sourceLang, targetLang string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}

	prompt := fmt.Sprintf(
		"Translate the following %s text to %s. "+
			"Output ONLY the translation, nothing else. "+
			"Keep it natural and concise (suitable for live stream subtitles). "+
			"For proper nouns and person names, output their romaji/romanization instead of translating them.\n\n%s",
		sourceLang, targetLang, text,
	)

	var (
		firstResult string // first untranslated-looking result, kept as last resort
		haveResult  bool
		errs        []error
	)
	for _, model := range t.attemptOrder() {
		if ctx.Err() != nil {
			break
		}
		result, err := t.call(ctx, model, prompt)
		if err != nil {
			if ctx.Err() != nil {
				// Caller cancelled — not the model's fault.
				errs = append(errs, fmt.Errorf("%s: %w", model, err))
				break
			}
			t.markFailed(model, err)
			errs = append(errs, fmt.Errorf("%s: %w", model, err))
			continue
		}
		result = strings.TrimSpace(result)

		// Untranslated output (still in source language): try the next model.
		if looksLikeSource(result, sourceLang, targetLang) {
			slog.Warn("translation returned source language, trying next model",
				"model", model, "source", text, "result", result)
			if !haveResult {
				firstResult, haveResult = result, true
			}
			continue
		}

		t.markOK(model)
		slog.Debug("translated", "from", text, "to", result, "target", targetLang, "model", model)
		return result, nil
	}

	if haveResult {
		// Every model either failed or still looked untranslated — a
		// possibly-wrong-language subtitle is better than dropping the line.
		return firstResult, nil
	}
	if len(errs) == 0 && ctx.Err() != nil {
		errs = append(errs, ctx.Err())
	}
	return "", fmt.Errorf("gemini translate: all models failed: %w", errors.Join(errs...))
}

// call runs one model with a per-attempt timeout and thinking disabled. A
// model that rejects thinkingBudget=0 is remembered and retried without it.
func (t *GeminiTranslator) call(ctx context.Context, model, prompt string) (string, error) {
	t.mu.Lock()
	thinkOff := !t.noThinkOff[model]
	t.mu.Unlock()

	attempt := func(off bool) (string, error) {
		actx, cancel := context.WithTimeout(ctx, t.attemptTimeout)
		defer cancel()
		return t.generate(actx, model, prompt, off)
	}

	res, err := attempt(thinkOff)
	if err != nil && thinkOff && isThinkingConfigRejected(err) {
		slog.Warn("model rejects thinkingBudget=0, calling without it", "model", model)
		t.mu.Lock()
		t.noThinkOff[model] = true
		t.mu.Unlock()
		res, err = attempt(false)
	}
	return res, err
}

func isThinkingConfigRejected(err error) bool {
	s := strings.ToLower(err.Error())
	return (strings.Contains(s, "400") || strings.Contains(s, "invalid_argument")) &&
		strings.Contains(s, "thinking")
}

// attemptOrder returns the chain with models on cooldown moved to the back
// (preserving relative order), so a healthy model is always tried first but a
// cooling model is still tried if everything else fails.
func (t *GeminiTranslator) attemptOrder() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	ready := make([]string, 0, len(t.models))
	cooling := make([]string, 0)
	for _, m := range t.models {
		if until, ok := t.coolUntil[m]; ok && now.Before(until) {
			cooling = append(cooling, m)
			continue
		}
		ready = append(ready, m)
	}
	return append(ready, cooling...)
}

func (t *GeminiTranslator) markFailed(model string, err error) {
	t.mu.Lock()
	_, already := t.coolUntil[model]
	t.coolUntil[model] = t.now().Add(t.cooldown)
	t.mu.Unlock()
	if !already {
		slog.Warn("translate model failed, falling back", "model", model,
			"cooldown", t.cooldown, "err", err)
	}
}

func (t *GeminiTranslator) markOK(model string) {
	t.mu.Lock()
	_, was := t.coolUntil[model]
	delete(t.coolUntil, model)
	switched := t.lastServed != model
	t.lastServed = model
	t.mu.Unlock()
	if was {
		slog.Info("translate model recovered", "model", model)
	}
	if switched {
		slog.Info("translate model active", "model", model, "chain", t.models)
	}
}

// looksLikeSource checks if the translation result is still in the source language.
// Uses simple heuristic: for ja→zh, check if result contains mostly Japanese-specific chars.
func looksLikeSource(text, sourceLang, targetLang string) bool {
	if text == "" {
		return false
	}
	srcShort := strings.SplitN(strings.ToLower(sourceLang), "-", 2)[0]
	tgtShort := strings.SplitN(strings.ToLower(targetLang), "-", 2)[0]

	if srcShort == tgtShort {
		return false // same language, can't detect
	}

	// Count character types
	jaCount := 0    // hiragana + katakana
	latinCount := 0 // ASCII letters
	cjkCount := 0   // CJK unified ideographs (shared by zh/ja)
	total := 0
	for _, r := range text {
		if r < 0x20 || r == ' ' {
			continue
		}
		total++
		if (r >= 0x3040 && r <= 0x309F) || (r >= 0x30A0 && r <= 0x30FF) {
			jaCount++
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			latinCount++
		}
		if r >= 0x4E00 && r <= 0x9FFF {
			cjkCount++
		}
	}

	if total == 0 {
		return false
	}

	jaRatio := float64(jaCount) / float64(total)
	latinRatio := float64(latinCount) / float64(total)

	// Source is Japanese but target is Chinese: check for leftover kana
	if srcShort == "ja" && tgtShort == "zh" && jaRatio > 0.3 {
		return true
	}

	// Target is CJK (zh/ja/ko) but result is mostly Latin = wrong language (English).
	// Names are kept as romaji per the prompt, so a name-heavy but otherwise
	// translated result can legitimately exceed 50% Latin — only flag when
	// there are no CJK chars at all.
	if (tgtShort == "zh" || tgtShort == "ja" || tgtShort == "ko") && latinRatio > 0.5 && cjkCount == 0 {
		return true
	}

	// Target is Latin-based (en/fr/de/es) but result is mostly CJK = wrong language
	if (tgtShort == "en" || tgtShort == "fr" || tgtShort == "de" || tgtShort == "es") &&
		float64(cjkCount)/float64(total) > 0.3 {
		return true
	}

	return false
}
