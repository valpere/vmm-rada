---
name: frontend-prune-plan-corrections
description: Dreaming-report-sourced plans cite section names/line locations that do not exist in the target file — verify every structural claim against the file before approving
type: feedback
---

Plans generated from dreaming-report inventories must have every structural claim
("Phase 3B", "Step 3A", "the `App.jsx` reference in X") verified against the live file before
APPROVED. Do not trust the plan's description of a file's structure.

**Why:** A plan describes a *location* that code-generator will act on literally. If the location is
wrong, code-generator either finds nothing and silently skips the criterion, or finds something
adjacent and deletes the wrong thing. Both failures are invisible in review because the acceptance
criterion reads as satisfied.

**Evidence:** Plan `1-dreaming-w32-frontend-prune.md` (2026-08-14) had three structural claims wrong
out of seven: (a) "housekeeping Phase 5 `.tsx` TODO/FIXME check" — no Phase 5 exists; it is Check 5,
a general Go TODO counter that merely lists `*.tsx` among its `--include` flags, and deleting it
would have destroyed a working backend check; (b) "improve Step 3A Frontend" — it is one bullet
inside step 3A, not a step, so deleting "Step 3A" would have taken four backend layer-boundary
questions with it; (c) "code-generator ... `App.jsx`/`api.js` references removed" — that file
contains no such references at all.

**How to apply:** For any plan whose acceptance criteria name headings, phases, steps, or symbols,
grep/read each one first. Report mismatches as required corrections to the criteria wording — the
plan's *direction* is usually right even when its *coordinates* are wrong, so this is
APPROVED WITH CHANGES, not REJECTED. Also treat "grep returns no hits afterwards" criteria as
suspect: they are only meetable if the file inventory is exhaustive and legitimate hits are
allowlisted. See [[frontend-extraction-stale-prompts]].
