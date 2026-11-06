package storagetest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// A package whose tests each get a database of their own, cloned from a shared
// template, has tests that are independent by construction, so they run in
// parallel: a test that is not parallel puts its whole length on the gate's
// critical path (#1537). The default is therefore a rule rather than a habit.
// Every top-level test starts with t.Parallel(), or says in a "// Serial:"
// comment why it cannot (it changes something process-wide: a package-level
// cap, the slog default, a global counter it asserts on). Serial tests finish
// before any parallel test is released, so they never overlap one.
//
// A package enforces the rule with one test:
//
//	func TestEveryTestRunsInParallelOrSaysWhyNot(t *testing.T) {
//		t.Parallel()
//		storagetest.RequireParallelOrSerial(t, ".")
//	}

// SerialMarker is the comment a test carries in its body to opt out of parallel.
const SerialMarker = "// Serial:"

// UnparallelTests names, as "file: TestName", every top-level test in dir's
// *_test.go files that neither opens with t.Parallel() nor carries
// [SerialMarker] in its body.
func UnparallelTests(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no test files in %s to scan", dir)
	}
	var out []string
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, name := range testsNeitherParallelNorSerial(fset, f, src) {
			out = append(out, filepath.Base(path)+": "+name)
		}
	}
	return out, nil
}

// testsNeitherParallelNorSerial names the top-level tests in a parsed file that
// neither open with t.Parallel() nor carry a serial marker in their body.
func testsNeitherParallelNorSerial(fset *token.FileSet, f *ast.File, src []byte) []string {
	var out []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") || !takesTestingT(fn) {
			continue
		}
		if opensParallel(fn) {
			continue
		}
		body := src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset]
		if strings.Contains(string(body), SerialMarker) {
			continue
		}
		out = append(out, fn.Name.Name)
	}
	return out
}

// takesTestingT is true for func(t *testing.T), the shape of a test the go tool
// runs; TestMain takes a *testing.M and is not one.
func takesTestingT(fn *ast.FuncDecl) bool {
	ps := fn.Type.Params.List
	if len(ps) != 1 {
		return false
	}
	star, ok := ps[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "T"
}

// opensParallel is true when the function's first statement is t.Parallel().
func opensParallel(fn *ast.FuncDecl) bool {
	if len(fn.Body.List) == 0 {
		return false
	}
	es, ok := fn.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Parallel"
}

// RequireParallelOrSerial fails t for every test in dir that is neither
// parallel nor marked serial.
func RequireParallelOrSerial(t interface {
	Helper()
	Fatalf(string, ...any)
	Errorf(string, ...any)
}, dir string) {
	t.Helper()
	bad, err := UnparallelTests(dir)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, name := range bad {
		t.Errorf("%s is not parallel. Start it with t.Parallel(), or, if it changes something "+
			"process-wide, say why in a %q comment in its body (see storagetest/parallelguard.go)",
			name, SerialMarker)
	}
}
