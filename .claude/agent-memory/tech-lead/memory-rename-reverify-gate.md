---
name: memory-rename-reverify-gate
description: Renaming/rescoping an agent-memory file requires re-verifying every retained claim against code before bumping last-verified — carried-forward facts rot silently
metadata:
  type: feedback
---

When a PR renames, splits, or rescopes a file under `.claude/agent-memory/`, every
claim that survives the edit must be re-checked against the current code before
`last-verified:` is bumped. Do not treat "I only deleted the stale half" as
verification of the half that stayed.

**Why:** In issue #336 (retire `security-reviewer`, 2026-08-14),
`project_frontend_security_posture.md` → `backend_security_posture.md` correctly
dropped the frontend facts, but carried forward "No CSP / security headers on Go
backend responses" as the file's *only* open security item — and re-stamped the
file `last-verified: 2026-08-14`. `internal/api/handler.go`'s `wrap()` has set
`X-Content-Type-Options`, `X-Frame-Options` and `Content-Security-Policy:
default-src 'none'` since the v2 API scaffold (f7cd7ad). The claim was a v1-era
leftover. A false "open item" in a security agent's memory is worse than no
memory: it manufactures a recurring false positive on every review, with a fresh
verification date defending it.

**Evidence:** `.claude/agent-memory/cors-allowed-methods.md` shows the same decay
independently — it still asserts `Access-Control-Allow-Methods: "GET, POST,
OPTIONS"` while `handler.go:168` sends `"GET, POST, PATCH, DELETE, OPTIONS"`.
Two of two security-posture memories in this repo had drifted from code.

**How to apply:** On any code review touching `.claude/agent-memory/`, grep the
code for each retained factual assertion (header names, config values, file
paths) before approving. Bumping `last-verified:` without that grep is a
required-change finding, not a nit. Corollary for the same PR class: an
allowlist-style memory that names *other* files by path and line
(`frontend-extraction-stale-prompts.md`) becomes wrong the moment those files
are edited — a PR that edits an allowlisted site must update the allowlist in
the same commit. See [[agent-scope-boundary-pairs]] and
[[review-verify-checked-out-branch]].
