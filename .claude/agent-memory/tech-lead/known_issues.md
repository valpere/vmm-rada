---
name: known_issues
description: Known issues and open debt in the vmm-rada v2 codebase (updated 2026-07-19)
type: project
last-verified: 2026-07-19
---

**Why:** Helps future sessions skip re-analysis and focus on actual open debt.
**How to apply:** Check before opening new issues — may already be tracked here.

## v2 rewrite note

The codebase was fully rewritten from v1 to v2 (clean-slate). The previous
`known_issues.md` (last updated 2026-03-22) described v1 problems — all are
now either resolved or irrelevant to the new architecture. Dead issue refs
(#9, #13, #53, #54) were removed; those issues no longer exist.

## Open (as of 2026-07-19)

None. Repo has been in pure maintenance mode for ~7 weeks — tooling/deps
only, no feature work. The Go-toolchain-CVE recurrence (W23→W25→W28) is
now structurally closed, not just re-flagged: a scheduled daily
`govulncheck.yml` CI gate (PR #286, issue #283) auto-opens a `p1`/
`security` issue on future toolchain CVEs — no watch-item to track here
anymore.

## Resolved (W25 → W29)

- ✅ Go toolchain CVE (GO-2026-5039/5037 → 1.26.4, PR #254; GO-2026-5856 → 1.26.5, PR #285/#282)
- ✅ Go-toolchain-CVE detection automated — `govulncheck.yml` scheduled daily gate (PR #286/#283), replaces manual `/housekeeping` Check 8 as the primary detection path
- ✅ Stage2.jsx bypassing `<Markdown>` — all render sites now route through it (PR #252): `Stage2.jsx:139,239,333,337,427,456`
- ✅ `/fix-review` skill vs practice divergence — SKILL.md rewritten to canonical `config.yaml`-driven flow (PR #257)
- ✅ 5 Dependabot PRs (#241–#245) — all merged/closed by 2026-06-24; `/review-deps` skill itself later deleted (PR #249), superseded by `dependabot-reviewer` agent

## Resolved (v2 baseline)

- ✅ API key validation at startup (config.go:109 errors if `AI_PROVIDER_API_KEY` empty)
- ✅ Graceful shutdown (cmd/server/main.go — SIGTERM/SIGINT → context cancel → drain)
- ✅ Stage3 error handling (v2 rewrite — function renamed and returns error correctly)
- ✅ Structured logging — `log/slog` throughout all packages
- ✅ HTTP client timeout — 120s in openrouter.Client
- ✅ All v1 PR-based fixes (PR #25–#37) — subsumed by v2 clean rewrite
