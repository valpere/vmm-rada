# Eval Harness — Backlog

Archived from GitHub issues #141–#151. These issues describe the full design of an
automated evaluation and optimization loop for VMM Rada.

---

## Dependency graph

```
#141 eval skeleton (cmd/eval/main.go)
    ├── #143 YAML council configs        (depends on #141)
    ├── #144 token + cost tracking       (parallel)
    └── #146 batch runner + report  ←── #142 benchmark format
              │                     ←── #145 meta-judge scoring  (p1)
              ├── #147 /improve-council skill
              ├── #148 iterate workflow  ←── #147
              │         └── #149 archive + revert
              ├── #150 eval documentation
              └── #151 CI smoke benchmark
```

---

## Issues

### #141 · feat(eval): headless single-question runner · `feature` `p2: medium` `eval`

**Overview**

Add a new binary `cmd/eval/main.go` that runs the full council pipeline headlessly —
without the HTTP server or React UI. This is the **foundation** for the automated
evaluation and optimization loop.

**Motivation**

Currently the only entry point is the HTTP server (`cmd/server/main.go`). To run
benchmarks programmatically and iterate on council configurations, we need a CLI that
calls `Rada.RunFull` directly.

**Acceptance criteria**

- [ ] Binary accepts `--question "..."`, `--council-type default`, `--out result.json` flags
- [ ] Imports `internal/council` and `internal/openrouter` directly (no HTTP layer)
- [ ] Captures all 3 stages via `EventFunc` callback and serialises to JSON
- [ ] `make eval-single Q="What is 2+2"` prints valid JSON with full metadata
- [ ] Reads council config from env vars (same as server)

**Files**

- New: `cmd/eval/main.go`
- Modified: `Makefile` — add `eval-single` target

**Existing code to reuse**

- `internal/council/runner.go` — `Rada.RunFull(ctx, query, councilTypeName, onEvent)`
- `internal/council/types.go` — `Metadata` struct (`ConsensusW`, `AggregateRankings`)
- `internal/openrouter/client.go` — concrete `LLMClient`
- `cmd/server/main.go` — `CouncilType` registry pattern to replicate

---

### #142 · feat(eval): benchmark dataset format + baseline question set · `feature` `p3: low` `eval`

**Overview**

Define the YAML schema for benchmark datasets and create the first `baseline.yaml` with
20 questions across 4 categories. Add a Go loader package.

**Schema**

```yaml
- id: code-001
  category: code        # code | analysis | factual | creative
  question: "Write a Go function that..."
  gold_answer: |        # optional — used by judge for rubric
    func ...
  rubric: |             # optional — scoring guidance for meta-judge
    Award full marks if...
```

Categories: `code`, `analysis`, `factual`, `creative` — 5 questions each = 20 total.

**Acceptance criteria**

- [ ] `eval/benchmarks/baseline.yaml` contains 20 valid questions
- [ ] `internal/eval/benchmark.go` parses YAML into `[]BenchmarkItem`
- [ ] `internal/eval/benchmark_test.go` covers parse + validation
- [ ] Loader returns typed error on malformed input

**Files**

- New: `eval/benchmarks/baseline.yaml`, `internal/eval/benchmark.go`, `internal/eval/benchmark_test.go`

---

### #143 · feat(eval): load council rosters from YAML configs · `feature` `p2: medium` `eval`

**Overview**

Extend the `CouncilType` registry to load configurations from `configs/*.yaml` files at
startup, instead of hardcoding a single `"default"` type. This enables running the eval
loop with different model rosters without code changes.

**Config schema**

```yaml
# configs/quality.yaml
name: quality
members:
  - google/gemini-2.5-pro
  - anthropic/claude-sonnet-4-6
  - openai/gpt-5.4
chairman: anthropic/claude-sonnet-4-6
temperature: 0.7
strategy: peer_review
```

Ship 3 example configs: `default`, `quality`, `fast`.

**Acceptance criteria**

- [ ] Server and eval binary both read `configs/*.yaml` on startup
- [ ] `eval --council-type quality` uses the roster from `configs/quality.yaml`
- [ ] Invalid YAML files produce a clear startup error
- [ ] Existing `.env`-based `RADA_MODELS` / `CHAIRMAN_MODEL` still work as fallback

**Files**

- New: `internal/config/council_configs.go`, `configs/default.yaml`, `configs/quality.yaml`, `configs/fast.yaml`
- Modified: `cmd/server/main.go` (registry loading)

**Depends on:** #141

---

### #144 · feat(eval): token and cost tracking in CompletionResponse · `feature` `p2: medium` `eval`

**Overview**

Add `Usage` to `CompletionResponse` and parse OpenRouter's `usage` field from the API
response. Aggregate per-run totals (tokens + USD cost) into `Metadata`.

**Changes**

`internal/council/types.go` — add to `CompletionResponse`:

```go
type Usage struct {
    PromptTokens     int     `json:"prompt_tokens"`
    CompletionTokens int     `json:"completion_tokens"`
    CostUSD          float64 `json:"cost_usd,omitempty"`
}
```

`internal/council/types.go` — extend `Metadata`:

```go
TotalTokens  int     `json:"total_tokens"`
TotalCostUSD float64 `json:"total_cost_usd"`
```

**Acceptance criteria**

- [ ] `result.metadata.total_cost_usd` appears in eval output
- [ ] `result.metadata.total_tokens` is sum of all LLM calls in the run
- [ ] Existing tests pass (backward-compatible JSON)
- [ ] `CostUSD` is omitted (zero-value) when OpenRouter doesn't return it

**Files**

- Modified: `internal/council/types.go`, `internal/openrouter/client.go`, `internal/openrouter/client_test.go`, `internal/council/runner_test.go`

**Note:** Can be implemented in parallel with #142 and #145.

---

### #145 · feat(eval): meta-judge scoring package · `feature` `p1: high` `eval`

**Overview**

Add `internal/eval/judge.go` — a package that uses a separate LLM (different from council
members) to score the council's final answer on a 0–10 scale across three criteria.

**Design**

```go
type JudgeResult struct {
    Score     float64            `json:"score"`     // 0–10, mean of criteria
    Criteria  map[string]float64 `json:"criteria"`  // correctness, completeness, clarity
    Rationale string             `json:"rationale"`
}

func Score(ctx context.Context, client LLMClient, item BenchmarkItem, finalAnswer string) (JudgeResult, error)
```

Scoring prompt instructs the judge (`temperature=0`) to evaluate:
- **Correctness** (0–10): factual accuracy / code correctness
- **Completeness** (0–10): all parts of the question addressed
- **Clarity** (0–10): structure, readability

Judge model: configurable via `EVAL_JUDGE_MODEL` env var (default:
`google/gemini-2.5-flash-lite`).

**Acceptance criteria**

- [ ] `judge.Score(ctx, client, item, answer)` returns valid `JudgeResult`
- [ ] Uses `temperature: 0` for determinism
- [ ] Returns structured JSON from LLM (JSON mode)
- [ ] `internal/eval/judge_test.go` covers happy path + LLM error with mock `LLMClient`
- [ ] Does NOT call real OpenRouter in tests

**Files**

- New: `internal/eval/judge.go`, `internal/eval/judge_test.go`

**Depends on:** #142

---

### #146 · feat(eval): batch benchmark runner + aggregate report · `feature` `p1: high` `eval`

**Overview**

Extend `cmd/eval/main.go` with a `--benchmark` mode that runs all questions from a YAML
file sequentially and writes a structured JSON report to `runs/<timestamp>/report.json`.

**Report schema**

```json
{
  "run_id": "2026-04-25T14:00:00Z",
  "council_type": "default",
  "mean_score": 7.4,
  "std_dev": 1.2,
  "mean_consensus_w": 0.61,
  "mean_latency_ms": 12400,
  "total_cost_usd": 0.38,
  "total_tokens": 142000,
  "by_category": {
    "code":     { "mean_score": 6.9, "n": 5 },
    "analysis": { "mean_score": 7.8, "n": 5 },
    "factual":  { "mean_score": 8.1, "n": 5 },
    "creative": { "mean_score": 6.7, "n": 5 }
  },
  "questions": [
    {
      "id": "code-001",
      "score": 7.0,
      "judge_rationale": "...",
      "consensus_w": 0.58,
      "latency_ms": 11200,
      "cost_usd": 0.019
    }
  ]
}
```

Rate limiting: 1-second pause between questions to avoid OpenRouter 429s.

**Acceptance criteria**

- [ ] `make eval-batch BENCHMARK=eval/benchmarks/baseline.yaml` runs all 20 questions
- [ ] Creates `runs/<timestamp>/report.json` with full schema
- [ ] Prints progress to stderr: `[3/20] code-003 score=7.0 (12.4s)`
- [ ] Handles individual question failure gracefully (logs error, continues)
- [ ] Respects `EVAL_MAX_COST_USD` env var (default `1.0`) — aborts if exceeded

**Files**

- Modified: `cmd/eval/main.go` (add `--benchmark` mode)
- New: `internal/eval/report.go` (report struct + serialisation)

**Depends on:** #141, #142, #145

---

### #147 · feat(eval): Claude Code skill /improve-council · `feature` `p2: medium` `eval`

**Overview**

Add a Claude Code skill `.claude/skills/improve-council.md` that analyzes an eval report,
diffs it against the baseline, identifies the weakest category, and proposes a single
targeted config change.

**Skill workflow**

1. Read `runs/latest/report.json` (symlink updated after each run)
2. Read `runs/baseline/report.json` for comparison
3. Identify: weakest category by mean score, largest score drop vs baseline
4. Propose **one** change — options:
   - Swap a council member (show which model, with what replacement)
   - Change the chairman model
   - Tweak Stage 2 or Stage 3 prompt in `internal/council/prompts.go`
   - Adjust temperature in the config YAML
5. Output the proposal as a minimal YAML diff or unified diff
6. Record reasoning in `runs/decisions.jsonl`

**Acceptance criteria**

- [ ] Skill file is valid Claude Code skill markdown
- [ ] Running `/improve-council` after a benchmark outputs a concrete, actionable proposal
- [ ] Proposal references specific model IDs or file:line for prompt changes
- [ ] Does NOT make changes itself — outputs a diff for the iterate script to apply

**Files**

- New: `.claude/skills/improve-council.md`

**Depends on:** #146

---

### #148 · feat(eval): run-compare-promote iteration workflow · `feature` `p2: medium` `eval`

**Overview**

Add `make eval-iterate` — a Makefile target backed by `scripts/eval-iterate.sh` that runs
a full A/B cycle: baseline run → Claude skill proposal → apply change → candidate run →
compare → promote or revert.

**Workflow**

```
make eval-iterate
  1. Run benchmark with current config  →  runs/<ts_A>/report.json
  2. Update runs/latest symlink → runs/<ts_A>/
  3. Invoke /improve-council skill (claude --skill improve-council)
  4. Apply the proposed diff to configs/ or prompts.go
  5. Run benchmark again              →  runs/<ts_B>/report.json
  6. Compare: B.mean_score > A.mean_score + 0.1 (significance threshold)?
       YES → promote: copy configs to baseline, update runs/baseline symlink
       NO  → revert: git checkout the changed files
  7. Append decision to runs/decisions.jsonl
```

`runs/decisions.jsonl` entry schema:

```json
{ "ts": "...", "from_score": 7.1, "to_score": 7.6, "change": "...", "decision": "promote" }
```

**Acceptance criteria**

- [ ] `make eval-iterate` completes a full cycle end-to-end
- [ ] Creates two timestamped run dirs
- [ ] Appends one entry to `runs/decisions.jsonl`
- [ ] On revert, working tree is clean after (no leftover changes)
- [ ] `EVAL_SIGNIFICANCE` env var controls the promotion threshold (default `0.1`)

**Files**

- New: `scripts/eval-iterate.sh`
- Modified: `Makefile` — add `eval-iterate` target

**Depends on:** #146, #147

---

### #149 · feat(eval): regression archive and revert mechanism · `feature` `p3: low` `eval`

**Overview**

After each successful promotion, archive the winning config and its report so any past
state can be restored without relying on git history.

**Design**

- On promote: copy config YAML to `configs/archive/<timestamp>/` alongside a copy of `report.json`
- Keep only the top 5 by `mean_score` (prune oldest when limit exceeded)
- `make eval-revert TAG=2026-04-25T14-00-00` restores config from archive

**Acceptance criteria**

- [ ] After a promotion, `configs/archive/<ts>/` exists with config + report
- [ ] `make eval-revert TAG=<ts>` restores baseline config from that archive entry
- [ ] Archive pruning keeps ≤ 5 entries, removes oldest when over limit
- [ ] `configs/archive/` is in `.gitignore`

**Files**

- New: `scripts/eval-revert.sh`
- Modified: `scripts/eval-iterate.sh` (add archive step), `configs/.gitignore`

**Depends on:** #148

---

### #150 · docs(eval): evaluation harness documentation · `docs` `p3: low` `eval`

**Overview**

Write `docs/eval.md` explaining the full evaluation workflow: how to add benchmark
questions, run a benchmark, read the report, and use `/improve-council` for optimization.

**Contents**

- Prerequisites (env vars: `EVAL_JUDGE_MODEL`, `EVAL_MAX_COST_USD`)
- Adding questions to `eval/benchmarks/baseline.yaml`
- Running a single question: `make eval-single Q="..."`
- Running a full benchmark: `make eval-batch`
- Reading `runs/<ts>/report.json` — field descriptions
- Running the iteration loop: `make eval-iterate`
- Reading `runs/decisions.jsonl`
- Reverting to a past config: `make eval-revert TAG=<ts>`
- Example report (`docs/eval-example.json`)

**Acceptance criteria**

- [ ] `docs/eval.md` exists with all sections above
- [ ] All commands shown are working (verified manually)
- [ ] `README.md` links to `docs/eval.md` under a new "Evaluation" section
- [ ] `docs/eval-example.json` included

**Files**

- New: `docs/eval.md`, `docs/eval-example.json`
- Modified: `README.md`

**Depends on:** #146

---

### #151 · feat(ci): smoke benchmark on pull requests · `feature` `task` `p3: low` `eval`

**Overview**

Add a GitHub Actions workflow that runs a 5-question smoke benchmark on every PR using
free OpenRouter models, then posts the results as a PR comment showing delta vs main.

**Models used (free tier)**

- Rada: `meta-llama/llama-3.3-8b-instruct:free`, `google/gemma-4-31b-it:free`
- Judge: `google/gemini-2.5-flash-lite` (~$0.002 for 5 questions)

**`eval/benchmarks/smoke.yaml`** — 5 questions (1 per category + 1 cross-domain).

**PR comment format**

```
## Eval smoke results

| Metric           | main  | this PR | delta  |
|------------------|-------|---------|--------|
| mean_score       | 7.40  | 7.65    | +0.25  |
| mean_consensus_w | 0.61  | 0.64    | +0.03  |
| total_cost_usd   | 0.002 | 0.002   | 0.000  |
```

**Acceptance criteria**

- [ ] Workflow runs on `pull_request` event
- [ ] Posts a PR comment with the comparison table
- [ ] Uses `AI_PROVIDER_API_KEY` from repo secrets
- [ ] Fails the check if `mean_score` drops by more than 1.0 vs main
- [ ] Skips on PRs that only change `docs/` or `*.md` files

**Files**

- New: `.github/workflows/eval-smoke.yml`, `eval/benchmarks/smoke.yaml`

**Depends on:** #146
