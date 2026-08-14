---
name: agent-scope-boundary-pairs
description: Retiring one agent of a mutually-cross-referencing pair requires editing the survivor's description field, not just skills; grep must not exclude the sibling's name
metadata:
  type: project
---

`.claude/agents/security-reviewer.md` (frontend/React-only) was ruled RETIRE on
2026-08-14 rather than rescoped, because its only surviving concern (CORS /
security headers) is already inside `go-security-reviewer`'s declared scope
("Go source code, configuration files"). Rescoping would have produced two
agents with overlapping scope and ambiguous dispatch.

`security-reviewer` and `go-security-reviewer` were a **mutually
cross-referencing pair**: each one's `description:` frontmatter told the
dispatcher "for the other kind of code, use <sibling> instead". Deleting one
leaves the survivor's `description:` — the field the dispatcher actually
matches on — pointing at a non-existent agent, plus a dead relative markdown
link in its body scope-boundary paragraph.

**Why:** a plan-stage grep of `.claude/skills/*/SKILL.md` reported "zero
references, safe to delete". It was wrong twice over: skills were never where
the reference lived, and the grep filter `grep -v go-security-reviewer`
suppressed the very lines that contained it. Use `grep -P '(?<!go-)security-reviewer'`
style negative lookbehind, or grep the bare name and read every hit.

**How to apply:** before approving any agent/skill deletion, grep
`.claude/agents/*.md` **and** `.claude/agent-memory/*/` — not just skills — for
the bare name, without filtering out longer names that contain it. Treat the
survivor's `description:` frontmatter as a required edit in the same PR.
Agent-memory files scoped to the deleted agent's domain
(e.g. `go-security-reviewer/project_frontend_security_posture.md`) are separate
cleanup: preserve any live backend facts, delete only the dead scope guidance.
