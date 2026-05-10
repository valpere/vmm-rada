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

The `Strategy` enum carries **7 constants — all implemented**. The strategy roadmap is complete. See [`strategies.md`](./strategies.md) for the full per-strategy reference.

```go
type Strategy int

const (
    PeerReview         Strategy = iota // implemented (runner.go:runPeerReview)
    RoleBased                          // implemented (rolebased.go:runRoleBased)
    Majority                           // implemented (majority.go:runMajority)
    GenerateRankRefine                 // implemented (generaterankrefine.go:runGenerateRankRefine)
    MultiAgentDebate                   // implemented (debate.go:runMultiAgentDebate)
    MixtureOfAgents                    // implemented (moa.go:runMixtureOfAgents)
    Delphi                             // implemented (delphi.go:runDelphi)
)

type CouncilType struct {
    Name          string
    Strategy      Strategy
    Models        []string    // Council members. RoleBased assigns by index mod len. UNUSED for MixtureOfAgents.
    Roles         []Role      // RoleBased only.
    ChairmanModel string      // Synthesiser / arbiter / facilitator. UNUSED for MixtureOfAgents.
    Temperature   float64
    QuorumMin       int       // 0 = strategy-specific default formula.
    RefineTopK      int       // GenerateRankRefine only.
    MaxDebateRounds int       // MultiAgentDebate only.

    // MixtureOfAgents-only model fields. Models / ChairmanModel are UNUSED for MoA.
    ProposerModels   []string // MoA Layer 1
    AggregatorModels []string // MoA Layer 2 (parallel, all-to-all over Layer 1)
    RefinerModel     string   // MoA Layer 3 (single refiner)
}
```

`CouncilType` carries strategy-specific scalars too — `RefineTopK` for GenerateRankRefine, `MaxDebateRounds` for MultiAgentDebate, `MaxDelphiRounds` and `DelphiConvergenceThreshold` for Delphi. `MixtureOfAgents` is the only strategy to skip both `Models` and `ChairmanModel` — the runner reads the layer-specific fields directly. The full field-usage matrix (which fields each strategy reads) lives in `CouncilType`'s doc-comment in `internal/council/types.go`.

`RunFull()` is a strategy-dispatch switch — every shipped strategy has its own `case`:

```go
switch ct.Strategy {
case PeerReview:         return c.runPeerReview(...)
case RoleBased:          return c.runRoleBased(...)
case Majority:           return c.runMajority(...)
case GenerateRankRefine: return c.runGenerateRankRefine(...)
case MultiAgentDebate:   return c.runMultiAgentDebate(...)
case MixtureOfAgents:    return c.runMixtureOfAgents(...)
case Delphi:             return c.runDelphi(...)
default:                 return fmt.Errorf("council: strategy %d not implemented", ct.Strategy)
}
```

The `default` arm is unreachable today (every strategy has a case), but is retained for forward compatibility — adding a new `Strategy` constant without a corresponding case fails loudly at runtime.

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

#### `Majority` pipeline (2 stages, no LLM Stage 2)

Implemented in `internal/council/majority.go`. Best for factual QA, classification, and math.

1. **Stage 1** — `runStage1` (reused): all council models run concurrently. Anonymous `Response A`/`B`/… labels assigned via `assignLabels` for SSE consistency with PeerReview.
2. **Quorum check** — `runMajority` resolves `need := ct.QuorumMin; if need == 0 { need = max(3, ⌈N/2⌉+1) }` inline before calling `checkQuorumWith`. Need ≥3 successful answers by default to break ties cleanly.
3. **Vote tally** — `buildVoteTally`: pure function over Stage 1 results, no LLM call. Clusters answers by **exact-match after normalisation** (lowercase + trim + collapse internal whitespace). Cluster output is sorted by votes descending, then by representative ascending for stable ordering. Emits `Stage2CompleteData{Kind: "vote_tally", Results: [], Metadata: {..., VoteTally: ...}}`.
4. **Stage 3** — `runMajorityStage3` selects the final answer based on tie state and chairman config:

   | Tie? | Chairman set? | Result |
   |------|---------------|--------|
   | No   | No            | Winning cluster's `Representative` emitted verbatim. `StageThreeResult{Model: "", DurationMs: 0}` — empty `Model` is the documented "no LLM call" signal. |
   | No   | Yes           | Chairman polishes the winner via `BuildMajorityPolishPrompt` (refines prose only, must not change substance). |
   | Yes  | Yes           | Chairman picks among tied candidates via `BuildMajorityTiebreakPrompt`. |
   | Yes  | No            | **Loud error** — `runMajority` returns rather than picking arbitrarily. Matches the loud-failure pattern in Stage 0 (PR #204). |

Voting variants beyond exact-match (cluster-by-embedding, Borda count, Tournament/Elo, weighted voting) are explicit follow-ups. Registration is opt-in: the `"majority"` council type is added to the registry only when `MAJORITY_MODELS` is set in the environment.

#### `GenerateRankRefine` pipeline (3 stages, arbiter + refiner)

Implemented in `internal/council/generaterankrefine.go`. Best for creative writing, analysis, and code generation — tasks where diverse generation matters and per-criterion ranking gives more leverage than peer-review consensus.

1. **Stage 1** — `runStage1` (reused): all council models run concurrently. Anonymous `Response A`/`B`/… labels assigned via `assignLabels`.
2. **Quorum check** — `runGenerateRankRefine` resolves `need` inline before calling `checkQuorum`:
   - `k = max(ct.RefineTopK, 3)` (default `3`)
   - `need = max(k+1, 3)` when `QuorumMin == 0`
   - The `k+1` floor enforces "at least one rejection" — refining all candidates defeats the rank-to-filter point.
3. **Stage 2** — `runRankStage`: single LLM call to `ct.ChairmanModel` with `BuildRankPrompt`. The arbiter scores each candidate against four hardcoded criteria (`correctness`, `clarity`, `completeness`, `originality`) on `[0.0, 1.0]` per criterion, then computes a `total_score`. Per-criterion scores are clamped to `[0.0, 1.0]`; `total_score` is recomputed from clamped values and clamped to `[0.0, len(criteria)]` (defends against arbiter responses with internally-inconsistent or out-of-range numbers). Unknown labels are dropped with a warn log; missing criterion values default to `0.0` with a warn log. **Parse failures are loud errors** — Stage 2 IS the entire ranking; silent fall-through would leak unranked candidates into refinement.
4. **Sorting + advancing.** Rankings are sorted by `total_score` descending, then by `Label` ascending for stable output. Exactly `k` candidates are marked `Advancing: true`. Tie at the `k` boundary is resolved deterministically by the secondary `Label` sort — no rebalancing, no chairman tiebreak.
5. **Stage 2 SSE event:** emits `Stage2CompleteData{Kind: "rank_refine", Results: [], Metadata: {..., RankRefine: ...}}`. The `RankRefine` payload carries the full `Rankings`, `TopK`, and `Criteria`.
6. **Stage 3** — `runRefineStage`: single LLM call to `ct.ChairmanModel` with `BuildRankRefinePrompt`. The chairman receives the top-K candidates and is instructed to **refine, not blend** — pick strong threads, don't average. Failure path matches `runStage3` and `runRoleBasedStage3` (returns `StageThreeResult{Model, DurationMs, Error}` with the wrapped error).

Cost: **N + 2** LLM calls per request (N generation + 1 rank + 1 refine). Both the rank and refine calls go to `ct.ChairmanModel`. Splitting into separate `RankerModel`/`RefinerModel` fields is a future variant.

Registration is opt-in AND requires both `GENERATE_RANK_REFINE_MODELS` and `GENERATE_RANK_REFINE_CHAIRMAN_MODEL`. If models alone are set, the server logs a warning at startup and skips registration so requests fail-fast at startup rather than silently at request time.

#### `MultiAgentDebate` pipeline (multi-round, first to use stage2_round_complete)

Implemented in `internal/council/debate.go`. Best for reasoning, ethics, and strategy — tasks where critique reveals logical errors that single-shot generation misses. **First multi-round strategy** in the project, and the first to actually emit the `stage2_round_complete` SSE event type that #202 designed.

1. **Round 0 (Stage 1)** — `runStage1` reused; emits `stage1_complete` with anonymously-labelled successful results. `assignLabels` runs once and labels persist across all rounds.
2. **Quorum check** — `max(2, ⌈N/2⌉+1)` when `QuorumMin == 0`. Standard `*QuorumError` if round 0 fails.
3. **Rounds 1..R** — for each round, all surviving debaters run in parallel via `runDebateRound`. Each debater sees all OTHER debaters' previous-round answers (anonymised — labels only, never model names) plus their own previous-round output (so they revise rather than start from scratch). Output is JSON `{critique, revision}` produced via `BuildDebateRoundPrompt`. Per-round event: `stage2_round_complete` with `kind: "debate_round"` and `round: r`.
4. **Per-round failure handling.** A debater's call error, JSON parse failure, or empty revision drops them from round R+1 onwards. The runner records a `DebaterDropout` entry (`{label, last_round, reason}`) so the chairman + frontend can reason about the evolving cast.
5. **Per-round quorum re-check.** After every round, if survivors drop below `need`, `runMultiAgentDebate` returns `fmt.Errorf("multi-agent debate: quorum failed after round %d (...)")` — loud failure, no partial-progress fallback. Matches the loud-error ethos from #204 (Stage 0) and #206 (Majority ties).
6. **Stage 2 terminal event** — `stage2_complete` with `kind: "debate_round"`, `Metadata.Debate` populated with the full transcript: `Rounds[]` (one entry per completed round), `FinalRound`, and `Dropouts` (omitempty). The terminal event is authoritative on replay.
7. **Stage 3** — `runDebateStage3`: chairman synthesises across the whole transcript via `BuildDebateChairmanPrompt`. The chairman receives round 0 (`Stage1`), all rounds' revisions WITH model attribution (the `LabelToModel` map is included), and any dropout markers. Failure path matches `runStage3` and `runRoleBasedStage3` (returns `StageThreeResult{Model, DurationMs, Error}` even on error).

**Cost:** `N + N×R + 1` LLM calls per request (round 0 + R rounds × N debaters + chairman). With defaults N=4 R=2: **13 calls**. The most expensive shipped strategy.

**Anonymisation contract:** the per-round prompt MUST NOT contain model names — only labels. The Stage 3 chairman prompt does include the `LabelToModel` map so the chairman can attribute provenance in its synthesis.

**Single source of truth on the frontend:** `msg.metadata.debate`. The `stage2_round_complete` handler appends to `metadata.debate.rounds`; the terminal `stage2_complete` overwrites with the canonical state (which includes any dropouts populated by the runner). On replay, only `metadata.debate` is available — the same render path applies.

**Round 0 is not in `Debate.Rounds`.** It lives on `AssistantMessage.Stage1` (backend) and `msg.stage1` (frontend). Single source of truth per layer; the schema doesn't lie about what a "debate round" is.

Registration is opt-in AND requires both `DEBATE_MODELS` and `DEBATE_CHAIRMAN_MODEL`. `DEBATE_MAX_ROUNDS` is optional (default 2; invalid values warn and fall back to default).

#### `MixtureOfAgents` pipeline (3 layers)

Implemented in `internal/council/moa.go`. Modelled on the Together AI MoA paper. Best for code generation and complex multi-aspect analysis — tasks where structured layered aggregation beats flat consensus. **First strategy to require new `CouncilType` fields beyond `Models`/`ChairmanModel`** — the field-usage matrix in `internal/council/types.go` documents which strategies read which fields.

1. **Layer 1 (Stage 1)** — `runStage1` reused with `ct.ProposerModels`. Anonymous `Response A`/`B`/… labels assigned via `assignLabels`. Emits `stage1_complete`.
2. **Layer 1 quorum** — `max(2, ⌈N_proposers/2⌉+1)` when `QuorumMin == 0`. Standard `*QuorumError` if Layer 1 fails.
3. **Layer 2 (Stage 2)** — `runMoaLayer2`: parallel call per aggregator (`ct.AggregatorModels`), all-to-all fan-out (every aggregator sees every proposer). Aggregators get distinct labels via `assignAggregatorLabels` — `Aggregator A`/`B`/… — so they don't collide with proposer labels in `Metadata.LabelToModel` (a single flat map containing both prefix families). Output is free-form text — no JSON parsing required (the aggregator's job is to write a better draft, not to score).
4. **Layer 2 quorum** — at least 1 successful aggregator. If all aggregators fail, `runMixtureOfAgents` returns `fmt.Errorf("mixture-of-agents: all aggregators failed (%d configured); refiner has no input", ...)` — loud failure, no `stage2_complete` emitted, no Layer 3 run. Matches the loud-error ethos from #204 / #206 / #212.
5. **Stage 2 SSE event** — `stage2_complete` with `kind: "moa_aggregator"`, `Metadata.MoaAggregator` populated with `Aggregators[]` (sorted by `Label` asc). Each `AggregatorOutput` carries `{Label, Model, Content, Sources, DurationMs}`; `Sources` lists the Layer 1 proposer labels fed in (today: all-to-all, so every aggregator's `Sources` lists every successful proposer). **No `stage2_round_complete` events** — MoA is single-pass per layer.
6. **Layer 3 (Stage 3)** — `runMoaRefine`: single LLM call to `ct.RefinerModel` via `BuildMoaRefinerPrompt`. The refiner sees aggregator outputs WITH model attribution (label + model name, mirroring how PeerReview/Debate chairmen receive `LabelToModel`) but does NOT see raw proposer outputs — the aggregators have already digested them. Failure path matches `runStage3` (returns `StageThreeResult{Model, DurationMs, Error}` even on error).

**Cost:** `N + M + 1` LLM calls per request (N proposers + M aggregators + 1 refiner). With defaults N=4 M=2: **7 calls**. Sits between RoleBased (N+1) and the more expensive Debate / PeerReview strategies.

**Anonymisation contract:** the aggregator prompt MUST NOT contain proposer model names — only labels (`Response A`/…). The refiner prompt does include aggregator labels with model attribution.

**Single source of truth on the frontend:** `msg.metadata.moa_aggregator`. The terminal `stage2_complete` is authoritative; `MoaView` reads `msg.metadata?.moa_aggregator?.aggregators` for Layer 2 and `msg.stage1` for Layer 1.

**Out of scope (future variants):** round-robin aggregator fan-out (today is all-to-all per the MoA paper), aggregator role specialisation, refiner-as-pass-through, iterative MoA depth ≥ 2 aggregator layers.

Registration is opt-in AND requires **all three** `MOA_PROPOSER_MODELS` / `MOA_AGGREGATOR_MODELS` / `MOA_REFINER_MODEL` env vars. Partial config logs a warning and skips registration — there's no no-LLM path for MoA.

#### `Delphi` pipeline (multi-round, second to use `stage2_round_complete`)

Implemented in `internal/council/delphi.go`. Modelled on the Delphi method. Best for evaluation tasks, expert consensus problems, and forecasting — domains where rating-based feedback drives convergence faster than free-form critique. **Last unimplemented strategy in the roadmap;** post-merge no `Strategy` constants are reserved.

1. **Stage 1** — `runStage1` reused with `ct.Models`. Anonymous `Response A`/`B`/… labels via `assignLabels`. Quorum: `max(3, ⌈N/2⌉+1)` when `QuorumMin == 0` — higher floor than PeerReview/Debate (3 vs 2) because statistical averaging needs ≥3 raters.
2. **Rounds 1..R** — for each round, all surviving raters concurrently rate ALL Stage 1 candidates against `defaultDelphiCriteria` (`correctness`, `clarity`, `completeness`) via `runDelphiRound`. Output is JSON `{ratings: {<criterion>: <0.0..1.0>, ...}, summary: "<1–2 sentences>"}`. Per-rater LLM/parse/empty-scores failure drops that rater for the rest of the run; per-round event: `stage2_round_complete` with `kind: "delphi_round"` and `round: r`.
3. **Round-1 vs round R≥2 prompt asymmetry.** Round 1 prompts contain only the question + criteria + Stage 1 candidates (with anonymous labels). Round R≥2 prompts add (a) the full prior-round aggregate stats history (mean/stddev per criterion, and DeltaMean from round 2 onwards) and (b) the rater's OWN previous-round ratings + summary so it can revise rather than start from scratch. **Other raters' raw ratings are NEVER exposed to a rater** — the aggregate is the entire feedback signal. Cross-rater leakage would defeat the anonymous-blind property of the method.
4. **Per-round quorum re-check.** After every round, if `len(survivors) < need`, `runDelphi` returns `fmt.Errorf("delphi: quorum failed after round %d (%d survivors, need %d)")` — loud failure, no `stage2_complete` emitted, no Stage 3 run. Mirror Debate's pattern (#213).
5. **Convergence detection** (`computeDelphiStats`): pure function. Mean/StdDev populated only for criteria with ≥1 rating in the round; DeltaMean populated only for criteria present in BOTH the current and prior round (and absent on round 1). After round R≥2, if `max(DeltaMean) < ct.DelphiConvergenceThreshold` for ALL criteria in DeltaMean, exit early with `Converged: true`. Conservative — every criterion must converge; `mean` would let one stable criterion paper over a divergent one.
6. **Stage 2 terminal event** — `stage2_complete` with `kind: "delphi_round"`, `Metadata.DelphiPanel` populated with `Rounds[]`, `FinalRound`, `Converged`, and `Criteria`.
7. **Stage 3** — `runDelphiStage3`: chairman synthesises across the final-round per-rater ratings + summaries + converged stats via `BuildDelphiChairmanPrompt`. The chairman receives the LabelToModel map for candidate provenance attribution. Synthesis guidance scales with the `Converged` flag — converged → confident synthesis from the highest-rated candidate; not-converged → balanced presentation. Failure path matches `runStage3` (returns `StageThreeResult{Model, DurationMs, Error}` even on error).

**Cost:** `N + N×R + 1` worst case (no convergence). With defaults N=4 R=3: **17 calls**. Convergence at round 2 → 9 calls; at round 1 the strategy can't yet detect convergence.

**No `DelphiDropout` type.** Dropped raters are simply absent from subsequent rounds' `Ratings` slices; chairman and frontend infer dropout by label-set diff between rounds. Delphi's transcript is a sample (smaller `n`), not a narrative — typed dropout markers would invite the chairman prompt to over-explain.

**Single source of truth on the frontend:** `msg.metadata.delphi`. The `stage2_round_complete` handler (factored into `mergeRoundIntoMessage` in `App.jsx`) appends to `metadata.delphi.rounds` for `kind: "delphi_round"` events, mirroring how it handles `kind: "debate_round"` for Debate.

Registration is opt-in AND requires both `DELPHI_MODELS` and `DELPHI_CHAIRMAN_MODEL`. `DELPHI_MAX_ROUNDS` (default 3) and `DELPHI_CONVERGENCE_THRESHOLD` (default 0.1; valid range `(0.0, 1.0)`) are optional; invalid values warn and fall back to defaults.

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
