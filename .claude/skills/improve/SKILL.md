---
name: improve
description: Critique any plan, architecture decision, feature design, or implementation approach. Researches best practices, identifies flaws and gaps, and gives a SHIP IT / IMPROVE IT / RETHINK IT / KILL IT verdict with concrete fixes.
user-invocable: true
argument-hint: "<topic/description> — plan, proposal, or implementation to critique"
metadata:
  version: "1.0"
  author: backend-claude
  last_updated: "2026-03-21"
---

# /improve

Technical improvement critic. Research-first — never critique from zero.

```
Our idea → Find best references → Extract patterns → Critique → Improve → Ship
```

## When to run proactively

Suggest `/improve` when:
- A proposal in `.proposals.md` is being elevated to active work
- A new interface or API contract is being designed
- A non-trivial architectural decision needs validation before implementation

Say: "This is new design — want me to run /improve on it before we build?"

## Procedure

### Step 1: Understand the subject

- What is it? (interface design, algorithm, API shape, concurrency pattern, config structure)
- What problem does it solve?
- What's the current proposal or draft?

### Step 2: Research

**Internal first:**
- Read `CLAUDE.md` for architecture constraints and conventions
- Read `.proposals.md` for related decisions already made
- Read `docs/architecture.md` and `docs/go-implementation.md` for system context
- Read `internal/council/interfaces.go` if the change touches the council pipeline
- Read affected source files directly

**External if needed:**

| Domain | Sources |
|--------|---------|
| Go concurrency | Go memory model docs, "Go Concurrency Patterns" (go.dev/blog) |
| HTTP / REST | RFC 7231, Go `net/http` docs, Fielding's REST dissertation |
| Interfaces / DI | "Go interfaces" (effective-go), Go blog on interface composition |
| Testing | Go testing docs, "testify" patterns, table-driven test conventions |
| Security | OWASP Top 10, Go security docs |
| SSE / streaming | MDN EventSource, Go `net/http` Flusher docs |

### Step 3: Structured critique

#### 3A. Architecture alignment
- Does this respect the layer boundaries: handler → Runner interface → Rada → LLMClient?
- Does it keep storage behind the Storer interface?
- Does it introduce package cycles?
- Does it follow Dependency Inversion (consumers define interfaces, not implementors)?
- **Frontend (when target is a file under `frontend/`):** the following are architectural invariants that must be preserved even under a RETHINK IT verdict — `api.js` as the sole SSE adapter boundary (components never consume raw SSE), `App.jsx` as the single state owner, co-located CSS modules, and the `react-markdown` rendering contract.

#### 3B. Flaws & risks
- What can go wrong?
- What's the worst-case: data corruption, silent failure, goroutine leak, blocked request?
- What assumptions could be wrong?
- Race conditions? Context misuse? Nil pointer dereference path?

#### 3C. Best-practice gap
- How does this compare to idiomatic Go?
- What do production Go systems do that ours is missing?
- What are we overcomplicating?

#### 3D. Simplicity check (YAGNI / KISS)
- Can this be simpler?
- What's the minimum viable version?
- What can be cut without losing core value?

#### 3E. Testability
- Can this be tested without a real OpenRouter call?
- Can it be tested without real disk I/O?
- Are the interfaces narrow enough to mock easily?

#### 3F. Security
- Any user input reaching file paths, shell, or external calls unvalidated?
- Any new env var or config that could leak secrets?

### Step 4: Improvement proposals

For each issue:

```
ISSUE: [what's wrong or missing]
REFERENCE: [who does it better and how]
FIX: [specific change to make]
IMPACT: [what improves if we do this]
EFFORT: Low / Medium / High
```

### Step 5: Verdict & score

| Dimension | Score (1–10) | Notes |
|-----------|-------------|-------|
| Architecture alignment | | |
| Correctness | | |
| Simplicity | | |
| Best-practice match | | |
| Testability | | |
| Security | | |
| **Overall** | | |

- **SHIP IT** (8+) — good enough, minor tweaks only
- **IMPROVE IT** (5–7) — solid foundation, needs specific fixes before building
- **RETHINK IT** (3–4) — core approach has issues, consider alternatives
- **KILL IT** (<3) — doesn't serve the goals, redirect energy elsewhere

### Step 6: Apply or propose

If SHIP IT or IMPROVE IT: apply fixes directly, update relevant files.

If RETHINK IT: present 2–3 alternatives with pros/cons and the references that inspired each.

If KILL IT: explain clearly why, suggest where energy should go instead.

## After a significant decision

If a significant architectural decision was made or validated, update `.proposals.md`:
add the decision and reasoning concisely.
