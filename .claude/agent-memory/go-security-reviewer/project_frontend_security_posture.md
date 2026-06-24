---
name: Frontend security posture
description: Security architecture decisions and known issues for vmm-rada frontend (updated 2026-06-24)
type: project
last-verified: 2026-06-24
---

Frontend was originally merged into monorepo in PR #52 (2026-03-31).
The codebase underwent a v2 clean-slate rewrite. This memory was refreshed
2026-06-24; issues #53 and #54 no longer exist (issue tracker is empty).

**Positive controls (verified 2026-06-24):**
- All LLM output in Stage0/Stage1/Stage3/ChatInterface rendered through `<Markdown>`
  (react-markdown wrapper) — no raw HTML injection found.
- **Exception:** `Stage2.jsx:332,336,426,455` — 4 spots render bare JSX string children
  for debate/MoA content. Fidelity issue (markdown symbols render raw), NOT XSS
  (React escapes string children; no `dangerouslySetInnerHTML` found anywhere in
  `frontend/src`). Plan filed: `.claude/plans/2-dreaming-W25-stage2-markdown.md`.
- No hardcoded secrets or API keys in JS source.
- No dynamic code execution patterns found.
- `api.js` is the sole HTTP/fetch boundary — components do not call fetch directly.
- CORS allowlist on Go backend; `Access-Control-Allow-Methods` includes DELETE, PATCH.
- `VITE_API_BASE` is a build-time env var, not a runtime injection point.
- Stage2.jsx deAnonymizeText: `split(label).join(...)` pattern — immune to regex metacharacters.

**Known open security items:**
- No CSP / security headers on Go backend responses (low severity, no issue tracked).
- Go toolchain 1.26.3 has CVEs GO-2026-5039/5037 — fixed in 1.26.4. Plan filed:
  `.claude/plans/2-dreaming-W25-go-toolchain-cve.md`.

**How to apply:** When reviewing `frontend/src/components/`, confirm no component
uses `fetch()` directly or renders LLM content without the `<Markdown>` wrapper.
For Go API changes, check CORS `Access-Control-Allow-Methods` includes all verbs
the browser sends (DELETE, PATCH needed for conversation management).
