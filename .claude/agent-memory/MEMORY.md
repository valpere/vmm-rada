# Agent Memory Index

Shared project-level memories for vmm-rada agents.
Each entry below is a link to a memory file with a one-line description.

- [error-status-mapping.md](error-status-mapping.md) — gateway errors surface as council-layer error types; handler must never import openrouter
- [usage-cost-aggregation.md](usage-cost-aggregation.md) — per-call token/cost aggregated via eval-side LLMClient decorator, never through council.Metadata or stage3_complete

<!-- Add pointers here as agents write memories. Keep under 200 lines. -->
