---
name: fix-review
description: Multi-model PR review pipeline. Runs 3 sequential reviewer rounds (each on the delta since the previous round), then an arbiter pass that confirms, dismisses, or defers each finding, then merges. Replaces the single-round GitHub Copilot loop with a deeper review cycle. Invoke with an optional PR number (defaults to the current branch's open PR).
user-invocable: true
argument-hint: "[pr-number]"
metadata:
  version: "1.0.0"
  domain: code-review
  scope: quality-gate
  debt-level: balanced
---

# /fix-review

Multi-model PR review pipeline for vmm-rada.

## Code Review Pyramid (fix in this order — base first)

```
        ▲
       /5\    Style       → NEVER flagged — go fmt handles this
      /---\
     / 4   \  Tests       → Critical paths covered for declared debt level?
    /-------\
   /    3    \ Docs        → Complex logic explained?
  /           \
 /      2      \ Implementation → Bugs, nil checks, goroutine leaks, security
/_______________\
       1          Architecture   → Layer violations, interface misuse, package cycles
```

**Priority:** Layer 1 errors → Layer 1 warnings → Layer 2 errors → Layer 2 warnings → Layer 3–4 → suggestions. An architectural flaw makes implementation fixes irrelevant — always fix from the base up.

## Pipeline

```
Round 1: go-security-reviewer  — full diff  → fix → commit+push
Round 2: code-simplifier       — delta diff → fix → commit+push
Round 3: tech-lead             — delta diff → fix → commit+push
Round 4: Arbiter (you)         — full diff + all round findings
                                 → CONFIRM / DISMISS / DEFER each finding
                                 → merge if no blockers remain
```

**Loop detection:** if round N flags ≥ 80% of the same file:line pairs as round N-1, stop — the reviewers are cycling. Proceed to arbiter.

## Step-by-step execution

### 0. Resolve PR

If an argument was given, use that PR number. Otherwise run:
```bash
gh pr view --json number,headRefName,state
```
Confirm the PR is open. Store the PR number as `$PR`.

### 1. Fetch the full diff

```bash
gh pr diff $PR
```

Store it as the **baseline diff** (used in Round 1 and Round 4).

### 2. Round 1 — Security review (go-security-reviewer agent)

Launch the `go-security-reviewer` agent with the full baseline diff as context.

Prompt: "Review this PR diff for security vulnerabilities (OWASP Top 10, injection, hardcoded secrets, unsafe API usage, insecure config). Report only actionable findings ranked by severity. Do not flag style."

For each High/Medium finding:
1. Apply the fix using Edit.
2. Stage + commit: `git commit -m "fix(pr#$PR): address review comments — round 1"`
3. Push: `git push`

After all fixes, capture the **Round 1 findings list** (file:line + severity + description) for loop detection.

### 3. Round 2 — Simplification (code-simplifier agent)

Fetch the delta since Round 1 baseline:
```bash
git diff origin/main...HEAD
```

Launch the `code-simplifier` agent with that delta diff.

Prompt: "Review only the changed code in this diff for unnecessary complexity, redundancy, or readability issues. Focus on implementation quality. Do not flag style."

For each finding:
1. Apply simplification.
2. Commit: `git commit -m "fix(pr#$PR): address review comments — round 2"`
3. Push.

Capture **Round 2 findings list** for loop detection. Compare against Round 1 — if ≥ 80% overlap on file:line, skip Round 3.

### 4. Round 3 — Architecture review (tech-lead agent)

Fetch the delta since Round 2:
```bash
git diff origin/main...HEAD
```

Launch the `tech-lead` agent with that delta diff.

Prompt: "Review the changed code for architectural violations: layer breaches, dependency inversion violations, missing interfaces, incorrect responsibility assignment. Focus on architecture and API design. Do not flag style."

For each finding:
1. Apply the fix.
2. Commit: `git commit -m "fix(pr#$PR): address review comments — round 3"`
3. Push.

Capture **Round 3 findings list**.

### 5. Round 4 — Arbiter (you, as main Claude instance)

Fetch the full PR diff again (post all fixes):
```bash
gh pr diff $PR
```

Consolidate all findings from Rounds 1–3. For each unique finding (de-duped by file:line),
apply the Code Review Pyramid — Layer 1 issues first:

| Ruling | Meaning | Action |
|--------|---------|--------|
| **CONFIRM** | Real issue, correctly identified | Fix it |
| **ESCALATE** | Real issue, more severe than flagged (e.g., warning → security error) | Fix it, note severity upgrade |
| **DISMISS** | False positive or conflicts with project patterns (often Layer 5 / style) | Skip, note reason |
| **DEFER** | Valid concern, out of scope for this PR | Create a GitHub issue |

Also run an **independent scan** of the full diff — look for anything rounds 1–3 missed.

For any fix commits in the arbiter round:
```bash
git commit -m "fix(pr#$PR): arbiter round — confirm, escalate, and independent findings"
```

Post a single PR comment summarising all rounds using collapsible blocks:

```
<details>
<summary>Round 1 — go-security-reviewer (N issues found, N fixed)</summary>

| File | Line | Layer | Severity | Status | Issue |
|------|------|-------|----------|--------|-------|
| internal/api/handler.go | 82 | 2 | error | fixed | ... |

</details>

<details>
<summary>Round 2 — code-simplifier (N issues found, N fixed)</summary>

| File | Line | Layer | Severity | Status | Issue |
|------|------|-------|----------|--------|-------|

</details>

<details>
<summary>Round 3 — tech-lead (N issues found, N fixed)</summary>

| File | Line | Layer | Severity | Status | Issue |
|------|------|-------|----------|--------|-------|

</details>

<details>
<summary>Round 4 — Arbiter (Claude) · N confirmed · N escalated · N dismissed · N deferred</summary>

| File | Line | Layer | Ruling | Issue |
|------|------|-------|--------|-------|
| internal/council/council.go | 130 | 2 | CONFIRM | ... |
| internal/api/handler.go | 45 | 5 | DISMISS | style preference, not flagged by pyramid |

</details>
```

For any DEFER items, create a GitHub issue:
```bash
gh issue create --title "..." --body "..."
```

### 6. Merge decision

If the diff contains files under `frontend/`, run:
```bash
cd frontend && npm run lint
```
Block merge if lint fails.

**Proceed to merge** if:
- No unresolved CONFIRM blockers remain
- All High-severity security findings are CONFIRM (fixed) or DISMISS (justified)

**Block merge** if:
- Any unfixed High-severity security finding exists

Merge with squash:
```bash
gh pr merge $PR --squash --delete-branch
```

Then sync main:
```bash
git checkout main && git pull
```

## Exit conditions

| State | Action |
|-------|--------|
| All rounds complete, no blockers | Merge |
| Loop detected (≥80% overlap) | Skip to arbiter |
| Round fails to push | Stop, report error to user |
| PR already merged | Report and exit |
| PR has merge conflicts | Stop, ask user to resolve |
