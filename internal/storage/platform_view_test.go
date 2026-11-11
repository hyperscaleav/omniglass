package storage_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/delivery"
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
	// day is the start of today in UTC: the fixture's samples sit at fixed
	// hours of it, because the metric table only holds partitions from today on.
	day time.Time
}

func newPlatformViewFixture(t *testing.T) platformViewFixture {
	t.Helper()
	gw, ctx, dsn := platformGatewayDSN(t)
	f := platformViewFixture{gw: gw, ctx: ctx, day: time.Now().UTC().Truncate(24 * time.Hour)}
	f.alpha = platformNode(t, ctx, gw, "edge-alpha")
	f.bravo = platformNode(t, ctx, gw, "edge-bravo")
	var err error
	if f.server, err = gw.EnsureServerComponent(ctx, "lab-server"); err != nil {
		t.Fatalf("ensure server component: %v", err)
	}
	f.startedAt = f.day.Add(9*time.Hour + 30*time.Minute)
	if err := gw.RecordServerIdentity(ctx, f.server, "v1.2.3", f.startedAt); err != nil {
		t.Fatalf("record server identity: %v", err)
	}

	older := f.day.Add(10 * time.Hour)
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
// written once a window, every window, zero when quiet, so the figure is the
// newest window's, summed across the instances that split it, and like a
// gauge it never ages out: an old sample means the node stopped reporting,
// which its checkin age says.
func TestPlatformViewReadsAWindowCountAsItsNewestWindow(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	window, earlier, gone := now.Add(-5*time.Second), now.Add(-20*time.Second), now.Add(-10*time.Minute)
	if err := f.gw.RecordPlatformMetrics(f.ctx, []storage.PlatformMetric{
		// edge-alpha's newest pass expired two undelivered and one unexecuted.
		{ComponentID: f.alpha.ID, Key: writerclass.CommandExpiredCount, Instance: "undelivered", Value: 2, TS: window},
		{ComponentID: f.alpha.ID, Key: writerclass.CommandExpiredCount, Instance: "unexecuted", Value: 1, TS: window},
		{ComponentID: f.alpha.ID, Key: writerclass.ScheduleSkipCount, Value: 4, TS: window},
		// edge-bravo's earlier pass expired seven unexecuted, its newest two
		// undelivered: the newest window is the figure, not their total.
		{ComponentID: f.bravo.ID, Key: writerclass.CommandExpiredCount, Instance: "unexecuted", Value: 7, TS: earlier},
		{ComponentID: f.bravo.ID, Key: writerclass.CommandExpiredCount, Instance: "undelivered", Value: 2, TS: window},
		// edge-bravo's last report window, ten minutes ago, counted five overruns.
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
	if got := alpha.Figures[writerclass.ScheduleSkipCount]; got.Value != 4 {
		t.Errorf("edge-alpha schedule-skip-count = %+v, want this window's 4", got)
	}
	if got := bravo.Figures[writerclass.CommandExpiredCount]; got.Value != 2 {
		t.Errorf("edge-bravo command-expired-count = %+v, want the newest window's 2", got)
	}
	if got := bravo.Figures[writerclass.TickOverrunCount]; got.Value != 5 || !got.ObservedAt.Equal(gone) {
		t.Errorf("edge-bravo tick-overrun-count = %+v, want its last window's 5 at %s: a count written every window does not age out", got, gone)
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
	newest := time.Now().UTC().Truncate(time.Second).Add(-5 * time.Second)
	earlier := newest.Add(-30 * time.Second)
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
	older := f.day.Add(11 * time.Hour)
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

// TestPlatformViewAgesOutAServerWindowFigure (#1645): a server key written
// only for a window that observed something (a request, a landed batch, a
// slow query) is that window's figure, not a current reading; a
// later window that saw nothing writes no row. So once two report windows
// have passed it is absent, instance by instance, as a node's window count is (#1644), and the
// panel reads not reported instead of the last lag as current. A gauge the
// server samples every window, and a retention fact its hourly pass writes,
// stay however old.
func TestPlatformViewAgesOutAServerWindowFigure(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	window, gone := now.Add(-5*time.Second), now.Add(-10*time.Minute)
	if err := f.gw.RecordPlatformMetrics(f.ctx, []storage.PlatformMetric{
		{ComponentID: f.server, Key: writerclass.IngestLag, Value: 4, TS: gone},
		{ComponentID: f.server, Key: writerclass.IngestLagMax, Value: 9, TS: gone},
		{ComponentID: f.server, Key: writerclass.IngestSampleCount, Value: 80, TS: gone},
		{ComponentID: f.server, Key: writerclass.HTTPRequestDurationMax, Instance: "GET /api/v1/old 2xx", Value: 50, TS: gone},
		{ComponentID: f.server, Key: writerclass.HTTPRequestCount, Instance: "GET /api/v1/old 2xx", Value: 6, TS: gone},
		{ComponentID: f.server, Key: writerclass.DBSlowQueryCount, Value: 2, TS: gone},
		{ComponentID: f.server, Key: writerclass.IngestBatchCount, Value: 4, TS: window},
		{ComponentID: f.server, Key: writerclass.OutboxQueuedDepth, Value: 7, TS: gone},
		{ComponentID: f.server, Key: writerclass.RetentionBehind, Instance: "metric", Value: 0, TS: gone},
	}); err != nil {
		t.Fatalf("record platform metrics: %v", err)
	}
	v, err := f.gw.PlatformView(f.ctx, all, all, all)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}
	for panel, keys := range map[string][]string{
		"requests": {writerclass.HTTPRequestDurationMax, writerclass.HTTPRequestCount, writerclass.DBSlowQueryCount},
		"ingest":   {writerclass.IngestLag, writerclass.IngestLagMax, writerclass.IngestSampleCount},
	} {
		for _, key := range keys {
			if got := figuresOf(serverPanel(t, v, panel), key); len(got) != 0 {
				t.Errorf("%s = %+v, want absent: its window ended ten minutes ago", key, got)
			}
		}
	}
	if got := figuresOf(serverPanel(t, v, "ingest"), writerclass.IngestBatchCount); len(got) != 1 || got[0].Value != 4 {
		t.Errorf("ingest-batch-count = %+v, want this window's 4", got)
	}
	if got := figuresOf(serverPanel(t, v, "queues"), writerclass.OutboxQueuedDepth); len(got) != 1 || got[0].Value != 7 {
		t.Errorf("outbox-queued-depth = %+v, want the gauge's latest, 7, however old", got)
	}
	if got := figuresOf(serverPanel(t, v, "retention"), writerclass.RetentionBehind); len(got) != 1 || got[0].Value != 0 {
		t.Errorf("retention-behind = %+v, want the hourly pass's 0, however old", got)
	}
}

// deliveryWindow is one window of a hop's three keys for one kind on one
// platform component, as the node's report or the server's recorder writes it.
func deliveryWindow(id, kind string, in, out float64, ts time.Time) []storage.PlatformMetric {
	loss := 0.0
	if in > 0 {
		loss = (in - out) / in
	}
	return []storage.PlatformMetric{
		{ComponentID: id, Key: writerclass.DeliveryInCount, Instance: kind, Value: in, TS: ts},
		{ComponentID: id, Key: writerclass.DeliveryOutCount, Instance: kind, Value: out, TS: ts},
		{ComponentID: id, Key: writerclass.DeliveryLossRatio, Instance: kind, Value: loss, TS: ts},
	}
}

func pipelineHop(t *testing.T, p *storage.PlatformPipeline, id delivery.Hop) storage.PlatformPipelineHopFigures {
	t.Helper()
	for _, h := range p.Hops {
		if h.Hop.ID == id {
			return h
		}
	}
	t.Fatalf("hop %s absent: %+v", id, p.Hops)
	return storage.PlatformPipelineHopFigures{}
}

func pipelineKind(t *testing.T, cells []storage.PlatformPipelineCell, kind delivery.Kind) storage.PlatformPipelineCell {
	t.Helper()
	for _, c := range cells {
		if c.Kind == kind {
			return c
		}
	}
	t.Fatalf("kind %s absent: %+v", kind, cells)
	return storage.PlatformPipelineCell{}
}

func ratio(f *float64) float64 {
	if f == nil {
		return -1
	}
	return *f
}

// TestPlatformViewReadsThePipelineHopByHop: two nodes and a server each write
// two windows of their hop's delivery. Each hop reads its newest window and
// its last hour per kind, composed from the summed counts; its components'
// in and out sum to its own; the node rows carry their own hop's last-hour
// loss; and a window older than the hour is not in it.
func TestPlatformViewReadsThePipelineHopByHop(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	older, newer, stale := now.Add(-2*time.Minute), now.Add(-time.Minute), now.Add(-3*time.Hour)
	var ms []storage.PlatformMetric
	// edge-alpha's spool lost 2 of 10 metrics in the older window and none of
	// 10 in the newer; edge-bravo lost none of 80 then none of 100. Three
	// hours ago edge-bravo lost all 50, outside the hour.
	ms = append(ms, deliveryWindow(f.alpha.ID, "metric", 10, 8, older)...)
	ms = append(ms, deliveryWindow(f.alpha.ID, "metric", 10, 10, newer)...)
	ms = append(ms, deliveryWindow(f.bravo.ID, "metric", 50, 0, stale)...)
	ms = append(ms, deliveryWindow(f.bravo.ID, "metric", 80, 80, older)...)
	ms = append(ms, deliveryWindow(f.bravo.ID, "metric", 100, 100, newer)...)
	ms = append(ms, deliveryWindow(f.alpha.ID, "property", 0, 0, newer)...)
	// The server landed 98 of 98 metrics then 108 of 110.
	ms = append(ms, deliveryWindow(f.server, "metric", 98, 98, older)...)
	ms = append(ms, deliveryWindow(f.server, "metric", 110, 108, newer)...)
	if err := f.gw.RecordPlatformMetrics(f.ctx, ms); err != nil {
		t.Fatalf("record delivery windows: %v", err)
	}

	v, err := f.gw.PlatformView(f.ctx, all, all, all)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}
	if v.Pipeline == nil {
		t.Fatal("pipeline = nil for an all-scope component read")
	}
	if len(v.Pipeline.Hops) != len(storage.PlatformPipelineHops) {
		t.Fatalf("hops = %d, want %d", len(v.Pipeline.Hops), len(storage.PlatformPipelineHops))
	}
	for i, h := range v.Pipeline.Hops {
		if h.Hop.ID != storage.PlatformPipelineHops[i].ID {
			t.Errorf("hop %d = %s, want %s (declared order)", i, h.Hop.ID, storage.PlatformPipelineHops[i].ID)
		}
	}
	if len(v.Pipeline.Keys) != 3 {
		t.Errorf("pipeline keys = %+v, want the three delivery keys' catalog facts", v.Pipeline.Keys)
	}

	node := pipelineHop(t, v.Pipeline, delivery.Node)
	m := pipelineKind(t, node.Kinds, delivery.Metric)
	if m.Newest.In != 110 || m.Newest.Out != 110 || ratio(m.Newest.Loss) != 0 {
		t.Errorf("node metric newest = %+v loss %v, want 110 in, 110 out, 0", m.Newest, ratio(m.Newest.Loss))
	}
	if m.LastHour.In != 200 || m.LastHour.Out != 198 || ratio(m.LastHour.Loss) != 0.01 {
		t.Errorf("node metric last hour = %+v loss %v, want 200 in, 198 out, 0.01", m.LastHour, ratio(m.LastHour.Loss))
	}
	if m.NewestAt == nil || !m.NewestAt.Equal(newer) {
		t.Errorf("node metric newest at = %v, want %s", m.NewestAt, newer)
	}
	p := pipelineKind(t, node.Kinds, delivery.Property)
	if p.Newest.In != 0 || p.Newest.Loss != nil {
		t.Errorf("node property = %+v, want a row with nothing in and no ratio", p.Newest)
	}
	var in, out uint64
	for _, c := range node.ByComponent {
		in += c.Total.LastHour.In
		out += c.Total.LastHour.Out
	}
	if len(node.ByComponent) != 2 || in != node.Total.LastHour.In || out != node.Total.LastHour.Out {
		t.Errorf("node components %+v sum to %d in, %d out; hop total %+v", node.ByComponent, in, out, node.Total.LastHour)
	}

	ingest := pipelineHop(t, v.Pipeline, delivery.Ingest)
	im := pipelineKind(t, ingest.Kinds, delivery.Metric)
	if im.Newest.In != 110 || im.Newest.Out != 108 || im.LastHour.In != 208 || im.LastHour.Out != 206 {
		t.Errorf("ingest metric = newest %+v, last hour %+v; want 110/108 and 208/206", im.Newest, im.LastHour)
	}
	if len(ingest.ByComponent) != 1 || ingest.ByComponent[0].ComponentID != f.server {
		t.Errorf("ingest components = %+v, want the server alone", ingest.ByComponent)
	}
	if len(ingest.Kinds) != 1 {
		t.Errorf("ingest kinds = %+v, want metric alone: a kind with no series has no row", ingest.Kinds)
	}

	rows := map[string]storage.PlatformNode{}
	for _, n := range v.Nodes {
		rows[n.Name] = n
	}
	if l := rows["edge-alpha"].Loss; l == nil || *l != 0.1 {
		t.Errorf("edge-alpha loss = %v, want 2 of 20 over the hour, 0.1", ratio(l))
	}
	if l := rows["edge-bravo"].Loss; l == nil || *l != 0 {
		t.Errorf("edge-bravo loss = %v, want 0: its loss three hours ago is outside the hour", ratio(l))
	}
	if v.LossHops == nil || len(v.LossHops) != 1 || v.LossHops[0] != delivery.Node {
		t.Errorf("loss hops = %v, want the node hop", v.LossHops)
	}
}

// TestPlatformViewOmitsThePipelineWithoutPlatformReach: the pipeline's
// figures are platform component figures, so a location-scoped component
// read gets no pipeline and no node loss.
func TestPlatformViewOmitsThePipelineWithoutPlatformReach(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	if err := f.gw.RecordPlatformMetrics(f.ctx, deliveryWindow(f.alpha.ID, "metric", 10, 9, time.Now().UTC())); err != nil {
		t.Fatalf("record delivery window: %v", err)
	}
	located := scope.Set{IDs: []string{"00000000-0000-0000-0000-0000000000aa"}}
	v, err := f.gw.PlatformView(f.ctx, all, all, located)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}
	if v.Pipeline != nil {
		t.Errorf("pipeline = %+v, want omitted for a location-scoped component read", v.Pipeline)
	}
	for _, n := range v.Nodes {
		if n.Loss != nil {
			t.Errorf("%s loss = %v, want omitted", n.Name, *n.Loss)
		}
	}
}

// TestPlatformViewGroupsCarryNoFoldedDropCount: the drop counts #1659 folded
// into the delivery keys read one way, on the pipeline, never as a column.
func TestPlatformViewGroupsCarryNoFoldedDropCount(t *testing.T) {
	t.Parallel()
	gone := map[string]bool{"spool-drop-count": true, "publish-failure-count": true, "bus-telemetry-drop-count": true}
	for _, g := range storage.PlatformViewGroups {
		for _, k := range g.Keys {
			if gone[k] {
				t.Errorf("group %s carries %s, folded into the delivery keys", g.Name, k)
			}
		}
	}
}

// TestPlatformViewPipelineNewestIsTheCurrentWindow: a node that stopped
// reporting twenty minutes ago keeps its lossy last window as its newest
// sample. Past two report windows (the default 60 s, so 2 minutes) that window
// is not the pipeline's newest: the hop's and the end to end's newest read the
// node still reporting, while the stopped node's loss still counts in the
// last hour.
func TestPlatformViewPipelineNewestIsTheCurrentWindow(t *testing.T) {
	t.Parallel()
	f := newPlatformViewFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	var ms []storage.PlatformMetric
	ms = append(ms, deliveryWindow(f.alpha.ID, "metric", 10, 10, now.Add(-30*time.Second))...)
	ms = append(ms, deliveryWindow(f.bravo.ID, "metric", 10, 0, now.Add(-20*time.Minute))...)
	if err := f.gw.RecordPlatformMetrics(f.ctx, ms); err != nil {
		t.Fatalf("record delivery windows: %v", err)
	}
	v, err := f.gw.PlatformView(f.ctx, all, all, all)
	if err != nil {
		t.Fatalf("platform view: %v", err)
	}
	if v.Pipeline == nil {
		t.Fatal("pipeline = nil for an all-scope component read")
	}
	m := pipelineKind(t, pipelineHop(t, v.Pipeline, delivery.Node).Kinds, delivery.Metric)
	if m.Newest.In != 10 || m.Newest.Out != 10 || ratio(m.Newest.Loss) != 0 {
		t.Errorf("node metric newest = %+v loss %v, want edge-alpha's current 10 of 10, 0", m.Newest, ratio(m.Newest.Loss))
	}
	if m.LastHour.In != 20 || m.LastHour.Out != 10 {
		t.Errorf("node metric last hour = %+v, want both nodes' 20 in, 10 out", m.LastHour)
	}
	if e := pipelineEnd(t, v.Pipeline.EndToEnd, delivery.Metric); ratio(e.Newest) != 0 {
		t.Errorf("metric end to end newest = %v, want 0: edge-bravo's last window is not current", ratio(e.Newest))
	}
}

func pipelineEnd(t *testing.T, ends []storage.PlatformPipelineEnd, kind delivery.Kind) storage.PlatformPipelineEnd {
	t.Helper()
	for _, e := range ends {
		if e.Kind == kind {
			return e
		}
	}
	t.Fatalf("kind %s absent from the end to end: %+v", kind, ends)
	return storage.PlatformPipelineEnd{}
}
