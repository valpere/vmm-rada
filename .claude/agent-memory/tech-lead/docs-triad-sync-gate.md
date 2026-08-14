---
name: docs-triad-sync-gate
description: Strategy/config/wire-shape changes must land their doc updates in the same PR; canonical doc target map for vmm-rada backend-only repo
type: project
---

A change that adds/changes a **deliberation strategy**, an **env var / `configs/council.yaml`
key family**, or the **REST+SSE wire shape** must update its docs in the *same* PR, never
as a follow-up. Canonical target map (backend-only repo as of 2026-07-19):

| Trigger | Must touch |
|---|---|
| new/changed strategy | `docs/strategies.md`, `docs/architecture-v2.md`, `CLAUDE.md` (if the strategy list/count is stated) |
| new/changed env var or `configs/council.yaml` key | `docs/architecture-v2.md`, `docs/user-guide.md`, `README.md` (if user-facing), `CLAUDE.md` |
| REST/SSE wire-shape change | `docs/api.md` **and** `docs/openapi.yaml` (pair — never one alone), `docs/architecture-v2.md` |

**Why:** docs-only drift-repair PRs are a recurring line item (#304, #327 among the last
8 merges). Dreaming 2026-W32 §1 classified this as structural, not incidental. `docs/`
files that no longer exist are still named in `.claude/agents/docs-maintainer.md`
(`architecture.md`, `go-implementation.md`, `council-stages.md`, `docs/frontend/`) — the
agent nominally responsible for doc sync has a stale map, so post-merge cleanup cannot be
relied on. `docs/openapi.yaml` drift is partially machine-caught by
`internal/api/spec_test.go`, but only for the assertions that test already makes.

**How to apply:** At *plan review*, require the plan's Files-to-change list to name the
docs above, or an explicit "docs: N/A — <reason>". At *code review*, check the diff.
Keep the trigger narrow: pure internal refactors, test-only, and dependency PRs are
exempt — do not turn this into a blanket "every PR touches docs" tax.

Related: [[governance-enforcement-point]] (this gate lives in `tech-lead.md`, not
`backlog/SKILL.md`), [[stage0-done-not-on-wire]] (a concrete instance of this drift).
