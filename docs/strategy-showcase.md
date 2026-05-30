# Strategy Showcase — Test Prompts

Three test prompts per deliberation strategy, chosen to highlight each strategy's
deliberation strength. Machine-readable version: `eval/benchmarks/strategy-showcase.yaml`.

---

## PeerReview

*Best when cross-critique between members improves accuracy and depth.*

| # | Prompt |
|---|--------|
| peer-001 | Explain the CAP theorem and when you would choose consistency over availability in a distributed system. |
| peer-002 | What are the key trade-offs between microservices and a monolith? When is each the right architectural choice? |
| peer-003 | Describe the SOLID principles with a concrete Go example for each one. |

---

## RoleBased

*Best when multiple specialist angles are needed on the same problem.*

| # | Prompt |
|---|--------|
| role-001 | We are building a healthcare data platform that stores patient records. What are the critical considerations from technical, legal, security, and UX perspectives? |
| role-002 | Should a 5-person startup use a managed cloud database or self-host Postgres? Analyse from engineering, cost, operations, and scaling angles. |
| role-003 | Design a feature flag system for a SaaS product. Cover the API design, data model, rollout mechanics (percentage, user segment), and monitoring/observability. |

---

## Majority

*Best for factual/correctness questions with a clear right answer.*

| # | Prompt |
|---|--------|
| maj-001 | What is the time complexity of Dijkstra's algorithm when implemented with a binary min-heap, and why? |
| maj-002 | Is Redis single-threaded for command execution? Explain the nuance, including how Redis 6+ changed the picture. |
| maj-003 | What HTTP status code should a REST API return when a resource does not exist versus when the client lacks permission to see it? Explain the security implications of each choice. |

---

## GenerateRankRefine

*Best when quality criteria are clear and the top draft benefits from refinement.*

| # | Prompt |
|---|--------|
| grr-001 | Write a Go function that finds all prime numbers up to N using the Sieve of Eratosthenes. Include proper documentation and edge-case handling for N ≤ 1. |
| grr-002 | Write a concise explanation of how a Bloom filter works, suitable for a technical blog post aimed at junior engineers. Use an analogy and explain the false-positive trade-off. |
| grr-003 | Write a system design document outline for a URL shortener that handles 10 million stored URLs and 1000 redirects per second. Cover requirements, data model, API, storage choice, and scalability. |

---

## MultiAgentDebate

*Best for contested trade-offs where steel-manning opposing views helps.*

| # | Prompt |
|---|--------|
| debate-001 | Is GraphQL always better than REST for new API projects? Make the strongest case for each position, then provide a resolved recommendation with conditions. |
| debate-002 | Should LLM applications prefer RAG or fine-tuning for incorporating domain-specific knowledge? Debate both approaches and resolve with a decision framework. |
| debate-003 | Is Kubernetes overkill for a team of 5 engineers? Present and rigorously critique both positions, then recommend a threshold for when Kubernetes becomes the right choice. |

---

## MixtureOfAgents

*Best for code generation and layered multi-aspect analysis.*

| # | Prompt |
|---|--------|
| moa-001 | Write a production-ready Go HTTP middleware that handles rate limiting (token bucket, per-IP), structured request logging (slog), and panic recovery with stack trace capture. The middleware must be composable and testable. |
| moa-002 | Design and implement an in-memory task queue in Go using only the standard library. Cover: public API, fixed-size worker pool, task retry with exponential backoff, and graceful shutdown via context cancellation. |
| moa-003 | Analyse the trade-offs between event sourcing and traditional CRUD for a financial ledger system. Cover consistency guarantees, auditability, query complexity, storage growth, and operational burden. Conclude with a recommendation. |

---

## Delphi

*Best for estimation, forecasting, and collective judgment converging to consensus.*

| # | Prompt |
|---|--------|
| delphi-001 | Estimate the engineering effort (person-weeks) to migrate a 100 000-line Python monolith to Go microservices. Provide a range (optimistic / realistic / pessimistic) with your key assumptions explicitly stated. |
| delphi-002 | Rate the production readiness of SQLite as the primary database for a B2B SaaS with 10 000 active users and 50 writes per second on a scale of 0–10. Justify your score with specific criteria. |
| delphi-003 | How many years from now (mid-2026) until LLMs can autonomously complete a two-week greenfield software project at senior-engineer quality, without human intervention? Provide a range and your reasoning. |
