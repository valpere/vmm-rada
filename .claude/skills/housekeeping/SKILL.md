---
name: housekeeping
description: "VMM Rada recurring repo health check. Runs 7 checks (stale branches, fmt.Println leaks, tracked .env, tracked backups, TODO/FIXME count, go vet, CI test delta) and outputs a pass/fail table. Usage: /housekeeping"
---

# Skill: /housekeeping
# VMM Rada — Repo Health Check

---

## OVERVIEW

```
/housekeeping  →  7 checks  →  Markdown table: Check | Status | Detail
                            →  Summary: N passed, M failed
```

Read-only. Never modifies files, never commits, never opens a PR.

---

## CHECKS

### Check 1 — Stale Local Branches

```bash
git remote prune origin 2>&1 | tail -3
LOCAL_COUNT=$(git branch | grep -v '^\*' | wc -l | tr -d ' ')
```

**Pass:** `LOCAL_COUNT <= 10`
**Fail:** "N local branches — prune merged ones"

---

### Check 2 — Debug Output in Source

**Goal:** Zero `fmt.Println` / `fmt.Printf` calls in non-test Go source (use structured logging).

```bash
FILES=$(grep -r --include="*.go" \
  -l "fmt\.Println\|fmt\.Printf\|fmt\.Print(" . 2>/dev/null \
  | grep -v "_test\.go" | grep -v "vendor/")
COUNT=$(echo "$FILES" | grep -c '.' 2>/dev/null || echo 0)
```

**Pass:** `COUNT == 0`
**Fail:** list offending files

Note: `fmt.Fprintf(os.Stderr, ...)` in `cmd/` main files is acceptable for startup messages.

---

### Check 3 — Tracked .env File

```bash
TRACKED=$(git ls-files .env 2>/dev/null)
```

**Pass:** empty
**Fail:** "`.env` is tracked — `git rm --cached .env` and add to .gitignore"

---

### Check 4 — Tracked Backup Files

```bash
TRACKED=$(git ls-files backup/ 2>/dev/null)
```

**Pass:** empty (or `backup/` doesn't exist)
**Fail:** list tracked backup files

---

### Check 5 — TODO/FIXME Count (informational)

```bash
COUNT=$(grep -r --include="*.go" --include="*.ts" --include="*.tsx" \
  -E "//\s*(TODO|FIXME)" --exclude-dir=.worktrees . 2>/dev/null | wc -l | tr -d ' ')
```

**Status:** Always `INFO`.
**Detail:** "N TODO/FIXME" — append " (consider a cleanup sprint)" if > 15.

---

### Check 6 — go vet

**Goal:** `go vet ./...` passes with zero errors.

```bash
go vet ./... 2>&1
```

**Pass:** exit 0, no output
**Fail:** show all vet errors

---

### Check 7 — CI Test Count Delta

**Goal:** Test count hasn't dropped since the previous successful CI run.

```bash
# Current test count
CURRENT=$(go test ./... -v 2>/dev/null | grep -c "^--- PASS\|^--- FAIL" || echo "0")

# Last 2 successful CI runs — look for test output in workflow artifacts or logs
gh run list --workflow=ci.yml --status=completed --limit=5 \
  --json databaseId,headBranch,conclusion 2>/dev/null
```

If CI run log available, extract test count and compare:
- `DELTA >= 0`: Pass — "N tests (delta: +M)"
- `DELTA < 0`: Fail — "N tests (delta: -M — tests were removed)"
- Unable to compare: SKIP — "CI test count not available in artifacts"

---

## OUTPUT FORMAT

```
## /housekeeping — Repo Health Report

| Check | Status | Detail |
|-------|--------|--------|
| Stale local branches  | PASS | 4 local branches |
| Debug output in src   | PASS | — |
| Tracked .env          | PASS | — |
| Tracked backup files  | PASS | — |
| TODO/FIXME count      | INFO | 6 TODO/FIXME |
| go vet                | PASS | — |
| CI test count delta   | PASS | 122 tests (delta: +0) |

**5 passed, 0 failed** (1 informational)
```

---

## RULES

1. Read-only — never modify, commit, push, or open a PR.
2. INFO checks never count as failed.
3. SKIP is not failure.
4. No auto-fix — report only.
5. `fmt.Fprintf(os.Stderr, ...)` in `cmd/` is intentional — don't flag it.
6. If any FAIL: end with "Run /housekeeping again after fixing the issues above."
