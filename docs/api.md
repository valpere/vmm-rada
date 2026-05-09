# LLM Council — HTTP API Reference

## Base URL

The server listens on port `PORT` (default `8001`) and binds on all interfaces (`0.0.0.0`)
by default. For local development: `http://localhost:{PORT}`. From other machines:
`http://<host>:{PORT}`.

The frontend dev server (Vite at `:5173`) proxies `/api` requests to the backend, so no
additional CORS headers are needed during development when using that proxy.

---

## CORS

Allowed origins (hardcoded):

- `http://localhost:5173`
- `http://localhost:3000`

When the request `Origin` header matches, the server reflects it and sets:

```
Access-Control-Allow-Origin: <origin>
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type
Vary: Origin
```

Preflight `OPTIONS` requests return `204 No Content`.

---

## Request limits

- **Body size:** 1 MiB (`http.MaxBytesReader`). Requests exceeding this limit receive `400 Bad Request`.

---

## Error reference

### Pre-SSE HTTP errors

Returned before the SSE stream is established (before `Content-Type: text/event-stream`
is written), so a proper HTTP status code is always possible.

**Shape:**

```json
{ "error": "human-readable message" }
```

| Failure | Status | `error` message |
|---------|--------|----------------|
| Invalid conversation ID format | `400` | `"invalid conversation id"` |
| Malformed or missing request body | `400` | `"invalid request body"` |
| Conversation not found | `404` | `"not found"` |
| Council quorum not met | `503` | `"council quorum not met"` |
| Review: one or more roles failed (all 4 required) | `503` | `"council quorum not met"` |
| Storage failure (pre-pipeline) | `500` | `"internal server error"` |
| SSE streaming not supported by server | `500` | `"streaming not supported"` |
| Round-N with already-answered round | `409` | `"clarification round already answered"` |
| Round-N with no pending clarification round | `409` | `"no pending clarification round"` |

### SSE error events

Once SSE is established (HTTP `200`, `Content-Type: text/event-stream`), the HTTP
status code is locked. Errors are communicated as SSE events; the stream terminates
immediately after the error event. No `complete` event follows.

**Shape:**

```json
{ "type": "error", "message": "human-readable message" }
```

| Failure | `message` |
|---------|-----------|
| Stage 1 quorum not met | `"council quorum not met"` |
| Stage 3 Chairman LLM failure | `"internal server error"` |
| Storage failure saving assistant message | `"internal server error"` |

When Stage 1 returns fewer than M_min successful responses, the handler logs
`Got`/`Need` at `WARN` level and emits the error event. No `stage2_complete` or
`stage3_complete` events are emitted before it. The user message may already have
been persisted; no assistant message is saved.

### Partial results

Not returned in the current implementation. On any pipeline failure the client
receives only the SSE error event; no stage outputs from the failed run are
persisted.

---

## Routes

### `GET /health/live`

Liveness probe.

**Response `200 OK`** — empty body.

---

### `GET /health/ready`

Readiness probe.

**Response `200 OK`** — empty body.

---

### `GET /api/conversations`

List all conversations, newest first.

**Response `200 OK`**

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "created_at": "2024-01-15T10:30:00Z",
    "title": "Explain the trolley problem",
    "message_count": 4
  }
]
```

Returns `[]` (empty array) when no conversations exist.

---

### `POST /api/conversations`

Create a new conversation.

**Request** — no body required.

**Response `201 Created`**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "created_at": "2024-01-15T10:30:00Z",
  "title": "New Conversation",
  "messages": []
}
```

---

### `GET /api/conversations/{id}`

Fetch a conversation with its full message history.

**Path parameter** — `id`: UUID v4.

**Response `200 OK`**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "created_at": "2024-01-15T10:30:00Z",
  "title": "Explain the trolley problem",
  "messages": [
    { "role": "user", "content": "Explain the trolley problem" },
    {
      "role": "assistant",
      "stage1": [ /* StageOneResult[] */ ],
      "stage2": [ /* StageTwoResult[] */ ],
      "stage3": { /* StageThreeResult */ },
      "metadata": { /* Metadata */ }
    }
  ]
}
```

`messages` is a heterogeneous array. Demux by the `"role"` field:
- `"user"` — `{ role, content }`
- `"assistant"` — `{ role, stage1, stage2, stage3, metadata }`

**Errors:** `400` (invalid UUID), `404` (not found).

---

### `POST /api/conversations/{id}/message`

Send a message and receive the full deliberation result in a single JSON response (blocking — waits for all three stages to complete).

**Path parameter** — `id`: UUID v4.

**Request body**

```json
{
  "content": "Explain the trolley problem",
  "council_type": "default"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `content` | string | yes | The user's message |
| `council_type` | string | no | Council strategy name; defaults to `DEFAULT_COUNCIL_TYPE` env var |

> **Planned (issue #154):** When Stage 0 clarification is enabled, the request body is XOR — supply exactly one of `content` (round 1) or `answers` (round 2+). Both present, or neither, returns `400`. The `council_type` for round 2+ is loaded from storage; do not re-send it.
>
> ```json
> // Round 2+ (answering clarification questions)
> { "answers": [ {"id": "q1", "text": "MySQL 5.7"}, {"id": "q2", "text": ""} ] }
> ```

**Response `200 OK`** — `AssistantMessage`

```json
{
  "role": "assistant",
  "stage1": [
    {
      "label": "Response A",
      "content": "...",
      "model": "openai/gpt-4o-mini",
      "duration_ms": 1240
    }
  ],
  "stage2": [
    {
      "reviewer_label": "Response B",
      "rankings": ["Response A", "Response C", "Response B"]
    }
  ],
  "stage3": {
    "content": "...",
    "model": "openai/gpt-4o-mini",
    "duration_ms": 890
  },
  "metadata": {
    "council_type": "default",
    "label_to_model": { "Response A": "openai/gpt-4o-mini", "Response B": "..." },
    "aggregate_rankings": [
      { "model": "openai/gpt-4o-mini", "score": 1.5 }
    ],
    "consensus_w": 0.83
  }
}
```

**Errors:** `400` (invalid body/UUID), `404` (not found), `503` (quorum not met), `500`.

---

### `POST /api/conversations/{id}/message/stream`

Send a message and receive the deliberation result as a Server-Sent Events stream.
Each event is flushed immediately as the stage completes — no polling required.

**Path parameter** — `id`: UUID v4.

**Request body** — same as `/message`.


**Response headers**

```
Content-Type: text/event-stream
Cache-Control: no-cache
X-Accel-Buffering: no
```

**SSE event format**

Every event is a single `data:` line followed by a blank line:

```
data: {"type":"<event_type>",...}\n\n
```

There is no `event:` line — demux by the `"type"` field of the JSON payload.

---

## SSE event sequence

```
data: {"type":"stage0_round_complete","data":{"round":1,"questions":[...]}}   ← stream closes here (Stage 0 enabled)
  … client submits answers via new POST …
data: {"type":"stage0_done"}                                                  ← Stage 1 follows

data: {"type":"stage1_complete","data":[...StageOneResult]}
data: {"type":"stage2_complete","data":[...StageTwoResult],"metadata":{...Metadata}}
data: {"type":"stage3_complete","data":{...StageThreeResult}}
data: {"type":"title_complete","data":{"title":"..."}}     ← may be absent if title generation times out
data: {"type":"complete"}
```

When Stage 0 is disabled (`CLARIFICATION_MAX_ROUNDS=0`, the default), no `stage0_*` events are emitted and the sequence is unchanged.

On failure at any point:

```
data: {"type":"error","message":"human-readable message"}
```

After an error event the stream ends. No `complete` event follows.

### `stage0_round_complete`

Emitted when the chairman has questions for the user. The SSE stream **closes** after this event. The client must open a new stream with `{answers:[...]}` to continue.

```json
{
  "type": "stage0_round_complete",
  "data": {
    "round": 1,
    "questions": [
      {"id": "q1", "text": "What database are you currently using and at what scale?"},
      {"id": "q2", "text": "What is prompting this migration?"}
    ]
  }
}
```

### `stage0_done`

Emitted when the Stage 0 loop ends — chairman said "enough", limits were reached, or the user submitted all-empty answers. `stage1_complete` follows immediately on the same stream.

```json
{ "type": "stage0_done" }
```

### `stage1_complete`

Emitted when all council models have responded in Stage 1.

```json
{
  "type": "stage1_complete",
  "data": [
    {
      "label": "Response A",
      "content": "...",
      "model": "openai/gpt-4o-mini",
      "duration_ms": 1240
    },
    {
      "label": "Response B",
      "content": "...",
      "model": "anthropic/claude-haiku-4-5",
      "duration_ms": 980
    }
  ]
}
```

Labels are assigned sequentially (`A`, `B`, `C`, …). The mapping of label → model is
revealed in `metadata.label_to_model` at `stage2_complete`.

### `stage2_complete`

Emitted when all peer-review rankings are computed. `metadata` is a **top-level field**
on the event object, not nested inside `data`.

```json
{
  "type": "stage2_complete",
  "data": [
    {
      "reviewer_label": "Response B",
      "rankings": ["Response A", "Response C", "Response B"]
    }
  ],
  "metadata": {
    "council_type": "default",
    "label_to_model": {
      "Response A": "openai/gpt-4o-mini",
      "Response B": "anthropic/claude-haiku-4-5"
    },
    "aggregate_rankings": [
      { "model": "openai/gpt-4o-mini", "score": 1.5 },
      { "model": "anthropic/claude-haiku-4-5", "score": 2.5 }
    ],
    "consensus_w": 0.83
  }
}
```

`aggregate_rankings` are sorted by `score` ascending (lower = better rank).
`consensus_w` is a 0–1 weight indicating agreement across reviewers.

### `stage3_complete`

Emitted when the Chairman model has synthesised the final answer.

```json
{
  "type": "stage3_complete",
  "data": {
    "content": "The trolley problem is a thought experiment...",
    "model": "openai/gpt-4o-mini",
    "duration_ms": 1100
  }
}
```

### `title_complete`

Emitted after `stage3_complete` when title generation succeeds. May be **absent** if
title generation times out (30-second deadline). The title is derived from the first
50 **bytes** of the Stage 3 response — responses containing multi-byte UTF-8 characters
may be cut mid-character.

```json
{
  "type": "title_complete",
  "data": { "title": "The trolley problem is a thought experimen" }
}
```

### `complete`

Signals the end of the stream. No payload.

```json
{ "type": "complete" }
```

### `error`

Emitted when the pipeline fails. Stream ends after this event.

```json
{ "type": "error", "message": "council quorum not met" }
```

---

## Type reference

### `StageOneResult`

| Field | Type | Description |
|-------|------|-------------|
| `label` | string | Anonymised label, e.g. `"Response A"` |
| `content` | string | Model's answer |
| `model` | string | OpenRouter model ID |
| `duration_ms` | number | Wall-clock time for this model's response |

### `StageTwoResult`

| Field | Type | Description |
|-------|------|-------------|
| `reviewer_label` | string | Label of the reviewing model |
| `rankings` | string[] | Labels ordered best-first |

### `StageThreeResult`

| Field | Type | Description |
|-------|------|-------------|
| `content` | string | Chairman's synthesised answer |
| `model` | string | OpenRouter model ID |
| `duration_ms` | number | Wall-clock time |

### `Metadata`

| Field | Type | Description |
|-------|------|-------------|
| `council_type` | string | Strategy name used for this run |
| `label_to_model` | object | Maps label → OpenRouter model ID |
| `aggregate_rankings` | `RankedModel[]` | Models sorted by aggregate score (ascending) |
| `consensus_w` | number | Consensus weight 0–1 |

### `RankedModel`

| Field | Type | Description |
|-------|------|-------------|
| `model` | string | OpenRouter model ID |
| `score` | number | Aggregate rank score (lower = ranked higher overall) |

### `TitleData`

| Field | Type | Description |
|-------|------|-------------|
| `title` | string | First 50 bytes of the Stage 3 response, used as the conversation title |

### `ClarificationQuestion`

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Stable identifier (e.g. `"q1"`) — use this as the `id` in answer submissions |
| `text` | string | Question text from the chairman (rendered via `react-markdown` in the UI) |
