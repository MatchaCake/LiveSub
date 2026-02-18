# LiveSub 🎙️

Real-time live stream translator — captures audio, transcribes with Google STT, translates with Gemini, and sends translated danmaku to Bilibili.

## Features

- **Auto stream capture** — Fetches stream URL directly via Bilibili API, decoded by ffmpeg
- **Multi-stream** — Translate N live rooms simultaneously (goroutines)
- **Live detection** — Monitors Bilibili room status (30s polling), auto-starts/stops pipelines
- **Real-time STT** — Google Cloud Speech-to-Text streaming with auto-reconnect on 305s limit
- **AI translation** — Gemini Flash for fast, context-aware translation
- **Shared worker pool** — N×3 translation workers across all streams, parallel translate with ordered send
- **Singing detection** — FFT spectral analysis + text length heuristic to skip BGM/lyrics
- **Danmaku output** — Bilibili live chat with auto-split for long messages, rejected message logging
- **Language detection** — Skips translation if speech is already in target language

## Architecture

```
                    ┌─── Stream Pipeline (per room) ──────────────────────┐
                    │                                                      │
  Bilibili API ──→ ffmpeg (PCM) ──→ AnalyzingReader ──→ Google STT        │
                                        │                   │              │
                                   MusicDetector       resultsCh          │
                                   (FFT/200ms)             │              │
                                        │          ┌───────▼────────┐     │
                                        └──────────│ Singing Filter │     │
                                                   └───────┬────────┘     │
                                                           │              │
                                              Shared Translation Pool     │
                                              (N×3 Gemini workers)        │
                                                           │              │
                                                   Ordered Sender ──→ Danmaku API
                                                   (per-stream seq)       │
                    └──────────────────────────────────────────────────────┘
```

**Key design:**
- Stream URL fetched directly from Bilibili API (no browser needed)
- ffmpeg decodes FLV→PCM s16le 16kHz mono
- `AnalyzingReader` taps PCM for FFT music detection while passing through to STT
- Translation workers shared across all streams for load balancing (peak shaving)
- Each stream maintains its own ordered sender to preserve subtitle sequence

## Singing Detection

3 features via FFT spectral analysis (every 200ms):
1. **Low frequency energy ratio** (40%) — BGM has bass/drums in 0-300Hz
2. **Spectral flatness** (35%) — Music spreads evenly across frequencies
3. **Energy spread** (25%) — Music covers full spectrum beyond voice band

Fallback: consecutive long text (>50 chars) detected as lyrics.

## Prerequisites

- Linux with ffmpeg
- Google Cloud service account with Speech-to-Text API enabled
- Gemini API key
- Bilibili account cookies (SESSDATA + bili_jct)

## Config

```yaml
google:
  credentials: "configs/google-credentials.json"

gemini:
  api_key: "your-gemini-api-key"
  model: "gemini-2.0-flash"
  target_lang: "zh-CN"

bilibili:
  sessdata: "your-sessdata-cookie"
  bili_jct: "your-csrf-token"
  danmaku_max: 30              # max chars per danmaku (20=default, 30=UL20+)

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

# Start monitoring & translating
livesub run configs/config.yaml
```

LiveSub will:
1. Poll configured rooms for live status (every 30s)
2. When a room goes live → fetch stream URL → ffmpeg capture → STT → translate → danmaku
3. When a room goes offline → stop pipeline → wait for next live

### Systemd

```bash
sudo cp livesub.service /etc/systemd/system/
sudo systemctl enable --now livesub
```

## Project Structure

```
cmd/livesub/           CLI entrypoint + pipeline orchestration
internal/
  audio/
    capture.go         ffmpeg PCM capture from stream URL
    analyzer.go        FFT-based music detection (Cooley-Tukey radix-2)
    tee_reader.go      Transparent PCM tap for analysis
    stream_url.go      Bilibili stream URL fetcher
  config/              YAML config loader with defaults
  danmaku/             Bilibili danmaku sender (rate-limited, auto-split)
  monitor/             Bilibili live status poller
  stt/                 Google Cloud STT streaming client
  translate/           Gemini translation client
```

## Cost

~$2/hr/stream (mostly Google STT). Gemini Flash translation is negligible.

## License

MIT
