---
name: review-deps
description: "Review and triage Dependabot PRs using the 6-stage security + stability pipeline. Creates GitHub issues for blocked major upgrades. Usage: /review-deps [--dry-run] [PR numbers...] | /review-deps all"
---

# Skill: /review-deps
# VMM Rada — Dependabot PR Triage Pipeline

---

## OVERVIEW

```
/review-deps                    → all open Dependabot PRs
/review-deps 12 13             → specific PRs
/review-deps --dry-run          → preview decisions, no merges/closes
```

6-stage pipeline per PR → Decision: MERGE / BLOCK / CLOSE+ISSUE / SKIP

---

## STEP 0: Parse Arguments

Detect `--dry-run`. Strip it. Remaining args:
- Empty or `all` →
  ```bash
  gh pr list --repo valpere/vmm-rada \
    --author "app/dependabot" --state open \
    --json number,title,headRefName --limit 50
  ```
- Numbers → validate each is a Dependabot PR

---

## STEP 1: Per-PR Pipeline

### Stage 1 — Classify

```bash
gh pr view {number} --json title,headRefName,labels
```

Semver bump: `patch` / `minor` / `major`. Ecosystem: `github-actions` or `npm` (frontend).

GitHub Actions majors → minor risk, skip Stages 3/5/6.

### Stage 2 — Security Check

```bash
gh pr view {number} --json body --jq '.body'
```

Scan for `CVE-*` or `GHSA-*`. High/critical → fast-track after CI.

### Stage 3 — Changelog

Skip for patches, github-actions, high CVEs.

Fetch release notes from GitHub. Red flags: "breaking change", "removed support for",
"changed default", "migration guide", "migration required".

`CHANGELOG_FLAGS=none|[flags]|unavailable`

Note: `godotenv` is the only non-stdlib Go dep — treat its updates conservatively.

Frontend known mappings:
- `vite` → `vitejs/vite`
- `typescript` → `microsoft/TypeScript`

### Stage 4 — CI Check

```bash
gh pr checks {number}
```

Poll pending (60s × 3). Trigger rebase if no CI: `gh pr comment {number} --body "@dependabot rebase"`.

`CI_STATUS=passed|failed|pending|no-runs`

### Stage 5 — Lockfile Review

**Backend (Go):** Check `go.sum` diff for unexpected new entries.
```bash
gh pr diff {number} -- go.sum 2>/dev/null | grep "^+" | wc -l
```
>10 new entries for a minor → suspicious.

**Frontend (npm):**
```bash
gh pr diff {number} -- frontend/package-lock.json 2>/dev/null | head -200
```
Count `^\+\s+"resolved"` lines. >5 (minor/patch) → suspicious.

Supply-chain signals: new `postinstall` in transitive dep → supply-chain-risk.

### Stage 6 — Bundle Impact

Frontend only: check if package is in `dependencies` or `devDependencies`.
Go backend: `BUNDLE_TYPE=go-stdlib-adjacent` — minimal risk by design.

---

## STEP 2: Decision Engine

```
1. CI_STATUS=failed                           → BLOCK
2. CI_STATUS=pending|no-runs                  → SKIP
3. LOCKFILE=supply-chain-risk                 → BLOCK
4. CVE=high AND CI=passed                     → MERGE (fast-track)
5. patch AND CI=passed                        → MERGE
6. github-actions AND CI=passed               → MERGE
7. minor AND CI=passed AND CHANGELOG=none AND LOCKFILE!=suspicious  → MERGE
8. minor AND CI=passed AND (unavailable OR suspicious)              → MERGE with note
9. major AND CI=passed AND CHANGELOG=none     → MERGE (comment: no breaking changes)
10. major AND CI=passed AND CHANGELOG=red-flags → CLOSE+ISSUE
11. major AND CHANGELOG=unavailable           → CLOSE+ISSUE
12. Fallback                                  → BLOCK
```

---

## STEP 3: Post PR Comment

```bash
gh pr comment {number} --body "## Dependabot Review
| Stage | Result |
|-------|--------|
| Classification | {BUMP_TYPE} · {OLD} → {NEW} |
| Security | {CVE_FOUND} |
| Changelog | {CHANGELOG_FLAGS} |
| CI | {CI_STATUS} |
| Lockfile | {LOCKFILE_FLAGS} |
| Bundle | {BUNDLE_TYPE} |
**Decision: {DECISION}** — {reason}"
```

Skip if `--dry-run`.

---

## STEP 4: Execute Decision

**MERGE:**
```bash
gh pr merge {number} --merge --auto
```

**BLOCK:** Leave open.

**CLOSE + CREATE GITHUB ISSUE:**

```bash
gh pr close {number} \
  --comment "Closing to track migration in a GitHub issue. This major version bump requires manual review."
```

```bash
gh issue create \
  --repo valpere/vmm-rada \
  --title "Migrate {package_name} from v{old_major} to v{new_major}" \
  --body "## Context

Dependabot PR #${number} was closed — major version bump requires manual migration.

**Package:** {package_name}
**Current version:** {old_version}
**Target version:** {new_version}
**Dependabot PR:** {pr_url}

## Why manual review

{reason — changelog flags or unavailable changelog}

## Checklist

- [ ] Read full migration guide / CHANGELOG for v{new_major}
- [ ] Identify breaking changes affecting this codebase
- [ ] Update all usages / imports in both \`internal/\` and \`frontend/\`
- [ ] Run \`go test -race -count=1 ./...\` after Go changes
- [ ] Run \`npm run build\` and \`npm run lint\` after frontend changes
- [ ] Verify CI passes
- [ ] Open migration PR" \
  --label "dependencies,enhancement"
```

---

## STEP 5: Final Summary

```markdown
## /review-deps Summary

| PR | Package | Bump | CI | Decision | Action |
|----|---------|------|----|----------|--------|

**Processed:** N · Merged: M · Blocked: B · Issues: T · Skipped: S
```

If `--dry-run`: prefix all actions with "(would)".

---

## RULES

1. CI must pass before any merge — no exceptions.
2. Never merge supply-chain-risk lockfile.
3. Never merge major with breaking-change changelog — always close + issue.
4. GitHub Actions majors are minor risk.
5. `godotenv` is the only external Go dep — major bump = careful changelog review.
6. Process sequentially.
7. `--dry-run` never modifies anything.
