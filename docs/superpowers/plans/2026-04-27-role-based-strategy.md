# Role-Based Strategy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `RoleBased` and `RoleBasedReview` deliberation strategies to the server alongside the existing `PeerReview` strategy, exposed via a dedicated `POST /api/conversations/{id}/review` endpoint.

**Architecture:** Each model in `RoleBased` gets a distinct role (e.g., security, logic, simplicity, architecture) and a role-specific system prompt. All roles run in parallel (Stage 1). Stage 2 is skipped (roles are complementary, not competing). The Chairman (Stage 3) synthesises findings. `RoleBasedReview` is `RoleBased` with pre-defined code-review roles baked in — users only supply models and chairman.

**REST separation:** strategies map to distinct endpoints, not to a `council_type` field. `POST /message` → PeerReview. `POST /review` → RoleBasedReview (always). Generic `RoleBased` with user-defined roles is **deferred (YAGNI)** — no endpoint for it in this plan.

**Input format:** `/review` accepts a raw git diff as `content`. Recommended caller flag: `git diff -U8` (8 context lines gives LLM reviewers enough surrounding code for data-flow and logic analysis). Chunking large diffs is the caller's responsibility.

**Tech Stack:** Go 1.26+, `internal/council` package, `sync.WaitGroup` for concurrency, `encoding/json` for structured findings, SSE events via existing `EventFunc` callback.

---

## File Map

| Action | File | What changes |
|---|---|---|
| Modify | `internal/council/types.go` | Add `Role` struct; `Roles []Role` field on `CouncilType`; `RoleBased`, `RoleBasedReview` constants |
| Modify | `internal/council/prompts.go` | Add `BuildRoleStage1Prompt`, `BuildRoleChairmanPrompt` |
| Modify | `internal/council/council.go` | Extract `runPeerReview`; add strategy dispatch in `RunFull`; add `assignRoleLabels` helper |
| Create | `internal/council/rolebased.go` | `runRoleBased`, `runRoleBasedStage1`, `runRoleBasedStage3` |
| Create | `internal/council/rolebased_test.go` | Tests for RoleBased pipeline |
| Create | `internal/council/review_roles.go` | `DefaultReviewRoles`, `NewCodeReviewCouncilType` |
| Modify | `internal/config/config.go` | Add `CodeReviewModels`, `CodeReviewChairmanModel` env vars |
| Modify | `cmd/server/main.go` | Register `"code-review"` council type |
| Modify | `internal/api/handler.go` | Add `handleReview` + `handleReviewStream`; register routes |

---

## Task 1: Extend types — Role struct + new Strategy constants

**Files:**

- Modify: `internal/council/types.go`

- [ ] **Step 1: Write the failing test** (compile-time only — verify new constants and struct exist)

Create `internal/council/types_role_test.go`:

```go
package council

import "testing"

func TestRoleBasedStrategyConstants(t *testing.T) {
 if RoleBased == PeerReview {
  t.Fatal("RoleBased must differ from PeerReview")
 }
 if RoleBasedReview == PeerReview {
  t.Fatal("RoleBasedReview must differ from PeerReview")
 }
 if RoleBased == RoleBasedReview {
  t.Fatal("RoleBased must differ from RoleBasedReview")
 }
}

func TestCouncilTypeHasRoles(t *testing.T) {
 ct := CouncilType{
  Name:     "test",
  Strategy: RoleBased,
  Roles: []Role{
   {Name: "critic", Instruction: "Find bugs."},
  },
 }
 if len(ct.Roles) != 1 {
  t.Fatalf("expected 1 role, got %d", len(ct.Roles))
 }
 if ct.Roles[0].Name != "critic" {
  t.Fatalf("unexpected role name %q", ct.Roles[0].Name)
 }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd /home/val/wrk/projects/vmm-rada/vmm-rada
go test ./internal/council/ -run TestRoleBasedStrategyConstants -v
```

Expected: compile error — `RoleBased`, `RoleBasedReview`, `Role` undefined.

- [ ] **Step 3: Add Role struct and new constants to types.go**

In `internal/council/types.go`, after the existing `PeerReview` constant and before `type CouncilType struct`:

```go
const (
 PeerReview Strategy = iota
 RoleBased
 RoleBasedReview
)

// Role defines a named participant with a specific mandate in a role-based council.
type Role struct {
 Name        string `json:"name"`
 Instruction string `json:"instruction"` // system-level prompt for this role
}
```

Then extend `CouncilType`:

```go
type CouncilType struct {
 Name          string
 Strategy      Strategy
 Models        []string // PeerReview: all council members; RoleBased: assigned to Roles by index mod len
 Roles         []Role   // RoleBased / RoleBasedReview: role definitions with instructions
 ChairmanModel string
 Temperature   float64
 QuorumMin     int // 0 = use formula: max(2, ⌈N/2⌉+1)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/council/ -run TestRoleBasedStrategyConstants -v
go test ./internal/council/ -run TestCouncilTypeHasRoles -v
```

Expected: PASS both.

- [ ] **Step 5: Run full existing test suite to confirm no regressions**

```bash
go test ./... -race
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/council/types.go internal/council/types_role_test.go
git commit -m "feat(council): add Role type and RoleBased/RoleBasedReview strategy constants"
```

---

## Task 2: Prompt builders for role-based pipeline

**Files:**

- Modify: `internal/council/prompts.go`

- [ ] **Step 1: Write failing tests**

Create `internal/council/prompts_role_test.go`:

```go
package council

import (
 "strings"
 "testing"
)

func TestBuildRoleStage1Prompt_ContainsInstruction(t *testing.T) {
 role := Role{Name: "security", Instruction: "Find security vulnerabilities."}
 msgs := BuildRoleStage1Prompt(role, "Review this diff: +foo()")

 if len(msgs) != 2 {
  t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
 }
 if msgs[0].Role != "system" {
  t.Errorf("first message must be system, got %q", msgs[0].Role)
 }
 if !strings.Contains(msgs[0].Content, "Find security vulnerabilities.") {
  t.Errorf("system message must contain role instruction, got: %q", msgs[0].Content)
 }
 if msgs[1].Role != "user" {
  t.Errorf("second message must be user, got %q", msgs[1].Role)
 }
 if !strings.Contains(msgs[1].Content, "+foo()") {
  t.Errorf("user message must contain query, got: %q", msgs[1].Content)
 }
}

func TestBuildRoleChairmanPrompt_ContainsRoleNames(t *testing.T) {
 results := []StageOneResult{
  {Label: "security", Content: `[{"file":"main.go","line":10,"severity":"high","body":"SQL injection"}]`},
  {Label: "logic", Content: `[{"file":"main.go","line":20,"severity":"medium","body":"nil dereference"}]`},
 }
 msgs := BuildRoleChairmanPrompt("Review this diff", results)

 if len(msgs) == 0 {
  t.Fatal("expected at least one message")
 }
 combined := ""
 for _, m := range msgs {
  combined += m.Content
 }
 if !strings.Contains(combined, "security") {
  t.Error("chairman prompt must include role name 'security'")
 }
 if !strings.Contains(combined, "logic") {
  t.Error("chairman prompt must include role name 'logic'")
 }
 if !strings.Contains(combined, "SQL injection") {
  t.Error("chairman prompt must include findings content")
 }
 if !strings.Contains(combined, "Review this diff") {
  t.Error("chairman prompt must include original query")
 }
}

func TestBuildRoleChairmanPrompt_EmptyResults(t *testing.T) {
 msgs := BuildRoleChairmanPrompt("some query", nil)
 if len(msgs) == 0 {
  t.Fatal("must return messages even with empty results")
 }
}
```

- [ ] **Step 2: Run tests to confirm failure**

```bash
go test ./internal/council/ -run "TestBuildRole" -v
```

Expected: compile error — `BuildRoleStage1Prompt`, `BuildRoleChairmanPrompt` undefined.

- [ ] **Step 3: Implement prompt builders in prompts.go**

Add at the end of `internal/council/prompts.go`:

```go
// BuildRoleStage1Prompt returns messages for a role participant.
// The system message carries the role instruction; the user message carries the query.
func BuildRoleStage1Prompt(role Role, query string) []ChatMessage {
 return []ChatMessage{
  {Role: "system", Content: role.Instruction},
  {Role: "user", Content: query},
 }
}

// BuildRoleChairmanPrompt returns messages for the chairman to synthesise role findings.
// Each role's findings appear in a labelled section. The chairman produces the final review.
func BuildRoleChairmanPrompt(query string, results []StageOneResult) []ChatMessage {
 var sb strings.Builder
 sb.WriteString("You are the lead reviewer. Synthesise the findings below into a clear, ")
 sb.WriteString("prioritised review. Remove duplicates. Group by file. Order by severity ")
 sb.WriteString("(critical → high → medium → low). Note which role(s) flagged each issue.\n\n")
 sb.WriteString("ORIGINAL QUERY:\n")
 sb.WriteString(query)
 sb.WriteString("\n\n")

 for _, r := range results {
  sb.WriteString("=== ")
  sb.WriteString(strings.ToUpper(r.Label))
  sb.WriteString(" REVIEWER FINDINGS ===\n")
  sb.WriteString(r.Content)
  sb.WriteString("\n\n")
 }

 sb.WriteString("Write your synthesised review in Markdown. ")
 sb.WriteString("If there are no findings across all reviewers, state that explicitly.")

 return []ChatMessage{
  {Role: "user", Content: sb.String()},
 }
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/council/ -run "TestBuildRole" -v
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Run full suite**

```bash
go test ./... -race
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/council/prompts.go internal/council/prompts_role_test.go
git commit -m "feat(council): add BuildRoleStage1Prompt and BuildRoleChairmanPrompt"
```

---

## Task 3: Extract PeerReview pipeline from RunFull + add strategy dispatch

**Files:**

- Modify: `internal/council/council.go`

- [ ] **Step 1: Confirm existing tests pass before any change**

```bash
go test ./internal/council/ -race -v 2>&1 | tail -20
```

Expected: all pass. Note the count — it must stay the same after this task.

- [ ] **Step 2: Extract current RunFull body into runPeerReview**

In `internal/council/council.go`, rename the body of the current `RunFull` to a new private method and replace `RunFull` with a strategy dispatcher:

```go
// RunFull dispatches to the pipeline implementation for the council type's strategy.
func (c *Council) RunFull(ctx context.Context, query string, councilTypeName string, onEvent EventFunc) error {
 ct, ok := c.registry[councilTypeName]
 if !ok {
  return fmt.Errorf("unknown council type %q", councilTypeName)
 }
 switch ct.Strategy {
 case PeerReview:
  return c.runPeerReview(ctx, query, ct, onEvent)
 default:
  return fmt.Errorf("strategy %d not implemented", ct.Strategy)
 }
}

// runPeerReview runs the Karpathy-style 3-stage peer review pipeline.
func (c *Council) runPeerReview(ctx context.Context, query string, ct CouncilType, onEvent EventFunc) error {
 // ... PASTE HERE the exact current body of RunFull, starting from line 67
 // (everything after the registry lookup, which is now in RunFull above)
}
```

Note: The existing body of `RunFull` already looks up `ct` from the registry. In `runPeerReview` we receive `ct` directly, so remove the registry lookup at the top of the moved code.

- [ ] **Step 3: Run full test suite**

```bash
go test ./... -race
```

Expected: same count of passing tests as before. Zero failures.

- [ ] **Step 4: Commit**

```bash
git add internal/council/council.go
git commit -m "refactor(council): extract runPeerReview; RunFull dispatches by strategy"
```

---

## Task 4: Implement RoleBased pipeline (rolebased.go)

**Files:**

- Create: `internal/council/rolebased.go`

- [ ] **Step 1: Write failing tests**

Create `internal/council/rolebased_test.go`:

```go
package council

import (
 "context"
 "errors"
 "sync"
 "testing"
)

// roleCouncilFixture returns a Council wired for RoleBased strategy with 2 roles.
func roleCouncilFixture(complete func(ctx context.Context, req CompletionRequest) (CompletionResponse, error)) *Council {
 registry := map[string]CouncilType{
  "roles": {
   Name:          "roles",
   Strategy:      RoleBased,
   Models:        []string{"model-a", "model-b"},
   ChairmanModel: "chairman",
   Temperature:   0.7,
   Roles: []Role{
    {Name: "security", Instruction: "Find security issues."},
    {Name: "logic", Instruction: "Find logic errors."},
   },
  },
 }
 return NewCouncil(&mockLLMClient{complete: complete}, registry, noopLogger())
}

func TestRunRoleBased_Stage1_ParallelRoles(t *testing.T) {
 var mu sync.Mutex
 called := map[string]int{}

 c := roleCouncilFixture(func(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
  // Identify which model was called by the model field
  mu.Lock()
  called[req.Model]++
  mu.Unlock()
  return makeResponse(`[{"file":"a.go","line":1,"severity":"low","body":"ok"}]`), nil
 })

 var events []string
 err := c.RunFull(context.Background(), "diff here", "roles", func(eventType string, _ any) {
  events = append(events, eventType)
 })
 if err != nil {
  t.Fatalf("unexpected error: %v", err)
 }
 // 2 role models + 1 chairman = 3 calls total
 total := 0
 for _, n := range called {
  total += n
 }
 if total != 3 {
  t.Errorf("expected 3 LLM calls (2 roles + chairman), got %d", total)
 }
}

func TestRunRoleBased_EmitsAllThreeEvents(t *testing.T) {
 c := roleCouncilFixture(func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
  return makeResponse("findings"), nil
 })

 var events []string
 err := c.RunFull(context.Background(), "query", "roles", func(eventType string, _ any) {
  events = append(events, eventType)
 })
 if err != nil {
  t.Fatalf("unexpected error: %v", err)
 }

 want := []string{"stage1_complete", "stage2_complete", "stage3_complete"}
 if len(events) != len(want) {
  t.Fatalf("expected events %v, got %v", want, events)
 }
 for i, e := range want {
  if events[i] != e {
   t.Errorf("event[%d]: want %q, got %q", i, e, events[i])
  }
 }
}

func TestRunRoleBased_QuorumFailure_ReturnsError(t *testing.T) {
 c := roleCouncilFixture(func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
  if req.Model != "chairman" {
   return CompletionResponse{}, errors.New("role model down")
  }
  return makeResponse("ok"), nil
 })

 err := c.RunFull(context.Background(), "query", "roles", func(string, any) {})
 if err == nil {
  t.Fatal("expected quorum error, got nil")
 }
 var qe *QuorumError
 if !errors.As(err, &qe) {
  t.Errorf("expected *QuorumError, got %T: %v", err, err)
 }
}

func TestRunRoleBased_Stage1UsesRoleInstructions(t *testing.T) {
 var systemPrompts []string
 var mu sync.Mutex

 c := roleCouncilFixture(func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
  for _, m := range req.Messages {
   if m.Role == "system" {
    mu.Lock()
    systemPrompts = append(systemPrompts, m.Content)
    mu.Unlock()
   }
  }
  return makeResponse("[]"), nil
 })

 _ = c.RunFull(context.Background(), "query", "roles", func(string, any) {})

 if len(systemPrompts) < 2 {
  t.Fatalf("expected at least 2 system prompts (one per role), got %d", len(systemPrompts))
 }
 found := map[string]bool{}
 for _, p := range systemPrompts {
  if p == "Find security issues." {
   found["security"] = true
  }
  if p == "Find logic errors." {
   found["logic"] = true
  }
 }
 if !found["security"] {
  t.Error("security role instruction not sent to any model")
 }
 if !found["logic"] {
  t.Error("logic role instruction not sent to any model")
 }
}

func TestRunRoleBased_Stage2CompleteData_HasLabelToModel(t *testing.T) {
 c := roleCouncilFixture(func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
  return makeResponse("[]"), nil
 })

 var stage2Data any
 _ = c.RunFull(context.Background(), "query", "roles", func(eventType string, data any) {
  if eventType == "stage2_complete" {
   stage2Data = data
  }
 })

 d, ok := stage2Data.(Stage2CompleteData)
 if !ok {
  t.Fatalf("stage2_complete data must be Stage2CompleteData, got %T", stage2Data)
 }
 if d.Metadata.LabelToModel == nil {
  t.Fatal("LabelToModel must not be nil")
 }
 if _, ok := d.Metadata.LabelToModel["security"]; !ok {
  t.Error("LabelToModel must contain 'security' role")
 }
}

func TestRunRoleBased_ModelsAssignedByIndex(t *testing.T) {
 // 3 roles, 2 models → models assigned: role0→model-a, role1→model-b, role2→model-a
 registry := map[string]CouncilType{
  "three-roles": {
   Name:          "three-roles",
   Strategy:      RoleBased,
   Models:        []string{"model-a", "model-b"},
   ChairmanModel: "chairman",
   Temperature:   0.7,
   Roles: []Role{
    {Name: "r0", Instruction: "Role 0"},
    {Name: "r1", Instruction: "Role 1"},
    {Name: "r2", Instruction: "Role 2"},
   },
  },
 }
 var mu sync.Mutex
 modelUsed := map[string]string{} // role label → model used (from first user msg context)

 c := NewCouncil(&mockLLMClient{
  complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
   // Find which role by system instruction
   for _, m := range req.Messages {
    if m.Role == "system" {
     mu.Lock()
     modelUsed[m.Content] = req.Model
     mu.Unlock()
    }
   }
   return makeResponse("[]"), nil
  },
 }, registry, noopLogger())

 _ = c.RunFull(context.Background(), "q", "three-roles", func(string, any) {})

 if modelUsed["Role 0"] != "model-a" {
  t.Errorf("role 0 should use model-a, got %q", modelUsed["Role 0"])
 }
 if modelUsed["Role 1"] != "model-b" {
  t.Errorf("role 1 should use model-b, got %q", modelUsed["Role 1"])
 }
 if modelUsed["Role 2"] != "model-a" {
  t.Errorf("role 2 should cycle back to model-a, got %q", modelUsed["Role 2"])
 }
}
```

- [ ] **Step 2: Run tests to confirm failure**

```bash
go test ./internal/council/ -run "TestRunRoleBased" -v
```

Expected: compile error — `rolebased.go` not yet created; also `noopLogger` may be missing.

- [ ] **Step 3: Check if noopLogger helper exists in tests**

```bash
grep -n "noopLogger" /home/val/wrk/projects/vmm-rada/vmm-rada/internal/council/*.go
```

If not found, add it to `internal/council/council_test.go` (or a new `testhelpers_test.go`):

```go
import "log/slog"

func noopLogger() *slog.Logger {
 return slog.New(slog.NewTextHandler(io.Discard, nil))
}
```

(Add `"io"` to imports.)

- [ ] **Step 4: Implement rolebased.go**

Create `internal/council/rolebased.go`:

```go
package council

import (
 "context"
 "fmt"
 "sync"
 "time"
)

// runRoleBased executes the role-based 2-stage pipeline (Stage 1 + Stage 3).
// Stage 2 is skipped; a minimal Stage2CompleteData event is emitted for SSE compatibility.
func (c *Council) runRoleBased(ctx context.Context, query string, ct CouncilType, onEvent EventFunc) error {
 if len(ct.Roles) == 0 {
  return fmt.Errorf("council type %q has no roles configured", ct.Name)
 }
 if len(ct.Models) == 0 {
  return fmt.Errorf("council type %q has no models configured", ct.Name)
 }

 // Stage 1: parallel role execution.
 stage1 := c.runRoleBasedStage1(ctx, query, ct)

 successful, err := checkQuorum(stage1, ct.QuorumMin)
 if err != nil {
  return err
 }

 // Build labelToModel map (role name → model used).
 labelToModel := make(map[string]string, len(successful))
 for _, r := range successful {
  labelToModel[r.Label] = r.Model
 }

 onEvent("stage1_complete", successful)

 // Stage 2: skipped for role-based strategies.
 // Emit a minimal Stage2CompleteData so SSE clients receive the expected event.
 meta := Metadata{
  CouncilType:  ct.Name,
  LabelToModel: labelToModel,
  ConsensusW:   1.0, // roles are complementary, not competing
 }
 onEvent("stage2_complete", Stage2CompleteData{Results: nil, Metadata: meta})

 // Stage 3: chairman synthesis.
 stage3, err := c.runRoleBasedStage3(ctx, query, successful, ct.ChairmanModel, ct.Temperature)
 if err != nil {
  return err
 }
 onEvent("stage3_complete", stage3)
 return nil
}

// runRoleBasedStage1 executes all roles concurrently.
// Model assignment: ct.Models[i % len(ct.Models)].
func (c *Council) runRoleBasedStage1(ctx context.Context, query string, ct CouncilType) []StageOneResult {
 results := make([]StageOneResult, len(ct.Roles))
 var wg sync.WaitGroup

 for i, role := range ct.Roles {
  wg.Add(1)
  go func(idx int, r Role) {
   defer wg.Done()
   model := ct.Models[idx%len(ct.Models)]
   start := time.Now()

   msgs := BuildRoleStage1Prompt(r, query)
   resp, err := c.client.Complete(ctx, CompletionRequest{
    Model:       model,
    Messages:    msgs,
    Temperature: ct.Temperature,
   })

   result := StageOneResult{
    Label:      r.Name,
    Model:      model,
    DurationMs: time.Since(start).Milliseconds(),
   }
   if err != nil {
    result.Error = err
   } else if len(resp.Choices) == 0 {
    result.Error = fmt.Errorf("role %q: empty response from %s", r.Name, model)
   } else {
    result.Content = resp.Choices[0].Message.Content
   }
   results[idx] = result
  }(i, role)
 }
 wg.Wait()
 return results
}

// runRoleBasedStage3 asks the chairman to synthesise all role findings.
func (c *Council) runRoleBasedStage3(ctx context.Context, query string, roleResults []StageOneResult, chairmanModel string, temperature float64) (StageThreeResult, error) {
 start := time.Now()
 msgs := BuildRoleChairmanPrompt(query, roleResults)

 resp, err := c.client.Complete(ctx, CompletionRequest{
  Model:       chairmanModel,
  Messages:    msgs,
  Temperature: temperature,
 })

 result := StageThreeResult{
  Model:      chairmanModel,
  DurationMs: time.Since(start).Milliseconds(),
 }
 if err != nil {
  result.Error = err
  return result, fmt.Errorf("role-based chairman (%s): %w", chairmanModel, err)
 }
 if len(resp.Choices) == 0 {
  result.Error = fmt.Errorf("empty response from chairman %s", chairmanModel)
  return result, result.Error
 }
 result.Content = resp.Choices[0].Message.Content
 return result, nil
}
```

- [ ] **Step 5: Wire up strategy dispatch in RunFull**

In `internal/council/council.go`, update the `switch` in `RunFull`:

```go
switch ct.Strategy {
case PeerReview:
    return c.runPeerReview(ctx, query, ct, onEvent)
case RoleBased, RoleBasedReview:
    return c.runRoleBased(ctx, query, ct, onEvent)
default:
    return fmt.Errorf("strategy %d not implemented", ct.Strategy)
}
```

- [ ] **Step 6: Run role-based tests**

```bash
go test ./internal/council/ -run "TestRunRoleBased" -v -race
```

Expected: all 6 tests PASS.

- [ ] **Step 7: Run full suite**

```bash
go test ./... -race
```

Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/council/rolebased.go internal/council/rolebased_test.go internal/council/council.go
git commit -m "feat(council): implement RoleBased pipeline (runRoleBased, stage1, stage3)"
```

---

## Task 5: Define default code-review roles (RoleBasedReview)

**Files:**

- Create: `internal/council/review_roles.go`

- [ ] **Step 1: Write failing test**

Create `internal/council/review_roles_test.go`:

```go
package council

import "testing"

func TestDefaultReviewRoles_Count(t *testing.T) {
 if len(DefaultReviewRoles) < 3 {
  t.Fatalf("expected at least 3 default review roles, got %d", len(DefaultReviewRoles))
 }
}

func TestDefaultReviewRoles_UniqueNames(t *testing.T) {
 seen := map[string]bool{}
 for _, r := range DefaultReviewRoles {
  if r.Name == "" {
   t.Error("role has empty name")
  }
  if seen[r.Name] {
   t.Errorf("duplicate role name: %q", r.Name)
  }
  seen[r.Name] = true
 }
}

func TestDefaultReviewRoles_InstructionsNonEmpty(t *testing.T) {
 for _, r := range DefaultReviewRoles {
  if r.Instruction == "" {
   t.Errorf("role %q has empty instruction", r.Name)
  }
 }
}

func TestNewCodeReviewCouncilType_Strategy(t *testing.T) {
 models := []string{"model-a", "model-b", "model-c", "model-d"}
 chairman := "chairman-model"
 ct := NewCodeReviewCouncilType(models, chairman, 0.7)

 if ct.Strategy != RoleBasedReview {
  t.Errorf("expected RoleBasedReview strategy, got %d", ct.Strategy)
 }
 if ct.Name != "code-review" {
  t.Errorf("expected name 'code-review', got %q", ct.Name)
 }
 if len(ct.Roles) != len(DefaultReviewRoles) {
  t.Errorf("expected %d roles, got %d", len(DefaultReviewRoles), len(ct.Roles))
 }
 for i, r := range ct.Roles {
  if r.Name != DefaultReviewRoles[i].Name {
   t.Errorf("role[%d]: expected %q, got %q", i, DefaultReviewRoles[i].Name, r.Name)
  }
 }
}
```

- [ ] **Step 2: Run tests to confirm failure**

```bash
go test ./internal/council/ -run "TestDefaultReviewRoles|TestNewCodeReview" -v
```

Expected: compile error — `DefaultReviewRoles`, `NewCodeReviewCouncilType` undefined.

- [ ] **Step 3: Implement review_roles.go**

Create `internal/council/review_roles.go`:

```go
package council

// DefaultReviewRoles are the four specialised roles used by the RoleBasedReview strategy.
// Each role independently reviews a code diff for a specific class of issues.
var DefaultReviewRoles = []Role{
 {
  Name: "security",
  Instruction: `You are a security code reviewer. Analyse the code diff for security vulnerabilities.
Focus on: OWASP Top 10, authentication/authorisation flaws, input validation, SQL/command injection,
hardcoded secrets, insecure dependencies, cryptography misuse, and unsafe API usage.
Return ONLY a JSON array of findings. Each finding: {"file":"...","line":N,"severity":"critical|high|medium|low","body":"..."}.
If no issues found, return an empty array: []`,
 },
 {
  Name: "logic",
  Instruction: `You are a logic and correctness reviewer. Analyse the code diff for logical errors.
Focus on: edge cases, nil/null pointer dereferences, off-by-one errors, race conditions,
incorrect error propagation, wrong algorithm assumptions, and missing bounds checks.
Return ONLY a JSON array of findings. Each finding: {"file":"...","line":N,"severity":"high|medium|low","body":"..."}.
If no issues found, return an empty array: []`,
 },
 {
  Name: "simplicity",
  Instruction: `You are a code quality reviewer. Analyse the code diff for unnecessary complexity and poor readability.
Focus on: code duplication (DRY violations), overly complex logic (KISS violations),
premature abstraction (YAGNI violations), poor naming, and missing or misleading comments.
Return ONLY a JSON array of findings. Each finding: {"file":"...","line":N,"severity":"medium|low","body":"..."}.
If no issues found, return an empty array: []`,
 },
 {
  Name: "architecture",
  Instruction: `You are an architecture reviewer. Analyse the code diff for design and structural problems.
Focus on: layer boundary violations, dependency direction issues, tight coupling, low cohesion,
interface design problems, missing abstractions, and SOLID principle violations.
Return ONLY a JSON array of findings. Each finding: {"file":"...","line":N,"severity":"high|medium|low","body":"..."}.
If no issues found, return an empty array: []`,
 },
}

// NewCodeReviewCouncilType returns a CouncilType configured for RoleBasedReview.
// models are assigned to roles by index (models[i % len(models)]).
// Pass at least 1 model; passing 4 models assigns one per role.
func NewCodeReviewCouncilType(models []string, chairmanModel string, temperature float64) CouncilType {
 return CouncilType{
  Name:          "code-review",
  Strategy:      RoleBasedReview,
  Models:        models,
  Roles:         DefaultReviewRoles,
  ChairmanModel: chairmanModel,
  Temperature:   temperature,
 }
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/council/ -run "TestDefaultReviewRoles|TestNewCodeReview" -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Run full suite**

```bash
go test ./... -race
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/council/review_roles.go internal/council/review_roles_test.go
git commit -m "feat(council): add DefaultReviewRoles and NewCodeReviewCouncilType for RoleBasedReview"
```

---

## Task 6: Wire up config and register code-review council type

**Files:**

- Modify: `internal/config/config.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add env vars to config.go**

In `internal/config/config.go`, extend the `Config` struct:

```go
type Config struct {
 // ... existing fields ...

 // Code-review council (RoleBasedReview strategy).
 // CODE_REVIEW_MODELS: comma-separated model IDs for the 4 reviewer roles.
 // Defaults to same models as DefaultCouncilModels.
 CodeReviewModels        []string
 // CODE_REVIEW_CHAIRMAN_MODEL: model for review synthesis.
 // Defaults to DefaultCouncilChairmanModel.
 CodeReviewChairmanModel string
}
```

In the `Load()` function, after the existing model loading, add:

```go
// Code-review models (optional; defaults to council models).
if raw := os.Getenv("CODE_REVIEW_MODELS"); raw != "" {
    cfg.CodeReviewModels = splitTrimmed(raw)
} else {
    cfg.CodeReviewModels = cfg.DefaultCouncilModels
}

// Code-review chairman (optional; defaults to council chairman).
if v := os.Getenv("CODE_REVIEW_CHAIRMAN_MODEL"); v != "" {
    cfg.CodeReviewChairmanModel = v
} else {
    cfg.CodeReviewChairmanModel = cfg.DefaultCouncilChairmanModel
}
```

Where `splitTrimmed` is the existing helper that splits and trims comma-separated strings. If it doesn't exist by that name, look at how `COUNCIL_MODELS` is parsed and replicate the pattern.

- [ ] **Step 2: Register code-review council type in main.go**

In `cmd/server/main.go`, after the existing registry entry, add:

```go
registry := map[string]council.CouncilType{
    cfg.DefaultCouncilType: {
        Name:          cfg.DefaultCouncilType,
        Strategy:      council.PeerReview,
        Models:        cfg.DefaultCouncilModels,
        ChairmanModel: cfg.DefaultCouncilChairmanModel,
        Temperature:   cfg.DefaultCouncilTemperature,
    },
    "code-review": council.NewCodeReviewCouncilType(
        cfg.CodeReviewModels,
        cfg.CodeReviewChairmanModel,
        cfg.DefaultCouncilTemperature,
    ),
}
```

- [ ] **Step 3: Build to verify no compile errors**

```bash
cd /home/val/wrk/projects/vmm-rada/vmm-rada
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Run full suite**

```bash
go test ./... -race
```

Expected: all pass.

- [ ] **Step 5: Update .env.example**

In `.env.example`, add after the existing council variables:

```
# Code-review council (RoleBasedReview strategy)
# Comma-separated models assigned to roles: security, logic, simplicity, architecture
# Defaults to COUNCIL_MODELS if not set.
# CODE_REVIEW_MODELS=openai/gpt-4o-mini,anthropic/claude-haiku-4-5,google/gemini-flash-1.5,openai/gpt-4o-mini

# Model for code-review synthesis (chairman). Defaults to CHAIRMAN_MODEL if not set.
# CODE_REVIEW_CHAIRMAN_MODEL=anthropic/claude-sonnet-4-5
```

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go cmd/server/main.go .env.example
git commit -m "feat(server): register code-review council type with RoleBasedReview strategy"
```

---

## Task 7: Integration test — full RoleBasedReview pipeline via RunFull

**Files:**

- Modify: `internal/council/rolebased_test.go` (add integration tests)

- [ ] **Step 1: Add integration tests**

Append to `internal/council/rolebased_test.go`:

```go
func TestRunRoleBasedReview_FullPipeline_EmitsAllEvents(t *testing.T) {
 registry := map[string]CouncilType{
  "code-review": NewCodeReviewCouncilType(
   []string{"model-a", "model-b", "model-c", "model-d"},
   "chairman",
   0.7,
  ),
 }
 c := NewCouncil(&mockLLMClient{
  complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
   if req.Model == "chairman" {
    return makeResponse("## Review\n\nNo critical issues."), nil
   }
   return makeResponse(`[{"file":"main.go","line":1,"severity":"low","body":"ok"}]`), nil
  },
 }, registry, noopLogger())

 var events []string
 var stage3Content string
 err := c.RunFull(context.Background(), "git diff HEAD...", "code-review", func(eventType string, data any) {
  events = append(events, eventType)
  if eventType == "stage3_complete" {
   if r, ok := data.(StageThreeResult); ok {
    stage3Content = r.Content
   }
  }
 })
 if err != nil {
  t.Fatalf("unexpected error: %v", err)
 }

 want := []string{"stage1_complete", "stage2_complete", "stage3_complete"}
 if len(events) != len(want) {
  t.Fatalf("expected events %v, got %v", want, events)
 }
 for i, e := range want {
  if events[i] != e {
   t.Errorf("event[%d]: want %q, got %q", i, e, events[i])
  }
 }
 if stage3Content == "" {
  t.Error("stage3 content must not be empty")
 }
}

func TestRunRoleBasedReview_Stage1HasFourRoles(t *testing.T) {
 registry := map[string]CouncilType{
  "code-review": NewCodeReviewCouncilType(
   []string{"model-a"},
   "chairman",
   0.7,
  ),
 }

 var stage1Results any
 c := NewCouncil(&mockLLMClient{
  complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
   return makeResponse("[]"), nil
  },
 }, registry, noopLogger())

 _ = c.RunFull(context.Background(), "diff", "code-review", func(eventType string, data any) {
  if eventType == "stage1_complete" {
   stage1Results = data
  }
 })

 results, ok := stage1Results.([]StageOneResult)
 if !ok {
  t.Fatalf("stage1_complete data must be []StageOneResult, got %T", stage1Results)
 }
 if len(results) != len(DefaultReviewRoles) {
  t.Errorf("expected %d role results, got %d", len(DefaultReviewRoles), len(results))
 }

 labels := map[string]bool{}
 for _, r := range results {
  labels[r.Label] = true
 }
 for _, role := range DefaultReviewRoles {
  if !labels[role.Name] {
   t.Errorf("missing role %q in stage1 results", role.Name)
  }
 }
}

func TestRunRoleBasedReview_ChairmanReceivesAllFindings(t *testing.T) {
 registry := map[string]CouncilType{
  "code-review": NewCodeReviewCouncilType(
   []string{"model-a"},
   "chairman",
   0.7,
  ),
 }

 var chairmanPrompt string
 c := NewCouncil(&mockLLMClient{
  complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
   if req.Model == "chairman" {
    for _, m := range req.Messages {
     chairmanPrompt += m.Content
    }
    return makeResponse("final review"), nil
   }
   return makeResponse(`[{"file":"x.go","line":1,"severity":"high","body":"issue"}]`), nil
  },
 }, registry, noopLogger())

 _ = c.RunFull(context.Background(), "diff", "code-review", func(string, any) {})

 // Chairman prompt must mention all 4 role names
 for _, role := range DefaultReviewRoles {
  if !contains(chairmanPrompt, role.Name) {
   t.Errorf("chairman prompt must mention role %q", role.Name)
  }
 }
}

// contains is a helper used in tests only.
func contains(s, substr string) bool {
 return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
 for i := 0; i <= len(s)-len(substr); i++ {
  if s[i:i+len(substr)] == substr {
   return true
  }
 }
 return false
}
```

Note: replace the `contains` / `containsStr` helpers with `strings.Contains` from the standard library if `strings` is already imported:

```go
import "strings"
// then use: strings.Contains(chairmanPrompt, role.Name)
```

- [ ] **Step 2: Run integration tests**

```bash
go test ./internal/council/ -run "TestRunRoleBasedReview" -v -race
```

Expected: all 3 tests PASS.

- [ ] **Step 3: Run full suite**

```bash
go test ./... -race
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/council/rolebased_test.go
git commit -m "test(council): add RoleBasedReview integration tests"
```

---

## Task 8: POST /review REST endpoint

**Files:**
- Modify: `internal/api/handler.go`

- [ ] **Step 1: Write failing handler test**

Add to `internal/api/handler_test.go` (or create `internal/api/review_handler_test.go`):

```go
func TestHandleReview_Returns200WithStage3Content(t *testing.T) {
	runner := &mockRunner{
		runFull: func(ctx context.Context, query, councilType string, onEvent council.EventFunc) error {
			if councilType != "code-review" {
				return fmt.Errorf("expected council_type=code-review, got %q", councilType)
			}
			onEvent("stage1_complete", []council.StageOneResult{})
			onEvent("stage2_complete", council.Stage2CompleteData{})
			onEvent("stage3_complete", council.StageThreeResult{Content: "## Review\n\nLGTM"})
			return nil
		},
	}
	h := newTestHandler(runner)

	convID := createConversation(t, h)
	body := `{"content": "diff --git a/main.go..."}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/conversations/"+convID+"/review", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var msg council.AssistantMessage
	if err := json.NewDecoder(w.Body).Decode(&msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if msg.Stage3.Content != "## Review\n\nLGTM" {
		t.Errorf("unexpected stage3 content: %q", msg.Stage3.Content)
	}
}

func TestHandleReview_AlwaysUsesCodeReviewCouncilType(t *testing.T) {
	var capturedType string
	runner := &mockRunner{
		runFull: func(_ context.Context, _, councilType string, onEvent council.EventFunc) error {
			capturedType = councilType
			onEvent("stage1_complete", []council.StageOneResult{})
			onEvent("stage2_complete", council.Stage2CompleteData{})
			onEvent("stage3_complete", council.StageThreeResult{Content: "ok"})
			return nil
		},
	}
	h := newTestHandler(runner)
	convID := createConversation(t, h)

	req := httptest.NewRequest(http.MethodPost,
		"/api/conversations/"+convID+"/review",
		strings.NewReader(`{"content":"diff"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if capturedType != "code-review" {
		t.Errorf("expected council type 'code-review', got %q", capturedType)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/api/ -run "TestHandleReview" -v
```

Expected: compile error or 404 — route not registered.

- [ ] **Step 3: Add reviewRequest type and handleReview handler to handler.go**

In `internal/api/handler.go`, add after the existing types:

```go
type reviewRequest struct {
	Content string `json:"content"`
}

func (req reviewRequest) validate() error {
	if strings.TrimSpace(req.Content) == "" {
		return errors.New("content is required")
	}
	return nil
}
```

Add the handler method:

```go
func (h *Handler) handleReview(w http.ResponseWriter, r *http.Request) {
	convID, err := parseConvID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req reviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := req.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.storage.SaveMessage(r.Context(), convID, storage.UserMessage(req.Content)); err != nil {
		http.Error(w, "save message: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var asst council.AssistantMessage
	asst.Role = "assistant"

	err = h.runner.RunFull(r.Context(), req.Content, "code-review", func(eventType string, data any) {
		switch eventType {
		case "stage1_complete":
			if v, ok := data.([]council.StageOneResult); ok {
				asst.Stage1 = v
			}
		case "stage2_complete":
			if v, ok := data.(council.Stage2CompleteData); ok {
				asst.Stage2 = v.Results
				asst.Metadata = v.Metadata
			}
		case "stage3_complete":
			if v, ok := data.(council.StageThreeResult); ok {
				asst.Stage3 = v
			}
		}
	})
	if err != nil {
		http.Error(w, "council error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.storage.SaveMessage(r.Context(), convID, asst); err != nil {
		http.Error(w, "save assistant message: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(asst)
}
```

- [ ] **Step 4: Add handleReviewStream handler**

```go
func (h *Handler) handleReviewStream(w http.ResponseWriter, r *http.Request) {
	convID, err := parseConvID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req reviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := req.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sendSSE := func(eventType string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
		flusher.Flush()
	}

	if err := h.storage.SaveMessage(r.Context(), convID, storage.UserMessage(req.Content)); err != nil {
		sendSSE("error", map[string]string{"message": err.Error()})
		return
	}

	var asst council.AssistantMessage
	asst.Role = "assistant"

	err = h.runner.RunFull(r.Context(), req.Content, "code-review", func(eventType string, data any) {
		switch eventType {
		case "stage1_complete":
			if v, ok := data.([]council.StageOneResult); ok {
				asst.Stage1 = v
			}
		case "stage2_complete":
			if v, ok := data.(council.Stage2CompleteData); ok {
				asst.Stage2 = v.Results
				asst.Metadata = v.Metadata
			}
		case "stage3_complete":
			if v, ok := data.(council.StageThreeResult); ok {
				asst.Stage3 = v
			}
		}
		sendSSE(eventType, data)
	})
	if err != nil {
		sendSSE("error", map[string]string{"message": err.Error()})
		return
	}

	_ = h.storage.SaveMessage(r.Context(), convID, asst)
	sendSSE("complete", map[string]string{"status": "ok"})
}
```

- [ ] **Step 5: Register routes in RegisterRoutes**

In the `RegisterRoutes` function, alongside the existing `/message` routes:

```go
mux.HandleFunc("POST /api/conversations/{id}/review",        h.handleReview)
mux.HandleFunc("POST /api/conversations/{id}/review/stream", h.handleReviewStream)
```

- [ ] **Step 6: Run handler tests**

```bash
go test ./internal/api/ -run "TestHandleReview" -v -race
```

Expected: PASS.

- [ ] **Step 7: Run full suite**

```bash
go test ./... -race
```

Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/api/handler.go internal/api/review_handler_test.go
git commit -m "feat(api): add POST /review and /review/stream endpoints for RoleBasedReview"
```

---

## Verification

After all tasks complete, verify the full feature end-to-end:

**1. Build the server:**

```bash
cd /home/val/wrk/projects/vmm-rada/vmm-rada
go build ./cmd/server/
```

**2. Run all tests with race detector:**

```bash
go test ./... -race -count=1
```

Expected: all pass.

**3. Confirm routes are registered:**

```bash
grep -n "review" internal/api/handler.go | grep HandleFunc
```

Expected: two lines — `/review` and `/review/stream`.

**4. Smoke test via curl (requires server running with valid `OPENROUTER_API_KEY`):**

```bash
go run ./cmd/server/ &

CONV_ID=$(curl -s -X POST http://localhost:8001/api/conversations \
  -H "Content-Type: application/json" | jq -r .id)

# Non-streaming review
curl -s -X POST "http://localhost:8001/api/conversations/$CONV_ID/review" \
  -H "Content-Type: application/json" \
  -d "{\"content\": \"$(git diff -U8 HEAD~1 | head -200)\"}" \
  | jq '.stage3.content'
```

Expected: Markdown code review.

**5. Verify SSE stream for /review:**

```bash
curl -s -N -X POST "http://localhost:8001/api/conversations/$CONV_ID/review/stream" \
  -H "Content-Type: application/json" \
  -d '{"content": "diff here"}' \
  | grep "^event:"
```

Expected: `event: stage1_complete`, `event: stage2_complete`, `event: stage3_complete`, `event: complete`.

---

## Self-Review Checklist

- [x] **Role struct** defined in types.go with Name + Instruction
- [x] **RoleBased + RoleBasedReview** constants added
- [x] **CouncilType.Roles** field added
- [x] **Prompt builders** for role stage1 and chairman
- [x] **RunFull dispatch** handles both new strategies with same runner
- [x] **runRoleBased** parallel stage1 + skipped stage2 + stage3
- [x] **Model assignment** by index mod len(Models)
- [x] **Quorum check** reused unchanged
- [x] **stage2_complete** emitted (SSE compatibility, ConsensusW = 1.0)
- [x] **DefaultReviewRoles** 4 roles with proper instructions
- [x] **NewCodeReviewCouncilType** helper for easy registration
- [x] **Config env vars** CODE_REVIEW_MODELS, CODE_REVIEW_CHAIRMAN_MODEL
- [x] **main.go** registers "code-review" council type
- [x] **.env.example** documents new variables
- [x] **All tests TDD** (failing first, then implement)
- [x] **Race detector** used in all test runs
- [x] **No regressions** — existing PeerReview tests unaffected
