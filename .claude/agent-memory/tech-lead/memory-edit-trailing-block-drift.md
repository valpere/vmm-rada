---
name: memory-edit-trailing-block-drift
description: When rewriting a memory file's body, the trailing Why/How-to-apply block is the most likely place to drift and contradict the rewritten section above it
type: feedback
---

When a memory file's main content is rewritten (e.g. a status changes from
"open gap" to "resolved"), explicitly re-read the closing `**Why:**` /
`**How to apply:**` block before considering the edit done — don't assume it
still agrees with what's above it.

**Why:** PR #340 rewrote `backend_security_posture.md`'s "Known open security
items" section from "CSP gap, unfixed" to "Resolved (security headers)", but
left the trailing "How to apply" line saying "The CSP gap above is real and
unfixed" unchanged. Same author, same PR, same file — the trailing block
simply wasn't re-scanned after the section above it changed meaning.
`/fix-review` caught it (2/3 models flagged it as a self-contradiction), not
the author. Surfaced by automated audit 2026-W33 §2 as a recurring shape (see
also PR #339's `review-verify-checked-out-branch.md` — a related but distinct
"verify before trusting" failure mode: that one is about the *reviewer*
trusting a stale working tree, this one is about the *author* not re-scanning
their own edit's trailing content).

**How to apply:** After any edit to a memory file's main body/status, do one
more pass over everything below it (Why/How-to-apply/Related lines) before
committing — treat "I changed the finding" and "I updated every sentence that
referenced the finding" as two separate steps, not one.

Related: [[review-verify-checked-out-branch]] — same family, different failure point.
