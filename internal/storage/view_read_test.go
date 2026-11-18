package storage_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/tail"
)

func viewGateway(t *testing.T) (context.Context, *storage.PG, *storage.Component, *storage.Component) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	a, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "view-a"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	b, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "view-b"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	return ctx, gw, a, b
}

// Each primitive reads its history through one binding: a metric's raw points
// and its buckets, a property's changes with the value carried in, events and
// log lines newest first a page at a time, all under one tail cursor.
func TestReadViewsEachPrimitive(t *testing.T) {
	t.Parallel()
	ctx, gw, a, _ := viewGateway(t)
	hour := time.Now().UTC().Truncate(time.Hour)
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "view-a", Key: "icmp-rtt-avg", Instance: "icmp", Value: 5, Source: "t", TS: hour.Add(-2*time.Hour + 5*time.Minute)},
		{OwnerKind: "component", OwnerID: "view-a", Key: "icmp-rtt-avg", Instance: "icmp", Value: 9, Source: "t", TS: hour.Add(-2*time.Hour + 35*time.Minute)},
		{OwnerKind: "component", OwnerID: "view-a", Key: "icmp-rtt-avg", Instance: "icmp", Value: 2, Source: "t", TS: hour.Add(-1*time.Hour + 5*time.Minute)},
	}); err != nil {
		t.Fatalf("insert metrics: %v", err)
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "view-a", Key: "icmp-reachable", Instance: "icmp", Value: "true", Source: "t", TS: hour.Add(-5 * time.Hour)},
		{OwnerKind: "component", OwnerID: "view-a", Key: "icmp-reachable", Instance: "icmp", Value: "false", Source: "t", TS: hour.Add(-90 * time.Minute)},
	}); err != nil {
		t.Fatalf("insert properties: %v", err)
	}
	if err := insertEvents(ctx, gw, []storage.EventWrite{
		{OwnerKind: "component", OwnerID: "view-a", Key: "call-started", Origin: "caught", Message: "one", Source: "t", TS: hour.Add(-50 * time.Minute)},
		{OwnerKind: "component", OwnerID: "view-a", Key: "call-started", Origin: "caught", Message: "two", Source: "t", TS: hour.Add(-40 * time.Minute)},
		{OwnerKind: "component", OwnerID: "view-a", Key: "call-started", Origin: "caught", Message: "three", Source: "t", TS: hour.Add(-30 * time.Minute)},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if err := gw.InsertLogLines(ctx, []storage.LogLineWrite{
		{OwnerKind: "component", OwnerID: "view-a", Source: "syslog", Severity: "info", Message: "hello", TS: hour.Add(-10 * time.Minute)},
		{OwnerKind: "component", OwnerID: "view-a", Source: "syslog", Severity: "error", Message: "boom", TS: hour.Add(-5 * time.Minute)},
	}); err != nil {
		t.Fatalf("insert logs: %v", err)
	}

	all := scope.Set{All: true}
	from := hour.Add(-3 * time.Hour)
	ref := func(p tail.Lane) storage.ViewBinding {
		return storage.ViewBinding{Primitive: p, Kind: "component", Refs: []string{"view-a"}, From: from, Read: all}
	}
	raw, buckets, prop, ev, logs := ref(tail.LaneMetric), ref(tail.LaneMetric), ref(tail.LaneProperty), ref(tail.LaneEvent), ref(tail.LaneLog)
	raw.Keys, buckets.Keys, prop.Keys, ev.Keys = []string{"icmp-rtt-avg"}, []string{"icmp-rtt-avg"}, []string{"icmp-reachable"}, []string{"call-started"}
	buckets.Step = time.Hour
	ev.Limit = 2
	logs.Logs = storage.LogFilter{Severity: "error"}
	snap, err := gw.ReadViews(ctx, []storage.ViewBinding{raw, buckets, prop, ev, logs})
	if err != nil {
		t.Fatalf("read views: %v", err)
	}
	if snap.Cursor == "" || len(snap.Results) != 5 {
		t.Fatalf("snapshot cursor %q, %d results", snap.Cursor, len(snap.Results))
	}

	r := snap.Results[0]
	if len(r.Entities) != 1 || r.Entities[0].ID != a.ID || r.Entities[0].Name != "view-a" {
		t.Fatalf("entities: %+v", r.Entities)
	}
	if len(r.Metric) != 1 || r.Metric[0].EntityID != a.ID || r.Metric[0].Instance != "icmp" || len(r.Metric[0].Points) != 3 || r.Metric[0].Points[2].Value != 2 {
		t.Fatalf("raw metric: %+v", r.Metric)
	}

	bs := snap.Results[1].Metric
	if len(bs) != 1 || len(bs[0].Buckets) != 4 {
		t.Fatalf("metric buckets: %+v", bs)
	}
	if b := bs[0].Buckets[1]; !b.TS.Equal(hour.Add(-2*time.Hour)) || b.Count != 2 || *b.Max != 9 || *b.Avg != 7 || *b.Last != 9 {
		t.Fatalf("the two-sample bucket: %+v", b)
	}
	if b := bs[0].Buckets[0]; b.Count != 0 || b.Avg != nil {
		t.Fatalf("an empty bucket: %+v", b)
	}

	ps := snap.Results[2].Property
	if len(ps) != 1 || ps[0].CarriedIn == nil || *ps[0].CarriedIn != "true" || len(ps[0].Points) != 1 || ps[0].Points[0].Value != "false" {
		t.Fatalf("property: %+v", ps)
	}

	er := snap.Results[3]
	if len(er.Events) != 2 || er.Events[0].Message != "three" || er.Events[1].Message != "two" || er.NextPage == "" {
		t.Fatalf("event page: %+v next %q", er.Events, er.NextPage)
	}
	ev.Page = er.NextPage
	more, err := gw.ReadViews(ctx, []storage.ViewBinding{ev})
	if err != nil {
		t.Fatalf("next event page: %v", err)
	}
	if got := more.Results[0]; len(got.Events) != 1 || got.Events[0].Message != "one" || got.NextPage != "" {
		t.Fatalf("second event page: %+v next %q", got.Events, got.NextPage)
	}

	lr := snap.Results[4]
	if len(lr.Logs) != 1 || lr.Logs[0].Message != "boom" || lr.Logs[0].Component != a.ID {
		t.Fatalf("log page: %+v", lr.Logs)
	}
}

// A selector resolves only inside the binding's read scope: a kind selector
// lists only the visible entities, and a literal ref to one outside it is the
// same not-found an absent one is.
func TestReadViewsResolvesInScope(t *testing.T) {
	t.Parallel()
	ctx, gw, a, _ := viewGateway(t)
	only := scope.Set{SelfIDs: []string{a.ID}, Resource: "component"}
	ents, err := gw.ResolveViewEntities(ctx, storage.ViewBinding{Primitive: tail.LaneMetric, Kind: "component", Read: only})
	if err != nil {
		t.Fatalf("resolve kind selector: %v", err)
	}
	if len(ents) != 1 || ents[0].ID != a.ID {
		t.Fatalf("a kind selector in scope resolved %+v, want only view-a", ents)
	}
	for _, ref := range []string{"view-b", "no-such-component"} {
		_, err := gw.ReadViews(ctx, []storage.ViewBinding{{Primitive: tail.LaneMetric, Kind: "component", Refs: []string{ref}, Keys: []string{"icmp-rtt-avg"}, Window: time.Hour, Read: only}})
		if !errors.Is(err, storage.ErrViewEntityNotFound) {
			t.Fatalf("ref %q: %v, want ErrViewEntityNotFound", ref, err)
		}
	}
}

// The history/live seam (ADR-1830): with samples landing while snapshots are
// taken, every sample is in exactly one of a snapshot's history and the tail
// after its cursor: none missing, none in both.
func TestReadViewsSeamHasNoGapOrDuplicate(t *testing.T) {
	t.Parallel()
	ctx, gw, a, _ := viewGateway(t)
	const n = 120
	start := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	binding := storage.ViewBinding{Primitive: tail.LaneMetric, Kind: "component", Refs: []string{"view-a"}, Keys: []string{"icmp-rtt-avg"}, Window: time.Hour, Read: scope.Set{All: true}}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
				{OwnerKind: "component", OwnerID: "view-a", Key: "icmp-rtt-avg", Instance: "icmp", Value: float64(i), Source: "t", TS: start.Add(time.Duration(i) * time.Second)},
			}); err != nil {
				t.Errorf("insert %d: %v", i, err)
				return
			}
		}
	}()
	var snaps []storage.ViewSnapshot
	for len(snaps) < 8 {
		s, err := gw.ReadViews(ctx, []storage.ViewBinding{binding})
		if err != nil {
			t.Fatalf("read views: %v", err)
		}
		snaps = append(snaps, s)
	}
	wg.Wait()

	for k, s := range snaps {
		seen := map[float64]int{}
		for _, series := range s.Results[0].Metric {
			for _, p := range series.Points {
				seen[p.Value]++
			}
		}
		commits, _, err := gw.ViewTail().Read(s.Cursor, 0)
		if err != nil {
			t.Fatalf("snapshot %d tail: %v", k, err)
		}
		for _, c := range commits {
			for _, row := range c.Rows {
				if row.Lane == tail.LaneMetric && row.OwnerID == a.ID && row.Key == "icmp-rtt-avg" {
					seen[row.Number]++
				}
			}
		}
		for i := 0; i < n; i++ {
			if seen[float64(i)] != 1 {
				t.Fatalf("snapshot %d: sample %d seen %d times across history and tail, want exactly once", k, i, seen[float64(i)])
			}
		}
	}
}

// An open-ended range has no upper bound in history, as it has none on the
// tail: a sample stamped ahead of the server's clock (a collector whose clock
// runs fast) that landed before the cursor is in the history, not lost
// between the two.
func TestReadViewsOpenRangeKeepsAheadOfClockRows(t *testing.T) {
	t.Parallel()
	ctx, gw, _, _ := viewGateway(t)
	ahead := time.Now().UTC().Add(2 * time.Minute)
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "view-a", Key: "icmp-rtt-avg", Instance: "icmp", Value: 7, Source: "t", TS: ahead},
	}); err != nil {
		t.Fatalf("insert metric: %v", err)
	}
	if err := insertEvents(ctx, gw, []storage.EventWrite{
		{OwnerKind: "component", OwnerID: "view-a", Key: "call-started", Origin: "caught", Message: "ahead", Source: "t", TS: ahead},
	}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	all := scope.Set{All: true}
	snap, err := gw.ReadViews(ctx, []storage.ViewBinding{
		{Primitive: tail.LaneMetric, Kind: "component", Refs: []string{"view-a"}, Keys: []string{"icmp-rtt-avg"}, Window: time.Hour, Read: all},
		{Primitive: tail.LaneEvent, Kind: "component", Refs: []string{"view-a"}, Window: time.Hour, Read: all},
	})
	if err != nil {
		t.Fatalf("read views: %v", err)
	}
	if m := snap.Results[0].Metric; len(m) != 1 || len(m[0].Points) != 1 || m[0].Points[0].Value != 7 {
		t.Fatalf("an ahead-of-clock sample is missing from an open range's history: %+v", m)
	}
	if e := snap.Results[1].Events; len(e) != 1 || e[0].Message != "ahead" {
		t.Fatalf("an ahead-of-clock event is missing from an open range's history: %+v", e)
	}
}

// A view read never holds the tail's fence while it waits on the pool: a
// sample writer holds its connection when it reaches the fence, so a read
// fencing first and then waiting for a connection would wait on the writer it
// fences out. With one connection, a writer parked between its begin and its
// commit, and a read queued behind it, the commit still lands and the read
// then answers from after it.
func TestReadViewsDoesNotFenceIngestWhileWaitingOnThePool(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t)+"&pool_max_conns=1")
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	commit, err := gw.ExportBeginSampleWrite(ctx)
	if err != nil {
		t.Fatalf("begin write: %v", err)
	}
	readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	type read struct {
		snap storage.ViewSnapshot
		err  error
	}
	reads := make(chan read, 1)
	go func() {
		snap, err := gw.ReadViews(readCtx, nil)
		reads <- read{snap, err}
	}()
	// The read is queued on the one connection the writer holds.
	time.Sleep(300 * time.Millisecond)
	committed := make(chan error, 1)
	go func() {
		committed <- commit([]tail.Row{{Lane: tail.LaneMetric, OwnerKind: "component", OwnerID: "x", Key: "k", TS: time.Now()}})
	}()
	select {
	case err := <-committed:
		if err != nil {
			t.Fatalf("commit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the writer's commit is blocked behind a view read waiting on the pool")
	}
	r := <-reads
	if r.err != nil {
		t.Fatalf("read views: %v", r.err)
	}
	if want := gw.ViewTail().Cursor(); r.snap.Cursor != want {
		t.Fatalf("the read's cursor %q, want %q: it was taken after the commit", r.snap.Cursor, want)
	}
}
