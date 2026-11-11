package stt

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	speech "cloud.google.com/go/speech/apiv1"
	speechpb "cloud.google.com/go/speech/apiv1/speechpb"
)

// GoogleSTT performs streaming speech-to-text using Google Cloud Speech API.
type GoogleSTT struct {
	client   *speech.Client
	language string   // primary language
	altLangs []string // additional languages for auto-detection
}

func NewGoogleSTT(ctx context.Context, language string, altLangs []string) (*GoogleSTT, error) {
	client, err := speech.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create speech client: %w", err)
	}

	return &GoogleSTT{
		client:   client,
		language: language,
		altLangs: altLangs,
	}, nil
}

// StreamResult represents a transcription result.
type StreamResult struct {
	Text       string
	IsFinal    bool
	Language   string  // detected language code (e.g. "ja-jp", "en-us", "zh-cn")
	Confidence float32 // 0.0-1.0, from STT engine
}

// Stream starts a streaming recognition session.
// Reads PCM s16le 16kHz mono from audioReader.
// Sends final transcription results to the results channel.
func (s *GoogleSTT) Stream(ctx context.Context, audioReader io.Reader, results chan<- StreamResult) error {
	stream, err := s.client.StreamingRecognize(ctx)
	if err != nil {
		return fmt.Errorf("start streaming: %w", err)
	}

	// Send config first
	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:                   speechpb.RecognitionConfig_LINEAR16,
					SampleRateHertz:            16000,
					LanguageCode:               s.language,
					AlternativeLanguageCodes:   s.altLangs,
					EnableAutomaticPunctuation: true,
				},
				InterimResults: true,
			},
		},
	}); err != nil {
		return fmt.Errorf("send config: %w", err)
	}

	// Goroutine: feed audio data
	go func() {
		buf := make([]byte, 3200) // 100ms of 16kHz 16-bit mono
		for {
			n, err := audioReader.Read(buf)
			if err != nil {
				if err != io.EOF {
					slog.Error("audio read error", "err", err)
				}
				_ = stream.CloseSend()
				return
			}
			if n > 0 {
				if err := stream.Send(&speechpb.StreamingRecognizeRequest{
					StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
						AudioContent: buf[:n],
					},
				}); err != nil {
					// io.EOF from Send means the server already ended the
					// stream — which it does every ~305s (max stream duration).
					// The real status comes from Recv below; logging this at
					// ERROR made every routine rotation look like an outage.
					if err == io.EOF {
						slog.Debug("audio send stopped: stream closed by server")
					} else {
						slog.Error("send audio error", "err", err)
					}
					return
				}
			}
		}
	}()

	return receive(ctx, stream.Recv, results, strings.ToLower(s.language))
}

// receive forwards recognition results until the stream ends. io.EOF (audio
// source finished) returns nil; any other error is returned wrapped.
//
// When the stream is cut by an error — in practice Google's ~305s max stream
// duration, 12 times an hour — whatever was being said at that moment only
// exists as an interim hypothesis, and used to be thrown away: the next stream
// starts with the second half of the sentence (2026-11-11 21:06: 「…の翻訳を
// 翻訳しないことでした」 with its subject gone). The audio is not replayed to
// the new stream, so that hypothesis is the only record of those words; it is
// emitted as a final result instead of being dropped.
func receive(ctx context.Context, recv func() (*speechpb.StreamingRecognizeResponse, error), results chan<- StreamResult, defaultLang string) error {
	var interim StreamResult // utterance in progress, not yet finalized by the server
	for {
		resp, err := recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			// Not on shutdown / stream-offline: nobody is left to translate it.
			if ctx.Err() == nil && strings.TrimSpace(interim.Text) != "" {
				interim.IsFinal = true
				if interim.Language == "" {
					interim.Language = defaultLang
				}
				slog.Info("STT stream cut mid-utterance, keeping interim text", "text", interim.Text, "lang", interim.Language)
				results <- interim
			}
			return fmt.Errorf("recv: %w", err)
		}
		if len(resp.Results) == 0 {
			continue // speech events etc.: nothing about the transcript changed
		}

		// A response holds at most one final result followed by the interim
		// results for the audio after it; together those interims are the
		// whole current hypothesis (stable part + unstable tail).
		var pending strings.Builder
		pendingLang := ""
		for _, result := range resp.Results {
			if len(result.Alternatives) > 0 {
				alt := result.Alternatives[0]
				sr := StreamResult{
					Text:       alt.Transcript,
					IsFinal:    result.IsFinal,
					Language:   result.GetLanguageCode(),
					Confidence: alt.Confidence,
				}

				if sr.IsFinal {
					slog.Info("STT final", "text", sr.Text, "lang", sr.Language, "confidence", alt.Confidence)
					pending.Reset()
				} else {
					pending.WriteString(alt.Transcript)
					if pendingLang == "" {
						pendingLang = sr.Language
					}
				}

				results <- sr
			}
		}
		interim = StreamResult{Text: pending.String(), Language: pendingLang}
	}
}

// Close closes the STT client.
func (s *GoogleSTT) Close() error {
	return s.client.Close()
}
