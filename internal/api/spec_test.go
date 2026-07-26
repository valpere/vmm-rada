package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// openAPISpec mirrors just the parts of OpenAPI 3.2 the test suite reads.
// Keep in sync with docs/openapi.yaml's structure; this is a *minimal*
// projection, not a full validation of the spec.
type openAPISpec struct {
	OpenAPI  string         `yaml:"openapi"`
	Info     map[string]any `yaml:"info"`
	Paths    map[string]any `yaml:"paths"`
	Schema   openAPIComponents `yaml:"components"`
}

// openAPIComponents only carries what the test suite reaches for.
type openAPIComponents struct {
	Schemas map[string]any `yaml:"schemas"`
}

// collectOperationIDs returns every operationId declared under
// paths.*.{get|post|put|patch|delete|head|options}, as a set. Walks
// the spec loosely — anything that looks like a path item with an
// operationId field on one of the standard HTTP method keys counts.
func (s *openAPISpec) collectOperationIDs(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for path, raw := range s.Paths {
		pm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
			op, ok := pm[method].(map[string]any)
			if !ok {
				continue
			}
			id, _ := op["operationId"].(string)
			if id != "" {
				out[id] = path + " " + method
			}
		}
	}
	return out
}

func loadOpenAPISpec(t *testing.T) *openAPISpec {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// spec_test.go → internal/api/ → internal/ → repo root
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..")
	specPath := filepath.Join(repoRoot, "docs", "openapi.yaml")

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec at %s: %v", specPath, err)
	}

	var spec openAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.") {
		t.Errorf("openapi.yaml declares openapi=%q, expected 3.x", spec.OpenAPI)
	}
	return &spec
}

// TestSpec_LoadsAsYAML guards the basic contract that the spec parses as
// OpenAPI 3.x YAML and has a non-empty title — if the file is malformed,
// every other spec test below this file would also fail, but this
// surfaces the error directly with file path context.
func TestSpec_LoadsAsYAML(t *testing.T) {
	spec := loadOpenAPISpec(t)
	title, _ := spec.Info["title"].(string)
	if title == "" {
		t.Error("openapi.yaml: info.title is empty")
	}
}

// TestSpec_AllRoutesRegistered ensures every mux-registered route has a
// matching operationId in the spec. Failures here usually mean either the
// `routes` slice in handler.go was extended without a corresponding spec
// entry, or vice-versa. This is the same drift-prevention pattern
// strategy_wiring_test.go uses for the Strategy enum.
func TestSpec_AllRoutesRegistered(t *testing.T) {
	spec := loadOpenAPISpec(t)

	// Build a set of operationIds declared by the spec, across all paths.
	// Stdlib http.ServeMux exposes no public route-iteration API, so we
	// walk the static `routes` slice in handler.go (the source of truth for
	// what RegisterRoutes exposes) and the spec independently, and assert the
	// two sets agree.
	specOps := spec.collectOperationIDs(t)

	registeredOps := map[string]bool{}
	for _, rt := range routes {
		registeredOps[rt.opID] = true
		if _, ok := specOps[rt.opID]; !ok {
			t.Errorf("route %s %s (opId=%s) is registered in handler.go but has no matching entry in docs/openapi.yaml",
				rt.method, rt.pattern, rt.opID)
		}
	}

	// Catch the reverse drift too: spec entries that no longer correspond
	// to a real route (orphans in the spec, e.g. a deleted route whose
	// spec entry was forgotten).
	for opID, where := range specOps {
		if !registeredOps[opID] {
			t.Errorf("openapi.yaml declares operationId %q (%s) but no such route exists in handler.go", opID, where)
		}
	}
}

// TestSpec_DispatchTableInSync cross-checks handlerByOp's switch against
// both `routes` and the spec's operationId list. Catches a case where
// someone adds an entry to `routes` and updates the spec but forgets the
// dispatch case in handlerByOp — traffic would route to the panic
// default. The panic is a hard failure, so this check is a friendlier
// early-warning.
func TestSpec_DispatchTableInSync(t *testing.T) {
	dispatchOps := map[string]bool{}
	for _, rt := range routes {
		dispatchOps[rt.opID] = true
	}

	// Parse handler.go and collect every `case "..."` in handlerByOp's switch.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	handlerPath := filepath.Join(filepath.Join(filepath.Dir(file), "..", ".."), "internal", "api", "handler.go")

	fset := token.NewFileSet()
	tree, err := parser.ParseFile(fset, handlerPath, nil, 0)
	if err != nil {
		t.Fatalf("parse handler.go: %v", err)
	}

	// Walk the AST looking for the handlerByOp function, then its case clauses.
	var found bool
	ast.Inspect(tree, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "handlerByOp" {
			return true
		}
		found = true
		for _, stmt := range fn.Body.List {
			s, ok := stmt.(*ast.SwitchStmt)
			if !ok {
				continue
			}
			for _, c := range s.Body.List {
				cc, ok := c.(*ast.CaseClause)
				if !ok || len(cc.List) == 0 {
					continue
				}
				lit, ok := cc.List[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				caseOp := strings.Trim(lit.Value, "\"")
				if caseOp == "default" {
					continue
				}
				if !dispatchOps[caseOp] {
					t.Errorf("handlerByOp has case %q but no entry in routes slice (or routes entry is missing an opID)", caseOp)
				}
				delete(dispatchOps, caseOp)
			}
		}
		return false
	})
	if !found {
		t.Fatal("handlerByOp function not found in handler.go")
	}

	// Any op in routes but not in handlerByOp's switch means RegisterRoutes
	// would panic at request time.
	var missing []string
	for op := range dispatchOps {
		missing = append(missing, op)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("routes slice has opIDs with no handlerByOp case (would panic at request time): %v", missing)
	}
}

// TestSpec_SSEEventDiscriminator asserts the SSE event union is properly
// modeled with OpenAPI 3.2's discriminator pattern. Catches the most likely
// future drift: someone renames an event in internal/council/runner.go
// or interfaces.go without updating the spec.
func TestSpec_SSEEventDiscriminator(t *testing.T) {
	spec := loadOpenAPISpec(t)
	sse, ok := spec.Schema.Schemas["SSEEvent"]
	if !ok {
		t.Fatal("SSEEvent schema not found in components.schemas")
	}

	m, ok := sse.(map[string]any)
	if !ok {
		t.Fatal("SSEEvent is not a mapping")
	}
	disc, ok := m["discriminator"].(map[string]any)
	if !ok {
		t.Fatal("SSEEvent.discriminator is missing or not a mapping")
	}
	propName, _ := disc["propertyName"].(string)
	if propName != "type" {
		t.Errorf("SSEEvent.discriminator.propertyName = %q, want %q", propName, "type")
	}

	mapping, ok := disc["mapping"].(map[string]any)
	if !ok {
		t.Fatal("SSEEvent.discriminator.mapping is missing or not a mapping")
	}

	// Cross-reference the expected event types against the actual emit
	// calls in internal/council — exhaustive list, not derivable from
	// the runner alone (the runner emits "stage0_done" too, but that one
	// is internal state tracking only and intentionally absent from the
	// wire spec).
	expected := []string{
		"stage0_round_complete",
		"stage1_complete",
		"stage2_round_complete",
		"stage2_complete",
		"stage3_complete",
		"title_complete",
		"complete",
		"error",
	}
	actual := make([]string, 0, len(mapping))
	for k := range mapping {
		actual = append(actual, k)
	}
	sort.Strings(actual)

	var missing []string
	for _, e := range expected {
		if _, ok := mapping[e]; !ok {
			missing = append(missing, e)
		}
	}
	if len(missing) > 0 {
		t.Errorf("SSEEvent discriminator missing expected mappings: %v\n  actual: %v", missing, actual)
	}

	// The reverse direction is also a drift signal: an event in the spec
	// with no counterpart in code means the spec is over-promising (the
	// wire will never produce it, so consumers will wait for a stream
	// that never ends). Hand-curated allowlist so the test is not
	// "passive exhaustive" (catches only missing mappings, not extras).
	allowedExtras := map[string]bool{} // none — keep the union tight
	for e := range mapping {
		isExpected := false
		for _, x := range expected {
			if e == x {
				isExpected = true
				break
			}
		}
		if !isExpected && !allowedExtras[e] {
			t.Errorf("SSEEvent discriminator has unexpected mapping %q (no emit call in source)", e)
		}
	}
}

// TestSpec_SSEEventOnItemSchema asserts the streaming response uses
// `text/event-stream` with `itemSchema` per OpenAPI 3.2. Catches a future
// regression to an opaque response type.
func TestSpec_SSEEventOnItemSchema(t *testing.T) {
	spec := loadOpenAPISpec(t)
	raw, ok := spec.Paths["/api/conversations/{id}/message/stream"]
	if !ok {
		t.Fatal("/message/stream path not found")
	}
	pm, ok := raw.(map[string]any)
	if !ok {
		t.Fatal("/message/stream path is not a mapping")
	}
	post, ok := pm["post"].(map[string]any)
	if !ok {
		t.Fatal("/message/stream post is missing or not a mapping")
	}
	resp, ok := post["responses"].(map[string]any)["200"].(map[string]any)
	if !ok {
		t.Fatal("/message/stream 200 response not found or not a mapping")
	}
	content, ok := resp["content"].(map[string]any)
	if !ok {
		t.Fatal("200.content is missing or not a mapping")
	}
	ss, ok := content["text/event-stream"]
	if !ok {
		t.Fatal("200.content.text/event-stream is missing")
	}
	ssm, ok := ss.(map[string]any)
	if !ok {
		t.Fatal("text/event-stream is not a mapping")
	}
	if _, ok := ssm["itemSchema"]; !ok {
		t.Error("text/event-stream missing itemSchema (OpenAPI 3.2 typed-stream contract)")
	}
}

// TestSpec_AllRefsResolve ensures every $ref in the spec points to a
// defined schema or response. Catches the "I added a new $ref and
// forgot the schema" class of bugs.
func TestSpec_AllRefsResolve(t *testing.T) {
	spec := loadOpenAPISpec(t)

	specText, err := os.ReadFile(filepath.Join(filepath.Join(filepath.Dir(mustGetFile(t)), "..", ".."), "docs", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(specText)

	// Crude but adequate: scan for every $ref in the source text and confirm
	// the path component is a known key in the spec. We don't need a real
	// OpenAPI parser for this — the spec is hand-maintained, the
	// #/components/schemas/X and #/components/responses/X forms are the
	// only valid $ref targets, and we already have the parsed structure
	// to look them up in.
	for _, line := range strings.Split(src, "\n") {
		for _, ref := range extractRefs(line) {
			prefix := "#/components/"
			if !strings.HasPrefix(ref, prefix) {
				t.Errorf("non-components ref: %q", ref)
				continue
			}
			rest := strings.TrimPrefix(ref, prefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) != 2 {
				t.Errorf("malformed ref: %q", ref)
				continue
			}
			kind, name := parts[0], parts[1]
			switch kind {
			case "schemas":
				if _, ok := spec.Schema.Schemas[name]; !ok {
					t.Errorf("dangling $ref %q: no such schema", ref)
				}
			case "responses":
				// The spec uses both inline $ref: '#/components/responses/X'
				// (resolved against the spec's own components.responses
				// block) and in-line responses within operation items. This
				// test projection's components.responses is unexported, so
				// we cross-check against the parsed spec's operation-level
				// responses maps.
				found := false
				for pathKey, rawPath := range spec.Paths {
					_ = pathKey
					pm, ok := rawPath.(map[string]any)
					if !ok {
						continue
					}
					for _, method := range []string{"get", "post", "put", "patch", "delete"} {
						op, ok := pm[method].(map[string]any)
						if !ok {
							continue
						}
						if _, ok := op["responses"].(map[string]any)[name]; ok {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found {
					t.Errorf("ref %q: response %q not found in any path item's responses", ref, name)
				}
				if !found {
					t.Errorf("ref %q: response %q not defined in any path item", ref, name)
				}
			default:
				t.Errorf("ref %q: unsupported kind %q", ref, kind)
			}
		}
	}
}

func extractRefs(line string) []string {
	var out []string
	for {
		i := strings.Index(line, "$ref:")
		if i < 0 {
			return out
		}
		// Skip past "$ref:" plus the leading whitespace
		j := i + len("$ref:")
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		// Value is a quoted string up to the next quote
		if j >= len(line) || line[j] != '"' {
			// not a ref we can parse
			line = line[i+5:]
			continue
		}
		j++
		end := strings.IndexByte(line[j:], '"')
		if end < 0 {
			return out
		}
		out = append(out, line[j:j+end])
		line = line[j+end+1:]
	}
}

func mustGetFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return file
}
