package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// platformViewFixture is two nodes and one server on a seeded platform: the
// nodes carry platform samples, the server its boot identity, and the
// platform system a recorded verdict.
type platformViewFixture struct {
	gw        *storage.PG
	ctx       context.Context
	alpha     *storage.Component
	bravo     *storage.Component
	server    string
	startedAt time.Time
}

func newPlatformViewFixture(t *testing.T) platformViewFixture {
	t.Helper()
	gw, ctx, dsn := platformGatewayDSN(t)
	f := platformViewFixture{gw: gw, ctx: ctx}
	f.alpha = platformNode(t, ctx, gw, "edge-alpha")
	f.bravo = platformNode(t, ctx, gw, "edge-bravo")
	var err error
	if f.server, err = gw.EnsureServerComponent(ctx, "lab-server"); err != nil {
		t.Fatalf("ensure server component: %v", err)
	}
	f.startedAt = time.Date(2026, 11, 9, 9, 30, 0, 0, time.UTC)
	if err := gw.RecordServerIdentity(ctx, f.server, "v1.2.3", f.startedAt); err != nil {
		t.Fatalf("record server identity: %v", err)
	}

	older := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	newer := older.Add(30 * time.Second)
	if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{
		{ComponentID: f.alpha.ID, Key: writerclass.CheckinAge, Value: 90, TS: older},
		{ComponentID: f.alpha.ID, Key: writerclass.CheckinAge, Value: 3, TS: newer},
		{ComponentID: f.alpha.ID, Key: writerclass.CommandPendingCount, Value: 2, TS: newer},
		{ComponentID: f.bravo.ID, Key: writerclass.CheckinAge, Value: 400, TS: newer},
	}); err != nil {
		t.Fatalf("record platform metrics: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `
		insert into property (owner_kind, system_id, property_type_id, instance, value, provenance, source_rule)
		values ('system', (select id from system where name = $1),
		        (select id from property_type where namespace = 'og' and name = 'health'), '', to_jsonb('degraded'::text), 'calculated', 'test')`,
		storage.PlatformSystemName); err != nil {
		t.Fatalf("record the platform verdict: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into property (owner_kind, component_id, property_type_id, instance, value, provenance, source_rule)
		values ('component', $1::uuid,
		        (select id from property_type where namespace = 'og' and name = 'health'), '', to_jsonb('outage'::text), 'calculated', 'test')`,
		f.bravo.ID); err != nil {
		t.Fatalf("record edge-bravo's verdict: %v", err)
	}
	return f
}

// TestPlatformViewReadsTheWholePlatform: an all-scope caller gets every node's
// latest figure per key, the server's version and start time, and the
// platform system's verdict, in one read.
func TestPlatformViewReadsTheWholePlatform(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	v, err := f.gw.PlatformView(f.ctx, all, all, all)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}

	if v.System == nil || v.System.Name != storage.PlatformSystemName || v.System.Verdict != "degraded" {
		t.Errorf("system = %+v, want the platform system, degraded", v.System)
	}
	if len(v.Servers) != 1 {
		t.Fatalf("servers = %+v, want the one server", v.Servers)
	}
	s := v.Servers[0]
	if s.ComponentID != f.server || s.Version == nil || *s.Version != "v1.2.3" ||
		s.StartedAt == nil || !s.StartedAt.Equal(f.startedAt) || s.Verdict != "healthy" {
		t.Errorf("server = %+v, want v1.2.3 started %s, healthy", s, f.startedAt)
	}
	if !v.FiguresRead {
		t.Error("figures read = false for an all-scope component read")
	}
	if len(v.Groups) != len(storage.PlatformViewGroups) || len(v.Keys) == 0 {
		t.Errorf("groups = %d, keys = %d, want every group and its catalog keys", len(v.Groups), len(v.Keys))
	}

	rows := map[string]storage.PlatformNode{}
	for _, n := range v.Nodes {
		rows[n.Name] = n
	}
	if len(rows) != 2 {
		t.Fatalf("nodes = %+v, want edge-alpha and edge-bravo", v.Nodes)
	}
	alpha := rows["edge-alpha"]
	if got := alpha.Figures[writerclass.CheckinAge]; got.Value != 3 {
		t.Errorf("edge-alpha checkin-age = %+v, want the latest, 3", got)
	}
	if got := alpha.Figures[writerclass.CommandPendingCount]; got.Value != 2 {
		t.Errorf("edge-alpha command-pending-count = %+v, want 2", got)
	}
	if _, ok := alpha.Figures[writerclass.ScheduleLateness]; ok {
		t.Error("edge-alpha reports a schedule-lateness it never sampled: a missing figure must be absent, not zero")
	}
	if alpha.Verdict == nil || *alpha.Verdict != "healthy" {
		t.Errorf("edge-alpha verdict = %v, want healthy", alpha.Verdict)
	}
	bravo := rows["edge-bravo"]
	if got := bravo.Figures[writerclass.CheckinAge]; got.Value != 400 {
		t.Errorf("edge-bravo checkin-age = %+v, want 400", got)
	}
	if bravo.Verdict == nil || *bravo.Verdict != "outage" {
		t.Errorf("edge-bravo verdict = %v, want outage", bravo.Verdict)
	}
	if bravo.ComponentID == nil || *bravo.ComponentID != f.bravo.ID {
		t.Errorf("edge-bravo component = %v, want %s", bravo.ComponentID, f.bravo.ID)
	}
}

// TestPlatformViewWithoutPlatformReach: a component read short of all-scope
// (a location-scoped holder) reaches no platform component, so the rows come
// back with no figures, no verdicts and no servers; a system read short of
// all-scope leaves the system null. Omitted, not zeroed.
func TestPlatformViewWithoutPlatformReach(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	located := scope.Set{IDs: []string{"00000000-0000-0000-0000-0000000000aa"}}
	v, err := f.gw.PlatformView(f.ctx, all, located, located)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}
	if v.System != nil {
		t.Errorf("system = %+v, want null without an all-scope system read", v.System)
	}
	if v.FiguresRead || len(v.Servers) != 0 {
		t.Errorf("figures read = %v, servers = %+v, want neither", v.FiguresRead, v.Servers)
	}
	if len(v.Nodes) != 2 {
		t.Fatalf("nodes = %+v, want both rows", v.Nodes)
	}
	for _, n := range v.Nodes {
		if n.Figures != nil || n.Verdict != nil {
			t.Errorf("%s figures = %v, verdict = %v, want both omitted", n.Name, n.Figures, n.Verdict)
		}
	}
}

// TestPlatformViewNeedsAnAllScopeNodeRead: the rows are ListNodes', so a
// location-scoped node read is ErrNodeForbidden, not an empty table.
func TestPlatformViewNeedsAnAllScopeNodeRead(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	located := scope.Set{IDs: []string{"00000000-0000-0000-0000-0000000000aa"}}
	if _, err := gw.PlatformView(ctx, located, all, all); !errors.Is(err, storage.ErrNodeForbidden) {
		t.Errorf("platform view with a scoped node read = %v, want ErrNodeForbidden", err)
	}
}

// TestRecordServerIdentityRefusesAMissingComponent: the boot's identity write
// onto a component that is gone is ErrComponentNotFound, which the boot logs.
func TestRecordServerIdentityRefusesAMissingComponent(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	err := gw.RecordServerIdentity(ctx, "00000000-0000-0000-0000-000000000001", "v1", time.Now())
	if !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("identity on a missing component = %v, want ErrComponentNotFound", err)
	}
}

// TestPlatformViewReadsAWindowCountAsItsNewestWindow: a window count is
// written only when nonzero, once a window, so the figure is the newest
// window's, summed across the instances that split it, and a count no
// window has repeated within two report windows is absent: that window is
// over and the ones since counted nothing. A gauge never ages out, and
// neither does a count written every window, 0 when quiet (ADR-1625): its
// newest sample is current.
func TestPlatformViewReadsAWindowCountAsItsNewestWindow(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	window, earlier, gone := now.Add(-5*time.Second), now.Add(-20*time.Second), now.Add(-10*time.Minute)
	if err := f.gw.RecordPlatformMetrics(f.ctx, []storage.PlatformMetric{
		// edge-alpha's earlier pass expired seven unexecuted, its newest two
		// undelivered and one unexecuted: the newest window is the figure,
		// summed over its instances, not the total of every pass.
		{ComponentID: f.alpha.ID, Key: writerclass.CommandExpiredCount, Instance: "unexecuted", Value: 7, TS: earlier},
		{ComponentID: f.alpha.ID, Key: writerclass.CommandExpiredCount, Instance: "undelivered", Value: 2, TS: window},
		{ComponentID: f.alpha.ID, Key: writerclass.CommandExpiredCount, Instance: "unexecuted", Value: 1, TS: window},
		// edge-bravo expired five commands ten minutes ago and none since,
		// and its last report window, as long ago, counted five overruns.
		{ComponentID: f.bravo.ID, Key: writerclass.CommandExpiredCount, Instance: "undelivered", Value: 5, TS: gone},
		{ComponentID: f.bravo.ID, Key: writerclass.TickOverrunCount, Value: 5, TS: gone},
	}); err != nil {
		t.Fatalf("record platform metrics: %v", err)
	}
	v, err := f.gw.PlatformView(f.ctx, all, all, all)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}
	rows := map[string]storage.PlatformNode{}
	for _, n := range v.Nodes {
		rows[n.Name] = n
	}
	alpha, bravo := rows["edge-alpha"], rows["edge-bravo"]
	if got := alpha.Figures[writerclass.CommandExpiredCount]; got.Value != 3 || !got.ObservedAt.Equal(window) {
		t.Errorf("edge-alpha command-expired-count = %+v, want 3 (undelivered 2 + unexecuted 1) at %s", got, window)
	}
	if got, ok := bravo.Figures[writerclass.CommandExpiredCount]; ok {
		t.Errorf("edge-bravo command-expired-count = %+v, want absent: its window ended ten minutes ago", got)
	}
	if got := bravo.Figures[writerclass.TickOverrunCount]; got.Value != 5 || !got.ObservedAt.Equal(gone) {
		t.Errorf("edge-bravo tick-overrun-count = %+v, want its last window's 5 at %s: a count written every window is current", got, gone)
	}
	if got := alpha.Figures[writerclass.CheckinAge]; got.Value != 3 {
		t.Errorf("edge-alpha checkin-age = %+v, want the gauge's latest, 3, however old", got)
	}
}
