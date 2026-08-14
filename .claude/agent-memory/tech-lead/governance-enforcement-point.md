---
name: governance-enforcement-point
description: Review criteria belong in .claude/agents/<agent>.md only; skill Step lists are non-normative summaries and must never grow a second copy of a criteria list
type: project
---

When a plan proposes adding a **review criterion / gate / checklist item**, it goes in
the *agent prompt* (`.claude/agents/tech-lead.md`), never in the invoking skill's step
description (`.claude/skills/backlog/SKILL.md` Step 5, `/ship` Step 7, etc.).
Skill step bullets are **non-normative summaries** of what the agent does.

**Why:** `backlog/SKILL.md` Step 5 already lists only 4 of the 5 plan-review criteria
in `tech-lead.md` (it omits **Risk**) — proof it is a summary that has already drifted,
not the enforcement point. The agent prompt is what is actually loaded at review time;
a checklist item added only to the skill text is never read by the reviewer. Duplicating
into both files creates exactly the drift class this project keeps paying for (see
[[skill-doc-drift-fixreview]], [[stage0-done-not-on-wire]]).

**How to apply:**
1. Plan says "add checklist item to `<skill>/SKILL.md` Step N" → APPROVED WITH CHANGES,
   retarget to the agent prompt. Add an explicit "do not modify `<skill>/SKILL.md`"
   acceptance criterion so code-generator does not edit both.
2. Distinguish **plan-time** from **code-review-time** wording. Any criterion phrased
   "the diff must ..." cannot live in the *Plan review* section — at plan time there is
   no diff. Plan-time form: "the plan's Files-to-change list must include ...".
3. Same rule for the skill-vs-agent split generally: `/backlog`, `/ship`, `/fix-review`
   own *sequencing*; agent prompts own *judgment criteria*.

**Exception — the /fix-review arbiter has no agent file.** Step 5 is titled
"Arbiter pass (Claude, main instance)": the arbiter is the main instance executing the
skill, not a dispatched subagent. There is no `.claude/agents/arbiter.md` to retarget to,
and `tech-lead.md` governs plan/code review — a different reviewer on a different trigger.
So **arbiter judgment criteria legitimately live in `fix-review/SKILL.md` Step 5**, and
rule 1 above does not fire. Do not reflexively reject that shape. Test before applying
rule 1: *is there an agent prompt actually loaded at the moment this criterion must
fire?* If no, the skill file is the enforcement point. Confirmed on issue #335
(2026-08-14, shell false-positive class + verify-before-done notes).

Ruling issued 2026-08-14 while reviewing plan `1-dreaming-w32-backlog-docs-triad.md`
(docs-triad pre-flight gate, dreaming pass 2026-W32).
