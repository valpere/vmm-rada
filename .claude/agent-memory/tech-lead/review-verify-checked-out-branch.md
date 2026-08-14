---
name: review-verify-checked-out-branch
description: Confirm HEAD equals the branch named in a code-review request before trusting any diff; this repo routinely has several task branches checked out at once
type: feedback
---

Before reviewing, run `git rev-parse --abbrev-ref HEAD` and compare it to the branch
named in the review request. If they differ, review via explicit refs —
`git diff main...<branch>` and `git show <branch>:<path>` — never the working tree.

**Why:** on 2026-08-14 a review request named `task/332-ship-skill-fix-fixreview-drift`,
but HEAD was `task/336-retire-security-reviewer`. Grepping the working-tree file returned
the *old* stale text and appeared to contradict the supplied diff, which would have
produced a false REJECT. `git show <branch>:<file>` is also immune to diff-output
post-processing hooks, which can reformat or mis-scope what `git diff` appears to print.

**How to apply:** first two commands of any code review. When they mismatch, say so in
the verdict (the ship/merge step may be operating on the wrong checkout) and pin every
subsequent check to the explicit ref. `git show <branch>:<file> | grep -n` is the
reliable way to assert "string X is absent from the file" for acceptance criteria.

Related: [[skill-doc-drift-fixreview]].
