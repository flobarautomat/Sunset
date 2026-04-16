#!/usr/bin/env bash
set -euo pipefail

# Use Node 22 if available (SvelteKit requires ^20.19 || ^22.12 || >=24)
if [ -d "/opt/homebrew/opt/node@22/bin" ]; then
  export PATH="/opt/homebrew/opt/node@22/bin:$PATH"
fi

# Load .env if present
if [ -f .env ]; then
  set -a
  source .env
  set +a
fi

trap 'kill 0' EXIT

echo "Starting moonrise..."
echo "  Go backend  → http://localhost:${PORT:-8080}"
echo "  Vite dev    → http://localhost:5173"

# Start Go backend
go run ./cmd/server &

# Start SvelteKit dev server
cd web && npm run dev -- --port 5173 &

wait
