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

## Translation model chain (2026-10-14)

Current: **`gemini-3.8-flash`** → fallback `gemini-3.7-flash` → `gemini-3.6-flash`,
set in server-side `configs/config.yaml`:

```yaml
translation:
    model: gemini-3.8-flash
    fallback_models: [gemini-3.7-flash, gemini-3.6-flash]
```

`internal/translate` tries the chain in order per line. Any failure (error, 10s
per-attempt timeout, 429/503, 404 unknown model) puts that model on a 30s cooldown
and the next one is tried; cooling models are moved to the back, not removed.
Rejected output (see "Translation prompt & output checks") also falls through to the
next model. Every call sends
`thinkingBudget: 0` (a model that rejects it is remembered and called without it).
Measured 2026-10-14 on the line below, thinking off: 3.8 1.64s / 3.7 1.95s /
3.6 1.41s, all render 「少し」 as "差一点". With thinking on (SDK default) the same
models take 6–19s — never deploy a 3.x model without `thinkingBudget: 0`.
The model in use is logged at Info as `translate model active` whenever it changes.
The translator is built at startup: model changes need a service restart.

## Translation prompt & output checks (2026-11-11)

The prompt lives in `internal/translate/prompt.go` (hard-coded: changing it needs a
deploy, it is not hot-reloadable). For Chinese targets it forbids romanization:
proper nouns use their Chinese name, kanji names keep their characters (平成 → 平成,
聖闘士星矢 → 圣斗士星矢), katakana is translated by meaning, and a name with no
Chinese form stays in the source script. The 2026-02 rule "output proper nouns as
romaji" is gone for Chinese — it put Heisei / Saint Seiya / Itoman / Danchou into
10–20% of all subtitle lines on every stream since February (not a model regression:
3.8/3.7/3.6 all obeyed it). Non-Chinese targets keep the old rule.

Measured 2026-11-11 on 48 real lines from that night (thinking off, same key):
lines containing Latin words absent from the source — old prompt 3.8: 32/48; new
prompt 3.8: 4/48, 3.7: 2/48, 3.6: 2/48 (what remains is English like "Fire Bomber").
Latency p50/p90: 3.8 old 2.14/2.98s → new 2.06/2.73s; 3.7 new 2.00/2.70s; 3.6 new
1.17/1.53s. 3.6 is the fastest but the least faithful (drops or rewrites names,
invents words), so the order stays 3.8 → 3.7 → 3.6.

`internal/translate/quality.go` rejects a reply and tries the next model when it is
(a) still in the source language or pure Latin for a non-Latin source, (b) Chinese
containing a Hepburn-romanized Japanese word (`isHepburn`; English titles such as
"SLAM DUNK" are not valid Hepburn and pass), or (c) model commentary — longer than
3×source+20 runes (2026-11-07: 「櫓が？」 came back as 700 runes of "Depending on
context… Let's choose…" and went out as ~40 danmaku; the rule has zero false
positives on 63k historical lines). If every model is rejected the first (a)/(b)
answer is still sent; commentary never is. Logged as `translation rejected, trying
next model reason=…`.

## Danmaku splitting

`splitWithWrap` never cuts inside a run of Latin letters / digits / ASCII punctuation
(`Heisei`, `2.5`, `1,000`, `21:30`, `OK!!`): the whole token moves to the next danmaku.
Only a token longer than one danmaku is cut.

## Outputs with dead accounts (2026-11-11)

An account is dead when it is absent from the bot pool or blanked (cookie expired).
`controller.AccountHealth` is the single definition; `sendMessage` rotates over the
usable accounts only, and a `-101` hands the chunk to the next account. An output
with no usable account sends nothing: it logs ONE error when that starts (and one
info when it recovers) instead of a `bot not found` WARN per line, and the panel
shows a red 「无可用账号」 badge on the output card, live or offline (`usable_bots` /
`dead_bots` / `no_accounts` in `/api/status`). Partially dead pools show
「N 个账号失效」. Nothing is auto-paused: after a QR re-scan the output resumes by itself.

## STT stream rotation

Google ends a streaming session after ~305s; the agent reconnects immediately. The
`io.EOF` the audio sender gets at that moment is expected (Debug). What the cut-off
used to lose was the utterance in progress, which exists only as interim results:
`stt.receive` now emits that last interim hypothesis as a final result when the
stream ends with an error (not on audio EOF, not on shutdown).

## Translation model selection (measured 2026-07-26, historical)

Then-current: **`gemini-3.1-flash-lite`**. Live subtitles are a sub-second, non-reasoning
workload — pick on latency first, then on idiom accuracy. Benchmarked on a real
failing line (`守ると見せかけて 私は攻める。あ 待って待って登れなかった。少し。`):

| model | latency | verdict |
|---|---|---|
| `gemini-3.1-flash-lite` | **0.78s** | **current** — fastest, and the only one that renders 「少し」 correctly as "差一点" |
| `gemini-3.5-flash-lite` | 0.80s | ok, but mistranslates the trailing 「少し」 as "稍微等下" |
| `gemini-3.5-flash` | 0.88s | usable **only with `thinkingConfig.thinkingBudget = 0`**; same 「少し」 miss |
| `gemini-3.6-flash` | 2.35s | rejected `thinkingBudget: 0` (HTTP 400) at the time; accepts it as of 2026-10-14 |
| `gemini-2.5-flash-lite` | — | previous default; retired after the 2026-07-26 degeneration incident |

Gotcha that makes a good model look broken: 3.x are reasoning models. With a small
`maxOutputTokens` (e.g. 200) the thinking budget eats the whole allowance and you get
`finishReason: MAX_TOKENS` with a truncated answer or a leaked draft
("Drafting translations (aiming for…"). That is a *budget* symptom, not model quality —
set `thinkingBudget: 0` (where supported) before judging a model.

**Never trust raw model output on the wire.** Any model can fall into a token-repetition
loop on low-confidence STT input; `internal/controller` drops degenerate output before
sending (see `degenerate_test.go` for the incident payload).

Meaningless lines are filtered too (`internal/controller/junk.go`, 2026-10-22): before
translation the agent drops empty STT results, pure punctuation, lone particle fragments
(「た。」「と。」…) and Google STT placeholders (`Unitelligible。` (sic), `Foreign speech`,
which arrive with 0.8–0.9 confidence and, being detected as Mandarin, used to bypass
translation straight into the room). Before queueing, the controller drops
punctuation-only text, bracketed annotations (`（嚼 嚼 / 咬 咬）`, `[音乐]`) and model
refusals; the splitter no longer sends a lone `。` tail. STT confidence is intentionally
not used — real speech often scores 0–0.2.

## Bilibili cookie auto-refresh (2026-10-28)

`internal/auth/refresh.go` runs Bilibili's official web cookie refresh (same flow as
bili-digest `refresh.py`): cookie/info → correspond (RSA-OAEP-SHA256 `refresh_<ms>`)
→ cookie/refresh → nav verify (isLogin + same mid) → write → hot-swap → confirm/refresh.
- `refresh_token` comes from the QR poll response and lives in `bili_accounts.refresh_token`
  in `configs/users.db` next to the cookies (plain SQLite; the store chmods
  users.db/-wal/-shm to 0600 on open). Accounts added before this have no token:
  re-scan once to enable auto-refresh (panel column 自动续期 shows 缺 refresh_token).
- The old row is untouched until the new cookie passed nav; the write is a CAS on the
  old SESSDATA. `OnPersisted` hot-swaps the bot pool and restarts danmaku listeners that
  used the account *before* confirm invalidates the old cookie. Per-account mutex
  serializes the 6h health check and the admin button (`POST /api/admin/bili-account/refresh?id=&force=1`).
- No notification channel exists: failures are logged at Error with the failed step and
  stored in `refresh_status`/`refresh_error` (shown in the admin panel). Never log cookie
  or token values; `RefreshError` details are step + HTTP status / API code only.

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
