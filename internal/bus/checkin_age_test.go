package bus

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The check-in age (#1625) is the server's account of how long each claimed
// node has been silent: the time since its last heartbeat, or since its claim
// before its first, written on its platform component every checkinAgeEvery.

func TestCheckinAgesReadTheTimeSinceEachNodeSpoke(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	got := checkinAges([]storage.NodeCheckin{
		{Node: "a", ComponentID: "ca", Since: now.Add(-45 * time.Second)},
		{Node: "b", ComponentID: "cb", Since: now.Add(-1500 * time.Millisecond)},
		{Node: "c", ComponentID: "cc", Since: now.Add(2 * time.Second)}, // a clock a little ahead of ours
	}, now)
	want := []storage.PlatformMetric{
		{ComponentID: "ca", Key: writerclass.CheckinAge, Value: 45, TS: now},
		{ComponentID: "cb", Key: writerclass.CheckinAge, Value: 1.5, TS: now},
		{ComponentID: "cc", Key: writerclass.CheckinAge, Value: 0, TS: now},
	}
	if !slices.Equal(got, want) {
		t.Errorf("checkin ages = %+v, want %+v", got, want)
	}
	if got := checkinAges(nil, now); len(got) != 0 {
		t.Errorf("checkin ages of no nodes = %+v, want none", got)
	}
}

// TestCheckinAgeGrowsWhileTheNodeIsSilent writes node-a's check-in age on
// two passes with no heartbeat between: the second reads the first plus the
// time between them, the series a node-silent rule reads.
func TestCheckinAgeGrowsWhileTheNodeIsSilent(t *testing.T) {
	f := newTickFixture(t)
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(f.ctx)
	heard := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if _, err := conn.Exec(f.ctx, `update node set enrolled_at = $1, last_heartbeat_at = $1 where name = 'node-a'`, heard); err != nil {
		t.Fatal(err)
	}
	for _, after := range []time.Duration{10 * time.Second, 40 * time.Second} {
		if err := f.srv.recordCheckinAges(f.ctx, heard.Add(after)); err != nil {
			t.Fatalf("record checkin ages at +%s: %v", after, err)
		}
	}

	rows, err := conn.Query(f.ctx, `select m.value from `+storagetest.MetricRows+` m
		join metric_type t on t.id = m.metric_type_id
		join node n on n.platform_component_id = m.component_id
		where n.name = 'node-a' and t.name = $1 order by m.ts`, writerclass.CheckinAge)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[float64])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []float64{10, 40}) {
		t.Errorf("node-a checkin-age series = %v, want [10 40]: the age grows while no heartbeat arrives", got)
	}
	// node-b, never claimed, has no silence to measure.
	var others int
	if err := conn.QueryRow(f.ctx, `select count(*) from `+storagetest.MetricRows+` m
		join node n on n.platform_component_id = m.component_id where n.name = 'node-b'`).Scan(&others); err != nil {
		t.Fatal(err)
	}
	if others != 0 {
		t.Errorf("node-b rows = %d, want none: it was never claimed", others)
	}
}
