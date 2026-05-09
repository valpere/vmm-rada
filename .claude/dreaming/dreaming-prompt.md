You are doing a **dreaming pass** for the **llm-council** project —
async, scheduled curation of project context. This is sleep-time
consolidation: review what accumulated since last pass, identify
patterns, suggest curation.

## Project context

- **llm-council** — multi-LLM deliberation system (Go backend + React frontend)
- Workflow: `/backlog → Tech Lead → /ship → Copilot → /fix-review → squash merge`
- Source of truth: `CLAUDE.md` (project instructions)
- See `.claude/context-essentials.md` for immutable rules

## Targets

| Path | What to look for |
|------|------------------|
| `.claude/context-essentials.md` | Are these rules still followed? Find recent commits that may violate them |
| `.claude/agent-memory/MEMORY.md` + per-agent dirs | Stale memories, outdated agent learnings |
| `.claude/plans/` | Plans that lingered too long, never made it to a PR |
| `.claude/agents/` | Agents with overlapping responsibilities |
| `.claude/skills/` | Skills that may be obsolete or duplicated |
| `git log --since="3 months ago"` | Recurring failure patterns, oft-reverted commits |
| Recent PR comments | Recurring `/fix-review` themes (security, simplifier, tech-lead) |

## What to find

### 1. Drift from context-essentials.md

For each rule in `.claude/context-essentials.md`, search recent commits
(last 30 days) for potential violations. Examples:
- "No `--no-verify`" → grep git log for `--no-verify`
- "No raw HTML rendering of LLM output" → grep frontend/ for the
  React-internal raw-HTML escape hatch (the `dangerouslySet...`
  attribute name) and any direct `innerHTML` writes
- "App.jsx owns all state" → grep frontend/src/components/ for `setCurrent`
- "Backend: Go, run `go test ./...` before /ship" → check CI pass rate

Flag violations with confidence level.

### 2. Recurring `/fix-review` themes

Read recent PR review comments via `gh pr list --state merged --limit 20`
and `gh pr view N --json comments`. Identify themes that come up multiple
times across PRs (e.g. "missing error handling in goroutines" appears
in 5 PRs). These are candidates for:
- New rule in `context-essentials.md`
- New skill or agent specialization
- Pattern documented in `.claude/agents/*/MEMORY.md`

### 3. Stale plans

Plans in `.claude/plans/` should be deleted after issue creation per
CLAUDE.md. Find any plans older than 14 days — they're either stuck
or forgotten.

### 4. Agent-memory health

Each agent (`code-generator`, `tech-lead`, etc.) has its own MEMORY.md
folder. Check:
- Memories that contradict context-essentials
- Memories not used in any recent session (mtime > 60 days)
- Duplicate memories across agents (suggest deduplicating to top-level)

### 5. Skill obsolescence

`.claude/skills/` may contain skills that were tried but now unused.
Cross-reference with recent slash-command usage in transcripts.

## Method

1. **Read** `.claude/context-essentials.md` first — it's the contract
2. **Sample** recent commits: `git log --oneline --since="30 days ago" | head -30`
3. **Sample** recent PRs: `gh pr list --state merged --limit 15 --json number,title,comments`
4. **Read** agent MEMORY.md files (selectively — one per agent)
5. **Inventory** plans, skills, agents
6. **Cross-compare** for patterns
7. **Output a report**

## Constraints (CRITICAL)

- **Read-only.** Do NOT modify any files.
- **No `git commit`, `gh pr edit`, `Edit`, `Write` calls.**
- **Allowed**: `Read`, `Glob`, `Grep`, `Bash` (read-only commands like `git log`, `gh pr view`, `ls`, `cat`, `wc`).
- **Cite sources** — every claim references a path or commit-SHA.
- **Confidence levels** — `high`/`medium`/`low`.

## Report format

```markdown
# llm-council dreaming pass — YYYY-W##

Date: YYYY-MM-DD
Branch surveyed: <current branch>
Commits examined: <count> (since <date>)
PRs examined: <count>

## TL;DR
- <2-4 highest-impact actionable items>

## 1. Context-essentials drift
- <rule>
  - Evidence: commit SHA / file:line
  - Severity: high/medium/low
  - Suggest: ...

## 2. Recurring /fix-review themes
- "<theme>" — n=<count> in PRs [#X, #Y, #Z]
  - Suggest: add to context-essentials? new skill?
  - Confidence: ...

## 3. Stale plans
- <plan-file> — age: <days>, status: ...
  - Suggest: ship / delete / revive

## 4. Agent-memory health
- <agent>:
  - Stale memories: N (oldest mtime)
  - Contradictions with context-essentials: ...
  - Duplicates with other agents: ...

## 5. Skill / agent inventory
- Unused skills: ...
- Overlapping agent roles: ...

## 6. Patterns I noticed
- ...

## 7. What I couldn't tell (need manual review)
- ...
```
