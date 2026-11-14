package build

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestNoTestRestartsAFaceAtALiteralAddress fails any test that stops a
// simulated face and starts it again on a port, at a loopback address spelled
// as a literal. The restart is a bind on the address and port it is told, so
// another run on the host that serves the same literal address can be handed
// that port in the gap (`bind: address already in use`), or its panel answers
// this run's node (#1116). A test that restarts a face takes its address from
// simtest.ClaimLoopback, which holds it for the test's life.
//
// It reads the same files as TestNoTestUsesAReleasedPort, and what that guard
// cannot see: a port released through a wrapper (Device.StopTCPSession, then
// Device.StartTCPSession on the port it reported).
func TestNoTestRestartsAFaceAtALiteralAddress(t *testing.T) {
	// The restarts not yet moved to a claim, each with its issue. A stale
	// entry fails, so the list never outlives the code it excuses.
	allowed := map[string]string{}
	for _, fn := range []string{
		"listener_hold_test.go:TestPanelListenerHearsPushesAndReArms",
		"session_hold_test.go:TestPanelHeldSessionSurvivesItsListenerDying",
		"ssh_session_hold_test.go:TestPanelSSHHostKeyChangeIsRejected",
		"ssh_session_hold_test.go:TestPanelSSHSessionOperatesRecoversAndReadsDown",
		"telnet_session_hold_test.go:TestPanelTelnetSessionLogsInOperatesAndRecovers",
		"tls_face_test.go:TestTLSFaceRestartsAndCountsHandshakes",
	} {
		allowed["internal/sim/"+fn] = "the simulator's own session tests, moved to a claim by #1881"
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
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil // a file that does not parse is the compiler's to report
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, u := range restartsAtLiteralAddress(fset, f) {
			key := rel + ":" + u.fn
			if _, ok := allowed[key]; ok {
				seen[key] = true
				continue
			}
			found = append(found, fmt.Sprintf("%s:%d: %s restarts a face (%s) at the literal address %q", rel, u.pos.Line, u.fn, u.restart, u.addr))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(found)
	for _, f := range found {
		t.Errorf("%s; another run serving that address can be handed the port in the gap. Take the address from simtest.ClaimLoopback", f)
	}
	for key := range allowed {
		if !seen[key] {
			t.Errorf("allowed restart %s no longer occurs; drop it from the list", key)
		}
	}
}

// faceStops and faceStarts are the Device methods that release and rebind a
// simulated face's listener.
var (
	faceStops  = map[string]string{"StopTCPSession": "TCPSession", "StopSSH": "SSH", "StopTelnet": "Telnet", "StopHTTP": "HTTP", "StopUDP": "UDP", "StopSNMP": "SNMP", "StopWebSocket": "WebSocket"}
	faceStarts = map[string]string{"StartTCPSession": "TCPSession", "StartTLSSession": "TCPSession", "StartSSH": "SSH", "StartTelnet": "Telnet", "StartHTTP": "HTTP", "StartUDP": "UDP", "StartSNMP": "SNMP", "StartWebSocket": "WebSocket"}
)

// claimRange is the 127.250.0.0/16 block simtest.ClaimLoopback draws from.
var claimRange = netip.MustParsePrefix("127.250.0.0/16")

// localhost is the one loopback address every host has: where a test serves a
// bus or a server on a port the kernel picks, never a simulated panel.
var localhost = netip.MustParseAddr("127.0.0.1")

type literalRestart struct {
	fn      string
	restart string
	addr    string
	pos     token.Position
}

// restartsAtLiteralAddress reports each top-level function of f that stops a
// face and then starts it on a port (any argument but the literal 0, which
// takes whatever the kernel hands out) while a loopback address is spelled in
// it as a string literal outside the claim range (127.0.0.1 aside).
func restartsAtLiteralAddress(fset *token.FileSet, f *ast.File) []literalRestart {
	var out []literalRestart
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		var addr string
		stopped := map[string]token.Pos{} // face -> first stop
		var restart *ast.CallExpr
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if addr == "" && n.Kind == token.STRING {
					if s, err := strconv.Unquote(n.Value); err == nil {
						if a, err := netip.ParseAddr(s); err == nil && a.IsLoopback() && a != localhost && !claimRange.Contains(a) {
							addr = s
						}
					}
				}
			case *ast.CallExpr:
				name := calleeName(n)
				if face, ok := faceStops[name]; ok {
					if _, seen := stopped[face]; !seen {
						stopped[face] = n.Pos()
					}
				}
				if face, ok := faceStarts[name]; ok && restart == nil && len(n.Args) > 0 && !isZeroLiteral(n.Args[0]) {
					if at, ok := stopped[face]; ok && at < n.Pos() {
						restart = n
					}
				}
			}
			return true
		})
		if addr != "" && restart != nil {
			out = append(out, literalRestart{fn: fn.Name.Name, restart: calleeName(restart), addr: addr, pos: fset.Position(restart.Pos())})
		}
	}
	return out
}

func isZeroLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "0"
}

// The detector finds a face restarted on its port at a literal address, and
// passes a claimed address, a restart on port 0 and a start with no stop.
func TestRestartsAtLiteralAddressFindsEachShape(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
		want int
	}{
		{"a literal address and a restart on the reported port", `
			ip := "127.89.16.1"
			panel := serve(ip)
			port := portOf(panel)
			panel.StopTCPSession()
			panel.StartTCPSession(port)`, 1},
		{"a tls restart after a tcp stop", `
			s := newGateStack(t, "127.89.16.8", "edge")
			s.panel.StopTCPSession()
			s.panel.StartTLSSession(port)`, 1},
		{"an ssh restart", `
			ip := "127.88.9.9"
			panel.StopSSH()
			panel.StartSSH(port, "admin", "pw", false)`, 1},
		{"a claimed address", `
			ip := simtest.ClaimLoopback(t)
			panel.StopTCPSession()
			panel.StartTCPSession(port)`, 0},
		{"the host's own localhost", `
			bus := serve("127.0.0.1")
			panel.StopTCPSession()
			panel.StartTCPSession(port)`, 0},
		{"a literal inside the claim range", `
			ip := "127.250.1.2"
			panel.StopTCPSession()
			panel.StartTCPSession(port)`, 0},
		{"a restart on port 0", `
			ip := "127.89.16.1"
			panel.StopTCPSession()
			panel.StartTCPSession(0)`, 0},
		{"a start with no stop", `
			ip := "127.89.16.1"
			panel.StartTCPSession(port)`, 0},
		{"a stop of another face", `
			ip := "127.89.16.1"
			panel.StopSSH()
			panel.StartTCPSession(port)`, 0},
		{"a literal that is no address", `
			name := "127.89"
			panel.StopTCPSession()
			panel.StartTCPSession(port)`, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := "package p\nfunc F() {" + c.body + "\n}\n"
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "p.go", src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := len(restartsAtLiteralAddress(fset, f)); got != c.want {
				t.Fatalf("reported %d, want %d", got, c.want)
			}
		})
	}
}
