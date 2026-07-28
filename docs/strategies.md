# Deliberation Strategies

The `Strategy` enum (`internal/council/types.go`) declares **7 constants — all implemented**. The strategy roadmap is complete.

For architecture context (package layout, layer boundaries, dispatch switch) see [`architecture-v2.md`](./architecture-v2.md). For the academic background of each strategy see [`council-research-synthesis.md`](./council-research-synthesis.md). For three hand-picked test prompts per strategy see [`strategy-showcase.md`](./strategy-showcase.md) (machine-readable: `eval/benchmarks/strategy-showcase.yaml`).

Stage 0 (clarification) runs **before** strategy dispatch and is strategy-independent — see the [Stage 0 section in architecture-v2.md](./architecture-v2.md#stage-0-clarification--strategy-independent). It has its own dedicated model configuration (`CLARIFICATION_MODELS`, `CLARIFICATION_ARBITER_MODEL`); see `.env.example`. Both env vars are optional and fall back to the council type's `Models` / `ChairmanModel` when unset.

---

## Status

| Strategy | Status | Pipeline file | Implementation PR |
|----------|--------|---------------|-------------------|
| `PeerReview` | shipped | `runner.go:runPeerReview` | initial |
| `RoleBased` | shipped | `rolebased.go:runRoleBased` | #177, registration #302 |
| `Majority` | shipped | `majority.go:runMajority` | #205 |
| `GenerateRankRefine` | shipped | `generaterankrefine.go:runGenerateRankRefine` | #210 |
| `MultiAgentDebate` | shipped | `debate.go:runMultiAgentDebate` | #212 |
| `MixtureOfAgents` | shipped | `moa.go:runMixtureOfAgents` | #214 |
| `Delphi` | shipped | `delphi.go:runDelphi` | #216 |

`RoleBased` was implemented and tested well before it was registered — see #302 for the
gap and its resolution. It is registered under the `"role-based"` council type name with
a fixed, generic role set (`council.DefaultRoles`: Creator, Critic, Verifier, Simplifier,
DevilsAdvocate — see [`council-research-synthesis.md §2.7`](./council-research-synthesis.md)).
This is unrelated to the old code-review-specific `/review*` endpoints removed in PR #199
(see [What's not here](#whats-not-here) below) — that was a different, narrower
4-role set behind dedicated routes; this is a generic strategy reachable through the
same strategy-agnostic REST API as every other strategy.

### LLM-call cost (per request, ignoring Stage 0)

Operators pick strategies on cost as much as on output quality. For a council of N members:

| Strategy | LLM calls | Notes |
|----------|-----------|-------|
| `PeerReview` | **2N + 1** | N generation + N peer review + 1 chairman synthesis |
| `RoleBased` | **N + 1** | N role outputs + 1 chairman synthesis |
| `Majority` | **N + (0 or 1)** | N generation; 0 chairman calls when there's a clear plurality and no chairman is configured; 1 for tiebreak or polish |
| `GenerateRankRefine` | **N + 2** | N generation + 1 ranking + 1 refinement (both go to the chairman) |
| `MultiAgentDebate` | **N + N×R + 1** | N generation + R rounds × N debaters revising + 1 chairman synthesis. With defaults N=4, R=2 → 13 calls. The most expensive shipped strategy; cost note in `.env.example`. |
| `MixtureOfAgents` | **N + M + 1** | N proposers (Layer 1) + M aggregators (Layer 2, all-to-all over proposers) + 1 refiner (Layer 3). With defaults N=4, M=2 → 7 calls. |
| `Delphi` | **N + N×R + 1** (worst case) | N generation (Stage 1) + R rounds × N raters + 1 chairman synthesis. Strategy may exit early on convergence (max(DeltaMean) < threshold across all criteria after any round R≥2), so effective cost = N + N×R_eff + 1 where R_eff = FinalRound. With defaults N=4, R=3: **17 calls** worst case (no convergence); **9 calls** on early convergence at round 2. |

---

## Per-strategy configuration

The council type registry (`map[string]council.CouncilType`, consumed by both
`cmd/server` and `cmd/eval` via `config.BuildRegistry`) is built one of two ways:

1. **YAML file** (primary) — `configs/council.yaml` (path overridable via
   `COUNCIL_CONFIG_PATH`). When present, it builds the *entire* registry; every
   per-strategy env var below is ignored.
2. **Env vars** (fallback) — used only when the YAML file doesn't exist. Each
   strategy gets its own namespaced env var family, opt-in (empty = not
   registered). Multiple registrations of the *same* `Strategy` are not
   possible via either mechanism — the registry is keyed by name, and both the
   YAML loader and the env builder enforce at most one entry per `Strategy`.

Set `COUNCIL_CONFIG_PATH=""` (explicitly empty, not merely unset) to force the
env fallback even when a YAML file exists at the default path.

### YAML schema

```yaml
councils:
  <registration-key>:        # also the client-facing council_type value (docs/api.md)
    strategy: <StrategyName> # PeerReview | RoleBased | Majority | GenerateRankRefine
                              # | MultiAgentDebate | MixtureOfAgents | Delphi
    arbiter: <model>          # → ChairmanModel (required except Majority, MixtureOfAgents)
    members: [<model>, ...]   # → Models (≥2; not used by RoleBased/MixtureOfAgents)
    roles:                    # RoleBased only — exactly these 5 keys, each → a model
      creator: <model>
      critic: <model>
      verifier: <model>
      simplifier: <model>
      devils_advocate: <model>
    refiner: <model>          # MixtureOfAgents only
    proposers: [<model>, ...] # MixtureOfAgents only, ≥1
    aggregators: [<model>, ...] # MixtureOfAgents only, ≥1
    temperature: <float>      # optional, any strategy; default DEFAULT_RADA_TEMPERATURE
    quorum: <int>             # optional, any strategy; default = strategy formula
                              # (RoleBased defaults to len(roles) = 5 when omitted)
    refine_top_k: <int>       # optional, GenerateRankRefine only
    max_debate_rounds: <int>  # optional, MultiAgentDebate only
    max_delphi_rounds: <int>  # optional, Delphi only
    delphi_convergence_threshold: <float> # optional, Delphi only
```

Loading is strict: unknown top-level fields, an unknown `strategy:` name, two
entries declaring the same `strategy:`, or any strategy's required fields
missing/misapplied are all hard errors collected together (`errors.Join`) —
the server fails fast at startup with every problem listed, rather than
silently registering a partial set of strategies. This is deliberately
stricter than the env fallback's warn-and-skip: a YAML file at a configured
path is an authored artifact, not ambient environment a deployment might
inherit unintentionally.

| Strategy | Required fields | Optional overrides |
|----------|-----------------|---------------------|
| `PeerReview` | `arbiter`, `members` (≥2) | `temperature`, `quorum` |
| `RoleBased` | `arbiter`, `roles` (exactly 5 keys) | `temperature`, `quorum` (default: `len(roles)`) |
| `Majority` | `members` (≥2) | `arbiter` (`""` = no tiebreak, ties error), `temperature`, `quorum` |
| `GenerateRankRefine` | `arbiter`, `members` (≥2) | `temperature`, `quorum`, `refine_top_k` |
| `MultiAgentDebate` | `arbiter`, `members` (≥2) | `temperature`, `quorum`, `max_debate_rounds` |
| `MixtureOfAgents` | `refiner`, `proposers` (≥1), `aggregators` (≥1) | `temperature`, `quorum` |
| `Delphi` | `arbiter`, `members` (≥2) | `temperature`, `quorum`, `max_delphi_rounds`, `delphi_convergence_threshold` |

`RoleBased`'s `roles:` map assigns a distinct model to each of the 5 fixed roles
(`council.DefaultRoleKeys`: `creator`/`critic`/`verifier`/`simplifier`/`devils_advocate`)
via `council.RolesWithModels` — named assignment, not positional. Role *content*
(names/instructions, `council.DefaultRoles`) is fixed in code either way, not
configurable via YAML or env.

`MixtureOfAgents` is the only strategy that doesn't use `Models`/`ChairmanModel` at
all — `CouncilType` carries three MoA-only fields instead:

```go
ProposerModels   []string  // Layer 1
AggregatorModels []string  // Layer 2
RefinerModel     string    // Layer 3 (final)
```

See the field-usage matrix in `CouncilType`'s doc-comment in `internal/council/types.go`.

### Fallback: env-var registry (used when no YAML file is present)

Each strategy gets its own namespaced env var family. `PeerReview` (keyed by
`DEFAULT_RADA_TYPE`, default `"default"`) is always registered; the other six
register only when their env var family is set — with the following variance
in what's required vs optional:

| Strategy | Env var family | Notes |
|----------|-----------------|-------|
| `PeerReview` | `RADA_MODELS` / `CHAIRMAN_MODEL` | Always registered — these are the global defaults |
| `RoleBased` | `ROLE_BASED_MODELS` / `ROLE_BASED_CHAIRMAN_MODEL` (both required) | `ROLE_BASED_MODELS` is distributed across the 5 fixed roles by `i % len(models)`, reproducing the historical round-robin at the env layer only |
| `Majority` | `MAJORITY_MODELS` (required) / `MAJORITY_CHAIRMAN_MODEL` (optional) | Chairman stays empty when unset — keeps the no-chairman tiebreak-error path reachable |
| `GenerateRankRefine` | `GENERATE_RANK_REFINE_MODELS` / `GENERATE_RANK_REFINE_CHAIRMAN_MODEL` (both required) | — |
| `MultiAgentDebate` | `DEBATE_MODELS` / `DEBATE_CHAIRMAN_MODEL` (both required) | `DEBATE_MAX_ROUNDS` optional, default 2 |
| `MixtureOfAgents` | `MOA_PROPOSER_MODELS` / `MOA_AGGREGATOR_MODELS` / `MOA_REFINER_MODEL` (all three required) | Partial config logs a warning and skips registration |
| `Delphi` | `DELPHI_MODELS` / `DELPHI_CHAIRMAN_MODEL` (both required) | `DELPHI_MAX_ROUNDS` (default 3) and `DELPHI_CONVERGENCE_THRESHOLD` (default 0.1) optional |

Unlike the YAML path, a missing/invalid registration here warns and skips
(server keeps running with fewer strategies) rather than failing startup —
env vars are ambient, inherited by a deployment that may not have set every
family intentionally.

---

## Quorum defaults

`QuorumMin == 0` means "use the strategy's default formula." A registration may override with any positive integer.

> Today `checkQuorum` is strategy-agnostic and applies `max(2, ⌈N/2⌉+1)` whenever `QuorumMin == 0`. Only the `PeerReview` row below is implemented as a runtime default — the other formulas are *proposed* defaults that will be wired into per-strategy quorum logic when each strategy ships. `RoleBased`'s `len(Roles)` value is set at registration time (e.g. by a constructor), not by the runner.

| Strategy | Default formula | Floor | Rationale |
|----------|-----------------|-------|-----------|
| `PeerReview` | `max(2, ⌈N/2⌉+1)` | 2 | Anonymous peer ranking is meaningless with 1 voter; majority of council needed for stable Kendall's W. |
| `RoleBased` | `len(Roles)` (set at registration; runner does not enforce) | all roles | Each role covers a unique concern; missing one = missing a perspective. |
| `Majority` | `max(3, ⌈N/2⌉+1)` | 3 | Need ≥3 to break ties; with N=2 a disagreement is a stalemate. |
| `GenerateRankRefine` | `max(K+1, 3)` where K is `RefineTopK` | 3 | Refining the top-K is meaningless if there are only K candidates. |
| `MultiAgentDebate` | `max(2, ⌈N/2⌉+1)` | 2 | Debate needs ≥2 actual positions. |
| `MixtureOfAgents` | `max(2, ⌈N_proposers/2⌉+1)` for Layer 1; aggregator layer needs ≥1 | 2 proposers, 1 aggregator | Layer 1 diversity is the input quality; one aggregator suffices (deterministic synthesis). |
| `Delphi` | `max(3, ⌈N/2⌉+1)` | 3 | Statistical averaging needs ≥3 to be informative; outliers swing 2-rater averages. |

---

## SSE event protocol — semantic four-slot model

Every strategy emits the same event family:

| Slot | Meaning | Mandatory? |
|------|---------|------------|
| `stage0_round_complete` | Clarification round-trips | No (skipped if `MaxRounds == 0`) |
| `stage0_done` | Internal state marker only — **not sent on the wire**. Tracked by the handler to decide whether to proceed to Stage 1; a client never sees this event. | N/A |
| `stage1_complete` | Initial generation results | Yes |
| `stage2_complete` | Intermediate processing | Yes (may be a stub) |
| `stage3_complete` | Final synthesis | Yes |

Stage 2 is polymorphic. The on-the-wire envelope carries a `kind` discriminator so the frontend can route each event to a strategy-specific renderer:

```jsonc
{
  "type": "stage2_complete",
  "kind": "<one of the seven values below>",
  "round": 1,                    // omitted when 0; reserved for multi-round strategies
  "data": [ /* strategy-specific payload — today []StageTwoResult */ ],
  "metadata": { /* shared envelope: council_type, label_to_model, … */ }
}
```

The `kind` field is **added** to the existing `Stage2CompleteData` shape — no field renames or removals — so today's clients keep working.

PeerReview's existing payload corresponds to `kind: "peer_ranking"`; RoleBased's stub corresponds to `kind: "role_stub"`. **Multi-round strategies** (`MultiAgentDebate` and `Delphi`, both shipped) fire a `stage2_round_complete` event per round followed by a terminal `stage2_complete` summary. The per-round event has a **required** `round: N` field (not omitempty); the terminal event omits `round` when zero. The terminal event's `metadata.debate` carries the canonical transcript across all rounds, so a client that misses round events can still render the full debate from the terminal event alone.

### Stage 2 `kind` values

| Kind | Strategy | Status | `data` shape | `round` semantics |
|------|----------|--------|--------------|-------------------|
| `peer_ranking` | `PeerReview` | **shipped** | `[]StageTwoResult` — each reviewer's ranked label list | always `0` |
| `role_stub` | `RoleBased` | **shipped** | `[]` — empty; metadata carries `aggregate_rankings: []`, `consensus_w: 1.0` | always `0` |
| `vote_tally` | `Majority` | **shipped** | `metadata.vote_tally` is a `VoteTally` (`{clusters: VoteCluster[], winner_label: string}`); `data` is `[]` (Majority does not produce per-reviewer Stage 2 results). `VoteCluster` is `{members: string[], representative: string, votes: int}`. Clusters are sorted by votes desc, then representative asc. | always `0` |
| `rank_refine` | `GenerateRankRefine` | **shipped** | `metadata.rank_refine` is a `RankRefine` (`{rankings: RankedCandidate[], top_k: int, criteria: string[]}`); `data` is `[]` (the ranking lives in metadata, not per-reviewer). `RankedCandidate` is `{label: string, scores: map<string, float64>, total_score: float64, advancing: bool}`. Rankings are sorted by `total_score` desc, then `label` asc. Exactly `top_k` candidates have `advancing: true`. Per-criterion scores clamped to `[0.0, 1.0]`; `total_score` to `[0.0, len(criteria)]`. | always `0` |
| `debate_round` | `MultiAgentDebate` | **shipped** | `metadata.debate` is a `Debate` (`{rounds: DebateRound[], final_round: int, dropouts?: DebaterDropout[]}`); `data` is `[]` (the transcript lives in metadata, not per-reviewer). Round events fire as `stage2_round_complete` per round (carrying just that round); the terminal `stage2_complete` carries the full transcript including dropouts. `DebaterDropout` is `{label, last_round, reason: "error"\|"json_parse"\|"empty_revision"}`. | `1..R`; one `stage2_round_complete` per round, then a terminal `stage2_complete` |
| `moa_aggregator` | `MixtureOfAgents` | **shipped** | `metadata.moa_aggregator` is a `MoaAggregator` (`{aggregators: AggregatorOutput[]}`); `data` is `[]` (the aggregator drafts live in metadata, not per-reviewer). `AggregatorOutput` is `{label, model, content, sources: string[], duration_ms}`. `sources` lists the Layer-1 proposer labels fed into that aggregator (today: all-to-all, so every aggregator's `sources` lists every successful proposer). Aggregators are sorted by `label` asc. `metadata.label_to_model` is a single flat map containing both proposer (`Response A → …`) and aggregator (`Aggregator A → …`) entries. | always `0` (single aggregator pass; no `stage2_round_complete` events) |
| `delphi_round` | `Delphi` | **shipped** | `metadata.delphi` is a `DelphiPanel` (`{rounds: DelphiRound[], final_round: int, converged: bool, criteria: string[]}`); `data` is `[]` (the rating transcript lives in metadata, not per-reviewer). `DelphiRound` is `{round, ratings: DelphiRating[], stats: DelphiStats}`. `DelphiRating` is `{label, model, scores: map<string, float64>, summary, duration_ms}` — scores clamped to `[0.0, 1.0]` per criterion. `DelphiStats` is `{mean, std_dev, delta_mean (omitempty)}` — `delta_mean` absent on round 1; on round R≥2, present only for criteria in BOTH the current and prior round. Round events fire as `stage2_round_complete` per round (carrying just that round's `DelphiRound`); the terminal `stage2_complete` carries the full transcript. NO `DelphiDropout` type — dropped raters are simply absent from subsequent `ratings` slices. | `1..R`; one `stage2_round_complete` per round, then a terminal `stage2_complete`. Strategy may exit early when `max(delta_mean) < threshold` across all criteria. |

All seven kinds are now shipped. The `kind` discriminator exists so a client-side
dispatcher can fall back gracefully on an unrecognised value instead of crashing when a
future strategy ships ahead of that client's support for it.

---

## REST is strategy-agnostic

`POST /api/conversations/{id}/message` and `/message/stream` cover **all** strategies. The request body's `council_type` field resolves to a registered `CouncilType`, whose `Strategy` value the runner dispatches on. There is no per-strategy endpoint and there will not be one. Strategy choice is a server-side configuration concern, not a client concern.

---

## What's not here

- **Code review** — was a thin `RoleBased` wrapper (4 specialist roles + `QuorumMin = len(Roles)` + duplicate `/review*` endpoints). Removed in PR #199 to clear the runway for the strategy expansion. Will return post-refactor, rebuilt on top of `Majority` or `MixtureOfAgents` with proper diff handling rather than prompt-only role instructions.
