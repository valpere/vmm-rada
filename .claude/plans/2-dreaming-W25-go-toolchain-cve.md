---
type: task
priority: p2
labels: task, p2: medium, security
github_issue: ""
debt: quick-fix
effort: xs
---

# Bump Go toolchain 1.26.3 → 1.26.4 + add govulncheck

## Dreaming reference
§6 of 2026-W25 report (carryover from W23). CVEs GO-2026-5039 and GO-2026-5037
fixed in 1.26.4; pin has been open 3+ weeks.

## Summary

Two CVEs in Go 1.26.3 were fixed in 1.26.4. Dependabot does not cover the
`go` / `toolchain` directive in `go.mod` — only module deps. Needs manual bump.

Additionally, the dreaming report suggests adding `govulncheck` to `/housekeeping`
so toolchain-pin gaps are auto-detected in future passes.

## Approach

### Part 1 — Bump toolchain (xs effort)

1. `go.mod:3`: `go 1.26.3` → `go 1.26.4`
2. `.github/workflows/ci.yml:22`: `"1.26.3"` → `"1.26.4"`
3. Run `go build ./...` and `go test ./...` to confirm no regressions.

### Part 2 — Add govulncheck to /housekeeping (xs effort)

The housekeeping skill (`skills/housekeeping/SKILL.md`) has 7 checks. Add Check 8:

```
### Check 8 — govulncheck

go run golang.org/x/vuln/cmd/govulncheck@latest ./... 2>&1

Pass: exit 0 (no vulnerabilities)
Fail: list vulnerabilities with CVE IDs and affected packages
```

This catches future toolchain-pin CVE gaps that Dependabot misses.

## Files to change

- `go.mod` — bump `go 1.26.4`
- `.github/workflows/ci.yml` — bump `go-version: "1.26.4"`
- `.claude/skills/housekeeping/SKILL.md` — add Check 8

## Acceptance criteria

- `go.mod` and `ci.yml` both reference 1.26.4
- `go build ./...` and `go test ./...` pass
- `govulncheck ./...` exits 0 on the updated toolchain
- `/housekeeping` skill includes govulncheck as Check 8
