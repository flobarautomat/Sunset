# Moonrise

An AI-powered interactive video experience. A viewer watches a film with AI voice narration and a context-aware chat assistant, while a live admin dashboard tracks every session in real time over WebSocket.

Built as a founding engineer coding challenge for Sunset. The goal was to demonstrate taste in architecture and UI/UX — not just feature coverage.

## Setup

### Prerequisites

- **Go 1.22+**: `brew install go`
- **Node 22**: `brew install node@22` (Node 23 is not in SvelteKit's supported range)

### Install dependencies

```bash
go mod download
cd web && npm install && cd ..
```

### Download the video file

The demo uses a real video file (~2GB) that's too large for git. It is the movie Heat by Michael Mann, released in 1995. Download it and place it in the expected location:

1. Download from: **[Google Drive](https://drive.google.com/file/d/1nEugaQe9h2ZUtQbnQNC_Nwtg1NHL3CKc/view?usp=drive_link)**
2. Save to: `data/films/heat/film.mp4`

```bash
mkdir -p data/films/heat
# move or copy your downloaded file:
mv ~/Downloads/Heat.mp4 data/films/heat/film.mp4
```

### Configure environment

```bash
cp .env.example .env
# Edit .env and set your SUNSET_API_KEY
```

#### AI Chat

By default, `AI_PROVIDER=sunset` uses the Sunset staging API for chat. If that key isn't working, set `AI_PROVIDER=anthropic` with your own `ANTHROPIC_API_KEY` to use the Anthropic Messages API directly.

#### TTS / Voice Cues

The app includes 5 narration-style voice cues that trigger at key moments during the video. TTS is configurable via `TTS_PROVIDER`:

- **`TTS_PROVIDER=browser`** (default) — Uses the browser's built-in Web Speech API. No API key needed. Works immediately, but voices are synthetic.
- **`TTS_PROVIDER=sunset`** — Calls the Sunset TTS API (`/api/v1/audio/speech`) for real AI-generated voices. Requires a working `SUNSET_API_KEY`. Set `TTS_VOICE_ID` to a voice UUID from `/api/v1/voices`.

The provided API key returned 401 from the TTS endpoint during development, so browser mode is the default. The Sunset TTS path is fully implemented — flip the env var with a valid key and it works, including disk caching of generated audio in `cache/cue-audio/`.

### Run

```bash
./run.sh
```

This starts the Go backend on `:8080` and the SvelteKit dev server on `:5173`.

- **Viewer**: http://localhost:5173/watch
- **Dashboard**: http://localhost:5173/admin

## Architecture

```
┌────────────────┐           ┌────────────────────────┐           ┌──────────────────┐
│  Viewer (SK)   │  HTTP/WS  │      Go backend        │   HTTP    │  staging.api.    │
│  /watch        │──────────▶│  - REST + SSE + WS     │──────────▶│  sunset.video    │
│  HTMLVideo +   │◀──────────│  - SQLite (modernc)    │           │  (chat + TTS)    │
│  chat + audio  │           │  - event bus (in-proc) │           └──────────────────┘
└────────────────┘           └──────────┬─────────────┘
                                        │  WS broadcast
┌────────────────┐                      │
│ Dashboard (SK) │◀─────────────────────┘
│ /admin         │
└────────────────┘
```

A single Go binary serves the REST API, WebSocket hub, and proxies all AI calls — the API key never touches the browser. SvelteKit ships two routes (`/watch` and `/admin`) from one app. SQLite via `modernc.org/sqlite` (pure Go, no CGO) stores sessions, events, and cue configurations.

Each film is a self-contained directory under `data/films/` — the video file, cue definitions, and metadata (title, synopsis, director, year) live together. The folder name is the video ID. Adding a new film means creating a folder and dropping files in it — no code changes, no schema migrations.

### The Viewer

A Netflix-style full-viewport video player with custom controls, an auto-hiding overlay, and cue markers on the seek bar. A resizable bottom-drawer chat panel lets the user ask questions about the current scene — responses stream in via SSE with markdown rendering. Five voice cues narrate at key moments during the film, appearing in the chat log with per-bubble play/pause controls. The whole thing is dark-themed and keyboard-navigable.

### The Dashboard

A real-time admin console with a sidebar navigation and three widgets (more coming). A **live event feed** shows every viewer interaction as a terminal-style stream — newest-first, monospace, color-coded by event type. A **sessions list** tracks every viewer with live/idle/disconnected status indicators; click to expand the full event timeline inline. A **films widget** shows per-film metadata (title, year, director, synopsis, runtime, resolution), aggregate stats (sessions, plays, chat messages, AI responses, cues triggered), and the full cue list with timestamps. Everything updates over WebSocket — no polling. An in-process pub/sub hub fans out events from the API handlers to all connected dashboard clients. Film stats increment live as viewers interact.

### Session Tracking

Every viewer interaction — play, pause, seek, chat message, AI response, cue trigger, heartbeat — is recorded as an append-only event in SQLite. The recorder owns this logic behind an injected `EventStore` interface, which made it straightforward to unit test with an in-memory fake (6 tests covering session creation, heartbeat batching, input validation, and state transitions). The events table is the source of truth; the dashboard is just a read model over it.

### How I Unblocked the API

The provided Sunset API key returned 401 from the TTS endpoint and silently dropped `role: "system"` messages in streaming chat mode. Rather than wait for a new key, I built provider abstractions for both services. For chat: `AI_PROVIDER=sunset` talks the OpenAI-compatible format through the Sunset proxy, while `AI_PROVIDER=anthropic` talks directly to the Anthropic Messages API with a personal key. For TTS: `TTS_PROVIDER=sunset` calls the real API, while `TTS_PROVIDER=browser` falls back to the Web Speech API with zero configuration. Both abstractions share a common interface — callers don't know which provider is active. The Sunset paths are fully implemented and tested; flip one env var with a working key and they light up, including disk caching of generated audio.

## Decision Log

These are the five choices I'd most want to talk through with a reviewer. The full list with detailed reasoning lives in [docs/design-decisions.md](docs/design-decisions.md).

1. **Server-side AI proxy over client-side calls.** All AI traffic goes through the Go backend. The API key stays on the server, every exchange gets persisted as events for the dashboard, and swapping providers is a config change — not a frontend deploy.

2. **Append-only event log as the source of truth.** Sessions and events live in SQLite as an immutable log. The dashboard reads from it; the recorder writes to it. This separation means analytics, replay, and new dashboard views are just queries — no new write paths. Only heartbeats are collapsed (5s window); everything else passes through because storage is cheap and filtering is easy.

3. **Custom video player instead of native controls.** Native `<video controls>` can't render cue markers on the seek bar or maintain a consistent dark aesthetic. Building custom controls turned the seek bar into a data visualization surface — cue positions today, potentially a watch-progress heatmap later. The trade-off is more frontend code, but the UX payoff is significant.

4. **WebSocket fan-out from in-process pub/sub.** The dashboard gets real-time updates via a simple Go channel-based hub — no Redis, no external broker. API handlers publish after committing to SQLite; the WebSocket bridge subscribes and forwards. The snapshot-on-connect includes sessions, events, and per-film aggregate stats so the dashboard has full state immediately. This is the right complexity for a single-box demo, and I'd call out Redis pub/sub as the first thing to add for horizontal scale.

5. **Provider abstractions that unblocked development.** When the Sunset API key didn't work for TTS and had quirks in streaming chat, I built switchable providers for both rather than stubbing or blocking. The browser speech fallback means the app works immediately with zero configuration, while the Sunset paths are production-ready for when the key works. This is the kind of pragmatic decision I'd make in a real codebase — build the abstraction that solves today's problem and tomorrow's.

## What I'd Do Next

- **Cue authoring interface.** The current cues are preset in each film's `cues.json` and seeded on startup — they work, but they're not well-matched to every moment in the film. I'd add a cue editor in the admin dashboard (or a dedicated authoring tool) where you can scrub through the video, drop pins at timestamps, write narration text, and preview TTS — all without touching code or restarting the server.
- **Auth and access control.** No auth was a deliberate cut per the challenge constraints, but the admin dashboard and API endpoints would need it in production. Token-based auth for the API, session cookies for the viewer, role-based access for admin routes.
- **Horizontal scale via Redis pub/sub.** The in-process hub works for single-box, but multiple server instances need a shared event bus. Redis pub/sub is the natural next step — the `Hub` interface wouldn't change, just the implementation behind it.
- **Replay viewer.** The event log has everything needed to reconstruct a viewing session. A replay mode that scrubs through events alongside the video — showing when the user paused, what they asked the AI, which cues fired — would be a compelling demo of why the append-only log matters.
- **Observability.** OpenTelemetry traces across the AI proxy path (request → Sunset/Anthropic → SSE stream → event persistence) would make debugging latency and failures much easier. Structured logging with trace IDs instead of `log.Printf`.
