# LiveSub 🎙️

Real-time live stream translator — captures audio from browser windows, transcribes with Google STT, translates with Gemini, and sends translated danmaku to Bilibili.

## Features

- **Auto browser management** — Opens Chromium per room, auto-detects PipeWire audio node
- **Multi-stream** — Translate N live rooms simultaneously (goroutines)
- **Live detection** — Monitors Bilibili room status, auto-starts/stops when streamer goes live/offline
- **Real-time STT** — Google Cloud Speech-to-Text streaming with auto-reconnect
- **AI translation** — Gemini Flash for fast, context-aware translation
- **Danmaku output** — Sends translated text as Bilibili live chat messages
- **Language detection** — Skips translation if speech is already in target language

## Architecture

```
                        ┌─── Stream 1 (goroutine) ───┐
                        │                             │
  Bilibili Room ──→ Chromium ──→ PipeWire ──→ Google STT ──→ Gemini ──→ Danmaku API
                        │                             │
                        └─────────────────────────────┘
                              × N rooms (concurrent)
```

**Key design:**
- Each room gets an isolated Chromium instance (`--app` mode, separate user-data-dir)
- PipeWire node auto-discovered by matching browser PID via `pw-dump`
- No manual `pw_node` configuration needed

## Prerequisites

- Linux with PipeWire (e.g. Ubuntu 22.04+)
- Chromium or Google Chrome
- `pw-record` and `pw-dump` (from `pipewire` package)
- Google Cloud service account with Speech-to-Text API enabled
- Gemini API key

## Config

```yaml
google:
  credentials: "path/to/service-account.json"

gemini:
  api_key: "your-gemini-api-key"
  model: "gemini-3-flash-preview"
  target_lang: "zh-CN"

bilibili:
  sessdata: "your-sessdata-cookie"
  bili_jct: "your-csrf-token"
  uid: 12345678

streams:
  - name: "VTuber A"
    room_id: 12345
    source_lang: "ja-JP"
  - name: "Streamer B"
    room_id: 67890
    source_lang: "en-US"
    alt_langs: ["ja-JP"]       # additional languages to detect
    target_lang: "en-US"       # per-stream override
```

## Usage

```bash
# Build
go build -o livesub ./cmd/livesub

# List PipeWire audio sources (for debugging)
livesub sources

# Start monitoring & translating
livesub run config.yaml
```

LiveSub will:
1. Poll configured rooms for live status (every 30s)
2. When a room goes live → open browser → detect audio node → start STT → translate → send danmaku
3. When a room goes offline → stop pipeline → close browser

## Project Structure

```
cmd/livesub/         CLI entrypoint
internal/
  audio/             PipeWire capture + browser management
  config/            YAML config loader
  danmaku/           Bilibili danmaku sender
  monitor/           Bilibili live status monitor
  stt/               Google STT streaming client
  translate/         Gemini translation client
```

## Cost

~$2/hr/stream (mostly Google STT). Gemini Flash translation is negligible.

## License

MIT
