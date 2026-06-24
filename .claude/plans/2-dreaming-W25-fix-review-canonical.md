---
type: task
priority: p2
labels: task, p2: medium
github_issue: ""
debt: quick-fix
effort: xs
---

# Decide and document canonical /fix-review approach

## Dreaming reference
§1.b of 2026-W25 report (and W23/W24 carryovers). Confidence: medium.

## Summary

`context-essentials.md:40` and the `/fix-review` skill both describe 3 named agents:
`go-security-reviewer → code-simplifier → tech-lead → arbiter`.

However, PRs #235 and #238 were reviewed using an "ollama parallel pass"
(qwen3-coder-next, minimax-m2.7, devstral-small-2 + Sonnet arbiter) — not those agents.

This doc/practice divergence has persisted 3+ weeks (W23→W25). Two possible resolutions:

**Option A — The 3-agent approach is canonical; the ollama run was a one-off.**
→ No doc change needed. Add a note to the fix-review skill: "The named agents are required;
ad-hoc model substitution is not canonical."

**Option B — The ollama parallel approach is now canonical (faster/cheaper).**
→ Update context-essentials.md:40 to say "multi-model review before merge" (remove agent names).
→ Update fix-review SKILL.md to describe the parallel-models approach.
→ This is the more significant change and needs Tech Lead sign-off on the new approach.

## Decision needed

Val should decide which option applies. This plan exists to force the decision and
route it through the Tech Lead gate before any doc change lands.

## Files to change (depends on option chosen)

- Option A: `.claude/skills/fix-review/SKILL.md` — add one-off vs canonical note
- Option B: `.claude/context-essentials.md` + `.claude/skills/fix-review/SKILL.md`

## Acceptance criteria

- context-essentials.md describes the actual canonical review approach
- fix-review skill is consistent with context-essentials
- No more doc/practice divergence flagged by future dreaming passes
