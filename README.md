# LiveSub 🎙️

Real-time live stream translator — captures audio, transcribes with Google STT, translates with Gemini, and sends translated danmaku to Bilibili.

## Features

- **Multi-stream** — Translate N live rooms simultaneously with shared worker pool
- **Live detection** — Auto-starts/stops translation when streamers go live (30s polling)
- **Real-time STT** — Google Cloud Speech-to-Text streaming with auto-reconnect & exponential backoff
- **AI translation** — Gemini Flash for fast, context-aware translation
- **Multi-account danmaku** — Multiple Bilibili accounts for sending, switch per-room
- **Web control panel** — Pause/resume translation, manage accounts, download transcripts
- **User management** — SQLite-backed auth with admin/user roles, per-room permissions
- **QR code login** — Add Bilibili accounts by scanning QR code in the web UI
- **Stream management** — Add/remove streams from web UI (auto-extract room ID from URL)
- **Transcript logging** — CSV logs per session (time, source text, translation) with download
- **Audit log** — Track all user actions (login, toggle, account switch, admin operations)
- **Hot reload** — Config changes apply without restart (streams, accounts, auth)
- **i18n** — Web UI supports Chinese, English, Japanese with one-click language switch
- **Language detection** — Skips translation if speech is already in target language

## Architecture

```
                    ┌─── Stream Pipeline (per room) ──────────────────────┐
                    │                                                      │
  Bilibili API ──→ ffmpeg (PCM) ──→ Google STT                             │
                                                       │                  │
                                                  resultsCh               │
                                              Shared Translation Pool     │
                                              (N×3 Gemini workers)        │
                                                           │              │
                                              ┌────────────┤              │
                                              ▼            ▼              │
                                        Transcript    Ordered Sender      │
                                        (CSV log)    ──→ Danmaku API      │
                    └──────────────────────────────────────────────────────┘

  Web Control Panel (:8899)
  ├── 🎙️ Room cards (live status, pause/resume, account switch)
  ├── 📄 Transcript download (per-user permission)
  ├── ⚙️ Admin panel
  │   ├── 📺 Stream management (add/delete rooms)
  │   ├── 🎮 Bilibili accounts (QR login, danmaku_max)
  │   ├── 👥 User management (roles, room/account assignment)
  │   └── 📋 Audit log
  └── 🔐 SQLite auth (bcrypt, sessions)
```

## Prerequisites

- Linux with ffmpeg
- Google Cloud service account with Speech-to-Text API enabled
- Gemini API key
- Bilibili account cookies (SESSDATA + bili_jct) — or add via web UI QR login

## Config

```yaml
auth:
  username: "admin"        # Web UI admin account
  password: "your-password"

google:
  credentials: "google-credentials.json"

gemini:
  api_key: "your-gemini-api-key"
  model: "gemini-2.0-flash"
  target_lang: "zh-CN"

bilibili:
  sessdata: "your-sessdata"    # Fallback default account
  bili_jct: "your-csrf-token"
  danmaku_max: 30              # 20=default, 30=UL20+

# Config streams (can also add via web UI)
streams:
  - name: "VTuber A"
    room_id: 12345
    source_lang: "ja-JP"
  - name: "Streamer B"
    room_id: 67890
    source_lang: "en-US"

web_port: 8899  # optional, default 8899
```

Additional Bilibili accounts can be added via the web UI (📱 QR code login) instead of the config file.

Streams can also be added/removed from the admin panel — just paste the Bilibili live URL.

## Usage

```bash
# Build
go build -o livesub ./cmd/livesub

# Start
livesub run configs/config.yaml
```

Open `http://localhost:8899` for the control panel.

### Docker

```bash
# Build image
docker build -t livesub .

# Run (mount your configs directory)
docker run -d -p 8899:8899 -v /path/to/configs:/app/configs livesub

# Custom config path
docker run -d -p 8899:8899 -v /my/config.yaml:/app/my.yaml livesub my.yaml
```

### Systemd

```bash
sudo cp livesub.service /etc/systemd/system/
sudo systemctl enable --now livesub
```

## Web UI

### Control Panel
- View all rooms with live status
- Pause/resume translation per room
- Switch danmaku account per room
- Download transcript CSVs

### Admin Panel (`/admin`)
- **📺 Stream management** — Add rooms by URL or room number, delete any stream
- **🎮 B站账号** — QR code login to add accounts, set per-account danmaku length limit
- **👥 User management** — Create users, assign rooms & accounts, role-based access
- **📋 Audit log** — View all user actions with timestamps and IPs

### Permissions
| Role | Rooms | Accounts | Transcripts | Admin Panel |
|------|-------|----------|-------------|-------------|
| Admin | All | All | All | ✅ |
| User | Assigned only | Assigned only | Assigned rooms | ❌ |

## Transcripts

Each live session generates a CSV file:
```
transcripts/<room_id>_<name>_<YYYYMMDD>_<HHMMSS>.csv
```

Format (UTF-8 with BOM for Excel compatibility):
```csv
时间,原文,翻译
14:30:05,こんにちは,大家好
14:30:12,今日は天気がいいですね,今天天气真好呢
```

Transcripts are recorded continuously even when danmaku sending is paused.

## Data Storage

```
configs/
├── config.yaml          # Main configuration
├── google-credentials.json
├── users.db             # SQLite (users, accounts, streams, audit log)
└── transcripts/         # CSV transcript files
```

## Project Structure

```
cmd/livesub/             CLI + pipeline orchestration
internal/
  auth/
    store.go             SQLite user/session management
    bilibili.go          QR login + account management
    streams.go           Stream DB management
  config/
    config.go            YAML config with defaults
    watcher.go           fsnotify hot reload
  danmaku/
    bilibili.go          Multi-account sender (wraps bilibili_dm_lib)
  stt/
    google.go            Google STT streaming (auto-reconnect, backoff)
  transcript/
    logger.go            CSV transcript writer
  translate/
    gemini.go            Gemini translation client
  web/
    server.go            HTTP handlers, auth, room control
    pages.go             HTML templates (login, control panel, admin)
    i18n.go              Client-side i18n (zh/en/ja)
Dockerfile               Multi-stage build
```

### External Libraries
- [bilibili_dm_lib](https://github.com/MatchaCake/bilibili_dm_lib) — Danmaku sending
- [bilibili_stream_lib](https://github.com/MatchaCake/bilibili_stream_lib) — Room monitoring + stream capture

## Cost

~$2/hr/stream (mostly Google STT). Gemini Flash translation is negligible.

## License

MIT
