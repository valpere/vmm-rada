---
name: error-status-mapping
description: Gateway/openrouter errors must surface to the handler as council-layer error types, never by importing openrouter into api
type: project
---

The HTTP handler (`internal/api/handler.go`) maps council-layer error types to status codes — it must NOT import `internal/openrouter`. The canonical example is `handleRunError`, which keys off `*council.QuorumError` → 503. Per-member `Complete()` (LLMClient) errors are absorbed into `StageOneResult.Error` and aggregated by `checkQuorum`; a provider failure reaches the handler as a `*council.QuorumError`, never as a raw openrouter error.

**Why:** The dependency graph is `api → council`, `api → storage`. Adding `api → openrouter` makes the handler depend on both the council abstraction AND the concrete gateway behind it — a Layer 1 interface-misuse violation. It is also usually redundant: when the gateway fails for all members (e.g. circuit open), the run already fails quorum and the existing path already returns 503.

**How to apply:** When a plan proposes mapping an openrouter sentinel (ErrCircuitOpen, rate-limit, timeout) directly in the handler via `errors.Is(err, openrouter.ErrX)`, REJECT the import. Instead surface it through the council layer: either enrich `QuorumError` with a cause/reason, or define a `council.ErrProviderUnavailable` sentinel that openrouter wraps into at the interface boundary. The handler keys off the council-layer type. The enrichment hook is `checkQuorum` in `internal/council/council.go` — the **sole** `QuorumError` construction site — which already sees each member's failure via `StageOneResult.Error`. A `QuorumError.Reason` field set only when *all* failed members wrap the sentinel is the approved pattern (verified 2026-05-30 on the circuit-breaker plan).

Note the two handler error paths: `handleRunError` (JSON, handler.go:701-703) maps `*council.QuorumError → 503`; the SSE path (`handler.go:644`) reuses the existing SSE error event.

Note the two handler error paths: `handleRunError` is the JSON path; the stream path (handler.go ~458+) has already set `text/event-stream` and called `WriteHeader`, so stream errors must reuse the existing SSE error event and never call `WriteHeader` twice.
