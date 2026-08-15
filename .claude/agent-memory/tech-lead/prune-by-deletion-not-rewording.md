---
name: prune-by-deletion-not-rewording
description: In prompt-file cleanup PRs, a rule that is reworded rather than deleted can silently invert its meaning — check every surviving reworded line against the repo's actual state
metadata:
  type: feedback
---

When reviewing a "remove stale X references" sweep over `.claude/` prompt files, treat every
**reworded** line as higher-risk than every **deleted** line. Deletions are self-evidently safe;
a reword keeps the sentence's authority while quietly detaching it from its original subject.

**Why:** These files are executable instructions consumed literally by small models (haiku/sonnet
agents). A rule that was true and scoped ("no npm test step — the *frontend* has no test suite")
becomes globally false when the qualifier is dropped ("no test step — no test suite in this repo"),
and the agent then acts on the false version. Deleting the rule outright would have been correct;
generalizing it inverted it.

**Evidence:** PR for issue #333 (2026-08-15). `ci-build-agent.md` rule 9 was reworded from a
frontend-scoped "no `npm run test` step" into "**No test step** — omit `npm run test` or similar
(no test suite in this repo)". The repo has a `test:` Makefile target, `.github/workflows/ci.yml`
runs `go test -race -count=1 ./...`, the same file's own `description:` advertises "make lint,
make test", and its own workflow template at L80 emits `go test -race ./...`. The reworded rule
told the CI-generating agent to omit the test step it elsewhere mandates. The PR's own commit
message explicitly *allowlisted* this line as "generic ... doesn't reference frontend" — the
rewording is exactly what made it look generic and safe.

Same PR, same class, lower severity: `frontend/src/` in a Boundaries list was reworded to `src/`,
a path that does not exist in this Go repo (`cmd/`, `internal/`).

**How to apply:** In any prune/cleanup review, diff for `-`/`+` line *pairs* (a reword), not just
`-` runs, and verify each survivor against the live repo — Makefile targets, `.github/workflows/`,
actual directory layout. Reject "we kept it because it no longer mentions X" reasoning: if a line
only existed to scope a now-removed subsystem, delete it. Related: [[frontend-prune-plan-corrections]]
(coordinates wrong), this one (coordinates right, semantics wrong).
