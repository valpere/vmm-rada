---
name: fix-review
description: Multi-model PR review pipeline. Dispatches the diff concurrently to 3 reviewer models (config.yaml), tallies vote counts per finding (informational), then Claude acts as arbiter (CONFIRM / DISMISS / DEFER) and merges when clean. Invoke with an optional PR number (defaults to the current branch's open PR).
user-invocable: true
argument-hint: "[pr-number]"
metadata:
  version: "2.0.0"
  domain: code-review
  scope: quality-gate
  debt-level: balanced
---

# /fix-review

Multi-model PR review pipeline for vmm-rada.

## Code Review Pyramid (arbiter evaluates in this order — base first)

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
Concurrent dispatch (config.yaml reviewers.openrouter.*):
  Reviewer model 1 (round_1) ──┐
  Reviewer model 2 (round_2) ──┼──→ JSON findings arrays
  Reviewer model 3 (round_3) ──┘
       ↓
  Vote tally: group by file:line, attach count N/3 (informational only)
  All findings reach the arbiter — votes do not gate
       ↓
  Arbiter (Claude, main instance)
    → full diff + all findings with vote metadata
    → CONFIRM / DISMISS / DEFER each finding
    → fix CONFIRM findings → commit+push
    → post PR comment with vote table
    → merge if no CONFIRM blockers remain
```

Note: `config.yaml` uses `round_1/round_2/round_3` keys for historical reasons — these
are concurrent dispatches, not sequential rounds. The models to use are always read from
`config.yaml`; do not hardcode model names here.

CLI failover tier (config.yaml `reviewers.cli`) engages automatically when the Ollama
cloud endpoint probe fails — same flow, local models instead of cloud.

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

Store it as the **baseline diff** (used in dispatch and arbiter pass).

### 2. Load reviewer config

Read `.claude/skills/fix-review/config.yaml`. Extract:
- `reviewers.openrouter.round_1/2/3` — cloud reviewer models
- `openrouter_api_url` — Ollama endpoint (`http://localhost:11434/v1/chat/completions`)
- `reviewers.cli` — local failover models (used if cloud endpoint unreachable)

Probe the endpoint and verify at least one configured model is loaded:
```bash
AVAILABLE=$(curl -sf --max-time 5 http://localhost:11434/v1/models 2>/dev/null \
  | grep -o '"id":"[^"]*"' | cut -d'"' -f4)

# Check if any of the three configured round models are in the available list
ROUND1_MODEL="<round_1 model from config>"
ROUND2_MODEL="<round_2 model from config>"
ROUND3_MODEL="<round_3 model from config>"

if echo "$AVAILABLE" | grep -qF "$ROUND1_MODEL" \
   || echo "$AVAILABLE" | grep -qF "$ROUND2_MODEL" \
   || echo "$AVAILABLE" | grep -qF "$ROUND3_MODEL"; then
  TIER="cloud"
else
  TIER="cli"   # endpoint up but no configured models loaded
  echo "⚠️  Ollama online but no configured reviewer models available — falling back to CLI tier"
fi
```

If endpoint unreachable OR no configured models loaded → use CLI failover tier (`reviewers.cli`).

### 3. Concurrent review dispatch

Build the review prompt combining the baseline diff with instructions:

> "Review this PR diff. Return ONLY a raw JSON array of findings — no prose, no markdown
> fences. Each finding: `{\"file\": \"path\", \"line\": N, \"layer\": 1-5, \"severity\":
> \"error|warn|sugg\", \"description\": \"...\"}`. Flag only real issues per the Code
> Review Pyramid. Layer 5 (style) is never flagged."

Send the prompt to each reviewer model via `ollama-review.sh`:

```bash
PROMPT="<diff + instructions>"

R1=$(echo "$PROMPT" | bash .claude/skills/fix-review/ollama-review.sh <round_1_model>)
R2=$(echo "$PROMPT" | bash .claude/skills/fix-review/ollama-review.sh <round_2_model>)
R3=$(echo "$PROMPT" | bash .claude/skills/fix-review/ollama-review.sh <round_3_model>)
```

Each call returns a JSON array (empty `[]` on parse failure — safe degradation).

### 4. Tally findings

Merge all three arrays. Group findings by `file:line`. For each unique `file:line`,
count how many of the 3 models flagged it.

Attach `votes: N/3` to each finding as **informational metadata only**. All findings
(even `votes: 1/3`) are passed to the arbiter — vote counts are a confidence signal,
not a gate. The arbiter's dismiss rate (~80%) is the actual filter.

### 5. Arbiter pass (Claude, main instance)

Re-fetch the full diff post-dispatch (should be unchanged, but confirms branch state):
```bash
gh pr diff $PR
```

For each finding (ordered Layer 1 first), apply the Code Review Pyramid:

| Ruling | Meaning | Action |
|--------|---------|--------|
| **CONFIRM** | Real issue, correctly identified | Fix it |
| **ESCALATE** | Real issue, more severe than flagged | Fix it, note severity upgrade |
| **DISMISS** | False positive or conflicts with project patterns | Skip, note reason |
| **DEFER** | Valid concern, out of scope for this PR | Create a GitHub issue |

Also run an **independent scan** of the full diff — look for anything the models missed.

For CONFIRM/ESCALATE findings:
1. Apply the fix using Edit.
2. Commit + push:
```bash
git add <files>
git commit -m "fix(pr#$PR): arbiter — address confirmed findings"
git push
```

For DEFER findings:
```bash
gh issue create --title "..." --body "..."
```

### 6. Post PR comment

Post a single collapsible summary:

```
<details>
<summary>/fix-review — parallel pass · N findings · N confirmed · N dismissed · N deferred</summary>

| File:Line | Votes | Layer | Sev | Ruling | Note |
|-----------|-------|-------|-----|--------|------|
| path/file.go:42 | 2/3 | 2 | error | CONFIRM | nil dereference on empty slice |
| path/file.go:87 | 1/3 | 5 | sugg | DISMISS | style — not flagged by pyramid |

Models: <round_1_model>, <round_2_model>, <round_3_model> (from config.yaml)
Arbiter: Claude Sonnet 4.6

</details>
```

### 7. Merge decision

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
| All findings arbitrated, no blockers | Merge |
| Cloud endpoint unreachable | Fall back to CLI tier, proceed |
| Model returns non-JSON | Treat as 0 findings for that model, proceed |
| Round fails to push | Stop, report error to user |
| PR already merged | Report and exit |
| PR has merge conflicts | Stop, ask user to resolve |
