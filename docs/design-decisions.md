# Design Decisions

Significant choices made during implementation and the reasoning behind them.

---

### Backend-served video with range requests

Videos live in `data/films/{id}/` and are served by the Go backend via `http.ServeContent`, not from SvelteKit's `static/` directory. This matches how production works — video delivery goes through the backend where you can add auth, access logging, and CDN origin behavior. `http.ServeContent` handles `Range` headers (HTTP 206 Partial Content) which browsers need for seeking in `<video>` elements, plus `If-Modified-Since` and correct `Content-Type` for free. The viewer discovers available videos via `GET /api/videos` rather than hardcoding paths.

### Real video asset, not a toy clip

Using a full-length ~2GB movie file instead of a 30-second freely-licensed clip. This exercises realistic conditions: range requests across a large file, seeking behavior, buffering, and the kind of I/O the backend would actually handle in production. The backend serves it from disk via `http.ServeContent` — this is the production serving path, not a demo shortcut. The file is gitignored because large binaries don't belong in version control; the README tells the reviewer where to download it and where to place it.

### Per-film directory structure over flat layout

Each film is a self-contained directory under `data/films/`: the video file (`film.mp4`), cue definitions (`cues.json`), and metadata (`metadata.json`) live together. The folder name IS the video ID — `data/films/heat/` means video ID `heat`. Adding a new film is "create a folder, drop files in it" — no code changes, no schema migrations, no database inserts. Film metadata (title, year, director, synopsis) lives in a JSON file rather than in SQLite because it's static content that changes with the film, not with user activity. Keeping it as a file means it's version-controllable, human-editable, and doesn't require database tools to inspect or update. The synopsis is surfaced to the viewer as a pre-play overlay and the first chat message, giving context before the film starts.

### MP4 metadata extraction via `alfg/mp4`

Extract duration, resolution, and bitrate from mp4 files at startup using a pure-Go mp4 parser. No CGO, no ffprobe dependency. Duration feeds cue validation (is `at_seconds` within range?), resolution and bitrate give the dashboard richer context per session. Metadata is computed once at startup and cached in an in-memory registry — the video directory is not re-scanned on every request.

### Recorder with injected EventStore

The recorder owns both session creation and event recording behind an `EventStore` interface. API handlers are thin — they decode JSON and call the recorder. This makes the core logic unit-testable with an in-memory fake store (no database, no HTTP). Only heartbeats are collapsed (within a 5s window); play/pause/seek pass through as-is since they're cheap and the dashboard can filter if needed.


### Custom video player over native controls

Native `<video controls>` can't render cue markers on the seek bar or provide a consistent dark UI. Building custom controls turns the seek bar into a data visualization surface — cue positions now, potentially a watch-progress heatmap later. The trade-off is more frontend code and losing built-in accessibility, mitigated by keeping keyboard shortcuts (space for play/pause) and using semantic HTML where possible. Full-viewport layout (not the Fullscreen API) with auto-hiding controls matches the Netflix pattern without requiring OS-level fullscreen permission.

### Stateless AI chat proxy

The Go backend proxies chat requests to the Sunset staging API (`staging.api.sunset.video/api/v1/chat/completions`) with SSE streaming. The backend is stateless — the frontend owns conversation history and sends the full message array with each request. This avoids server-side session memory management and keeps the backend simple. Events (`ai_message`, `ai_response`) are persisted to SQLite after each exchange for analytics, but are never used to reconstruct conversation state. The model is configurable via `AI_MODEL` env var (defaults to `anthropic/claude-haiku-4-5-20251001` for speed; can switch to sonnet for quality).

### Switchable AI provider (Sunset proxy vs direct Anthropic)

The Sunset staging API key was returning 401 during development. Rather than block on getting a new key, added a provider abstraction: `AI_PROVIDER=sunset` talks the OpenAI-compatible format through the Sunset proxy, `AI_PROVIDER=anthropic` talks directly to the Anthropic Messages API with a personal key. The two providers share a common `readSSE` helper that handles HTTP + line-by-line SSE parsing, with per-provider parse callbacks for the different JSON shapes (OpenAI's `choices[0].delta.content` vs Anthropic's `content_block_delta`). The `ChatStream` channel interface is identical to callers — the chat handler doesn't know which provider is active.

### Markdown rendering in chat bubbles

Assistant messages are rendered as markdown via `snarkdown` (1KB, zero-dependency parser). User messages stay as plain text. This lets the AI use bold, lists, code blocks, and links naturally — which it does by default — without the output looking like raw markup. Chose snarkdown over heavier parsers (marked, markdown-it) because the chat context doesn't need footnotes, tables, or GFM extensions.

### Resizable chat panel over modal or sidebar

The chat panel is a bottom drawer that starts at 1/3 viewport height and is drag-resizable. The video shrinks to fill remaining space above. This keeps the video always visible (unlike a modal) and uses horizontal space better than a sidebar (video aspect ratio is landscape). When collapsed, only the prompt input bar shows — always accessible without expanding the full chat history.

### Switchable TTS provider (Sunset API vs browser speech)

The coding challenge provides a TTS endpoint at `staging.api.sunset.video/api/v1/audio/speech`. However, the provided API key returned 401 during development. Rather than block on getting a new key, added a `TTS_PROVIDER` abstraction: `"sunset"` calls the Sunset API and returns mp3 audio, `"browser"` returns the text for the frontend to speak via the Web Speech API (`speechSynthesis`). The backend mediates both paths — even for browser mode, the frontend asks the backend for cue text via `/api/cue-audio`, so the architecture stays consistent. The reviewer can flip one env var to `TTS_PROVIDER=sunset` with their working key and get real TTS voices. Default is `"browser"` so the app works immediately without any API key.

### Static narration cues over LLM-generated lines

Cue prompts contain the final narration text directly rather than being sent to an LLM to generate a spoken line. This keeps cue playback deterministic (same text every time), avoids an extra API call and its latency, and makes cues editable by non-technical users who can just edit the film's `cues.json`. The trade-off is less dynamic/contextual narration, but for preset scene descriptions this is the right call.

### Cue seeding via JSON with upsert

Voice cues are defined in each film's `cues.json` (e.g. `data/films/heat/cues.json`) and seeded into the `cues` table on server startup via `INSERT OR REPLACE` keyed on `(video_id, at_seconds)`. The `video_id` is auto-set from the folder name. This means editing the JSON file and restarting the server updates cues without a code change — satisfying the bonus requirement ("configurable without a code change"). The upsert approach was chosen over skip-if-present because it lets you iterate on cue text without wiping the database.

### Chat responses spoken aloud via TTS

After an AI chat response finishes streaming, the full response text is spoken using the same TTS provider as voice cues. For browser mode this happens directly in the frontend (no server round-trip); for sunset mode the frontend calls `POST /api/tts` to generate audio. This makes the AI feel more present — it speaks as well as types — without complicating the SSE streaming path. Markdown is stripped before speaking (bold markers, code blocks, list bullets, link syntax, etc.) so the speech sounds natural. Each assistant bubble has a play/pause button for user control.

### Cue narration appears in chat log

When a voice cue triggers during playback, its narration text is added to the chat panel as an assistant message. This serves two purposes: the user can see what was said (useful if volume is low or speech overlaps dialogue), and the per-bubble play/pause controls let them replay or pause the narration. The cue scheduler delegates playback to the page via an `onCue` callback rather than handling audio directly — this keeps the scheduler focused on timing while the page owns the chat log and TTS routing.

### Unified speech module for both playback modes

The `speech.ts` module handles both browser `speechSynthesis` and `HTMLAudioElement` (mp3) playback behind the same state interface. Pause/resume/cancel work identically regardless of which mode is active. This means per-bubble controls, cue playback, and chat TTS all share one state machine — no parallel tracking of audio elements vs speech utterances. The module exposes an `onChange` listener so Svelte reactive state stays in sync without polling.