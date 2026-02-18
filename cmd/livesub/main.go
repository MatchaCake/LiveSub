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
					if err := runStream(streamCtx, cfg, sc, translator); err != nil {
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

func runStream(ctx context.Context, cfg *config.Config, sc config.StreamConfig, translator *translate.GeminiTranslator) error {
	// 1. Get live stream URL
	streamURL, err := audio.GetBilibiliStreamURL(sc.RoomID)
	if err != nil {
		return fmt.Errorf("get stream url: %w", err)
	}
	slog.Info("got stream URL", "name", sc.Name, "room", sc.RoomID)

	// 2. Audio capture via ffmpeg
	capturer := audio.NewCapturer()
	audioReader, err := capturer.Start(ctx, streamURL)
	if err != nil {
		return fmt.Errorf("start audio: %w", err)
	}
	defer audioReader.Close()

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

	for result := range resultsCh {
		if !result.IsFinal {
			continue
		}

		// If already in target language, send directly
		if isTargetLang(result.Language, targetLang) {
			slog.Info("📝 direct", "name", sc.Name, "text", result.Text, "lang", result.Language)
			if err := sender.Send(result.Text); err != nil {
				slog.Error("danmaku error", "name", sc.Name, "err", err)
			}
			continue
		}

		translated, err := translator.Translate(ctx, result.Text, result.Language)
		if err != nil {
			slog.Error("translate error", "name", sc.Name, "err", err)
			continue
		}
		if translated == "" {
			continue
		}

		slog.Info("📝 translated", "name", sc.Name, "src", result.Text, "dst", translated, "lang", result.Language)
		if err := sender.Send(translated); err != nil {
			slog.Error("danmaku error", "name", sc.Name, "err", err)
		}
	}

	return nil
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
