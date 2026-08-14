---
name: Backend security posture
description: Security architecture decisions and known issues for vmm-rada backend (updated 2026-08-14)
type: project
last-verified: 2026-08-14
---

Renamed from `project_frontend_security_posture.md` on 2026-08-14 — the
`security-reviewer` agent (frontend-scoped) was retired since `frontend/`
moved to `vmm-rada-web-ui` on 2026-07-19, and this project no longer has any
frontend code in this repo for that agent to review. The frontend-specific
observations below (react-markdown, `api.js`, `Stage2.jsx`, `VITE_API_BASE`)
were dropped; the backend facts that remain are still live and load-bearing.

**Known open security items:**
- No CSP / security headers on Go backend responses (low severity, no issue
  tracked).

**Resolved (Go toolchain CVE treadmill):**
- `1.26.3` CVEs (GO-2026-5039/5037) fixed by the bump to `1.26.4` (PR #254).
  `1.26.4`'s own **GO-2026-5856** (crypto/tls ECH privacy leak) fixed by the
  bump to `1.26.5` (PR #285, issue #282) — Tech Lead confirmed `p2` (server
  terminates no inbound TLS, ECH never configured). `go.mod` currently pins
  `1.26.5`.
- This recurring manual-detection gap is now closed structurally: a
  scheduled daily `govulncheck.yml` CI gate (PR #286, issue #283) opens a
  `p1`/`security` issue automatically on future toolchain CVEs — no longer
  dependent on a dreaming pass noticing.

**How to apply:** For Go API changes, check CORS `Access-Control-Allow-Methods`
includes all verbs the browser sends (DELETE, PATCH needed for conversation
management). The CSP gap above is real and unfixed — flag it again if you
review `internal/api/handler.go`'s response headers, but it has no tracked
issue yet.
