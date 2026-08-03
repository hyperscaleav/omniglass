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

// TestListEventFeed covers the fleet-wide occurrence feed the event-feed view
// runs: newest-first with a TOTAL order (ts then id, so a same-instant pair
// cannot reorder between runs), keyset paging that walks the whole set without
// duplicates or gaps, the optional owner filter, and the scope filter bounding
// the rows in the query itself.
func TestListEventFeed(t *testing.T) {
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

	disp := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "disp"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "cam"}, all)

	// Six occurrences: four on disp, two on cam. Two share an instant, so the
	// id tie-break is exercised rather than assumed.
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	writes := []storage.EventWrite{
		{OwnerKind: "component", OwnerID: "disp", Key: "call.started", Message: "e1", Source: "t", TS: base},
		{OwnerKind: "component", OwnerID: "cam", Key: "call.started", Message: "e2", Source: "t", TS: base.Add(time.Minute)},
		{OwnerKind: "component", OwnerID: "disp", Key: "call.started", Message: "e3", Source: "t", TS: base.Add(2 * time.Minute)},
		{OwnerKind: "component", OwnerID: "disp", Key: "call.started", Message: "e4", Source: "t", TS: base.Add(3 * time.Minute)},
		{OwnerKind: "component", OwnerID: "cam", Key: "call.started", Message: "e5", Source: "t", TS: base.Add(3 * time.Minute)},
		{OwnerKind: "component", OwnerID: "disp", Key: "call.started", Message: "e6", Source: "t", TS: base.Add(4 * time.Minute)},
	}
	for _, w := range writes {
		if err := gw.InsertEvents(ctx, []storage.EventWrite{w}); err != nil {
			t.Fatalf("insert %s: %v", w.Message, err)
		}
	}

	// Newest first across the whole estate, owner names resolved.
	page, err := gw.ListEventFeed(ctx, all, storage.EventFeedQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page) != 6 {
		t.Fatalf("rows = %d, want 6", len(page))
	}
	if page[0].Message != "e6" || page[0].Owner != "disp" {
		t.Errorf("newest row = %+v, want e6 on disp", page[0])
	}
	if page[0].Key != "call.started" {
		t.Errorf("event type = %q, want call.started", page[0].Key)
	}
	// The same-instant pair orders by id descending (e5 was written after e4).
	if page[1].Message != "e5" || page[2].Message != "e4" {
		t.Errorf("tie order = %s,%s, want e5,e4 (higher id first)", page[1].Message, page[2].Message)
	}

	// Keyset paging walks the full set: two pages of four then two, with no
	// duplicate and no gap, INCLUDING across the same-instant boundary.
	first, err := gw.ListEventFeed(ctx, all, storage.EventFeedQuery{Limit: 4})
	if err != nil || len(first) != 4 {
		t.Fatalf("first page = %d rows, err %v", len(first), err)
	}
	last := first[len(first)-1]
	second, err := gw.ListEventFeed(ctx, all, storage.EventFeedQuery{Limit: 4, BeforeTS: last.TS, BeforeID: last.ID})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range append(append([]storage.EventFeedRow{}, first...), second...) {
		if seen[r.Message] {
			t.Errorf("duplicate row across pages: %s", r.Message)
		}
		seen[r.Message] = true
	}
	if len(seen) != 6 {
		t.Errorf("paged walk saw %d of 6 rows: %v", len(seen), seen)
	}

	// The owner filter narrows to one component.
	only, err := gw.ListEventFeed(ctx, all, storage.EventFeedQuery{Limit: 10, Owner: "cam"})
	if err != nil {
		t.Fatalf("owner filter: %v", err)
	}
	if len(only) != 2 {
		t.Fatalf("cam rows = %d, want 2", len(only))
	}
	for _, r := range only {
		if r.Owner != "cam" {
			t.Errorf("owner filter leaked %+v", r)
		}
	}

	// Scope bounds the feed: a scope rooted at disp never sees cam's rows.
	scoped, err := gw.ListEventFeed(ctx, scope.Set{IDs: []string{disp.ID}}, storage.EventFeedQuery{Limit: 10})
	if err != nil {
		t.Fatalf("scoped: %v", err)
	}
	if len(scoped) != 4 {
		t.Fatalf("disp-scope rows = %d, want 4", len(scoped))
	}
	for _, r := range scoped {
		if r.Owner != "disp" {
			t.Errorf("scope leaked %+v", r)
		}
	}
	none, err := gw.ListEventFeed(ctx, scope.Set{}, storage.EventFeedQuery{Limit: 10})
	if err != nil || len(none) != 0 {
		t.Fatalf("empty scope = %d rows, err %v, want 0", len(none), err)
	}
}

// TestListSampleHistory covers the series read behind the sample-history view:
// both sample lanes (numeric metric and categorical state) for one owner and
// key, oldest-first in a total order, bounded by the window, and narrowable to
// one instance.
func TestListSampleHistory(t *testing.T) {
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
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "disp"}, all)

	now := time.Now().UTC().Truncate(time.Microsecond)
	old := now.Add(-48 * time.Hour)
	// Three in-window rtt readings on two instances, plus one outside it.
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "disp", Key: "icmp.rtt_avg", Instance: "eth0", Value: 3, Source: "icmp", TS: now.Add(-2 * time.Hour)},
		{OwnerKind: "component", OwnerID: "disp", Key: "icmp.rtt_avg", Instance: "eth0", Value: 4, Source: "icmp", TS: now.Add(-time.Hour)},
		{OwnerKind: "component", OwnerID: "disp", Key: "icmp.rtt_avg", Instance: "eth1", Value: 9, Source: "icmp", TS: now.Add(-90 * time.Minute)},
		{OwnerKind: "component", OwnerID: "disp", Key: "icmp.rtt_avg", Instance: "eth0", Value: 99, Source: "icmp", TS: old},
	}); err != nil {
		t.Fatalf("insert metrics: %v", err)
	}

	since := now.Add(-24 * time.Hour)
	rows, err := gw.ListSampleHistory(ctx, all, storage.SampleHistoryQuery{Owner: "disp", Key: "icmp.rtt_avg", Since: since, Limit: 100})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("in-window rows = %d, want 3 (the 48h-old reading is outside)", len(rows))
	}
	// Oldest first, so a chart plots left to right without re-sorting.
	if !rows[0].TS.Before(rows[1].TS) || !rows[1].TS.Before(rows[2].TS) {
		t.Errorf("series is not oldest-first: %+v", rows)
	}
	if rows[0].Value == nil || *rows[0].Value != 3 {
		t.Errorf("first value = %v, want 3", rows[0].Value)
	}
	if rows[0].State != "" {
		t.Errorf("a metric row carries a state value: %+v", rows[0])
	}

	// One instance narrows the series.
	one, err := gw.ListSampleHistory(ctx, all, storage.SampleHistoryQuery{Owner: "disp", Key: "icmp.rtt_avg", Instance: "eth1", Since: since, Limit: 100})
	if err != nil || len(one) != 1 || one[0].Instance != "eth1" {
		t.Fatalf("instance filter = %+v, err %v, want the single eth1 reading", one, err)
	}

	// The state lane reads through the same call: a categorical series carries
	// its value as text with a null number, so one shape serves both lanes.
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp", Key: "interface.reachable", Instance: "eth0", Value: "up", Source: "icmp", TS: now.Add(-3 * time.Hour)},
	}); err != nil {
		t.Fatalf("insert state: %v", err)
	}
	states, err := gw.ListSampleHistory(ctx, all, storage.SampleHistoryQuery{Owner: "disp", Key: "interface.reachable", Since: since, Limit: 100})
	if err != nil {
		t.Fatalf("state history: %v", err)
	}
	if len(states) != 1 || states[0].State != "up" || states[0].Value != nil {
		t.Fatalf("state series = %+v, want one up row with no numeric value", states)
	}
}

// TestEstateCounts covers the stat tiles: component-tier counts, each bounded
// by the caller's scope, in a fixed declared order.
func TestEstateCounts(t *testing.T) {
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
	disp := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "disp"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "cam"}, all)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, params) values
		('disp-icmp', (select id from interface_type where name = 'icmp'), (select id from component where name = 'disp'), '{}'::jsonb),
		('disp-tcp',  (select id from interface_type where name = 'tcp'),  (select id from component where name = 'disp'), '{}'::jsonb),
		('cam-icmp',  (select id from interface_type where name = 'icmp'), (select id from component where name = 'cam'),  '{}'::jsonb)`); err != nil {
		t.Fatalf("insert interfaces: %v", err)
	}
	ts := time.Now().UTC().Truncate(time.Microsecond)
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp", Key: "interface.reachable", Instance: "disp-icmp", Value: "up", Source: "icmp", TS: ts},
		{OwnerKind: "component", OwnerID: "disp", Key: "interface.reachable", Instance: "disp-tcp", Value: "down", Source: "tcp", TS: ts},
		{OwnerKind: "component", OwnerID: "cam", Key: "interface.reachable", Instance: "cam-icmp", Value: "up", Source: "icmp", TS: ts},
	}); err != nil {
		t.Fatalf("insert verdicts: %v", err)
	}

	counts, err := gw.EstateCounts(ctx, all)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts.Components != 2 || counts.Interfaces != 3 {
		t.Errorf("all-scope counts = %+v, want 2 components and 3 interfaces", counts)
	}
	if counts.InterfacesUp != 2 || counts.InterfacesDown != 1 {
		t.Errorf("verdict counts = %+v, want 2 up and 1 down", counts)
	}

	// Scope bounds every tile: disp alone has two interfaces, one of each.
	scoped, err := gw.EstateCounts(ctx, scope.Set{IDs: []string{disp.ID}})
	if err != nil {
		t.Fatalf("scoped counts: %v", err)
	}
	if scoped.Components != 1 || scoped.Interfaces != 2 || scoped.InterfacesUp != 1 || scoped.InterfacesDown != 1 {
		t.Errorf("disp-scope counts = %+v, want 1/2/1/1", scoped)
	}
	empty, err := gw.EstateCounts(ctx, scope.Set{})
	if err != nil {
		t.Fatalf("empty-scope counts: %v", err)
	}
	if empty.Components != 0 || empty.Interfaces != 0 {
		t.Errorf("empty-scope counts = %+v, want zeros", empty)
	}
}
