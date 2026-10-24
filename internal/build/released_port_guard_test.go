package build

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestNoTestUsesAReleasedPort fails any test that listens, reads the
// listener's address, closes the listener and then uses the address: a port
// picked and released. The port is free the moment the listener closes, so a
// concurrent run on the host can be handed it before the use, and a dial the
// test expects refused reaches a stranger's listener (#1089), or a bind the
// test expects to win loses (#1077). A test that needs a refused dial aims at
// nettest.RefusedAddr, which holds its port for the test's life; a server
// binds port 0 and reports what it got.
//
// Test files are read whatever their build tags, and so are the non-test
// files of a test-helper package (a directory named *test), where a helper
// returning a released address would otherwise hide.
func TestNoTestUsesAReleasedPort(t *testing.T) {
	// The uses that cannot be avoided, each with why. A stale entry fails,
	// so the list never outlives the code it excuses.
	allowed := map[string]string{
		"internal/node/datagram_test.go:TestAUDPSocketRebindsOnlyWhenWhatItDependsOnChanges":                       "the code under test binds the listen port it is told, so the test must name one; a taken port fails the bind and the pick retries",
		"internal/collection/udp_integration_test.go:TestDatagramBindsTheEndpointsListenPortAndItsRequestsShareIt": "the code under test binds the listen port it is told, so the test must name one; a taken port fails the bind and the pick retries",
		"internal/nettest/refused_test.go:TestAReleasedPortCanBeTakenInTheGap":                                     "the contrast that proves the race nettest.RefusedAddr closes",
	}

	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	seen := map[string]bool{}
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
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(filepath.Base(filepath.Dir(path)), "test") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil // a file that does not parse is the compiler's to report
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, u := range releasedPortUses(fset, f) {
			key := rel + ":" + u.fn
			if _, ok := allowed[key]; ok {
				seen[key] = true
				continue
			}
			found = append(found, fmt.Sprintf("%s:%d: %s uses %s's address after closing it", rel, u.pos.Line, u.fn, u.listener))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(found)
	for _, f := range found {
		t.Errorf("%s; a port picked and released can be taken before the use. Aim a refused dial at nettest.RefusedAddr", f)
	}
	for key := range allowed {
		if !seen[key] {
			t.Errorf("allowed use %s no longer occurs; drop it from the list", key)
		}
	}
}

// releasedPortUse is one use of a listener's address after the listener
// closed: fn is the top-level function it sits in.
type releasedPortUse struct {
	fn       string
	listener string
	pos      token.Position
}

// releasedPortUses finds, in each function body of f (a declaration's and
// every function literal's), a listener (a variable assigned from a Listen*
// call) whose address was read (Addr or LocalAddr, kept in a variable or
// read again) and which was then closed outright, not deferred and not from
// a closure, before the address was used.
func releasedPortUses(fset *token.FileSet, f *ast.File) []releasedPortUse {
	var out []releasedPortUse
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		bodies := []*ast.BlockStmt{fn.Body}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				bodies = append(bodies, lit.Body)
			}
			return true
		})
		for _, body := range bodies {
			for _, u := range releasedInBody(body) {
				out = append(out, releasedPortUse{fn: fn.Name.Name, listener: u.listener, pos: fset.Position(u.pos)})
			}
		}
	}
	return out
}

type bodyUse struct {
	listener string
	pos      token.Pos
}

// releasedInBody walks one body in source order. It does not look inside a
// nested function literal for listeners, closes or assignments (that body is
// walked on its own), but it does follow a use of the address into one, as a
// subtest run after the close is still a use after it.
func releasedInBody(body *ast.BlockStmt) []bodyUse {
	listeners := map[string]bool{}
	closed := map[string]token.Pos{}
	addrOf := map[string]string{} // address variable -> the listener it was read from
	lhs := map[*ast.Ident]bool{}
	var out []bodyUse

	var walk func(n ast.Node, nested bool) bool
	walk = func(n ast.Node, nested bool) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			ast.Inspect(n.Body, func(m ast.Node) bool { return walk(m, true) })
			return false
		case *ast.DeferStmt:
			return false
		case *ast.AssignStmt:
			if nested {
				break
			}
			for _, l := range n.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					lhs[id] = true
				}
			}
			assign(n.Lhs, n.Rhs, listeners, addrOf)
		case *ast.ValueSpec:
			if nested {
				break
			}
			var ls []ast.Expr
			for _, id := range n.Names {
				lhs[id] = true
				ls = append(ls, id)
			}
			assign(ls, n.Values, listeners, addrOf)
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				break
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || !listeners[x.Name] {
				break
			}
			switch sel.Sel.Name {
			case "Close":
				if _, done := closed[x.Name]; !done && !nested {
					closed[x.Name] = n.Pos()
				}
			case "Addr", "LocalAddr":
				if at, done := closed[x.Name]; done && n.Pos() > at {
					out = append(out, bodyUse{listener: x.Name, pos: n.Pos()})
				}
			}
		case *ast.Ident:
			l, ok := addrOf[n.Name]
			if !ok || lhs[n] {
				break
			}
			if at, done := closed[l]; done && n.Pos() > at {
				out = append(out, bodyUse{listener: l, pos: n.Pos()})
				delete(addrOf, n.Name) // one report per released address
			}
		}
		return true
	}
	ast.Inspect(body, func(n ast.Node) bool { return walk(n, false) })
	return out
}

// assign records what an assignment makes of its targets: a Listen* call
// makes the first a listener; a value read from a listener's address makes
// every target an address of it; anything else ends a target's tracking.
func assign(lhs, rhs []ast.Expr, listeners map[string]bool, addrOf map[string]string) {
	var listened bool
	var from string
	for _, r := range rhs {
		if isListenCall(r) {
			listened = true
		}
		if l := addrRead(r, listeners); l != "" {
			from = l
		}
	}
	for i, l := range lhs {
		id, ok := l.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		delete(addrOf, id.Name)
		switch {
		case listened && i == 0:
			listeners[id.Name] = true
		case from != "":
			addrOf[id.Name] = from
		}
	}
}

// isListenCall reports a call whose function is named Listen*: net.Listen,
// net.ListenUDP, net.ListenPacket, a ListenConfig's Listen.
func isListenCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return strings.HasPrefix(fun.Sel.Name, "Listen")
	case *ast.Ident:
		return strings.HasPrefix(fun.Name, "Listen")
	}
	return false
}

// addrRead returns the listener whose address e reads, if any.
func addrRead(e ast.Expr, listeners map[string]bool) string {
	var from string
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Addr" && sel.Sel.Name != "LocalAddr") {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && listeners[x.Name] {
			from = x.Name
		}
		return true
	})
	return from
}

// The detector finds each shape of a released port and passes each shape of
// a listener held for as long as its address is used.
func TestReleasedPortUsesFindsPickAndRelease(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
		want []string // listener names reported, in order
	}{
		{"a refused dial at a released port", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			_ = ln.Close()
			dial(addr)`, []string{"ln"}},
		{"inside a subtest", `
			t.Run("refused", func(t *testing.T) {
				ln, _ := net.Listen("tcp", "127.0.0.1:0")
				addr := ln.Addr().String()
				ln.Close()
				dial(addr)
			})`, []string{"ln"}},
		{"a udp port handed to a later bind", `
			free, _ := net.ListenUDP("udp4", &net.UDPAddr{})
			port := free.LocalAddr().(*net.UDPAddr).Port
			_ = free.Close()
			bind(port)`, []string{"free"}},
		{"a helper returning a released address", `
			ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
			if err != nil {
				return ""
			}
			a := ln.Addr().String()
			ln.Close()
			return a`, []string{"ln"}},
		{"the address read again after the close", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			ln.Close()
			dial(ln.Addr().String())`, []string{"ln"}},
		{"a use in a closure after the close", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			ln.Close()
			t.Run("dial", func(t *testing.T) { dial(addr) })`, []string{"ln"}},
		{"a var declaration", `
			var ln, _ = net.Listen("tcp", "127.0.0.1:0")
			var addr = ln.Addr().String()
			ln.Close()
			dial(addr)`, []string{"ln"}},
		{"a deferred close", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			defer ln.Close()
			addr := ln.Addr().String()
			dial(addr)`, nil},
		{"a close at cleanup", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			t.Cleanup(func() { _ = ln.Close() })
			dial(ln.Addr().String())`, nil},
		{"a close after the last use", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			dial(addr)
			_ = ln.Close()`, nil},
		{"the variable reused for a held listener", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			ln.Close()
			addr = held()
			dial(addr)`, nil},
		{"a connection, not a listener", `
			c, _ := net.Dial("tcp", target)
			local := c.LocalAddr().String()
			c.Close()
			report(local)`, nil},
		{"a refused address held for the test", `
			addr := nettest.RefusedAddr(t)
			dial(addr)`, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := "package p\nfunc F() string {" + c.body + "\nreturn \"\"\n}\n"
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "p.go", src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			var got []string
			for _, u := range releasedPortUses(fset, f) {
				if u.fn != "F" {
					t.Fatalf("use in %q, want F", u.fn)
				}
				got = append(got, u.listener)
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("reported %v, want %v", got, c.want)
			}
		})
	}
}
