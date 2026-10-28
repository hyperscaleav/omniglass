package storage_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/accesspath"
	"github.com/jackc/pgx/v5"
)

// pruneFixture is a migrated database with one component and one system, a
// metric and a property type, and helpers that insert a sample row at an exact
// time, so age is exact rather than racy.
type pruneFixture struct {
	t    *testing.T
	ctx  context.Context
	gw   *storage.PG
	conn *pgx.Conn
}

func newPruneFixture(t *testing.T) *pruneFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into component (name, product_id) values ('prune-c1', (select id from product where name = 'generic-device'))`)
	mustExec(t, conn, `insert into system (name) values ('prune-s1')`)
	mustExec(t, conn, `insert into property_type (name, data_type) values ('prune-power', 'string')`)
	mustExec(t, conn, `insert into metric_type (name, data_type) values ('prune-rtt', 'float')`)
	return &pruneFixture{t: t, ctx: ctx, gw: gw, conn: conn}
}

// row inserts one sample: lane is metric or property, owner is component or
// system (prune-c1 or prune-s1).
func (f *pruneFixture) row(lane, owner string, ts time.Time, provenance string, value float64) {
	f.t.Helper()
	col, name := "component_id", "prune-c1"
	if owner == "system" {
		col, name = "system_id", "prune-s1"
	}
	if lane == "metric" {
		mustExec(f.t, f.conn, `insert into metric (ts, owner_kind, `+col+`, metric_type_id, instance, provenance, value)
			values ($1, $2, (select id from `+owner+` where name = $3), (select id from metric_type where name = 'prune-rtt'), '', $4, $5)`,
			ts, owner, name, provenance, value)
		return
	}
	mustExec(f.t, f.conn, `insert into property (ts, owner_kind, `+col+`, property_type_id, instance, provenance, value)
		values ($1, $2, (select id from `+owner+` where name = $3), (select id from property_type where name = 'prune-power'), '', $4, to_jsonb($5::text))`,
		ts, owner, name, provenance, strconv.FormatFloat(value, 'f', -1, 64))
}

// values lists one lane's surviving rows for one owner, oldest first, as
// provenance:value.
func (f *pruneFixture) values(lane, owner string) []string {
	f.t.Helper()
	col := "component_id"
	if owner == "system" {
		col = "system_id"
	}
	value := `(value #>> '{}')`
	if lane == "metric" {
		value = `value::text`
	}
	rows, err := f.conn.Query(f.ctx, `select provenance || ':' || `+value+` from `+lane+`
		where owner_kind = $1 and `+col+` is not null order by ts, id`, owner)
	if err != nil {
		f.t.Fatalf("read %s %s: %v", lane, owner, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

func (f *pruneFixture) prune(lane string, before time.Time, limit int) int64 {
	f.t.Helper()
	n, err := f.gw.PruneSamples(f.ctx, lane, before, limit)
	if err != nil {
		f.t.Fatalf("prune %s: %v", lane, err)
	}
	return n
}

// TestPruneSamplesKeepsDeclaredAndLatestRows pins the retention floor (#591,
// ADR-0080) on both lanes and on a component and a system owner: a prune
// deletes samples older than the cutoff EXCEPT a declared row (an operator's
// assertion is the truth, not a sample) and EXCEPT the latest row of every
// series (type, owner arc, instance, provenance), so a prune never erases a
// current value. A re-run deletes nothing.
func TestPruneSamplesKeepsDeclaredAndLatestRows(t *testing.T) {
	f := newPruneFixture(t)
	now := time.Now().UTC()
	twoYears := now.Add(-2 * 365 * 24 * time.Hour)
	tenDays := now.Add(-10 * 24 * time.Hour)
	fiveDays := now.Add(-5 * 24 * time.Hour)
	cutoff := now.Add(-24 * time.Hour)

	for _, owner := range []string{"component", "system"} {
		// Property lane: two declared edits years old (never pruned), and an
		// observed series wholly older than the cutoff, so only its latest
		// row survives.
		f.row("property", owner, twoYears.Add(-24*time.Hour), "declared", 1)
		f.row("property", owner, twoYears, "declared", 2)
		f.row("property", owner, tenDays, "observed", 3)
		f.row("property", owner, fiveDays, "observed", 4)
		// Metric lane: the same shape, a superseded old row and an old latest.
		f.row("metric", owner, tenDays, "observed", 12)
		f.row("metric", owner, fiveDays, "observed", 34)
	}

	if n := f.prune("property", cutoff, 100); n != 2 {
		t.Fatalf("property prune deleted %d rows, want 2 (the superseded observed row of each owner)", n)
	}
	if n := f.prune("metric", cutoff, 100); n != 2 {
		t.Fatalf("metric prune deleted %d rows, want 2 (the superseded observed row of each owner)", n)
	}
	for _, owner := range []string{"component", "system"} {
		if got := strings.Join(f.values("property", owner), ","); got != "declared:1,declared:2,observed:4" {
			t.Errorf("%s property rows after prune = %s, want both declared edits and the latest observed", owner, got)
		}
		if got := strings.Join(f.values("metric", owner), ","); got != "observed:34" {
			t.Errorf("%s metric rows after prune = %s, want the latest observed", owner, got)
		}
	}
	if n := f.prune("property", cutoff, 100) + f.prune("metric", cutoff, 100); n != 0 {
		t.Fatalf("a re-run deleted %d rows, want 0", n)
	}
}

// TestPruneSamplesCompletesABacklogInChunks: a backlog larger than one call's
// limit completes across calls, each deleting at most the limit in short
// statements of its own, and the chunk boundary loses nothing and spares the
// row inside the window.
func TestPruneSamplesCompletesABacklogInChunks(t *testing.T) {
	f := newPruneFixture(t)
	now := time.Now().UTC()
	start := now.Add(-30 * 24 * time.Hour)
	for i := 0; i < 25; i++ {
		f.row("metric", "component", start.Add(time.Duration(i)*time.Hour), "observed", float64(i))
	}
	f.row("metric", "component", now.Add(-time.Hour), "observed", 99)
	cutoff := now.Add(-24 * time.Hour)

	var got []int64
	for _, want := range []int64{10, 10, 5, 0} {
		n := f.prune("metric", cutoff, 10)
		got = append(got, n)
		if n != want {
			t.Fatalf("chunks deleted %v, want 10, 10, 5, then 0", got)
		}
	}
	if v := f.values("metric", "component"); len(v) != 1 || v[0] != "observed:99" {
		t.Fatalf("after the backlog, rows = %v, want only the row inside the window", v)
	}
}

// TestPruneSamplesReachesAnIndexOnEveryArc holds the prune's access path with
// the accesspath instrument: on every owner arc, the probe for a newer row of a
// candidate's series (the scan aliased n) searches that arc's series index with
// a condition bound to the candidate row (c), so it runs once per old row as an
// index lookup. A probe the planner flattens into a hash semi join reads every
// row of the table into a hash and carries no condition bound to c, which is
// what the 2026-10-28 review measured an EXISTS probe doing on PG18 at 2.5M
// rows; that shape fails here. Which index the candidates (c) come through for
// the cutoff, the ts BRIN or a series index's ts column, is the planner's
// preference and not asserted. No writer gives a node a sample today and no index serves one, so the
// node arc is left out until something writes one.
func TestPruneSamplesReachesAnIndexOnEveryArc(t *testing.T) {
	f := newPruneFixture(t)
	want := map[string]map[string]string{
		"metric":   {"component": "metric_owner_idx", "system": "metric_arc_owner_idx", "location": "metric_arc_owner_idx"},
		"property": {"component": "property_series_idx", "system": "property_system_owner_idx", "location": "property_location_owner_idx"},
	}
	// The series indexes that end in ts DESC; the property owner indexes for a
	// system or a location are ordered by id instead.
	tsOrdered := map[string]bool{"metric_owner_idx": true, "metric_arc_owner_idx": true, "property_series_idx": true}
	for lane, arcs := range want {
		// The lane's own prune, and a typed prune of one type with a window of
		// its own (#417), which differ only in the type predicate.
		for _, v := range []struct {
			stmts map[string]string
			args  []any
		}{
			{storage.ExportPruneSamplesSQL(lane), []any{time.Now(), 10}},
			{storage.ExportPruneSamplesOfTypeSQL(lane), []any{time.Now(), 10, "00000000-0000-0000-0000-000000000000", 3}},
		} {
			stmts := v.stmts
			for arc, index := range arcs {
				plan, err := accesspath.Explain(f.ctx, f.conn, stmts[arc], v.args...)
				if err != nil {
					t.Fatalf("explain the %s prune on a %s: %v", lane, arc, err)
				}
				var probe []accesspath.Scan
				for _, s := range plan.Scans(lane) {
					if s.Alias == "n" {
						probe = append(probe, s)
					}
				}
				if len(probe) != 1 || probe[0].Index != index || probe[0].Disabled ||
					!strings.Contains(probe[0].Cond, "c."+arc+"_id") && !strings.Contains(probe[0].Cond, "c.id") {
					t.Errorf("the %s prune on a %s: the newer-row probe is not a per-row search of %s bound to the candidate:\n\t%s", lane, arc, index, plan)
				}
				// On a ts-ordered series index the probe walks up from the
				// candidate's ts (a backward scan of a ts DESC index), not down
				// from the newest row past any a skewed clock future-dated.
				if tsOrdered[index] {
					mustExec(t, f.conn, "set enable_seqscan = off")
					var text string
					rows, err := f.conn.Query(f.ctx, "explain (costs off) "+stmts[arc], v.args...)
					if err != nil {
						t.Fatal(err)
					}
					for rows.Next() {
						var line string
						if err := rows.Scan(&line); err != nil {
							t.Fatal(err)
						}
						text += line + "\n"
					}
					rows.Close()
					mustExec(t, f.conn, "reset enable_seqscan")
					if !strings.Contains(text, "Index Scan Backward using "+index+" on "+lane+" n") {
						t.Errorf("the %s prune on a %s: the probe does not walk up from the candidate's ts:\n%s", lane, arc, text)
					}
				}
			}
		}
	}
}

// TestRetentionWindowsReadTheSettingsStore: the gateway reads the windows the
// retention pass prunes by from the platform settings level, defaults filled,
// so an admin's change is the next pass's window.
func TestRetentionWindowsReadTheSettingsStore(t *testing.T) {
	f := newPruneFixture(t)
	w, err := f.gw.RetentionWindows(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if w.MetricDays != 14 || w.PropertyDays != 90 || w.TrendDays != 730 {
		t.Fatalf("default windows %+v, want 14, 90 and 730 days", w)
	}
	if _, err := f.gw.UpsertSettingOverride(f.ctx, "", "platform", "retention", map[string]any{"metric_days": 7}, nil); err != nil {
		t.Fatalf("override: %v", err)
	}
	if w, err = f.gw.RetentionWindows(f.ctx); err != nil || w.MetricDays != 7 || w.PropertyDays != 90 {
		t.Fatalf("after an override, windows %+v (err %v), want metric 7 and property still 90", w, err)
	}
}

// TestPruneSamplesSparesWhatAReaderCallsLatest: the floor holds under both
// orders the readers use. After the node's clock stepped back (#801), a series'
// newest row by id carries an older ts than the row before it: the latest-value
// reads take the higher ts, the health reads the higher id, and neither row may
// go. A command's intended row is kept with the command, which the command
// table (never pruned) reads back by its command_id, however many rows came
// after it.
func TestPruneSamplesSparesWhatAReaderCallsLatest(t *testing.T) {
	f := newPruneFixture(t)
	now := time.Now().UTC()
	f.row("metric", "component", now.Add(-10*24*time.Hour), "observed", 1) // lower id, newer ts
	f.row("metric", "component", now.Add(-20*24*time.Hour), "observed", 2) // higher id, older ts

	mustExec(t, f.conn, `insert into command_type (name, label, target_property_type_id)
		values ('prune-set-power', 'Set power', (select id from property_type where name = 'prune-power'))`)
	// Two commands, each opening its intended row; the second supersedes the
	// first in the series, and both are past the window.
	for i, age := range []time.Duration{30, 20} {
		var cmd int64
		if err := f.conn.QueryRow(f.ctx, `insert into command (owner_kind, component_id, command_type_id)
			values ('component', (select id from component where name = 'prune-c1'), (select id from command_type where name = 'prune-set-power'))
			returning id`).Scan(&cmd); err != nil {
			t.Fatalf("command: %v", err)
		}
		mustExec(t, f.conn, `insert into property (ts, owner_kind, component_id, property_type_id, instance, provenance, value, command_id)
			values ($1, 'component', (select id from component where name = 'prune-c1'),
			        (select id from property_type where name = 'prune-power'), '', 'intended', to_jsonb($2::text), $3)`,
			now.Add(-age*24*time.Hour), strconv.Itoa(10+i), cmd)
	}

	cutoff := now.Add(-24 * time.Hour)
	if n := f.prune("metric", cutoff, 100); n != 0 {
		t.Errorf("the metric prune deleted %d rows of a series whose ts and id orders disagree, want 0", n)
	}
	if got := strings.Join(f.values("property", "component"), ","); f.prune("property", cutoff, 100) != 0 ||
		got != "intended:10,intended:11" {
		t.Errorf("after the prune, property rows %s, want the command's intended row kept though superseded", strings.Join(f.values("property", "component"), ","))
	}
}
