# LiveSub Project Overview

## Purpose
Real-time Bilibili live stream translator. Captures audio via ffmpeg, transcribes with Google Cloud STT, translates with Gemini, sends translated danmaku to Bilibili.

## Tech Stack
- Go 1.25.7 with CGO (sqlite3)
- Google Cloud Speech-to-Text, Gemini API
- bilibili_dm_lib, bilibili_stream_lib
- SQLite with WAL mode, fsnotify for config hot reload
- Embedded HTML/CSS/JS web UI (no templates, raw strings)

## Key Commands
- Build: `go build -o livesub ./cmd/livesub`
- Vet: `go vet ./...`
- Run: `./livesub run configs/config.yaml`

## Code Style
- log/slog for logging
- sync.Mutex/RWMutex for concurrency
- Chinese comments and audit log entries
- No test files exist
- Git: user.name=MatchaCake, no Co-Authored-By

## Structure
- cmd/livesub/main.go — entry point, pipeline orchestration
- internal/config/ — YAML config with hot reload
- internal/stt/ — Google STT streaming
- internal/translate/ — Gemini translation
- internal/danmaku/ — Multi-account Bilibili sender
- internal/auth/ — SQLite user/session/account/stream mgmt, QR login
- internal/web/ — HTTP server, embedded HTML pages, i18n
- internal/transcript/ — CSV logging
