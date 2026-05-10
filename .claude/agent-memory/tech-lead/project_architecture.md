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
internal/council/runner.go    — Runner orchestration, structured logging via slog
internal/council/rolebased.go — Role-based 2-stage pipeline (Stage 1 + Stage 3, no peer review)
internal/council/prompts.go   — Prompt templates (per-stage)
internal/council/rankings.go  — Stage 2 ranking aggregation
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
- **Structured logging via `log/slog`** — used in `runner.go`, `eval.go`, `server/main.go`,
  `handler.go`, `storage.go`. NOT stdlib `log` package.
- **Tests:** 17 test files across api/council/eval/storage. Real file I/O (temp dirs),
  not mocks per `copilot-instructions.md`. Frontend has Vitest harness too.
- **Linting:** `go vet ./...` is the gate; staticcheck/golangci-lint not yet integrated.
- **External deps:** only `github.com/joho/godotenv v1.5.1`. Go 1.26.3.

## Key design decisions

- **Interfaces over concrete types** for council dependencies: `LLMClient`, `Runner`,
  `Stage0Runner` (in `interfaces.go`). Lets handlers depend on abstraction; tests
  use real implementations against temp dirs.
- **labelToModel mapping is ephemeral** — not persisted, only in API response
- **Stage 2 capped at 26 council members** (A-Z label limit)
- **Role-based pipeline** (Stage 1 + Stage 3) for use cases that don't need peer
  review; emits a minimal Stage2CompleteData event for SSE compatibility (`rolebased.go`)
- **Stage 0 clarification loop** (configurable via `CLARIFICATION_*` env vars).
  `ClarificationMaxRounds=0` disables the feature.
  Stage 0 model overrides are intentionally NOT pre-filled from default council
  models — runner resolves the fall-through chain at request time.
- **Title generation** runs concurrently with RunFull/Stage1 to avoid blocking.
  Look in current `runner.go` / `council.go` for the active model selection;
  the previous "hardcoded `google/gemini-2.5-flash`" claim no longer matches.
- **Graceful shutdown:** `cmd/server/main.go:74-100` — SIGINT/SIGTERM handler
  cancels context, then `srv.Shutdown(ctx)` with 10s timeout.
- **Retry policy:** `LLMAPIMaxRetries` config (default 2, total 3 attempts) on
  transient OpenRouter failures (429/502/503/504, timeouts, EOFs).

## What changed since 2026-03-14 (previous version of this file)

This memory was updated and expanded on 2026-05-10 from current code state,
after a dreaming pass (W19) flagged 4 false claims:
- ❌ "No tests exist yet" → ✅ 17 test files
- ❌ "No structured logging; stdlib log only" → ✅ `log/slog` in 5 modules
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
