---
name: usage-cost-aggregation
description: Per-call LLM usage/cost must be aggregated via an eval-side LLMClient decorator, not smuggled through council.Metadata
type: reference
---

Token/cost telemetry (and any other per-`Complete()`-call metric) is aggregated by
wrapping `council.LLMClient` in a metering decorator passed into `NewCouncil`, NOT by
adding totals to `council.Metadata` or any SSE event payload.

**Why:**
- `council.Metadata` is a persisted + wire type (storage + frontend). Adding eval-only
  fields (TotalTokens, TotalCostUSD) pollutes the serving path with bench concerns.
- The `onEvent` callback only sees stage-completion events, never individual LLM calls,
  so an event consumer cannot sum per-call usage. `stage3_complete` emits a bare
  `StageThreeResult` with NO `Metadata` field — only `stage2_complete`
  (`Stage2CompleteData`) carries `Metadata`. Multi-round strategies (Debate/Delphi/MoA)
  make many more `Complete()` calls than events expose.
- A decorator keeps 100% of cost logic in `internal/eval`, touches zero council strategy
  code, and is the lowest-coupling option.

**How to apply:**
- The ONLY allowed change to `council/types.go` for cost is adding `Usage` to
  `CompletionResponse` (mirrors OpenRouter's `usage` body field; `CostUSD` omitempty for
  Ollama). `decodeBody()` already unmarshals the whole body — purely additive.
- Stage 1 fans out concurrently → any usage accumulator MUST be mutex-guarded or use
  atomics. Always require a concurrent-accumulation test under `-race`.
- Reject any plan that threads per-call telemetry through `Metadata` or invents totals on
  `stage3_complete`. See [[error-status-mapping]] for the related rule that serving-path
  types must not absorb non-serving concerns.
