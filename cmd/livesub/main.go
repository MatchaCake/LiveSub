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

	"path/filepath"

	"github.com/christian-lee/livesub/internal/audio"
	"github.com/christian-lee/livesub/internal/auth"
	"github.com/christian-lee/livesub/internal/config"
	"github.com/christian-lee/livesub/internal/danmaku"
	"github.com/christian-lee/livesub/internal/monitor"
	"github.com/christian-lee/livesub/internal/stt"
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

type activeStream struct {
	cancel context.CancelFunc
}

func run(cfgPath string) error {
	hotCfg, err := config.NewHotConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg := hotCfg.Get()

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

	// Room control (pause/resume per room)
	rc := web.NewRoomControl()

	// Init SQLite auth store (before building stream map, since DB streams need it)
	dbPath := filepath.Join(filepath.Dir(cfgPath), "users.db")
	authStore, err := auth.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("init auth store: %w", err)
	}
	defer authStore.Close()

	// Ensure admin from config
	if cfg.Auth.Username != "" && cfg.Auth.Password != "" {
		if err := authStore.EnsureAdmin(cfg.Auth.Username, cfg.Auth.Password); err != nil {
			slog.Error("ensure admin failed", "err", err)
		}
	}

	// Build room → stream config mapping (config + DB)
	mergeStreams := func() map[int64]config.StreamConfig {
		currentCfg := hotCfg.Get()
		merged := make(map[int64]config.StreamConfig)
		for _, sc := range currentCfg.Streams {
			merged[sc.RoomID] = sc
		}
		// DB streams (override config if same room_id)
		if dbStreams, err := authStore.ListStreams(); err == nil {
			for _, ds := range dbStreams {
				if _, exists := merged[ds.RoomID]; !exists {
					sc := config.StreamConfig{
						Name:       ds.Name,
						RoomID:     ds.RoomID,
						SourceLang: ds.SourceLang,
						TargetLang: ds.TargetLang,
					}
					// Fill defaults
					if sc.SourceLang == "" {
						sc.SourceLang = currentCfg.Google.STTLanguage
					}
					if sc.TargetLang == "" {
						sc.TargetLang = currentCfg.Gemini.TargetLang
					}
					merged[sc.RoomID] = sc
				}
			}
		}
		return merged
	}

	streamMap := mergeStreams()
	for _, sc := range streamMap {
		rc.Register(sc.RoomID, sc.Name)
	}

	// Start web control panel
	webPort := cfg.WebPort
	if webPort == 0 {
		webPort = 8899
	}
	transcriptBaseDir := filepath.Join(filepath.Dir(cfgPath), "transcripts")
	webServer := web.NewServer(rc, webPort, authStore, transcriptBaseDir)

	// Sync DB accounts to all active senders
	syncAccountsToSenders := func() {
		dbAccounts, err := authStore.ListBiliAccounts()
		if err != nil {
			slog.Error("load bili accounts from DB", "err", err)
			return
		}
		// Build account list: config default + DB accounts
		currentCfg := hotCfg.Get()
		var accounts []danmaku.Account
		if currentCfg.Bilibili.SESSDATA != "" {
			accounts = append(accounts, danmaku.Account{
				Name: "默认(配置)", SESSDATA: currentCfg.Bilibili.SESSDATA,
				BiliJCT: currentCfg.Bilibili.BiliJCT, UID: currentCfg.Bilibili.UID,
				DanmakuMax: currentCfg.Bilibili.DanmakuMax,
			})
		}
		for _, a := range dbAccounts {
			if !a.Valid {
				continue
			}
			accounts = append(accounts, danmaku.Account{
				Name: a.Name, SESSDATA: a.SESSDATA,
				BiliJCT: a.BiliJCT, UID: a.UID,
				DanmakuMax: a.DanmakuMax,
			})
		}
		// Update all senders
		for _, room := range rc.GetAll() {
			if sender := rc.GetSender(room.RoomID); sender != nil {
				sender.SetAccounts(accounts)
			}
		}
		slog.Info("synced bili accounts to senders", "count", len(accounts))
	}

	webServer.Start()

	// Monitor live status
	mon := monitor.NewBilibiliMonitor(30 * time.Second)
	events := make(chan monitor.RoomEvent, 10)

	// Track active streams
	var mu sync.Mutex
	active := make(map[int64]*activeStream)

	// Register callbacks that need mu/active/mon/events
	webServer.OnAccountChange(syncAccountsToSenders)
	webServer.OnStreamChange(func() {
		newStreamMap := mergeStreams()

		mu.Lock()
		var removedIDs []int64
		for id := range streamMap {
			if _, exists := newStreamMap[id]; !exists {
				removedIDs = append(removedIDs, id)
				if as, running := active[id]; running {
					slog.Info("🔄 stopping removed stream (DB)", "room", id)
					as.cancel()
					delete(active, id)
				}
				rc.Unregister(id)
			}
		}
		var addedIDs []int64
		for id, sc := range newStreamMap {
			if _, exists := streamMap[id]; !exists {
				addedIDs = append(addedIDs, id)
				rc.Register(id, sc.Name)
			}
		}
		streamMap = newStreamMap
		mu.Unlock()

		if len(removedIDs) > 0 {
			mon.RemoveRooms(removedIDs, events)
		}
		if len(addedIDs) > 0 {
			mon.AddRooms(addedIDs)
		}
		slog.Info("🔄 streams updated (DB)", "total", len(newStreamMap), "added", len(addedIDs), "removed", len(removedIDs))
	})

	// Hot reload config
	hotCfg.OnReload(func(newCfg *config.Config) {
		// Update admin credentials from config
		if newCfg.Auth.Username != "" && newCfg.Auth.Password != "" {
			authStore.EnsureAdmin(newCfg.Auth.Username, newCfg.Auth.Password)
		}

		// Reuse stream change logic (config + DB merged)
		newRoomSet := mergeStreams()

		mu.Lock()
		var removedIDs []int64
		for id := range streamMap {
			if _, exists := newRoomSet[id]; !exists {
				removedIDs = append(removedIDs, id)
				if as, running := active[id]; running {
					slog.Info("🔄 stopping removed stream", "room", id)
					as.cancel()
					delete(active, id)
				}
				rc.Unregister(id)
			}
		}
		var addedIDs []int64
		for id, sc := range newRoomSet {
			if _, exists := streamMap[id]; !exists {
				addedIDs = append(addedIDs, id)
				rc.Register(id, sc.Name)
			}
		}
		streamMap = newRoomSet
		mu.Unlock()

		if len(removedIDs) > 0 {
			mon.RemoveRooms(removedIDs, events)
		}
		if len(addedIDs) > 0 {
			mon.AddRooms(addedIDs)
		}

		syncAccountsToSenders()

		slog.Info("🔄 config reloaded",
			"total", len(newRoomSet),
			"added", len(addedIDs),
			"removed", len(removedIDs),
		)
	})
	hotCfg.Watch()

	// Event handler
	go func() {
		for ev := range events {
			mu.Lock()
			rc.SetLive(ev.RoomID, ev.Live)

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
					transcriptDir := filepath.Join(filepath.Dir(cfgPath), "transcripts")
				if err := runStream(streamCtx, hotCfg.Get(), sc, translator, pool, rc, syncAccountsToSenders, transcriptDir); err != nil {
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

	roomIDs := make([]int64, 0, len(streamMap))
	for id := range streamMap {
		roomIDs = append(roomIDs, id)
	}

	webURL := fmt.Sprintf("http://localhost:%d", webPort)
	slog.Info("livesub started", "streams", len(streamMap), "rooms", roomIDs, "web", webURL)

	// Auto-open browser
	openBrowser(webURL)

	return mon.Watch(ctx, roomIDs, events)
}

func runStream(ctx context.Context, cfg *config.Config, sc config.StreamConfig, translator *translate.GeminiTranslator, pool *translatePool, rc *web.RoomControl, syncAccounts func(), transcriptBaseDir string) error {
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

	// 4. Danmaku sender (multi-account from DB + config fallback)
	sender := danmaku.NewBilibiliSender(
		sc.RoomID,
		cfg.Bilibili.SESSDATA,
		cfg.Bilibili.BiliJCT,
		cfg.Bilibili.UID,
	)
	if cfg.Bilibili.DanmakuMax > 0 {
		sender.MaxLength = cfg.Bilibili.DanmakuMax
	}
	// Add config accounts as fallback
	for _, acc := range cfg.Bilibili.Accounts {
		sender.AddAccount(danmaku.Account{
			Name:       acc.Name,
			SESSDATA:   acc.SESSDATA,
			BiliJCT:    acc.BiliJCT,
			UID:        acc.UID,
			DanmakuMax: acc.DanmakuMax,
		})
	}
	rc.SetSender(sc.RoomID, sender)
	// Sync DB accounts (overwrites with full list including DB accounts)
	if syncAccounts != nil {
		syncAccounts()
	}

	// 5. Transcript logger
	transcriptDir := transcriptBaseDir
	tlog, err := transcript.NewLogger(transcriptDir, sc.RoomID, sc.Name)
	if err != nil {
		slog.Warn("transcript logger failed, continuing without", "err", err)
	} else {
		defer tlog.Close()
		slog.Info("📄 transcript logging", "path", tlog.Path())
	}

	// Pipeline: STT → Translate → Send
	// Use a pausable reader that discards audio when paused (saves STT cost)
	pauseReader := audio.NewPausableReader(audioReader, func() bool {
		return rc.IsPaused(sc.RoomID)
	})

	resultsCh := make(chan stt.StreamResult, 50)

	go func() {
		for {
			if ctx.Err() != nil {
				close(resultsCh)
				return
			}
			if err := sttClient.Stream(ctx, pauseReader, resultsCh); err != nil {
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
		type pendingEntry struct {
			text   string
			source string
		}
		pending := make(map[int]pendingEntry)
		for msg := range doneCh {
			pending[msg.seq] = pendingEntry{text: msg.text, source: msg.source}
			for {
				entry, ok := pending[nextSeq]
				if !ok {
					break
				}
				delete(pending, nextSeq)
				nextSeq++
				if entry.text == "" {
					continue
				}
				// Log transcript regardless of pause
				if tlog != nil {
					tlog.Write(entry.source, entry.text)
				}
				// Check pause before sending
				if rc.IsPaused(sc.RoomID) {
					slog.Info("⏸ paused, dropping", "name", sc.Name, "text", entry.text)
					continue
				}
				slog.Info("📝 sending", "name", sc.Name, "seq", nextSeq-1, "text", entry.text)
				if err := sender.Send(entry.text); err != nil {
					slog.Error("danmaku error", "name", sc.Name, "err", err)
				}
			}
		}
	}()

	// Dispatch STT results to shared pool
	seq := 0
	for result := range resultsCh {
		if !result.IsFinal {
			continue
		}

		// Update last text for web UI
		rc.SetLastText(sc.RoomID, result.Text)

		// Check pause before translating (save API calls)
		if rc.IsPaused(sc.RoomID) {
			continue
		}

		slog.Info("📊 dispatch", "name", sc.Name,
			"conf", result.Confidence, "len", len([]rune(result.Text)),
			"text", result.Text)

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
			source: result.Text,
		})
		seq++
	}
	close(doneCh)
	senderWg.Wait()

	return nil
}

// --- Shared translation pool ---

type translateResult struct {
	seq    int
	text   string
	source string // original STT text
}

type translateJob struct {
	seq    int
	text   string
	lang   string
	name   string // stream name for logging
	direct bool
	doneCh chan<- translateResult
	source string // original text (same as text for direct)
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
					job.doneCh <- translateResult{seq: job.seq, text: job.text, source: job.source}
					continue
				}
				translated, err := translator.Translate(ctx, job.text, job.lang)
				if err != nil {
					slog.Error("translate error", "worker", id, "name", job.name, "err", err)
					job.doneCh <- translateResult{seq: job.seq, text: "", source: job.source}
					continue
				}
				if translated != "" {
					slog.Info("📝 translated", "worker", id, "name", job.name, "src", job.text, "dst", translated)
				}
				job.doneCh <- translateResult{seq: job.seq, text: translated, source: job.source}
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
