# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

VMM Rada — a multi-LLM deliberation system. Rada models independently answer a query,
anonymously peer-review each other, and a Chairman model synthesises a final answer.

**Status: v2 shipping.** The v1 implementation is archived on `archive/v1`. v2 is the active
codebase; the rewrite is well past the research phase.

See `docs/` for the current source of truth:
- `docs/architecture-v2.md` — package layout, layer boundaries, composition root, pipeline behaviour
- `docs/strategies.md` — the 7 deliberation strategies (all implemented), per-registration model config, quorum defaults, SSE protocol
- `docs/api.md` — REST + SSE event reference
- `docs/pipeline.md` — Stage 0/1/2/3 internals
- `docs/council-research-synthesis.md` — aggregated design research

All seven strategies are implemented (`PeerReview`, `RoleBased`, `Majority`,
`GenerateRankRefine`, `MultiAgentDebate`, `MixtureOfAgents`, `Delphi`). Stage 0
(clarification) runs before strategy dispatch and is strategy-independent.
See [`docs/strategies.md`](docs/strategies.md) for the full enum, status, and per-strategy
configuration.

## Stack

- **Backend:** Go 1.26.3
- **Frontend:** React 19 + Vite 8, plain JavaScript (no TypeScript) — lives in `frontend/`
- **LLM Gateway:** configurable provider via `AI_PROVIDER_NAME` (default `openrouter`); URL override via `LLM_API_BASE_URL` for Ollama / vLLM
- **API key:** `.env` → `AI_PROVIDER_API_KEY=<key>` (use any non-empty placeholder for keyless providers)

## Frontend architecture rules (immutable)

These constraints are enforced by the `tech-lead` agent and must not be violated:

1. **Components are pure UI.** No direct `fetch` or `api.js` calls from any component.
2. **`src/api.js` is the adapter boundary.** `onEvent(type, event)` is the only interface `App.jsx` sees. Raw SSE lines and HTTP status codes never leak past this layer.
3. **`App.jsx` owns all state.** Only `App.jsx` writes the assistant message shape via `setCurrentConversation`.
4. **`react-markdown` is the only renderer for LLM output.** Inserting raw HTML is forbidden — it is an XSS risk with LLM-generated content.

See `docs/frontend/` for the API contract, SSE streaming spec, component architecture, and user guide.

## Workflow

Full pipeline:
```
/backlog → Tech Lead (APPROVED) → gh issue create → plan file deleted
    → /ship → code-generator → [/fix-review rounds] → squash merge
```

### Skills

| Skill | Invoke | Purpose |
|-------|--------|---------|
| `/backlog` | `/backlog <task or issue#>` | Plan → Tech Lead gate → creates GitHub issue → deletes plan file |
| `/ship` | `/ship` | Select issue → implement → PR → Copilot → `/fix-review` → squash merge |
| `/fix-review` | `/fix-review [pr#]` | 3-round review (security + simplifier + tech-lead) + arbiter |
| `/find-bugs` | `/find-bugs` | Audit current branch changes for bugs/security — report only |
| `/improve` | `/improve <target>` | Critic pass: SHIP IT / IMPROVE IT / RETHINK IT / KILL IT |

### Agents (invoked by skills)

| Agent | Model | Role |
|-------|-------|------|
| `tech-lead` | opus | Approves plans + reviews code; architectural authority |
| `code-generator` | sonnet | Implements Tech Lead-approved plans |
| `bug-fixer` | sonnet | Targeted bug fixes; one bug, one commit |
| `docs-maintainer` | sonnet | Post-merge doc sync only |
| `ci-build-agent` | sonnet | Generates GitHub Actions CI workflows for Go + npm |
| `pm-issue-writer` | sonnet | Drafts RFC 2119 GitHub issues with structured frontmatter |

### Plans

Implementation plans live in `.claude/plans/`. Naming: `{N}-{slug}.md` where N is the
priority digit (0=critical, 3=low). Each plan has frontmatter with type, priority,
labels, and `github_issue` filled after issue creation.

See `.claude/plans/README.md` for the full schema.

### Debt levels

| Symbol | Level | Tests | Docs |
|--------|-------|-------|------|
| ⚡ | quick-fix | Happy-path only | Inline comments |
| ⚖️ | balanced | Core paths | Update if public API changed |
| 🏗️ | proper-refactor | Full unit + integration | Full update |

### Labels (GitHub)

**Type:** `bug` · `feature` · `task` · `test` · `docs`
**Priority:** `p0: critical` · `p1: high` · `p2: medium` · `p3: low`
**Status:** `blocked` · `wontfix` · `duplicate`

### PR workflow

1. Branch → implement → `go build/vet/test` all pass
2. `/ship` → creates PR → waits for Copilot review
3. Address comments → `/fix-review` → squash merge → `git checkout main && git pull`
