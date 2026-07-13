---
name: Frontend security posture
description: Security architecture decisions and known issues for vmm-rada frontend (updated 2026-07-13)
type: project
last-verified: 2026-07-13
---

Frontend was originally merged into monorepo in PR #52 (2026-03-31).
The codebase underwent a v2 clean-slate rewrite. This memory was refreshed
2026-06-24; issues #53 and #54 no longer exist (issue tracker is empty).

**Positive controls (verified 2026-07-13):**
- All LLM output in Stage0/Stage1/Stage2/Stage3/ChatInterface rendered through
  `<Markdown>` (react-markdown wrapper) — no raw HTML injection found. The
  former `Stage2.jsx` exception (bare JSX string children at 4 sites) was
  resolved by PR #252 — `Stage2.jsx:139,239,333,337,427,456` all now route
  through `<Markdown>`.
- No hardcoded secrets or API keys in JS source.
- No dynamic code execution patterns found.
- `api.js` is the sole HTTP/fetch boundary — components do not call fetch directly.
- CORS allowlist on Go backend; `Access-Control-Allow-Methods` includes DELETE, PATCH.
- `VITE_API_BASE` is a build-time env var, not a runtime injection point.
- Stage2.jsx deAnonymizeText: `split(label).join(...)` pattern — immune to regex metacharacters.

**Known open security items:**
- No CSP / security headers on Go backend responses (low severity, no issue tracked).
- Go toolchain — `1.26.3` CVEs (GO-2026-5039/5037) were fixed by the bump to
  `1.26.4` (PR #254). A new CVE surfaced 2026-07-12: **GO-2026-5856**
  (crypto/tls ECH privacy leak), fixed in `1.26.5`, currently un-patched.
  Rolling target — see dreaming report `2026-W28.md` §6 for trace sites
  (`cmd/server/main.go:192`, `internal/openrouter/client.go:202,228`,
  `internal/council/prompts.go:749`). Whether this warrants a `p1` is a
  Tech Lead call (ECH privacy-leak exploitability for this server's threat
  model), not yet decided as of 2026-07-13.

**How to apply:** When reviewing `frontend/src/components/`, confirm no component
uses `fetch()` directly or renders LLM content without the `<Markdown>` wrapper.
For Go API changes, check CORS `Access-Control-Allow-Methods` includes all verbs
the browser sends (DELETE, PATCH needed for conversation management).
