# Development Workflow

This document describes the full development process for the VMM Rada frontend — from idea to merged code. It covers the roles of humans, skills, and agents, how they interact, and which tool to reach for in each situation.

---

## Actors

| Actor | What it is | How it works |
|-------|-----------|--------------|
| **Human** | The engineer driving the session | Types commands; approves agent plans; selects issues |
| **Skill** | A slash command (`/backlog`, `/ship`, …) | A SKILL.md file loaded by Claude Code; runs as a prompt macro in the main thread |
| **Agent** | A specialised subprocess | An agent file (`.claude/agents/<name>.md`) launched via the `Agent` tool; runs in its own context |

Skills and agents are different invocation mechanisms for Claude. Skills run in the same conversation context. Agents run in isolated sub-conversations and return a result.

---

## Skills

All skills are invoked with `/skill-name`. They are defined in `.claude/skills/` and registered per-machine with the symlink setup in `CLAUDE.md`.

### `/backlog` — Plan before coding

**When to use:** before implementing anything that touches more than one file or requires a design decision. Also to browse the open backlog.

**Entry points:**
- `/backlog` — show top 5 ready items; select one or enter a new task description
- `/backlog <task description>` — go straight to planning a specific task

**Flow:**
1. Checks `.claude/plans/` for an existing plan; checks GitHub for a duplicate issue
2. Reads all files that would change
3. Produces: files to change, approach, risks, commit message, frontmatter (type/priority/debt/effort/component)
4. Invokes `tech-lead` agent if the plan touches `App.jsx` state model, `src/api.js`, or new API calls
5. Offers to create a GitHub issue; writes the issue; deletes the plan file (issue becomes source of truth)
6. **Does not write any code** — confirmation required before implementation begins

### `/ship` — Full issue lifecycle

**When to use:** to implement, PR, review, and merge a single issue end-to-end.

**Entry points:**
- `/ship` — list the top 5 open unblocked issues; wait for selection
- `/ship <number>` — pick an issue by GitHub number
- `/ship <title>` — search issues by title; confirm best match

**Flow:**
1. Pick issue → branch off `main`
2. Implement → `npm run lint`
3. **Parallel pre-PR review:** launch `security-reviewer` + `static-analysis` simultaneously; address findings
4. Push → create PR with `Closes #N` and debt emoji in title
5. Poll for Copilot review (up to 5 min); address one round of comments using the Code Review Pyramid
6. Squash merge → `git checkout main && git pull`

**Rules:**
- One issue at a time
- Only PRs created by Claude or explicitly named — never touch Dependabot PRs
- One round of Copilot comments; do not loop on re-reviews

### `/fix-review` — Address review comments

**When to use:** when a PR has Copilot comments to address, or when a Dependabot PR needs triage.

**Copilot flow:**
1. Fetch all Copilot comments
2. Classify each by pyramid layer (see Code Review Pyramid below)
3. Assign a ruling: CONFIRM / ESCALATE / DISMISS / DEFER
4. Fix in priority order; run `npm run lint`; commit and push

**Dependabot flow (no Copilot wait needed):**
- **Patch bump** → merge immediately
- **Minor bump** → check changelog for breaking changes; merge if clean
- **Major bump** → write plan, create tracking issue via `/backlog`, comment PR, close PR without merging

### `/find-bugs` — Security and bug audit

**When to use:** to audit the current branch's changes for bugs, security issues, and code quality problems before opening a PR. Report-only — never modifies code.

Runs a 5-phase analysis: architecture violations, correctness issues, null-handling gaps, security patterns, and performance concerns.

### `/improve` — Critique a plan or design

**When to use:** before committing to an approach. Give it a plan, architecture proposal, or feature description; it returns a verdict.

**Verdicts:** SHIP IT · IMPROVE IT · RETHINK IT · KILL IT

Backs its verdict with concrete, actionable findings and best-practice references.

---

## Agents

Agents are launched via the `Agent` tool (by Claude or skills). They run in isolated sub-conversations. Some are invoked proactively by other agents; all can be invoked by the human directly.

All agents have persistent memory in `.claude/agent-memory/<agent-name>/`. They accumulate institutional knowledge across conversations.

### `tech-lead` — Architectural authority

**When invoked:**
- `/backlog` calls it before any plan touching `App.jsx` state, `src/api.js`, or new streaming components
- `code-generator` calls it at the start of implementation if the issue is architecturally sensitive
- The human can invoke it to review any code or plan

**What it does:**
- Enforces the adapter boundary (`src/api.js` is the only HTTP/SSE client)
- Enforces state ownership (`App.jsx` is the only writer)
- Reviews the complete Code Review Checklist (architecture, SSE boundary, state shape, error handling, security, correctness, React best practices)
- Rejects code that violates architecture; provides corrected patterns

### `bug-fixer` — Surgical one-bug fix

**When invoked:** reactively, when a runtime error or console error is reported.

One bug, one minimal fix, one commit. Does not refactor surrounding code. Branches → commits → pushes → opens PR (does not squash into existing branches).

### `code-simplifier` — Readability pass

**When invoked:** after implementation is complete (by `code-generator` or manually).

Behaviour preservation is absolute. Applies: optional chaining, nullish coalescing, guard clauses, array methods. Never touches: SSE parsing in `api.js`, `onEvent` dispatch logic, `loading` flags.

### `docs-maintainer` — Post-merge doc sync

**When invoked:** after significant changes merge — new SSE event types, new REST endpoints, new architectural patterns, or resolved proposals.

Updates `CLAUDE.md`, `docs/`, `.proposals.md`. Never modifies source code.

### `security-reviewer` — Security audit

**When invoked:**
- `/ship` calls it in parallel with `static-analysis` before creating the PR
- Can be invoked manually to audit any recently changed files

Report-only. Checks: XSS via LLM output rendering, API URL injection, SSE stream parsing safety, hardcoded secrets, CORS, sensitive data exposure.

### `static-analysis` — ESLint enforcement

**When invoked:**
- `/ship` calls it in parallel with `security-reviewer` before creating the PR
- Can be invoked manually when lint fails

Applies cosmetic fixes only (unused imports, unused variables). Flags semantic violations for escalation. One pass; reports remaining violations; never changes runtime behaviour.

### `code-generator` — Full implementation lifecycle

**When invoked:** when a GitHub issue is approved (by human or `tech-lead`) and ready to implement.

Manages the complete implementation flow: branch → code → lint → **parallel review** (security-reviewer + static-analysis) → code-simplifier → PR.

### `pm-issue-writer` — Issue drafter

**When invoked:** when a user request, bug report, or feature brief is too informal to implement directly.

Produces RFC 2119-compliant GitHub issue draft text (does not create the issue directly — pass the draft to `/backlog`).

### `ci-build-agent` — CI/CD pipeline

**When invoked:** when a GitHub Actions workflow needs to be created or modified; when CI fails due to workflow configuration.

Only modifies `.github/workflows/`. Uses `npm ci`, Node 20, concurrency cancellation. Never adds a test step (no test suite).

### `backend-sync` — Cross-repo coordination

**When invoked:** hourly by cron (re-created on `SessionStart`).

Checks the backend (same repo) for SSE contract changes, new REST endpoints, and new issues. Exchanges notes with the backend Claude instance via aga2aga.

---

## Agent Pipeline

Agents compose into pipelines for complex tasks:

### Feature implementation pipeline

```
Human or /backlog
    ↓ plan + issue
tech-lead (if architecture-sensitive)
    ↓ approval
code-generator
    ↓ implementation + lint
┌──────────────────────────────┐  ← parallel
│ security-reviewer            │
│ static-analysis              │
└──────────────────────────────┘
    ↓ findings addressed
code-simplifier
    ↓ readability pass
/ship (or manual PR)
    ↓ PR created
Copilot review → /fix-review (one round)
    ↓
squash merge → docs-maintainer (if architecture changed)
```

### Bug fix pipeline

```
Human reports error/symptom
    ↓
bug-fixer
    ↓ minimal fix, PR
/fix-review (Copilot round if needed)
    ↓
squash merge
```

### Dependabot pipeline

```
Dependabot opens PR
    ↓
/fix-review (auto-detect Dependabot author)
    ↓ patch
merge immediately
    ↓ minor
check changelog → merge if clean
    ↓ major
write plan → /backlog → tracking issue → close PR
```

---

## Code Review Pyramid

Used by `/fix-review` (Copilot rulings) and informally by all agents when reviewing code. Fix from the bottom up.

```
        ▲
       /5\    Style          → NEVER fixed manually — automated by ESLint
      /---\
     / 4   \  Tests          → N/A (no test suite in this project)
    /-------\
   /    3    \ Documentation → Is complex logic explained? Misleading names?
  /           \
 /      2      \ Correctness → Bugs, null gaps, stale closures, race conditions, security
/_______________\
       1          Architecture → SSE adapter boundary, App.jsx state model, design flaws
```

**Fix priority order within a PR:**
1. Layer 1 errors
2. Layer 1 warnings
3. Layer 2 errors
4. Layer 2 warnings
5. Layer 3 issues
6. Suggestions (any layer)

**Why bottom-up:** an architectural flaw (Layer 1) can make correctness fixes (Layer 2) irrelevant — you'd be polishing broken scaffolding.

**Rulings (per comment in `/fix-review`):**

| Ruling | Meaning | Action |
|--------|---------|--------|
| CONFIRM | Real issue, model was right | Fix it |
| ESCALATE | Real issue, more severe than flagged | Fix it, note severity upgrade |
| DISMISS | False positive or conflicts with CLAUDE.md | Skip, note reason |
| DEFER | Real but out of scope for this PR | Log only, do not fix |

---

## Branch, Commit, and PR Conventions

### Branches

```
feat/<short-description>     e.g. feat/stage3-error-ui
fix/<description>            e.g. fix/loading-stage3-never-clears
docs/<description>           e.g. docs/update-streaming-contract
refactor/<description>       e.g. refactor/extract-sse-handler
chore/<description>          e.g. chore/add-ci-workflow
```

### Commits

Conventional Commits format:

```
fix(scope): short description
feat(scope): short description
docs(scope): short description
refactor(scope): short description
chore(scope): short description
```

### PR titles

Include a debt emoji:

```
⚡ fix(api): buffer partial SSE lines across chunks
⚖️ feat(stage3): add error banner for SSE error events
🏗️ refactor(app): extract SSE event handler map
```

Debt levels:
- ⚡ **quick-fix** — targeted, no refactor, minimal blast radius
- ⚖️ **balanced** — sensible trade-offs, some refactoring acceptable
- 🏗️ **proper-refactor** — full refactor, higher effort, higher long-term payoff

### PR body template

```markdown
## Summary
- <bullet describing what changed and why>

Closes #N

## Test plan
- [ ] Dev server starts (`npm run dev`)
- [ ] Feature works end-to-end with backend running
- [ ] No console errors
```

---

## Agent Memory System

All agents maintain persistent memory in `.claude/agent-memory/<agent-name>/`. Memory files survive across conversations, giving agents institutional knowledge about user preferences, project decisions, and patterns to repeat or avoid.

### Memory types

| Type | Saves | Structure |
|------|-------|-----------|
| `user` | Role, preferences, working style | Free-form |
| `feedback` | Corrections and confirmed approaches | Rule · **Why:** · **How to apply:** |
| `project` | Ongoing decisions, goals, blockers | Fact · **Why:** · **How to apply:** |
| `reference` | External system pointers | Location + purpose |

Memory files use frontmatter (`name`, `description`, `type`) and are indexed in `MEMORY.md` inside each agent's directory.

**Not saved:** code patterns, architecture, file paths (derivable from the code), git history (use `git log`), anything in `CLAUDE.md`, or ephemeral task state.

---

## Onlooking Protocol (Backend ↔ Frontend)

The frontend and backend each have a sibling Claude instance. They exchange notes via append-only files:

| Direction | File | Writer | Reader |
|-----------|------|--------|--------|
| Frontend → Backend | aga2aga MCP | `backend-sync` agent | Backend Claude |
| Backend → Frontend | `.claude/.onlooking-from-backend.md` | Backend Claude | `backend-sync` agent |

**Rules:**
- The outbox is **append-only** — never overwrite; the receiver truncates after reading
- Use timestamped headings: `## [YYYY-MM-DD HH:MM] Frontend sync — <topic>`
- Silence is fine when nothing changed

The `backend-sync` agent runs hourly (re-created on `SessionStart`). It checks the backend git log for SSE contract changes and REST endpoint modifications, then sends a note if anything is relevant.

---

## Environment Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `VITE_API_BASE` | `http://localhost:8001` | Backend URL; trimmed and trailing slashes removed |

Set in a `.env` file (see `.env.example`). The variable is read at build time by Vite and at runtime via `import.meta.env.VITE_API_BASE`.

The backend must be running before starting the dev server. CORS is configured on the backend for `localhost:5173`.

---

## Quick Reference — What to Reach For

| Situation | Tool |
|-----------|------|
| New idea / feature request | `/backlog` |
| Informal request → GitHub issue | `pm-issue-writer` agent → `/backlog` |
| Implementing a GitHub issue | `/ship` or `code-generator` agent |
| Bug reported with a stack trace | `bug-fixer` agent |
| Dependabot PR open | `/fix-review` |
| PR has Copilot comments | `/fix-review` |
| Audit changed code for security | `security-reviewer` agent or `/find-bugs` |
| Lint failing | `static-analysis` agent |
| Plan seems risky / architectural | `tech-lead` agent or `/improve` |
| Code is complex / hard to read | `code-simplifier` agent |
| Just merged — docs need sync | `docs-maintainer` agent |
| CI workflow needed | `ci-build-agent` agent |
| Backend SSE contract changed | `backend-sync` agent (or check it manually) |
