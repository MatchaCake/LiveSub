# livesub

Real-time live stream translation tool. Captures audio from specific windows, transcribes via Google STT, translates via Gemini, and sends translated danmaku/chat messages.

## Features

- 🎙️ Per-window audio capture via PipeWire (not system-wide)
- 🔄 Multi-stream support (translate N streams simultaneously)
- 🗣️ Real-time speech-to-text (Google STT Streaming)
- 🌐 AI translation (Gemini 3 Flash)
- 💬 Auto-send to Bilibili live danmaku
- 📺 YouTube Live Chat support (planned)

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌───────────────┐     ┌──────────────┐
│  PipeWire    │────→│  Google STT  │────→│  Gemini Flash │────→│  Bilibili    │
│  Audio Cap   │     │  Streaming   │     │  Translation  │     │  Danmaku API │
└──────────────┘     └──────────────┘     └───────────────┘     └──────────────┘
       × N streams (goroutines)
```

## Config

```yaml
# config.yaml
google:
  credentials: "path/to/service-account.json"
  stt_language: "ja-JP"        # source language

gemini:
  api_key: "..."
  model: "gemini-3.0-flash"
  target_lang: "zh-CN"         # translate to

bilibili:
  sessdata: "..."
  bili_jct: "..."               # csrf token

streams:
  - name: "VTuber A"
    room_id: 12345
    pw_node: 47                 # PipeWire node ID
    source_lang: "ja-JP"
  - name: "Streamer B"
    room_id: 67890
    pw_node: 52
    source_lang: "en-US"
```

## Usage

```bash
# List available PipeWire audio sources
livesub sources

# Start translating
livesub run --config config.yaml

# Single stream quick start
livesub run --room 12345 --node 47 --lang ja-JP
```
