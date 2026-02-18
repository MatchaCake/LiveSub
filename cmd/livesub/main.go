package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	stream "github.com/MatchaCake/bilibili_stream_lib"
	"github.com/christian-lee/livesub/internal/agent"
	"github.com/christian-lee/livesub/internal/auth"
	"github.com/christian-lee/livesub/internal/bot"
	"github.com/christian-lee/livesub/internal/config"
	"github.com/christian-lee/livesub/internal/controller"
	"github.com/christian-lee/livesub/internal/transcript"
	"github.com/christian-lee/livesub/internal/translate"
	"github.com/christian-lee/livesub/internal/web"
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

func run(cfgPath string) error {
	hotCfg, err := config.NewHotConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg := hotCfg.Get()

	if cfg.Streamer.RoomID == 0 {
		return fmt.Errorf("no streamer room_id configured")
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

	// Init Gemini translator
	translator, err := translate.NewGeminiTranslator(ctx, cfg.Translation.APIKey, cfg.Translation.Model)
	if err != nil {
		return fmt.Errorf("init translator: %w", err)
	}
	defer translator.Close()

	// Init bot pool from config
	pool := bot.NewPool()
	for _, bc := range cfg.Bots {
		b := bot.NewBilibiliBot(bc.Name, cfg.Streamer.RoomID, bc.SESSDATA, bc.BiliJCT, bc.UID, bc.DanmakuMax)
		pool.Add(b)
	}

	// Init SQLite auth store
	dbPath := filepath.Join(filepath.Dir(cfgPath), "users.db")
	authStore, err := auth.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("init auth store: %w", err)
	}
	defer authStore.Close()

	// Ensure admin from config
	if cfg.Web.Auth.Username != "" && cfg.Web.Auth.Password != "" {
		if err := authStore.EnsureAdmin(cfg.Web.Auth.Username, cfg.Web.Auth.Password); err != nil {
			slog.Error("ensure admin failed", "err", err)
		}
	}

	// Sync DB accounts to bot pool
	syncDBBots := func() {
		dbAccounts, err := authStore.ListBiliAccounts()
		if err != nil {
			slog.Error("load bili accounts from DB", "err", err)
			return
		}
		for _, a := range dbAccounts {
			if !a.Valid {
				continue
			}
			existing := pool.Get(a.Name)
			if existing != nil {
				// Update existing bot credentials
				if bb, ok := existing.(*bot.BilibiliBot); ok {
					bb.UpdateCredentials(a.SESSDATA, a.BiliJCT, a.UID, a.DanmakuMax)
				}
			} else {
				// Add new bot from DB
				b := bot.NewBilibiliBot(a.Name, cfg.Streamer.RoomID, a.SESSDATA, a.BiliJCT, a.UID, a.DanmakuMax)
				pool.Add(b)
			}
		}
		slog.Info("synced DB accounts to bot pool", "total_bots", len(pool.Names()))
	}
	syncDBBots()

	// Transcript logger setup
	transcriptDir := filepath.Join(filepath.Dir(cfgPath), "transcripts")

	// Web port
	webPort := cfg.Web.Port
	if webPort == 0 {
		webPort = 8899
	}

	// Web server
	webServer := web.NewServer(pool, webPort, authStore, transcriptDir, cfg)

	// Register callbacks
	webServer.OnAccountChange(syncDBBots)

	// Hot reload
	hotCfg.OnReload(func(newCfg *config.Config) {
		if newCfg.Web.Auth.Username != "" && newCfg.Web.Auth.Password != "" {
			if err := authStore.EnsureAdmin(newCfg.Web.Auth.Username, newCfg.Web.Auth.Password); err != nil {
				slog.Error("ensure admin on reload", "err", err)
			}
		}
		syncDBBots()
	})
	hotCfg.Watch()

	webServer.Start()

	// Monitor live status
	mon := stream.NewMonitor(stream.WithMonitorInterval(30 * time.Second))
	monEvents, err := mon.Watch(ctx, []int64{cfg.Streamer.RoomID})
	if err != nil {
		return fmt.Errorf("start monitor: %w", err)
	}

	type activeStream struct {
		cancel context.CancelFunc
	}
	var (
		mu     sync.Mutex
		active *activeStream
	)

	go func() {
		for ev := range monEvents {
			mu.Lock()
			webServer.SetLive(ev.Live)

			if ev.Live {
				if active != nil {
					mu.Unlock()
					continue
				}

				slog.Info("room went live, starting pipeline",
					"name", cfg.Streamer.Name,
					"room", ev.RoomID,
					"title", ev.Title,
				)

				streamCtx, streamCancel := context.WithCancel(ctx)
				active = &activeStream{cancel: streamCancel}
				mu.Unlock()

				go func() {
					currentCfg := hotCfg.Get()

					// Create transcript logger for this session
					tlog, err := transcript.NewLogger(transcriptDir, currentCfg.Streamer.RoomID, currentCfg.Streamer.Name)
					if err != nil {
						slog.Warn("transcript logger failed, continuing without", "err", err)
					} else {
						defer tlog.Close()
						slog.Info("transcript logging", "path", tlog.Path())
					}

					// Create controller
					ctrl := controller.New(pool, currentCfg.Outputs, tlog)
					ctrl.Start(streamCtx)
					webServer.SetController(ctrl)

					// Create and run agent
					a := agent.New(currentCfg, translator, ctrl)
					if err := a.Run(streamCtx); err != nil {
						slog.Error("stream ended", "name", currentCfg.Streamer.Name, "err", err)
					}

					ctrl.Stop()
					webServer.SetController(nil)
					streamCancel()

					mu.Lock()
					active = nil
					mu.Unlock()
				}()
			} else {
				if active != nil {
					slog.Info("room went offline, stopping", "room", ev.RoomID)
					active.cancel()
					active = nil
				}
				mu.Unlock()
			}
		}
	}()

	webURL := fmt.Sprintf("http://localhost:%d", webPort)
	slog.Info("livesub started",
		"streamer", cfg.Streamer.Name,
		"room", cfg.Streamer.RoomID,
		"outputs", len(cfg.Outputs),
		"bots", len(pool.Names()),
		"web", webURL,
	)

	openBrowser(webURL)

	<-ctx.Done()
	return ctx.Err()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
