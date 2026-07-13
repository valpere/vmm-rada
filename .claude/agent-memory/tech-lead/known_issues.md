---
name: known_issues
description: Known issues and open debt in the vmm-rada v2 codebase (updated 2026-07-13)
type: project
last-verified: 2026-07-13
---

**Why:** Helps future sessions skip re-analysis and focus on actual open debt.
**How to apply:** Check before opening new issues — may already be tracked here.

## v2 rewrite note

The codebase was fully rewritten from v1 to v2 (clean-slate). The previous
`known_issues.md` (last updated 2026-03-22) described v1 problems — all are
now either resolved or irrelevant to the new architecture. Dead issue refs
(#9, #13, #53, #54) were removed; those issues no longer exist.

## Open (as of 2026-07-13)

None. All items tracked as of the W25 dreaming pass were resolved by
2026-07-01 (see W25→W28 resolutions below). Repo has been in pure
maintenance mode since — tooling/deps only, no feature work — see
dreaming report `2026-W28.md` §6 for the current watch item (Go
toolchain CVE GO-2026-5856, fixed in 1.26.5).

## Resolved (W25 → W28, closed by 2026-07-01)

- ✅ Go toolchain CVE (GO-2026-5039/5037) — `go.mod` bumped to `1.26.4` (PR #254)
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
