# Plans

Implementation plans for the VMM Rada backend.

Plans are created by the `/plan` skill. Each plan can be promoted to a GitHub issue.

---

## Naming Convention

```
{N}-{slug}.md
```

`N` is the priority digit (0 = highest, 3 = lowest). Matches the `p{N}` priority label.

| Prefix | Priority | GitHub label | Meaning |
|--------|----------|-------------|---------|
| `0-` | p0 | `p0: critical` | Blocker — data loss, security, broken build. Do this now. |
| `1-` | p1 | `p1: high` | Top of the queue — ships this sprint. |
| `2-` | p2 | `p2: medium` | Should do — not blocking anything. |
| `3-` | p3 | `p3: low` | Nice to have — backlog. |

Examples: `1-handler-tests.md`, `2-structured-logging-slog.md`, `3-staticcheck-lint.md`

---

## Frontmatter Schema

Every plan file starts with this YAML frontmatter:

```yaml
---
title: "Short human-readable title"
type: bug           # bug | feature | task | test
priority: p1        # p0 | p1 | p2 | p3
status: draft       # draft | ready | in-progress | done | blocked
debt: balanced      # quick-fix | balanced | proper-refactor  (⚡/⚖️/🏗️)
effort: s           # xs | s | m | l | xl
component:          # api | council | storage | openrouter | config | cmd | test | dx | frontend
  - api
  - test
labels:             # used verbatim as GitHub issue labels
  - test
  - p1: high
blocked_by: null    # plan slug or GitHub issue number, e.g. "gh#18"
github_issue: null  # filled in after gh issue create, e.g. 19
created: YYYY-MM-DD
updated: YYYY-MM-DD
---
```

### Type

| Value | GitHub label | Use |
|-------|-------------|-----|
| `bug` | `bug` | Something is broken or incorrect |
| `feature` | `feature` | New user-visible capability |
| `task` | `task` | Maintenance, refactor, infrastructure, DX |
| `test` | `test` | Test coverage — no behavior change |

### Priority

| Value | GitHub label | Meaning |
|-------|-------------|---------|
| `p0` | `p0: critical` | Drop everything |
| `p1` | `p1: high` | Next sprint goal |
| `p2` | `p2: medium` | This quarter |
| `p3` | `p3: low` | Backlog / nice to have |

### Status lifecycle

```
draft → ready → in-progress → done
                    ↓
                 blocked
```

### Debt level

| Value | Emoji | Meaning |
|-------|-------|---------|
| `quick-fix` | ⚡ | Targeted, minimal, no refactor |
| `balanced` | ⚖️ | Sensible trade-offs, some cleanup OK |
| `proper-refactor` | 🏗️ | Full refactor, break things cleanly |

### Effort

| Value | Meaning |
|-------|---------|
| `xs` | < 30 min, trivial |
| `s` | 30 min – 2 hours |
| `m` | Half a day |
| `l` | Full day |
| `xl` | Multiple days, needs breakdown |

---

## Plan File Structure

```markdown
---
(frontmatter)
---

## Summary
1–3 sentences. Problem statement + why it matters.
This section becomes the GitHub issue description.

## Acceptance Criteria
- [ ] Specific, testable outcome
- [ ] Another outcome

## Implementation

### Files to change
- `internal/...` — what changes and why

### Files to read (context only)
- `internal/...` — why relevant

### Approach
Step-by-step notes. Call out decisions with more than one reasonable answer.

### Risks / Unknowns
Use WEP vocabulary: "Almost certainly...", "Likely...", "Unlikely..."

## Not in Scope
Explicit list of what this plan intentionally excludes.

## Commit Message
\`\`\`
fix(scope): description ⚡
\`\`\`

## After Implementing
- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] `/ship` to create PR and merge
- [ ] Update plan `status: done`, fill `github_issue` if created
```

---

## GitHub Issue Creation

After the plan is confirmed, `/plan` offers to create a GitHub issue:

```bash
gh issue create \
  --repo valpere/vmm-rada \
  --title "<type>(<component>): <title>" \
  --label "<comma-separated labels>" \
  --body "$(cat <<'EOF'
## Summary
...

## Acceptance Criteria
- [ ] ...

**Plan:** \`.claude/plans/{N}-{slug}.md\`
EOF
)"
```

The issue body uses only `## Summary` and `## Acceptance Criteria` from the plan —
implementation details stay internal.

After creation, fill `github_issue:` in the frontmatter with the issue number.
