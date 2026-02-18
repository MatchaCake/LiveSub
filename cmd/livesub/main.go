package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	stream "github.com/MatchaCake/bilibili_stream_lib"
	"github.com/christian-lee/livesub/internal/auth"
	"github.com/christian-lee/livesub/internal/config"
	"github.com/christian-lee/livesub/internal/danmaku"
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
		hidden := authStore.ListHiddenRooms()
		merged := make(map[int64]config.StreamConfig)
		for _, sc := range currentCfg.Streams {
			if hidden[sc.RoomID] {
				continue
			}
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

	// mu protects both streamMap and active.
	var mu sync.Mutex
	streamMap := mergeStreams()
	active := make(map[int64]*activeStream)

	// newSender creates a BilibiliSender from current config.
	newSender := func(roomID int64) *danmaku.BilibiliSender {
		c := hotCfg.Get()
		s := danmaku.NewBilibiliSender(roomID, c.Bilibili.SESSDATA, c.Bilibili.BiliJCT, c.Bilibili.UID)
		if c.Bilibili.DanmakuMax > 0 {
			s.MaxLength = c.Bilibili.DanmakuMax
		}
		return s
	}

	for _, sc := range streamMap {
		rc.Register(sc.RoomID, sc.Name)
		rc.SetSender(sc.RoomID, newSender(sc.RoomID))
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
		for _, room := range rc.GetAll() {
			if sender := rc.GetSender(room.RoomID); sender != nil {
				sender.SetAccounts(accounts)
			}
		}
		slog.Info("synced bili accounts to senders", "count", len(accounts))
	}

	webServer.Start()
	syncAccountsToSenders() // initial sync so web UI shows accounts before any stream goes live

	// Monitor live status
	mon := stream.NewMonitor(stream.WithMonitorInterval(30 * time.Second))
	var monEvents <-chan stream.RoomEvent

	// applyStreamChanges diffs streamMap against freshly-merged streams,
	// stopping removed streams and registering added ones.
	applyStreamChanges := func() {
		newStreamMap := mergeStreams()

		mu.Lock()
		var removedIDs []int64
		for id := range streamMap {
			if _, exists := newStreamMap[id]; !exists {
				removedIDs = append(removedIDs, id)
				if as, running := active[id]; running {
					slog.Info("stopping removed stream", "room", id)
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
				rc.SetSender(id, newSender(id))
			}
		}
		streamMap = newStreamMap
		mu.Unlock()

		for _, id := range removedIDs {
			mon.RemoveRoom(id)
		}
		for _, id := range addedIDs {
			mon.AddRoom(id)
		}
		slog.Info("streams updated", "total", len(newStreamMap), "added", len(addedIDs), "removed", len(removedIDs))
	}

	// Register callbacks
	webServer.OnAccountChange(syncAccountsToSenders)
	webServer.OnStreamChange(applyStreamChanges)

	hotCfg.OnReload(func(newCfg *config.Config) {
		if newCfg.Auth.Username != "" && newCfg.Auth.Password != "" {
			if err := authStore.EnsureAdmin(newCfg.Auth.Username, newCfg.Auth.Password); err != nil {
				slog.Error("ensure admin on reload", "err", err)
			}
		}
		applyStreamChanges()
		syncAccountsToSenders()
	})
	hotCfg.Watch()

	// Start monitor
	roomIDs := make([]int64, 0, len(streamMap))
	for id := range streamMap {
		roomIDs = append(roomIDs, id)
	}
	monEvents, err = mon.Watch(ctx, roomIDs)
	if err != nil {
		return fmt.Errorf("start monitor: %w", err)
	}

	// Event handler
	go func() {
		for ev := range monEvents {
			mu.Lock()
			rc.SetLive(ev.RoomID, ev.Live)

			if ev.Live {
				if _, running := active[ev.RoomID]; running {
					mu.Unlock()
					continue
				}

				sc, ok := streamMap[ev.RoomID]
				if !ok {
					mu.Unlock()
					slog.Warn("live event for unknown room", "room", ev.RoomID)
					continue
				}
				streamCtx, streamCancel := context.WithCancel(ctx)

				slog.Info("room went live, starting pipeline",
					"name", sc.Name,
					"room", ev.RoomID,
					"title", ev.Title,
				)

				active[ev.RoomID] = &activeStream{cancel: streamCancel}
				mu.Unlock()

				go func(sc config.StreamConfig, streamCtx context.Context, streamCancel context.CancelFunc) {
					if err := runStream(streamCtx, sc, translator, pool, rc, newSender, syncAccountsToSenders, transcriptBaseDir); err != nil {
						slog.Error("stream ended", "name", sc.Name, "err", err)
					}
					streamCancel()
					mu.Lock()
					delete(active, sc.RoomID)
					mu.Unlock()
				}(sc, streamCtx, streamCancel)
			} else {
				if as, running := active[ev.RoomID]; running {
					slog.Info("room went offline, stopping", "room", ev.RoomID)
					as.cancel()
					delete(active, ev.RoomID)
				}
				mu.Unlock()
			}
		}
	}()

	webURL := fmt.Sprintf("http://localhost:%d", webPort)
	slog.Info("livesub started", "streams", len(streamMap), "rooms", roomIDs, "web", webURL)

	openBrowser(webURL)

	<-ctx.Done()
	return ctx.Err()
}

func runStream(ctx context.Context, sc config.StreamConfig, translator *translate.GeminiTranslator, pool *translatePool, rc *web.RoomControl, newSender func(int64) *danmaku.BilibiliSender, syncAccounts func(), transcriptBaseDir string) error {
	// 1. Get live stream URL
	streamURL, err := stream.GetStreamURL(ctx, sc.RoomID)
	if err != nil {
		return fmt.Errorf("get stream url: %w", err)
	}
	slog.Info("got stream URL", "name", sc.Name, "room", sc.RoomID)

	// 2. Audio capture via ffmpeg
	audioReader, err := stream.CaptureAudio(ctx, streamURL, nil)
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

	// 4. Reuse existing sender (created at startup/stream-add, accounts already synced)
	sender := rc.GetSender(sc.RoomID)
	if sender == nil {
		sender = newSender(sc.RoomID)
		rc.SetSender(sc.RoomID, sender)
		syncAccounts()
	}

	// 5. Transcript logger
	tlog, err := transcript.NewLogger(transcriptBaseDir, sc.RoomID, sc.Name)
	if err != nil {
		slog.Warn("transcript logger failed, continuing without", "err", err)
	} else {
		defer tlog.Close()
		slog.Info("transcript logging", "path", tlog.Path())
	}

	// Pipeline: STT → Translate → Send
	pauseReader := &pausableReader{inner: audioReader, isPaused: func() bool {
		return rc.IsPaused(sc.RoomID)
	}}

	resultsCh := make(chan stt.StreamResult, 50)

	// STT reader goroutine with exponential backoff on reconnect
	go func() {
		defer close(resultsCh)
		backoff := time.Second
		const maxBackoff = 30 * time.Second

		for {
			if ctx.Err() != nil {
				return
			}
			if err := sttClient.Stream(ctx, pauseReader, resultsCh); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("STT stream ended, reconnecting...", "name", sc.Name, "err", err, "backoff", backoff)
				select {
				case <-time.After(backoff):
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
				// Increase backoff, reset on successful stream
				backoff = min(backoff*2, maxBackoff)
			} else {
				backoff = time.Second // reset on clean exit
			}
		}
	}()

	targetLang := sc.TargetLang

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
				if tlog != nil {
					tlog.Write(entry.source, entry.text)
				}
				if rc.IsPaused(sc.RoomID) {
					slog.Info("paused, dropping", "name", sc.Name, "text", entry.text)
					continue
				}
				slog.Info("sending", "name", sc.Name, "seq", nextSeq-1, "text", entry.text)
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

		rc.SetLastText(sc.RoomID, result.Text)

		if rc.IsPaused(sc.RoomID) {
			continue
		}

		slog.Info("dispatch", "name", sc.Name,
			"conf", result.Confidence, "len", len([]rune(result.Text)),
			"text", result.Text)

		direct := isTargetLang(result.Language, targetLang)
		if direct {
			slog.Info("direct", "name", sc.Name, "text", result.Text, "lang", result.Language)
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
	source string
}

type translateJob struct {
	seq    int
	text   string
	lang   string
	name   string
	direct bool
	doneCh chan<- translateResult
	source string
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
					slog.Info("translated", "worker", id, "name", job.name, "src", job.text, "dst", translated)
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

// pausableReader wraps a PCM reader and discards audio when paused,
// preventing audio from being sent to STT (saves API cost).
// The underlying reader (ffmpeg) keeps running to maintain the stream.
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
