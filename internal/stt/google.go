package stt

import (
	"context"
	"fmt"
	"io"
	"log/slog"

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
	Text     string
	IsFinal  bool
	Language string // detected language code (e.g. "ja-jp", "en-us", "zh-cn")
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
					slog.Error("send audio error", "err", err)
					return
				}
			}
		}
	}()

	// Receive results
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("recv: %w", err)
		}

		for _, result := range resp.Results {
			if len(result.Alternatives) > 0 {
				alt := result.Alternatives[0]
				sr := StreamResult{
					Text:     alt.Transcript,
					IsFinal:  result.IsFinal,
					Language: result.GetLanguageCode(),
				}

				if sr.IsFinal {
					slog.Info("STT final", "text", sr.Text, "lang", sr.Language, "confidence", alt.Confidence)
				}

				results <- sr
			}
		}
	}
}

// Close closes the STT client.
func (s *GoogleSTT) Close() error {
	return s.client.Close()
}
