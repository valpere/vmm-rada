---
name: rolebased-strategy-orphan
description: "HISTORICAL (resolved 2026-07-21, both phases merged) — RoleBased registered as a standalone opt-in strategy; extraction-into-MoA plan overruled"
type: project
status: resolved-historical
---

> **Status: RESOLVED.** Both phases below are merged (#301, and RoleBased's
> registration + later #322/#323 evolution). Kept as the historical decision
> record — the "why standalone, not MoA" reasoning stays load-bearing (don't
> resurrect the extraction plan) — but there is no open work here. See
> [[project_architecture]] for RoleBased's current shape (5 roles, YAML-first
> registration).

`council.RoleBased` (enum in `internal/council/types.go`, impl `rolebased.go`,
dispatched via `runner.go`) is a real, tested 2-stage pipeline (parallel specialist
roles → chairman; Stage 2 skipped, `role_stub` SSE event). It was orphaned — no env-var
family constructed `CouncilType{Strategy: RoleBased}`, so it was unreachable in
production.

**Resolution (decided 2026-07-21, phased):**
- Phase 1 (#301, merged): build-time invariant `TestAllStrategiesRegisteredOrExempted`
  in `strategy_wiring_test.go` — every `Strategy` const must be registered in
  `cmd/server/main.go` or listed in `registrationExemptions`. RoleBased got the sole
  exemption pending Phase 2. The test derives the enum via AST (not a hand list).
- Phase 2 (plan `2-register-rolebased-strategy.md`, tech-lead APPROVED 2026-07-21):
  **register RoleBased standalone.** Adds `ROLE_BASED_MODELS` / `ROLE_BASED_CHAIRMAN_MODEL`
  config family (mirrors Debate/Delphi opt-in: both required, no no-LLM path, warn+skip
  if only one set), a fixed in-code `council.DefaultRoles` (Generator/Critic/Verifier/
  Simplifier per `docs/council-research-synthesis.md §2.7`), and registers with
  `QuorumMin: len(DefaultRoles)`. Removes the RoleBased `registrationExemptions` entry.

**Why standalone (NOT extracted into MixtureOfAgents):** An earlier ideation pass
proposed folding RoleBased's mechanism into MoA as a role-assignment mode. The user
explicitly reviewed and **overruled** that, citing external research contrasting
Role-Based vs MoA vs Majority: Role-Based fits "heavy contextual data / complex
multi-step workflows," which does not overlap with MoA's creative-synthesis strength or
Majority's exact-answer strength. RoleBased stays a distinct strategy.

**How to apply:** The extraction-into-MoA path is dead — do not resurrect it. Role
*content* (names/instructions) is fixed in code, intentionally NOT env-configurable
(YAGNI, no requester). `QuorumMin = len(DefaultRoles)` is deliberate: every role is a
unique concern, so RoleBased has zero fault tolerance (one role's model failure fails the
whole deliberation) — documented intent (`docs/pipeline.md`, `strategies.md` quorum
table: "all roles"), not a bug. Separate from the removed `/review*` code-review feature
(PR #199), which will be rebuilt on Majority/MoA independently — do not conflate. See
[[usage-cost-aggregation]] for the layering rule keeping council impl details out of
handler/main wiring.
