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

// activeStream tracks a running streamer pipeline.
type activeStream struct {
	cancel context.CancelFunc
	ctrl   *controller.Controller
	name   string
}

func run(cfgPath string) error {
	hotCfg, err := config.NewHotConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg := hotCfg.Get()

	if len(cfg.Streamers) == 0 {
		return fmt.Errorf("no streamers configured")
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
		b := bot.NewBilibiliBot(bc.Name, 0, bc.SESSDATA, bc.BiliJCT, bc.UID, bc.DanmakuMax)
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
				if bb, ok := existing.(*bot.BilibiliBot); ok {
					bb.UpdateCredentials(a.SESSDATA, a.BiliJCT, a.UID, a.DanmakuMax)
				}
			} else {
				b := bot.NewBilibiliBot(a.Name, 0, a.SESSDATA, a.BiliJCT, a.UID, a.DanmakuMax)
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

	// Active streams map: room_id → activeStream
	var mu sync.Mutex
	active := make(map[int64]*activeStream)

	// Web server
	webServer := web.NewServer(pool, webPort, authStore, transcriptDir, cfg, cfgPath)

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

	// Monitor live status for all streamers
	mon := stream.NewMonitor(stream.WithMonitorInterval(30 * time.Second))
	roomIDs := cfg.RoomIDs()
	monEvents, err := mon.Watch(ctx, roomIDs)
	if err != nil {
		return fmt.Errorf("start monitor: %w", err)
	}

	// Register streamer change callback — resync monitor rooms
	webServer.OnStreamerChange(func() {
		newCfg := hotCfg.Get()
		mu.Lock()

		// Find rooms that were removed
		newRoomSet := make(map[int64]bool)
		for _, rid := range newCfg.RoomIDs() {
			newRoomSet[rid] = true
		}
		for rid, as := range active {
			if !newRoomSet[rid] {
				slog.Info("stopping removed streamer", "room", rid, "name", as.name)
				as.cancel()
				delete(active, rid)
			}
		}

		// Find rooms that were added
		currentRooms := make(map[int64]bool)
		for rid := range active {
			currentRooms[rid] = true
		}
		mu.Unlock()

		// Sync monitor rooms
		for _, s := range newCfg.Streamers {
			if s.RoomID != 0 {
				mon.AddRoom(s.RoomID)
			}
		}

		webServer.SetLive(newCfg.Streamers[0].Name, false)
	})

	webServer.Start()

	// Process monitor events for all streamers
	go func() {
		for ev := range monEvents {
			mu.Lock()
			currentCfg := hotCfg.Get()
			sc := currentCfg.FindStreamerByRoom(ev.RoomID)
			if sc == nil {
				mu.Unlock()
				slog.Warn("monitor event for unknown room", "room", ev.RoomID)
				continue
			}

			streamerName := sc.Name
			webServer.SetLive(streamerName, ev.Live)

			if ev.Live {
				if active[ev.RoomID] != nil {
					mu.Unlock()
					continue
				}

				slog.Info("room went live, starting pipeline",
					"name", sc.Name,
					"room", ev.RoomID,
					"title", ev.Title,
				)

				streamCtx, streamCancel := context.WithCancel(ctx)
				streamerCfg := *sc // copy
				active[ev.RoomID] = &activeStream{
					cancel: streamCancel,
					name:   sc.Name,
				}
				mu.Unlock()

				go func(sc config.StreamerConfig) {
					// Create transcript logger for this session
					tlog, err := transcript.NewLogger(transcriptDir, sc.RoomID, sc.Name)
					if err != nil {
						slog.Warn("transcript logger failed, continuing without", "err", err)
					} else {
						defer tlog.Close()
						slog.Info("transcript logging", "path", tlog.Path())
					}

					// Create controller for this streamer
					ctrl := controller.New(pool, sc.Outputs, tlog, sc.RoomID)
					webServer.SetController(sc.Name, ctrl) // sync pause state BEFORE start
					ctrl.Start(streamCtx)

					mu.Lock()
					if as, ok := active[sc.RoomID]; ok {
						as.ctrl = ctrl
					}
					mu.Unlock()

					// Create and run agent
					a := agent.New(sc, translator, ctrl)
					if err := a.Run(streamCtx); err != nil {
						slog.Error("stream ended", "name", sc.Name, "err", err)
					}

					ctrl.Stop()
					webServer.SetController(sc.Name, nil)
					streamCancel()

					mu.Lock()
					delete(active, sc.RoomID)
					mu.Unlock()
				}(streamerCfg)
			} else {
				if as, ok := active[ev.RoomID]; ok {
					slog.Info("room went offline, stopping", "name", as.name, "room", ev.RoomID)
					as.cancel()
					delete(active, ev.RoomID)
				}
				mu.Unlock()
			}
		}
	}()

	webURL := fmt.Sprintf("http://localhost:%d", webPort)
	streamerNames := make([]string, len(cfg.Streamers))
	for i, s := range cfg.Streamers {
		streamerNames[i] = s.Name
	}
	slog.Info("livesub started",
		"streamers", streamerNames,
		"rooms", roomIDs,
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
