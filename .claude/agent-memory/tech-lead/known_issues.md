---
name: known_issues
description: Prioritized improvements and fixes identified in initial codebase analysis (2026-03-14); updated 2026-03-22 to mark resolved items
type: project
---

Initial analysis completed 2026-03-14. Updated 2026-03-22 to mark items resolved by PRs #25–#37.

**Why:** Recorded so future sessions can pick up implementation work without re-analyzing.
**How to apply:** Work down this list in severity order. Do not add new abstractions without checking this list first for related issues.

## Critical

- **No API key validation at startup** — config/config.go: if AI_PROVIDER_API_KEY is empty the server starts silently and every request fails with a cryptic OpenRouter 401. Add a validation step in main.go before starting the HTTP server.
- **Stage3SynthesizeFinal never returns an error** — council/council.go: signature returns StageThreeResult (no error), swallows the error into a string. Callers (RunFull, handler) cannot distinguish a real error from a valid "error:" response. Change the signature to return (StageThreeResult, error).
- **No graceful shutdown** — cmd/server/main.go: `http.ListenAndServe` is called with no shutdown handling. A SIGTERM/SIGINT kills in-flight 3-stage requests mid-execution (including mid-SSE stream), leaving the conversation in a partially-written state. Add `http.Server` with `Shutdown` on signal.

## High

- ~~**Concrete type coupling breaks testability**~~ — ✅ Resolved: PR #27 (interfaces defined at consumer boundaries), PR #37 (handler tests using mock interfaces via `council.Runner`, `storage.Storer`).
- ~~**No HTTP client timeout**~~ — ✅ Resolved: PR #25 (`http.Client.Timeout` set to 150s in openrouter.Client).
- ~~**Title generation goroutine leaks on context cancellation**~~ — ✅ Resolved: PR #25 (title goroutine now uses `context.WithTimeout(context.Background(), 30s)`; PR #31 replaced `titleCh` with `awaitTitle func() string` closure scoped inside `if isFirst`).
- ~~**Handler.sendMessage returns 200 for createConversation**~~ — ✅ Resolved: PR #31 (`createConversation` now returns `http.StatusCreated` 201).
- **Config.Load() has no validation and no error return** — config/config.go: returns `*Config` without reporting missing required fields. Callers cannot distinguish misconfiguration from empty string. Add a `Validate() error` method or change Load to `Load() (*Config, error)`.

## Medium

- **CalculateAggregateRankings is exported without a clear consumer reason** — council/council.go: added to Runner interface in PR #36 (Kendall's W), but still called from handler. Should be folded into RunFull so the handler only calls RunFull and receives aggregate rankings + consensus W in the Result. See issue #9.
- **Duplicate request decoding logic** — api/handler.go: `sendMessage` and `sendMessageStream` are nearly identical in their first ~25 lines (decode body, get conv, check nil, isFirst). DRY violation; extract a helper.
- **`list` scans all files on every request** — storage/storage.go: List() reads and unmarshals every JSON file in the data dir. With hundreds of conversations this becomes slow and memory-intensive. Consider an in-memory index or a separate metadata file updated on Create/UpdateTitle.
- ~~**Hardcoded title-generation model**~~ — ✅ Resolved: PR #27 (`TitleModel` field added to Config, defaults to `google/gemini-2.5-flash`, configurable via `TITLE_MODEL` env var).
- **`log.Printf` is the only observability** — entire codebase: no structured logging, no request IDs, no timing, no way to correlate logs to a specific conversation or request. Adopt `log/slog` (stdlib since Go 1.21) with at minimum `conversation_id` and `model` fields. See issue #13.
- ~~**`json.NewEncoder(w).Encode(v)` silently drops write errors**~~ — ✅ Resolved: PR #33 (writeJSON now logs encode errors via `slog.Warn` with status context).
- ~~**Stage 2 peer-ranking prompt is embedded as a raw string literal**~~ — ✅ Resolved: PR #27 (all prompts extracted to named constants in `internal/council/prompts.go`).
- **`sendMessageStream` does not send a `stage3_start` event before the call** — api/handler.go line 249: SSE docs say `stage3_start` should be emitted; the code emits it correctly but the `stage2_start` and `stage3_start` events are missing for the non-streaming `sendMessage` path (irrelevant there, but the asymmetry should be documented).

## Low

- ~~**No `make lint` with a real linter**~~ — ✅ Resolved: PR #34 (`make lint` now runs `go vet ./...` + `go run honnef.co/go/tools/cmd/staticcheck ./...` via pinned `tools.go` dependency).
- ~~**No tests**~~ — ✅ Partially resolved: PR #26 (unit tests for `parseRankingFromText`, `CalculateAggregateRankings`), PR #37 (integration tests for storage with real filesystem + race detection), PR #37 (handler tests using mock interfaces). Rada stage integration tests still absent.
- **`validID` regex compiled at package level** — storage/storage.go: this is actually fine (package-level compiled regex is idiomatic Go), but worth noting for consistency with the rest of the analysis.
- **`Conversation.CreatedAt` is a string (RFC3339), not `time.Time`** — storage/storage.go: string comparison `metas[i].CreatedAt > metas[j].CreatedAt` works for RFC3339 lexicographically, but using `time.Time` would be more correct and enable duration calculations. Low-priority refactor.
- ~~**CORS allowed origins are hardcoded**~~ — ✅ Resolved: PR #31 (`CORSOrigins []string` added to Config, loaded from `CORS_ORIGINS` env var, defaults to `["http://localhost:5173", "http://localhost:3000"]`).
- ~~**`sendMessageStream` `titleCh` channel is always created even when `!isFirst`**~~ — ✅ Resolved: PR #31 (replaced with `awaitTitle func() string` closure; channel now scoped inside `if isFirst` block entirely).
