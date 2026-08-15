# Memory Index

- [project_architecture.md](project_architecture.md) — VMM Rada Go backend architecture and module map
- [known_issues.md](known_issues.md) — Prioritized list of improvements identified in initial analysis (2026-03-14)
- [context-essentials-inclusion-test.md](context-essentials-inclusion-test.md) — Bar for adding a line to context-essentials.md; why the W32 module-extraction rule was rejected as over-fit
- [skill-doc-drift-fixreview.md](skill-doc-drift-fixreview.md) — Never restate /fix-review's pipeline or model names in other skill docs; reference it instead
- [governance-enforcement-point.md](governance-enforcement-point.md) — Review criteria live in .claude/agents/*.md, not skill Steps — except the /fix-review arbiter, which has no agent file
- [docs-triad-sync-gate.md](docs-triad-sync-gate.md) — Strategy/config/wire-shape changes must ship their doc updates in the same PR; canonical doc target map
- [agent-scope-boundary-pairs.md](agent-scope-boundary-pairs.md) — security-reviewer retired 2026-08-14; deleting a cross-referencing agent requires editing the survivor's description + grep without name-substring filters
- [review-verify-checked-out-branch.md](review-verify-checked-out-branch.md) — Confirm HEAD matches the branch named in a review request; use `git show <branch>:<file>`, not the working tree
- [review-criteria-need-output-slot.md](review-criteria-need-output-slot.md) — New tech-lead.md criteria need a line in the printed Output format block + explicit blocking semantics
- [prune-by-deletion-not-rewording.md](prune-by-deletion-not-rewording.md) — In cleanup sweeps, reworded lines are riskier than deleted ones; a dropped qualifier can invert a rule
