# VMM Rada Research Synthesis

A comprehensive synthesis of research and design patterns for multi-LLM deliberation systems, aggregated from 9 LLM conversations covering strategies, protocols, Go implementation patterns, operational hazards, and production deployment.

---

## Table of Contents

1. [Core Concept](#1-core-concept)
2. [Deliberation Strategies](#2-deliberation-strategies)
3. [LCCP Protocol State Machine](#3-lccp-protocol-state-machine)
4. [Artifact Schemas](#4-artifact-schemas)
5. [Scoring, Consensus, and Finalization Policies](#5-scoring-consensus-and-finalization-policies)
6. [Failure Handling and Safety](#6-failure-handling-and-safety)
7. [Operational Hazards and Mitigations](#7-operational-hazards-and-mitigations)
8. [Go Implementation Patterns](#8-go-implementation-patterns)
9. [Production Deployment Patterns](#9-production-deployment-patterns)
10. [Configuration Profiles and Conformance Levels](#10-configuration-profiles-and-conformance-levels)
11. [REST API Design Considerations](#11-rest-api-design-considerations)
12. [Implementation Design Decisions](#12-implementation-design-decisions)

---

## 1. Core Concept

An **VMM Rada** (also: Multi-Agent Debate, Mixture of Agents) is a deliberation system where multiple LLMs independently answer a prompt, then through structured interaction converge on or synthesize a higher-quality final answer.

### Structure

```plaintext
Prompt P
  ↓ (parallel)
LLM1 → A1
LLM2 → A2          ← Stage 1: Independent Generation
LLMn → An
  ↓
[Interaction rounds: critique / rank / revise]  ← Stage 2: Deliberation
  ↓
Arbiter (LLM0) or Synthesizer
  ↓
Final Answer A*  ← Stage 3: Finalization
```

An optional **LLM0 (Arbiter/Chairman)** ranks, adjudicates, or synthesizes. The key constraint is guaranteed termination — the system must not cycle, must produce bounded output, and must reach a decision in a small fixed number of iterations.

### When to use a council

A council adds value for tasks where **diversity of perspective genuinely helps**: complex reasoning, analysis, creative synthesis, high-stakes decisions. It is generally overkill for:

- Simple factual retrieval (one model + RAG is cheaper and more accurate)
- Formal transformations (parsing, classification)
- Tasks where all models share the same training blind spot

**Rule of thumb:** If a human expert would agree 95%+ of the time with a single strong model, a council adds overhead without benefit.

---

## 2. Deliberation Strategies

### 2.1 Majority / Weighted Voting

All LLMs generate independently; select by exact match, keyword overlap, or embedding similarity. Optionally weight by model trust score, prior accuracy, or confidence.

- **Best for:** Factual QA, math, classification (objective tasks with a clear correct answer).
- **Variants:** Borda count (each model ranks all answers), Tournament/Elo (pairwise elimination), Cluster voting (group similar answers, vote by cluster size).
- **Limitation:** Selects, does not synthesize. Cannot produce a better-than-best answer.

### 2.2 Generate → Rank → Refine

1. All LLMs generate (parallel, high temperature for diversity).
2. Arbiter ranks responses against structured criteria.
3. Top-K (usually 2–3) candidates go to a final refinement or synthesis step.

- **Best for:** Creative writing, analysis, code generation.
- **Predictable iteration count** (typically 1–2 rounds). Good cost control.
- **Variant — Champion Strategy:** Instead of synthesis, select the best answer and polish only it. Avoids blandness averaging.

### 2.3 Multi-Agent Debate

1. All LLMs generate initial answers (round 0).
2. Each model sees all others' answers (anonymized) and produces critiques / revised answers.
3. Repeat for N rounds. Arbiter synthesizes or selects.

- **Best for:** Reasoning, ethics, strategy — tasks where critique reveals logical errors.
- **Risk:** Can cycle into polite agreement or endless argument. Needs clear stopping criteria.
- **Anonymization is critical:** Never reveal which model authored which answer; prevents positional and authority bias.

### 2.4 Mixture of Agents (MoA)

Hierarchical layered architecture:

- **Layer 1 (Proposers):** 2–4 models generate drafts.
- **Layer 2 (Aggregators):** 1–2 models improve drafts using all Layer 1 outputs as context.
- **Final Layer (Refiner/Judge):** 1 model synthesizes.

- **Best for:** Code generation (structured output), complex multi-aspect analysis.
- **Strong benchmark results** (Together AI research).
- **Downside:** Context grows with each layer; expensive on tokens.

### 2.5 Karpathy-Style Peer Review Rada

Three clean stages (referenced as "gold standard" by Grok):

1. **Stage 1 — Independent Generation:** Each model answers the query in isolation.
2. **Stage 2 — Anonymous Peer Review:** Each model receives all N answers (numbered A1, A2..., without authorship) and critiques / ranks them.
3. **Stage 3 — Chairman Synthesis:** LLM0 (Chairman) receives all responses + all reviews and writes the final answer.

This is the design implemented in this project's existing codebase.

### 2.6 Delphi Method

Multiple anonymous blind review rounds with averaged ratings. Well-suited for structured survey-like tasks; less common in LLM contexts.

### 2.7 Role-Based Rada

Assign specialized roles to different models:

- **Generator (Creator):** High temperature, broad exploration.
- **Critic:** Identifies flaws, logical gaps.
- **Verifier:** Checks factual correctness, uses tools/search.
- **Simplifier:** Distills to the most concise correct form.
- **Devil's Advocate:** Intentionally generates counter-examples.
- **Arbiter/Chairman:** Low temperature, synthesizes.

Roles can rotate across rounds to prevent authority bias.

### Strategy Selection Guide

| Task Type | Strategy | Max Rounds | Notes |
|---|---|---|---|
| Factual QA / math | Majority Vote | 1 | One correct answer; voting effective |
| Creative writing | Generate→Rank→Refine | 2 | Diversity matters; refine improves |
| Complex reasoning / analysis | Debate | 3 | Critique reveals logical errors |
| Code generation | MoA | 2 | Layered aggregation works well for structured output |
| High-stakes / auditable | Peer Review (Karpathy) + Verifier | 2 | Anonymization + external verification |

---

## 3. LCCP Protocol State Machine

The **VMM Rada Consensus Protocol (LCCP)** is a formal state machine specification developed through extensive analysis. It provides a rigorous foundation for implementation.

### 3.1 Roles and Objects

**Roles:**

- `Participant Model (LLMi)` — generates candidate answers, optionally critiques/revises
- `Controller` — orchestrator: enforces state transitions, budgets, validation, stopping
- `Arbiter (LLM0)` — optional: ranks, adjudicates, synthesizes
- `Synthesizer` — merges top candidates (may be same model as Arbiter)

**Core objects:**

- `A_i^t` — answer from participant i at round t
- `C_i→j^t` — critique by participant i of participant j's answer
- `S_i^t` — quality score (controller-computed)
- `K^t` — consensus metric (average pairwise similarity)
- `D^t` — delta metric (change between rounds)

### 3.2 System Parameters

| Parameter | Description | Typical Range |
|---|---|---|
| `N` | Number of participants | 2–5 |
| `R_max` | Max deliberation rounds | 1–3 |
| `B_total` | Total token/compute budget | — |
| `L_max` | Max answer length | 1200 tokens |
| `L_final_max` | Max final answer length | 1400 tokens |
| `M_min` | Minimum valid responses required (quorum) | 2 |
| `epsilon_consensus` | Consensus stop threshold | 0.85–0.95 |
| `epsilon_delta` | Delta stop threshold (no change) | 0.03–0.06 |
| `gamma_max` | Max answer length growth ratio per round | 1.08–1.20 |
| `S_stall` | Stagnation rounds before stop | 1–2 |
| `W_max` | Wall-clock timeout | — |

### 3.3 State Machine (12 states)

```
INIT
  │
  ▼
GENERATE ───────────────────────────────────────────┐
  │                                                 │
  ▼                                                 │
VALIDATE_GENERATION                                 │
  │                                                 │
  ▼                                                 │
EVALUATE                                            │
  │                                                 │
  ▼                                                 │
VALIDATE_EVALUATION                                 │
  │                                                 │
  ▼                                                 │
AGGREGATE                                           │
  │                                                 │
  ▼                                                 │
DECIDE ─── stop_met=true ──────────────────────┐    │
  │                                            │    │
  │ can_refine=true                            │    │
  ▼                                            │    │
REFINE ◄───────────────────────────────────────┘    │
  │                                                 │
  ▼                                                 │
VALIDATE_REFINEMENT                                 │
  │                                                 │
  └─────────────────────────────────────────────────┘ (back to EVALUATE)
                                                 │
                                            FINALIZE
                                                 │
                                            TERMINATE

ANY state ──► FAIL (absorbing)
```

**Forbidden transitions:** INIT→REFINE, GENERATE→FINALIZE, REFINE→GENERATE, TERMINATE→ANY, FAIL→ANY.

TERMINATE and FAIL are absorbing states (no exit).

### 3.4 Key Invariants

| ID | Invariant |
|---|---|
| I1 | Round Boundedness: `0 ≤ t ≤ R_max` |
| I2 | Budget Boundedness: `B_used ≤ B_total` |
| I3 | Answer Length Boundedness: `len(A_i^t) ≤ L_max` |
| I4 | Monotonic Traceability: every artifact traceable to participant_id, round_id, parent |
| I5 | No State Resurrection: TERMINATE and FAIL are absorbing |
| I6 | Single Revision: max one revised answer per participant per round |
| I7 | Deliberation Boundedness: no unrestricted cross-talk; all through controller-approved artifacts |
| I8 | Context Compression: compress if cumulative context exceeds `X_context_max` |
| I9 | Convergence Gate: if convergence met, no further refinement |
| I10 | Non-Expansion Discipline: `avg_len(t+1) ≤ gamma * avg_len(t)` |
| I11 | Validity Before Scoring: only schema-valid answers may be scored/ranked/synthesized |
| I12 | Minimum Viable Diversity: at least `D_min` distinct candidates should exist |

**Termination is guaranteed** by: finite state space, absorbing terminal states, bounded `R_max`, bounded `B_total`, nonzero resource consumption per cycle.

### 3.5 Stopping Rules

**Hard stops (highest priority):**

- `t ≥ R_max`
- `B_used ≥ B_total`
- `wall_clock ≥ W_max`

**Convergence stops:**

- `K^t ≥ epsilon_consensus` (pairwise similarity high)
- `D^t ≤ epsilon_delta` (round-to-round change small)
- `top_score_margin ≥ margin_stop`

**Stagnation stop:**

- No improvement for `S_stall` consecutive rounds

**Bloat stop:**

- `avg_len(t+1) / avg_len(t) > gamma_max`

**Stop priority:** hard > safety > bloat > convergence > stagnation

**Composite predicate:**

```
stop_met(t) := hard_stop(t) OR convergence_stop(t) OR delta_stop(t) OR stagnation_stop(t) OR bloat_stop(t)
```

### 3.6 Metrics

```
Similarity:   sim(A_i, A_j) ∈ [0,1]   — embedding cosine, NLI, or judge-based

Consensus:    K^t = avg_{i≠j}(sim(A_i^t, A_j^t))

Delta:        D^t = avg_i(1 - sim(A_i^t, A_i^{t-1}))

Quality:      S_i^t = w_correctness·correctness + w_completeness·completeness
                    + w_clarity·clarity + w_brevity·brevity
                    + w_faithfulness·faithfulness + w_safety·safety

Growth Ratio: growth(t) = avg_len(t) / avg_len(t-1)

Improvement:  I^t = best_score(t) - best_score(t-1)
```

---

## 4. Artifact Schemas

All inter-component communication uses structured artifacts with a common envelope.

### 4.1 Common Envelope (required on every artifact)

```yaml
message_id: msg-{uuid}
message_type: answer | critique | score | decision | final_answer | failure | ...
protocol_id: lccp
protocol_version: "1.0"
task_id: task-{uuid}
round: 0
sender_id: llm-1 | controller | arbiter
timestamp: "2026-04-15T12:00:00Z"   # RFC 3339 UTC
parent_ids: [msg-abc, msg-def]       # provenance chain
schema_version: "1.0"
```

### 4.2 Eleven Canonical Artifact Types

**Minimum required:** `answer`, `critique` or `score`, `decision`, `final_answer`, `failure`.

#### task

```yaml
type: task
prompt: "..."
objective: "..."
constraints:
  max_answer_length: 1200
  max_rounds: 3
  required_format: markdown | json | plain
  safety_policy: standard | strict
scoring_policy:
  weights: {correctness: 0.35, completeness: 0.15, clarity: 0.10, brevity: 0.10, faithfulness: 0.20, safety: 0.10}
finalization_policy: select_best | synthesize_top_k | fallback_best_so_far
```

#### answer

```yaml
type: answer
answer_id: ans-{uuid}
role: generator | critic | verifier | simplifier
confidence: 0.82       # [0,1]
estimated_quality: 0.75
summary: "..."         # required when body is long
body: "..."
claims:                # structured, with stable IDs
  - id: claim-1
    text: "..."
assumptions: [...]
limitations: [...]
```

#### critique

```yaml
type: critique
critique_id: crt-{uuid}
target_answer_id: ans-{uuid}
evaluator_role: critic | verifier
scores:
  correctness: 0.7
  completeness: 0.6
  clarity: 0.8
  brevity: 0.9
  faithfulness: 0.7
issues:
  - id: issue-1
    severity: low | medium | high | critical
    text: "..."
suggested_fixes:       # short, atomic — no full rewrites
  - "..."
verdict: accept | revise | reject | uncertain
```

#### score

```yaml
type: score
score_id: scr-{uuid}
target: ans-{uuid}
dimensions: {correctness: 0.82, completeness: 0.75, ...}
aggregate_score: 0.80
confidence: 0.85
rationale: "..."
```

#### metrics

```yaml
type: metrics
consensus: 0.78
delta: 0.14
avg_length: 420
growth_ratio: 1.07
improvement: 0.05
top_score_margin: 0.12
ranking:
  - rank: 1
    answer_id: ans-103
    score: 0.85
clusters: [...]
```

#### decision

```yaml
type: decision
decision_id: dec-{uuid}
state: DECIDE
action: refine | finalize | fail
reason_code: convergence_not_met | consensus_reached | budget_exhausted | ...
finalize_mode: select_best | synthesize_top_k | fallback_best_so_far
shortlisted_answer_ids: [ans-101, ans-103]
stop_predicates:
  hard_stop: false
  convergence_stop: false
  delta_stop: false
  stagnation_stop: false
  bloat_stop: false
```

#### refinement_packet

```yaml
type: refinement_packet
participant_id: llm-1
own_previous_answer_id: ans-101
objective: "..."
top_issues: [...]           # bounded list
shortlist_answers: [...]    # top-K only, not all
compressed_context: "..."   # summary, NOT raw transcript — anti-bloat
```

#### final_answer

```yaml
type: final_answer
final_answer_id: fin-{uuid}
mode: select_best | synthesize_top_k | fallback_best_so_far
source_answer_ids: [ans-113, ans-111]
summary: "..."
body: "..."
confidence: 0.89
rationale: "..."             # short, bounded
```

#### failure

```yaml
type: failure
failure_id: fail-{uuid}
reason_code: invalid_task_config | insufficient_quorum | budget_exhausted |
             evaluation_invalid | aggregation_failed | finalization_failed |
             schema_violation | controller_error | participant_timeout_exhaustion
last_safe_state: EVALUATE
recoverable: false
details: "..."
```

### 4.3 Anti-Bloat Schema Rules

1. Critique body SHOULD be absent; use structured `issues` array.
2. Suggested fixes SHOULD be short and atomic — never full replacement answers.
3. `refinement_packet.compressed_context` SHOULD be a summary, not raw transcript.
4. Answer `summary` SHOULD be present when `body` is long.
5. `final_answer.rationale` SHOULD be short and bounded.

**Schema validity predicate:**

```
valid(m) := envelope_valid(m) AND payload_valid(m) AND referential_integrity_valid(m) AND bounds_valid(m)
```

---

## 5. Scoring, Consensus, and Finalization Policies

### 5.1 Scoring

**Canonical weighted model:**

```
S(a) = w_correctness·correctness(a)
     + w_completeness·completeness(a)
     + w_clarity·clarity(a)
     + w_brevity·brevity(a)
     + w_faithfulness·faithfulness(a)
     + w_safety·safety(a)
```

**Canonical weights:**

```
correctness = 0.35, faithfulness = 0.20, completeness = 0.15,
clarity = 0.10, brevity = 0.10, safety = 0.10
```

**Critique-derived penalty:**

```
penalty(a) = 0.01·count(low) + 0.03·count(medium) + 0.08·count(high) + 0.20·count(critical)
S'(a) = max(0, S(a) - penalty(a))
```

**Trust-weighted scoring:**

```
score_dim(a, d) = Σ(trust(e) · score_e(a, d)) / Σ(trust(e))
```

**Brevity score:** `brevity(a) = clamp(1 - overlength_ratio, 0, 1)` — must not dominate correctness.

**Safety score:** In high-assurance profiles, safety is a **gating predicate** (unsafe answers rejected entirely), not just an additive weight.

### 5.2 Ranking

Descending aggregate score. Deterministic tie-break order: higher correctness → higher faithfulness → lower penalty → shorter length → earlier artifact ID.

### 5.3 Consensus Computation

**Global:** `K^t = avg_{i≠j}(sim(A_i^t, A_j^t))`

**Clustered (better for diverse outputs):** Build similarity graph, derive clusters, use dominant cluster's intra-similarity.

**Similarity implementations:**

- Embedding cosine (embedding API + cosine dot product)
- NLI-based (entailment probability)
- Judge-model (LLM rates similarity 0–1)
- Hybrid lexical-semantic

For production Go without embedding infrastructure: **LLM-as-Judge** is simplest — prompt a model to output CONVERGED/DIVERGED.

### 5.4 Finalist Selection

- **Top-K** (K in {1, 2, 3})
- **Diversity-aware:** add next answer only if `sim(new, already_selected) < tau_redundant`
- `tau_redundant` = 0.94–0.97

### 5.5 Finalization Modes

**select_best:** `argmax S(a)`. Use for objective tasks, fixed-schema output, high synthesis risk.

**synthesize_top_k:** Merge bounded finalist set.

- Shared-Core First: identify claims supported by quorum (`q_shared`)
- Contradiction Resolution: prefer higher faithfulness → majority support → higher rank → omit
- Anti-Concatenation Rule: **MUST NOT simply concatenate** finalists

**fallback_best_so_far:** Emit BestSoFar. Required fallback for degraded termination.

**Decision logic:**

```
if objective_task AND fixed_schema_required → select_best
elif high_consensus AND finalists_complementary AND low_bloat → synthesize_top_k
else → select_best
```

**Downgrade chain (never skip steps):** synthesize_top_k → select_best → fallback_best_so_far → fail

---

## 6. Failure Handling and Safety

### 6.1 Safety Goals

1. **Termination Safety:** finite time to TERMINATE or FAIL
2. **Boundedness Safety:** rounds, budget, runtime, lengths, context, finalist set
3. **Validation Safety:** no unvalidated artifacts influence state
4. **Fallback Safety:** degrade before failing
5. **Traceability Safety:** all outcomes explainable through trace

### 6.2 Failure Taxonomy

| Category | Examples |
|---|---|
| Configuration | Invalid thresholds, contradictory schemas — always fatal before generation |
| Participant | Timeout, empty, malformed, oversized, duplicate, repeated low quality |
| Evaluation | Malformed critiques, invalid references, rewrite critiques |
| Aggregation | Metric computation failure, corrupted ranking, similarity engine failure |
| Refinement | Insufficient valid revisions, length explosions, broken parent links |
| Finalization | Invalid final schema, oversized, unresolved contradictions, synthesis failure |
| Controller | Illegal transition, terminal-state escape, invariant violation — always fatal |
| Safety-policy | Safety gate violation |

**Severity levels:**

- **Recoverable:** can continue without violating hard bounds/quorum
- **Degraded:** normal flow impossible but safe fallback exists
- **Fatal:** no compliant continuation possible

### 6.3 Participant Safety Rules

- **Isolation Rule:** all participant outputs treated as untrusted until validated
- **Timeout Rule:** exclude participant, continue if quorum met; finalize by fallback if quorum lost
- **Retry Rule:** `max_retries_per_phase_per_participant ≤ 1`
- **Quarantine Rule:** mark degraded after repeated malformed/empty/duplicate artifacts
- **Collapse Rule:** repeatedly failing participant removed for remainder of run

### 6.4 Critique Safety Rules

- **Rewrite Prohibition:** critiques MUST NOT contain full replacement answers
- **Low-Signal Critique Rule:** uninformative/repetitive critiques may be discarded
- **Critique Spam Rule:** no participant may dominate refinement context through volume

### 6.5 Arbiter Robustness

Arbiter outputs are still subject to validation. Failure fallback order:

1. Deterministic weighted ranking
2. Majority voting
3. Non-arbiter aggregation (score-based)

Bias mitigation: combine with participant scores, limit arbiter weight, calibration benchmarks.

### 6.6 Collusion / Coordinated Bias Detection

Indicators: unusually high mutual scoring, near-identical bodies, repeated unsupported agreement.

Mitigation: trust reweighting, cluster deduplication, per-clique influence caps.

### 6.7 Canonical Failure Response Order

1. Validate and reject malformed artifact
2. Discount low-trust evaluator
3. Quarantine repeated offender
4. Continue if quorum remains
5. Degrade protocol mode
6. Finalize by bounded fallback
7. Fail explicitly

### 6.8 BestSoFar

Maintain `BestSoFar` state (highest-ranked valid answer seen so far). Update after each AGGREGATE phase. Enables fault-tolerant partial completion — never fail with an empty result when any valid answer was produced.

---

## 7. Operational Hazards and Mitigations

25+ identified hazards from research. The **top 5 most dangerous** are: false consensus, convergence to mediocrity, single model/judge dominance, context poisoning, and no external verification.

### Cognitive and Quality Hazards

| Hazard | Symptom | Mitigation |
|---|---|---|
| **False Consensus** | Models converge on plausible but wrong answer; high K^t on incorrect claim | External verifier role; RAG/search for factual queries; never present consensus as proof of correctness |
| **Convergence to Mediocrity** | Synthesis removes sharp insights; generic "committee text" | Champion strategy; preserve `best_so_far`; merge only top-2; explicitly prompt to preserve unique insights |
| **Correlated Errors / Shared Blindspots** | Unanimous wrong answer from similar-training models | Use models from different providers; include verifier role; RAG |
| **Sycophancy / Herding** | Models defer to majority or apparent authority; positional bias | Randomize answer order; explicitly prompt "identify flaws even if majority agrees"; temperature > 0 |
| **Single Model Dominance** | Expensive/confident model frames all others ("LLM monarchy") | Blinded evaluation; anonymize answers; cap authority weights at 30% |
| **Anchor Bias** | First answer in context dominates all subsequent reasoning | Shuffle presentation order per round; blind peer review |
| **Reward Hacking** | Models optimize for judge criteria (verbosity, structure) not real quality | Partially hide rubric; hidden checks; penalize shallow structural inflation |
| **Pseudo-Critique** | Critiques sound good but contain no actionable issues | Strict critique schema: issue/severity/evidence/fix; reject low-signal critiques |
| **Refinement Degradation** | Good initial answer made worse through over-correction | Compare against previous score; allow "reject-refinement" option; preserve BestSoFar |
| **Output Blandness** | Expressive unique insights removed during synthesis | Champion strategy; explicitly prompt to preserve unique insights, not average |
| **Minority Insight Suppression** | Rare but critical edge case lost through majority vote | Dissent channel; "minority objection" section; risk register |

### Structural / Architectural Hazards

| Hazard | Symptom | Mitigation |
|---|---|---|
| **Context Poisoning** | Bad summary or critique corrupts next round | Include source links/IDs; spot-check originals; sanitize inter-round output |
| **Summary Bottleneck** | Compression loses nuance, counterarguments, edge cases | Multi-layer summary: shared core + dissent + unresolved risks |
| **Pseudo-Diversity** | N nominally different models produce functionally identical output | Require different providers; different system prompts; different temperatures |
| **Wrong Stop Metric** | Stop too early on pseudo-consensus or stalled stability | Composite stop with multiple independent criteria; log divergence per round |
| **Instruction Contamination / Drift** | Critique/summary injects new constraints; task semantics drift | Freeze task spec; include task hash in every artifact; re-inject system prompt each round |
| **Prompt Injection Propagation** | Malicious injection in one response propagates to all agents | Sanitize inter-round output; pass only arbiter's summary, not raw output |
| **Confidence Inflation** | Each round agents raise confidence because others agree (group euphoria) | Track response entropy; high agreement + no quality improvement = suspicious |
| **Goodhart's Law** | Models use same words to increase cosine similarity without improving content | Combine cosine similarity with LLM-Judge; track both semantic similarity and quality score |
| **Persona/Role Collapse** | By round 3, agents forget roles; context overflow washes out system prompt | Re-inject full role prompt each round; use JSON structs for structured responses |

### Operational Hazards

| Hazard | Symptom | Mitigation |
|---|---|---|
| **Cost / Latency Explosion** | 5 models × 3 rounds × arbiter × embeddings = expensive | 3–5 models max; 1–2 refine rounds; early exit; synthesize only top-2; tiered models |
| **Result Instability** | Different runs produce different answers | Lower temperature for synthesis; deterministic policies; audit trace |
| **Debugging Difficulty** | Multi-agent systems "break diffusely" — no clear root cause | Strong traceability; event log; parent_ids; artifact replay; structured logging |
| **False Consensus Confidence** | "3/3 models agree" shown as reliability signal | Never present consensus as proof; add disclaimers; external verification |
| **Wrong Domain Application** | Rada used for simple factual answers where one model + RAG is better | Task complexity classifier; adaptive router |
| **Race Conditions** | Parallel agents see intermediate streaming results and diverge mid-generation | Barrier synchronization; no streaming between agents within a round |

### Minimum Practical Defense Set

Implementations SHOULD address at minimum:

1. `best_so_far` tracking
2. External verifier hook (search, test execution, fact-check)
3. Critique schema with low-signal rejection
4. Bounded refinement packet (no raw transcript)
5. Duplicate/near-duplicate suppression
6. Minority objection preservation
7. Composite stop rule (hard + convergence + stagnation + bloat)
8. Fallback chain: synthesis → select_best → fallback_best_so_far

---

## 8. Go Implementation Patterns

### 8.1 Core Interfaces

```go
// LLM is the primary participant interface.
type LLM interface {
    ID() string
    Generate(ctx context.Context, prompt string, temperature float64, maxTokens int) (string, error)
}

// Arbiter extends LLM with evaluation and synthesis capabilities.
type Arbiter interface {
    LLM
    Rank(ctx context.Context, prompt string, responses []Response) ([]Response, error)
    Synthesize(ctx context.Context, prompt string, topResponses []Response) (string, error)
    CheckConsensus(ctx context.Context, prev, curr []Response) (bool, float64, error)
}
```

### 8.2 Key Types

```go
type Strategy int
const (
    StrategyGenerateRankRefine Strategy = iota
    StrategyDebate
    StrategyMoA
    StrategyMajorityVote
    StrategyPeerReview   // Karpathy-style
)

type Config struct {
    MaxRounds          int           // hard iteration limit (2–4)
    ConsensusThreshold float64       // stop when delta < threshold
    MaxResponseTokens  int           // token limit per response (e.g. 1024)
    TemperatureStart   float64       // initial temperature (0.7–0.9 for diversity)
    TemperatureEnd     float64       // final temperature (0.1–0.3 for convergence)
    Strategy           Strategy
    TimeoutPerRound    time.Duration
    BudgetTotal        int           // total token budget across all rounds
    MinQuorum          int           // min valid responses required (M_min)
}

type Response struct {
    ModelID   string
    Content   string
    Score     float64  // 0..1, filled by arbiter
    Reasoning string
    Round     int
    ParentID  string   // for traceability
}

type RoundResult struct {
    Round     int
    Responses []Response
    Best      *Response
    Consensus bool
    Delta     float64  // round-to-round change
    Metrics   Metrics
}

type Metrics struct {
    Consensus   float64  // K^t
    Delta       float64  // D^t
    AvgLength   int
    GrowthRatio float64
    Improvement float64
    BestScore   float64
}
```

### 8.3 Parallel Generation

```go
func generateParallel(ctx context.Context, llms []LLM, prompt string, temp float64, maxTokens int) []Response {
    results := make([]Response, len(llms))
    var wg sync.WaitGroup
    var mu sync.Mutex

    for i, llm := range llms {
        wg.Add(1)
        go func(idx int, l LLM) {
            defer wg.Done()
            content, err := l.Generate(ctx, prompt, temp, maxTokens)
            if err != nil {
                return // tolerate partial failure; quorum check later
            }
            mu.Lock()
            results[idx] = Response{ModelID: l.ID(), Content: content}
            mu.Unlock()
        }(i, llm)
    }
    wg.Wait()
    return filterNonEmpty(results)
}
```

### 8.4 Temperature Decay

```go
// Linear interpolation from start (diversity) to end (convergence).
func temperatureForRound(round, maxRounds int, start, end float64) float64 {
    if maxRounds <= 1 {
        return end
    }
    t := float64(round-1) / float64(maxRounds-1)
    return start + t*(end-start)
}
```

Alternative: exponential decay `T = T_0 * 0.9^i`.

### 8.5 Convergence Check

**Option 1 — LLM-as-Judge** (simplest for Go; no embedding library required):

```go
// Ask arbiter to return structured JSON: {"consensus_reached": bool, "score": 0.0-1.0}
func checkConsensusViaJudge(ctx context.Context, arbiter LLM, prev, curr []Response) (bool, float64, error) {
    // build prompt with anonymized prev/curr responses
    // parse JSON response
}
```

**Option 2 — Cosine Similarity** (requires embedding API):

```go
func cosineSimilarity(a, b []float32) float64 {
    var dot, normA, normB float64
    for i := range a {
        dot += float64(a[i]) * float64(b[i])
        normA += float64(a[i]) * float64(a[i])
        normB += float64(b[i]) * float64(b[i])
    }
    if normA == 0 || normB == 0 {
        return 0
    }
    return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
```

**Option 3 — For code** (most precise for code tasks): AST comparison using `go/ast`, or unit test pass/fail (functional consensus).

### 8.6 JSON Extraction from Dirty LLM Output

```go
func extractJSON(s string) string {
    start := strings.Index(s, "{")
    end := strings.LastIndex(s, "}")
    if start < 0 || end < start {
        return ""
    }
    return s[start : end+1]
}
```

### 8.7 Context Compression

Pass summaries between rounds, not raw transcripts:

```
Round N+1 prompt contains:
  - Original task (always present; frozen)
  - Your previous answer
  - Summary of top issues identified in round N (structured critique issues)
  - Shortlisted top-K answers from round N (not all answers)
  NOT: full history of all previous answers from all rounds
```

### 8.8 Arbiter Prompt for Structured Ranking

```go
const rankPromptTemplate = `
You are evaluating {{.N}} candidate answers to the following task:

TASK: {{.Task}}

CANDIDATE ANSWERS:
{{range .Answers}}
[{{.Label}}]: {{.Content}}
{{end}}

Evaluate each answer on: correctness, completeness, clarity, brevity, faithfulness.
Respond ONLY with a JSON array:
[
  {"label": "A1", "score": 0.85, "reasoning": "..."},
  ...
]
`
```

### 8.9 OpenRouter Adapter Pattern

```go
type OpenRouterLLM struct {
    modelID string
    apiKey  string
    baseURL string // "https://openrouter.ai/api/v1/chat/completions"
}

// Example council composition:
// Members: claude-sonnet, gpt-4o, gemini-2.5-pro, deepseek-r1
// Arbiter: claude-opus (or any high-capability model)
```

### 8.10 Stopping Criterion Interface

```go
type StoppingCriterion interface {
    ShouldStop(ctx context.Context, state *CouncilState) (bool, *StoppingDecision)
}

// Five built-in criteria:
// 1. HardLimitCriterion (rounds, budget, wall-clock)
// 2. QualityThresholdCriterion (average quality above target)
// 3. ConsensusAgreementCriterion (pairwise agreement + stability)
// 4. DiminishingReturnsCriterion (improvement < epsilon for N rounds)
// 5. CostBenefitCriterion (ROI = quality_gain * value / next_round_cost)
```

---

## 9. Production Deployment Patterns

### 9.1 Task Complexity Classification and Adaptive Routing

Not every query warrants a full council. Route adaptively:

| Complexity | Criteria | Action |
|---|---|---|
| Trivial | Factual lookup, simple classification | Single LLM |
| Simple + low stakes | Straightforward with minor ambiguity | Single LLM |
| Simple + high stakes | Low complexity but high cost of error | Lightweight council (2 LLMs) |
| Moderate | Reasoning, ambiguity, multiple valid answers | Rada (2–3 LLMs) |
| Complex / Expert | High stakes, auditing required, domain expertise | Full council with roles |

**Scoring factors:** +2 for reasoning requirements, +2 for ambiguity, +2 for domain expertise required, +3 for high stakes, -2 for factual/classification tasks, -1 for reversible decisions.

### 9.2 Tiered Model Strategy

Use cheaper models for early rounds, expensive models only for final synthesis:

- **Round 1–2:** GPT-4o-mini / Claude Haiku / Gemini Flash (exploration)
- **Final Arbitration:** GPT-4o / Claude Sonnet-3.5 / Gemini Pro (synthesis)
- **Optional Verifier:** Any model with tool access for factual checking

Early exit: if round-1 confidence > 0.90, skip remaining rounds.

### 9.3 Context Compression Strategies

| Strategy | When to use | Quality | Cost |
|---|---|---|---|
| Raw (full log) | Max 2 rounds, short responses | Highest | Highest |
| Adaptive (last round full + previous summarized) | Standard production | Good | Medium |
| Bullet points (facts only) | Many rounds, long responses | Facts preserved | Low |
| Skeletonize (code: signatures only) | Code tasks with finalized functions | High for code | Low |
| Diff-Focus | Code: send only changed function with context | High for code | Lowest |

**Dynamic token budgeting for code tasks:**

- `<4000 tokens:` full log
- `4000–12000 tokens:` skeletonize finalized functions
- `>12000 tokens:` names + rationale only

### 9.4 Anti-Sycophancy Mechanisms

- **Anonymous Peer Review:** Never reveal which model authored which answer during critique rounds. Use labels (A1, A2...).
- **Order Randomization:** Shuffle answer order on every arbiter call (positional bias mitigation).
- **Devil's Advocate Protocol:** After initial proposals, if consensus > 0.6, engage a designated model to actively find problems. Rotate the devil's advocate role. Config: `RotateDevil`, `MaxDevilRounds`.
- **Confidence Calibration:** Weight by calibrated confidence, not just vote count. Penalize agreement (high agreement may indicate shared bias). Flag: high agreement + low disagreement = suspicious.
- **Authority Weight Cap:** Cap any single model's influence at 30% regardless of its trust score.

### 9.5 Inter-Agent Communication Patterns

| Pattern | Description | Best For |
|---|---|---|
| Hub-and-Spoke | All messages go through controller/arbiter | Simple councils; audit trail |
| Mesh (P2P) | Direct model-to-model messaging | Debate strategy |
| Pipeline (Chain) | A critiques B, B critiques C, C critiques A | Sequential refinement |
| Broadcast (Conference) | All agents see all messages simultaneously | MoA aggregation layer |

**Message envelope** for structured communication:

```go
type MessageEnvelope struct {
    Type        string   // proposal, critique, vote, question, agreement, disagreement, summarize, refine
    Sender      string
    Recipients  []string
    Round       int
    Confidence  float64
    Importance  float64
    Domain      string
    Stance      string  // for/against/neutral
    TokensSpent int
    Content     string
}
```

### 9.6 Monitoring and Observability

**Key metrics to track:**

```
Rada-level:
  - request_count, request_duration, request_cost, request_quality
  - rounds_to_converge, early_stop_rate, agreement_rate, dissent_rate

Per-LLM:
  - latency, token_usage, error_rate, quality_score, first_place_rate

Resource:
  - total_tokens_per_request, cost_per_request, timeout_count
```

**Observability stack:** Prometheus (metrics) + Loki (logs) + Tempo (traces) + Grafana (dashboards)

**Structured logging fields (every log entry):**

```json
{
  "trace_id": "...", "span_id": "...",
  "task_id": "...", "round": 2, "llm_id": "gpt-4o",
  "event": "answer_accepted", "tokens": 420, "quality": 0.82
}
```

**Divergence logging rule:** Per-round consensus K^t should rise then fall (or stabilize). If K^t drops immediately from round 0, it's likely groupthink — flag it.

### 9.7 Cost Optimization

- Semantic caching for similar prompts (embedding-based cache lookup).
- Cache embeddings for convergence checks between rounds.
- Circuit breaker + exponential backoff for API failures.
- Rate limit handling: skip slow LLM in a given iteration if quorum is preserved.
- Max 3–5 models; 1–2 refine rounds; synthesize only top-2.

### 9.8 Error Handling

```go
// Graceful degradation order:
// 1. Retry failed participant (max 1 retry)
// 2. Exclude participant, continue if quorum met
// 3. Reduce to degraded mode (skip synthesis, select_best)
// 4. Finalize with fallback_best_so_far
// 5. Return explicit failure with reason code

// Never fail silently — always return a failure artifact with reason_code and last_safe_state.
```

### 9.9 Security Considerations

- **Prompt injection propagation** is the primary council-specific security risk.
  - Sanitize all LLM outputs before injecting into other models' prompts.
  - Use structured JSON outputs (schemas constrain injection surface).
  - Never pass raw responses; pass arbiter's validated summary.
- **Tool use** in council agents (web search, code execution) creates significant attack surface.
  - Sandboxed execution; never run arbitrary code from LLM suggestions in production.
  - All tool arguments validated before execution.
- **System prompt leakage:** agents may quote each other's system prompts. Use opaque role IDs.

---

## 10. Configuration Profiles and Conformance Levels

### 10.1 Configuration Profiles

#### Minimal Profile (`lccp-minimal-v1`)

```yaml
participants: 2
max_rounds: 1
evaluation: score_only
arbiter: none
finalization: select_best
epsilon_consensus: 0.95
epsilon_delta: 0.03
gamma_max: 1.10
```

**Use:** Low-cost, low-latency, objective tasks.

#### Balanced Profile (`lccp-balanced-v1`) — **recommended default**

```yaml
participants: 3-4
max_rounds: 2
evaluation: critique+score
arbiter: optional
finalization: synthesize_top_k (k=2)
redundancy_threshold: 0.97
epsilon_consensus: 0.90
epsilon_delta: 0.05
gamma_max: 1.15
```

#### High-Assurance Profile (`lccp-high-assurance-v1`)

```yaml
participants: 3-4
max_rounds: 1
evaluation: critique+score+verification
verifier: required
finalization: select_best (k=1)
synthesis: disabled
safety_policy: strict_gating
epsilon_consensus: 0.92
epsilon_delta: 0.04
gamma_max: 1.08
```

**Use:** Legal, financial, medical, security contexts.

#### Creative Synthesis Profile (`lccp-creative-synthesis-v1`)

```yaml
participants: 3-5
max_rounds: 2
evaluation: critique+score
finalization: synthesize_top_k (k=3, diversity_aware)
redundancy_threshold: 0.94
epsilon_consensus: 0.85
epsilon_delta: 0.06
gamma_max: 1.20
```

**Use:** Ideation, strategy, brainstorming.

### 10.2 Conformance Levels

| Level | Description | Recommended For |
|---|---|---|
| **Core** | State machine, structured artifacts, validation, hard bounds, one scoring policy, traceability | Prototypes |
| **Safe** | Core + fallback behavior, failure classification, quarantine, anti-bloat, convergence stopping | **Production** |
| **Robust** | Safe + duplicate-aware consensus, arbiter fallback, trust weighting, contradiction-aware synthesis, adversarial handling | High-value deployments |
| **Auditable** | Robust + full trace reconstruction, policy versioning, deterministic replay, full provenance chain | Regulated / research |

### 10.3 Canonical Production Configuration

```yaml
# Scoring weights
scoring:
  correctness: 0.35
  faithfulness: 0.20
  completeness: 0.15
  clarity: 0.10
  brevity: 0.10
  safety: 0.10

# Penalty coefficients
penalty:
  low: 0.01
  medium: 0.03
  high: 0.08
  critical: 0.20

# Consensus
consensus:
  method: global_similarity_average
  epsilon: 0.90

# Delta (round-to-round change)
delta:
  method: self_alignment
  epsilon: 0.05

# Improvement / stagnation
improvement:
  method: best_score_delta
  epsilon: 0.01
  stall_rounds: 1

# Bloat control
bloat:
  gamma_max: 1.15

# Finalist selection
finalists:
  method: top_k
  k: 2
  redundancy_threshold: 0.97

# Finalization
finalization:
  mode: synthesize_top_k
  contradiction_resolution: prefer_higher_faithfulness_then_majority
```

---

## 11. REST API Design Considerations

For a conversational council API server, key design decisions:

### 11.1 Core Endpoints

```
POST   /api/v1/council             — start a council session
GET    /api/v1/council/{id}        — get session status and result
GET    /api/v1/council/{id}/rounds — get per-round detail (for transparency/debugging)
POST   /api/v1/council/{id}/continue — resume with follow-up message (conversational)
DELETE /api/v1/council/{id}        — cancel in-progress session

GET    /api/v1/council-types       — list supported council configurations
GET    /api/v1/health              — liveness
```

### 11.2 Session Request Shape

```json
{
  "prompt": "...",
  "council_type": "balanced | high-assurance | creative | minimal | custom",
  "conversation_id": "...",     // optional: link to prior session for context
  "preferences": {
    "max_rounds": 2,
    "participants": ["claude-sonnet", "gpt-4o", "gemini-pro"],
    "arbiter": "claude-opus",
    "strategy": "peer-review | debate | moa | vote"
  }
}
```

### 11.3 Response Shape

```json
{
  "session_id": "...",
  "status": "complete | running | failed",
  "final_answer": {
    "body": "...",
    "confidence": 0.89,
    "finalization_mode": "synthesize_top_k",
    "rounds_taken": 2,
    "consensus_score": 0.92
  },
  "metadata": {
    "total_tokens": 4200,
    "estimated_cost_usd": 0.043,
    "wall_clock_ms": 8400
  }
}
```

### 11.4 Streaming

For long-running sessions, support SSE or WebSocket streaming:

- `round_started` event
- `participant_answer` event (one per model per round)
- `round_complete` event with metrics
- `final_answer` event
- `error` event with reason code

### 11.5 Rada Type Registry

Register named council configurations that users can select by name. Each `CouncilType` record contains:

- Name and description
- Participant model IDs
- Arbiter model ID
- Strategy (peer-review, debate, moa, vote)
- Config profile (balanced, high-assurance, creative, minimal)

### 11.6 Conversational Context

For multi-turn conversations:

- Maintain a `conversation_id` that chains council sessions.
- Pass a compressed summary of prior exchanges to each new session.
- Limit context to last 2–3 turns; compress older turns.

### 11.7 Transparency Mode

Optionally expose per-round artifacts for debugging, research, or user-facing transparency:

- All generated answers (anonymized during council, attributed in response)
- All critique scores and issues
- Per-round metrics (K^t, D^t, growth ratio)
- Stopping reason and finalization mode used

---

## 12. Implementation Design Decisions

Resolved architectural choices and open questions for the current v2 implementation.
The v1 implementation is preserved on `archive/v1` for reference.

### Context model — stateless per query

Each question is a new, independent council run with no memory of prior turns. A
conversation record exists for UI history only; it does not feed back into the council.
This is intentional for the first stage.

### Consensus metric — Kendall's W for rank-order strategies

Peer review (Stage 2 produces ordered rankings) maps directly to Kendall's W — no
external embedding calls, measures exactly rank agreement. For free-text strategies
(Debate, MoA), LLM-as-Judge is the v1 default (no embedding infrastructure required).
Cosine similarity is deferred until free-text strategies are added.

The W-to-prose translation from archive/v1 is carried forward:
- W ≥ 0.70 → "synthesize confidently"
- 0.40 ≤ W < 0.70 → "acknowledge alternatives"
- W < 0.40 → "present multiple perspectives"

### Self-evaluation paradox — anonymization is sufficient mitigation for v1

Separate ranker models add cost and operational complexity with uncertain benefit.
Mitigation: shuffled label assignment (`rand.Perm`) per request so no model is
systematically "Response A". Self-identification is probabilistic, not deterministic.
Separate ranker pools are a named council type variant, not a v1 requirement.

### Rada type scope — model sets only for v1; strategy abstraction deferred

A "council type" is a named configuration: a fixed strategy (Karpathy peer-review)
combined with a configurable model set and parameters. Multiple council types with
different model sets require zero interface changes. Strategy variants require a dispatch
layer and are out of scope until the peer-review design is proven.

### LCCP first-stage scope — Core conformance, single round, no REFINE loop

Effective state machine for v1:

```
INIT → GENERATE → VALIDATE_GENERATION → EVALUATE → VALIDATE_EVALUATION
     → AGGREGATE → DECIDE → FINALIZE → TERMINATE  (+ FAIL branch at any node)
```

Explicitly deferred: REFINE loop, BestSoFar tracking, fallback finalization modes
(synthesize_top_k → select_best → fallback_best_so_far). Target: Core conformance level.
Robust and Auditable conformance are post-v1. This is the scope-limiting decision that
drives the entire architecture.

### Quorum enforcement — M_min = ⌈N/2⌉ + 1, minimum 2; failure → error

If Stage 1 returns fewer than M_min successful responses, return an explicit error to the
caller. A single-model Stage 2 ranking is meaningless (a model ranking its own answer).
Within-quorum partial failures (some models fail but M_min succeed) are tolerated.
The quorum threshold is a council type configuration parameter, not hardcoded.

### Metadata persistence — store full metadata with every assistant message

Schema addition to the assistant message:

```json
{
  "role": "assistant",
  "stage1": [...],
  "stage2": [...],
  "stage3": {...},
  "metadata": {
    "council_type": "default",
    "label_to_model": {"Response A": "model-x", ...},
    "aggregate_rankings": [...],
    "consensus_w": 0.72
  }
}
```

Old conversation files (no `metadata` field) fail gracefully via zero-value defaults
in Go struct unmarshalling.

### Structured JSON output for Stage 2 rankings

Use `response_format: json_object` on OpenRouter for Stage 2 ranking responses.
Schema: `{"rankings": ["Response C", "Response A", "Response B", "Response D"]}`.
If JSON parsing fails: log `slog.Warn`, treat as missing ranking (existing midrank
imputation in Kendall's W handles it). Replaces the silent regex failure mode from v1.

### Trust weighting — deferred; uniform weights in v1

Trust weighting requires a calibration mechanism (ground-truth oracle or historical
performance data) that doesn't exist. Uniform weights are the correct default. Defer
until an evaluation framework exists.

### Strategy vs. council type — distinct concepts in code and API

- **Strategy:** A deliberation algorithm (`PeerReview`, `MajorityVoting`, `MoA`). Pure
  behavior, no model specifics. Typed constant/enum in Go.
- **Rada type:** A named, user-selectable configuration combining strategy + model
  set + parameters (`"default"`, `"expert"`, `"fast"`). User-facing.

In the API, the field is `council_type` (string name). Strategy is resolved server-side
from the council type registry. Users never specify a strategy directly.

### Chairman input — parsed/structured rankings, not raw Stage 2 prose

The Chairman receives parsed rankings formatted as structured attribution:
"Model X ranked these responses: 1st: Response C, 2nd: Response A, ...". The Chairman
prompt is constructed from Go structs, not by concatenating raw LLM output. With
structured JSON output in Stage 2 (see above), the ranking content contains no
user-controlled text — only server-assigned labels.

This resolves the internal synthesis contradiction: §2.5 (pass raw output) vs. §7/§9.9
(pass only validated summary). Middle ground: parsed structure, no full sanitization pass.

### Rada type selection API shape — field in POST request body

```json
POST /api/conversations/{id}/message
{"content": "What is the best sorting algorithm?", "council_type": "default"}
```

`council_type` is optional; defaults to server-configured default. Server-wide config
only (archive/v1 approach) is inadequate once multiple council types exist. Query
parameters are less discoverable and harder to validate.

### Stage 3 graceful non-synthesis — deferred to post-v1

The W-to-prose translation already guides the Chairman toward presenting perspectives
rather than synthesizing when W < 0.40. A first-class "council could not reach a
synthesis" outcome requires the REFINE loop and LCCP fallback chain — both explicitly
out of scope. Document as a known v1 limitation.

### Streaming architecture — stage-completion events only; no intra-stage token streaming

Token-by-token streaming within a stage requires threading a streaming callback through
the entire council pipeline interface. The SSE event-per-stage model provides meaningful
progress feedback without this interface complexity. Intra-stage token streaming is a
valid post-v1 enhancement.

### Storage interface — pluggable Storer from day one; JSON backend is v1 only

The `Storer` interface must be designed to allow replacing the JSON backend without
rewriting handlers. The JSON file backend is v1 only — `List()` is O(n) on disk and
does not scale. Interface design must not leak JSON-specific assumptions.

### OpenRouter model lifecycle — model IDs in configuration, not code

Rada type registry stores model IDs as configuration strings updatable without
redeployment. The implementation must not hardcode model IDs in source code.

### CalculateAggregateRankings — package-level function, not on Runner interface

In archive/v1 this lived on the `Runner` interface, forcing every strategy to satisfy
it regardless of whether it produces rankings. In v2 it is a package-level function
(or belongs to a separate ranking/metrics layer).

### Infrastructure prerequisites — confirmed requirements for any v2 build

The following are prerequisites, not optional niceties:

- Config validation at startup (fail fast on missing API key)
- HTTP client timeout on the OpenRouter client
- Graceful shutdown for the HTTP server
- Structured logging (`slog`)
- Handler tests using mock interfaces

---

## Open Questions

### "Preserve unique insights" is not operationalised

The synthesis repeatedly recommends "explicitly prompt synthesis to preserve unique
insights." Neither the synthesis nor any source material defines what makes an insight
"unique" or how to distinguish a fringe claim from a minority-but-correct insight.

**Current position:** The Chairman prompt includes an explicit instruction to surface
minority perspectives that appear well-reasoned even if not consensus — particularly when
W < 0.40. Formal operationalization (defining "unique" algorithmically, tracking insight
provenance) is deferred until an evaluation framework exists to validate any criterion.

**What needs answering before this can be closed:** A concrete rubric or test for
distinguishing "unique minority insight" from "fringe claim," or an explicit decision
that this is permanently a prompt-level heuristic.

---

## Key References

- **ChatGPT conversations:** Full LCCP RFC specification (state machine, artifact schemas, scoring policies, safety, profiles, hazards). The most formally rigorous source.
- **Claude conversations:** Practical Go implementation with 4 strategy types; identified sycophancy/herding and prompt injection propagation as critical council-specific risks.
- **DeepSeek conversations:** Tournament/Elo ranking; 2-random-answers-per-round pattern; diversity collapse distinction; arbiter inconsistency mitigation.
- **Gemini/Gemma4 conversations:** DPO dataset generation; framework comparison (AutoGen vs CrewAI vs LangGraph); convergence alternatives (AST, BERTScore, LLM-as-Judge); code-specific compression.
- **Grok conversations:** Karpathy's peer-review council as "gold standard"; `cloudwego/eino` Go framework; MAST failure taxonomy; concrete quality improvement quantification (+5–15% for complex tasks).
- **MiniMax conversations:** Most comprehensive production architecture — monitoring stack (Prometheus/Grafana/Loki/Tempo), inter-LLM communication bus, dynamic weight management, adaptive early stopping with ROI calculation, RAG integration, anti-bandwagon protocols, cognitive bias detectors.
- **Kimi/Mistral conversations:** Practical operations checklist; domain clustering for scalability; rollback and sandbox patterns.
