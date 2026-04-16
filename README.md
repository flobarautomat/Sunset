# Moonrise

Minimal AI-powered interactive video experience. A viewer watches a video with AI voice cues and chat, while an admin dashboard shows live session activity.

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

The demo uses a real video file (~2GB) that's too large for git. Download it and place it in the expected location:

1. Download from: **[Google Drive link — TODO]**
2. Save to: `data/videos/default.mp4`

```bash
mkdir -p data/videos
# move or copy your downloaded file:
mv ~/Downloads/Heat.mp4 data/videos/default.mp4
```

### Configure environment

```bash
cp .env.example .env
# Edit .env and set your SUNSET_API_KEY
```

### Run

```bash
./run.sh
```

This starts the Go backend on `:8080` and the SvelteKit dev server on `:5173`.

- **Viewer**: http://localhost:5173/watch
- **Dashboard**: http://localhost:5173/admin

## Decision Log

See [docs/design-decisions.md](docs/design-decisions.md) for the full list with reasoning.

## What I'd Do Next

_TODO — fill in during Phase 6._
