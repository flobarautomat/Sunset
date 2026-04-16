# Moonrise — Implementation Plan

_Living document — updated as decisions are made and phases are completed._

## 1. Goals & non-goals

**Goals**
- Demonstrate taste in architecture more than feature count.
- Hit all 5 required features (video, AI voice cue, chat, session tracking, dashboard).
- Land all 3 bonuses cleanly: WebSocket dashboard, configurable cue timecodes, unit tests for session recording.
- Produce a README with a decision log that shows judgment.

**Non-goals**
- Auth, accounts, multi-tenant.
- Production hardening (rate limits, retries with backoff, prod TLS, observability stack).
- Anything pretty — utilitarian UI, minimal CSS.

## 2. Architecture at a glance

```
┌────────────────┐           ┌────────────────────────┐           ┌──────────────────┐
│  Viewer (SK)   │  HTTP/WS  │      Go backend        │   HTTP    │  staging.api.    │
│  /watch        │──────────▶│  - REST + SSE + WS     │──────────▶│  sunset.video    │
│  HTMLVideo +   │◀──────────│  - SQLite (modernc)    │           │  (chat + TTS)    │
│  chat + audio  │           │  - cue scheduler       │           └──────────────────┘
└────────────────┘           │  - event bus (in-proc) │
                             └──────────┬─────────────┘
┌────────────────┐                      │  WS broadcast
│ Dashboard (SK) │◀─────────────────────┘
│ /admin         │
└────────────────┘
```

Single Go binary serves the REST API, WebSocket hub, and proxies AI calls. SvelteKit ships two routes (`/watch`, `/admin`) from one app. SQLite via `modernc.org/sqlite` (pure Go, no CGO).

## 3. Tech stack

- **Backend:** Go 1.26, `net/http` + `chi` router, `nhooyr.io/websocket`, `modernc.org/sqlite`, `alfg/mp4` (metadata), `google/uuid`.
- **Frontend:** SvelteKit 2 + Svelte 5, TypeScript, minimal CSS (no Tailwind to keep deps light). Node 22 (required by SvelteKit's supported range).
- **AI:** `staging.api.sunset.video` for chat completions and TTS. Server-side proxy — API key never touches the browser.
- **Build/run:** `./run.sh` starts Go + Vite together, auto-detects Node 22.

## 4. Data model (SQLite)

```sql
-- one row per viewer tab
CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,          -- uuid, server-issued
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  user_agent   TEXT,
  video_id     TEXT NOT NULL
);

-- append-only event log (the source of truth for the dashboard)
CREATE TABLE events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  kind       TEXT NOT NULL,               -- video_play|pause|seek|ended|ai_message|ai_response|cue_played|heartbeat
  at         INTEGER NOT NULL,            -- server ts ms
  video_pos  REAL,                        -- seconds into video, when relevant
  payload    TEXT                         -- JSON blob, kind-specific
);

-- configurable voice cues per video (the bonus)
CREATE TABLE cues (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  video_id   TEXT NOT NULL,
  at_seconds REAL NOT NULL,
  prompt     TEXT NOT NULL,               -- sent to LLM to generate line
  voice_id   TEXT NOT NULL,               -- TTS voice uuid
  enabled    INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX idx_events_session ON events(session_id, at);
CREATE INDEX idx_cues_video ON cues(video_id, at_seconds);
```

Cues are seeded from `cues.json` on startup so they're "configurable without a code change" — editing the JSON (or hitting a tiny admin endpoint) updates them.

## 5. Backend API surface

| Method | Path | Purpose |
|---|---|---|
| `GET`  | `/api/videos` | List available videos (id, title, duration). |
| `GET`  | `/api/videos/:id/stream` | Serve video file with range request support (HTTP 206). |
| `POST` | `/api/sessions` | Create session, return `{session_id}`. |
| `POST` | `/api/sessions/:id/events` | Append one or many events (batched from client). |
| `GET`  | `/api/videos/:id/cues` | Cue list for the viewer to schedule locally. |
| `POST` | `/api/chat` | `{session_id, message}` → streams AI response (SSE). Also records `ai_message` + `ai_response` events. |
| `GET`  | `/api/cue-audio?cue_id=...` | Returns mp3 for a cue. Cached on disk after first generation. |
| `GET`  | `/api/admin/sessions` | Paginated session list with latest event summary. |
| `GET`  | `/api/admin/sessions/:id` | Full event timeline for one session. |
| `GET`  | `/api/admin/films` | Film metadata + aggregate stats + cues per film. |
| `GET`  | `/api/admin/stats` | System health + AI usage stats + uptime + cache. |
| `GET`  | `/api/admin/heatmap` | Event density bucketed by video position. |
| `WS`   | `/ws/admin` | Pushes `session_created`, `event_appended`, `session_idle` messages in real time. |

All AI traffic is server-side. Client sends plain text to `/api/chat`, Go calls `/api/v1/chat/completions`, streams chunks back over SSE, and persists the final transcript.

All video serving is server-side. Videos live in `data/videos/` and are served via `http.ServeContent` which handles `Range`, `If-Modified-Since`, and `Content-Type` headers — matching how a production CDN/origin would behave. The viewer discovers videos via `GET /api/videos` and sets `<video src="/api/videos/:id/stream">`.

## 6. Viewer app (`/watch`)

- **Netflix-style custom player** — full-viewport (`100vw × 100vh`), dark background, video fills via `object-fit: contain`. No native browser controls.
- **Custom controls overlay** — play/pause, mute toggle, seek bar, time display. Auto-hides after 3s of mouse inactivity, cursor hides too.
- **Seek bar with cue markers** — custom seek bar with Netflix-red progress track. Cue positions rendered as yellow dots at `(at_seconds / duration) * 100%`. Hover shows prompt tooltip, click seeks to cue time.
- **Prompt input** — always-visible text input at bottom of viewport for AI chat (wired in Phase 3).
- On mount: `POST /api/sessions` → stash `session_id`, fetch `GET /api/videos/default/cues` → render cue markers.
- Tracker (`$lib/tracker.ts`) attaches to the underlying `<video>` element — custom controls don't interfere.
- **Cue scheduler (client-side, Phase 4):** track `currentTime`, trigger cue when `prev < at <= now`. Trigger = request `/cue-audio?cue_id` and play via `<audio>`, log a `cue_played` event.
- **Chat (Phase 3):** prompt input submits to `/api/chat` with SSE, renders streamed tokens.

## 7. Dashboard (`/admin`)

- Table of sessions: id (short), started, last seen, events count, current video pos, "live" dot.
- Click a session → right pane with event timeline (scrolls, auto-follows).
- Opens a WebSocket to `/ws/admin`; updates list + timeline without refresh.
- No auth, but note it in the README as a deliberate cut.

## 8. Real-time (WebSocket bonus)

- In-process `pubsub` pkg with typed topics: `session.created`, `session.*.event`, `session.*.idle`.
- Every `POST /events` and `POST /chat` publishes to pubsub after committing to SQLite.
- `/ws/admin` subscribes to the firehose; dashboard filters client-side.
- Idle detector: goroutine per-session isn't needed — a single sweep goroutine marks sessions idle when `last_seen_at > 30s` and publishes once.

## 9. Voice cue bonus — "configurable without a code change"

Two layers:
1. `cues.json` seeded into the `cues` table on boot; changing the file and restarting is enough.
2. Tiny admin endpoint `POST /api/admin/cues` (guarded by a shared env-var token, not real auth) so cues can be added at runtime without restart. Mentioned in README as "90% of the way to proper config without scope creep".

Generation pipeline per cue:
- On first play: server calls LLM with the cue's `prompt` → gets a line → calls TTS → caches mp3 on disk keyed by `sha256(prompt + voice_id)`.
- Subsequent plays serve from cache. Makes the experience deterministic and cheap.

## 10. Session recording unit tests (bonus)

Target the pure-Go `recorder` package (no HTTP, no DB dependency — inject an `EventStore` interface):
- `TestRecorder_CreateSession` — returns valid UUID, session persisted with correct fields.
- `TestRecorder_BatchesHeartbeats` — collapses N heartbeats within a 5s window into one.
- `TestRecorder_RejectsUnknownKind` — returns error for invalid event kinds.
- `TestRecorder_ClampsVideoPos` — negative video_pos clamped to 0.
- `TestRecorder_OrderingWithSeek` — seek followed by play emits events in correct logical order.
- `TestRecorder_IdleTransition` — `last_seen_at` updated on event receipt.

Use `testing` + `testify/assert` + `testify/require`. In-memory `fakeStore` for speed. All 6 pass.

## 11. Project layout

```
moonrise/
├── README.md
├── run.sh                        # starts Go + Vite concurrently
├── cmd/server/main.go
├── internal/
│   ├── api/          # chi handlers
│   ├── ai/           # AI client (Sunset proxy + direct Anthropic)
│   ├── tts/          # TTS provider (Sunset API + browser speech fallback)
│   ├── config/       # Config struct, env parsing
│   ├── recorder/     # session + event logic (unit tested)
│   ├── video/        # mp4 metadata + in-memory registry
│   ├── cues/         # cue loader + audio cache
│   ├── pubsub/       # in-proc bus
│   ├── store/        # sqlite queries + migrations
│   └── ws/           # websocket hub
├── data/films/                   # per-film directories
│   └── heat/
│       ├── film.mp4              # ~2GB, gitignored, see README
│       ├── cues.json             # voice cue definitions
│       └── metadata.json         # title, year, director, synopsis
├── web/                          # SvelteKit app
│   ├── src/routes/watch/+page.svelte
│   ├── src/routes/admin/+page.svelte
│   └── src/lib/
```

## 12. Implementation phases

Each phase ends with something runnable. Dependencies flow downward — don't skip ahead.

---

### Phase 0 — Local dev setup ✅

_Goal: all tools installed and verified. You can compile Go and run SvelteKit before writing any project code._

**Status: COMPLETE**

**What was done:**
- Installed Go 1.26.2 via `brew install go`
- Installed Node 22.22.2 via `brew install node@22` (Node 23 is not in SvelteKit's supported range: `^20.19 || ^22.12 || >=24`)
- Bootstrapped Go module with chi, websocket, sqlite, testify deps
- Scaffolded SvelteKit skeleton (TypeScript, minimal template via `npx sv create`)
- Created `.env.example`, `.env`, `.gitignore`, `run.sh`
- `run.sh` auto-detects and uses Node 22 from homebrew keg path
- Configured Vite proxy: `/api` and `/ws` → Go backend on `:8080`

**Decision: Node 22 over Node 23** — SvelteKit's latest `@sveltejs/vite-plugin-svelte@7.0.0` requires `^20.19 || ^22.12 || >=24`. Rather than downgrade the system Node, installed Node 22 alongside it. `run.sh` handles the PATH override.

---

### Phase 1 — Scaffold & plumbing ✅

_Goal: both servers start, talk to each other, and persist data. Nothing visible yet beyond "hello world."_

**Status: COMPLETE**

**What was built:**
- `cmd/server/main.go` — chi router with `/api` route group, graceful shutdown via `signal.NotifyContext`, CORS middleware, chi's `Logger` and `Recoverer`
- `internal/config/config.go` — `Config` struct parsed from env, explicit wiring (no globals). `LoadOrDefault()` for dev (won't fail on missing API key), `Load()` for production (validates key)
- `internal/store/db.go` — opens SQLite with WAL mode, `foreign_keys=ON`, `busy_timeout=5000`. Runs embedded migrations via `go:embed`
- `internal/store/migrations/001_init.sql` — `sessions`, `events`, `cues` tables with indexes
- `web/src/routes/watch/+page.svelte` — stub page, hits `/api/healthz` to confirm proxy works
- `web/src/routes/admin/+page.svelte` — stub dashboard page, same backend check
- `docs/design-decisions.md` — records all architectural choices with reasoning

**Key decisions made (see `docs/design-decisions.md`):**
- Chi over stdlib — route grouping and middleware stack make intent scannable
- Config struct with explicit wiring — no `os.Getenv` scattered through packages
- Embedded SQL migrations — `.sql` files get syntax highlighting, adding a migration is just a new file
- SQLite WAL mode — concurrent reads during writes for dashboard queries
- Dashboard shows message content — more useful for demo, privacy noted as deliberate cut
- All backend routes under `/api` prefix — clean Vite proxy boundary

**Verified:** `go build ./...` clean, `curl localhost:8080/api/healthz` → `ok`, SvelteKit pages render with backend status

---

### Phase 2 — Video playback + session tracking (+ unit tests) ✅

_Goal: a user watches a video, and every play/pause/seek lands in SQLite. Tests prove the recorder logic is correct._

**Status: COMPLETE**

**What was built:**
- `internal/video/registry.go` — scans `data/videos/` at startup, extracts mp4 metadata (duration, resolution, bitrate) via `alfg/mp4`, builds in-memory registry
- `internal/api/videos.go` — `GET /api/videos` (list with metadata) and `GET /api/videos/{id}/stream` (serve via `http.ServeContent` with range requests)
- `internal/recorder/recorder.go` — `Recorder` with `EventStore` interface. Validates event kinds, clamps negative `video_pos` to 0, collapses heartbeats within 5s window. Owns session creation (generates UUID) and event recording.
- `internal/recorder/recorder_test.go` — 6 tests: `CreateSession`, `BatchesHeartbeats`, `RejectsUnknownKind`, `ClampsVideoPos`, `OrderingWithSeek`, `IdleTransition`. All pass with in-memory fake store.
- `internal/store/sessions.go` — SQLite `EventStore` implementation with transaction batching for events. Also exposes `ListSessions` and `ListEvents` for the dashboard (Phase 5).
- `internal/api/sessions.go` — `POST /api/sessions` and `POST /api/sessions/{id}/events`. Thin handlers that decode JSON and delegate to recorder.
- `web/src/lib/tracker.ts` — `createTracker(sessionId, videoEl)`: listens for play/pause/seeking/ended, buffers events, flushes every 2s, heartbeat every 10s. Re-queues on flush failure.
- `web/src/routes/watch/+page.svelte` — video player with `src="/api/videos/default/stream"`, creates session on mount, wires tracker, cleanup on unmount.

**Key decisions (see `docs/design-decisions.md`):**
- MP4 metadata extracted at startup with pure-Go parser — no ffprobe dependency
- Recorder owns sessions + events behind injected interface — API handlers are thin
- Only heartbeats collapsed (5s window); play/pause/seek pass through as-is
- Time-based batching (2s interval) — simple and predictable

**Verified:** `go test ./internal/recorder/... -v` → 6/6 pass, `go build ./...` clean

---

### Phase 2.5 — Netflix-style video player + cue markers ✅

_Goal: replace the bare `<video>` with an immersive full-viewport player with custom controls, cue markers on the seek bar, and a prompt input stub._

**Status: COMPLETE**

**What was built:**
- `web/src/routes/watch/+page.svelte` — complete rewrite. Full-viewport dark player (`100vw × 100vh`), custom controls overlay (play/pause, mute toggle, seek bar, time display) that auto-hides after 3s. Netflix-red seek bar with draggable seeking. Click-to-toggle-play on video, space bar shortcut. Cue markers as yellow dots on the seek bar — hover shows prompt tooltip, click seeks to timestamp.
- `internal/api/cues.go` — `GET /api/videos/{id}/cues` handler returning enabled cues as JSON.
- `internal/store/sessions.go` — added `Cue` type and `ListCues(videoID)` method.
- `cmd/server/main.go` — wired cues handler and route.
- Prompt input bar at bottom of viewport — always visible, stub for Phase 3 chat wiring.

**Key decisions (see `docs/design-decisions.md`):**
- Custom player over native controls — native seek bar can't render cue markers
- Full-viewport layout, not Fullscreen API — matches Netflix pattern without OS permission
- Mute toggle instead of volume slider — keeps controls minimal

**Verified:** `go build ./...` clean, `svelte-check` passes, `vite build` clean

---

### Phase 3 — AI chat (streaming) ✅

_Goal: user sends a message in the viewer, gets a streamed LLM response, exchange is persisted as events._

**Go backend:**
- `internal/ai/client.go` — `Client` struct with `ChatStream(ctx, messages) <-chan Chunk`. Supports two providers via `AI_PROVIDER` env var: `"sunset"` (OpenAI-compatible format via `staging.api.sunset.video`) and `"anthropic"` (direct Anthropic Messages API). Both share a common `readSSE` helper with per-provider JSON parse callbacks. No external SSE library. The Sunset path folds `role:"system"` messages into the first user message since the proxy silently drops them in streaming mode. Error responses now include the upstream body for easier debugging.
- `internal/api/chat.go` — `POST /api/chat` handler. Reads `{session_id, message, video_pos, history}`. Prepends system prompt with video position context, appends history + new user message. Calls `ai.ChatStream`, writes each chunk as an SSE event (`text/event-stream`), flushes after each write. On stream end, persists `ai_message` + `ai_response` events via recorder. Returns 503 if API key not configured.
- `internal/config/config.go` — Added `AIProvider` (default `"sunset"`) and `AnthropicKey` fields.

**SvelteKit viewer:**
- `web/src/lib/chat.ts` — `sendMessage(sessionId, message, videoPos, history, onToken, onDone, onError)`: POST fetch to `/api/chat`, reads from `ReadableStream`, buffers partial SSE frames, parses `data:` lines for content tokens. Callback-based API for typewriter rendering.
- `web/src/routes/watch/+page.svelte` — resizable bottom-drawer chat panel (1/3 viewport default, drag handle to resize). User bubbles left, assistant bubbles right (with markdown rendering via `snarkdown`). Multi-turn conversation history maintained in frontend state. When collapsed, only the prompt input bar shows. Video section fills remaining space above chat panel (flex layout).

**Status:** Verified end-to-end with both providers. Streaming, multi-turn history, markdown rendering, and event persistence all working. The Sunset proxy silently drops `role:"system"` messages in streaming mode (returns `data: [DONE]` with 200 status) — fixed by folding system content into the first user message. Frontend guards against empty API responses (removes empty assistant placeholders, filters empty messages from history, skips TTS on empty text).

---

### Phase 4 — Voice cues + TTS ✅

_Goal: at configured timecodes, the AI speaks over the video. Chat responses are also spoken aloud. TTS provider is configurable._

**Status: COMPLETE**

**What was built:**

**Go backend:**
- `internal/tts/tts.go` — `Provider` interface with `Speak(ctx, text, voiceID) (*Result, error)`. `Result` contains either mp3 audio bytes (`Type: "audio"`) or text for browser synthesis (`Type: "text"`). `NewProvider(provider, apiURL, apiKey)` factory returns sunset or browser provider.
- `internal/tts/sunset.go` — Sunset provider: POST to `/api/v1/audio/speech`, returns mp3 bytes. Uses `Authorization: Bearer` header.
- `internal/tts/browser.go` — Browser provider: returns text directly, no network call.
- `internal/tts/cache.go` — `CachedProvider` wraps any provider. Caches audio results to `cache/cue-audio/{sha256}.mp3`. Text results pass through uncached.
- `internal/store/cueloader.go` — `SeedCues(path)` reads `data/cues.json` and upserts into `cues` table via `INSERT OR REPLACE` on `(video_id, at_seconds)`.
- `internal/store/sessions.go` — Added `GetCue(id)` method for single cue lookup.
- `internal/api/cues.go` — Expanded `CuesHandler` with `TTS` field. Added `Audio(w, r)` handler for `GET /api/cue-audio?cue_id=N` — returns mp3 bytes or JSON text depending on provider.
- `internal/api/tts.go` — `TTSHandler` with `Speak(w, r)` for `POST /api/tts` — used by frontend for chat response TTS.
- `cmd/server/main.go` — Wired cue seeding, TTS provider, new routes (`/api/cue-audio`, `/api/tts`, `/api/config`).
- `data/cues.json` — 5 narration-style cues for Heat at ~3:00, ~34:00, ~1:15:00, ~2:00:00, ~2:40:00.

**SvelteKit viewer:**
- `web/src/lib/speech.ts` — Unified speech module handling both browser `speechSynthesis` and `HTMLAudioElement` (mp3) playback. `speak(text, id?)` for browser TTS, `speakAudio(blob, id?)` for mp3. `pause()`, `resume()`, `cancel()` work for both modes. Strips markdown before browser speech. Exposes reactive state via `onChange` listener for UI binding.
- `web/src/lib/cueScheduler.ts` — `createCueScheduler(videoEl, {cues, sessionId, onCue})`: listens to `timeupdate`, triggers when `prevTime < at_seconds <= currentTime`. Delegates playback to the page via `onCue` callback. Posts `cue_played` events. Handles seek backward (re-enables cues).
- `web/src/routes/watch/+page.svelte` — Cue narration appears in chat log when triggered, with per-bubble play/pause controls. `playTTS(text, id)` routes through browser speech or `POST /api/tts` based on provider. `playCueAudio(cueId, id)` fetches `GET /api/cue-audio` for cue-specific audio. Chat `onDone` speaks via `playTTS`. Fetches `/api/config` to determine TTS provider mode.

**Key decisions (see `docs/design-decisions.md`):**
- TTS provider abstraction — same pattern as AI chat provider, reviewer flips one env var
- Static narration cues — deterministic, no LLM call needed
- Cue seeding via upsert — edit JSON + restart to reconfigure (bonus: "configurable without code change")
- Chat responses spoken aloud — makes AI feel more present
- Cue narration in chat log — visible text + replayable via per-bubble controls
- Unified speech module — one state machine for both speechSynthesis and mp3 audio

---

### Phase 5 — Dashboard + WebSocket ✅

_Goal: admin page shows live session activity, updated in real-time over WebSocket._

**Status: COMPLETE**

**What was built:**

**Go backend:**
- `internal/pubsub/hub.go` — In-process channel-based fan-out hub. `Hub` struct with `sync.RWMutex`-protected subscriber map. Non-blocking `Publish()` drops messages if a subscriber's buffer (cap 64) is full. `Subscribe()` returns a buffered channel and unsub closure. Three message types: `session_created`, `events_recorded`, `session_idle`.
- `internal/ws/handler.go` — WebSocket bridge from pub/sub to browser clients via `nhooyr.io/websocket`. On connect: sends snapshot (`ListSessionsWithStats()` + `ListRecentEvents(200)`, reversed to chronological order). Write loop forwards pub/sub messages with 5s write timeout. Concurrent read loop drains incoming frames to detect disconnects. `InsecureSkipVerify: true` for CORS in dev.
- `internal/api/admin.go` — `GET /api/admin/sessions` returns `[]SessionWithStats` (LEFT JOIN with event counts + last event kind). `GET /api/admin/sessions/{id}` returns session + full event timeline as `[]EventWithSession`.
- `internal/api/sessions.go` — Added `Hub *pubsub.Hub`. After `Create` succeeds, publishes `session_created` with `{video_id, user_agent}` payload. After `RecordEvents` succeeds, publishes `events_recorded` with marshaled event array.
- `internal/api/chat.go` — Added `Hub *pubsub.Hub`. After recording `ai_message` + `ai_response` events, publishes `events_recorded` so dashboard sees chat activity in real time.
- `cmd/server/main.go` — Creates hub, passes to handlers, wires routes (`GET /api/admin/sessions`, `GET /api/admin/sessions/{id}`, `GET /ws/admin`). Idle sweep goroutine: 10s ticker, checks `last_seen_at > 30s`, publishes `session_idle` for newly-idle sessions, tracks idle set to avoid re-publishing.

**SvelteKit dashboard:**
- `web/src/lib/adminWs.ts` — WebSocket client with auto-reconnect (exponential backoff 1s→2s→4s→8s→15s cap). Callbacks: `onSnapshot`, `onSessionCreated`, `onEventsRecorded`, `onSessionIdle`, `onConnectionChange`.
- `web/src/routes/admin/+page.svelte` — Two-panel layout: terminal-style live event feed (top ~50vh, newest-first, monospace, color-coded by event kind) and session list (bottom ~50vh, sorted by last_seen_at DESC). Status dots: green=live (<30s), amber=idle (>30s), gray=disconnected (>60s). Heartbeat toggle (default off). Auto-scroll pins to top, "Jump to latest" button on scroll-away. Verbose: ai_message/ai_response show full content inline, responses rendered via snarkdown. Click session to expand inline timeline (lazy-fetched from REST). Feed capped at 500 entries.

**Key decisions:**
- `json.RawMessage` for flexible payload embedding in pub/sub messages
- Snapshot-on-connect pattern so dashboard gets full state immediately without separate REST fetch
- Idle sweep as a single goroutine (not per-session) that respects shutdown context

---

### Phase 6 — Polish & README ✅

_Goal: the project is submission-ready._

**Status: COMPLETE**

**What was done:**
- `README.md` — Full rewrite: architecture overview with ASCII diagram, narrative sections (viewer, dashboard, session tracking, API unblocking story), 5-point decision log, "what I'd do next" with cue authoring, auth, Redis, replay viewer, observability
- Landing page (`web/src/routes/+page.svelte`) — replaced SvelteKit placeholder with styled homepage: hero section, setup guide, architecture cards, footer. Dark theme with red accents.
- Tab navigation — bottom nav bar on both `/watch` and `/admin` pages for navigating between views
- Per-film directory structure — restructured `data/videos/default.mp4` + `data/cues.json` into `data/films/heat/` with `film.mp4`, `cues.json`, `metadata.json`. Video registry scans subdirs, loads metadata. Frontend fetches film list dynamically.
- Synopsis overlay — pre-play card on watch page showing title, year, director, synopsis. Also injected as first chat message.
- Cue seek bug fix — added `seeking` flag + jump threshold to prevent cues from firing when user seeks past them
- Design decisions and implementation plan docs updated throughout

---

### Phase 7 — Admin Dashboard Expansion

_Goal: transform the admin dashboard from a 2-panel view into a full admin console with sidebar navigation and 6 analytics widgets, all updating in real time._

#### Phase 7A — Sidebar Navigation + Layout Restructure ✅

_Goal: replace stacked panels with sidebar nav + main content area._

**Status: COMPLETE**

**Frontend (`web/src/routes/admin/+page.svelte`):**

**Layout change:**
```
┌──────────────────────────────────────────────────────────┐
│ moonrise  DASHBOARD                           ● live     │  48px header
├────────────┬─────────────────────────────────────────────┤
│            │                                             │
│  ▶ Feed    │   [Active widget content fills              │
│    Sessions│    the entire main area]                    │
│    Films   │                                             │
│    System  │                                             │
│    AI Usage│                                             │
│    Heatmap │                                             │
│            │                                             │
│            │                                             │
│  ● live    │                                             │
└────────────┴─────────────────────────────────────────────┘
  ~200px                    remaining width
```

**New state:** `let activeView = $state<'feed' | 'sessions' | 'films' | 'system' | 'ai' | 'heatmap'>('feed');`

**Sidebar (~200px, always visible):**
- Text labels with small icons for each widget
- Active item highlighted with `rgba(255,255,255,0.1)` bg + white text + left accent border
- Inactive items: `rgba(255,255,255,0.45)` text
- Connection status (dot + "live"/"offline" label) in sidebar footer
- Styled to match existing theme: `rgba(20,20,20,0.95)` bg, `rgba(255,255,255,0.08)` border-right

**Main content area:**
- Conditionally renders active widget: `{#if activeView === 'feed'}`, `{#if activeView === 'sessions'}`, etc.
- Existing feed panel and sessions panel code moves into these blocks with minimal changes
- Remove bottom tab nav (sidebar replaces it)
- Each widget takes full height of main area (header height subtracted)

**Verification:**
- Sidebar visible, clicking tabs switches content
- Feed and Sessions work exactly as before
- Films/System/AI/Heatmap show placeholder "Coming soon" text
- Connection dot in sidebar footer updates correctly

---

#### Phase 7B — Films Widget ✅

_Goal: show film metadata, aggregate playback stats, and cue definitions for each film in the library._

**Status: COMPLETE**

**Backend — new store queries (`internal/store/sessions.go`):**

```go
type FilmStats struct {
    VideoID        string  `json:"video_id"`
    SessionCount   int     `json:"session_count"`        // COUNT(DISTINCT sessions)
    ActiveSessions int     `json:"active_sessions"`       // sessions with last_seen_at within 30s
    PlayCount      int     `json:"play_count"`            // events where kind='video_play'
    ChatMessages   int     `json:"chat_messages"`         // kind='ai_message'
    AIResponses    int     `json:"ai_responses"`          // kind='ai_response'
    CuesTriggered  int     `json:"cues_triggered"`        // kind='cue_played'
}

func (s *SessionStore) ListFilmStats() ([]FilmStats, error)
```

SQL approach:
```sql
SELECT s.video_id,
  COUNT(DISTINCT s.id) as session_count,
  COUNT(DISTINCT CASE WHEN s.last_seen_at > :threshold THEN s.id END) as active_sessions,
  SUM(CASE WHEN e.kind = 'video_play' THEN 1 ELSE 0 END) as play_count,
  SUM(CASE WHEN e.kind = 'ai_message' THEN 1 ELSE 0 END) as chat_messages,
  SUM(CASE WHEN e.kind = 'ai_response' THEN 1 ELSE 0 END) as ai_responses,
  SUM(CASE WHEN e.kind = 'cue_played' THEN 1 ELSE 0 END) as cues_triggered
FROM sessions s
LEFT JOIN events e ON e.session_id = s.id
GROUP BY s.video_id
```

**Backend — new admin endpoint (`internal/api/admin.go`):**

Update `AdminHandler` struct: add `Registry *video.Registry` field.

```go
func (h *AdminHandler) ListFilms(w, r)
// GET /api/admin/films
// Response: [{
//   ...video.Video fields (id, title, year, director, synopsis, duration, width, height, ...),
//   stats: FilmStats,
//   cues: []Cue
// }]
```

Merges data from three sources: `Registry.List()` for metadata, `Store.ListFilmStats()` for aggregate stats, `Store.ListCues(videoID)` for cue definitions.

**Backend — wiring (`cmd/server/main.go`):**
- Update `AdminHandler` initialization: `&api.AdminHandler{Store: sessionStore, Registry: registry}`
- Add route: `r.Get("/api/admin/films", adminHandler.ListFilms)`

**Backend — WebSocket snapshot expansion (`internal/ws/handler.go`):**

Expand snapshot to include film data:
```go
type snapshotMessage struct {
    Type      string                   `json:"type"`
    Sessions  []store.SessionWithStats `json:"sessions"`
    Events    []store.EventWithSession `json:"events"`
    Films     []filmData               `json:"films"`     // NEW
}

type filmData struct {
    video.Video                                           // embedded metadata
    Stats store.FilmStats                `json:"stats"`
    Cues  []store.Cue                    `json:"cues"`
}
```

Handler needs `Registry *video.Registry` field. Update `ws.Handler` struct and wiring in main.go.

**Frontend — adminWs.ts changes:**

Add types:
```typescript
export interface FilmData {
    id: string;
    title: string;
    year?: number;
    director?: string;
    synopsis?: string;
    duration: number;
    width: number;
    height: number;
    stats: {
        video_id: string;
        session_count: number;
        active_sessions: number;
        play_count: number;
        chat_messages: number;
        ai_responses: number;
        cues_triggered: number;
    };
    cues: { id: number; at_seconds: number; prompt: string; voice_id: string }[];
}
```

Add `onSnapshot` to include `films: FilmData[]`. Add `films` to snapshot callback signature.

**Frontend — Films panel (`web/src/routes/admin/+page.svelte`):**

New state: `let films = $state<FilmData[]>([]);`

Film card layout per film:
```
┌─────────────────────────────────────────────────────┐
│  Heat  (1995) · Directed by Michael Mann            │
│  2h 50m · 1920×800                                  │
├─────────────────────────────────────────────────────┤
│  A master thief and an obsessive detective...       │  synopsis (collapsible)
├─────────────────────────────────────────────────────┤
│  ┌──────────┐ ┌──────────┐ ┌──────────┐            │
│  │ 12       │ │ 3        │ │ 47       │            │  stat cards
│  │ sessions │ │ active   │ │ plays    │            │
│  └──────────┘ └──────────┘ └──────────┘            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐            │
│  │ 28       │ │ 31       │ │ 15       │            │
│  │ messages │ │ responses│ │ cues hit │            │
│  └──────────┘ └──────────┘ └──────────┘            │
├─────────────────────────────────────────────────────┤
│  CUES (5)                                    [▼]    │  expandable
│  3:00    The city of Los Angeles sprawls...         │
│  34:00   Two men on opposite sides...               │
│  1:15:00 The diner scene...                         │
│  2:00:00 The downtown heist erupts...               │
│  2:40:00 The runway lights stretch...               │
└─────────────────────────────────────────────────────┘
```

**Live updates:** When `events_recorded` arrives, the frontend checks the event's session's `video_id` and increments the matching film's stat counters locally. Active sessions count updates when `session_created` / `session_idle` messages arrive.

**Verification:**
- Films tab shows Heat card with correct metadata
- Stats are populated from real data
- Cues section expands to show all 5 cues with formatted timestamps
- Play/chat in viewer → film stats increment in real time
- New session → session_count increments

---

#### Phase 7C.1 — System Health Widget ✅

_Goal: server health overview — uptime, connections, totals, TTS cache stats._

**Status: COMPLETE**

**Backend — new store query (`internal/store/sessions.go`):**

```go
type SystemStats struct {
    TotalSessions  int   `json:"total_sessions"`
    ActiveSessions int   `json:"active_sessions"`   // last_seen_at within 30s
    TotalEvents    int   `json:"total_events"`
}

func (s *SessionStore) GetSystemStats() (SystemStats, error)
```

SQL:
```sql
SELECT
  (SELECT COUNT(*) FROM sessions) as total_sessions,
  (SELECT COUNT(*) FROM sessions WHERE last_seen_at > :threshold) as active_sessions,
  (SELECT COUNT(*) FROM events) as total_events
```

**Backend — pub/sub connection count (`internal/pubsub/hub.go`):**

```go
func (h *Hub) ConnectionCount() int {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return len(h.subs)
}
```

**Backend — server uptime (`cmd/server/main.go`):**

Track `startTime := time.Now()` at top of `main()`. Pass to handlers or expose via a struct.

**Backend — TTS cache stats:**

Simple utility: count files + sum sizes in `cache/cue-audio/` directory. Add to admin handler or a new stats handler.

**Backend — WebSocket snapshot expansion:**

Add `system_stats` to snapshot:
```go
type systemSnapshot struct {
    TotalSessions  int   `json:"total_sessions"`
    ActiveSessions int   `json:"active_sessions"`
    TotalEvents    int   `json:"total_events"`
    UptimeSeconds  int64 `json:"uptime_seconds"`
    WsConnections  int   `json:"ws_connections"`
    CacheFiles     int   `json:"cache_files"`
    CacheSizeBytes int64 `json:"cache_size_bytes"`
}
```

**Frontend — System panel:**

Card grid layout:
```
┌───────────┐ ┌───────────┐ ┌───────────┐ ┌───────────┐
│  2h 14m   │ │  3        │ │  58       │ │  247      │
│  uptime   │ │  ws conns │ │  sessions │ │  events   │
└───────────┘ └───────────┘ └───────────┘ └───────────┘
┌───────────┐ ┌───────────┐
│  12       │ │  4.2 MB   │
│ cache hits│ │ cache size│
└───────────┘ └───────────┘
```

Stats update live: session/event totals increment with incoming events, uptime ticks via a local `setInterval`.

**Verification:**
- System tab shows uptime (updates every second via local timer)
- WS connections count reflects number of open dashboard tabs
- Total sessions/events match database
- TTS cache stats reflect actual disk state

---

#### Phase 7C.2 — AI Usage Widget ✅

_Goal: AI chat analytics — message counts, response metrics._

**Status: COMPLETE**

**Backend — new store query (`internal/store/sessions.go`):**

```go
type AIStats struct {
    TotalMessages  int     `json:"total_messages"`       // kind='ai_message'
    TotalResponses int     `json:"total_responses"`      // kind='ai_response'
    AvgResponseLen float64 `json:"avg_response_length"`  // avg char length of ai_response payload text
    TotalCuePlays  int     `json:"total_cue_plays"`      // kind='cue_played'
}

func (s *SessionStore) GetAIStats() (AIStats, error)
```

SQL:
```sql
SELECT
  SUM(CASE WHEN kind = 'ai_message' THEN 1 ELSE 0 END) as total_messages,
  SUM(CASE WHEN kind = 'ai_response' THEN 1 ELSE 0 END) as total_responses,
  AVG(CASE WHEN kind = 'ai_response' THEN LENGTH(JSON_EXTRACT(payload, '$.text')) END) as avg_response_length,
  SUM(CASE WHEN kind = 'cue_played' THEN 1 ELSE 0 END) as total_cue_plays
FROM events
```

**Backend — WebSocket snapshot expansion:**

Add `ai_stats` to snapshot message alongside `system_stats`.

**Frontend — AI Usage panel:**

```
┌───────────┐ ┌───────────┐ ┌───────────┐ ┌───────────┐
│  28       │ │  31       │ │  ~240     │ │  15       │
│  messages │ │ responses │ │ avg chars │ │ cues hit  │
└───────────┘ └───────────┘ └───────────┘ └───────────┘

RECENT CONVERSATIONS
┌─────────────────────────────────────────────────────┐
│ 12:34:05  a3f2b1c8  "Tell me about this scene"      │
│ 12:34:15  a3f2b1c8  "In this scene, Vincent..."      │
│ 12:41:22  f7e9d2a1  "What's the significance of..."  │
│ 12:41:30  f7e9d2a1  "The diner scene represents..."  │
└─────────────────────────────────────────────────────┘
```

The recent conversations list is a filtered view of the existing feed — showing only `ai_message` and `ai_response` events. This reuses the existing `feedEvents` state with a `$derived` filter.

**Verification:**
- AI tab shows correct message/response counts
- Average response length is reasonable (~100-500 chars)
- Recent conversations show latest AI exchanges
- Stats update when new chat messages arrive

---

#### Phase 7C.3 — Timeline Heatmap Widget ✅

_Goal: visualize where in the film viewers interact most — event density bucketed by video position, rendered as an SVG bar chart._

**Status: COMPLETE**

**Backend — new store query (`internal/store/sessions.go`):**

```go
type HeatmapBucket struct {
    BucketStart float64 `json:"bucket_start"` // seconds
    BucketEnd   float64 `json:"bucket_end"`   // seconds
    PlayCount   int     `json:"play_count"`
    PauseCount  int     `json:"pause_count"`
    SeekCount   int     `json:"seek_count"`
    ChatCount   int     `json:"chat_count"`
    CueCount    int     `json:"cue_count"`
    Total       int     `json:"total"`
}

func (s *SessionStore) GetHeatmap(videoID string, bucketSeconds float64) ([]HeatmapBucket, error)
```

SQL:
```sql
SELECT
  CAST(FLOOR(e.video_pos / :bucket_size) * :bucket_size AS REAL) as bucket_start,
  CAST(FLOOR(e.video_pos / :bucket_size) * :bucket_size + :bucket_size AS REAL) as bucket_end,
  SUM(CASE WHEN e.kind = 'video_play' THEN 1 ELSE 0 END) as play_count,
  SUM(CASE WHEN e.kind = 'video_pause' THEN 1 ELSE 0 END) as pause_count,
  SUM(CASE WHEN e.kind = 'video_seek' THEN 1 ELSE 0 END) as seek_count,
  SUM(CASE WHEN e.kind IN ('ai_message', 'ai_response') THEN 1 ELSE 0 END) as chat_count,
  SUM(CASE WHEN e.kind = 'cue_played' THEN 1 ELSE 0 END) as cue_count,
  COUNT(*) as total
FROM events e
JOIN sessions s ON e.session_id = s.id
WHERE s.video_id = :video_id AND e.video_pos IS NOT NULL AND e.kind != 'heartbeat'
GROUP BY FLOOR(e.video_pos / :bucket_size)
ORDER BY bucket_start
```

**Backend — new endpoint (`internal/api/admin.go`):**

```go
func (h *AdminHandler) GetHeatmap(w, r)
// GET /api/admin/heatmap?video_id=heat&bucket_size=60
// Returns: []HeatmapBucket
```

Route: `r.Get("/api/admin/heatmap", adminHandler.GetHeatmap)`

**Backend — WebSocket snapshot expansion:**

Add heatmap data to snapshot (for default/first film, 60s buckets). The heatmap updates when new events with `video_pos` arrive.

**Frontend — Heatmap panel:**

```
┌─────────────────────────────────────────────────────┐
│  TIMELINE HEATMAP — Heat                            │
│                                                     │
│  ██                                                 │
│  ██      ██                              ██         │
│  ██  ██  ██      ██          ██  ██      ██  ██     │  SVG stacked bars
│  ██  ██  ██  ██  ██  ██  ██  ██  ██  ██  ██  ██     │
│  ├───┴───┴───┴───┴───┴───┴───┴───┴───┴───┴───┤     │
│  0:00        0:30        1:00        1:30           │  time axis
│  ▼           ▼                       ▼              │  cue markers
│                                                     │
│  ■ play/pause  ■ seek  ■ chat  ■ cue               │  legend
└─────────────────────────────────────────────────────┘
```

**SVG rendering (inline, no dependencies):**
- X-axis: video timeline divided into 60-second buckets
- Y-axis: event count (auto-scaled to max bucket)
- Stacked bars: each bar divided into colored segments by event type
  - White (`rgba(255,255,255,0.5)`): play/pause
  - Yellow (`#f5c518`): seek
  - Cyan (`#4fc3f7`): chat
  - Gold (`#ff9800`): cue
- Cue markers: vertical dashed lines at cue timestamps (below the chart)
- Time labels along X-axis (every N minutes based on film length)
- Hover tooltip: shows bucket time range + breakdown of event counts

**Interactive features:**
- Hover a bar → tooltip with time range and per-kind counts
- Cue markers labeled with cue number

**Live updates:** When new events arrive with `video_pos`, find the matching bucket and increment the appropriate counter. Re-render the affected bar.

**Verification:**
- Heatmap tab shows bars for each 60s bucket where events occurred
- Bars are color-coded by event type (stacked)
- Cue markers appear at correct positions
- Hover shows detailed breakdown
- Play/pause/seek/chat in viewer → corresponding bar grows in real time
- Empty buckets show no bar (sparse rendering)

## 13. README / decision log seeds

Lead with the 3–5 choices I expect to defend:
1. **Server-side AI proxy** — simplest way to keep the key safe and centralize event logging.
2. **Events table as source of truth** — dashboard is just a read model; trivially extends to analytics.
3. **WebSocket fan-out from in-proc pubsub** — no Redis, no external broker; fine for single-box demo, called out as a scaling limit.
4. **Pre-generated + cached cue audio** — deterministic playback, avoids TTS latency spikes during video.
5. **SvelteKit for both viewer and dashboard** — one deploy target, shared types, matches Sunset's stack.

"What I'd do next": auth, horizontal scale via Redis pubsub, cue authoring UI, replay viewer that scrubs alongside the video, OpenTelemetry traces across the AI proxy.

## 14. Risks & cuts

- **TTS latency** on first cue play can be multi-second — mitigated by pre-warming on server start for enabled cues.
- **SSE vs WS for chat:** using SSE for chat (one-way) and WS only for dashboard (two-way subscription model). Avoids mixing a WS protocol into the chat path.
- **If behind at hour 4:** cut admin live updates to polling every 2s, keep the WS code in a branch, mention it in README.

## 15. Open questions for Brian

- ~~Should the dashboard show AI message content, or just metadata?~~ → **Resolved: show content**, note privacy cut in README.
- ~~Any preference on the video clip (length, tone)?~~ → **Resolved: using Heat (~2GB, ~2.8hrs)**. Real movie file exercises realistic I/O. Gitignored, Google Drive link in README.
- Want me to include a `docker compose up` path, or is `./run.sh` enough?
