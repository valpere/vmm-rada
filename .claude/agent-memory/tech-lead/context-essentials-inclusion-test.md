---
name: context-essentials-inclusion-test
description: The bar for adding a line to .claude/context-essentials.md — must change agent behaviour immediately post-compaction, not merely be true
metadata:
  type: project
---

A proposed line earns a place in `.claude/context-essentials.md` only if an agent,
immediately after a compaction event, would **do the wrong thing without it**. Being
true, useful, or well-phrased is not sufficient.

**Why:** the file is re-injected into context after every compaction and states its own
~60-line budget ("every line costs tokens on each re-injection"). Its value is signal
density; each line that does not alter post-compaction behaviour dilutes the lines that
do. Every entry that has earned its place guards a behaviour that is *agent-initiated,
routine, reflexive, and low-ceremony* (run `go build` before ship, squash-merge only,
mark PLANNED vs current, never `--no-verify`) — the agent will get these wrong within
minutes of losing context.

**How to apply:** score a candidate on four axes — who initiates it (agent vs user),
frequency, whether it is reflexive or deliberate, and ceremony. Candidates that are
*user-initiated, rare, deliberate, and high-ceremony* fail: the user is present and
directing when they occur, so a re-injected reminder cannot fire when it would matter.
Route those to a periodic audit instead (`housekeeping` skill phase, or the dreaming
pass) — retrospective checks belong in retrospective processes.

Two further tests, both applied in the 2026-08-14 rejection below:

- **Would the rule have prevented the incident that motivated it?** If the drift was
  found by an existing audit mechanism, that mechanism will find the next instance too;
  a permanent rule adds cost without adding detection.
- **Is the load-bearing fact already present?** Meta-instructions *about maintaining* an
  existing entry are process, not an invariant that must survive summarization.

"There is room in the budget" is never an argument for adding. 60 lines is a ceiling,
not a quota.

**Precedent (2026-08-14):** dreaming pass 2026-W32 proposed a permanent "audit dependents
when a module is extracted to another repo" rule, generalized from the `frontend/` →
`vmm-rada-web-ui` extraction (2026-07-19). REJECTED as over-fit at N=1 — the only module
extraction in project history. The one-shot cleanup (plan
`1-dreaming-w32-frontend-prune`) was the correct and sufficient response. Revisit only if
a second extraction ever occurs, which would change the base rate from one-off to pattern.
The dreaming report itself flagged the over-fit risk and deferred the call to Tech Lead —
that framing was correct and worth repeating for medium-confidence suggestions.
