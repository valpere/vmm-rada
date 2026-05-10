---
name: apply-dreaming
description: "Read the latest llm-council dreaming report and apply
  high-confidence findings to .claude/ memory, agents, context-essentials,
  and code. Goes through the project's standard /backlog → Tech Lead →
  /ship → /fix-review workflow for code-touching changes; direct edit for
  .claude/ tooling changes. Annotates report with [applied YYYY-MM-DD]
  markers. Usage: /apply-dreaming [week|latest]"
---

# /apply-dreaming (llm-council)

Weekly review skill for processing dreaming reports from
`.claude/dreaming/reports/`. Walks items interactively, routes
substantive code changes through the project's standard PR workflow,
applies tooling-only changes directly. Leaves an audit trail.

## When to invoke

- Monday morning after Sunday 04:00 cron-run produced a fresh report.
- Or any time after a manual `.claude/dreaming/dreaming.sh` run.
- Or `/apply-dreaming` (with optional `latest` or `2026-W##` argument).

## Inputs

- Optional argument: `latest` (default) or `YYYY-W##`.
- Project root: `~/wrk/projects/llm-council/llm-council/`.

## Steps

### 1. Locate report

```bash
WEEK="${1:-latest}"
DIR=".claude/dreaming/reports"
if [[ "$WEEK" == "latest" ]]; then
  REPORT=$(ls -1t "$DIR"/2026-W*.md 2>/dev/null | head -1)
else
  REPORT="$DIR/$WEEK.md"
fi
```

### 2. Parse into structured items

Read REPORT. For each numbered sub-item, extract:

- `id`, `title`, `confidence` (high/medium/low)
- `evidence` — paths/commits/PRs cited
- `suggestion`
- `category` — infer from suggestion verb:
  - "add to context-essentials" → `update-rules`
  - "rewrite memory / refresh stale facts" → `update-memory`
  - "merge / consolidate agents" → `update-agents`
  - "add new skill" → `add-skill`
  - "fix broken /ship workflow / `gh` invocation" → `fix-tooling`
  - "code change / refactor / fix bug" → `code-change`
  - else → `other`

### 3. Show TL;DR + counts

```
2026-W##: N items (X high, Y medium, Z low)
TL;DR: ...
Process all? [y/select/skip-low/abort]
```

### 4. Triage walk

Iterate `high → medium → low`. For each item:

```
[H 1/N] §<id>  <title>
  Evidence: <paths/commits>
  Suggestion: <suggestion>
  
  [a]pply / [s]kip / [v]erify-first / [e]vidence / [q]uit
```

For `low`: skip silently unless user opted in.

### 5. Apply per category

#### `update-rules` — context-essentials.md

Direct edit on `main` if branch protection allows (it doesn't currently —
project has protection). So:

1. Create branch: `git switch -c add-essentials-W##`
2. Edit `.claude/context-essentials.md`. Cite source via inline comment:
   `<!-- Refs: .claude/dreaming/reports/2026-W##.md §<id> -->`
3. Show diff, confirm, commit.
4. Push + `gh pr create` referencing the report.

#### `update-memory` — agent-memory/<agent>/<file>.md

These are local-only Claude Code files (often gitignored under
`.claude/agent-memory/`). Edit directly, no PR needed:

1. Read target memory file.
2. Apply suggested changes (rewrite if "stale", append if "missing").
3. Set `last-verified: <today>` in frontmatter.
4. No commit needed unless `.claude/agent-memory/` is git-tracked here
   (check `git check-ignore` first).

#### `update-agents` — .claude/agents/*.md

These ARE typically tracked. Same as `update-rules`:

1. Create branch.
2. Edit agent prompt(s).
3. Show diff, commit, push, PR.

#### `add-skill` — new file under .claude/skills/

1. Create branch.
2. Scaffold `.claude/skills/<name>/SKILL.md` with frontmatter.
3. Commit, push, PR.

#### `fix-tooling` — scripts under .claude/

1. Create branch.
2. Edit script.
3. Smoke-test if possible.
4. Commit, push, PR.

#### `code-change`

Substantive code changes go through the project's standard workflow.
**Don't edit code directly.** Instead:

1. Create a draft plan referencing the dreaming finding:
   `.claude/plans/<priority>-dreaming-W##-<slug>.md`
2. Plan frontmatter: `type`, `priority`, `labels`, `github_issue: null`.
3. Plan body: cite report §<id>, evidence, suggested change.
4. Tell user: "Created plan. Run `/backlog` to gate through Tech Lead,
   then `/ship` for implementation."

#### `other` — manual review

Print suggestion + evidence. Don't apply. Annotate
`[manual-review-required 2026-MM-DD]`.

### 6. Annotate report

After each applied item, append (don't modify original):

```markdown
> [applied 2026-MM-DD: <action>; commit <sha>; PR <num>]
```

For created plans:
```markdown
> [planned 2026-MM-DD: .claude/plans/<file>; awaiting /backlog]
```

For skipped:
```markdown
> [skipped 2026-MM-DD: <reason>]
```

### 7. Final summary

```
Applied: N (direct edits on .claude/ tooling)
Plans created: M (awaiting /backlog → Tech Lead → /ship)
Manual review: K
Skipped: P

PRs opened: <list>
Plans pending: <list>
Backup: /tmp/dreaming-W##-llm-council-backup-HHMM/

Next steps:
1. Watch PRs for Copilot review (one round).
2. Run /backlog on each plan to gate through Tech Lead.
3. Run /ship on approved plans.
```

## Constraints (CRITICAL)

- **NEVER push to main directly** — branch protection requires PR.
- **NEVER auto-apply low confidence** without explicit request.
- **NEVER skip Tech Lead gate for code-changes** — route through plans.
- **ALWAYS backup before destructive operations**.
- **ALWAYS cite report-section** in commit messages and PR body.
- **One PR per category-batch** (don't mix `update-rules` with
  `update-agents` in the same PR — different review focus).
- **Confirm before destructive ops** even at high confidence.

## Anti-patterns

- ❌ Edit code on main directly (will fail branch protection).
- ❌ Skip plan creation for code-changes (Tech Lead gate is mandatory).
- ❌ Mix tooling and code changes in one PR.
- ❌ Modify report's original suggestions (annotate only).
- ❌ Apply CORS-style "false claims" findings without verifying against
  current code first (the dreaming pass can be wrong; verify first).

## Companion skills

- `/backlog` — gate plans through Tech Lead.
- `/ship` — implement approved plan + create PR.
- `/fix-review` — handle Copilot/Tech Lead review rounds.
- `/revival` — health snapshot (synchronous, complementary to dreaming).

## See also

- `~/Documents/llm-wiki/wiki/dreaming.md` — concept and pattern.
- `.claude/dreaming/dreaming-prompt.md` — what's looked for in the pass.
- `.claude/context-essentials.md` — load-bearing rules (target of many
  promote-to-rules suggestions).
