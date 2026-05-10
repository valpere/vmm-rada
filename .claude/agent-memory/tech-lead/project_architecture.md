---
name: project_architecture
description: LLM Council Go backend — module map, key design decisions, established conventions
type: project
last-verified: 2026-05-10
---

Go backend for LLM Council, a 3-stage multi-LLM deliberation system.

**Why:** Python/FastAPI original was rewritten to Go for performance and deployment simplicity.
**How to apply:** Architecture decisions must remain consistent with the modular monolith pattern already established.

## Module layout

```
cmd/server/main.go            — wire-up + graceful shutdown (SIGINT/SIGTERM, 10s drain)
cmd/eval/main.go              — evaluation harness CLI (council vs single-model quality)
internal/config/config.go     — Config struct, Load() from env; supports Stage 0 clarification overrides
internal/openrouter/client.go — Concrete LLM gateway; implements council.LLMClient interface
internal/council/interfaces.go — LLMClient / Runner / Stage0Runner interfaces (DI seam)
internal/council/types.go     — Domain types: CouncilType, Stage*Result, EventFunc, CompletionRequest/Response
internal/council/council.go   — Council struct; main 3-stage pipeline (peer review)
internal/council/runner.go    — Runner orchestration + Strategy dispatch switch (3 implemented, 4 planned); structured logging via slog
internal/council/rolebased.go — Role-based 2-stage pipeline (Stage 1 + Stage 3, no peer review)
internal/council/majority.go  — Majority strategy (vote-tally, opt-in via MAJORITY_MODELS); Stage 2 emits kind="vote_tally"
internal/council/prompts.go   — Prompt templates (per-stage, per-strategy)
internal/council/rankings.go  — Stage 2 ranking aggregation (PeerReview's Kendall's W)
internal/storage/storage.go   — JSON file store; atomic writes; per-conv mutex via sync.Map; UUID validation via regex
internal/api/handler.go       — HTTP handlers + SSE streaming; CORS + security middleware via wrap()
internal/eval/                — Eval harness: judge, report, eval runner; LLM-as-judge for council quality
```

## Established conventions

- **Atomic file writes:** write to `{id}.json.tmp`, then `os.Rename`
- **Per-conversation locking:** `sync.Map` of `*sync.Mutex`, acquired via `lockConv(id)`
- **UUID-validated IDs** before any file path construction (path traversal prevention).
  Validation is via regex (`uuidRE`), no `github.com/google/uuid` dependency.
- **Request body capped at 1MB** via `http.MaxBytesReader` (handler.go:178, 387)
- **CORS:** still hardcoded to `localhost:5173` and `localhost:3000` (`var allowedOrigins`
  in `internal/api/handler.go:24`). **Not yet configurable via env** — known limitation.
- **Security headers:** `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Content-Security-Policy: default-src 'none'` set on every response by `wrap()`.
- **Data dir permissions:** 0700; file permissions: 0600
- **SSE events:** `data: {...}\n\n` with `type` field; no SSE `event:` line
- **Structured logging via `log/slog`** — used in 8 files: `cmd/server/main.go`,
  `cmd/eval/main.go`, `internal/api/handler.go`, `internal/config/config.go`,
  `internal/council/runner.go`, `internal/eval/eval.go`, `internal/openrouter/client.go`,
  `internal/storage/storage.go`. NOT stdlib `log` package.
- **Tests:** 14 Go test files across api/council/config/eval/openrouter/storage.
  Real file I/O (temp dirs), not mocks per `copilot-instructions.md`. Frontend
  has a Vitest harness as well (2 spec files).
- **Linting:** `go vet ./...` is the gate; staticcheck/golangci-lint not yet integrated.
- **External deps:** only `github.com/joho/godotenv v1.5.1`. Go 1.26.3.

## Key design decisions

- **Interfaces over concrete types** for council dependencies: `LLMClient`, `Runner`,
  `Stage0Runner` (in `interfaces.go`). Lets handlers depend on abstraction; tests
  use real implementations against temp dirs.
- **labelToModel mapping is ephemeral** — not persisted, only in API response
- **Stage 2 capped at 26 council members** (A-Z label limit)
- **Strategy enum** (`internal/council/types.go`) declares 7 constants. **Three
  implemented today:** `PeerReview` (3-stage Karpathy peer review), `RoleBased`
  (2-stage roles → chairman; emits a minimal `Stage2CompleteData` stub), and
  `Majority` (parallel generation → vote tally, no LLM Stage 2; opt-in registration
  via `MAJORITY_MODELS`). **Four reserved/planned:** `GenerateRankRefine`,
  `MultiAgentDebate`, `MixtureOfAgents`, `Delphi` — runner returns
  `"strategy %d not implemented"`. Stage 2 carries a `kind` discriminator
  (`peer_ranking`, `role_stub`, `vote_tally`, plus reserved kinds for the planned
  strategies) so future strategies ship without touching shared SSE code.
- **Stage 0 clarification loop** (configurable via `CLARIFICATION_*` env vars).
  `ClarificationMaxRounds=0` disables the feature.
  Stage 0 model overrides are intentionally NOT pre-filled from default council
  models — runner resolves the fall-through chain at request time.
- **Title generation** runs concurrently with RunFull/Stage1 to avoid blocking.
  Look in current `runner.go` / `council.go` for the active model selection;
  the previous "hardcoded `google/gemini-2.5-flash`" claim no longer matches.
- **Graceful shutdown:** `cmd/server/main.go:93-118` — `signal.NotifyContext`
  catches SIGINT/SIGTERM, then `srv.Shutdown(ctx)` with 10s timeout.
- **Retry policy:** `LLMAPIMaxRetries` config (default 2, total 3 attempts) on
  transient OpenRouter failures (429/502/503/504, timeouts, EOFs).

## What changed since 2026-03-14 (previous version of this file)

This memory was updated and expanded on 2026-05-10 from current code state,
after a dreaming pass (W19) flagged 4 false claims:
- ❌ "No tests exist yet" → ✅ 14 Go test files (+ frontend Vitest harness)
- ❌ "No structured logging; stdlib log only" → ✅ `log/slog` in 8 files
- ❌ "No graceful shutdown" → ✅ `srv.Shutdown` with signal trap
- ❌ "GenerateTitle uses hardcoded `google/gemini-2.5-flash`" → claim no longer
  matches code; verify in current `runner.go` / `council.go` if working on titles.

Two additional drifts found while rewriting:
- Module layout grew significantly (eval package, rolebased pipeline, Stage 0
  clarification, interfaces.go); old module list was misleadingly minimal.
- `github.com/google/uuid` dep was removed; UUIDs validated via regex now.

CORS hardcoding was also flagged as "false (PR #31 made it configurable)" by
dreaming, but verification against actual code shows the hardcoded map is still
in place. Treat dreaming reports as evidence, not as truth — verify against
current code before believing claims.
