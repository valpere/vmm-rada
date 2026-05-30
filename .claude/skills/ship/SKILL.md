---
name: ship
description: Implement a GitHub issue end-to-end — branch, code, tests, PR, Copilot review (once), merge, resolve. Without args: shows top 5 unblocked issues to select from. With issue number or title: ships that issue directly.
user-invocable: true
argument-hint: "[issue-number | issue-title]"
metadata:
  version: "3.1"
  author: backend-claude
  last_updated: "2026-04-16"
---

# /ship

Implement one GitHub issue from selection to merged PR, then present the next.

```
select issue → read issue + files → resolve uncertainties → branch → implement → pre-flight → PR → Copilot (once) → address → merge → Resolved → next
```

## Rules

- **One issue at a time.** Never work on multiple issues in parallel.
- **Branch protection** — no direct pushes to `main`. Always use a PR.
- **Copilot review once.** Poll for it yourself. Address comments if any. Then merge. Do not wait for a second review.
- Only ship PRs created by Claude or explicitly named by the user. Never touch Dependabot PRs.
- After merge: checkout main, pull, then present the next unblocked issue.

---

## Step 0: Select issue

**`/ship <number>`** — fetch that issue directly, skip menu.

**`/ship <title>`** — search open issues for a title match, skip menu if unambiguous.

**`/ship` (no args)** — list the top 5 open, unblocked issues sorted by priority:

```bash
gh issue list --repo valpere/vmm-rada --state open \
  --json number,title,labels \
  --jq '[.[] | select(.labels | map(.name) | contains(["blocked"]) | not)]
        | sort_by(
            (.labels | map(.name) | map(
              if . == "p0: critical" then 0
              elif . == "p1: high" then 1
              elif . == "p2: medium" then 2
              else 3 end
            ) | min) // 3
          )
        | .[:5]
        | to_entries[]
        | "\(.key + 1). #\(.value.number) \(.value.title) [\(.value.labels | map(.name) | join(", "))]"'
```

If **all open issues are blocked**, say so and stop — do not show a menu.

Display as a numbered menu and wait for selection.

---

## Step 1: Read the issue

```bash
gh issue view <number> --repo valpere/vmm-rada --json title,body,labels
```

Read `## Summary` and `## Acceptance Criteria`. These define what done looks like.

---

## Step 2: Read affected files

Read every file that will change. Do not guess — read them first.

Typical candidates:
- **API / HTTP** — `internal/api/handler.go`
- **Rada logic** — `internal/council/council.go`, `interfaces.go`, `types.go`, `prompts.go`
- **Config** — `internal/config/config.go`
- **Storage** — `internal/storage/storage.go`
- **Entry point** — `cmd/server/main.go`
- **Tests** — `internal/council/council_test.go`, `internal/api/handler_test.go`
- **Frontend** — `frontend/src/App.jsx`, `frontend/src/api.js`, `frontend/src/components/`

Run `go build ./...` to confirm baseline compiles.

---

## Step 3: Resolve uncertainties

Before touching any code, identify everything that is ambiguous or has more than one valid approach.

**Look for:**
- Naming mismatches — env var names, field names, or method signatures that differ between the issue, `CLAUDE.md`, `.env.example`, and existing code
- Multiple valid implementation approaches with real trade-offs
- Missing prerequisites — types, interfaces, or packages the issue depends on that don't exist yet
- Conflicting constraints — two parts of the spec that can't both be satisfied as written

**For each ambiguity, investigate first.** Read related files — `CLAUDE.md`, `docs/`, `.env.example`, existing package code — to find a ground truth. Many apparent ambiguities resolve silently from the codebase.

**Then decide:**

| Situation | Action |
|-----------|--------|
| One clear answer from docs/code | Resolve silently. Note the source. Proceed. |
| Multiple valid options with real trade-offs | List them numbered. **Stop and wait for user selection.** |
| No solution found — spec is genuinely incomplete | State what is missing. **Stop and ask for clarification.** |

Do not branch until all uncertainties are resolved. A question answered before implementation costs nothing; a question discovered during Copilot review costs a round-trip.

---

## Step 4: Create branch and implement

```bash
git checkout -b <type>/<slug>   # e.g. fix/cors-origins, test/handler-tests
```

Implement changes within layer boundaries:

```
cmd/server/main.go     ← wiring only
internal/api/          ← parse → call interfaces → respond; no logic
internal/council/      ← deliberation logic; no net/http
internal/storage/      ← persistence; no net/http, no council
internal/openrouter/   ← LLM calls; no council, no storage
```

---

## Step 5: Pre-flight

```bash
go build ./...
go vet ./...
go test ./...
git status
git log main..HEAD --oneline
```

If changes touch `frontend/`:
```bash
cd frontend && npm run lint && npm run build
```

Fix any failures from your changes before proceeding. Note pre-existing failures separately.

---

## Step 6: Create PR

Push the branch and open a PR:

```bash
git push -u origin <branch>

gh pr create \
  --title "<debt-emoji> <type>(<scope>): <description>" \
  --body "$(cat <<'EOF'
## Summary
<bullet points>

Closes #<issue-number>

## Test plan
- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] `npm run lint` passes (frontend changes only)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Debt emoji: `⚡` quick-fix · `⚖️` balanced · `🏗️` proper-refactor

---

## Step 7: Wait for Copilot review (poll yourself)

Check review status yourself — do not ask the user:

```bash
# Poll until a review appears or 5 minutes pass
gh pr view <number> --repo valpere/vmm-rada --json reviews,statusCheckRollup
```

Or watch checks:
```bash
gh pr checks <number> --watch --interval 30
```

Once Copilot has posted its review (or 5 minutes have elapsed with no review), proceed.

---

## Step 8: Address Copilot comments (if any)

Fetch the review comments:

```bash
gh pr view <number> --repo valpere/vmm-rada --json reviews \
  --jq '.reviews[] | select(.author.login == "copilot-pull-request-reviewer") | .body'

gh api repos/valpere/vmm-rada/pulls/<number>/comments \
  --jq '.[] | "[\(.path):\(.line)] \(.body)"'
```

- If **no comments**: skip to Step 9.
- If **there are comments**: address each one, commit the fixes, push.

```bash
git add <files>
git commit -m "address Copilot review comments"
git push
```

**One round only.** Do not wait for re-review after pushing fixes.

---

## Step 9: Merge

```bash
gh pr merge <number> --squash --delete-branch
git checkout main && git pull
```

---

## Step 10: Resolve and report

The `Closes #N` in the PR body auto-closes the issue on merge. Verify:

```bash
gh issue view <number> --repo valpere/vmm-rada --json state,closed
```

If not closed automatically:
```bash
gh issue close <number> --repo valpere/vmm-rada --comment "Resolved in PR #<pr-number>."
```

Report: issue closed, PR merged, what Copilot flagged (if anything).

---

## Step 11: Present next issue

Show the next unblocked issue from the queue (same query as Step 0, skip already-resolved).
**Do not start implementing it** — wait for the user's command.

---

## What NOT to do

- Do not work on more than one issue at a time.
- Do not bump version numbers or update changelogs unless asked.
- Do not open follow-up issues unless review reveals a real bug outside PR scope.
- Do not run `go mod tidy` unless the PR adds/removes dependencies.
- Do not invoke `/fix-review` — this skill handles the review loop itself.
