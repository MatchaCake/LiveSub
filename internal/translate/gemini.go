package translate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/genai"
)

// GeminiTranslator translates text using Gemini API.
// Falls back to fallbackModel on 429/503, auto-recovers.
type GeminiTranslator struct {
	client        *genai.Client
	model         string
	fallbackModel string
	degraded      atomic.Bool
	recoverAt     atomic.Int64 // unix millis
}

func NewGeminiTranslator(ctx context.Context, apiKey, model string, opts ...TranslatorOption) (*GeminiTranslator, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}

	t := &GeminiTranslator{
		client:        client,
		model:         model,
		fallbackModel: "gemini-2.0-flash",
	}
	for _, o := range opts {
		o(t)
	}
	return t, nil
}

// TranslatorOption configures a GeminiTranslator.
type TranslatorOption func(*GeminiTranslator)

// WithFallbackModel sets the fallback model for rate limit situations.
func WithFallbackModel(model string) TranslatorOption {
	return func(t *GeminiTranslator) {
		t.fallbackModel = model
	}
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

	model := t.activeModel()
	resp, err := t.client.Models.GenerateContent(ctx, model, genai.Text(prompt), nil)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "429") || strings.Contains(errStr, "503") || strings.Contains(errStr, "RESOURCE_EXHAUSTED") || strings.Contains(errStr, "UNAVAILABLE") {
			// Degrade to fallback for 30s
			if !t.degraded.Load() {
				slog.Warn("rate limited, falling back", "from", model, "to", t.fallbackModel, "duration", "30s")
			}
			t.degraded.Store(true)
			t.recoverAt.Store(time.Now().Add(30 * time.Second).UnixMilli())

			// Retry with fallback model
			resp, err = t.client.Models.GenerateContent(ctx, t.fallbackModel, genai.Text(prompt), nil)
			if err != nil {
				return "", fmt.Errorf("gemini translate (fallback): %w", err)
			}
		} else {
			return "", fmt.Errorf("gemini translate: %w", err)
		}
	}

	result := resp.Text()
	result = strings.TrimSpace(result)

	// Detect untranslated output: if result looks like source language, retry with fallback
	if model != t.fallbackModel && looksLikeSource(result, sourceLang, targetLang) {
		slog.Warn("translation returned source language, retrying with fallback",
			"model", model, "source", text, "result", result)
		resp2, err2 := t.client.Models.GenerateContent(ctx, t.fallbackModel, genai.Text(prompt), nil)
		if err2 == nil {
			fallbackResult := strings.TrimSpace(resp2.Text())
			if !looksLikeSource(fallbackResult, sourceLang, targetLang) {
				return fallbackResult, nil
			}
		}
		// If fallback also fails, return empty to skip this message
		return "", nil
	}

	slog.Debug("translated", "from", text, "to", result, "target", targetLang, "model", model)
	return result, nil
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

	// Target is CJK (zh/ja/ko) but result is mostly Latin = wrong language (English)
	if (tgtShort == "zh" || tgtShort == "ja" || tgtShort == "ko") && latinRatio > 0.5 {
		return true
	}

	// Target is Latin-based (en/fr/de/es) but result is mostly CJK = wrong language
	if (tgtShort == "en" || tgtShort == "fr" || tgtShort == "de" || tgtShort == "es") &&
		float64(cjkCount)/float64(total) > 0.3 {
		return true
	}

	return false
}

// activeModel returns the current model, auto-recovering from degraded state.
func (t *GeminiTranslator) activeModel() string {
	if t.degraded.Load() {
		if time.Now().UnixMilli() >= t.recoverAt.Load() {
			t.degraded.Store(false)
			slog.Info("recovered from rate limit, back to primary model", "model", t.model)
			return t.model
		}
		return t.fallbackModel
	}
	return t.model
}

func (t *GeminiTranslator) Close() {
	// genai client doesn't need explicit close
}
