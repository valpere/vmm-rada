# VMM Rada — User Guide

VMM Rada is a backend API that runs a **3-stage multi-model deliberation pipeline**. Instead of asking one AI for an answer, it asks a council of models, has them anonymously peer-review each other, and uses a designated Chairman to synthesize a final answer from all inputs.

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Configuration](#configuration)
3. [Running the Server](#running-the-server)
4. [API Reference](#api-reference)
5. [Using the Streaming Endpoint](#using-the-streaming-endpoint)
6. [Understanding the Response](#understanding-the-response)
7. [Health Checks](#health-checks)
8. [Data Storage](#data-storage)
9. [Integration Notes](#integration-notes)

---

## Quick Start

### Prerequisites

- Go 1.26 or later
- An [OpenRouter](https://openrouter.ai) API key with credits

### Setup

```bash
# Clone and enter the repo
cd vmm-rada

# Create .env with your API key
echo "AI_PROVIDER_API_KEY=sk-or-v1-..." > .env

# Run (must be from repo root)
go run ./cmd/server
```

The server starts on port **8001** by default. The frontend (in the `frontend/` directory) connects to this port.

---

## Configuration

All configuration is via environment variables. The server reads from `.env` at startup via the shell — there is no built-in `.env` loader; use `source .env` or a runner that handles it (the frontend dev proxy sets this up automatically).

| Variable | Default | Description |
|----------|---------|-------------|
| `AI_PROVIDER_API_KEY` | *(required)* | Your OpenRouter API key. The server refuses to start if this is not set. |
| `RADA_MODELS` | See below | Comma-separated list of model IDs to use as council members. |
| `CHAIRMAN_MODEL` | `openai/gpt-4o-mini` | Model that synthesizes the final answer in Stage 3. |
| `DEFAULT_RADA_TEMPERATURE` | `0.7` | Sampling temperature for council and chairman calls. |
| `PORT` | `8001` | TCP port the HTTP server listens on. |
| `DATA_DIR` | `data/conversations` | Directory where conversation JSON files are stored. Relative to the working directory. |
| `LLM_API_BASE_URL` | *(optional)* | Override the OpenRouter API base URL. Must be an absolute `http`/`https` URL. Useful for pointing at a compatible local proxy. |
| `CLARIFICATION_MAX_ROUNDS` | `2` | Max clarification rounds before Stage 1. Set to `0` to disable Stage 0 entirely — the API then behaves identically to before Stage 0. |
| `CLARIFICATION_MAX_TOTAL_QUESTIONS` | `5` | Hard cap on questions accumulated across all rounds in one query. |
| `CLARIFICATION_MAX_QUESTIONS_PER_ROUND` | `3` | Chairman trims to this many questions per round. |

### Default council models

```
openai/gpt-4o-mini
anthropic/claude-haiku-4-5
google/gemini-flash-1.5
```

### Custom council example

```bash
RADA_MODELS="openai/gpt-4o,anthropic/claude-3-5-sonnet,google/gemini-flash-1.5" \
CHAIRMAN_MODEL="openai/gpt-4o" \
go run ./cmd/server
```

Any model available on OpenRouter can be used. The council works best with at least 3 models.

---

## Running the Server

```bash
# Development (from repo root)
go run ./cmd/server

# Build and run
go build -o vmm-rada ./cmd/server
./vmm-rada

# With custom config
AI_PROVIDER_API_KEY=sk-or-v1-... PORT=9000 go run ./cmd/server
```

**Important:** Always run from the repo root, not from `cmd/server/`. The default data directory (`data/conversations`) is relative to the working directory.

On startup the server logs its configuration to stdout as structured JSON:

```json
{"time":"...","level":"INFO","msg":"server starting","port":"8001"}
```

---

## API Reference

Base URL: `http://localhost:8001`

### Conversations

#### `GET /api/conversations`

Returns all conversations, sorted by creation time (newest first).

**Response** `200 OK`:
```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "created_at": "2026-03-22T09:00:00Z",
    "title": "What is the Fermi paradox?",
    "message_count": 2
  }
]
```

Returns `[]` when no conversations exist.

---

#### `POST /api/conversations`

Creates a new empty conversation.

**Response** `201 Created`:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "created_at": "2026-03-22T09:00:00Z",
  "title": "New Conversation",
  "messages": []
}
```

---

#### `GET /api/conversations/{id}`

Returns a full conversation including all messages.

**Response** `200 OK`:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "created_at": "2026-03-22T09:00:00Z",
  "title": "What is the Fermi paradox?",
  "messages": [...]
}
```

Returns `404` if the conversation does not exist.

---

#### `POST /api/conversations/{id}/message`

Sends a message and waits for the full 3-stage pipeline to complete before responding. Use this for simple integrations that do not need streaming.

**Request body**:
```json
{ "content": "What is the Fermi paradox?" }
```

**Response** `200 OK` — see [Understanding the Response](#understanding-the-response).

---

#### `POST /api/conversations/{id}/message/stream`

Sends a message and streams stage events as Server-Sent Events (SSE). Use this for UIs that want to show progressive updates.

**Request body**: same as `/message`

**Response**: `text/event-stream` — see [Using the Streaming Endpoint](#using-the-streaming-endpoint).

---

### Error responses

All error responses use the same shape:

```json
{ "error": "description of what went wrong" }
```

| Status | When |
|--------|------|
| `400` | Malformed JSON body or request too large (> 1 MB) |
| `404` | Conversation not found |
| `503` | Rada quorum not met (too many models failed) |
| `500` | Internal error (storage failure) |

---

## Using the Streaming Endpoint

The streaming endpoint emits [Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events). Each event is a `data:` line containing a JSON object with a `type` field.

### Event sequence

```
→ POST /api/conversations/{id}/message/stream

← data: {"type":"stage0_round_complete","data":{"round":1,"questions":[...]}}   ← planned; stream closes
  → POST /api/conversations/{id}/message/stream  {"answers":[...]}              ← planned; client re-opens
← data: {"type":"stage0_done"}                                                  ← planned; Stage 1 follows

← data: {"type":"stage1_complete","data":[...]}

← data: {"type":"stage2_complete","data":[...],"metadata":{...}}

← data: {"type":"stage3_complete","data":{...}}

← data: {"type":"title_complete","data":{"title":"..."}}   ← first message only; may be absent
← data: {"type":"complete"}
```

The `stage0_*` events are only emitted when `CLARIFICATION_MAX_ROUNDS > 0`, which is the case under the default (`CLARIFICATION_MAX_ROUNDS=2`). Set the variable to `0` to skip Stage 0 entirely; the sequence then starts at `stage1_complete`.

There are no `*_start` events — the client receives each stage result only when it is fully complete.

### Event payloads

#### `stage1_complete`

```json
{
  "type": "stage1_complete",
  "data": [
    { "label": "Response A", "content": "The Fermi paradox is...", "model": "openai/gpt-4o-mini", "duration_ms": 1240 },
    { "label": "Response B", "content": "Named after Enrico Fermi...", "model": "anthropic/claude-haiku-4-5", "duration_ms": 980 },
    { "label": "Response C", "content": "...", "model": "google/gemini-flash-1.5", "duration_ms": 1100 }
  ]
}
```

Labels (`Response A`, `Response B`, …) are randomly assigned per request — the same model will get a different label on each run.

#### `stage2_complete`

```json
{
  "type": "stage2_complete",
  "data": [
    { "reviewer_label": "Response A", "rankings": ["Response C", "Response B", "Response A"] },
    { "reviewer_label": "Response B", "rankings": ["Response C", "Response A", "Response B"] }
  ],
  "metadata": {
    "council_type": "default",
    "label_to_model": {
      "Response A": "openai/gpt-4o-mini",
      "Response B": "anthropic/claude-haiku-4-5",
      "Response C": "google/gemini-flash-1.5"
    },
    "aggregate_rankings": [
      { "model": "google/gemini-flash-1.5", "score": 1.0 },
      { "model": "anthropic/claude-haiku-4-5", "score": 1.5 },
      { "model": "openai/gpt-4o-mini", "score": 2.5 }
    ],
    "consensus_w": 0.72
  }
}
```

`aggregate_rankings` is sorted ascending by `score` (lower = ranked higher). `consensus_w` is Kendall's W coefficient (0–1): ≥ 0.7 indicates strong agreement among reviewers.

#### `stage3_complete`

```json
{
  "type": "stage3_complete",
  "data": {
    "content": "The Fermi paradox, named after physicist Enrico Fermi, asks...",
    "model": "openai/gpt-4o-mini",
    "duration_ms": 890
  }
}
```

#### `title_complete` *(first message in a conversation only)*

```json
{ "type": "title_complete", "data": { "title": "The Fermi paradox, named after phys" } }
```

The title is the first 50 bytes of the Stage 3 response. It may be absent if generation does not complete within the 30-second deadline.

#### `error`

```json
{ "type": "error", "message": "council quorum not met" }
```

An `error` event means the pipeline failed mid-stream. The stream ends immediately after this event — no `complete` event follows.

### Consuming SSE in JavaScript

```js
const response = await fetch(`/api/conversations/${id}/message/stream`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ content: 'What is the Fermi paradox?' }),
});

const reader = response.body.getReader();
const decoder = new TextDecoder();
let buffer = '';

while (true) {
  const { done, value } = await reader.read();
  if (done) break;
  buffer += decoder.decode(value, { stream: true });
  const lines = buffer.split('\n');
  buffer = lines.pop(); // keep incomplete line
  for (const line of lines) {
    if (!line.startsWith('data: ')) continue;
    const event = JSON.parse(line.slice(6));
    switch (event.type) {
      case 'stage1_complete': console.log('Stage 1:', event.data); break;
      case 'stage3_complete': console.log('Final:', event.data.content); break;
      case 'complete':        console.log('Done'); break;
    }
  }
}
```

---

## Understanding the Response

Both `/message` and `/message/stream` (after `complete`) represent the same data shape:

```json
{
  "role": "assistant",
  "stage1": [
    { "label": "Response A", "content": "...", "model": "openai/gpt-4o-mini", "duration_ms": 1240 },
    ...
  ],
  "stage2": [
    { "reviewer_label": "Response B", "rankings": ["Response A", "Response C", "Response B"] },
    ...
  ],
  "stage3": {
    "content": "...",
    "model": "openai/gpt-4o-mini",
    "duration_ms": 890
  },
  "metadata": {
    "council_type": "default",
    "label_to_model": { "Response A": "openai/gpt-4o-mini", ... },
    "aggregate_rankings": [
      { "model": "openai/gpt-4o-mini", "score": 1.5 }
    ],
    "consensus_w": 0.72
  }
}
```

### Consensus W (Kendall's W)

`metadata.consensus_w` measures inter-reviewer agreement on the rankings (0 = no agreement, 1 = perfect agreement):

| Value | Interpretation |
|-------|---------------|
| ≥ 0.7 | **Strong consensus** — reviewers agree clearly on quality order |
| 0.4–0.7 | **Moderate consensus** — partial agreement |
| < 0.4 | **Weak consensus** — reviewers disagree significantly |

The Chairman model receives the consensus level as part of its synthesis prompt and adjusts its tone accordingly — a strong consensus lets the Chairman speak with more confidence about which response was best.

### Aggregate rankings

`metadata.aggregate_rankings` lists models sorted by aggregate score across all reviewers (lower = better). Use this to see which model the council collectively preferred for this query.

---

## Stage 0: Clarification

When `CLARIFICATION_MAX_ROUNDS` is greater than `0`, the pipeline runs a clarification loop *before* Stage 1. Each council model identifies ambiguities, contradictions, and unstated assumptions in the user's query and proposes questions. The Chairman consolidates and prioritises them (up to `CLARIFICATION_MAX_QUESTIONS_PER_ROUND` per round), then either sends them to the user or skips to generation if enough context exists.

The user answers via a follow-up request with `{ "answers": [{"id": "q1", "text": "..."}, ...] }`. This repeats until the Chairman decides the context is sufficient, limits are reached, or the user submits all-empty answers to skip Stage 0.

Once the loop ends, the original query plus all Q/A history is passed to Stage 1 as an augmented prompt — the existing 3-stage deliberation pipeline runs unchanged.

| Setting | Recommendation |
|---------|---------------|
| `CLARIFICATION_MAX_ROUNDS=0` | Disabled — Stage 0 skipped entirely, API behaves as before Stage 0 was added |
| `CLARIFICATION_MAX_ROUNDS=1` | Single clarification round before generation |
| `CLARIFICATION_MAX_ROUNDS=2` | Default — multi-round; good for complex or ambiguous queries |
| `CLARIFICATION_MAX_ROUNDS=3` | Maximum useful depth — diminishing returns past this point |

Set `CLARIFICATION_MAX_ROUNDS=0` to disable Stage 0 and keep the pre-Stage-0 behaviour with no API changes.

---

## Health Checks

| Endpoint | Status | When |
|----------|--------|------|
| `GET /health/live` | `200` | Always — process is alive |
| `GET /health/ready` | `200` | Always — empty body |

Use `/health/live` for liveness probes and `/health/ready` for readiness probes in container orchestration.

```bash
curl http://localhost:8001/health/live
# 200 OK (empty body)
```

---

## Data Storage

Conversations are stored as individual JSON files:

```
data/conversations/
  550e8400-e29b-41d4-a716-446655440000.json
  7c9e6679-7425-40de-944b-e07fc1f90ae7.json
  ...
```

Each file is a self-contained JSON object with the full conversation including all messages and stage results. Files are written atomically (write to `.tmp`, then rename) to prevent corruption on crash.

### Backup

Simply copy the `data/conversations/` directory. Each file is independent.

### Changing the data directory

```bash
DATA_DIR=/var/lib/vmm-rada/conversations go run ./cmd/server
```

The directory is created automatically on first use with permissions `0700`.

---

## Integration Notes

### CORS

The server allows cross-origin requests from these hardcoded origins:

- `http://localhost:5173` (Vite dev server)
- `http://localhost:3000`

CORS origins are not configurable via environment variable. For production deployments on a different origin, modify `allowedOrigins` in `internal/api/handler.go`.

Preflight `OPTIONS` requests are handled automatically.

### Request size limit

Request bodies are limited to **1 MB**. Requests exceeding this return `400 Bad Request`.

### Model timeouts

Each individual model query has a **120-second timeout**. If a model does not respond within 120 seconds, it is skipped and the pipeline continues with successful responses. The overall request does not fail unless the quorum minimum is not met.

### Title generation

After every response, the server truncates the Stage 3 content to 50 runes, saves it as the conversation title, and emits a `title_complete` SSE event before `complete`. The derivation is an in-memory string operation, so the 30-second fallback timeout is effectively unreachable in practice.

### Structured logs

The server logs to stdout as structured JSON using `log/slog`. Log level is `INFO` by default. Errors during request handling are logged at `ERROR`; minor issues (like a client disconnecting during a write) at `WARN`.

```json
{"time":"2026-03-22T09:00:00Z","level":"INFO","msg":"server starting","port":"8001"}
{"time":"2026-03-22T09:00:05Z","level":"WARN","msg":"writeJSON: encode failed","status":200,"error":"write tcp: broken pipe"}
```

## Frontend Usage

See [`docs/frontend/user-guide.md`](frontend/user-guide.md) for the end-user UI walkthrough.
