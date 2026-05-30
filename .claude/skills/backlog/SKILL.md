---
name: backlog
description: Plan the implementation of a feature or fix. Without args: shows top 5 queued plans to select from. With a task description or issue number: reads files, produces a plan, gets Tech Lead approval, creates a GitHub issue, then deletes the plan file. The GitHub issue is the canonical record.
user-invocable: true
argument-hint: "[task description | issue number]"
metadata:
  version: "1.3"
  author: backend-claude
  last_updated: "2026-03-21"
---

# /backlog

## Purpose

Prevent wasted effort by aligning on approach before touching code. For trivial changes
(one-line fix, typo) skip this and just do it. For anything that touches more than one
file or requires design decisions, plan first.

The plan file is a temporary working document — it is deleted once a GitHub issue is
created. The GitHub issue is the canonical record.

---

## Step 0: No argument — show plan menu

If `/backlog` is called with no argument, list draft plan files from `.claude/plans/`
(files without a GitHub issue yet), sorted by priority prefix, excluding `README.md`.
**Do NOT list open GitHub issues** — those are already created and belong to `/ship`.

```
Queued drafts:

  1. [p2] 2-health-check-endpoints.md — Add /health/live and /health/ready endpoints
  2. [p3] 3-chairman-consensus-prompt.md — Pass consensus score to chairman prompt

Or type a new task description:
```

Wait for user input, then proceed with the selected draft or new task description.

---

## Steps (with argument)

### 1. Understand the task

**If argument is a number:** check if it matches a plan file prefix (select that plan)
or a GitHub issue number (fetch it with `gh issue view <n> --repo valpere/vmm-rada`).

**If argument is a description:** check `.claude/plans/` for an existing plan matching
the description. If found, use it. If not, create a new one.

Read `CLAUDE.md` and relevant `docs/` if needed.

### 2. Read affected files

Identify and read every file that will change. Do not guess — read them.

Typical candidates per area:
- **API / HTTP** — `internal/api/handler.go`
- **Council logic** — `internal/council/council.go`, `interfaces.go`, `types.go`, `prompts.go`
- **Config** — `internal/config/config.go`
- **Storage** — `internal/storage/storage.go`
- **Entry point** — `cmd/server/main.go`
- **Tests** — `internal/council/council_test.go`, `internal/api/handler_test.go`
- **Frontend** — `frontend/src/App.jsx`, `frontend/src/api.js`, `frontend/src/components/`

### 3. Determine metadata

| Field | How to determine |
|-------|-----------------|
| `type` | `bug` / `feature` / `task` / `test` |
| `priority` | `p0`–`p3` based on impact and urgency |
| `debt` | `quick-fix` / `balanced` / `proper-refactor` |
| `effort` | `xs` / `s` / `m` / `l` / `xl` |
| `component` | which packages are touched (`frontend` for React changes) |
| `labels` | type label + priority label |
| filename prefix | matches priority digit: `0-`, `1-`, `2-`, `3-` |

### 4. Write (or update) the plan file

Save to `.claude/plans/{N}-{slug}.md` following the schema in `.claude/plans/README.md`.

This file is **temporary** — it will be deleted after the GitHub issue is created.

### 5. Tech Lead review

Launch the `tech-lead` agent with the plan content. The Tech Lead evaluates:
- Layer compliance (no boundary violations)
- Interface design (interfaces near consumers, compile-time checks)
- Scope (appropriately bounded to the issue)
- Debt level match (tests match declared ⚡/⚖️/🏗️)

Wait for verdict:
- **APPROVED** → proceed
- **APPROVED WITH CHANGES** → update plan file, proceed
- **REJECTED** → revise plan file, re-submit

Do not start `code-generator` until Tech Lead approves and user confirms.

### 6. Create GitHub issue

Always create after Tech Lead approval. Do not ask.

**Check for duplicates first:**
```bash
gh issue list --repo valpere/vmm-rada --state open --search "<title keywords>"
```

If a duplicate exists (same topic, less detailed body): close it, then create the new one.
```bash
gh issue close <old-number> --repo valpere/vmm-rada \
  --comment "Superseded by #<new-number> — replaced with plan-driven issue."
```

Create using only `## Summary` and `## Acceptance Criteria` — implementation details
stay internal:

```bash
gh issue create \
  --repo valpere/vmm-rada \
  --title "<type>(<component>): <title>" \
  --label "<comma-separated labels>" \
  --body "$(cat <<'EOF'
## Summary
<summary section from plan>

## Acceptance Criteria
<acceptance criteria from plan>
EOF
)"
```

### 7. Delete the plan file

After the issue is created, delete the plan file:
```bash
rm .claude/plans/{N}-{slug}.md
```

The GitHub issue is now the canonical record. The plan file is no longer needed.

### 8. Report and stop

Report the created issue URL and confirm the plan file was deleted. Stop.
Implementation is triggered separately via `/ship`.

---

## Output format

```
## Plan: <task name>  <scope emoji>

**Scope:** ⚖️ balanced — <one sentence>
**Type:** bug | feature | task | test
**Priority:** p1: high
**Effort:** s (1–2 hours)

**Files to change:**
- `internal/api/handler.go` — ...
- `internal/config/config.go` — ...

**Approach:**
1. ...
2. ...

**Risks:**
- Almost certainly: ...
- Unlikely: ...

**Not in scope:**
- ...

---
GitHub issue: #<number> — <url>
Plan file deleted: .claude/plans/{N}-{slug}.md
```
