---
name: skill-doc-drift-fixreview
description: Skill docs that restate /fix-review's pipeline drift every time config.yaml changes — reference it, never duplicate model names or round structure
type: project
---

Any skill/agent doc that describes the `/fix-review` pipeline must **reference**
`.claude/skills/fix-review/SKILL.md` as canonical rather than restating it. Never
hardcode reviewer model names (`qwen3.5:cloud`, `minimax-m2.7:cloud`, …) or a
round-by-round agent breakdown into a second file.

**Why:** `/fix-review` is a *concurrent* 3-model dispatch reading models from
`config.yaml`, followed by a single Claude arbiter pass (CONFIRM/ESCALATE/DISMISS/
DEFER) — with a 3-tier failover (`reviewers.openrouter` → `reviewers.external_agents`
→ `reviewers.cli`). It is **not** a sequential 3-round pipeline of named agents.
`/ship/SKILL.md` drifted into describing the dead sequential model (`go-security-reviewer`
→ `code-simplifier` → `tech-lead`) and stayed wrong for months, in two places at once
(Step 7 *and* the Rules bullet). `fix-review/SKILL.md` itself carries the rule:
"The models to use are always read from `config.yaml`; do not hardcode model names here."

**How to apply:** When reviewing any plan that touches process docs mentioning review:
1. Reject hardcoded model names in any file other than `config.yaml`.
2. Reject "round 1/2/3 = <agent name>" phrasing — `round_1/2/3` are config *keys* for
   concurrent dispatch, named that for historical reasons only.
3. Grep the **whole file**, not just the named section — this drift class duplicates
   itself into summary/rules blurbs that plans routinely scope out.
4. Note that `go-security-reviewer`, `code-simplifier`, and `tech-lead` agents still
   exist and are used by other skills — the correction is "`/fix-review` does not
   dispatch to them", never "these agents are gone".

Related: [[stage0-done-not-on-wire]] — same recurring doc-drift shape.
