# Context Essentials — llm-council

> Re-injected into session context after compaction (via SessionStart hook
> with matcher='compact') and emphasized to the compactor (via PreCompact
> hook). Source of truth for rules that MUST survive context summarization.
>
> Keep this file under ~60 lines — every line costs tokens on each
> re-injection.

## Frontend architecture (immutable)

These four rules are **load-bearing**. Violations break the architecture
contract and require Tech Lead override.

1. **Components are pure UI.** No `fetch` or `api.js` calls from any component.
2. **`src/api.js` is the adapter boundary.** `onEvent(type, event)` is the only
   interface `App.jsx` sees. Raw SSE lines and HTTP status codes never leak.
3. **`App.jsx` owns all state.** Only `App.jsx` writes the assistant message
   shape via `setCurrentConversation`.
4. **`react-markdown` is the only renderer for LLM output.** Inserting raw
   HTML is forbidden — XSS risk with LLM-generated content.

## Stack constraints

- Backend: **Go**. Run `go build`, `go vet`, `go test ./...` before `/ship`.
- Frontend: **React 19 + Vite 8, plain JavaScript**. No TypeScript.
- LLM Gateway: **OpenRouter API**. Key in `.env` as `OPENROUTER_API_KEY`.

## Workflow gates

```
/backlog → Tech Lead (APPROVED) → gh issue create → plan file deleted
    → /ship → code-generator → [/fix-review rounds] → squash merge
```

- **Plans** live in `.claude/plans/` with frontmatter (type, priority, labels,
  github_issue). After issue creation, delete the plan file.
- **Tech Lead approval** is the gate before any code generation.
- **PRs** are squash-merged. Never merge commits or rebase-merge.
- **`/fix-review`** runs 3 rounds: security + simplifier + tech-lead → arbiter.

## Docs discipline

- **Mark planned vs current explicitly.** When a doc describes a feature
  not yet wired into code, prefix the section with `PLANNED:` or
  `NOT YET WIRED:`. Never write future-tense behaviour as if it were
  current. Recurring `/fix-review` theme — see dreaming W19 §2.
- **Update `CLAUDE.md`, `architecture-v2.md`, `strategies.md`
  together** when a feature lands. Drift between these three is the
  most common review comment in this repo.

## Banned patterns

- No `--no-verify` on git operations.
- No direct `fetch` in components — must go through `src/api.js`.
- No raw HTML rendering of LLM output — `react-markdown` only.
- No state writes outside `App.jsx`.
- No TypeScript in `frontend/`.
- No commits skipping pre-commit hooks unless user explicitly requests.
- No `fmt.Println` in `cmd/` packages — use the configured logger.
