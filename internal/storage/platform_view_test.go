package storage_test

import (
	"context"
	"errors"
	"fmt"
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
	// No server, so no server panel: the groups ride on servers[] (#1645).
	if v.FiguresRead || len(v.Servers) != 0 {
		t.Errorf("figures read = %v, servers = %+v, want neither, and so no server groups", v.FiguresRead, v.Servers)
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
// over and the ones since counted nothing. A gauge never ages out.
func TestPlatformViewReadsAWindowCountAsItsNewestWindow(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	window, earlier, gone := now.Add(-5*time.Second), now.Add(-20*time.Second), now.Add(-10*time.Minute)
	if err := f.gw.RecordPlatformMetrics(f.ctx, []storage.PlatformMetric{
		// edge-alpha's newest pass expired two undelivered and one unexecuted.
		{ComponentID: f.alpha.ID, Key: writerclass.CommandExpiredCount, Instance: "undelivered", Value: 2, TS: window},
		{ComponentID: f.alpha.ID, Key: writerclass.CommandExpiredCount, Instance: "unexecuted", Value: 1, TS: window},
		{ComponentID: f.alpha.ID, Key: writerclass.PublishFailureCount, Value: 4, TS: window},
		// edge-bravo's earlier pass expired seven unexecuted, its newest two
		// undelivered: the newest window is the figure, not their total.
		{ComponentID: f.bravo.ID, Key: writerclass.CommandExpiredCount, Instance: "unexecuted", Value: 7, TS: earlier},
		{ComponentID: f.bravo.ID, Key: writerclass.CommandExpiredCount, Instance: "undelivered", Value: 2, TS: window},
		// edge-bravo dropped five batches ten minutes ago and none since.
		{ComponentID: f.bravo.ID, Key: writerclass.SpoolDropCount, Value: 5, TS: gone},
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
	if got := alpha.Figures[writerclass.PublishFailureCount]; got.Value != 4 {
		t.Errorf("edge-alpha publish-failure-count = %+v, want this window's 4", got)
	}
	if got := bravo.Figures[writerclass.CommandExpiredCount]; got.Value != 2 {
		t.Errorf("edge-bravo command-expired-count = %+v, want the newest window's 2", got)
	}
	if got, ok := bravo.Figures[writerclass.SpoolDropCount]; ok {
		t.Errorf("edge-bravo spool-drop-count = %+v, want absent: its window ended ten minutes ago", got)
	}
	if got := alpha.Figures[writerclass.CheckinAge]; got.Value != 3 {
		t.Errorf("edge-alpha checkin-age = %+v, want the gauge's latest, 3, however old", got)
	}
}

// serverPanel is the named panel of the view's one server.
func serverPanel(t *testing.T, v *storage.PlatformView, name string) storage.PlatformPanel {
	t.Helper()
	if len(v.Servers) != 1 {
		t.Fatalf("servers = %+v, want the one server", v.Servers)
	}
	for _, p := range v.Servers[0].Groups {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("server %s has no %s panel: %+v", v.Servers[0].Name, name, v.Servers[0].Groups)
	return storage.PlatformPanel{}
}

// figuresOf is a panel's figures for one key, in the order the view returned.
func figuresOf(p storage.PlatformPanel, key string) []storage.PlatformInstanceFigure {
	var out []storage.PlatformInstanceFigure
	for _, f := range p.Figures {
		if f.Key == key {
			out = append(out, f)
		}
	}
	return out
}

// TestPlatformViewRanksTheSlowestRoutesOfTheNewestWindow (#1645): a server
// with thirty routes sampled in its newest window reads back the ten slowest,
// highest first, sorted in SQL; a route only an earlier window sampled is
// not this window's, however slow it was.
func TestPlatformViewRanksTheSlowestRoutesOfTheNewestWindow(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	earlier := time.Date(2026, 11, 9, 11, 0, 0, 0, time.UTC)
	newest := earlier.Add(30 * time.Second)
	samples := []storage.PlatformMetric{
		{ComponentID: f.server, Key: writerclass.HTTPRequestDurationMax, Instance: "GET /api/v1/old 2xx", Value: 9999, TS: earlier},
	}
	for i := 1; i <= 30; i++ {
		samples = append(samples, storage.PlatformMetric{
			ComponentID: f.server, Key: writerclass.HTTPRequestDurationMax,
			Instance: fmt.Sprintf("GET /api/v1/route%02d 2xx", i), Value: float64(i), TS: newest,
		})
	}
	if err := f.gw.RecordPlatformMetrics(f.ctx, samples); err != nil {
		t.Fatalf("record platform metrics: %v", err)
	}
	v, err := f.gw.PlatformView(f.ctx, all, all, all)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}
	got := figuresOf(serverPanel(t, v, "requests"), writerclass.HTTPRequestDurationMax)
	if len(got) != 10 {
		t.Fatalf("http-request-duration-max = %d rows %+v, want the ten slowest", len(got), got)
	}
	for i, fig := range got {
		want := float64(30 - i)
		if fig.Value != want || fig.Instance != fmt.Sprintf("GET /api/v1/route%02d 2xx", 30-i) || !fig.ObservedAt.Equal(newest) {
			t.Errorf("row %d = %+v, want route%02d at %v ms in the newest window", i, fig, 30-i, want)
		}
	}
}

// TestPlatformViewReadsEachRetentionLane (#1645): a split key that is not
// ranked reads back its latest sample per instance, one entry per lane, and
// a lane's older pass is not a second entry.
func TestPlatformViewReadsEachRetentionLane(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	older := time.Date(2026, 11, 9, 11, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	if err := f.gw.RecordPlatformMetrics(f.ctx, []storage.PlatformMetric{
		{ComponentID: f.server, Key: writerclass.RetentionBehind, Instance: "metric", Value: 1, TS: older},
		{ComponentID: f.server, Key: writerclass.RetentionBehind, Instance: "metric", Value: 0, TS: newer},
		{ComponentID: f.server, Key: writerclass.RetentionBehind, Instance: "event", Value: 1, TS: newer},
		{ComponentID: f.server, Key: writerclass.RetentionBehind, Instance: "metric_trend compaction", Value: 0, TS: older},
		{ComponentID: f.server, Key: writerclass.DatabaseBytes, Value: 4096, TS: newer},
	}); err != nil {
		t.Fatalf("record platform metrics: %v", err)
	}
	v, err := f.gw.PlatformView(f.ctx, all, all, all)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}
	p := serverPanel(t, v, "retention")
	lanes := map[string]float64{}
	for _, fig := range figuresOf(p, writerclass.RetentionBehind) {
		if _, dup := lanes[fig.Instance]; dup {
			t.Errorf("lane %q read back twice", fig.Instance)
		}
		lanes[fig.Instance] = fig.Value
	}
	want := map[string]float64{"event": 1, "metric": 0, "metric_trend compaction": 0}
	if len(lanes) != len(want) {
		t.Errorf("retention-behind lanes = %v, want %v", lanes, want)
	}
	for lane, val := range want {
		if got, ok := lanes[lane]; !ok || got != val {
			t.Errorf("retention-behind %q = %v (present %v), want %v", lane, got, ok, val)
		}
	}
	if got := figuresOf(p, writerclass.DatabaseBytes); len(got) != 1 || got[0].Value != 4096 || got[0].Instance != "" {
		t.Errorf("database-bytes = %+v, want one unsplit 4096", got)
	}
	if got := figuresOf(serverPanel(t, v, "queues"), writerclass.OutboxQueuedDepth); len(got) != 0 {
		t.Errorf("outbox-queued-depth = %+v, want none: the server never sampled it", got)
	}
}

// TestPlatformViewServerPanelsCarryTheirCatalogAndAlarms (#1645): every
// server carries every server group in declared order with its keys' catalog
// facts, and the open alarms on its component with the key each one's rule
// samples, so the page can flag a panel without a threshold of its own. A
// cleared alarm is not open.
func TestPlatformViewServerPanelsCarryTheirCatalogAndAlarms(t *testing.T) {
	t.Parallel()
	gw, ctx, dsn := platformGatewayDSN(t)
	server, err := gw.EnsureServerComponent(ctx, "lab-server")
	if err != nil {
		t.Fatalf("ensure server component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `
		insert into alarm (component_id, severity, message, dedup_key, raised_by_rule_id, cleared_at)
		values ($1::uuid, 'warning', 'ingest is behind', 'lag', (select id from event_rule where name = 'ingest-lag-high'), null),
		       ($1::uuid, 'warning', 'retention caught up', 'ret', (select id from event_rule where name = 'retention-behind'), now())`,
		server); err != nil {
		t.Fatalf("raise the alarms: %v", err)
	}
	v, err := gw.PlatformView(ctx, all, all, all)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}
	if len(v.Servers) != 1 {
		t.Fatalf("servers = %+v, want the one server", v.Servers)
	}
	s := v.Servers[0]
	if len(s.Groups) != len(storage.PlatformServerGroups) {
		t.Fatalf("groups = %+v, want every server group", s.Groups)
	}
	for i, g := range storage.PlatformServerGroups {
		got := s.Groups[i]
		if got.Name != g.Name || got.Label != g.Label || len(got.Keys) != len(g.Keys) {
			t.Errorf("group %d = %s/%s with %d keys, want %s/%s with %d", i, got.Name, got.Label, len(got.Keys), g.Name, g.Label, len(g.Keys))
			continue
		}
		for j, k := range g.Keys {
			if got.Keys[j].Name != k.Name || got.Keys[j].Of != k.Of || got.Keys[j].Rank != k.Rank || got.Keys[j].Description == "" {
				t.Errorf("%s key %d = %+v, want %s with its catalog description", g.Name, j, got.Keys[j], k.Name)
			}
		}
		if len(got.Figures) != 0 {
			t.Errorf("%s figures = %+v, want none before a report window", g.Name, got.Figures)
		}
	}
	if len(s.Alarms) != 1 {
		t.Fatalf("alarms = %+v, want the one open alarm", s.Alarms)
	}
	a := s.Alarms[0]
	if a.Rule != "ingest-lag-high" || a.Key == nil || *a.Key != writerclass.IngestLagMax || a.Severity != "warning" || a.Message != "ingest is behind" {
		t.Errorf("alarm = %+v, want ingest-lag-high on ingest-lag-max", a)
	}
}
