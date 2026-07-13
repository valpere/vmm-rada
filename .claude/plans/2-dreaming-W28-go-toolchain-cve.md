---
type: task
priority: p2
labels: task, p2: medium, security
github_issue: ""
debt: quick-fix
effort: xs
---

# Bump Go toolchain 1.26.4 → 1.26.5 (GO-2026-5856)

## Dreaming reference
§6 of 2026-W28 report. Third occurrence of this same structural gap
(W23 → W25 → W28) — Dependabot doesn't cover the `go`/`toolchain`
directive in `go.mod`, only module deps.

## Summary

`govulncheck` flags **GO-2026-5856** (crypto/tls ECH privacy leak) in
`go1.26.4`, fixed in `1.26.5`. Traced (toolchain-level, not
project-code-level) via `cmd/server/main.go:192`,
`internal/openrouter/client.go:202,228`, `internal/council/prompts.go:749`
— all paths that link `net/http`'s TLS stack, not code this project
directly controls.

The W25 plan for the previous CVE round (GO-2026-5039/5037 → 1.26.4)
already added `govulncheck` as Check 8 to `/housekeeping`
(`.claude/skills/housekeeping/SKILL.md:121`) — that part is done and
doesn't need repeating. The gap is that `/housekeeping` is manual —
nothing runs it automatically, so this CVE sat undetected until the
next dreaming pass found it by chance. See the companion medium-priority
item (this same dreaming pass, §6) for the automation follow-up
(scheduled/CI govulncheck gate) — out of scope for this plan, tracked
separately.

**Tech Lead judgment needed:** the 2026-W28 report explicitly flags
that whether this warrants `p1` (vs. the `p2` default here) is a call
based on ECH privacy-leak exploitability for this server's threat
model — this plan defaults to `p2` per the previous round's precedent,
Tech Lead may reclassify.

## Approach

1. `go.mod:3`: `go 1.26.4` → `go 1.26.5`
2. `.github/workflows/ci.yml:21`: `go-version: "1.26.4"` → `"1.26.5"`
3. Run `go build ./...` and `go test ./...` to confirm no regressions.
4. Run `govulncheck ./...` to confirm GO-2026-5856 no longer flags.

## Files to change

- `go.mod` — bump `go 1.26.5`
- `.github/workflows/ci.yml` — bump `go-version: "1.26.5"`

## Acceptance criteria

- `go.mod` and `ci.yml` both reference 1.26.5
- `go build ./...` and `go test ./...` pass
- `govulncheck ./...` no longer flags GO-2026-5856
