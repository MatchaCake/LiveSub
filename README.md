# LiveSub

Real-time live stream translator — captures audio, transcribes with Google STT, translates with Gemini, and sends translated danmaku to Bilibili.

## Features

- **Multi-stream** — Translate N live rooms simultaneously with shared worker pool
- **Multi-output** — Per-streamer outputs: different languages, rooms, bots, prefix/suffix per output
- **Live detection** — Auto-starts/stops translation when streamers go live (30s polling)
- **Real-time STT** — Google Cloud Speech-to-Text streaming with auto-reconnect & exponential backoff
- **AI translation** — Gemini 2.5 Flash-Lite for fast, context-aware translation with language detection
- **Multi-account danmaku** — Bot pool with per-output account assignment and round-robin delivery
- **Danmaku commands** — `/off` `/on` `/list` `/help` commands in live room with UID whitelist
- **Web control panel** — Pause/resume per output, manage accounts, download transcripts
- **Persistent sessions** — Login once, stay logged in for 7 days (survives service restarts)
- **User management** — SQLite-backed auth with admin/user roles, per-room permissions
- **QR code login** — Add Bilibili accounts by scanning QR code in the web UI
- **Stream management** — Add/remove streams and outputs from the admin panel
- **Transcript logging** — CSV logs per session with timeline, source/target language columns
- **Ordered delivery** — Per-output sequence buffering ensures subtitles arrive in order
- **Message splitting** — Long translations split at word boundaries with prefix/suffix on each chunk
- **Sequence emoji** — Number emojis (0️⃣–🔟) prefixed after user prefix for message tracking
- **Audit log** — Track all user actions (login, toggle, account switch, admin operations)
- **Hot reload** — Config changes apply without restart
- **i18n** — Web UI supports Chinese, English, Japanese
- **WBI auth** — Auto wbi signature for Bilibili danmaku WebSocket (bypasses -352 risk control)
- **3s delay queue** — Messages buffer before sending with skip/review in UI

## Architecture

```
  ┌─── Agent (per streamer) ──────────────────────────────────────┐
  │                                                               │
  │  Bilibili API ──→ ffmpeg (PCM) ──→ Google STT                 │
  │                                        │                      │
  │                                   resultsCh                   │
  │                              Translation Pool                 │
  │                              (N×3 Gemini workers)             │
  │                                        │                      │
  │                                   Controller                  │
  │                              ┌─────────┼──────────┐           │
  │                              ▼         ▼          ▼           │
  │                       Transcript   Output A    Output B       │
  │                       (CSV log)   ──→ Bot A   ──→ Bot B       │
  │                                   ──→ Room X  ──→ Room Y      │
  └───────────────────────────────────────────────────────────────┘

  Bot Pool (shared)
  ├── BilibiliBot "account1" (SESSDATA, danmaku_max)
  ├── BilibiliBot "account2"
  └── ...

  Web Control Panel (:8899)
  ├── Room cards (live status, per-output pause/resume)
  ├── Transcript download (per-user permission)
  └── Admin panel
      ├── Stream management (add/delete rooms + outputs)
      ├── Bilibili accounts (QR login, danmaku_max)
      ├── User management (roles, room/account assignment)
      └── Audit log
```

### 3-Layer Design

1. **Agent** — Captures audio stream, runs STT, fans out to multi-language translation via semaphore-limited worker pool, submits to Controller
2. **Controller** — Receives translations, routes to correct bot per output config, maintains per-output ordered sending with sequence buffer, manages pause state
3. **Bot Pool** — Platform-specific senders (BilibiliBot), pooled and reusable across streamers

## Prerequisites

- Linux with ffmpeg
- Google Cloud service account with Speech-to-Text API enabled
- Gemini API key
- Bilibili account cookies — or add via web UI QR login

## Config

```yaml
stt:
  credentials: "google-credentials.json"   # relative to config dir

translation:
  api_key: "your-gemini-api-key"
  model: "gemini-2.5-flash-lite"

bots:
  - name: "bot1"
    sessdata: "your-sessdata"
    bili_jct: "your-csrf-token"
    danmaku_max: 30                        # 20=default, 30=UL20+

streamers:
  - name: "VTuber A"
    room_id: 12345
    source_lang: "ja-JP"
    alt_langs: ["en-US"]
    outputs:
      - name: "中文翻译"
        target_lang: "zh-CN"
        account: "bot1"                    # bot name from bots[]
        room_id: 0                         # 0 = same room as streamer
        prefix: "【翻译】"
        suffix: ""
      - name: "English"
        target_lang: "en-US"
        account: "bot1"
        room_id: 67890                     # send to a different room
        prefix: "[EN] "

web:
  port: 8899
  auth:
    username: "admin"
    password: "your-password"
```

Additional Bilibili accounts can be added via the web UI (QR code login). Streams and outputs can also be managed from the admin panel.

## Usage

```bash
# Build
go build -o livesub ./cmd/livesub

# Start
./livesub run configs/config.yaml
```

Open `http://localhost:8899` for the control panel.

### Docker

```bash
docker build -t livesub .
docker run -d -p 8899:8899 -v /path/to/configs:/app/configs livesub
```

### Systemd

```bash
sudo cp livesub.service /etc/systemd/system/
sudo systemctl enable --now livesub
```

## Web UI

### Control Panel

- View all rooms with live status
- Pause/resume translation per output
- Switch danmaku account per output
- Download transcript CSVs

### Admin Panel (`/admin`)

- **Stream management** — Add/remove rooms, configure outputs per streamer
- **Bilibili accounts** — QR code login, per-account danmaku length limit
- **User management** — Create users, assign rooms & accounts, role-based access
- **Audit log** — View all user actions with timestamps and IPs

### Permissions

| Role  | Rooms         | Accounts      | Transcripts    | Admin |
|-------|---------------|---------------|----------------|-------|
| Admin | All           | All           | All            | Yes   |
| User  | Assigned only | Assigned only | Assigned rooms | No    |

## Danmaku Commands

Control translation directly from the live room chat. Only whitelisted UIDs can execute commands.

| Command | Alias | Description |
|---------|-------|-------------|
| `/off` | `/pause` `/暂停` | Pause all outputs |
| `/on` | `/resume` `/恢复` | Resume all outputs |
| `/off <name>` | `/pause <name>` `/暂停 <name>` | Pause specific output |
| `/on <name>` | `/resume <name>` `/恢复 <name>` | Resume specific output |
| `/list` | `/列表` | Show outputs with ▶/⏸ status |
| `/help` | `/帮助` | Show command usage |

Configure per-streamer in `config.yaml`:

```yaml
streamers:
  - name: "VTuber A"
    room_id: 12345
    command_uids: [857369]    # Bilibili UIDs allowed to use commands
```

Replies are sent via account pool round-robin for speed and rate-limit avoidance.

## Transcripts

Each live session generates a CSV file:

```
transcripts/<room_id>_<name>_<YYYYMMDD>_<HHMMSS>.csv
```

Format (UTF-8 with BOM for Excel):

```csv
时间,时间轴,原文语言,原文,目标语言,翻译
14:30:05,0:00,ja-jp,こんにちは,zh-CN,大家好
14:30:12,0:07,ja-jp,今日は天気がいいですね,zh-CN,今天天气真好呢
```

Transcripts are recorded continuously even when danmaku sending is paused.

## Data Storage

```
configs/
├── config.yaml              # Main configuration
├── google-credentials.json
├── users.db                 # SQLite (users, accounts, streams, audit log)
└── transcripts/             # CSV transcript files
```

## Project Structure

```
cmd/livesub/             CLI + pipeline orchestration
internal/
  agent/
    agent.go             Agent pipeline (STT → translate → controller)
  bot/
    bot.go               Bot interface (Send, Platform, Name, MaxMessageLen)
    bilibili.go          BilibiliBot (wraps bilibili_dm_lib)
    pool.go              Thread-safe bot registry
  controller/
    controller.go        Translation routing, ordered sender, pause, text splitting
  command/
    handler.go           Danmaku command handler (UID whitelist, /off /on /list /help)
  config/
    config.go            YAML config with defaults + old format migration
    watcher.go           fsnotify hot reload
  stt/
    google.go            Google STT streaming (auto-reconnect, backoff)
  translate/
    gemini.go            Gemini translation client
  transcript/
    logger.go            CSV transcript writer with timeline
  auth/
    store.go             SQLite user/session management
    bilibili.go          QR login + account management
    streams.go           Stream DB management
  web/
    server.go            HTTP handlers, auth middleware, room control
    pages.go             Embedded HTML (login, control panel, admin)
    i18n.go              Client-side i18n (zh/en/ja)
Dockerfile               Multi-stage build (golang → debian-slim + ffmpeg)
```

### Dependencies

- [bilibili_dm_lib](https://github.com/MatchaCake/bilibili_dm_lib) — Danmaku sending + receiving (WBI auth)
- [bilibili_stream_lib](https://github.com/MatchaCake/bilibili_stream_lib) — Room monitoring + stream capture
- [cloud.google.com/go/speech](https://pkg.go.dev/cloud.google.com/go/speech) — Google STT
- [google.golang.org/genai](https://pkg.go.dev/google.golang.org/genai) — Gemini
- [go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite with CGO
- [fsnotify](https://github.com/fsnotify/fsnotify) — Config hot reload

## Cost

~$2/hr/stream (mostly Google STT). Gemini Flash translation is negligible.

## License

MIT
