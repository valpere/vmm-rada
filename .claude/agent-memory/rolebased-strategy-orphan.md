---
name: rolebased-strategy-orphan
description: RoleBased Strategy is implemented+tested but has no registration path; two-phase plan to guard then extract it
type: project
---

`council.RoleBased` (enum in `internal/council/types.go`, impl `rolebased.go`,
dispatched `runner.go`) is implemented and tested but is **never registered** in
`cmd/server/main.go` — no env-var family constructs `CouncilType{Strategy: RoleBased}`,
so it is unreachable in production. Tracked as a gap in `docs/requirements.md#6-gap-analysis`
and `docs/strategies.md` (issue #177).

**Why:** Came out of an ideation session on what to do with the orphaned strategy.
Decision is a two-phase plan:
- Phase 1 (issue for plan `2-strategy-registration-invariant-test.md`): a test-only
  build-time invariant — every `Strategy` constant must be registered in `main.go` or
  explicitly exempted with a reason. `RoleBased` gets the sole exemption.
- Phase 2 (not yet planned): extract RoleBased's mechanism into `MixtureOfAgents` as a
  role-assignment mode and delete the standalone enum value; the exemption entry is
  removed then.

**How to apply:** When reviewing Phase-1 code, the invariant test must derive the strategy
set from the enum via AST (parse the `Strategy = iota` block in types.go), NOT a
hand-maintained slice — a hand list reintroduces the same drift the test exists to prevent.
When Phase 2 lands, expect the `RoleBased` const and its exemption entry to be removed
together. See [[usage-cost-aggregation]] for the layering rule that council impl details
stay out of the handler/main wiring.
