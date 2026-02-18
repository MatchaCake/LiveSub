package translate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/genai"
)

// GeminiTranslator translates text using Gemini API.
type GeminiTranslator struct {
	client *genai.Client
	model  string
}

func NewGeminiTranslator(ctx context.Context, apiKey, model string) (*GeminiTranslator, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}

	return &GeminiTranslator{
		client: client,
		model:  model,
	}, nil
}

// Translate translates text from sourceLang to targetLang.
func (t *GeminiTranslator) Translate(ctx context.Context, text, sourceLang, targetLang string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}

	prompt := fmt.Sprintf(
		"Translate the following %s text to %s. "+
			"Output ONLY the translation, nothing else. "+
			"Keep it natural and concise (suitable for live stream subtitles).\n\n%s",
		sourceLang, targetLang, text,
	)

	resp, err := t.client.Models.GenerateContent(ctx, t.model, genai.Text(prompt), nil)
	if err != nil {
		return "", fmt.Errorf("gemini translate: %w", err)
	}

	result := resp.Text()
	result = strings.TrimSpace(result)

	slog.Debug("translated", "from", text, "to", result, "target", targetLang)
	return result, nil
}

func (t *GeminiTranslator) Close() {
	// genai client doesn't need explicit close
}
