package e2e

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// startServer is the one way this package starts `omniglass server`, and the
// only place that names its listen settings: a test that ran the server
// itself, or set OMNIGLASS_ADDR or OMNIGLASS_NATS_ADDR, could hand it a port
// picked in advance, which a concurrent run can take first (#1077). Every
// file is read, whatever its build tags, so a netadmin-only test is held to
// it too.
func TestOnlyStartServerStartsAServer(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name == "startServer" {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					if isExecCommand(n) && hasStringArg(n, "server") {
						t.Errorf("%s: %s runs `omniglass server` itself; start it with startServer", fset.Position(n.Pos()), fn.Name.Name)
					}
				case *ast.BasicLit:
					v, err := strconv.Unquote(n.Value)
					if n.Kind != token.STRING || err != nil {
						break
					}
					for _, k := range listenSettings {
						if strings.HasPrefix(v, k+"=") {
							t.Errorf("%s: %s sets %s; the listen settings are startServer's", fset.Position(n.Pos()), fn.Name.Name, v)
						}
					}
				}
				return true
			})
		}
	}
}

// listenSettings are the environment variables that pick the server's ports.
var listenSettings = []string{"OMNIGLASS_ADDR", "OMNIGLASS_NATS_ADDR"}

// isExecCommand reports a call of exec.Command or exec.CommandContext.
func isExecCommand(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext")
}

// hasStringArg reports whether a call passes the string literal want.
func hasStringArg(c *ast.CallExpr, want string) bool {
	for _, a := range c.Args {
		if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if v, err := strconv.Unquote(lit.Value); err == nil && v == want {
				return true
			}
		}
	}
	return false
}
