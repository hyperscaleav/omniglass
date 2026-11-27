package sim

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// variableProtocolOK are the functions whose hit protocol is a parameter or a
// value the caller chose, not a literal: publishOperation and runShellSession
// take it as an argument, and the guard reads the literals every caller passes;
// operateCommand passes the protocol of the console's command fire, which the
// server restricts to ssh, telnet and tcp (fireKind) and which the hub drops
// if it is anything else.
var variableProtocolOK = map[string]bool{"publishOperation": true, "runShellSession": true, "operateCommand": true}

// protocolSites reads this package's non-test sources for the protocols the
// simulator reports a hit by (a literal named Protocol in a RequestLog or a
// RequestEvent, the first argument of publishOperation, the last of
// runShellSession) and the protocols its binder serves (the cases of
// bindInterface's switch on the interface's protocol), each with where it
// appears. A protocol set by a value rather than a literal, anywhere else, is
// reported so the guard is never blind to an emitter.
func protocolSites(t *testing.T) (reported, served map[string][]string, variable []string) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	reported, served = map[string][]string{}, map[string][]string{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn := ""
			if fd, ok := decl.(*ast.FuncDecl); ok {
				fn = fd.Name.Name
			}
			at := func(n ast.Node) string { return fmt.Sprintf("%s (in %s)", fset.Position(n.Pos()), fn) }
			record := func(v ast.Expr, n ast.Node) {
				if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					p, _ := strconv.Unquote(lit.Value)
					reported[p] = append(reported[p], at(n))
					return
				}
				if !variableProtocolOK[fn] {
					variable = append(variable, at(n))
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CompositeLit:
					id, ok := x.Type.(*ast.Ident)
					if !ok || (id.Name != "RequestLog" && id.Name != "RequestEvent") {
						return true
					}
					for _, el := range x.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Protocol" {
								record(kv.Value, kv)
							}
						}
					}
				case *ast.CallExpr:
					switch f := x.Fun.(type) {
					case *ast.SelectorExpr:
						if f.Sel.Name == "publishOperation" && len(x.Args) > 0 {
							record(x.Args[0], x)
						}
					case *ast.Ident:
						if f.Name == "runShellSession" && len(x.Args) == 6 {
							record(x.Args[5], x)
						}
					}
				case *ast.SwitchStmt:
					if sel, ok := x.Tag.(*ast.SelectorExpr); ok && fn == "bindInterface" && sel.Sel.Name == "Protocol" {
						for _, c := range x.Body.List {
							for _, e := range c.(*ast.CaseClause).List {
								if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
									p, _ := strconv.Unquote(lit.Value)
									served[p] = append(served[p], at(lit))
								}
							}
						}
					}
				}
				return true
			})
		}
	}
	return reported, served, variable
}

// The hub counts a hit by a protocol in collection.SimProtocols and drops any
// other (#1366: the websocket face reported a protocol the hub had never been
// told). So what the simulator's own sources emit is held to that vocabulary
// from the producer's side, whatever the profile schema says: every protocol a
// hit emitter names, and every protocol the binder serves (icmp aside, which
// the kernel answers), is in the vocabulary, and every protocol in the
// vocabulary is emitted and served, so neither a new protocol nor a stale
// entry slips by. A protocol set by a value in an emitter this guard cannot
// read fails it, naming the site.
func TestEveryProtocolTheSimulatorReportsAndServesIsOneTheHubCounts(t *testing.T) {
	reported, served, variable := protocolSites(t)
	if len(reported) == 0 || len(served) == 0 {
		t.Fatalf("found %d reported and %d served protocols: the guard is reading the wrong sources", len(reported), len(served))
	}
	for _, v := range variable {
		t.Errorf("a hit's protocol is set by a value at %s: set it by a literal, or add the function to variableProtocolOK with why", v)
	}
	vocab := collection.SimProtocols()
	for p, where := range reported {
		if !collection.IsSimProtocol(p) {
			t.Errorf("the simulator reports a hit by %q (%s), which the hub rejects: add it to collection.SimProtocols", p, strings.Join(where, "; "))
		}
	}
	for p, where := range served {
		if p != "icmp" && !collection.IsSimProtocol(p) {
			t.Errorf("the engine serves %q (%s), which the hub would reject: add it to collection.SimProtocols", p, strings.Join(where, "; "))
		}
	}
	for _, p := range vocab {
		if _, ok := reported[p]; !ok {
			t.Errorf("collection.SimProtocols names %q, which no emitter in the simulator reports a hit by: delete it", p)
		}
		if _, ok := served[p]; !ok {
			t.Errorf("collection.SimProtocols names %q, which the engine's binder does not serve: delete it", p)
		}
	}
}
