//go:build linux

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

const calcE2EPortShift = 26000

// TestCalcRuleEndToEnd drives a calc rule as an operator would (#996): the
// real server, a simulated device, and a node collecting its tcp probe. A
// calc rule authored through the CLI folds the probe's tcp-open over its
// instances into a metric of its own, the server's calc tick writes it as a
// calculated sample naming the rule, and an event rule on that output key
// raises an alarm on the component.
func TestCalcRuleEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("calc e2e: skipped under -short (Postgres testcontainer + go build)")
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
	addr := "127.0.0.1:" + freePort(t)
	natsPort := freePort(t)
	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := exec.CommandContext(srvCtx, binPath, "server")
	srv.Env = append(os.Environ(),
		"OMNIGLASS_DSN="+dsn,
		"OMNIGLASS_ADDR="+addr,
		"OMNIGLASS_NATS_ADDR=127.0.0.1:"+natsPort,
		"OMNIGLASS_NATS_URL=nats://127.0.0.1:"+natsPort,
		"OMNIGLASS_NATS_STORE_DIR="+t.TempDir(),
	)
	srv.Stdout, srv.Stderr = os.Stderr, os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { cancel(); _ = srv.Wait() })
	server := "http://" + addr
	pollHealthz(t, server+"/api/v1/healthz")

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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-c"}, all, all); err != nil {
		t.Fatal(err)
	}
	nodeTok := "calc-e2e-token"
	sum := sha256.Sum256([]byte(nodeTok))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-c", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "calc-dev"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	product, port := e2eSimulatedProduct(t, ctx, conn)
	target := net.JoinHostPort("127.33.0.20", fmt.Sprint(port+calcE2EPortShift))
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`update component set product_id = (select id from product where name = $1) where name = 'calc-dev'`, []any{product}},
		{`insert into endpoint (name, transport, component, node_name, params) values ('calc-dev-tcp', 'tcp', (select id from component where name = 'calc-dev'), (select principal_id from node where name = 'edge-c'), jsonb_build_object('target', $1::text))`, []any{target}},
		{`insert into task (id, mode, endpoint_id, enabled) values ('t-calc', 'poll', (select id from endpoint where name = 'calc-dev-tcp'), true)`, nil},
	} {
		if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}

	cli := runCLI(t, root, binPath, os.Environ())
	must := func(args ...string) {
		t.Helper()
		if out, code := cli(append(args, "--server", server, "--token", ownerTok)...); code != 0 {
			t.Fatalf("%v exit %d:\n%s", args, code, out)
		}
	}
	// The operator authors the output key, the calc rule over the probe,
	// and an event rule on the output.
	must("metric-type", "create", "--name", "port-open-peak", "--data-type", "float")
	must("calc-rule", "create", "--name", "port-open", "--owner-kind", "component",
		"--inputs", `[{"alias":"open","key":"tcp-open"}]`, "--reduce", `{"expr":"max(open)"}`, "--output-key", "port-open-peak")
	must("event-rule", "create", "--name", "port-up", "--sample-key", "port-open-peak",
		"--fire-criteria", `{"expr":"value >= 1.0"}`, "--severity", "high", "--fire-event-type", "condition-started")

	simCtx, stopSim := context.WithCancel(ctx)
	defer stopSim()
	simCmd := exec.CommandContext(simCtx, binPath, "sim", "run",
		"--port-offset", fmt.Sprint(calcE2EPortShift), "--poll", "300ms")
	simCmd.Env = append(os.Environ(),
		"OMNIGLASS_SERVER="+server, "OMNIGLASS_NODE_NAME=edge-c", "OMNIGLASS_NODE_TOKEN="+nodeTok)
	simCmd.Stdout, simCmd.Stderr = os.Stderr, os.Stderr
	if err := simCmd.Start(); err != nil {
		t.Fatalf("start sim: %v", err)
	}
	t.Cleanup(func() { stopSim(); _ = simCmd.Wait() })

	scalarInt := func(sql string) int {
		var n int
		if err := conn.QueryRow(ctx, sql).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Collect until the probe has reached the simulated device.
	waitForWithin(t, 60*time.Second, "the node's tcp probe to reach the simulated device", func() bool {
		_, _ = cli("node", "run", "--server", server, "--name", "edge-c", "--token", nodeTok, "--once")
		return scalarInt(`select count(*) from metric m join metric_type t on t.id = m.metric_type_id
			where t.name = 'tcp-open' and m.value = 1 and m.component_id = (select id from component where name = 'calc-dev')`) > 0
	})
	waitFor(t, "the calc tick to write port-open-peak as a calculated sample", func() bool {
		return scalarInt(`select count(*) from metric m join metric_type t on t.id = m.metric_type_id
			where t.name = 'port-open-peak' and m.provenance = 'calculated' and m.source_rule = 'port-open'
			and m.source_rule_version = 1 and m.value = 1 and m.instance = ''`) > 0
	})
	waitFor(t, "the event rule on the calc output to raise its alarm", func() bool {
		return scalarInt(`select count(*) from alarm a join event_rule e on e.id = a.raised_by_rule_id
			where e.name = 'port-up' and a.cleared_at is null
			and a.component_id = (select id from component where name = 'calc-dev')`) == 1
	})
}
