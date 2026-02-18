# CLAUDE.md — LiveSub

## Overview
Real-time Bilibili live stream translator. Captures audio via ffmpeg, transcribes with Google Cloud STT, translates with Gemini, sends translated danmaku to Bilibili. Web control panel with multi-user auth.

## Module
`github.com/christian-lee/livesub`

## Architecture
```
ffmpeg (PCM) → Google STT → Shared Translation Pool (Gemini) → Ordered Sender → Bilibili Danmaku API
                                                                      ↓
                                                              Transcript CSV
```

### Key Packages
- `cmd/livesub/main.go` — Pipeline orchestration, hot reload, stream lifecycle
- `internal/audio/` — ffmpeg capture (`capture.go`), Bilibili stream URL fetch (`bilibili.go`)
- `internal/stt/` — Google Cloud STT streaming client with auto-reconnect
- `internal/translate/` — Gemini translation client
- `internal/danmaku/` — Multi-account danmaku sender (uses `github.com/MatchaCake/bilibili_dm_lib`)
- `internal/monitor/` — Bilibili room live/offline poller
- `internal/web/` — HTTP server, auth, room control, admin panel
- `internal/auth/` — SQLite user/session/account/stream management, Bilibili QR login
- `internal/transcript/` — CSV transcript logging
- `internal/config/` — YAML config with fsnotify hot reload

### Concurrency Model
- One goroutine per active stream pipeline (STT + translation + sending)
- Shared translation worker pool (N×3 workers) across all streams
- Per-stream ordered sender (sequence numbers) preserves subtitle order
- `sync.Mutex` protects streamMap and active streams map
- `sync.RWMutex` in RoomControl, BilibiliMonitor, BilibiliSender
- SQLite with `MaxOpenConns(1)` + WAL mode + busy_timeout

### Web UI
- Login page, control panel (room cards), admin panel
- i18n: zh/en/ja with localStorage persistence
- Room cards: live status, pause/resume, account selector, transcripts
- Admin: stream management, user management, Bilibili QR login, audit log

## Config
`configs/config.yaml` — Google STT credentials, Gemini API key, Bilibili cookies, streams, web port, auth

## Dependencies
- `github.com/MatchaCake/bilibili_dm_lib` — Danmaku sending
- `cloud.google.com/go/speech` — Google STT
- `google.golang.org/genai` — Gemini
- `github.com/gorilla/websocket` — (transitive via dm_lib)
- `github.com/mattn/go-sqlite3` — SQLite
- `github.com/fsnotify/fsnotify` — Config hot reload
- ffmpeg (system binary)

## Build & Deploy
```bash
go build -o livesub ./cmd/livesub
sudo systemctl restart livesub
```

## Data
- `configs/users.db` — SQLite (users, sessions, accounts, streams, audit)
- `configs/transcripts/` — CSV per stream session

## Git
- Author: MatchaCake <MatchaCake@users.noreply.github.com>
- No Co-Authored-By lines
