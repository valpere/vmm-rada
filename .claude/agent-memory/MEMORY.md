# Agent Memory Index

Shared project-level memories for vmm-rada agents.
Each entry below is a link to a memory file with a one-line description.

- [error-status-mapping.md](error-status-mapping.md) — gateway errors surface as council-layer error types; handler must never import openrouter
- [usage-cost-aggregation.md](usage-cost-aggregation.md) — per-call token/cost aggregated via eval-side LLMClient decorator, never through council.Metadata or stage3_complete
- [cors-allowed-methods.md](cors-allowed-methods.md) — CORS Access-Control-Allow-Methods must include every verb used by browser fetch (DELETE, PATCH); missing verbs cause preflight 405
- [storage-title-handling.md](storage-title-handling.md) — SaveTitle already on Storer; maxTitleRunes=50 truncates (not rejects); no RenameConversation needed

<!-- Add pointers here as agents write memories. Keep under 200 lines. -->
