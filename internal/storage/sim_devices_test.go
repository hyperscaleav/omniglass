package storage_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestNodeSimDevices proves the simulator's device list: each component with
// an interface placed on the node, once, with its product and the host its
// interfaces target, and nothing placed on another node or on none.
func TestNodeSimDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	for _, n := range []string{"sim-a", "sim-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatalf("create node %s: %v", n, err)
		}
	}
	for _, c := range []string{"dsp-1", "dsp-2", "loose"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c}, all, all, all, all); err != nil {
			t.Fatalf("create component %s: %v", c, err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	exec := func(sql string) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	// The product is minted for the test: what the list carries is the
	// product's name, whichever product that is.
	prod := storagetest.MintProduct(t, ctx, gw, "")
	exec(`update component set product_id = (select id from product where name = '` + prod.Name + `') where name = 'dsp-1'`)
	endpoint := func(name, typ, comp, node, target string) {
		t.Helper()
		nodeExpr := "null"
		if node != "" {
			nodeExpr = "(select principal_id from node where name = '" + node + "')"
		}
		exec(`insert into endpoint (name, transport, component, node_name, params) values ('` + name + `',
			'` + typ + `',
			(select id from component where name = '` + comp + `'), ` + nodeExpr + `,
			'{"target":"` + target + `"}'::jsonb)`)
	}
	endpoint("dsp-1-http", "tcp", "dsp-1", "sim-a", "127.20.0.5:80")
	endpoint("dsp-1-icmp", "icmp", "dsp-1", "sim-a", "127.20.0.5")
	endpoint("dsp-2-http", "tcp", "dsp-2", "sim-b", "127.20.0.6:80")
	endpoint("loose-http", "tcp", "loose", "", "127.20.0.7:80")

	got, err := gw.NodeSimDevices(ctx, "sim-a", all)
	if err != nil {
		t.Fatalf("sim devices: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("sim-a devices = %+v, want dsp-1 once", got)
	}
	d := got[0]
	if d.Name != "dsp-1" || d.Product != prod.Name || d.Address != "127.20.0.5" || d.ComponentID == "" {
		t.Fatalf("device = %+v", d)
	}

	// The list honors the caller's component scope (resolved component
	// roots): a reader whose scope covers dsp-1 sees it, one whose scope
	// covers only another component, or nothing, does not.
	var dsp2ID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'dsp-2'`).Scan(&dsp2ID); err != nil {
		t.Fatal(err)
	}
	if got, err := gw.NodeSimDevices(ctx, "sim-a", scope.Set{IDs: []string{d.ComponentID}}); err != nil || len(got) != 1 {
		t.Fatalf("scoped to dsp-1: %v %+v", err, got)
	}
	for _, read := range []scope.Set{{IDs: []string{dsp2ID}}, {}} {
		if got, err := gw.NodeSimDevices(ctx, "sim-a", read); err != nil || len(got) != 0 {
			t.Fatalf("scope %+v sees %+v (%v), want nothing", read, got, err)
		}
	}

	none, err := gw.NodeSimDevices(ctx, "no-such-node", all)
	if err != nil || len(none) != 0 {
		t.Fatalf("unknown node: %v %+v, want empty", err, none)
	}
}
