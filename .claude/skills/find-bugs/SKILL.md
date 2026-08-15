---
name: find-bugs
description: Find bugs, security vulnerabilities, and code quality issues in local branch changes. Report-only — no code changes. Use when asked to review changes, find bugs, or security-audit code on the current branch.
user-invocable: true
argument-hint: "[topic] — optional focus area (security, correctness, performance, etc.)"
metadata:
  version: "1.0"
  author: backend-claude
  last_updated: "2026-03-21"
---

# /find-bugs

Review changes on this branch for bugs, security vulnerabilities, and code quality issues.
Report only — do not make changes.

## Phase 1: Complete Input Gathering

1. Get the full diff: `git diff $(git merge-base main HEAD)...HEAD`
2. If output is truncated, read each changed file individually until every changed line is seen
3. List all files modified in this branch before proceeding

## Phase 2: Attack Surface Mapping

For each changed file, identify and list:

- All user-controlled inputs (URL path values, request bodies, query params)
- All calls to `h.store.*` or `h.council.*` that could fail silently
- All goroutines started — context lifecycle, channel management, leak potential
- All file system operations in `storage.go` — path construction, atomic rename, mutex scope
- All HTTP response writes — headers set before body? WriteHeader called once?
- All external HTTP calls — timeout set? response body closed? error checked?

## Phase 3: Security Checklist (check EVERY item for EVERY changed file)

- [ ] **Path traversal**: UUIDs validated before constructing file paths? (`storage.go`)
- [ ] **Injection**: Any user input interpolated into shell commands or SQL?
- [ ] **Information disclosure**: Error messages returning internal paths or stack traces?
- [ ] **Request body limits**: `http.MaxBytesReader` applied before decoding JSON?
- [ ] **Goroutine leaks**: All goroutines bounded by context or timeout? Channels buffered?
- [ ] **Nil dereference**: All pointer returns checked before use?
- [ ] **CORS bypass**: Origin header validated strictly (exact match, not prefix/contains)?
- [ ] **Resource exhaustion**: No unbounded loops or allocations on user-supplied size hints?
- [ ] **Hardcoded secrets**: No API keys, tokens, or passwords in changed code?
- [ ] **Race conditions**: Shared state accessed without mutex? Maps written concurrently?
- [ ] **SSE correctness**: `w.WriteHeader` called exactly once? Headers set before body starts?
- [ ] **Context propagation**: Request contexts passed through to all blocking calls?

## Phase 4: Verification

For each potential issue:

- Check if it's already guarded elsewhere in the changed code
- Read surrounding context (at least 10 lines each direction) to confirm the issue is real
- Check Go semantics: nil interface vs nil pointer, zero values, deferred cleanup order

## Phase 5: Pre-Conclusion Audit

Before finalizing:

1. List every file reviewed and confirm it was read completely
2. List every checklist item: found issues or confirmed clean
3. List any areas you could NOT fully verify and why
4. Only then provide final findings

## Output Format

**Priority:** security vulnerabilities > correctness bugs > performance > code quality

**Skip:** style, formatting, naming preferences

For each issue:

```
**File:Line** — brief description
Severity: Critical / High / Medium / Low
Problem: what's wrong
Evidence: why this is real (no existing guard, Go semantics confirm it)
Fix: concrete suggestion
```

If nothing significant: say so — don't invent issues.
