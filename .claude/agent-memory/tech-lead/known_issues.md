---
name: known_issues
description: Known issues and open debt in the vmm-rada v2 codebase (updated 2026-06-24)
type: project
last-verified: 2026-06-24
---

**Why:** Helps future sessions skip re-analysis and focus on actual open debt.
**How to apply:** Check before opening new issues — may already be tracked here.

## v2 rewrite note

The codebase was fully rewritten from v1 to v2 (clean-slate). The previous
`known_issues.md` (last updated 2026-03-22) described v1 problems — all are
now either resolved or irrelevant to the new architecture. Dead issue refs
(#9, #13, #53, #54) were removed; those issues no longer exist.

## Open (as of 2026-06-24)

### Medium

- **Go toolchain CVE** — `go.mod` and `ci.yml` pin `1.26.3`; GO-2026-5039
  and GO-2026-5037 are fixed in 1.26.4. Dependabot does not cover the
  `toolchain` line — needs manual bump. Flagged W23, still open W25.

- **Stage2.jsx bypasses `<Markdown>` wrapper** — 4 spots render bare
  JSX string children instead of routing through `react-markdown`:
  `Stage2.jsx:332,336,426,455`. Fidelity risk (markdown symbols render raw),
  not XSS. Plan drafted: `.claude/plans/2-dreaming-W25-stage2-markdown.md`.

### Low

- **`/fix-review` skill vs practice** — skill documents 3 named agents
  (go-security-reviewer, code-simplifier, tech-lead); PRs #235/#238 used
  ollama parallel models. Doc/practice divergence unresolved since W23.
  Plan: `.claude/plans/2-dreaming-W25-fix-review-canonical.md`.

- **5 Dependabot PRs (#241–#245)** — open since 2026-06-02, all frontend
  dep bumps. Untriaged. Run `/review-deps` to process.

## Resolved (v2 baseline)

- ✅ API key validation at startup (config.go:109 errors if `AI_PROVIDER_API_KEY` empty)
- ✅ Graceful shutdown (cmd/server/main.go — SIGTERM/SIGINT → context cancel → drain)
- ✅ Stage3 error handling (v2 rewrite — function renamed and returns error correctly)
- ✅ Structured logging — `log/slog` throughout all packages
- ✅ HTTP client timeout — 120s in openrouter.Client
- ✅ All v1 PR-based fixes (PR #25–#37) — subsumed by v2 clean rewrite
