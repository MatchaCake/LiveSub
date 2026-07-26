# CLAUDE.md — LiveSub

## Overview
Real-time Bilibili live stream translator. One Docker container per streamer. Captures audio via ffmpeg, transcribes with Google Cloud STT, translates to multiple languages with Gemini, sends translated danmaku to Bilibili via bot pool. Web control panel with multi-user auth.

## Module
`github.com/christian-lee/livesub`

## Architecture
```
Agent (per streamer):
  ffmpeg (PCM) → Google STT → Translation Fan-out (Gemini) → Controller

Controller:
  Receives multi-lang Translation → routes to Outputs → Bot Pool

Bot Pool:
  BilibiliBot (danmaku sender) — future: YouTubeBot, TwitchBot
                              ↓
                       Transcript CSV
```

### 3-Layer Design
1. **Agent** — Captures audio stream, runs STT, fans out to multi-language translation, submits to Controller
2. **Controller** — Receives translations, routes to correct bots per output config, manages per-output ordering & pause
3. **Bot Pool** — Platform-specific senders (BilibiliBot), pooled and reusable

### Key Packages
- `cmd/livesub/main.go` — Pipeline orchestration, hot reload, stream lifecycle
- `internal/agent/` — Agent pipeline (STT → translate → submit)
- `internal/bot/` — Bot interface, BilibiliBot, Pool (uses `github.com/MatchaCake/bilibili_dm_lib`)
- `internal/controller/` — Translation routing, ordered sender, pause control
- `internal/audio/` — ffmpeg capture (`capture.go`), Bilibili stream URL fetch (`bilibili.go`)
- `internal/stt/` — Google Cloud STT streaming client with auto-reconnect
- `internal/translate/` — Gemini translation client (target lang per-call)
- `internal/web/` — HTTP server, auth, control panel, admin panel
- `internal/auth/` — SQLite user/session/account management, Bilibili QR login
- `internal/transcript/` — Multi-language CSV transcript logging
- `internal/config/` — YAML config with fsnotify hot reload

### Concurrency Model
- One goroutine for Agent pipeline (STT + translation fan-out)
- Controller runs ordered sender per output
- Translation fan-out: parallel goroutines translate to N languages from single STT result
- Per-output ordered sender (sequence numbers + pending map) preserves subtitle order
- `sync.Mutex` protects active stream state
- `sync.RWMutex` in Controller, Bot Pool
- SQLite with `MaxOpenConns(1)` + WAL mode + busy_timeout

### Web UI
- Login page, control panel (streamer card with output cards), admin panel
- i18n: zh/en/ja with localStorage persistence
- Output cards: platform, target_lang, bot assignment, last text, pause/resume
- Admin: user management, Bilibili QR login, audit log

## Config
`configs/config.yaml` — Per-streamer config: STT credentials, Gemini API key, outputs (platform + lang + bot), bots, web port, auth

## Translation model selection (measured 2026-07-26)

Current: **`gemini-3.1-flash-lite`**. Live subtitles are a sub-second, non-reasoning
workload — pick on latency first, then on idiom accuracy. Benchmarked on a real
failing line (`守ると見せかけて 私は攻める。あ 待って待って登れなかった。少し。`):

| model | latency | verdict |
|---|---|---|
| `gemini-3.1-flash-lite` | **0.78s** | **current** — fastest, and the only one that renders 「少し」 correctly as "差一点" |
| `gemini-3.5-flash-lite` | 0.80s | ok, but mistranslates the trailing 「少し」 as "稍微等下" |
| `gemini-3.5-flash` | 0.88s | usable **only with `thinkingConfig.thinkingBudget = 0`**; same 「少し」 miss |
| `gemini-3.6-flash` | 2.35s | **unusable** — rejects `thinkingBudget: 0` (HTTP 400), so it always burns ~190 thinking tokens |
| `gemini-2.5-flash-lite` | — | previous default; retired after the 2026-07-26 degeneration incident |

Gotcha that makes a good model look broken: 3.x are reasoning models. With a small
`maxOutputTokens` (e.g. 200) the thinking budget eats the whole allowance and you get
`finishReason: MAX_TOKENS` with a truncated answer or a leaked draft
("Drafting translations (aiming for…"). That is a *budget* symptom, not model quality —
set `thinkingBudget: 0` (where supported) before judging a model.

**Never trust raw model output on the wire.** Any model can fall into a token-repetition
loop on low-confidence STT input; `internal/controller` drops degenerate output before
sending (see `degenerate_test.go` for the incident payload).

## Dependencies
- `github.com/MatchaCake/bilibili_dm_lib` — Danmaku sending
- `github.com/MatchaCake/bilibili_stream_lib` — Stream monitoring & audio capture
- `cloud.google.com/go/speech` — Google STT
- `google.golang.org/genai` — Gemini
- `github.com/mattn/go-sqlite3` — SQLite
- `github.com/fsnotify/fsnotify` — Config hot reload
- ffmpeg (system binary)

## Build & Deploy
```bash
go build -o livesub ./cmd/livesub
# Docker (one container per streamer):
docker build -t livesub .
docker run -v ./configs:/app/configs livesub
```

## Data
- `configs/users.db` — SQLite (users, sessions, accounts, audit)
- `configs/transcripts/` — CSV per stream session (5 columns: time, source_lang, source, target_lang, translated)

## Git
- Author: MatchaCake <MatchaCake@users.noreply.github.com>
- No Co-Authored-By lines
