---
name: frontend-extraction-stale-prompts
description: After the 2026-07-19 frontend extraction, stale frontend content survives in 13 skill/agent prompt files — full inventory plus the grep hits that are legitimate and must NOT be pruned
type: project
---

`frontend/` was extracted to `vmm-rada-web-ui` on 2026-07-19; this repo is backend-only Go.
Stale frontend content still lives in prompt files under `.claude/`. Inventory verified 2026-08-14.

**Why:** A "prune stale frontend refs" sweep keeps recurring because each pass finds only a
subset. Two of these files (`ci-build-agent.md`, `static-analysis.md`, `ship/SKILL.md`,
`fix-review/SKILL.md`, `bug-fixer.md`) contain *executable* instructions (`cd frontend && npm run lint`)
that fail outright against a nonexistent directory — those are operationally harmful, not merely
descriptive drift. The rest are descriptive-only and lower urgency.

**Operationally harmful (executable commands against a nonexistent dir):**
`.claude/agents/ci-build-agent.md` (generates a whole frontend CI job + `VITE_API_BASE` secret),
`.claude/agents/static-analysis.md`, `.claude/agents/bug-fixer.md`,
`.claude/skills/ship/SKILL.md`, `.claude/skills/fix-review/SKILL.md`,
`.claude/agents/code-generator.md`, `.claude/skills/revival/SKILL.md`

**Descriptive-only drift:**
`.claude/agents/tech-lead.md`, `.claude/agents/code-simplifier.md`,
`.claude/agents/docs-maintainer.md`, `.claude/agents/pm-issue-writer.md`,
`.claude/skills/backlog/SKILL.md`, `.claude/skills/improve/SKILL.md`,
`.claude/skills/find-bugs/SKILL.md`

**Legitimate hits — never prune (a naive `grep -i frontend|jsx|react|npm run` catches these):**
- `.claude/agents/code-simplifier.md` ~L233 and `.claude/agents/go-security-reviewer.md` ~L242 —
  boilerplate *examples* inside the memory-system instruction block ("first time touching the React
  side of this repo"). Illustrative prose, not project invariants.
- `.claude/skills/self-learn/PORTABILITY-GUIDE.md` L104 — deliberately generic guidance for porting
  the skill to *other* projects, one of which may be a frontend.
- `.claude/skills/housekeeping/SKILL.md` L79 — Check 5 is a general TODO/FIXME counter whose
  `grep --include` list happens to contain `*.tsx`. Only the dead `--include` flags are stale;
  deleting Check 5 would destroy a working Go check.
- `.claude/agents/security-reviewer.md` — frontend-only *by design*; whether it is deleted, kept as a
  cross-repo tool, or moved to `vmm-rada-web-ui` is a standalone judgment call, tracked separately.
  `.claude/agents/go-security-reviewer.md` L21-24 cross-references it and must move in lockstep.

**How to apply:** When reviewing or writing any "prune frontend refs" plan, check it against this
inventory before approving. A plan asserting "grep returns no frontend hits afterwards" is
unachievable unless it covers all 13 files AND allowlists the five legitimate-hit sites above.
See [[frontend-prune-plan-corrections]] for the specific misreadings that plan 1 (2026-W32) shipped.
