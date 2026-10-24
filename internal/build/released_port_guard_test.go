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

// TestNoTestUsesAReleasedPort fails any test that listens (a Listen* call or
// an httptest server), reads the address, closes the listener and then uses
// the address: a port picked and released. The port is free the moment the listener closes, so a
// concurrent run on the host can be handed it before the use, and a dial the
// test expects refused reaches a stranger's listener (#1089), or a bind the
// test expects to win loses (#1077). A test that needs a refused dial aims at
// nettest.RefusedAddr, which holds its port for the test's life; a server
// binds port 0 and reports what it got.
//
// Test files are read whatever their build tags, and so are the non-test
// files of a test-helper package (a directory named *test), where a helper
// returning a released address would otherwise hide. A port released through
// a wrapper (a simulated face's StopTCPSession, then a restart on the port)
// is outside what this reads; #1116 tracks the one class of it on the tree.
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

// releasedPortUses walks each function of f in source order and reports a
// listener's address used once the listener has closed. A listener is a
// variable or field assigned from a Listen* call or an httptest server; its
// address is what Addr, LocalAddr, a server's URL or its Listener's Addr
// reads, kept in a variable or anything derived from one. A close counts
// where it runs in line with the code: in the body, or in a subtest's
// function (t.Run runs it there and then). A close in any other closure (a
// cleanup, a goroutine, a stop function) holds the listener until then, and
// a close on a path that returns or fails the test releases it only on that
// path. A deferred close releases it at the function's return, so a
// function returning the address hands back a released port.
func releasedPortUses(fset *token.FileSet, f *ast.File) []releasedPortUse {
	var out []releasedPortUse
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		d := &releaseDetector{
			gen: map[string]int{}, listeners: map[string]bool{}, httpServers: map[string]bool{},
			closes: map[string][]closeSpan{}, deferred: map[*ast.BlockStmt]map[string]bool{},
			addrOf: map[string]string{}, reported: map[string]bool{},
		}
		d.visit(fn.Body, walkCtx{frame: fn.Body, block: fn.Body})
		for _, u := range d.out {
			out = append(out, releasedPortUse{fn: fn.Name.Name, listener: u.listener, pos: fset.Position(u.pos)})
		}
	}
	return out
}

type bodyUse struct {
	listener string
	pos      token.Pos
}

// closeSpan is where a close leaves its listener released: from at to
// until, or to the end when until is unset.
type closeSpan struct{ at, until token.Pos }

// walkCtx is where the walk stands: the function a deferred close runs at
// the return of (its body), the innermost block, whether a close here runs
// in line (held is false) and whether it is deferred.
type walkCtx struct {
	frame     *ast.BlockStmt
	block     *ast.BlockStmt
	held      bool
	deferring bool
}

// releaseDetector tracks one top-level function. A listener's key is its
// expression (ln, s.ln) and a generation, bumped by every assignment, so an
// address read from a listener since replaced stays tied to the one it came
// from.
type releaseDetector struct {
	gen         map[string]int
	listeners   map[string]bool // versioned key
	httpServers map[string]bool // versioned key of an httptest server
	closes      map[string][]closeSpan
	deferred    map[*ast.BlockStmt]map[string]bool
	addrOf      map[string]string // address variable -> versioned listener key
	reported    map[string]bool
	out         []bodyUse
}

func (d *releaseDetector) visit(n ast.Node, c walkCtx) {
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			if n != c.block {
				inner := c
				inner.block = n
				d.visit(n, inner)
				return false
			}
		case *ast.FuncLit:
			d.visit(n.Body, walkCtx{frame: n.Body, block: n.Body, held: true})
			return false
		case *ast.GoStmt:
			held := c
			held.held = true
			d.visit(n.Call, held)
			return false
		case *ast.DeferStmt:
			deferred := c
			deferred.deferring = true
			d.visit(n.Call, deferred)
			return false
		case *ast.AssignStmt:
			for _, r := range n.Rhs {
				d.visit(r, c)
			}
			d.assign(n.Lhs, n.Rhs)
			return false
		case *ast.ValueSpec:
			for _, r := range n.Values {
				d.visit(r, c)
			}
			lhs := make([]ast.Expr, len(n.Names))
			for i, id := range n.Names {
				lhs[i] = id
			}
			d.assign(lhs, n.Values)
			return false
		case *ast.ReturnStmt:
			for _, r := range n.Results {
				d.visit(r, c)
				if key, ok := d.addrSource(r); ok && d.deferred[c.frame][key] {
					d.report(key, "return:"+key, r.Pos())
				}
			}
			return false
		case *ast.CallExpr:
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "Close" && len(n.Args) == 0 {
					if key, ok := d.listenerKey(sel.X); ok {
						d.close(key, n, c)
						return false
					}
				}
				if sel.Sel.Name == "Run" {
					// A subtest runs its function in line, at the call.
					d.visit(n.Fun, c)
					for _, a := range n.Args {
						if lit, ok := a.(*ast.FuncLit); ok {
							d.visit(lit.Body, walkCtx{frame: lit.Body, block: lit.Body, held: c.held})
							continue
						}
						d.visit(a, c)
					}
					return false
				}
			}
		case *ast.SelectorExpr:
			if key, ok := d.addrRead(n); ok {
				if d.released(key, n.Pos()) {
					d.report(key, "read:"+key, n.Pos())
				}
				return false
			}
		case *ast.Ident:
			if key, ok := d.addrOf[n.Name]; ok && d.released(key, n.Pos()) {
				d.report(key, "var:"+n.Name+":"+key, n.Pos())
			}
		}
		return true
	})
}

// close records a listener's close: deferred to the function's return, held
// to a later time when it sits in a closure that does not run in line, or
// released here, until the end of a block that returns or fails the test.
func (d *releaseDetector) close(key string, call *ast.CallExpr, c walkCtx) {
	switch {
	case c.deferring:
		if d.deferred[c.frame] == nil {
			d.deferred[c.frame] = map[string]bool{}
		}
		d.deferred[c.frame][key] = true
	case c.held:
	default:
		span := closeSpan{at: call.Pos()}
		if end, ok := terminates(c.block); ok {
			span.until = end
		}
		d.closes[key] = append(d.closes[key], span)
	}
}

func (d *releaseDetector) released(key string, at token.Pos) bool {
	for _, s := range d.closes[key] {
		if s.at < at && (s.until == token.NoPos || at < s.until) {
			return true
		}
	}
	return false
}

func (d *releaseDetector) report(key, once string, at token.Pos) {
	if d.reported[once] {
		return
	}
	d.reported[once] = true
	name, _, _ := strings.Cut(key, "#")
	d.out = append(d.out, bodyUse{listener: name, pos: at})
}

// assign applies an assignment: a Listen* call or an httptest server makes
// its first target a new listener; a value carrying an address makes every
// target an address of that listener; anything else ends a target's part.
func (d *releaseDetector) assign(lhs, rhs []ast.Expr) {
	listened, httpServer := false, false
	from, carries := "", false
	for _, r := range rhs {
		if call, ok := r.(*ast.CallExpr); ok {
			name := calleeName(call)
			listened = listened || strings.HasPrefix(name, "Listen")
			httpServer = httpServer || name == "NewServer" || name == "NewTLSServer" || name == "NewUnstartedServer"
		}
		if key, ok := d.addrSource(r); ok {
			from, carries = key, true
		}
	}
	for i, l := range lhs {
		name := exprKey(l)
		if name == "" || name == "_" {
			continue
		}
		delete(d.addrOf, name)
		d.gen[name]++
		key := fmt.Sprintf("%s#%d", name, d.gen[name])
		switch {
		case (listened || httpServer) && i == 0:
			d.listeners[key] = true
			d.httpServers[key] = httpServer
		case carries:
			if _, isIdent := l.(*ast.Ident); isIdent && name != "err" && name != "ok" {
				d.addrOf[name] = from
			}
		}
	}
}

// listenerKey is the versioned key of the listener x names, if it is one.
func (d *releaseDetector) listenerKey(x ast.Expr) (string, bool) {
	name := exprKey(x)
	if name == "" {
		return "", false
	}
	key := fmt.Sprintf("%s#%d", name, d.gen[name])
	return key, d.listeners[key]
}

// addrRead reports a selector that reads a listener's address: its Addr or
// LocalAddr, an httptest server's URL, or the Addr of a server's Listener.
func (d *releaseDetector) addrRead(sel *ast.SelectorExpr) (string, bool) {
	switch sel.Sel.Name {
	case "Addr", "LocalAddr":
		if key, ok := d.listenerKey(sel.X); ok {
			return key, true
		}
		if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "Listener" {
			if key, ok := d.listenerKey(inner.X); ok && d.httpServers[key] {
				return key, true
			}
		}
	case "URL":
		if key, ok := d.listenerKey(sel.X); ok && d.httpServers[key] {
			return key, true
		}
	}
	return "", false
}

// addrSource reports the listener whose address e is: read directly, an
// address variable, or a reshaping of one (a method or field of it, a type
// assertion, a string built from it, a composite holding it, or one of the
// functions that split, join, format or parse an address). A call that
// merely takes the address (a dial, a probe) makes something else of it.
func (d *releaseDetector) addrSource(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.Ident:
		key, ok := d.addrOf[e.Name]
		return key, ok
	case *ast.SelectorExpr:
		if key, ok := d.addrRead(e); ok {
			return key, true
		}
		return d.addrSource(e.X)
	case *ast.ParenExpr:
		return d.addrSource(e.X)
	case *ast.StarExpr:
		return d.addrSource(e.X)
	case *ast.UnaryExpr:
		return d.addrSource(e.X)
	case *ast.TypeAssertExpr:
		return d.addrSource(e.X)
	case *ast.BinaryExpr:
		if key, ok := d.addrSource(e.X); ok {
			return key, true
		}
		return d.addrSource(e.Y)
	case *ast.KeyValueExpr:
		return d.addrSource(e.Value)
	case *ast.CompositeLit:
		for _, el := range e.Elts {
			if key, ok := d.addrSource(el); ok {
				return key, true
			}
		}
	case *ast.CallExpr:
		if key, ok := d.addrSource(e.Fun); ok {
			return key, true // the read itself (ln.Addr()), or a method of the address (a.String())
		}
		if addressFuncs[calleeName(e)] {
			for _, a := range e.Args {
				if key, ok := d.addrSource(a); ok {
					return key, true
				}
			}
		}
	}
	return "", false
}

// addressFuncs reshape an address without using it.
var addressFuncs = map[string]bool{
	"SplitHostPort": true, "JoinHostPort": true, "Sprintf": true, "Sprint": true,
	"Itoa": true, "Atoi": true, "Parse": true, "ParseAddrPort": true,
	"ResolveTCPAddr": true, "ResolveUDPAddr": true, "TrimPrefix": true, "TrimSuffix": true,
}

// terminates reports a block whose last statement leaves it for good, and
// where its release ends: a return or branch at the block's end (a returned
// address is handed back released), a call that ends the test or the
// goroutine at that call (its arguments are a message, not a use).
func terminates(b *ast.BlockStmt) (token.Pos, bool) {
	if len(b.List) == 0 {
		return token.NoPos, false
	}
	switch last := b.List[len(b.List)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return b.End(), true
	case *ast.ExprStmt:
		if call, ok := last.X.(*ast.CallExpr); ok {
			switch calleeName(call) {
			case "Fatal", "Fatalf", "FailNow", "Skip", "Skipf", "SkipNow", "panic", "Exit", "Goexit":
				return last.Pos(), true
			}
		}
	}
	return token.NoPos, false
}

// calleeName is the called function's own name: Listen in net.Listen and
// lc.Listen, panic in panic(x).
func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fun.Sel.Name
	case *ast.Ident:
		return fun.Name
	}
	return ""
}

// exprKey spells an identifier or a field selector chain (ln, s.ln), empty
// for anything else.
func exprKey(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if x := exprKey(e.X); x != "" {
			return x + "." + e.Sel.Name
		}
	}
	return ""
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
		// The review of #1089's shapes: each is the race, and the first cut
		// passed every one.
		{"a helper deferring the close and returning the address", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			defer ln.Close()
			return ln.Addr().String()`, []string{"ln"}},
		{"a helper deferring the close and returning a kept address", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			defer ln.Close()
			a := ln.Addr().String()
			return a`, []string{"ln"}},
		{"a listener closed inside a subtest", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			t.Run("closed", func(t *testing.T) {
				ln.Close()
				dial(addr)
			})`, []string{"ln"}},
		{"an httptest server's kept URL after its close", `
			srv := httptest.NewServer(h)
			u := srv.URL
			srv.Close()
			get(u)`, []string{"srv"}},
		{"an httptest server's URL read after its close", `
			srv := httptest.NewServer(h)
			srv.Close()
			get(srv.URL)`, []string{"srv"}},
		{"an httptest server's listener address after its close", `
			srv := httptest.NewUnstartedServer(h)
			srv.Start()
			a := srv.Listener.Addr().String()
			srv.Close()
			dial(a)`, []string{"srv"}},
		{"a listener in a struct field", `
			s.ln, _ = net.Listen("tcp", "127.0.0.1:0")
			addr := s.ln.Addr().String()
			s.ln.Close()
			dial(addr)`, []string{"s.ln"}},
		{"an address derived through another variable", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			a := ln.Addr()
			_, port, _ := net.SplitHostPort(a.String())
			ln.Close()
			dial(port)`, []string{"ln"}},
		{"a reassigned listener's old address", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			ln.Close()
			ln, _ = net.Listen("tcp", "127.0.0.1:0")
			defer ln.Close()
			dial(addr)`, []string{"ln"}},
		{"a reassigned listener's new address", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			ln.Close()
			ln, _ = net.Listen("tcp", "127.0.0.1:0")
			defer ln.Close()
			dial(ln.Addr().String())`, nil},
		{"a close on an error path that returns", `
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			if err := setup(addr); err != nil {
				_ = ln.Close()
				return ""
			}
			dial(addr)`, nil},
		{"a close on a path that fails the test", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			if bad {
				ln.Close()
				t.Fatalf("bad %s", addr)
			}
			dial(addr)`, nil},
		{"a use on the error path after its close", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			if bad {
				ln.Close()
				dial(addr)
				return ""
			}`, []string{"ln"}},
		{"a close in a returned cleanup", `
			srv := httptest.NewServer(h)
			stop := func() { srv.Close() }
			defer stop()
			get(srv.URL)`, nil},
		{"a cleanup method value", `
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			get(srv.URL)`, nil},
		{"a close in a goroutine", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			go func() { <-done; ln.Close() }()
			dial(ln.Addr().String())`, nil},
		{"a dial's result kept before the close", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			addr := ln.Addr().String()
			res, err := dial(addr)
			ln.Close()
			check(res, err)`, nil},
		{"a deferred close returning what a dial made of the address", `
			ln, _ := net.Listen("tcp", "127.0.0.1:0")
			defer ln.Close()
			return dial(ln.Addr().String())`, nil},
		{"an address rebuilt into a udp address", `
			free, _ := net.ListenUDP("udp4", &net.UDPAddr{})
			port := free.LocalAddr().(*net.UDPAddr).Port
			target := &net.UDPAddr{IP: ip, Port: port}
			free.Close()
			send(target)`, []string{"free"}},
		{"a subtest deferring its own close", `
			t.Run("held", func(t *testing.T) {
				ln, _ := net.Listen("tcp", "127.0.0.1:0")
				defer ln.Close()
				dial(ln.Addr().String())
			})`, nil},
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
