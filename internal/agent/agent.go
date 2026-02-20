package agent

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	stream "github.com/MatchaCake/bilibili_stream_lib"
	"github.com/christian-lee/livesub/internal/config"
	"github.com/christian-lee/livesub/internal/controller"
	"github.com/christian-lee/livesub/internal/stt"
	"github.com/christian-lee/livesub/internal/translate"
)

// Agent captures audio from a stream, runs STT, and fans out
// translations to the Controller.
type Agent struct {
	streamer   config.StreamerConfig
	translator *translate.GeminiTranslator
	ctrl       *controller.Controller
}

// New creates a new Agent for a specific streamer.
func New(streamer config.StreamerConfig, translator *translate.GeminiTranslator, ctrl *controller.Controller) *Agent {
	return &Agent{
		streamer:   streamer,
		translator: translator,
		ctrl:       ctrl,
	}
}

// Run starts the Agent pipeline: stream capture → STT → translate → controller.
// Blocks until ctx is cancelled or the stream ends.
// Automatically restarts ffmpeg + STT if the audio stream dies.
func (a *Agent) Run(ctx context.Context) error {
	sc := a.streamer
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := a.runPipeline(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			slog.Warn("pipeline ended, restarting...", "name", sc.Name, "err", err, "backoff", backoff)
		} else {
			slog.Warn("pipeline ended normally (stream EOF?), restarting...", "name", sc.Name, "backoff", backoff)
		}

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// runPipeline runs one cycle of: get stream URL → ffmpeg capture → STT → translate.
// Returns when the audio stream ends (ffmpeg dies) or ctx is cancelled.
func (a *Agent) runPipeline(ctx context.Context) error {
	sc := a.streamer

	// 1. Get live stream URL
	streamURL, err := stream.GetStreamURL(ctx, sc.RoomID)
	if err != nil {
		return err
	}
	slog.Info("got stream URL", "name", sc.Name, "room", sc.RoomID)

	// 2. Audio capture via ffmpeg
	audioReader, err := stream.CaptureAudio(ctx, streamURL, nil)
	if err != nil {
		return err
	}
	defer audioReader.Close()

	// 3. STT client
	sttClient, err := stt.NewGoogleSTT(ctx, sc.SourceLang, sc.AltLangs)
	if err != nil {
		return err
	}
	defer sttClient.Close()

	// Pipeline: STT → Translate fan-out → Controller
	pauseReader := &pausableReader{inner: audioReader, isPaused: func() bool {
		return a.ctrl.IsAnyPaused()
	}}

	resultsCh := make(chan stt.StreamResult, 50)

	// STT reader goroutine with reconnect on 305s timeout.
	// Returns (closing resultsCh) when audio EOF is hit — caller restarts pipeline.
	go func() {
		defer close(resultsCh)
		sttBackoff := time.Second
		const maxSTTBackoff = 30 * time.Second

		for {
			if ctx.Err() != nil {
				return
			}
			err := sttClient.Stream(ctx, pauseReader, resultsCh)
			if ctx.Err() != nil {
				return
			}

			if err == nil {
				// Stream returned nil = audio reader EOF → ffmpeg died.
				// Signal caller to restart the whole pipeline.
				slog.Warn("STT stream returned nil (audio EOF), ending pipeline", "name", sc.Name)
				return
			}

			// STT error (e.g., 305s timeout) — reconnect STT only, ffmpeg still alive.
			slog.Warn("STT stream ended, reconnecting...", "name", sc.Name, "err", err, "backoff", sttBackoff)
			select {
			case <-time.After(sttBackoff):
			case <-ctx.Done():
				return
			}
			newClient, err := stt.NewGoogleSTT(ctx, sc.SourceLang, sc.AltLangs)
			if err != nil {
				slog.Error("STT reconnect failed", "err", err)
				return
			}
			if err := sttClient.Close(); err != nil {
				slog.Warn("close old STT client", "err", err)
			}
			sttClient = newClient
			sttBackoff = min(sttBackoff*2, maxSTTBackoff)
		}
	}()

	// Dispatch STT results to translation pool
	workerCount := len(sc.Outputs) * 3
	if workerCount < 3 {
		workerCount = 3
	}
	sem := make(chan struct{}, workerCount)
	slog.Info("translation pool", "streamer", sc.Name, "outputs", len(sc.Outputs), "workers", workerCount)

	seq := 0
	var translateWg sync.WaitGroup
	for result := range resultsCh {
		if !result.IsFinal {
			continue
		}

		slog.Info("STT final", "name", sc.Name,
			"conf", result.Confidence, "text", result.Text, "lang", result.Language)

		if a.ctrl.IsAnyPaused() {
			continue
		}

		currentSeq := seq
		seq++

		sem <- struct{}{} // acquire worker slot
		translateWg.Add(1)
		go func(s int, text, lang string) {
			defer func() { <-sem }() // release worker slot
			defer translateWg.Done()
			controller.TranslateAndSubmit(ctx, a.ctrl, a.translator, s, text, lang, sc.Outputs)
		}(currentSeq, result.Text, result.Language)
	}

	translateWg.Wait()
	return nil
}

// pausableReader wraps an audio reader and discards audio when paused.
type pausableReader struct {
	inner    io.ReadCloser
	isPaused func() bool
}

func (r *pausableReader) Read(p []byte) (int, error) {
	for r.isPaused() {
		buf := make([]byte, 3200) // 100ms of 16kHz 16-bit mono
		if _, err := r.inner.Read(buf); err != nil {
			return 0, err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return r.inner.Read(p)
}

func (r *pausableReader) Close() error {
	return r.inner.Close()
}
