# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

VMM Rada — a multi-LLM deliberation system. Rada models independently answer a query,
anonymously peer-review each other, and a Chairman model synthesises a final answer.

**Status: v2 shipping.** The v1 implementation is archived on `archive/v1`.

See `docs/` for the current source of truth:
- `docs/architecture-v2.md` — package layout, layer boundaries, composition root, pipeline behaviour
- `docs/strategies.md` — the 7 deliberation strategies (all implemented and registered), per-strategy config, quorum defaults, SSE protocol
- `docs/api.md` — REST + SSE narrative reference; `docs/openapi.yaml` is the machine-readable OpenAPI 3.2 contract, kept drift-proof by `internal/api/spec_test.go` and linted in CI
- `docs/pipeline.md` — Stage 0/1/2/3 internals
- `docs/council-research-synthesis.md` — aggregated design research

## Stack

- **Backend:** Go 1.26.5. Frontend was extracted (2026-07-19) to [`vmm-rada-web-ui`](https://github.com/valpere/vmm-rada-web-ui) (React 19 + Vite 8) — this repo is backend API only; `docs/api.md`/`docs/openapi.yaml` remain the authoritative contract regardless of which frontend consumes it.
- **LLM Gateway:** configurable provider via `AI_PROVIDER_NAME` (default `openrouter`); URL override via `LLM_API_BASE_URL` for Ollama / vLLM.
- **API key:** `.env` → `AI_PROVIDER_API_KEY=<key>` (any non-empty placeholder for keyless providers).

## Workflow

```
/backlog → Tech Lead (APPROVED) → gh issue create → plan file deleted
    → /ship → code-generator → /fix-review → squash merge → git checkout main && git pull
```

### Skills

| Skill | Invoke | Purpose |
|-------|--------|---------|
| `/backlog` | `/backlog <task or issue#>` | Plan → Tech Lead gate → creates GitHub issue → deletes plan file |
| `/ship` | `/ship [issue#]` | Select issue → implement → PR → `/fix-review` → squash merge |
| `/fix-review` | `/fix-review [pr#]` | Parallel multi-model review (`config.yaml`) + Claude arbiter |
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

Model shown here for quick reference — each agent's own frontmatter in `.claude/agents/*.md` is authoritative.

### Plans

Implementation plans live in `.claude/plans/`; naming convention, frontmatter schema,
and the priority↔label mapping are documented in `.claude/plans/README.md`. After
issue creation, the plan file is deleted — the GitHub issue is the canonical record.

### Debt levels

| Symbol | Level | Tests | Docs |
|--------|-------|-------|------|
| ⚡ | quick-fix | Happy-path only | Inline comments |
| ⚖️ | balanced | Core paths | Update if public API changed |
| 🏗️ | proper-refactor | Full unit + integration | Full update |

### GitHub labels

**Type:** `bug` · `feature` · `task` · `test` · `docs`
**Status:** `blocked` · `wontfix` · `duplicate`
**Priority:** `p0`–`p3` — see `.claude/plans/README.md` for the full priority↔label mapping.

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
