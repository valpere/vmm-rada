---
name: review-criteria-need-output-slot
description: A review criterion added to tech-lead.md must also get a line in the printed Output format block, or it silently degrades — and blocking semantics must be stated if the gate is meant to block
type: project
---

When adding a criterion to `.claude/agents/tech-lead.md`, two mechanical follow-ons are
part of the change, not polish:

1. **Add a matching line to the printed Output format block.** The block is the template
   the reviewer actually fills in; a criterion with no slot there is reported only when
   the reviewer happens to remember it.
2. **State the verdict consequence.** The Code review block says *"Required changes
   before ship — Layer 1 findings only block"*. A criterion whose findings land at
   Layer 2–4 therefore cannot block a ship unless the criterion says so explicitly.
   A gate that cannot block is advisory.

**Why:** criterion 5 (**Risk**) is the control case — it has existed for a long time,
has *no* line in the Plan review Output format block, and is precisely the one criterion
that `backlog/SKILL.md`'s downstream summary dropped (see
[[governance-enforcement-point]]). Unprinted criterion → invisible in output → omitted
from every derived summary. Observed again on issue #334 (docs-triad sync gate, reviewed
2026-08-15): the added criterion 6 reproduced the same shape one level down — the PR
closing a drift class was itself missing its reporting slot.

**How to apply:** at code review of any `.claude/agents/*.md` change that adds a
criterion/checklist, diff the criteria list against the Output format block and require
parity before ship. Same test for the Code review table's `Layer` column — a new finding
class needs a layer it maps to, and if that layer is non-blocking, say so on purpose.
Do not silently widen an already-drifted downstream summary; delete the summary and
point at the agent file instead ([[prune-by-deletion-not-rewording]]).
