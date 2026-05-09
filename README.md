# LLM Council

A multi-LLM deliberation system. Rather than asking a single AI model for an
answer, LLM Council assembles a council of models that independently respond,
anonymously review each other, and have a designated Chairman synthesize a
final answer.

---

## How it works

```
User query
    │
    ▼
┌─────────────────────────────────────┐
│ Stage 1 — Individual Responses      │
│ All council models answer in        │
│ parallel, unaware of each other     │
└──────────────────┬──────────────────┘
                   │
                   ▼
┌─────────────────────────────────────┐
│ Stage 2 — Anonymized Peer Review    │
│ Each model ranks the others'        │
│ responses labeled A–Z               │
│ (labels are shuffled to avoid bias) │
└──────────────────┬──────────────────┘
                   │
                   ▼
┌─────────────────────────────────────┐
│ Stage 3 — Chairman Synthesis        │
│ One designated model reads all      │
│ responses and rankings, then        │
│ writes the definitive final answer  │
└──────────────────┬──────────────────┘
                   │
                   ▼
              Final answer
```

**Why three stages?**

- **Stage 1** surfaces diverse perspectives — each model answers independently,
  so you get genuinely different approaches rather than one model's blind spots.
- **Stage 2** adds a credibility signal — peers evaluate each other without
  knowing authorship, producing a bias-reduced quality ranking ("street cred").
- **Stage 3** leverages the Chairman's synthesis ability — a single model reads
  all responses *and* the peer rankings, giving it the full picture to write the
  best possible answer.

---

## Tech stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26+ |
| HTTP server | `net/http` (stdlib) |
| Concurrency | `sync.WaitGroup` + per-conversation `sync.Mutex` (stdlib) |
| Streaming | Server-Sent Events over `net/http` |
| Storage | JSON files on disk (no database) |
| LLM gateway | [OpenRouter](https://openrouter.ai) REST API |
| Config | Environment variables + `godotenv` |
| ID generation | `crypto/rand` (stdlib) |
| Frontend | React 19 + Vite 8 (`frontend/` directory) |

---

## Prerequisites

- **Go 1.26+**
- **Node.js 20+** (for the frontend)
- An **[OpenRouter](https://openrouter.ai) API key**

---

## Quick start

```bash
# 1. Clone and enter the repo
git clone git@github.com:valpere/llm-council.git
cd llm-council

# 2. Configure the backend
cp .env.example .env
# Edit .env — set OPENROUTER_API_KEY and choose your council models

# 3. Run the backend
make dev
# → LLM Council API listening on :8001
```

In a second terminal, start the frontend:

```bash
# 4. Install frontend deps (first time only)
cd frontend && npm ci

# 5. Start the frontend dev server
make fr-dev
# → http://localhost:5173
```

The Vite dev server proxies all `/api` requests to the backend at `:8001`
automatically — no extra config needed.

---

## Configuration

All configuration is done via environment variables. Copy `.env.example` to
`.env` to get started.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `OPENROUTER_API_KEY` | **Yes** | — | Your OpenRouter API key. |
| `COUNCIL_MODELS` | No | 3 small dev fallbacks¹ | Comma-separated list of OpenRouter model IDs for council members. |
| `CHAIRMAN_MODEL` | No | `openai/gpt-4o-mini`¹ | Model used for Stage 3 synthesis. |
| `DEFAULT_COUNCIL_TYPE` | No | `default` | Council pipeline variant (`default` = PeerReview). |
| `DEFAULT_COUNCIL_TEMPERATURE` | No | `0.7` | Sampling temperature for all LLM calls (0.0–2.0). |
| `DATA_DIR` | No | `data/conversations` | Directory where conversation JSON files are stored. |
| `PORT` | No | `8001` | TCP port the server listens on. |

¹ When `COUNCIL_MODELS` or `CHAIRMAN_MODEL` are unset the server logs a warning
and falls back to small, inexpensive models suitable for local development only.
See `.env.example` for recommended production values.

For the frontend, see `frontend/.env.example`.

---

## Development

```bash
# Backend
make build      # Compile to bin/llm-council
make dev        # Run without compiling (go run ./cmd/server)
make lint       # go vet + staticcheck
make test       # go test -race -count=1 ./...
make clean      # Remove bin/llm-council

# Frontend
make fr-dev     # Vite dev server on localhost:5173
make fr-build   # Production build to frontend/dist/
make fr-lint    # ESLint
```

> Always run `make` commands from the **project root**.
> The server resolves `DATA_DIR` relative to the working directory.

---

## API reference

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health/live` | Liveness probe — always returns 200 with an empty body if the process is up |
| `GET` | `/health/ready` | Readiness probe — always returns 200 with an empty body (no dependency check today) |
| `GET` | `/api/conversations` | List all conversations |
| `POST` | `/api/conversations` | Create a new conversation |
| `GET` | `/api/conversations/{id}` | Get a conversation with all messages |
| `POST` | `/api/conversations/{id}/message` | Send a message, wait for the full result |
| `POST` | `/api/conversations/{id}/message/stream` | Send a message, receive stage results via SSE |

### Streaming events (`/message/stream`)

Uses [Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events).
Each event is a `data:` line with a JSON object containing a `type` field:

```
data: {"type":"stage1_start"}
data: {"type":"stage1_complete","data":[...]}
data: {"type":"stage2_start"}
data: {"type":"stage2_complete","data":[...],"metadata":{"label_to_model":{...},"aggregate_rankings":[...],"consensus_w":0.72}}
data: {"type":"stage3_start"}
data: {"type":"stage3_complete","data":{...}}
data: {"type":"title_complete","data":{"title":"..."}}
data: {"type":"complete"}
```

On any error: `data: {"type":"error","message":"..."}` — the stream then closes.

### Conversation storage format

Each conversation is a single JSON file under `DATA_DIR`:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "created_at": "2025-01-01T12:00:00Z",
  "title": "My conversation",
  "messages": [
    { "role": "user", "content": "What is the best sorting algorithm?" },
    { "role": "assistant", "stage1": [...], "stage2": [...], "stage3": {...} }
  ]
}
```

---

## Project structure

```
llm-council/
├── cmd/server/main.go            Entry point — wires config → client → council → storage → api
├── internal/
│   ├── config/config.go          Config struct and Load() from environment variables
│   ├── openrouter/client.go      HTTP client for OpenRouter (parallel and single queries)
│   ├── council/
│   │   ├── runner.go             RunFull() dispatcher + PeerReview pipeline stages
│   │   ├── rolebased.go          RoleBased pipeline
│   │   ├── review_roles.go       DefaultReviewRoles + NewCodeReviewCouncilType
│   │   ├── prompts.go            Prompt builders for all pipeline stages
│   │   └── types.go              Result types: CouncilType, Strategy, Role, StageOneResult, etc.
│   ├── storage/storage.go        JSON file persistence with atomic writes and per-conv locks
│   └── api/handler.go            HTTP handlers, CORS middleware, SSE streaming
├── frontend/                     React 19 + Vite 8 single-page app (see below)
├── docs/                         Architecture, stage logic, and implementation notes
├── data/conversations/           Created at runtime — one JSON file per conversation
├── Makefile
├── .env.example                  Template for all supported backend environment variables
└── go.mod                        Module: llm-council, Go 1.26+
```

### Frontend structure

```
frontend/
├── src/
│   ├── api.js                    API adapter — all fetch/SSE calls; only file that talks to the backend
│   ├── App.jsx                   Root component; owns all application state
│   ├── theme.css                 CSS design tokens (dark/light themes via data-theme attribute)
│   ├── index.css                 Global styles and typography
│   └── components/
│       ├── ChatInterface.jsx/css  Main chat view + always-visible input
│       ├── Sidebar.jsx/css        Collapsible conversation list with theme toggle
│       ├── Stage1.jsx/css         Individual responses — collapsed accordion
│       ├── Stage2.jsx/css         Peer rankings — collapsed accordion with consensus score
│       ├── Stage3.jsx/css         Final answer — always-expanded hero card
│       ├── EmptyState.jsx/css     Welcome screen with suggested prompt chips
│       └── Markdown.jsx           react-markdown + rehype-highlight wrapper
├── index.html
├── vite.config.js
└── package.json
```

---

## Design notes

**Minimal dependencies.** The server uses only the Go standard library for HTTP,
JSON, concurrency, file I/O, and UUID generation (`crypto/rand`). The only
external package is `github.com/joho/godotenv` to load `.env` files.

**Atomic storage.** Conversation files are written to a `.tmp` file and then
renamed into place, so a crash mid-write never leaves a corrupt file. Concurrent
writes to the same conversation are serialized with a per-conversation mutex.

**Bias-free peer review.** Stage 2 labels (A–Z) are assigned to a *shuffled*
order of Stage 1 results each request, so no model is systematically favored by
always being "Response A". The label-to-model mapping is ephemeral — computed per
request and returned in the API response, but never persisted.

**Graceful degradation.** If one council model fails in Stage 1 or Stage 2, the
pipeline continues with the successful responses. On total Stage 1 failure the
streaming endpoint emits a `{"type":"error",...}` SSE event and closes the stream.

**Frontend architecture rules.** Components are pure UI — no `fetch` calls
inside components. `api.js` is the sole adapter layer. `App.jsx` owns all state.
`react-markdown` is the only renderer for LLM output (XSS risk with raw HTML).

---

## License

[MIT](LICENSE)
