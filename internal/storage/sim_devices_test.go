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

// TestOutpostSimDevices proves the simulator's device list: each component with
// an interface placed on the outpost, once, with its product and the host its
// interfaces target, and nothing placed on another outpost or on none.
func TestOutpostSimDevices(t *testing.T) {
	t.Parallel()
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
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: n}, all, all); err != nil {
			t.Fatalf("create outpost %s: %v", n, err)
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
	endpoint := func(name, typ, comp, outpost, target string) {
		t.Helper()
		outpostExpr := "null"
		if outpost != "" {
			outpostExpr = "(select user_id from outpost where name = '" + outpost + "')"
		}
		exec(`insert into endpoint (name, transport, component, outpost_id, params) values ('` + name + `',
			'` + typ + `',
			(select id from component where name = '` + comp + `'), ` + outpostExpr + `,
			'{"target":"` + target + `"}'::jsonb)`)
	}
	endpoint("dsp-1-http", "tcp", "dsp-1", "sim-a", "127.20.0.5:80")
	endpoint("dsp-1-icmp", "icmp", "dsp-1", "sim-a", "127.20.0.5")
	endpoint("dsp-2-http", "tcp", "dsp-2", "sim-b", "127.20.0.6:80")
	endpoint("loose-http", "tcp", "loose", "", "127.20.0.7:80")

	got, err := gw.OutpostSimDevices(ctx, "sim-a", all)
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
	if got, err := gw.OutpostSimDevices(ctx, "sim-a", scope.Set{IDs: []string{d.ComponentID}}); err != nil || len(got) != 1 {
		t.Fatalf("scoped to dsp-1: %v %+v", err, got)
	}
	for _, read := range []scope.Set{{IDs: []string{dsp2ID}}, {}} {
		if got, err := gw.OutpostSimDevices(ctx, "sim-a", read); err != nil || len(got) != 0 {
			t.Fatalf("scope %+v sees %+v (%v), want nothing", read, got, err)
		}
	}

	none, err := gw.OutpostSimDevices(ctx, "no-such-outpost", all)
	if err != nil || len(none) != 0 {
		t.Fatalf("unknown outpost: %v %+v, want empty", err, none)
	}
}

// TestComponentPlaces proves the simulation table's tiebreak (#1724, #2446):
// two components that share a name each read back their own primary system and
// the places from the root down to their location, an unplaced component reads
// empty, and an id that is no component has no entry.
func TestComponentPlaces(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	for _, l := range []storage.LocationSpec{
		{Name: "hq", LocationType: "campus"},
		{Name: "level-2", LocationType: "building", ParentName: strptr("hq")},
		{Name: "annex", LocationType: "campus"},
	} {
		if _, err := gw.CreateLocation(ctx, "", l, all, all); err != nil {
			t.Fatalf("create location %s: %v", l.Name, err)
		}
	}
	for _, s := range []storage.SystemSpec{{Name: "board-1", LocationName: strptr("level-2")}, {Name: "board-2", LocationName: strptr("annex")}} {
		if _, err := gw.CreateSystem(ctx, "", s, all, all); err != nil {
			t.Fatalf("create system %s: %v", s.Name, err)
		}
	}
	prod := storagetest.MintProduct(t, ctx, gw, "")
	mk := func(spec storage.ComponentSpec) string {
		t.Helper()
		spec.ProductName = &prod.Name
		c, err := gw.CreateComponent(ctx, "", spec, all, all, all, all)
		if err != nil {
			t.Fatalf("create component %+v: %v", spec, err)
		}
		return c.ID
	}
	a := mk(storage.ComponentSpec{Name: "amp-1", LocationName: strptr("level-2"), SystemName: strptr("board-1")})
	b := mk(storage.ComponentSpec{Name: "amp-1", LocationName: strptr("annex"), SystemName: strptr("board-2")})
	loose := mk(storage.ComponentSpec{Name: "loose-1"})

	got, err := gw.ComponentPlaces(ctx, []string{a, b, loose, "00000000-0000-0000-0000-000000000000"})
	if err != nil {
		t.Fatalf("component places: %v", err)
	}
	if p := got[a]; p.System != "board-1" || len(p.Path) != 2 || p.Path[0] != "hq" || p.Path[1] != "level-2" {
		t.Fatalf("first amp-1 = %+v, want board-1 at hq, level-2", p)
	}
	if p := got[b]; p.System != "board-2" || len(p.Path) != 1 || p.Path[0] != "annex" {
		t.Fatalf("second amp-1 = %+v, want board-2 at annex", p)
	}
	if p := got[loose]; p.System != "" || len(p.Path) != 0 {
		t.Fatalf("unplaced = %+v, want nothing", p)
	}
	if _, ok := got["00000000-0000-0000-0000-000000000000"]; ok || len(got) != 3 {
		t.Fatalf("places = %+v, want only the three real components", got)
	}
}
