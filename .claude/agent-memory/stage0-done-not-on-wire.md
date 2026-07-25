---
name: stage0-done-not-on-wire
description: stage0_done is an internal EventFunc-only event, never emitted on the SSE wire — recurring source of doc drift
type: project
---

`stage0_done` fires on the `council.EventFunc` callback but is **never written to the SSE
wire**. `internal/api/handler.go`'s `onEvent` handles `case "stage0_done"` by setting
`stage0Event` for control flow only, with the comment "Do not emit to client — stage 1
will follow immediately." Stage 1 begins directly after Stage 0 resolves.

**Why:** this internal/wire distinction has produced identical drift in at least three
docs (`docs/api.md` event sequence + a dedicated `### stage0_done` section,
`docs/user-guide.md` SSE transcript, `docs/strategies.md` event table), because the event
genuinely exists in the pipeline — it just never reaches a client.
`docs/pipeline.md` is the one place the term is used *correctly*, since it describes
callback-level flow, not the wire.

**How to apply:** when reviewing any doc or frontend change that lists SSE events, check
whether `stage0_done` is being presented as a wire event — if so, REJECT that line. When
a plan proposes fixing this drift in one file, require the sweep across all wire-level
docs; a single-file fix leaves the trap in place. Same rule for any future event handled
in `onEvent` without a `sendSSE` call.

Related: [[error-status-mapping]]
