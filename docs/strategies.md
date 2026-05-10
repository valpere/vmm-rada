# Deliberation Strategies

The `Strategy` enum (`internal/council/types.go`) declares **7 constants**. Two are implemented today; five are reserved for planned strategies. The runner returns `"strategy %d not implemented"` for unimplemented constants.

For architecture context (package layout, layer boundaries, dispatch switch) see [`architecture-v2.md`](./architecture-v2.md). For the academic background of each strategy see [`council-research-synthesis.md`](./council-research-synthesis.md).

Stage 0 (clarification) runs **before** strategy dispatch and is strategy-independent — see the [Stage 0 section in architecture-v2.md](./architecture-v2.md#stage-0-clarification--strategy-independent). It has its own dedicated model configuration (`CLARIFICATION_MODELS`, `CLARIFICATION_ARBITER_MODEL`); see `.env.example`. Both env vars are optional and fall back to the council type's `Models` / `ChairmanModel` when unset.

---

## Status

| Strategy | Status | Pipeline file | Implementation PR |
|----------|--------|---------------|-------------------|
| `PeerReview` | shipped | `runner.go:runPeerReview` | initial |
| `RoleBased` | shipped | `rolebased.go:runRoleBased` | #177 |
| `Majority` | planned | — | TBD |
| `GenerateRankRefine` | planned | — | TBD |
| `MultiAgentDebate` | planned | — | TBD |
| `MixtureOfAgents` | planned | — | TBD |
| `Delphi` | planned | — | TBD |

---

## Per-strategy configuration

Each registration in `cmd/server/main.go` and `cmd/eval/main.go` carries its own `Models` and `ChairmanModel`. Multiple registrations may reuse the same `Strategy` with different model sets — the registry is keyed by `Name`, not `Strategy`. New strategies adopt namespaced env var families with fall-through to the global defaults.

| Strategy | What `Models` represents | What `ChairmanModel` represents | Env var family | Fall-through |
|----------|--------------------------|---------------------------------|----------------|--------------|
| `PeerReview` | Council members (generators + reviewers) | Stage 3 synthesiser | `COUNCIL_MODELS` / `CHAIRMAN_MODEL` | — (these are the defaults) |
| `RoleBased` | Pool assigned to roles by `i % len(Models)` | Synthesiser across role findings | (none today; roles config is in code) | — |
| `Majority` | Voters | Tiebreaker (optional; `""` = no tiebreak) | `MAJORITY_MODELS` / `MAJORITY_CHAIRMAN_MODEL` | `COUNCIL_MODELS` / `CHAIRMAN_MODEL` |
| `GenerateRankRefine` | Generators | Ranker + refiner (single model today) | `GENERATE_RANK_REFINE_MODELS` / `GENERATE_RANK_REFINE_CHAIRMAN_MODEL` | `COUNCIL_MODELS` / `CHAIRMAN_MODEL` |
| `MultiAgentDebate` | Debaters | Synthesiser | `DEBATE_MODELS` / `DEBATE_CHAIRMAN_MODEL` | `COUNCIL_MODELS` / `CHAIRMAN_MODEL` |
| `MixtureOfAgents` | (see below — 3 layers) | Refiner (or `""` to use `RefinerModel` field) | `MOA_PROPOSER_MODELS` / `MOA_AGGREGATOR_MODELS` / `MOA_REFINER_MODEL` | `COUNCIL_MODELS` for proposers; `CHAIRMAN_MODEL` for refiner |
| `Delphi` | Raters | Facilitator (optional) | `DELPHI_MODELS` / `DELPHI_CHAIRMAN_MODEL` | `COUNCIL_MODELS` / `CHAIRMAN_MODEL` |

`MixtureOfAgents` is the only strategy that does not fit the `Models` + `ChairmanModel` shape. When MoA ships, `CouncilType` gains:

```go
ProposerModels   []string  // Layer 1
AggregatorModels []string  // Layer 2
RefinerModel     string    // Layer 3 (final)
```

These fields are zero-valued for every other strategy. Adding optional fields is non-breaking; defer the decision between explicit fields and a generic `Layers map[string][]string` until MoA's implementation PR.

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
| `stage0_round_complete` / `stage0_done` | Clarification round-trips | No (skipped if `MaxRounds == 0`) |
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

PeerReview's existing payload corresponds to `kind: "peer_ranking"`; RoleBased's stub corresponds to `kind: "role_stub"`. For multi-round strategies (`MultiAgentDebate`, `Delphi`) — both **planned, not yet implemented** — the server is expected to fire a `stage2_round_complete` event per round followed by a final `stage2_complete` summary; this event type does not exist in the runtime today and ships with the first multi-round strategy.

### Stage 2 `kind` values

| Kind | Strategy | Status | `data` shape | `round` semantics |
|------|----------|--------|--------------|-------------------|
| `peer_ranking` | `PeerReview` | **shipped** | `[]StageTwoResult` — each reviewer's ranked label list | always `0` |
| `role_stub` | `RoleBased` | **shipped** | `[]` — empty; metadata carries `aggregate_rankings: []`, `consensus_w: 1.0` | always `0` |
| `vote_tally` | `Majority` | **reserved** | per-candidate vote counts (or weighted scores), winner identifier, optional cluster groupings | always `0` |
| `rank_refine` | `GenerateRankRefine` | **reserved** | ranked candidate list with criterion scores; top-K subset that proceeds to refinement | always `0` |
| `debate_round` | `MultiAgentDebate` | **reserved** | per-debater critique-and-revise output for the current round; references to the round's targets | `1..N`; one event per round, then a final `stage2_complete` with summary |
| `moa_aggregator` | `MixtureOfAgents` | **reserved** | Layer-2 aggregator outputs; references to which Layer-1 proposers fed each aggregator | always `0` (single aggregator pass) |
| `delphi_round` | `Delphi` | **reserved** | per-rater rating list for the current round; running averages and convergence indicator | `1..N`; one event per round |

Reserved kinds are not yet emitted by the runtime. The frontend `Stage2.jsx` dispatcher renders any unknown kind via a fallback view (`Stage 2 — kind: <X> (view not implemented yet)`) so a strategy in flight does not crash the UI.

---

## REST is strategy-agnostic

`POST /api/conversations/{id}/message` and `/message/stream` cover **all** strategies. The request body's `council_type` field resolves to a registered `CouncilType`, whose `Strategy` value the runner dispatches on. There is no per-strategy endpoint and there will not be one. Strategy choice is a server-side configuration concern, not a client concern.

---

## What's not here

- **Code review** — was a thin `RoleBased` wrapper (4 specialist roles + `QuorumMin = len(Roles)` + duplicate `/review*` endpoints). Removed in PR #199 to clear the runway for the strategy expansion. Will return post-refactor, rebuilt on top of `Majority` or `MixtureOfAgents` with proper diff handling rather than prompt-only role instructions.
