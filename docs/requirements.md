# Requirements & Use Cases

This is the single source of truth for **what VMM Rada is for, who it's for, and what it
must do** — as distinct from [`architecture-v2.md`](./architecture-v2.md) (how it's
built), [`api.md`](./api.md) / [`user-guide.md`](./user-guide.md) (how to call it), or
[`council-research-synthesis.md`](./council-research-synthesis.md) (why, and the research
behind each design choice). It also tracks where the current implementation diverges from
what's documented elsewhere — see [Gap Analysis](#gap-analysis).

---

## 1. Problem & thesis

A single LLM call has blind spots specific to that model's training. VMM Rada assembles a
**council** of independently-answering models, has them anonymously peer-review each
other, and has a designated **Chairman** model synthesise a final answer informed by both
the raw answers and the peer critique.

**When a council helps:** complex reasoning, analysis, creative synthesis, and high-stakes
decisions — tasks where diversity of perspective plausibly changes the outcome. Estimated
quality gain: **+5–15% for complex tasks** (per the aggregated research in
[`council-research-synthesis.md`](./council-research-synthesis.md), sourced from the Grok
conversation transcript — an estimate, not a measurement this codebase has produced; see
the [Known Gaps](#gap-analysis) note on the eval harness).

**When it's overkill — the 95% rule of thumb:** *"If a human expert would agree 95%+ of
the time with a single strong model, a council adds overhead without benefit."* This rules
out simple factual retrieval, formal transformations (parsing, classification), and any
task where every candidate model shares the same training blind spot. Operators choosing
a `council_type` for a given workload should apply this test first — it's cheaper than any
of the seven strategies below.

---

## 2. Personas

VMM Rada has exactly two kinds of user; the same person may be both, but the system does
not blur the two roles into one knob.

### Operator

Deploys and configures the server. Chooses which strategies exist at all (each strategy
is opt-in — see [`strategies.md`](./strategies.md#per-strategy-configuration)), which
models back each one, quorum thresholds, and cost tier via environment variables
(`.env.example` is the full surface). Decides what `DEFAULT_RADA_TYPE` API clients get
when they don't specify one.

### API Client

Sends `{ "content": "...", "council_type": "default" }` to `POST
/api/conversations/{id}/message` or `/message/stream` and reads back a 3-stage result (or
consumes it progressively over SSE). If Stage 0 clarification is enabled (the default),
the stream may instead pause after a `stage0_round_complete` event — the client must then
POST again with `{ "answers": [...] }` (never re-sending `content` or `council_type`) to
continue, possibly more than once, before Stage 1 begins. **Never selects strategy
internals** — no per-stage knobs, no model IDs, no quorum overrides in the request body.
`council_type` is a server-registered *name*, not a strategy enum value; the same name
could be re-registered by the operator with different models without any client-visible
change. See
[`strategies.md`](./strategies.md#rest-is-strategy-agnostic): "There is no per-strategy
endpoint and there will not be one."

---

## 3. Functional requirements

Derived from what is actually built and wired (not aspirational — see
[Gap Analysis](#gap-analysis) for anything listed elsewhere that isn't).

| Requirement | Current implementation |
|---|---|
| Create, list, fetch, rename, delete conversations | `internal/api/handler.go` conversation CRUD routes; JSON-file storage in `internal/storage/` |
| Send a message and get a full deliberation result, blocking | `POST /message` |
| Send a message and get progressive results | `POST /message/stream` (SSE) |
| Optional pre-generation clarification round | Stage 0, `CLARIFICATION_MAX_ROUNDS` (default 2 — **on by default**), strategy-independent, runs before dispatch |
| Multiple deliberation strategies, operator-selectable per registration | 7 `Strategy` constants, all reachable — `PeerReview` is the always-on default, the other six (including `RoleBased`, registered #302) are opt-in via their own env-var family; see [`strategies.md`](./strategies.md) for the full per-strategy cost/quorum/config table |
| Graceful degradation on partial model failure | Quorum checks per strategy; pipeline continues with survivors above the quorum floor |
| Liveness/readiness signals for orchestration | `GET /health/live`, `GET /health/ready` — see [Gap Analysis](#gap-analysis) for `/health/ready`'s current fidelity |
| Crash-safe persistence | Atomic tmp-file-then-rename writes, one JSON file per conversation |

---

## 4. Non-functional constraints

- **Standard-library-first backend.** The only non-stdlib dependency of the server binary
  (`cmd/server`) is `github.com/joho/godotenv`. The eval binary (`cmd/eval`,
  `internal/eval/`) additionally uses `gopkg.in/yaml.v3` for benchmark-file parsing — not
  part of the running server. No ORM, no web framework, no DB driver.
- **No database.** One JSON file per conversation under `DATA_DIR`. This is an explicit v1
  choice, not an oversight — see [`council-research-synthesis.md`](./council-research-synthesis.md#12-implementation-design-decisions):
  the `Storer` interface is deliberately pluggable so a future backend swap doesn't touch
  handlers, but the JSON backend itself is scoped as v1-only (`List()` is O(n) on disk).
- **Stateless-per-query deliberation.** Prior conversation turns are not fed back into the
  council pipeline as context beyond what Stage 0's augmented-query mechanism captures for
  the *current* turn's clarification round. There is no cross-turn memory in the council
  models themselves.
- **Single-tenant, no authentication.** There is no user/session/auth concept anywhere in
  the request path.
- **Frontend-agnostic.** This repo is the backend API only; any HTTP+SSE client can
  consume it. The reference frontend is a separate repo,
  [`vmm-rada-web-ui`](https://github.com/valpere/vmm-rada-web-ui) (frontend extracted
  2026-07-19).

---

## 5. Explicit non-goals

Pulled from [`council-research-synthesis.md §12`](./council-research-synthesis.md#12-implementation-design-decisions)
and [`.proposals.md`](../.proposals.md) — decisions made, not gaps to fill:

- **No client-selectable strategy internals.** `council_type` is a name, never a strategy
  enum or a bag of per-stage parameters, on either the request or response contract.
- **No trust weighting, no embedding-based consensus.** Kendall's W (exact-agreement
  statistics) is the only consensus signal computed; per-model trust scores and
  embedding-similarity clustering are researched (§2.1, §5 of the synthesis doc) but not
  implemented and not planned for v1.
- **No API versioning prefix yet.** `.proposals.md §D` defers `/api/v1/` and a structured
  `{"error":{"code":...}}` shape until frontend coordination — do not implement
  unilaterally.
- **Eval harness is deliberately minimal and NOT CI-wired.** `internal/eval/` is a
  manually-invoked, cost-bounded (`EVAL_MAX_COST_USD`) regression detector, not a
  golden-output-assertion framework and not a gate on merges.
- **LCCP is Core-conformance only.** The formal LCCP state machine
  ([`council-research-synthesis.md §3`](./council-research-synthesis.md#3-lccp-protocol-state-machine))
  is aspirational research, not a build target beyond what's already implemented. Per
  [§12](./council-research-synthesis.md#12-implementation-design-decisions): "Core
  conformance level... single round, no REFINE loop." Robust and Auditable conformance
  (arbiter fallback chains, full trace reconstruction, policy versioning) are explicitly
  post-v1.
- **No graceful non-synthesis outcome.** A first-class "the council could not agree"
  result requires the REFINE loop and LCCP fallback chain (both out of scope above). Today
  low-consensus (`consensus_w < 0.40`) only steers Chairman *prose* toward presenting
  multiple perspectives — it can't refuse to pick a "final" answer.
- **No intra-stage token streaming.** SSE emits one event per completed stage, not
  per-token. A valid future enhancement, not a current requirement.

---

## 6. Gap Analysis

Requirements vs. what's actually wired, as of this pass (2026-07-20). Each of these is a
**known gap**, not a silently-fixed bug — the fix is deferred to a future `/backlog` item
so it goes through the normal Tech-Lead-gated flow rather than landing inside a docs pass.

| Gap | What's claimed/expected | What's actually true | Suggested next step |
|---|---|---|---|
| **CORS origins are dev-only and unconfigurable** | N/A — inherited from when `frontend/` lived in this repo | Hardcoded to `http://localhost:5173` / `http://localhost:3000` in `internal/api/handler.go`. Since the frontend extraction (2026-07-19) this backend cannot serve any non-localhost frontend deployment without a source change; there's no env var to add an origin. | File a `/backlog` item: make allowed origins configurable (e.g. `CORS_ALLOWED_ORIGINS`). |
| **`/health/ready` is a stub** | A readiness probe implies *some* dependency check | `internal/api/handler.go`'s `healthReady` unconditionally returns `200` — no check of storage writability, OpenRouter reachability, or anything else | Low priority until there's an actual dependency to check (e.g. a future non-JSON storage backend). File a `/backlog` item if/when that lands. |
| **Only `PeerReview`'s quorum formula is runtime-wired as a true default** | `strategies.md`'s quorum table lists a formula per strategy | `checkQuorum` applies one formula (`max(2, ⌈N/2⌉+1)`) whenever `QuorumMin == 0`, regardless of strategy. Each strategy's `run*` function resolves its *own* effective `need` inline before calling `checkQuorum` — so the per-strategy formulas in the table are correct as implemented, just not centralised in `checkQuorum` itself. This is accurately caveated in `strategies.md` already ("Only the `PeerReview` row below is implemented as a runtime default"). | No action — already correctly documented as a caveat, not a contradiction. Listed here for completeness. |
| **`backlog-eval.md` describes an eval system that wasn't built** | Reads as a design doc for the shipped eval harness | Describes a 0–10 meta-judge, cost-optimization loop, and `/improve-council` skill — none of which exist. What shipped (`internal/eval/`) is a leaner blinded pairwise-preference regression detector. | Fixed in this pass — `backlog-eval.md` now carries a SUPERSEDED header pointing at [`testing-strategy.md §7`](./testing-strategy.md#7-evaluation-harness). No further action needed. |
| **The +5–15% quality claim is a literature estimate, not a measurement** | Could be read as an empirical result of this codebase | Sourced from an aggregated external research conversation (`council-research-synthesis.md`), not from `internal/eval/` output against this project's own strategies | The eval harness exists precisely to eventually validate or refute this per-strategy. No action needed beyond awareness — don't cite the number as this project's own benchmark result. |

---

## 7. See also

- [`architecture-v2.md`](./architecture-v2.md) — package layout, layer boundaries, composition root
- [`pipeline.md`](./pipeline.md) — full code-anchored request walkthrough
- [`strategies.md`](./strategies.md) — per-strategy cost, quorum, and SSE contract reference
- [`api.md`](./api.md) — REST + SSE wire reference
- [`user-guide.md`](./user-guide.md) — setup and integration walkthrough
- [`council-research-synthesis.md`](./council-research-synthesis.md) — the "why," and every design decision's rationale
- [`testing-strategy.md`](./testing-strategy.md) — test layers and the eval harness
