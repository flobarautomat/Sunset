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
├── data/
│   ├── cues.json
│   └── videos/
│       └── default.mp4           # ~2GB, gitignored, see README
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
- `internal/ai/client.go` — `Client` struct with `ChatStream(ctx, messages) <-chan Chunk`. Supports two providers via `AI_PROVIDER` env var: `"sunset"` (OpenAI-compatible format via `staging.api.sunset.video`) and `"anthropic"` (direct Anthropic Messages API). Both share a common `readSSE` helper with per-provider JSON parse callbacks. No external SSE library.
- `internal/api/chat.go` — `POST /api/chat` handler. Reads `{session_id, message, video_pos, history}`. Prepends system prompt with video position context, appends history + new user message. Calls `ai.ChatStream`, writes each chunk as an SSE event (`text/event-stream`), flushes after each write. On stream end, persists `ai_message` + `ai_response` events via recorder. Returns 503 if API key not configured.
- `internal/config/config.go` — Added `AIProvider` (default `"sunset"`) and `AnthropicKey` fields.

**SvelteKit viewer:**
- `web/src/lib/chat.ts` — `sendMessage(sessionId, message, videoPos, history, onToken, onDone, onError)`: POST fetch to `/api/chat`, reads from `ReadableStream`, buffers partial SSE frames, parses `data:` lines for content tokens. Callback-based API for typewriter rendering.
- `web/src/routes/watch/+page.svelte` — resizable bottom-drawer chat panel (1/3 viewport default, drag handle to resize). User bubbles left, assistant bubbles right (with markdown rendering via `snarkdown`). Multi-turn conversation history maintained in frontend state. When collapsed, only the prompt input bar shows. Video section fills remaining space above chat panel (flex layout).

**Status:** Verified end-to-end using direct Anthropic API (`AI_PROVIDER=anthropic`). Streaming, multi-turn history, markdown rendering, and event persistence all working. The provided `SUNSET_API_KEY` returns 401 from `staging.api.sunset.video` — may be expired or revoked. The Sunset provider path is implemented and ready for a valid key.

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
- `web/src/lib/speech.ts` — `speak(text, id?)`, `pause()`, `resume()`, `cancel()` wrapping browser `speechSynthesis` API. Strips markdown before speaking (bold, links, code blocks, list markers, etc.). Exposes reactive state via `onChange` listener for UI binding.
- `web/src/lib/cueScheduler.ts` — `createCueScheduler(videoEl, {cues, sessionId})`: listens to `timeupdate`, triggers when `prevTime < at_seconds <= currentTime`. Fetches `/api/cue-audio`, checks `Content-Type` to decide between `<audio>` playback or `speechSynthesis`. Posts `cue_played` events. Handles seek backward (re-enables cues).
- `web/src/routes/watch/+page.svelte` — Wired cue scheduler on mount. Chat `onDone` callback speaks the response via browser speech or `/api/tts`. Per-bubble speech controls on assistant messages (play/pause/resume). Fetches `/api/config` to determine TTS provider mode. Cleanup cancels speech on unmount.

**Key decisions (see `docs/design-decisions.md`):**
- TTS provider abstraction — same pattern as AI chat provider, reviewer flips one env var
- Static narration cues — deterministic, no LLM call needed
- Cue seeding via upsert — edit JSON + restart to reconfigure (bonus: "configurable without code change")
- Chat responses spoken aloud — makes AI feel more present

---

### Phase 5 — Dashboard + WebSocket

_Goal: admin page shows live session activity, updated in real-time over WebSocket._

**Go backend:**
- `internal/pubsub/pubsub.go` — `Hub` struct with `Publish(topic, payload)` and `Subscribe(pattern) <-chan Message`. In-process, uses Go channels. Topic patterns: `session.created`, `session.{id}.event`, `session.{id}.idle`.
- `internal/ws/hub.go` — `AdminHub`: upgrades `/ws/admin` to WebSocket via `nhooyr.io/websocket`, subscribes to pubsub firehose `session.*`, writes JSON frames to all connected dashboard clients. Handles client disconnect gracefully.
- `internal/api/admin.go` — `GET /api/admin/sessions` (paginated list with last event summary + event count) and `GET /api/admin/sessions/:id` (full event timeline)
- `internal/api/sessions.go` — after writing events to DB, also `hub.Publish("session.{id}.event", ...)`. After creating a session, publish `session.created`.
- `cmd/server/main.go` — wire pubsub hub, pass to handlers and WS hub, start idle-sweep goroutine (marks sessions idle after 30s without heartbeat, publishes `session.{id}.idle`)

**SvelteKit dashboard:**
- `web/src/lib/adminWs.ts` — opens WebSocket to `/ws/admin`, exposes a Svelte store that accumulates session state from incoming messages
- `web/src/routes/admin/+page.svelte` — session table (id, started, last seen, events count, current video pos, live/idle indicator). Click a row → expands event timeline. All updated reactively from the WS store. Initial state loaded from `GET /api/admin/sessions` on mount.

**Verify:** open `/watch` in one tab, `/admin` in another. Play/pause/chat in the viewer — dashboard updates within a second without refresh. Open a second viewer tab — new session appears in the dashboard.

---

### Phase 6 — Polish & README

_Goal: the project is submission-ready._

- `README.md` — how to run (`./run.sh`), decision log (5 bullets from section 13), "what I'd do next"
- `run.sh` — verify it works from a clean clone (`go mod download && cd web && npm install && ...`)
- Quick pass over code: remove dead imports, add brief doc comments on exported types, make sure error messages are helpful
- Test `go test ./...` passes, no Svelte build warnings
- Optional if time: `Makefile` with `make dev`, `make test`, `make build`

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
