package main

import (
	"context"
	"errors"
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

	dm "github.com/MatchaCake/bilibili_dm_lib"
	stream "github.com/MatchaCake/bilibili_stream_lib"
	"github.com/christian-lee/livesub/internal/agent"
	"github.com/christian-lee/livesub/internal/auth"
	"github.com/christian-lee/livesub/internal/bot"
	"github.com/christian-lee/livesub/internal/command"
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
			if errors.Is(err, context.Canceled) {
				// Graceful shutdown (SIGTERM) — not a failure; exit 0 so
				// systemd doesn't record every stop as 'Failed'.
				return
			}
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
				// Disable any live bot for this account: leaving it in the pool
				// keeps sending with a revoked cookie until the next restart.
				if existing := pool.Get(a.Name); existing != nil {
					if bb, ok := existing.(*bot.BilibiliBot); ok {
						bb.UpdateCredentials("", "", 0, 0)
						slog.Info("disabled invalid bot account", "name", a.Name)
					}
				}
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
	var streamWg sync.WaitGroup // tracks stream pipeline goroutines for graceful shutdown

	// Web server
	webServer := web.NewServer(pool, webPort, authStore, transcriptDir, cfg, cfgPath)

	// Register callbacks
	webServer.OnAccountChange(syncDBBots)

	// Danmaku command handlers for streamers with command_uids.
	// Guarded by mu (mutated on hot reload as streamers are added/removed).
	type cmdEntry struct {
		handler *command.Handler
		cancel  context.CancelFunc
	}
	cmdHandlers := make(map[int64]*cmdEntry) // roomID → handler + its cancel

	// getCmdHandler safely fetches a handler for the monitor goroutine.
	getCmdHandler := func(roomID int64) *command.Handler {
		mu.Lock()
		defer mu.Unlock()
		if e, ok := cmdHandlers[roomID]; ok {
			return e.handler
		}
		return nil
	}

	// startCmdHandler creates and starts a danmaku command handler for sc.
	// Caller must hold mu. No-op if one already exists for the room.
	startCmdHandler := func(sc config.StreamerConfig) {
		if len(sc.CommandUIDs) == 0 || sc.RoomID == 0 {
			return
		}
		if _, exists := cmdHandlers[sc.RoomID]; exists {
			return
		}
		// Use 佯攻菲娜 for danmaku listening; fall back to any available bot
		var dmClient *dm.Client
		if bb, ok := pool.Get("佯攻菲娜").(*bot.BilibiliBot); ok && bb.Available() {
			dmClient = dm.NewClient(dm.WithCookie(bb.SESSDATA(), bb.BiliJCT()), dm.WithRoomID(sc.RoomID))
		} else {
			for _, b := range pool.All() {
				if bb, ok := b.(*bot.BilibiliBot); ok && bb.Available() {
					dmClient = dm.NewClient(dm.WithCookie(bb.SESSDATA(), bb.BiliJCT()), dm.WithRoomID(sc.RoomID))
					break
				}
			}
		}
		if dmClient == nil {
			slog.Warn("no bot available for command handler", "room", sc.RoomID)
			return
		}
		hctx, hcancel := context.WithCancel(ctx)
		h := command.New(sc.RoomID, sc.CommandUIDs, dmClient, command.WithPool(pool))
		// Reconnect an already-live streamer's controller to the new handler.
		if as, ok := active[sc.RoomID]; ok && as.ctrl != nil {
			h.SetController(as.ctrl)
		}
		cmdHandlers[sc.RoomID] = &cmdEntry{handler: h, cancel: hcancel}
		go func(roomID int64, client *dm.Client, handler *command.Handler) {
			go handler.Run(hctx)
			if err := client.Start(hctx); err != nil && hctx.Err() == nil {
				slog.Error("command dm client failed", "room", roomID, "err", err)
			}
		}(sc.RoomID, dmClient, h)
		slog.Info("command handler started", "room", sc.RoomID, "uids", len(sc.CommandUIDs))
	}

	// stopCmdHandler tears down the handler for a room. Caller must hold mu.
	stopCmdHandler := func(roomID int64) {
		if e, ok := cmdHandlers[roomID]; ok {
			e.cancel()
			delete(cmdHandlers, roomID)
			slog.Info("command handler stopped", "room", roomID)
		}
	}

	mu.Lock()
	for _, sc := range cfg.Streamers {
		startCmdHandler(sc)
	}
	mu.Unlock()

	// Monitor live status for all streamers (created early for hot reload access)
	mon := stream.NewMonitor(stream.WithMonitorInterval(30 * time.Second))

	// Hot reload
	hotCfg.OnReload(func(newCfg *config.Config) {
		if newCfg.Web.Auth.Username != "" && newCfg.Web.Auth.Password != "" {
			if err := authStore.EnsureAdmin(newCfg.Web.Auth.Username, newCfg.Web.Auth.Password); err != nil {
				slog.Error("ensure admin on reload", "err", err)
			}
		}
		syncDBBots()
		// Update server config pointer
		webServer.UpdateConfig(newCfg)

		// Stop pipelines for streamers removed by a direct file edit —
		// mirrors OnStreamerChange (web UI path); without this the removed
		// streamer keeps translating until the process exits.
		newRoomSet := make(map[int64]bool)
		for _, rid := range newCfg.RoomIDs() {
			newRoomSet[rid] = true
		}
		mu.Lock()
		for rid, as := range active {
			if !newRoomSet[rid] {
				slog.Info("stopping streamer removed from config", "room", rid, "name", as.name)
				as.cancel()
				delete(active, rid)
			}
		}
		// Reconcile command handlers with the new config: start newly-eligible
		// streamers, stop removed ones, refresh whitelists on the rest.
		desired := make(map[int64]config.StreamerConfig)
		for _, sc := range newCfg.Streamers {
			if len(sc.CommandUIDs) > 0 && sc.RoomID != 0 {
				desired[sc.RoomID] = sc
			}
		}
		for rid := range cmdHandlers {
			if _, ok := desired[rid]; !ok {
				stopCmdHandler(rid)
			}
		}
		for rid, sc := range desired {
			if e, ok := cmdHandlers[rid]; ok {
				e.handler.UpdateUIDs(sc.CommandUIDs)
			} else {
				startCmdHandler(sc)
			}
		}
		mu.Unlock()

		// Add new rooms to monitor
		for _, rid := range newCfg.RoomIDs() {
			mon.AddRoom(rid)
		}
		slog.Info("hot reload: server config + monitor rooms + command handlers updated")
	})
	hotCfg.Watch()

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

		// Reconcile command handlers (same as the file-reload path).
		desired := make(map[int64]config.StreamerConfig)
		for _, sc := range newCfg.Streamers {
			if len(sc.CommandUIDs) > 0 && sc.RoomID != 0 {
				desired[sc.RoomID] = sc
			}
		}
		for rid := range cmdHandlers {
			if _, ok := desired[rid]; !ok {
				stopCmdHandler(rid)
			}
		}
		for rid, sc := range desired {
			if e, ok := cmdHandlers[rid]; ok {
				e.handler.UpdateUIDs(sc.CommandUIDs)
			} else {
				startCmdHandler(sc)
			}
		}

		mu.Unlock()

		// Sync monitor rooms
		for _, s := range newCfg.Streamers {
			if s.RoomID != 0 {
				mon.AddRoom(s.RoomID)
			}
		}
	})

	webServer.Start()

	// Process monitor events for all streamers
	go func() {
		for ev := range monEvents {
			mu.Lock()
			currentCfg := hotCfg.Get()
			sc := currentCfg.FindStreamerByRoom(ev.RoomID)
			if sc == nil {
				// Streamer removed from config (file edit / web UI) while its
				// pipeline is running: still honor offline events, otherwise the
				// pipeline becomes an unstoppable orphan.
				if as, ok := active[ev.RoomID]; ok && !ev.Live {
					slog.Info("room removed from config went offline, stopping orphan", "name", as.name, "room", ev.RoomID)
					as.cancel()
					delete(active, ev.RoomID)
				}
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

				streamWg.Add(1)
				go func(sc config.StreamerConfig) {
					defer streamWg.Done()
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
					ctrl.OnChange(func() { webServer.BroadcastStatus() })
					ctrl.Start(streamCtx)

					mu.Lock()
					if as, ok := active[sc.RoomID]; ok {
						as.ctrl = ctrl
					}
					mu.Unlock()

					// Link command handler to this controller
					if ch := getCmdHandler(sc.RoomID); ch != nil {
						ch.SetController(ctrl)
					}

					// Create and run agent
					a := agent.New(sc, translator, ctrl)
					if err := a.Run(streamCtx); err != nil {
						slog.Error("stream ended", "name", sc.Name, "err", err)
					}

					ctrl.Stop()
					streamCancel()

					// Only tear down shared state if it still belongs to THIS
					// pipeline. On off→on flapping, a newer pipeline may already
					// own active[room]/the web+command controllers; clobbering them
					// would leak the new pipeline (unstoppable) and double-send.
					mu.Lock()
					if as, ok := active[sc.RoomID]; ok && as.ctrl == ctrl {
						delete(active, sc.RoomID)
						mu.Unlock()
						webServer.SetController(sc.Name, nil)
						if ch := getCmdHandler(sc.RoomID); ch != nil {
							ch.SetController(nil)
						}
					} else {
						mu.Unlock()
					}
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

	// Graceful drain: give pipelines a bounded window to finish their teardown
	// (controller flush, transcript Close). Exiting immediately loses the tail
	// of every transcript on each restart/deploy.
	drained := make(chan struct{})
	go func() {
		streamWg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(10 * time.Second):
		slog.Warn("shutdown drain timed out, exiting anyway")
	}
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
