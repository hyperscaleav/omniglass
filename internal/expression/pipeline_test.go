package expression

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/expr-lang/expr"
	exprast "github.com/expr-lang/expr/ast"
)

// The engine compiles through expr.Compile and never reimplements its
// pipeline (the architect's ruling on #1058): no file of this package
// imports Expr's checker or compiler, and compile calls expr.Compile. conf
// stays imported for the option type and inspect's parse configuration.
func TestCompileGoesThroughExprCompile(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	called := false
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == "github.com/expr-lang/expr/checker" || path == "github.com/expr-lang/expr/compiler" {
				t.Errorf("%s imports %s: compile through expr.Compile, never Expr's pipeline stages", f, path)
			}
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "compile" || fn.Recv != nil {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Compile" {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "expr" {
						called = true
					}
				}
				return true
			})
		}
	}
	if !called {
		t.Error("compile does not call expr.Compile")
	}
}

// The claim refine rules stand on, held against Expr v1.17.8: an expr.Patch
// visitor that only records the root's slot sees the tree expr.Compile
// checks, and when the check refuses, the nodes it reached keep the types it
// set (the checker annotates the same nodes in place and carries on past the
// first fault).
func TestRefusedTreeKeepsCheckedTypes(t *testing.T) {
	var rec rootRecorder
	_, err := expr.Compile("sum(items, # > 1) > 0", expr.Env(sampleEnv{}), expr.Patch(&rec), expr.Optimize(false))
	if err == nil {
		t.Fatal("a sum over bools compared with an int compiled")
	}
	root := rec.root()
	if root == nil {
		t.Fatal("the visitor recorded no root")
	}
	var pred *exprast.PredicateNode
	exprast.Walk(&root, visitFunc(func(n *exprast.Node) {
		if p, ok := (*n).(*exprast.PredicateNode); ok {
			pred = p
		}
	}))
	if pred == nil || pred.Node.Type() == nil || pred.Node.Type().Kind() != reflect.Bool {
		t.Fatalf("the refused tree's predicate is typed %v, want bool", pred)
	}
	// Recording mutates nothing: the same source compiles the same way with
	// and without the recorder.
	_, plain := expr.Compile("sum(items, # > 1) > 0", expr.Env(sampleEnv{}), expr.Optimize(false))
	if plain == nil || plain.Error() != err.Error() {
		t.Fatalf("the recorder changed the refusal: %v vs %v", err, plain)
	}
}
