# Agent Memory Index

Shared project-level memories for vmm-rada agents.
Each entry below is a link to a memory file with a one-line description.

- [error-status-mapping.md](error-status-mapping.md) — gateway errors surface as council-layer error types; handler must never import openrouter
- [usage-cost-aggregation.md](usage-cost-aggregation.md) — per-call token/cost aggregated via eval-side LLMClient decorator, never through council.Metadata or stage3_complete
- [cors-allowed-methods.md](cors-allowed-methods.md) — CORS Access-Control-Allow-Methods must include every verb used by browser fetch (DELETE, PATCH); missing verbs cause preflight 405
- [storage-title-handling.md](storage-title-handling.md) — SaveTitle already on Storer; maxTitleRunes=50 truncates (not rejects); no RenameConversation needed

- [stage0-done-not-on-wire.md](stage0-done-not-on-wire.md) — stage0_done is EventFunc-internal only, never on the SSE wire; recurring doc-drift trap across api.md/user-guide.md/strategies.md

- [rolebased-strategy-orphan.md](rolebased-strategy-orphan.md) — HISTORICAL: RoleBased resolved (registered standalone, extraction-into-MoA overruled); see project_architecture.md for current shape
- [code-generator/MEMORY.md](code-generator/MEMORY.md) — bootstrapped W32: no memories yet
- [code-simplifier/MEMORY.md](code-simplifier/MEMORY.md) — bootstrapped W32: no memories yet

<!-- Add pointers here as agents write memories. Keep under 200 lines. -->
