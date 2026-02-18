package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/christian-lee/livesub/internal/audio"
	"github.com/christian-lee/livesub/internal/config"
	"github.com/christian-lee/livesub/internal/danmaku"
	"github.com/christian-lee/livesub/internal/monitor"
	"github.com/christian-lee/livesub/internal/stt"
	"github.com/christian-lee/livesub/internal/translate"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  livesub run [config]     Start monitoring & translating")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		cfgPath := "config.yaml"
		if len(os.Args) > 2 {
			cfgPath = os.Args[2]
		}
		if err := run(cfgPath); err != nil {
			slog.Error("run failed", "err", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

type activeStream struct {
	cancel context.CancelFunc
}

func run(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Streams) == 0 {
		return fmt.Errorf("no streams configured")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("shutting down...")
		cancel()
	}()

	// Init Gemini translator (shared)
	translator, err := translate.NewGeminiTranslator(ctx, cfg.Gemini.APIKey, cfg.Gemini.Model, cfg.Gemini.TargetLang)
	if err != nil {
		return fmt.Errorf("init translator: %w", err)
	}
	defer translator.Close()

	// Shared translation worker pool (3 workers per stream)
	poolSize := len(cfg.Streams) * 3
	if poolSize < 3 {
		poolSize = 3
	}
	pool := newTranslatePool(ctx, translator, poolSize)
	defer pool.close()

	// Build room → stream config mapping
	streamMap := make(map[int64]config.StreamConfig)
	roomIDs := make([]int64, 0, len(cfg.Streams))
	for _, sc := range cfg.Streams {
		streamMap[sc.RoomID] = sc
		roomIDs = append(roomIDs, sc.RoomID)
	}

	// Monitor live status
	mon := monitor.NewBilibiliMonitor(30 * time.Second)
	events := make(chan monitor.RoomEvent, 10)

	// Track active streams
	var mu sync.Mutex
	active := make(map[int64]*activeStream)

	// Event handler
	go func() {
		for ev := range events {
			mu.Lock()
			if ev.Live {
				if _, running := active[ev.RoomID]; running {
					mu.Unlock()
					continue
				}

				sc := streamMap[ev.RoomID]
				streamCtx, streamCancel := context.WithCancel(ctx)

				slog.Info("🎙️ room went live, starting pipeline",
					"name", sc.Name,
					"room", ev.RoomID,
					"title", ev.Title,
				)

				active[ev.RoomID] = &activeStream{cancel: streamCancel}
				mu.Unlock()

				go func(sc config.StreamConfig, streamCtx context.Context, streamCancel context.CancelFunc) {
					if err := runStream(streamCtx, cfg, sc, translator, pool); err != nil {
						slog.Error("stream ended", "name", sc.Name, "err", err)
					}
					streamCancel()
					mu.Lock()
					delete(active, sc.RoomID)
					mu.Unlock()
				}(sc, streamCtx, streamCancel)
			} else {
				if as, running := active[ev.RoomID]; running {
					slog.Info("📴 room went offline, stopping", "room", ev.RoomID)
					as.cancel()
					delete(active, ev.RoomID)
				}
				mu.Unlock()
			}
		}
	}()

	slog.Info("livesub started", "streams", len(cfg.Streams), "rooms", roomIDs)
	return mon.Watch(ctx, roomIDs, events)
}

func runStream(ctx context.Context, cfg *config.Config, sc config.StreamConfig, translator *translate.GeminiTranslator, pool *translatePool) error {
	// 1. Get live stream URL
	streamURL, err := audio.GetBilibiliStreamURL(sc.RoomID)
	if err != nil {
		return fmt.Errorf("get stream url: %w", err)
	}
	slog.Info("got stream URL", "name", sc.Name, "room", sc.RoomID)

	// 2. Audio capture via ffmpeg + music detection
	capturer := audio.NewCapturer()
	rawReader, err := capturer.Start(ctx, streamURL)
	if err != nil {
		return fmt.Errorf("start audio: %w", err)
	}
	defer rawReader.Close()

	musicDetector := audio.NewMusicDetector(16000)
	audioReader := audio.NewAnalyzingReader(rawReader, musicDetector)

	// 3. STT
	sttClient, err := stt.NewGoogleSTT(ctx, sc.SourceLang, sc.AltLangs)
	if err != nil {
		return fmt.Errorf("init stt: %w", err)
	}
	defer sttClient.Close()

	// 4. Danmaku sender
	sender := danmaku.NewBilibiliSender(
		sc.RoomID,
		cfg.Bilibili.SESSDATA,
		cfg.Bilibili.BiliJCT,
		cfg.Bilibili.UID,
	)
	if cfg.Bilibili.DanmakuMax > 0 {
		sender.MaxLength = cfg.Bilibili.DanmakuMax
	}

	// Pipeline: STT → Translate → Send
	resultsCh := make(chan stt.StreamResult, 50)

	go func() {
		for {
			if ctx.Err() != nil {
				close(resultsCh)
				return
			}
			if err := sttClient.Stream(ctx, audioReader, resultsCh); err != nil {
				if ctx.Err() != nil {
					close(resultsCh)
					return
				}
				slog.Warn("STT stream ended, reconnecting...", "name", sc.Name, "err", err)
				time.Sleep(1 * time.Second)
				newClient, err := stt.NewGoogleSTT(ctx, sc.SourceLang, sc.AltLangs)
				if err != nil {
					slog.Error("STT reconnect failed", "err", err)
					close(resultsCh)
					return
				}
				sttClient.Close()
				sttClient = newClient
			}
		}
	}()

	targetLang := sc.TargetLang
	if targetLang == "" {
		targetLang = cfg.Gemini.TargetLang
	}

	// Per-stream result channel for ordered sending
	doneCh := make(chan translateResult, 50)

	// Ordered sender: buffer out-of-order results, send in sequence
	var senderWg sync.WaitGroup
	senderWg.Add(1)
	go func() {
		defer senderWg.Done()
		nextSeq := 0
		pending := make(map[int]string)
		for msg := range doneCh {
			pending[msg.seq] = msg.text
			for {
				text, ok := pending[nextSeq]
				if !ok {
					break
				}
				delete(pending, nextSeq)
				nextSeq++
				if text == "" {
					continue
				}
				slog.Info("📝 sending", "name", sc.Name, "seq", nextSeq-1, "text", text)
				if err := sender.Send(text); err != nil {
					slog.Error("danmaku error", "name", sc.Name, "err", err)
				}
			}
		}
	}()

	// Dispatch STT results to shared pool
	// Music detection: spectral analysis + long text heuristic
	// Skip when: music detected OR (borderline music + long lyrics-like text)

	seq := 0
	for result := range resultsCh {
		if !result.IsFinal {
			continue
		}

		musicScore := musicDetector.Score()
		isMusic := musicDetector.IsMusic()
		textLen := len([]rune(result.Text))

		// Hard skip: clear music
		// Soft skip: borderline score (0.25+) AND long text (>60 chars, likely lyrics)
		softSkip := musicScore > 0.25 && textLen > 60

		if isMusic || softSkip {
			slog.Info("🎵 skipping (music detected)", "name", sc.Name,
				"text", result.Text, "score", fmt.Sprintf("%.2f", musicScore),
				"conf", result.Confidence, "len", textLen,
				"hard", isMusic, "soft", softSkip)
			continue
		}

		slog.Info("📊 dispatch", "name", sc.Name,
			"musicScore", fmt.Sprintf("%.2f", musicScore),
			"conf", result.Confidence, "len", textLen, "text", result.Text)

		direct := isTargetLang(result.Language, targetLang)
		if direct {
			slog.Info("📝 direct", "name", sc.Name, "text", result.Text, "lang", result.Language)
		}

		pool.submit(translateJob{
			seq:    seq,
			text:   result.Text,
			lang:   result.Language,
			name:   sc.Name,
			direct: direct,
			doneCh: doneCh,
		})
		seq++
	}
	close(doneCh)
	senderWg.Wait()

	return nil
}

// --- Shared translation pool ---

type translateResult struct {
	seq  int
	text string
}

type translateJob struct {
	seq    int
	text   string
	lang   string
	name   string // stream name for logging
	direct bool
	doneCh chan<- translateResult
}

type translatePool struct {
	jobCh chan translateJob
	wg    sync.WaitGroup
}

func newTranslatePool(ctx context.Context, translator *translate.GeminiTranslator, workers int) *translatePool {
	p := &translatePool{
		jobCh: make(chan translateJob, 100),
	}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go func(id int) {
			defer p.wg.Done()
			for job := range p.jobCh {
				if job.direct {
					job.doneCh <- translateResult{seq: job.seq, text: job.text}
					continue
				}
				translated, err := translator.Translate(ctx, job.text, job.lang)
				if err != nil {
					slog.Error("translate error", "worker", id, "name", job.name, "err", err)
					job.doneCh <- translateResult{seq: job.seq, text: ""}
					continue
				}
				if translated != "" {
					slog.Info("📝 translated", "worker", id, "name", job.name, "src", job.text, "dst", translated)
				}
				job.doneCh <- translateResult{seq: job.seq, text: translated}
			}
		}(i)
	}
	return p
}

func (p *translatePool) submit(job translateJob) {
	p.jobCh <- job
}

func (p *translatePool) close() {
	close(p.jobCh)
	p.wg.Wait()
}

func isTargetLang(detected, target string) bool {
	if detected == "" || target == "" {
		return false
	}
	d := strings.ToLower(detected)
	t := strings.ToLower(target)

	if strings.HasPrefix(d, strings.Split(t, "-")[0]) {
		return true
	}
	if strings.Contains(t, "zh") && (strings.Contains(d, "cmn") || strings.Contains(d, "zh")) {
		return true
	}
	return false
}
