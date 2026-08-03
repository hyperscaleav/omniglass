package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestListInterfaceReachability covers the fleet-wide reachability read the
// component-reachability view runs: every in-scope component's interfaces with
// the LATEST interface.reachable verdict per interface (late rows lose, ties
// break on id), a never-probed interface surfacing with no verdict, and the
// scope filter bounding the rows exactly like every other component read.
func TestListInterfaceReachability(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Two root components: disp (two interfaces) and cam (one, never probed).
	disp := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "disp"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "cam"}, all)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, params) values
		('disp-icmp', (select id from interface_type where name = 'icmp'), (select id from component where name = 'disp'), '{"target":"10.0.0.1"}'::jsonb),
		('disp-tcp',  (select id from interface_type where name = 'tcp'),  (select id from component where name = 'disp'), '{"target":"10.0.0.1","port":5000}'::jsonb),
		('cam-icmp',  (select id from interface_type where name = 'icmp'), (select id from component where name = 'cam'),  '{"target":"10.0.0.2"}'::jsonb)`); err != nil {
		t.Fatalf("insert interfaces: %v", err)
	}

	// disp-icmp flips up then down (latest wins); disp-tcp has one up verdict;
	// cam-icmp is never probed.
	// Microsecond precision: timestamps round-trip through Postgres timestamptz.
	t0 := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Minute)
	t1 := t0.Add(time.Minute)
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp", Key: "interface.reachable", Instance: "disp-icmp", Value: "up", Source: "icmp", TS: t0},
	}); err != nil {
		t.Fatalf("insert up: %v", err)
	}
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp", Key: "interface.reachable", Instance: "disp-icmp", Value: "down", Source: "icmp", TS: t1},
	}); err != nil {
		t.Fatalf("insert verdicts: %v", err)
	}
	// A LATE ARRIVAL: an older observation written after the current verdict
	// (newer id, older ts). Observation time must win, so down stays latest;
	// an id-ordered read would wrongly resurrect this row.
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp", Key: "interface.reachable", Instance: "disp-icmp", Value: "up", Source: "icmp", TS: t0.Add(-time.Minute)},
	}); err != nil {
		t.Fatalf("insert late arrival: %v", err)
	}
	// A same-instant pair on disp-tcp: the higher id (the later write) must
	// win the tie, LatestState's exact rule.
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp", Key: "interface.reachable", Instance: "disp-tcp", Value: "down", Source: "tcp", TS: t1},
	}); err != nil {
		t.Fatalf("insert tcp first: %v", err)
	}
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp", Key: "interface.reachable", Instance: "disp-tcp", Value: "up", Source: "tcp", TS: t1},
	}); err != nil {
		t.Fatalf("insert tcp tie-breaker: %v", err)
	}

	// The all scope sees every interface, ordered component then interface.
	rows, err := gw.ListInterfaceReachability(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("all-scope rows = %d, want 3: %+v", len(rows), rows)
	}
	if rows[0].Component != "cam" || rows[1].Component != "disp" || rows[2].Component != "disp" {
		t.Errorf("component order = %s,%s,%s, want cam,disp,disp", rows[0].Component, rows[1].Component, rows[2].Component)
	}
	if rows[1].Interface != "disp-icmp" || rows[2].Interface != "disp-tcp" {
		t.Errorf("interface order under disp = %s,%s, want disp-icmp,disp-tcp", rows[1].Interface, rows[2].Interface)
	}
	// The never-probed interface has no verdict; a probed one carries the LATEST.
	if rows[0].Value != "" || rows[0].TS != nil {
		t.Errorf("cam-icmp: want no verdict, got %q at %v", rows[0].Value, rows[0].TS)
	}
	if rows[1].Value != "down" {
		t.Errorf("disp-icmp latest = %q, want down (observation time wins over the late arrival)", rows[1].Value)
	}
	if rows[1].TS == nil || !rows[1].TS.Equal(t1) {
		t.Errorf("disp-icmp ts = %v, want %v", rows[1].TS, t1)
	}
	if rows[2].Value != "up" {
		t.Errorf("disp-tcp latest = %q, want up (the higher id wins the same-instant tie)", rows[2].Value)
	}
	if rows[1].Type != "icmp" || rows[2].Type != "tcp" {
		t.Errorf("interface types = %s,%s, want icmp,tcp", rows[1].Type, rows[2].Type)
	}

	// A scope rooted at disp sees only disp's interfaces; cam is filtered in
	// the query itself, not post-hoc.
	scoped, err := gw.ListInterfaceReachability(ctx, scope.Set{IDs: []string{disp.ID}})
	if err != nil {
		t.Fatalf("scoped list: %v", err)
	}
	if len(scoped) != 2 || scoped[0].Component != "disp" || scoped[1].Component != "disp" {
		t.Fatalf("disp-scope rows = %+v, want only disp's 2", scoped)
	}

	// The empty scope admits nothing.
	none, err := gw.ListInterfaceReachability(ctx, scope.Set{})
	if err != nil || len(none) != 0 {
		t.Fatalf("empty-scope rows = %d, err %v, want 0", len(none), err)
	}
}
