package build

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// directStoreEngines lists the calls in one Go source that build a simulator
// engine or catalog from a YAML profile store: sim.NewEngine (or NewEngine, in
// package sim) and spec.NewCatalog given anything but a literal nil. A store
// holds no Go family, so such an engine stops seeing a product the day its
// profile becomes a family (#2826); the engine of a test is built over the
// shipped catalog instead.
func directStoreEngines(src string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "x_test.go", src, 0)
	if err != nil {
		return nil, err
	}
	var found []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		name := ""
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == "NewEngine" {
				name = "NewEngine"
			}
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok && ((x.Name == "sim" && fn.Sel.Name == "NewEngine") || (x.Name == "spec" && fn.Sel.Name == "NewCatalog")) {
				name = x.Name + "." + fn.Sel.Name
			}
		}
		if name == "" {
			return true
		}
		if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == "nil" {
			return true
		}
		found = append(found, name)
		return true
	})
	return found, nil
}

func TestDirectStoreEngineIsRecognized(t *testing.T) {
	for src, want := range map[string]int{
		"package a\nfunc f() { _ = sim.NewEngine(store, profiles.Templates()) }":      1,
		"package sim\nfunc f() { _ = NewEngine(store, nil) }":                         1,
		"package a\nfunc f() { _, _ = spec.NewCatalog(store, fam) }":                  1,
		"package a\nfunc f() { _ = sim.NewEngine(nil, nil) }":                         0,
		"package a\nfunc f() { _, _ = spec.NewCatalog(nil, fam) }":                    0,
		"package a\nfunc f() { _ = sim.NewEngineOver(cat, nil) }":                     0,
		"package a\nfunc f() { _ = collection.NewCatalog(metrics, nil, nil) }":        0,
		"package a\nfunc f() { _ = shipped.Engine(t); _ = shipped.Catalog(t) }":       0,
		"package a\nfunc f() { _ = sim.NewEngine(store, nil); _ = sim.NewEngine(s) }": 2,
	} {
		got, err := directStoreEngines(src)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != want {
			t.Errorf("%q: found %v, want %d", src, got, want)
		}
	}
}

// TestTestsBuildTheirEngineOverTheShippedCatalog guards the shared test engine
// (#2845). A test that builds an engine from the YAML profile store alone
// serves YAML profiles and no Go family, so the day a profile it names becomes
// a family (#2826) it fails, and the family's slice has to touch it. Every test
// reaches the catalog through sim/simtest/shipped (or, inside package sim,
// shippedEngine), which serves the YAML profiles and every family; the
// conversion of a family then needs no per-test churn.
//
// The exceptions are tests that mean a store of their own: the catalog's own
// tests and a hand-built profile. The list is stale-checked, so an entry whose
// file no longer builds one fails.
func TestTestsBuildTheirEngineOverTheShippedCatalog(t *testing.T) {
	allowed := map[string]string{
		"sim/spec/":                   "the catalog's own tests build catalogs from synthetic stores",
		"sim/identity_tokens_test.go": "a hand-built two-profile store, to hold the identity tokens it renders",
	}
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "dist", ".git", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "sim/profile/") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		calls, perr := directStoreEngines(string(b))
		if perr != nil || len(calls) == 0 {
			return nil
		}
		for prefix := range allowed {
			if rel == prefix || (strings.HasSuffix(prefix, "/") && strings.HasPrefix(rel, prefix)) {
				used[prefix] = true
				return nil
			}
		}
		found = append(found, rel+": "+strings.Join(calls, ", "))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Errorf("tests build an engine from a YAML store, which serves no Go family; use shipped.Engine(t) from sim/simtest/shipped (shippedEngine inside package sim):\n  %s", strings.Join(found, "\n  "))
	}
	for prefix, why := range allowed {
		if !used[prefix] {
			t.Errorf("the allowance for %s (%s) no longer excuses anything; remove it", prefix, why)
		}
	}
}
