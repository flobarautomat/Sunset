# Moonrise — Implementation Plan

_Draft v1 — iterate on this before writing any code._

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

- **Backend:** Go 1.22, `net/http` + `chi` router, `nhooyr.io/websocket`, `modernc.org/sqlite`.
- **Frontend:** SvelteKit 2 + Svelte 5, TypeScript, minimal CSS (no Tailwind to keep deps light).
- **AI:** `staging.api.sunset.video` for chat completions and TTS. Server-side proxy — API key never touches the browser.
- **Build/run:** single `make dev` or `./run.sh` that starts Go + Vite together. `docker compose` optional if time allows.

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
| `POST` | `/api/sessions` | Create session, return `{session_id}`. |
| `POST` | `/api/sessions/:id/events` | Append one or many events (batched from client). |
| `GET`  | `/api/videos/:id/cues` | Cue list for the viewer to schedule locally. |
| `POST` | `/api/chat` | `{session_id, message}` → streams AI response (SSE). Also records `ai_message` + `ai_response` events. |
| `GET`  | `/api/cue-audio?cue_id=...` | Returns mp3 for a cue. Cached on disk after first generation. |
| `GET`  | `/api/admin/sessions` | Paginated session list with latest event summary. |
| `GET`  | `/api/admin/sessions/:id` | Full event timeline for one session. |
| `WS`   | `/ws/admin` | Pushes `session_created`, `event_appended`, `session_idle` messages in real time. |

All AI traffic is server-side. Client sends plain text to `/api/chat`, Go calls `/api/v1/chat/completions`, streams chunks back over SSE, and persists the final transcript.

## 6. Viewer app (`/watch`)

- `<video>` element with a freely-licensed clip (Blender's Big Buck Bunny or a short NASA clip, committed to `/static`).
- On mount: `POST /api/sessions` → stash `session_id` in memory.
- Attach listeners for `play`, `pause`, `seeking`, `ended`, `timeupdate` — push events in batches (every ~2s or on state change) to `/events`. Heartbeat every 10s.
- **Cue scheduler (client-side):** fetch `/cues` once, track `currentTime`, trigger cue when `prev < at <= now`. Trigger = request `/cue-audio?cue_id` and play it via `<audio>`, log a `cue_played` event.
- **Chat:** text input + message list. On submit → fetch `/api/chat` with SSE, render tokens as they arrive.

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
- `TestRecorder_BatchesHeartbeats` — collapses N heartbeats within a window into one.
- `TestRecorder_RejectsUnknownKind`.
- `TestRecorder_ClampsVideoPos` (negative / > duration).
- `TestRecorder_OrderingWithSeek` — seek followed by play emits events in correct logical order.
- `TestRecorder_IdleTransition` — store behavior across the idle threshold.

Use `testing` + `testify/assert`. In-memory `EventStore` fake for speed.

## 11. Project layout

```
moonrise/
├── README.md
├── run.sh                        # starts Go + Vite concurrently
├── cmd/server/main.go
├── internal/
│   ├── api/          # chi handlers
│   ├── ai/           # client for staging.api.sunset.video
│   ├── recorder/     # session + event logic (unit tested)
│   ├── cues/         # cue loader + audio cache
│   ├── pubsub/       # in-proc bus
│   ├── store/        # sqlite queries
│   └── ws/           # websocket hub
├── data/
│   └── cues.json
├── web/                          # SvelteKit app
│   ├── src/routes/watch/+page.svelte
│   ├── src/routes/admin/+page.svelte
│   └── src/lib/
└── static/video.mp4
```

## 12. Implementation phases

Each phase ends with something runnable. Dependencies flow downward — don't skip ahead.

---

### Phase 0 — Local dev setup

_Goal: all tools installed and verified. You can compile Go and run SvelteKit before writing any project code._

**Prerequisites (one-time):**
- Install Go: `brew install go` — verify with `go version` (need 1.22+)
- Install Node: `brew install node` — verify with `node -v` (need 18+) and `npm -v`
- You already have these if `which go` and `which node` return paths

**Bootstrap the Go module:**
```bash
mkdir moonrise && cd moonrise
go mod init moonrise
```
This creates `go.mod`. Then pull in the three main dependencies:
```bash
go get github.com/go-chi/chi/v5
go get nhooyr.io/websocket
go get modernc.org/sqlite
go get github.com/stretchr/testify
```
Each `go get` adds a line to `go.mod` and updates `go.sum`. Run `go mod tidy` afterward to clean up.

**Scaffold the SvelteKit app:**
```bash
npm create svelte@latest web
```
The CLI wizard will ask a few questions — pick "Skeleton project", TypeScript, and no extras (no Playwright, no Vitest for now). Then:
```bash
cd web && npm install && cd ..
```
This creates `web/package.json`, `web/node_modules/`, and the SvelteKit boilerplate.

**Grab a video clip:**
- Download a short (30–60s) freely-licensed clip — e.g. a Big Buck Bunny extract or a NASA public domain clip
- Save to `web/static/video.mp4` — SvelteKit serves everything in `static/` at the root URL

**Set up env vars:**
- Create `.env` from `.env.example`:
  ```
  PORT=8080
  SUNSET_API_KEY=<your key from the challenge email>
  SUNSET_API_URL=https://staging.api.sunset.video
  ```

**Verify:**
- `go build ./...` — compiles with zero errors (no code yet, but confirms deps resolve)
- `cd web && npm run dev` — opens `http://localhost:5173` with the SvelteKit welcome page
- You're ready for Phase 1

---

### Phase 1 — Scaffold & plumbing

_Goal: both servers start, talk to each other, and persist data. Nothing visible yet beyond "hello world."_

**Go backend:**
- `cmd/server/main.go` — chi router, graceful shutdown, CORS middleware, config from env vars (`PORT`, `API_KEY`, `SUNSET_API_URL`)
- `internal/store/db.go` — open SQLite via `modernc.org/sqlite`, run `migrations/001_init.sql` on startup
- `internal/store/migrations/001_init.sql` — `sessions`, `events`, `cues` tables + indexes (from section 4)

**SvelteKit:**
- `web/` — `npm create svelte@latest`, TypeScript, minimal skeleton
- `web/src/routes/watch/+page.svelte` — blank page, confirms Vite dev server proxies `/api` to Go
- `web/vite.config.ts` — proxy rule: `/api` + `/ws` → `http://localhost:8080`

**Glue:**
- `run.sh` — starts Go (`go run ./cmd/server`) and Vite (`cd web && npm run dev`) concurrently with `trap` cleanup
- `.env.example` — documents all env vars

**Verify:** `./run.sh` starts both processes. Hitting `http://localhost:5173/watch` shows the blank page. `curl localhost:8080/healthz` returns 200.

---

### Phase 2 — Video playback + session tracking (+ unit tests)

_Goal: a user watches a video, and every play/pause/seek lands in SQLite. Tests prove the recorder logic is correct._

**Go backend:**
- `internal/recorder/recorder.go` — `Recorder` struct with `CreateSession(ua, videoID)` and `RecordEvents(sessionID, []Event)`. Accepts an `EventStore` interface so it's testable without a DB. Validates event kinds, clamps `video_pos`, collapses rapid heartbeats.
- `internal/recorder/recorder_test.go` — the 5 tests from section 10 (`BatchesHeartbeats`, `RejectsUnknownKind`, `ClampsVideoPos`, `OrderingWithSeek`, `IdleTransition`). In-memory `EventStore` fake.
- `internal/store/sessions.go` — SQLite implementation of `EventStore` interface
- `internal/api/sessions.go` — `POST /api/sessions` (creates session, returns `{session_id}`) and `POST /api/sessions/:id/events` (accepts batch, calls recorder)

**SvelteKit viewer:**
- `web/static/video.mp4` — download a short freely-licensed clip (Big Buck Bunny 30s extract or similar)
- `web/src/lib/tracker.ts` — `createTracker(sessionId)`: attaches `play`, `pause`, `seeking`, `ended`, `timeupdate` listeners to a `<video>` element, buffers events, flushes to `/api/sessions/:id/events` every 2s, sends heartbeat every 10s
- `web/src/routes/watch/+page.svelte` — mounts `<video>`, calls `POST /api/sessions` on mount, wires up tracker

**Verify:** play/pause the video, then `sqlite3 moonrise.db "SELECT kind, video_pos FROM events ORDER BY at"` shows the event trail. `go test ./internal/recorder/...` passes.

---

### Phase 3 — AI chat (streaming)

_Goal: user sends a message in the viewer, gets a streamed LLM response, exchange is persisted as events._

**Go backend:**
- `internal/ai/client.go` — `ChatStream(ctx, model, messages) <-chan Chunk` — calls `staging.api.sunset.video/api/v1/chat/completions` with `stream: true`, parses SSE, yields `Chunk{Text, Done, Err}` on a channel
- `internal/api/chat.go` — `POST /api/chat` handler. Reads `{session_id, message, video_pos}`. Calls `ai.ChatStream`, writes each chunk as an SSE event to the HTTP response (`text/event-stream`). On stream end, persists `ai_message` + `ai_response` events via recorder.

**SvelteKit viewer:**
- `web/src/lib/chat.ts` — `sendMessage(sessionId, message, videoPos)`: opens `fetch` to `/api/chat` with streaming body reader, yields tokens via a callback or async iterator
- `web/src/routes/watch/+page.svelte` — add chat panel below video: message list + text input. On submit, calls `sendMessage`, appends tokens to the assistant bubble as they arrive (typewriter effect). Includes `video_pos` from `currentTime`.

**Verify:** type a message, see the response stream in character by character. Check `events` table for the `ai_message` and `ai_response` rows.

---

### Phase 4 — Voice cues + TTS

_Goal: at configured timecodes, the AI speaks over the video. Audio is generated once and cached._

**Go backend:**
- `internal/ai/tts.go` — `GenerateSpeech(ctx, text, voiceID) ([]byte, error)` — calls `/api/v1/audio/speech`, returns raw mp3 bytes
- `internal/cues/loader.go` — on startup, reads `data/cues.json`, upserts into `cues` table. Exposes `GetCuesForVideo(videoID) []Cue`.
- `internal/cues/cache.go` — `GetOrGenerateAudio(cue) ([]byte, error)` — checks disk cache at `cache/cue-audio/{sha256}.mp3`, if miss: calls LLM with cue's `prompt` to get a spoken line → calls `tts.GenerateSpeech` → writes to disk → returns bytes
- `internal/api/cues.go` — `GET /api/videos/:id/cues` (returns cue list) and `GET /api/cue-audio?cue_id=...` (serves cached mp3, generates on first request)
- `data/cues.json` — seed file, e.g. `[{"video_id": "default", "at_seconds": 8.0, "prompt": "Greet the viewer", "voice_id": "..."}]`

**SvelteKit viewer:**
- `web/src/lib/cueScheduler.ts` — `createCueScheduler(videoEl, sessionId)`: fetches `/api/videos/:id/cues` once. On each `timeupdate`, checks if `prevTime < cue.at_seconds <= currentTime`. If triggered: fetches `/api/cue-audio?cue_id=...` as a blob, plays via a hidden `<audio>` element, posts a `cue_played` event. Handles seek (recomputes next cue index).
- `web/src/routes/watch/+page.svelte` — add hidden `<audio id="cue">`, wire up cue scheduler alongside the tracker

**Verify:** play the video past the configured timecode, hear the AI voice. Seek backward past it and replay — it fires again. Check `events` for `cue_played`.

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

- Any preference on the video clip (length, tone)? Short clip (~30–60s) makes cue testing faster.
- Want me to include a `docker compose up` path, or is `./run.sh` enough?
- Should the dashboard show AI message content, or just metadata (counts, timestamps) for privacy signaling?
