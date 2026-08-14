---
name: known_issues
description: Known issues and open debt in the vmm-rada v2 codebase (updated 2026-08-14)
type: project
last-verified: 2026-08-14
---

**Why:** Helps future sessions skip re-analysis and focus on actual open debt.
**How to apply:** Check before opening new issues — may already be tracked here.

## v2 rewrite note

The codebase was fully rewritten from v1 to v2 (clean-slate). The previous
`known_issues.md` (last updated 2026-03-22) described v1 problems — all are
now either resolved or irrelevant to the new architecture. Dead issue refs
(#9, #13, #53, #54) were removed; those issues no longer exist.

## Open (as of 2026-08-14)

None known. **Correction to the previous version of this file:** it claimed
"pure maintenance mode for ~7 weeks, no feature work" — that was false even
at the time and has been false since. Real feature work shipped between
2026-07-19 and now: `RoleBased` registered as a live opt-in strategy (#303),
`DevilsAdvocate` role + `Creator` rename + named per-role model assignment
(#322), YAML-based council registry replacing per-strategy env vars as the
primary config path (#323), `external_agents` tier 2 added to `/fix-review`
(#328, #329). The Go-toolchain-CVE recurrence (W23→W25→W28) is still
structurally closed: a scheduled daily `govulncheck.yml` CI gate (PR #286,
issue #283) auto-opens a `p1`/`security` issue on future toolchain CVEs — no
watch-item to track here for that.

## Resolved (W25 → W29)

- ✅ Go toolchain CVE (GO-2026-5039/5037 → 1.26.4, PR #254; GO-2026-5856 → 1.26.5, PR #285/#282)
- ✅ Go-toolchain-CVE detection automated — `govulncheck.yml` scheduled daily gate (PR #286/#283), replaces manual `/housekeeping` Check 8 as the primary detection path
- ✅ Stage2.jsx bypassing `<Markdown>` — all render sites now route through it (PR #252): `Stage2.jsx:139,239,333,337,427,456`
- ✅ `/fix-review` skill vs practice divergence — SKILL.md rewritten to canonical `config.yaml`-driven flow (PR #257)
- ✅ 5 Dependabot PRs (#241–#245) — all merged/closed by 2026-06-24; `/review-deps` skill itself later deleted (PR #249), superseded by `dependabot-reviewer` agent

## Resolved (W30 → W32)

- ✅ `RoleBased` orphan strategy (unreachable for months) — registered standalone,
  opt-in (#303); see [[rolebased-strategy-orphan]] — archived, not deleted, since it's
  the resolution record
- ✅ RoleBased expanded 4→5 roles (`DevilsAdvocate` added, `Generator`→`Creator`
  renamed), named per-role model assignment replacing positional `Models[i % len]`
  (#322)
- ✅ YAML-based council registry (`configs/council.yaml`, `COUNCIL_CONFIG_PATH`)
  replacing per-strategy env vars as the primary config path; also fixed `cmd/eval`'s
  duplicated registry silently missing `role-based` (#323)
- ✅ `/fix-review` `external_agents` tier 2 (cursor-agent, agy, omp, codex, opencode,
  kilo) added to the failover cascade (#328, #329)

## Resolved (v2 baseline)

- ✅ API key validation at startup (config.go:109 errors if `AI_PROVIDER_API_KEY` empty)
- ✅ Graceful shutdown (cmd/server/main.go — SIGTERM/SIGINT → context cancel → drain)
- ✅ Stage3 error handling (v2 rewrite — function renamed and returns error correctly)
- ✅ Structured logging — `log/slog` throughout all packages
- ✅ HTTP client timeout — 120s in openrouter.Client
- ✅ All v1 PR-based fixes (PR #25–#37) — subsumed by v2 clean rewrite
