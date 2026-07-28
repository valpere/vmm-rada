package council

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

// registrationExemptions lists Strategy names that are intentionally
// implemented but not registered in internal/config/registry_env.go, with
// the reason. TestAllStrategiesRegisteredOrExempted treats these as
// satisfied without requiring an env-registry registration path. Empty
// today — RoleBased was the only entry and is now registered (see
// registry_env.go's "role-based" registration block).
var registrationExemptions = map[string]string{}

// TestAllStrategiesRegisteredOrExempted fails if a Strategy constant declared
// in types.go has neither a registration path in
// internal/config/registry_env.go nor an explicit entry in
// registrationExemptions. This makes "implemented but silently unreachable"
// strategies structurally impossible to introduce without a deliberate
// decision — the exact gap RoleBased fell into.
//
// Scoped to the env-var fallback registry only (registry_env.go), not the
// YAML-driven registry (registry_yaml.go) — the YAML side has its own
// structural check, internal/config's TestShippedConfigCoversAllStrategies,
// which fails if any council.AllStrategies() value has no entry in
// configs/council.yaml.
func TestAllStrategiesRegisteredOrExempted(t *testing.T) {
	root := projectRoot(t)

	declared := strategyConstNames(t, filepath.Join(root, "internal", "council", "types.go"))
	registered := registeredStrategyNames(t, filepath.Join(root, "internal", "config", "registry_env.go"))

	for _, name := range declared {
		if registered[name] {
			continue
		}
		if reason, ok := registrationExemptions[name]; ok && reason != "" {
			t.Logf("Strategy %q is exempted from registration: %s", name, reason)
			continue
		}
		t.Errorf("Strategy %q has no registration path in internal/config/registry_env.go "+
			"and no entry in registrationExemptions — either register it or add an "+
			"exemption with a reason", name)
	}

	declaredSet := make(map[string]bool, len(declared))
	for _, name := range declared {
		declaredSet[name] = true
	}
	for name := range registrationExemptions {
		if !declaredSet[name] {
			t.Errorf("registrationExemptions has entry %q but no such Strategy constant "+
				"exists in types.go — remove the stale exemption", name)
		}
	}
}

// projectRoot resolves the repository root relative to this test file, so
// the test works regardless of the working directory `go test` is run from.
func projectRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to resolve this test file's path")
	}
	// thisFile: <root>/internal/council/strategy_wiring_test.go
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// strategyConstNames parses the `Strategy = iota` const block in the given
// types.go file and returns the declared constant names, in source order.
// Deriving names from the source of truth (rather than a hand-maintained
// list) means adding an 8th Strategy constant is automatically picked up —
// no separate list to remember to update.
func strategyConstNames(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST || len(gd.Specs) == 0 {
			continue
		}
		first, ok := gd.Specs[0].(*ast.ValueSpec)
		if !ok {
			continue
		}
		typeIdent, ok := first.Type.(*ast.Ident)
		if !ok || typeIdent.Name != "Strategy" {
			continue
		}

		var names []string
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				names = append(names, n.Name)
			}
		}
		return names
	}

	t.Fatalf("no `const (... Strategy = iota ...)` block found in %s", path)
	return nil
}

// registeredStrategyNames parses internal/config/registry_env.go and returns
// the set of strategy names referenced as a `Strategy: council.<Name>` field
// inside a composite literal. Deliberately scoped to the env-fallback
// registry-building code, never internal/council/runner.go's dispatch switch
// — that switch references every strategy by construction, which would make
// this check a tautology.
func registeredStrategyNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	found := make(map[string]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Strategy" {
			return true
		}
		sel, ok := kv.Value.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok || pkgIdent.Name != "council" {
			return true
		}
		found[sel.Sel.Name] = true
		return true
	})
	return found
}
