# Founding Engineer Coding Challenge

## Overview

Build **Sunspot** — a minimal AI-powered interactive video experience. This is a reduced version of a real production system. We care less about polish and more about the decisions you make.

**Time limit:** 4–6 hours. Don't try to finish everything. Show us how you think.

---

## What to Build

Features to implement:

- Video playback
- AI voice that speaks during the video
- A text input to ask questions and get AI responses
- Session tracking (who's watching, what they're doing)
- An admin view of session activity

How you structure and connect these is entirely up to you. Three interconnected pieces to think about:

### 1. Viewer App

A page where a user watches a video (use any freely licensed clip or a local file). At some point during playback, an AI-generated voice speaks. The user can also interact with the AI by sending a message and getting a response.

### 2. Backend

A server that supports the viewer and the dashboard. What it does and how it's structured is up to you.

### 3. Dashboard

A minimal admin view of what's happening — who's watching, what they're doing.

---

## Constraints

- **Backend**: Any language. We use Go, but use what you're confident in.
- **Frontend**: Any framework. We use SvelteKit, but React/Next/Nuxt/etc. are all fine.
- **Database**: SQLite is fine. No need for Postgres.
- **AI APIs**: We provide a unified API endpoint — no need to create your own accounts. See the **AI API Access** section below. You can also stub with mocks if you want to focus on architecture.
- **No auth required.** Skip login flows.

---

## AI API Access

We provide a single API endpoint that gives you access to multiple AI providers. No need to sign up for individual API keys.

**Base URL:** `https://staging.api.sunset.video`
**Auth:** Include your API key in the `Authorization` header.

### Available Models

| Provider | Model |
|----------|-------|
| Anthropic | `anthropic/claude-sonnet-4-5-20250929` |
| Anthropic | `anthropic/claude-haiku-4-5-20251001` |
| OpenAI | `openai/gpt-4o-2024-11-20` |
| OpenAI | `openai/gpt-4o-mini` |
| xAI | `xai/grok-2-latest` |

### Example Request

```bash
curl https://staging.api.sunset.video/api/v1/chat/completions \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "anthropic/claude-sonnet-4-5-20250929",
    "messages": [
      {"role": "user", "content": "Hello"}
    ]
  }'
```

### Response

```json
{
  "id": "chatcmpl-...",
  "model": "anthropic/claude-sonnet-4-5-20250929",
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "Hello! How can I help you?"
      }
    }
  ],
  "usage": {
    "prompt_tokens": 12,
    "completion_tokens": 8
  }
}
```

### Streaming

Add `"stream": true` to the request body to get Server-Sent Events:

```bash
curl https://staging.api.sunset.video/api/v1/chat/completions \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "anthropic/claude-sonnet-4-5-20250929",
    "messages": [{"role": "user", "content": "Count to 5"}],
    "stream": true
  }'
```

### Request Parameters

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `model` | string | yes | — | `provider/model-name` format |
| `messages` | array | yes | — | `[{role, content}]` |
| `temperature` | float | no | 1.0 | 0.0–2.0 |
| `max_tokens` | int | no | 4096 | Max response tokens |
| `stream` | bool | no | false | Enable SSE streaming |

### Multi-turn Conversations

Include the full history in `messages`:

```json
{
  "model": "anthropic/claude-haiku-4-5-20251001",
  "messages": [
    {"role": "user", "content": "My name is Jane"},
    {"role": "assistant", "content": "Nice to meet you, Jane!"},
    {"role": "user", "content": "What's my name?"}
  ]
}
```

### Text-to-Speech

The API also provides text-to-speech. You can generate spoken audio from text using any of the available voices.

#### List Available Voices

```bash
curl https://staging.api.sunset.video/api/v1/voices \
  -H "Authorization: Bearer YOUR_API_KEY"
```

Response:

```json
[
  {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "name": "aria",
    "display_name": "Aria",
    "description": "Warm and conversational"
  },
  {
    "id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
    "name": "river",
    "display_name": "River",
    "description": "Calm and clear"
  }
]
```

#### Generate Speech

Use a voice `id` from the listing endpoint:

```bash
curl https://staging.api.sunset.video/api/v1/audio/speech \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Hello, welcome to the experience.",
    "voice": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
  }' \
  --output speech.mp3
```

The response is raw audio bytes (`audio/mpeg`), not JSON — use `--output` to save to a file.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `input` | string | yes | Text to convert to speech |
| `voice` | string | yes | Voice UUID from `/api/v1/voices` |

---

## What We're Evaluating

We're not checking if it works perfectly. We're looking at how you think — the choices you make, what you prioritize, and what you leave out.

There's no checklist. We'll read your code and your README and form our own picture.

---

## Submission

Include a `README.md` with:
1. How to run it locally (must work with a single command or a small number of steps)
2. A short **decision log** — 3–5 bullet points on significant choices you made and why
3. What you'd do next with more time

---

## Bonus (not required)
- WebSocket for real-time session updates in the dashboard
- Multiple voice cue timecodes configurable without a code change
- Unit tests for the session recording logic

---

*Tip: We've seen candidates use AI coding tools to scaffold projects. That's fine — we expect it. What we're testing is whether you understand and own what gets built, and whether you can make the right calls when the AI gives you something generic.*
