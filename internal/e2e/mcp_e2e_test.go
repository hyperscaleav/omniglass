//go:build linux

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// mcpSimPortShift lifts the simulated profile's ports clear of the other
// simulator test's.
const mcpSimPortShift = 25000

// TestMCPStdioClientDrivesTheSimulatedFleet is the proof of #1162 end to end:
// the real binary's `omniglass mcp tools` and `omniglass mcp call`, an MCP
// client over stdio, launch `omniglass mcp`, which relays to the real server's
// /mcp. A read tool reports a simulated component's reachability after a real
// node probed it on the wire, and a write tool lands an audit row naming the
// user, the MCP channel and the declared client. Skipped under -short.
func TestMCPStdioClientDrivesTheSimulatedFleet(t *testing.T) {
	if testing.Short() {
		t.Skip("mcp e2e: skipped under -short (Postgres testcontainer + go build)")
	}
	ctx := context.Background()
	root := repoRoot(t)
	binPath := filepath.Join(t.TempDir(), "omniglass")
	build := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/omniglass")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	dsn := storagetest.NewDSN(t)
	server := startServer(t, binPath, "OMNIGLASS_DSN="+dsn).URL

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-m"}, all, all); err != nil {
		t.Fatal(err)
	}
	nodeTok := "mcp-e2e-token"
	sum := sha256.Sum256([]byte(nodeTok))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-m", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "mcp-panel"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	product, port := e2eSimulatedProduct(t, ctx, conn)
	target := net.JoinHostPort(simtest.ClaimLoopback(t), fmt.Sprint(port+mcpSimPortShift))
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`update component set product_id = (select id from product where name = $1) where name = 'mcp-panel'`, []any{product}},
		{`insert into endpoint (name, transport, component, node_name, params) values ('mcp-panel-tcp', 'tcp', (select id from component where name = 'mcp-panel'), (select principal_id from node where name = 'edge-m'), jsonb_build_object('target', $1::text))`, []any{target}},
		{`insert into task (id, mode, endpoint_id, enabled) values ('t-mcp', 'poll', (select id from endpoint where name = 'mcp-panel-tcp'), true)`, nil},
	} {
		if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	simCtx, stopSim := context.WithCancel(ctx)
	defer stopSim()
	simCmd := exec.CommandContext(simCtx, binPath, "sim", "run", "--port-offset", fmt.Sprint(mcpSimPortShift), "--poll", "300ms")
	simCmd.Env = append(os.Environ(), "OMNIGLASS_SERVER="+server, "OMNIGLASS_NODE_NAME=edge-m", "OMNIGLASS_NODE_TOKEN="+nodeTok)
	simCmd.Stdout, simCmd.Stderr = os.Stderr, os.Stderr
	if err := simCmd.Start(); err != nil {
		t.Fatalf("start sim: %v", err)
	}
	t.Cleanup(func() { stopSim(); _ = simCmd.Wait() })

	// The agent's environment: the server and the user's token, nothing else.
	env := append(os.Environ(), "OMNIGLASS_SERVER="+server, "OMNIGLASS_TOKEN="+ownerTok)
	cli := runCLI(t, root, binPath, env)
	waitFor(t, "the simulator to serve the component", func() bool {
		out, code := cli("node", "simulation", "edge-m")
		return code == 0 && strings.Contains(out, `"serving": true`)
	})
	if out, code := cli("node", "run", "--name", "edge-m", "--token", nodeTok, "--once"); code != 0 {
		t.Fatalf("node run exit %d:\n%s", code, out)
	}

	out, code := cli("mcp", "tools")
	if code != 0 || !strings.Contains(out, `"name": "get-component-reachability"`) || !strings.Contains(out, `"name": "create-secret"`) {
		t.Fatalf("mcp tools exit %d:\n%s", code, out)
	}
	if strings.Contains(out, ownerTok) {
		t.Fatal("mcp tools printed the token")
	}

	var last string
	defer func() {
		if t.Failed() {
			t.Logf("last reachability read:\n%s", last)
		}
	}()
	waitFor(t, "the probe's verdict to read through the MCP read tool", func() bool {
		out, code := cli("mcp", "call", "get-component-reachability", "--args", `{"name":"mcp-panel"}`)
		last = out
		return code == 0 && strings.Contains(out, "mcp-panel-tcp") && strings.Contains(out, `"value": "up"`)
	})

	out, code = cli("mcp", "call", "create-secret", "--args", `{"name":"mcp-e2e-key","secret_type":"api-key","owner_kind":"platform","fields":{"key":"k-1"}}`)
	if code != 0 {
		t.Fatalf("mcp call create-secret exit %d:\n%s", code, out)
	}
	out, code = cli("mcp", "call", "get-component", "--args", `{"name":"no-such-component"}`)
	if code == 0 || !strings.Contains(out, "404") {
		t.Fatalf("a tool error should exit non-zero with the route's refusal, got exit %d:\n%s", code, out)
	}

	out, code = cli("audit-log", "list", "--resource", "secret", "--verb", "create")
	if code != 0 {
		t.Fatalf("audit-log list exit %d:\n%s", code, out)
	}
	var trail struct {
		Events []struct {
			ActorName string         `json:"actor_name"`
			Channel   string         `json:"channel"`
			Client    string         `json:"client"`
			New       map[string]any `json:"new"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &trail); err != nil {
		t.Fatalf("decode audit-log: %v\n%s", err, out)
	}
	for _, e := range trail.Events {
		if e.New["name"] == "mcp-e2e-key" {
			if e.ActorName != "root" || e.Channel != "mcp" || !strings.HasPrefix(e.Client, "omniglass-cli") {
				t.Fatalf("audit row = %+v, want actor root, channel mcp, client omniglass-cli", e)
			}
			return
		}
	}
	t.Fatalf("no audit row for the MCP write:\n%s", out)
}
