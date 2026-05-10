# LLM Council — Architecture (v2)

> This document reflects the current v2 codebase. The v1 implementation is preserved on `archive/v1`.

---

## Overview

LLM Council is a multi-LLM deliberation system. A set of council models independently
answer a user query, anonymously peer-review each other's answers, and a Chairman model
synthesises a final response. The result streams to the browser over Server-Sent Events.

```
Browser (React + Vite)
    │  SSE / JSON over HTTP
    ▼
Go HTTP server  (:8001)
    │
    ├── internal/api        — HTTP handlers + SSE streaming
    ├── internal/council    — 3-stage deliberation pipeline
    ├── internal/openrouter — LLM gateway client
    ├── internal/storage    — JSON file persistence
    └── internal/config     — environment variable loading
```

---

## Backend

### Package layout

| Package | File | Responsibility |
|---------|------|----------------|
| `cmd/server` | `main.go` | Composition root — wires all concrete types, starts HTTP server |
| `internal/config` | `config.go` | Reads and validates env vars; returns `*Config` |
| `internal/openrouter` | `client.go` | `LLMClient` implementation; POSTs to OpenRouter (or compatible) API |
| `internal/council` | `types.go` | Shared types: `CouncilType`, `Strategy`, `StageOneResult`, `StageTwoResult`, `StageThreeResult`, `Metadata`, `EventFunc` |
| `internal/council` | `runner.go` | `Council` struct; `RunFull()` strategy dispatch; `runPeerReview` + Stage 1/2/3 helpers |
| `internal/council` | `rolebased.go` | `runRoleBased` — 2-stage roles → chairman pipeline |
| `internal/council` | `council.go` | Helpers: `checkQuorum()`, `assignLabels()`, `QuorumError` |
| `internal/council` | `rankings.go` | `CalculateAggregateRankings()` — Kendall's W consensus coefficient |
| `internal/council` | `prompts.go` | Prompt builder functions for each stage |
| `internal/storage` | `storage.go` | `Storer` interface + `Store` implementation; JSON file I/O |
| `internal/api` | `handler.go` | HTTP handlers, CORS middleware, SSE streaming; `RegisterRoutes()` |

### Layer boundaries

```
cmd/server/main.go
    ├── imports all internal/* packages (composition root only)

internal/api
    ├── may import: internal/council, internal/storage (currently direct; interface refactor in progress)
    └── must not import: internal/openrouter

internal/council
    └── must not import: net/http, internal/storage, internal/api, internal/openrouter

internal/storage
    ├── may import: internal/council (for council.AssistantMessage persistence type)
    └── must not import: net/http, internal/openrouter, internal/api

internal/openrouter
    ├── may import: internal/council (for council.CompletionRequest/Response and LLMClient interface)
    └── must not import: internal/storage, internal/api

internal/config
    └── must not import any other internal/* package
```

`internal/api` currently imports `internal/council` and `internal/storage` directly.
Moving these behind consumer-defined interfaces is an in-progress refactor target —
do not add new direct coupling beyond what already exists.

Interfaces are defined at the **consumer boundary**:
- `council.LLMClient` — defined in `internal/council`, consumed by `internal/council`, implemented by `internal/openrouter`
- `council.Runner` — defined in `internal/council`, consumed by `internal/api`
- `storage.Storer` — defined in `internal/storage`, consumed by `internal/api`

Every interface implementation has a compile-time assertion:

```go
var _ council.LLMClient = (*openrouter.Client)(nil)
var _ council.Runner    = (*council.Council)(nil)
```

### Composition root (`cmd/server/main.go`)

`main.go` is the only place that wires concrete types:

```
config.Load()
    → openrouter.NewClient(apiKey, baseURL, timeout)
    → council.NewCouncil(client, registry, logger)
    → storage.NewStore(dataDir, logger)
    → api.NewHandler(runner, store, logger, councilType)
    → http.Server{Addr, Handler: mux}
```

This keeps all dependency injection in one place and makes each package independently testable.

### Council pipeline (`internal/council`)

The `Strategy` enum carries **7 constants** — 2 are implemented today, 5 are reserved for planned strategies. See [`strategies.md`](./strategies.md) for the full roadmap.

```go
type Strategy int

const (
    PeerReview         Strategy = iota // implemented (runner.go:runPeerReview)
    RoleBased                          // implemented (rolebased.go:runRoleBased)
    Majority                           // not implemented
    GenerateRankRefine                 // not implemented
    MultiAgentDebate                   // not implemented
    MixtureOfAgents                    // not implemented
    Delphi                             // not implemented
)

type CouncilType struct {
    Name          string
    Strategy      Strategy
    Models        []string    // Council members. RoleBased assigns by index mod len.
    Roles         []Role      // RoleBased only.
    ChairmanModel string      // Synthesiser (PeerReview, RoleBased) / arbiter / facilitator.
    Temperature   float64
    QuorumMin     int         // 0 = strategy-specific default formula.
}
```

`RunFull()` is a strategy-dispatch switch:

```go
switch ct.Strategy {
case PeerReview: return c.runPeerReview(...)
case RoleBased:  return c.runRoleBased(...)
default:         return fmt.Errorf("council: strategy %d not implemented", ct.Strategy)
}
```

#### `PeerReview` pipeline (3 stages)

1. **Stage 1** — `runStage1`: all council models run concurrently (`sync.WaitGroup`); each writes to a pre-allocated result slot (no mutex needed)
2. **Quorum check** — `checkQuorum`: requires `max(2, ⌈N/2⌉+1)` successful responses; returns `*QuorumError` if not met
3. **Label assignment** — `assignLabels`: shuffles models into anonymous labels (`Response A`, `Response B`, …) using `math/rand.Perm`; labels are per-request so reviewers cannot identify each other
4. **Stage 2** — `runStage2`: all successful Stage 1 models run concurrently as peer reviewers; each receives the full set of anonymised responses and returns a ranked ordering as JSON
5. **Rankings** — `CalculateAggregateRankings`: computes aggregate rank scores and Kendall's W consensus coefficient
6. **Stage 3** — `runStage3`: single call to the Chairman model; synthesises a final answer using the peer rankings

#### `RoleBased` pipeline (2 stages, Stage 2 stub)

1. **Stage 1** — `runRoleBasedStage1`: roles run concurrently; model assignment is `ct.Models[i % len(ct.Models)]`. Labels are role names, not anonymised.
2. **Quorum check** — same `checkQuorum`. `QuorumMin` is typically set to `len(Roles)` so every specialist must succeed; the runner does not enforce this — it's a registration-time choice.
3. **Stage 2** — skipped. A minimal `Stage2CompleteData{Results:[], Metadata:{AggregateRankings:[], ConsensusW:1.0}}` event is emitted for SSE compatibility.
4. **Stage 3** — `runRoleBasedStage3`: chairman synthesises across all role findings.

#### Per-registration model configuration

Every `CouncilType` registration is independent. Two registrations with the same `Strategy` but different `Models` / `ChairmanModel` are valid — e.g. `"factual-majority"` and `"creative-majority"` both use `Strategy: Majority` with different voter pools. Each strategy has its own namespaced env var family (`MAJORITY_MODELS`, `MAJORITY_CHAIRMAN_MODEL`, `DEBATE_MODELS`, etc.) with fall-through to `COUNCIL_MODELS` / `CHAIRMAN_MODEL` when unset; see [`strategies.md`](./strategies.md) for the full table. Plumbing lands with each strategy's implementation PR.

#### Stage 0 (clarification) — strategy-independent

Stage 0 runs before any strategy dispatch. It is independent of the `Strategy` value and has its own dedicated model configuration:

- `CLARIFICATION_MODELS` (env) — comma-separated generator pool. Single-model `CLARIFICATION_MODELS=foo/bar` is the common case (a cheap fast model usually suffices for clarifying-question generation).
- `CLARIFICATION_ARBITER_MODEL` (env) — single model that dedupes generator candidates, prioritises, and decides whether to actually ask.

Both env vars are optional. The runner resolves a two-step fall-through chain at request time:

```
generator models : cfg.Models     → ct.Models        → error
arbiter model    : cfg.ArbiterModel → ct.ChairmanModel → error
```

The error is loud — `RunClarificationRound` returns explicitly rather than emitting `stage0_done`. This catches misconfiguration (e.g. a future strategy with no `ChairmanModel`, registered without setting `CLARIFICATION_ARBITER_MODEL`).

The config loader does **not** pre-fill the Stage 0 fields from `COUNCIL_MODELS` / `CHAIRMAN_MODEL`; it leaves them empty and lets the runner do the resolution. This preserves the per-council-type fall-back hop, which is what "no existing deployments break" actually means.

### Storage (`internal/storage`)

Conversations are persisted as JSON files: one file per conversation under `DATA_DIR`
(default `data/conversations/`).

Key design constraints:

| Constraint | Detail |
|-----------|--------|
| **Atomic writes** | Writes go to `{id}.json.tmp`, then `os.Rename` → `{id}.json`. `rename(2)` is atomic on Linux. Never write to the target file directly. |
| **Store-level locking** | A single `sync.RWMutex` on `Store` serialises all write operations (`Lock`); reads use `RLock`. |
| **File permissions** | Data directory: `0700`. Conversation files: `0600`. |
| **UUID v4 IDs** | Generated with `crypto/rand`; no external uuid package. |

### HTTP layer (`internal/api`)

| Constraint | Detail |
|-----------|--------|
| **Request body limit** | `http.MaxBytesReader(w, r.Body, 1<<20)` (1 MiB) applied before `json.Decode` |
| **UUID validation** | Path parameter `{id}` validated against `^[0-9a-f]{8}-...-4...-[89ab]...$` before any storage call |
| **SSE format** | `data: {...}\n\n` — no `event:` line; demux by `"type"` field |
| **CORS** | Allowed origins hardcoded: `http://localhost:5173`, `http://localhost:3000`; `Vary: Origin` set when reflecting |
| **Security headers** | `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Content-Security-Policy: default-src 'none'` applied to every route |

---

## Frontend

**Stack:** React 19, Vite 8, plain JavaScript (no TypeScript).  
**Directory:** `frontend/`

### Architecture rules (immutable)

These four rules are enforced in every code review — any violation must be flagged:

1. **Components under `frontend/src/components/` are pure UI.** They must not call `fetch`, `XMLHttpRequest`, or import `api.js`.
2. **`src/api.js` is the sole network boundary.** `App.jsx` is the only file that may import it. HTTP status codes and raw SSE lines never leak past this module. The only interface `App.jsx` sees is `onEvent(type, event)`.
3. **`App.jsx` owns all state.** Only `App.jsx` calls `setCurrentConversation` / `setConversations`. State flows down via props; events flow up via callbacks.
4. **`react-markdown` is the only renderer for LLM output.** Injecting raw HTML directly into the DOM is forbidden — LLM-generated content is untrusted and must go through the Markdown component.

### Component hierarchy

```
App.jsx                     — root; owns all application state
├── Sidebar.jsx             — conversation list, new-conversation button, theme toggle
│   └── Sidebar.css
└── ChatInterface.jsx       — message thread + always-visible input form
    ├── EmptyState.jsx      — welcome screen with prompt chips (shown when no messages)
    ├── Stage1.jsx          — accordion: individual model responses (collapsed by default)
    ├── Stage2.jsx          — accordion: peer rankings + consensus badge (collapsed by default)
    ├── Stage3.jsx          — hero card: chairman synthesis (always expanded)
    └── Markdown.jsx        — shared react-markdown wrapper with rehype-highlight
```

### State shape (`App.jsx`)

`currentConversation.messages` is a flat array. Each element is either a user message or
an assistant message:

```js
// user message
{ role: 'user', content: '...' }

// assistant message (progressive — fields fill in as SSE events arrive)
{
  role: 'assistant',
  stage1: null | StageOneResult[],
  stage2: null | StageTwoResult[],
  stage3: null | StageThreeResult,
  metadata: null | Metadata,
  loading: { stage1: true, stage2: false, stage3: false },
  error: null | string,
}
```

`loading.stage1` starts as `true` when the assistant message is first created (before any
SSE events) so the Stage 1 spinner renders immediately. Each field is set by the
corresponding SSE event handler in `App.jsx`.

### Theme system

Design tokens live in `frontend/src/theme.css` as CSS custom properties on `:root` (dark
default) and `[data-theme="light"]`. No hardcoded colour values are permitted in component
CSS files — use `var(--token)` only.

The active theme is stored in `App.jsx` state, persisted in `localStorage`, and applied by
setting `document.documentElement.dataset.theme` via `useEffect`.

### Dev proxy

`vite.config.js` reads `PORT` from the root `.env` via Vite's `loadEnv` and configures a
proxy so `/api` requests from the browser are forwarded to `http://localhost:{PORT}`. This
means CORS headers are not needed during local development. `VITE_API_BASE` is only used
for cross-origin production deployments.

---

## Data flow

### Sending a message (streaming path)

```
User types a message and presses Enter
    │
    ▼
App.jsx: onSendMessage(content)
    │  adds user message + empty assistant message (loading.stage1=true) to state
    ▼
api.js: sendMessageStream(conversationId, content, councilType, onEvent)
    │  POST /api/conversations/{id}/message/stream
    ▼
handler.go: sendMessageStream
    │  saves user message to storage
    │  calls council.RunFull(ctx, query, councilType, onEvent)
    │      │
    │      ├── Stage 1 (parallel LLM calls) → emits stage1_complete → SSE flush
    │      ├── Stage 2 (parallel peer review) → emits stage2_complete → SSE flush
    │      └── Stage 3 (chairman synthesis) → emits stage3_complete → SSE flush
    │  saves assistant message to storage
    │  emits title_complete (first 50 bytes of Stage 3) → SSE flush
    │  emits complete → SSE flush
    ▼
api.js: onEvent callback fires for each SSE event
    ▼
App.jsx: sseHandlers[eventType](event)
    │  updates currentConversation.messages[last] in place via functional setState
    │  loading.stage1/2/3 cleared as each *_complete arrives
    ▼
React re-render → Stage1/Stage2/Stage3 components receive new props
```

### Conversation persistence

Each conversation is stored as a single JSON file:

```
data/conversations/
└── {uuid}.json          — { id, created_at, title, messages: [...] }
```

`messages` is `[]json.RawMessage` so heterogeneous user/assistant objects survive
round-trips without loss of type information. The `role` field identifies the type.

Writes are atomic: the file is serialised to `{uuid}.json.tmp` then renamed to
`{uuid}.json`. The store-level `sync.RWMutex` prevents concurrent write corruption.
