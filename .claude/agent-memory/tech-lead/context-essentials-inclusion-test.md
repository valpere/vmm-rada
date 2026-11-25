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
Route those to a periodic audit instead (`housekeeping` skill phase, or an automated
pass) — retrospective checks belong in retrospective processes.

Two further tests, both applied in the 2026-08-14 rejection below:

- **Would the rule have prevented the incident that motivated it?** If the drift was
  found by an existing audit mechanism, that mechanism will find the next instance too;
  a permanent rule adds cost without adding detection.
- **Is the load-bearing fact already present?** Meta-instructions *about maintaining* an
  existing entry are process, not an invariant that must survive summarization.

"There is room in the budget" is never an argument for adding. 60 lines is a ceiling,
not a quota.

**Adding a line and correcting an existing line are different decisions.** The bar above
governs *admission*. A line already admitted that has become factually wrong (names files
that no longer exist, or omits the files the drift actually hits) is a *correction* —
net-zero-ish cost, no new admission, and a stale line is worse than no line because it
teaches a wrong answer confidently. Do not apply the admission bar to a correction.

**"tech-lead.md doesn't survive compaction" is not a valid argument for mirroring into
this file.** Agent prompts under `.claude/agents/` are subagent prompts, loaded fresh into
a new context every dispatch; main-session compaction cannot make them invisible. The
criterion is always loaded at the moment it must fire. Expect this argument to recur from
automated audits — rebut it rather than re-litigating.

**Split rule when a mechanism spans both files:** `context-essentials.md` gets the *facts*
(which files pair with which change), the agent prompt keeps the *governance* (verdicts,
blocking semantics, exemptions, who enforces). Non-overlapping content is not a second
copy. A summary of the governance in `context-essentials.md` *is*, and fails both this
test and [[governance-enforcement-point]].

**Precedent (2026-08-14):** automated audit 2026-W32 proposed a permanent "audit dependents
when a module is extracted to another repo" rule, generalized from the `frontend/` →
`vmm-rada-web-ui` extraction (2026-07-19). REJECTED as over-fit at N=1 — the only module
extraction in project history. The one-shot cleanup (plan
the W32 frontend-prune plan) was the correct and sufficient response. Revisit only if
a second extraction ever occurs, which would change the base rate from one-off to pattern.
The audit report itself flagged the over-fit risk and deferred the call to Tech Lead —
that framing was correct and worth repeating for medium-confidence suggestions.

**Precedent (2026-08-23):** the 2026-W33 audit proposed mirroring the
[[docs-triad-sync-gate]] gate from `tech-lead.md` into `context-essentials.md` for
"compaction survival" (W33 docs-triad mirror plan).
APPROVED WITH CHANGES, retargeted: the compaction premise was false (subagent prompt),
but the existing bullet named `CLAUDE.md`/`architecture-v2.md`/`strategies.md` while the
two incidents it cites repaired `architecture-v2.md` + `strategy-showcase.md` (#304) and
`README.md` + `user-guide.md` (#327) — 3 of those 4 files unnamed. Kept as a factual
file-list correction; all gate/verdict/blocking/"see tech-lead.md" wording stripped.
Useful shape: when a mirror request is wrong, check whether the *target line itself* is
stale — the audit may be right that the file needs editing for the wrong reason.
