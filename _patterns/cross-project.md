# Cross-Project Patterns

Patterns proven in 2+ areas/projects. Promoted automatically by /self-learn retro.

---

## govulncheck in pre-merge pipeline catches stdlib CVEs automatically (2026-05-09)

A `govulncheck` hook wired into the pre-PR pipeline (e.g. as a PreToolUse:Bash hook or CI step) catches Go stdlib CVEs without a separate scanning step. Fixes land in the same PR that introduced the exposure — no follow-up issue needed.

**Confirmed in:** llm-council `/fix-review` pipeline (PR #185 — caught GO-2026-4971 + GO-2026-4918 in go1.26.2, fixed by bumping to go1.26.3)

**Applies to:** any Go project with a CI or pre-merge review pipeline.
