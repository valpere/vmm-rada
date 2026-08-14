---
name: project_architecture
description: VMM Rada Go backend — module map, key design decisions, established conventions
type: project
last-verified: 2026-08-14
---

Go backend for VMM Rada, a 3-stage multi-LLM deliberation system.

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
internal/council/council.go   — Rada struct; main 3-stage pipeline (peer review)
internal/council/runner.go    — Runner orchestration + Strategy dispatch switch (all 7 strategies implemented); structured logging via slog
internal/council/rolebased.go — Role-based 2-stage pipeline (Stage 1 + Stage 3, no peer review); 5 named roles
internal/council/roles.go     — DefaultRoles (Creator/Critic/Verifier/Simplifier/DevilsAdvocate), RolesWithModels()
internal/council/majority.go  — Majority strategy (vote-tally, no LLM Stage 2); Stage 2 emits kind="vote_tally"
internal/council/generaterankrefine.go — GenerateRankRefine strategy
internal/council/debate.go    — MultiAgentDebate strategy
internal/council/moa.go       — MixtureOfAgents strategy (proposers → aggregators → refiner)
internal/council/delphi.go    — Delphi strategy (multi-round convergence)
internal/council/strategy_wiring_test.go — AST-derived invariant: every Strategy const must be
  registered or exempted; also asserts configs/council.yaml covers all 7
internal/council/prompts.go   — Prompt templates (per-stage, per-strategy)
internal/council/rankings.go  — Stage 2 ranking aggregation (PeerReview's Kendall's W)
internal/config/registry.go      — BuildRegistry(): YAML-first, env-fallback, shared by cmd/server + cmd/eval
internal/config/registry_yaml.go — configs/council.yaml parser (yaml.v3, KnownFields strict)
internal/config/registry_env.go  — legacy per-strategy env-var registry (fallback only)
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
- **External deps:** `github.com/joho/godotenv v1.5.1` and `gopkg.in/yaml.v3` (council
  registry config, promoted to direct dep in PR #323). Go 1.26.5.

## Key design decisions

- **Interfaces over concrete types** for council dependencies: `LLMClient`, `Runner`,
  `Stage0Runner` (in `interfaces.go`). Lets handlers depend on abstraction; tests
  use real implementations against temp dirs.
- **labelToModel mapping is ephemeral** — not persisted, only in API response
- **Stage 2 capped at 26 council members** (A-Z label limit)
- **Strategy enum** (`internal/council/types.go`) declares 7 constants — **all 7
  implemented and registered** (as of PR #323, closing a gap PR #303 had already
  started closing for `RoleBased`): `PeerReview` (3-stage Karpathy peer review),
  `RoleBased` (2-stage, 5 named roles → chairman), `Majority` (parallel generation →
  vote tally, no LLM Stage 2), `GenerateRankRefine`, `MultiAgentDebate`,
  `MixtureOfAgents` (proposers → aggregators → refiner), `Delphi` (multi-round
  convergence). Registration is **YAML-first** — `configs/council.yaml` (parsed by
  `internal/config/registry_yaml.go`, path via `COUNCIL_CONFIG_PATH`) builds the
  entire registry when present; per-strategy env vars (`MAJORITY_MODELS`,
  `ROLE_BASED_MODELS`, etc.) are fallback-only, used only when the YAML file is
  absent. `TestAllStrategiesRegisteredOrExempted` (AST-derived from the enum) and
  `TestShippedConfigCoversAllStrategies` both gate against silent registration gaps
  — the class of bug that left `RoleBased` unreachable for months and `cmd/eval`
  missing it even longer. Stage 2 carries a `kind` discriminator (`peer_ranking`,
  `role_stub`, `vote_tally`, plus one kind per remaining strategy) so strategies ship
  without touching shared SSE code.
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

## What changed since 2026-05-10 (previous version of this file)

Rewritten 2026-08-14 (dreaming pass W32, §4) — this file had drifted badly:
strategy count (claimed 3 implemented + 4 planned; verified 7/7 via
`internal/council/types.go` Strategy enum + `configs/council.yaml`'s 7
`strategy:` entries), Go version (claimed 1.26.3; `go.mod` says 1.26.5), and
module layout (missing `strategy_wiring_test.go`, `roles.go`,
`generaterankrefine.go`, `debate.go`, `moa.go`, `delphi.go`,
`internal/config/registry*.go`). All facts above re-verified against current
code/`go.mod`/`configs/council.yaml` at rewrite time, not carried forward from
the stale version.
