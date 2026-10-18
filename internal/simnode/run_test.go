//go:build linux

package simnode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

const runPortShift = 23000

// simulatedProduct picks a product the seed ships that the simulator catalog
// has a profile for, with an HTTP interface, and returns its name and HTTP
// port. Derived rather than named, so the test follows the catalogs.
func simulatedProduct(t *testing.T, ctx context.Context, conn *pgx.Conn) (string, int) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := conn.Query(ctx, `select name from product`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	rows.Close()
	sort.Strings(names)
	for _, n := range names {
		if p, ok := store.Devices[n]; ok {
			for _, iface := range p.Interfaces {
				if iface.Protocol == "http" && !iface.TLS {
					return n, iface.Port
				}
			}
		}
	}
	t.Fatal("no seeded product has a simulator profile with plain HTTP")
	return "", 0
}

func answers(addr string) bool {
	c := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := c.Get("http://" + addr + "/")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestRunServesTheNodesDevicesAndFollowsChanges is the run mode's loop: a
// simulator holding a node's credential serves the devices placed on that
// node, its hits reach the server, a device added on the server starts
// serving without a restart, and a device removed goes dark.
func TestRunServesTheNodesDevicesAndFollowsChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-s"}, all, all); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("sim-secret"))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-s", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	product, port := simulatedProduct(t, ctx, conn)
	addDevice := func(name, ip string) string {
		t.Helper()
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `update component set product_id = (select id from product where name = $1) where name = $2`, product, name); err != nil {
			t.Fatal(err)
		}
		target := net.JoinHostPort(ip, fmt.Sprint(port+runPortShift))
		if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values ($1, 'tcp', (select id from component where name = $2), (select principal_id from node where name = 'edge-s'), jsonb_build_object('target', $3::text))`, name+"-tcp", name, target); err != nil {
			t.Fatal(err)
		}
		return target
	}
	first := addDevice("sim-one", "127.31.0.10")

	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer busSrv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(busSrv.ClientURL())))
	defer apiSrv.Close()

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- Run(runCtx, Config{ServerURL: apiSrv.URL, Name: "edge-s", Token: "sim-secret",
			PortOffset: runPortShift, Poll: 200 * time.Millisecond, FlushEvery: 100 * time.Millisecond})
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run returned %v", err)
		}
	}()

	eventually(t, "the first device to answer", func() bool { return answers(first) })
	var oneID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'sim-one'`).Scan(&oneID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the hit to reach the server", func() bool {
		st, ok := busSrv.SimStatus("edge-s")
		return ok && st.Devices[oneID].Count > 0 && st.Serving[oneID] && st.Capabilities.PortOffset == runPortShift
	})

	second := addDevice("sim-two", "127.31.0.11")
	eventually(t, "the added device to answer without a restart", func() bool { return answers(second) })

	if _, err := conn.Exec(ctx, `delete from endpoint where name = 'sim-one-tcp'`); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the removed device to go dark", func() bool { return !answers(first) })
	if !answers(second) {
		t.Fatal("removing one device darkened the other")
	}
}
