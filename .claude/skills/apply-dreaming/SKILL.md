---
name: apply-dreaming
description: "Read the latest vmm-rada dreaming report and apply
  high-confidence findings. Routes findings through the project's
  standard /backlog → Tech Lead → /ship → /fix-review workflow for
  code-touching changes AND for changes to load-bearing .claude/
  files (agent prompts, context-essentials). Direct edit only for
  agent-local memory and tooling scripts. Annotates report with
  [applied YYYY-MM-DD] markers."
user-invocable: true
argument-hint: "[week|latest]"
---

# /apply-dreaming (vmm-rada)

Walks each dreaming-report finding interactively and leaves an audit
trail (`[applied YYYY-MM-DD]` / `[planned …]` / `[skipped …]` markers
appended to the report).

## When to invoke

- Monday morning after Sunday 04:00 cron-run produced a fresh report.
- Or any time after a manual `.claude/dreaming/dreaming.sh` run.
- Or `/apply-dreaming` (with optional `latest` or `2026-W##` argument).

## Inputs

- Optional argument: `latest` (default) or `YYYY-W##`.
- Project root: `~/wrk/projects/vmm-rada/vmm-rada/`.

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

**Confidence inheritance.** Reports state confidence at the **section
level** (e.g. `§2 — confidence: high`), not always per item. When a
sub-item has no explicit `confidence` field, inherit it from the
enclosing section. Default to `medium` only when neither item nor
section declares a level.

**Idempotency — skip already-marked items.** If the next non-blank
line after an item starts with `> [applied …]`, `> [planned …]`,
`> [skipped …]`, or `> [manual-review-required …]`, skip the item
silently. The report is appended to (never rewritten) on each pass,
so re-running `/apply-dreaming` on the same report processes only
new items. Print a summary line at the start: `2026-W##: M new
items (N already-processed skipped)`.

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

**Routing rule.** Two paths only:
- **Plan-and-gate path** for anything load-bearing on agent or runtime
  behaviour: `code-change`, `update-rules` (context-essentials),
  `update-agents` (agent prompts), `add-skill`. These all draft a plan
  and route through `/backlog → Tech Lead → /ship → /fix-review`.
- **Direct-edit path** only for non-load-bearing artefacts:
  `update-memory` (agent-local memory, often gitignored) and
  `fix-tooling` (`.claude/dreaming/*.sh`, `.claude/hooks/*.sh`,
  internal helper scripts that don't change agent prompts).

Agent prompts and `context-essentials.md` ARE the runtime for the
agent workflow — including the Tech Lead. Editing them without a
gate would let dreaming reports silently steer the very agent that
should review the change. Always plan-and-gate.

#### `update-rules` / `update-agents` / `add-skill` — plan-and-gate

Same as `code-change` (see below). Draft a plan that cites the
dreaming finding; route through `/backlog`. Tech Lead reviews the
proposed change to load-bearing infrastructure before it lands.

#### `update-memory` — agent-memory/<agent>/<file>.md (direct)

Agent-local files (often gitignored under `.claude/agent-memory/`):

1. Read target memory file.
2. Apply suggested changes (rewrite if "stale", append if "missing").
3. Set `last-verified: <today>` in frontmatter.
4. Skip commit unless `.claude/agent-memory/` is git-tracked here
   (check `git check-ignore` first).

#### `fix-tooling` — scripts under .claude/ (direct + PR)

For helper scripts (`dreaming.sh`, hook scripts, etc.) that don't
embed agent prompts:

1. Create branch: `git switch -c fix/dreaming-W##-<slug>`.
2. Edit script.
3. Smoke-test: `bash -n <script>` for syntax; run with sample input
   if the script has a dry-run mode.
4. Commit, push, PR.

#### `code-change` — plan-and-gate

Substantive code changes (or any of the plan-and-gate categories
above) go through the project's standard workflow. **Don't edit
directly.** Instead:

1. Draft a plan file referencing the dreaming finding:
   `.claude/plans/<priority>-dreaming-W##-<slug>.md`
2. Plan frontmatter: `type`, `priority`, `labels`, `github_issue: ""`.
3. Plan body: cite report §<id>, evidence, suggested change, files
   to touch, acceptance criteria.
4. Tell user: "Created plan `<priority>-dreaming-W##-<slug>`. Run
   `/backlog <slug>` to gate it through Tech Lead — pass the **slug
   without the priority prefix** so `/backlog` finds the existing
   plan rather than drafting a fresh one. Then `/ship` for
   implementation."

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
Backup: /tmp/dreaming-W##-vmm-rada-backup-HHMM/

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

- `.claude/dreaming/dreaming-prompt.md` — what the dreaming pass looks for.
- `.claude/dreaming/README.md` — how the dreaming script is wired and run.
- `.claude/context-essentials.md` — load-bearing rules (target of many
  promote-to-rules suggestions).
