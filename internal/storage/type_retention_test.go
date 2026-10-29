package storage_test

import (
	"errors"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"strconv"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func days(n int) *int { return &n }

// TestATypesRetentionOverrideIsSetOnAnOfficialType (#417, PR 3): a shipped type
// is read-only, but how long an install keeps its samples is the install's own
// choice, so setRetention works on an official metric or property type, is
// audited as its own action, is bounded as the lane windows are, and clears
// with null.
func TestATypesRetentionOverrideIsSetOnAnOfficialType(t *testing.T) {
	f := newPruneFixture(t)
	if err := seed.Run(f.ctx, f.gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mt, err := f.gw.SetMetricTypeRetention(f.ctx, "", "tcp-connect-time", days(3))
	if err != nil || mt.RetentionDays == nil || *mt.RetentionDays != 3 || !mt.Official {
		t.Fatalf("set an official metric type's override: %+v, err %v; want 3 days on the official type", mt, err)
	}
	if got, _ := f.gw.GetMetricType(f.ctx, "tcp-connect-time"); got.RetentionDays == nil || *got.RetentionDays != 3 {
		t.Fatalf("read back %+v, want 3 days", got)
	}
	pt, err := f.gw.SetPropertyTypeRetention(f.ctx, "", "endpoint-reachable", days(365))
	if err != nil || pt.RetentionDays == nil || *pt.RetentionDays != 365 {
		t.Fatalf("set an official property type's override: %+v, err %v", pt, err)
	}
	var audited int
	if err := f.conn.QueryRow(f.ctx, `select count(*) from audit_log where verb = 'set_retention'`).Scan(&audited); err != nil || audited != 2 {
		t.Fatalf("audit rows for set_retention = %d (err %v), want 2", audited, err)
	}
	for _, bad := range []int{0, 36501} {
		if _, err := f.gw.SetMetricTypeRetention(f.ctx, "", "tcp-connect-time", days(bad)); !errors.Is(err, storage.ErrMetricTypeInvalid) {
			t.Errorf("an override of %d days = %v, want ErrMetricTypeInvalid", bad, err)
		}
	}
	if _, err := f.gw.SetMetricTypeRetention(f.ctx, "", "no-such-type", days(3)); !errors.Is(err, storage.ErrMetricTypeNotFound) {
		t.Errorf("an unknown type = %v, want ErrMetricTypeNotFound", err)
	}
	if mt, err := f.gw.SetMetricTypeRetention(f.ctx, "", "tcp-connect-time", nil); err != nil || mt.RetentionDays != nil {
		t.Fatalf("clear the override: %+v, err %v; want none", mt, err)
	}
}

// TestARetentionOverrideSurvivesTheBootSeed: the boot seed is authoritative on
// a shipped type's definition, but an override is the install's, so a re-seed
// (every server start) keeps it.
func TestARetentionOverrideSurvivesTheBootSeed(t *testing.T) {
	f := newPruneFixture(t)
	if err := seed.Run(f.ctx, f.gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := f.gw.SetMetricTypeRetention(f.ctx, "", "tcp-connect-time", days(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.SetPropertyTypeRetention(f.ctx, "", "endpoint-reachable", days(365)); err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(f.ctx, f.gw); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	mt, _ := f.gw.GetMetricType(f.ctx, "tcp-connect-time")
	pt, _ := f.gw.GetPropertyType(f.ctx, "endpoint-reachable")
	if mt.RetentionDays == nil || *mt.RetentionDays != 3 || pt.RetentionDays == nil || *pt.RetentionDays != 365 {
		t.Fatalf("after a re-seed: metric %v, property %v; want the overrides kept", mt.RetentionDays, pt.RetentionDays)
	}
}

// TestPruneSamplesHonorsATypesOverride: a type with an override leaves its
// lane's default window and is pruned by its own, shorter or longer. The lane
// prune skips it; the type's own prune takes it to its window.
func TestPruneSamplesHonorsATypesOverride(t *testing.T) {
	f := newPruneFixture(t)
	for _, name := range []string{"rt-short", "rt-default", "rt-long"} {
		mustExec(t, f.conn, `insert into metric_type (name, data_type) values ($1, 'float')`, name)
	}
	if _, err := f.gw.SetMetricTypeRetention(f.ctx, "", "rt-short", days(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.SetMetricTypeRetention(f.ctx, "", "rt-long", days(30)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := func(typ string, age time.Duration) {
		t.Helper()
		if err := f.gw.MakeMetricMonths(f.ctx, now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		mustExec(t, f.conn, storagetest.WideMetricInsert(`ts, owner_kind, component_id, metric_type_id, instance, provenance, value`,
			`values ($1, 'component', (select id from component where name = 'prune-c1'), (select id from metric_type where name = $2), '', 'observed', 1)`),
			now.Add(-age), typ)
	}
	for _, typ := range []string{"rt-short", "rt-default", "rt-long"} {
		row(typ, 20*24*time.Hour)
		row(typ, 5*24*time.Hour)
		row(typ, time.Hour)
	}
	count := func(typ string) int {
		t.Helper()
		var n int
		if err := f.conn.QueryRow(f.ctx, `select count(*) from `+storagetest.MetricRows+` metric where metric_type_id = (select id from metric_type where name = $1)`, typ).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// The lane's default window, 14 days: only the type without an override.
	if n := f.prune("metric", now.Add(-14*24*time.Hour), 100); n != 1 {
		t.Fatalf("the lane prune deleted %d rows, want 1 (rt-default's 20-day row only)", n)
	}
	overrides, err := f.gw.RetentionOverrides(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]storage.TypeRetention{}
	for _, o := range overrides {
		byName[o.TypeName] = o
	}
	if len(byName) != 2 || byName["rt-short"].Days != 3 || byName["rt-long"].Days != 30 || byName["rt-short"].Lane != "metric" {
		t.Fatalf("overrides = %+v, want rt-short 3 and rt-long 30 on the metric lane", overrides)
	}
	// A typed prune carries the window the pass read. If the override has
	// changed since (an admin cleared or raised it mid-pass), it prunes
	// nothing: the next pass reads the new window.
	if n, err := f.gw.PruneSamplesOfType(f.ctx, "metric", byName["rt-short"].TypeID, 1, now.AddDate(0, 0, -1), 100); err != nil || n != 0 {
		t.Fatalf("a typed prune by a stale window (1 day, the type now 3) deleted %d (err %v), want 0", n, err)
	}
	for _, typ := range []string{"rt-short", "rt-long"} {
		o := byName[typ]
		if _, err := f.gw.PruneSamplesOfType(f.ctx, o.Lane, o.TypeID, o.Days, now.AddDate(0, 0, -o.Days), 100); err != nil {
			t.Fatal(err)
		}
	}
	got := strconv.Itoa(count("rt-short")) + "," + strconv.Itoa(count("rt-default")) + "," + strconv.Itoa(count("rt-long"))
	if got != "1,2,3" {
		t.Fatalf("rows left (short, default, long) = %s, want 1,2,3: short keeps its hour-old row, default its 5-day and hour-old, long all three", got)
	}
}
