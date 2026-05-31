---
name: storage-title-handling
description: SaveTitle already exists on Storer; title limit is maxTitleRunes=50 with truncation, not 200 with rejection
type: reference
---

Conversation title persistence is already implemented:
- `Store.SaveTitle(id, title)` (internal/storage/storage.go) — read → set Title →
  write under `s.mu.Lock()`, on the `Storer` interface.
- Handler enforces `maxTitleRunes = 50` (internal/api/handler.go) and the
  auto-title path TRUNCATES over-limit titles rather than rejecting them.

**Why:** Plans proposing a "rename" feature tend to invent a new
`RenameConversation` method (DRY violation — SaveTitle already does it) and assume
a 200-char limit with 400-on-overflow (contradicts the real 50-rune truncating
behavior). Both assumptions produce tests that contradict the code.

**How to apply:** For rename/title plans, point the generator at the existing
`SaveTitle` and reconcile any length-limit acceptance criteria against
`maxTitleRunes`. Verify the constant still reads 50 before citing it — grep
`maxTitleRunes` in handler.go.
