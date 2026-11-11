package stt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode"

	speech "cloud.google.com/go/speech/apiv1"
	speechpb "cloud.google.com/go/speech/apiv1/speechpb"
)

// GoogleSTT performs streaming speech-to-text using Google Cloud Speech API.
type GoogleSTT struct {
	client   *speech.Client
	language string   // primary language
	altLangs []string // additional languages for auto-detection
	// lockAfter: empty alternative-language finals that end a stream as
	// language-locked (see ErrLanguageLock). 0 disables the watchdog.
	lockAfter int
}

// ErrLanguageLock is returned by Stream when the watchdog ends a stream that
// Google locked onto an alternative language and that is producing nothing.
//
// With alternative_language_codes Google picks ONE language per streaming
// session, from the first audio it hears. In a week of logs, 31 of 273 streams
// came up as cmn-hans-cn for Japanese-speaking streamers (music or singing at
// the moment the stream started) and not one of them ever produced a ja-jp
// result afterwards: the stream stays wrong until its ~305s limit, emitting only
// empty finals (conf 0, every 30–60s) and the odd line of confident nonsense.
// Viewers see subtitles freeze for five minutes (2026-11-11 22:07 and 22:22).
// Ending the stream makes Google detect the language again on fresh audio.
var ErrLanguageLock = errors.New("stt: stream locked onto an alternative language")

// maxLockRebuilds bounds consecutive watchdog rebuilds. After that many
// streams in a row ended by the watchdog, the next one is left alone until its
// normal rotation: if every stream locks (long stretch of music with nobody
// talking, or a streamer who really is speaking the alternative language) we
// stop reconnecting and behave exactly as before.
const maxLockRebuilds = 3

// LockGuard paces the language-lock watchdog across the successive streams of
// one pipeline. Instead of sleeping between rebuilds (unread audio would back
// up in the ffmpeg pipe) it asks for more evidence each time: the first stream
// is ended by its 1st empty alternative-language final, the next by its 2nd,
// then its 3rd, and after maxLockRebuilds the watchdog sits one stream out.
// Any stream that ends for another reason resets the count.
type LockGuard struct {
	streak int // consecutive streams ended by the watchdog
}

// Threshold is the number of empty alternative-language finals that should end
// the next stream; 0 means the watchdog is off for that stream.
func (g *LockGuard) Threshold() int {
	if g.streak >= maxLockRebuilds {
		return 0
	}
	return g.streak + 1
}

// Observe records how a stream ended (the error returned by Stream).
func (g *LockGuard) Observe(err error) {
	if errors.Is(err, ErrLanguageLock) {
		g.streak++
		return
	}
	g.streak = 0
}

// Streak reports how many streams in a row the watchdog has ended.
func (g *LockGuard) Streak() int { return g.streak }

// SetLockThreshold arms the language-lock watchdog for the next Stream call
// (see LockGuard.Threshold). It has no effect without alternative languages.
func (s *GoogleSTT) SetLockThreshold(n int) { s.lockAfter = n }

// lockWatch is the per-stream watchdog configuration.
type lockWatch struct {
	primary string // base of the configured primary language, e.g. "ja"
	after   int    // empty alternative-language finals that end the stream; 0 = off
}

func langBase(code string) string {
	return strings.SplitN(strings.ToLower(strings.TrimSpace(code)), "-", 2)[0]
}

// unproductive reports whether a final result carries nothing to translate.
func unproductive(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
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
	// Per-stream context: when Stream returns while the RPC is still open (the
	// watchdog ending a locked stream), cancelling it stops the audio feeder
	// below. Otherwise it would keep stealing audio from the next stream.
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := s.client.StreamingRecognize(sctx)
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
					if err == io.EOF || sctx.Err() != nil {
						slog.Debug("audio send stopped: stream closed")
					} else {
						slog.Error("send audio error", "err", err)
					}
					return
				}
			}
		}
	}()

	var w lockWatch
	if len(s.altLangs) > 0 {
		w = lockWatch{primary: langBase(s.language), after: s.lockAfter}
	}
	return receive(ctx, stream.Recv, results, strings.ToLower(s.language), w)
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
//
// w arms the language-lock watchdog: once w.after finals that are in an
// alternative language AND empty have arrived, and the stream has not produced
// a single primary-language final, receive returns ErrLanguageLock. A
// primary-language final disarms it for the rest of the stream (the language
// never changes within one stream).
func receive(ctx context.Context, recv func() (*speechpb.StreamingRecognizeResponse, error), results chan<- StreamResult, defaultLang string, w lockWatch) error {
	var interim StreamResult // utterance in progress, not yet finalized by the server
	var (
		started    = time.Now()
		sawPrimary bool   // a primary-language final arrived: this stream is fine
		altEmpty   int    // empty finals in an alternative language
		streamLang string // language of the latest final
	)
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
					interim.Language = streamLang
				}
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
					if sr.Language != "" {
						streamLang = sr.Language
					}
					switch {
					case sr.Language == "" || sawPrimary || w.after <= 0:
						// unknown language, healthy stream, or watchdog off
					case langBase(sr.Language) == w.primary:
						sawPrimary = true
					case unproductive(sr.Text):
						altEmpty++
						if altEmpty >= w.after {
							slog.Warn("STT stream locked onto alternative language and producing nothing, rebuilding it",
								"lang", sr.Language, "primary", w.primary, "empty_finals", altEmpty,
								"stream_age", time.Since(started).Round(time.Second))
							return ErrLanguageLock
						}
					}
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
