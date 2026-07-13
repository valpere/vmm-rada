---
type: task
priority: p2
labels: task, p2: medium, security, ci
github_issue: ""
debt: balanced
effort: s
---

# Automate govulncheck — scheduled gate instead of manual /housekeeping

## Dreaming reference
§6 of 2026-W28 report — "The Go-toolchain-CVE treadmill." Third dreaming
pass in a row (W23 → W25 → W28) surfacing the same structural gap:
Dependabot doesn't cover the `go`/`toolchain` directive, and the
existing detection mechanism (`govulncheck` as Check 8 in
`/housekeeping`, added by the W25 plan) is manual — nobody ran it
between W25 (2026-06-21) and W28 (2026-07-12), so GO-2026-5856 sat
undetected for ~3 weeks until the next dreaming pass happened to
invoke it.

## Summary

`/housekeeping`'s govulncheck check works correctly when run, but
relies on someone remembering to run `/housekeeping`. The fix isn't
another manual flag — it's removing the human-memory dependency
entirely. Two viable approaches, Tech Lead to pick:

**Option A — Scheduled GitHub Actions workflow (recommended default)**
New `.github/workflows/govulncheck.yml`, cron-triggered (e.g. daily or
weekly — matches the cadence of `dreaming.sh`'s existing Sunday
04:00 timer). Runs `govulncheck ./...`; on new findings, opens or
updates a `p1`-labeled GitHub issue (idempotent — check for an
existing open issue with a matching CVE ID before creating a
duplicate). Non-blocking to normal PR merges (doesn't gate `ci.yml`),
purely a detection/alerting mechanism.

**Option B — Add to existing ci.yml on every PR/push to main**
Add a `govulncheck` step to the existing `go` job in `ci.yml`. Catches
new CVEs on every push instead of waiting for a schedule, but: (a) can
block unrelated PRs on a vulnerability the PR author didn't introduce
and has no fix available for yet, (b) doesn't fail fast if `main`
itself goes quiet for weeks (same gap as Dependabot, just faster
per-push).

**Tech Lead call:** cadence (daily vs weekly), blocking vs
informational-only, and issue-dedup strategy (by CVE ID, by go.mod
line) all need a decision before implementation — this plan proposes
Option A as the default but doesn't prescribe the final shape.

## Approach (Option A, if selected)

1. New workflow file, cron schedule (`on: schedule: cron: ...`).
2. Run `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`.
3. Parse output; if non-zero exit (vulnerabilities found), check for an
   existing open GitHub issue referencing the same CVE ID(s)
   (`gh issue list --search "GO-XXXX-XXXX in:title"`).
4. If none exists, `gh issue create` with `p1`/`security` labels,
   CVE ID(s), affected package(s), and fixed-in version in the body.
5. If a matching issue already exists, no-op (avoid duplicate noise).

## Files to change

- New: `.github/workflows/govulncheck.yml`
- Possibly: `.claude/skills/housekeeping/SKILL.md` — note that Check 8
  is now redundant with the scheduled workflow, or keep both (manual
  spot-check + automated gate aren't mutually exclusive)

## Acceptance criteria

- New workflow runs on schedule without manual intervention
- A deliberately-reverted `go.mod` version (or a known-vulnerable test
  case) triggers issue creation in a dry run
- Re-running against an already-flagged CVE does not create a duplicate issue
- Tech Lead has signed off on cadence + blocking/non-blocking choice
