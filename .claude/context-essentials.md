# Context Essentials — vmm-rada

> Re-injected into session context after compaction (via SessionStart hook
> with matcher='compact') and emphasized to the compactor (via PreCompact
> hook). Source of truth for rules that MUST survive context summarization.
>
> Keep this file under ~60 lines — every line costs tokens on each
> re-injection.

## Frontend

`frontend/` was removed from this repo (2026-07-19) — active frontend
development is at [`vmm-rada-web-ui`](https://github.com/valpere/vmm-rada-web-ui).
This repo is the backend API only.

## Stack constraints

- Backend: **Go**. Run `go build`, `go vet`, `go test ./...` before `/deliver`.
- LLM Gateway: configurable (`AI_PROVIDER_NAME`, default `openrouter`). Key in `.env` as `AI_PROVIDER_API_KEY`.

## Workflow gates

```
/backlog → Tech Lead (APPROVED) → gh issue create → plan file deleted
    → /deliver → code-generator → [/fix-review rounds] → squash merge
```

- **Plans** live in `.claude/plans/` with frontmatter (type, priority, labels,
  github_issue). After issue creation, delete the plan file.
- **Tech Lead approval** is the gate before any code generation.
- **PRs** are squash-merged. Never merge commits or rebase-merge.
- **`/fix-review`** runs parallel multi-model review (see `config.yaml`) + Claude arbiter.

## Docs discipline

- **Mark planned vs current explicitly.** When a doc describes a feature
  not yet wired into code, prefix the section with `PLANNED:` or
  `NOT YET WIRED:`. Never write future-tense behaviour as if it were
  current. Recurring `/fix-review` theme.
- **Docs land in the same PR as the change, never a follow-up.** Which
  docs depends on what changed: strategy → `strategies.md`,
  `strategy-showcase.md`, `architecture-v2.md`; env var or
  `configs/council.yaml` key → `architecture-v2.md`, `user-guide.md`,
  `README.md`; REST/SSE shape → `api.md` **and** `openapi.yaml` (never
  one alone). Update `CLAUDE.md` too if it states the changed fact.

## Banned patterns

- No `--no-verify` on git operations.
- No commits skipping pre-commit hooks unless user explicitly requests.
- No `fmt.Println` in `cmd/` packages — use the configured logger.
