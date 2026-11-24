package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestSeverityLevels proves severity is a catalog of named, ranked levels:
// the boot seed lands five spaced official levels and a re-run changes
// nothing; an operator inserts a level between two without renumbering, may
// rename it (its alarms follow), and may not touch an official one; an alarm
// names a level that exists; and a component's verdict reads the level's
// rank, so high and above is an outage and anything lower degrades.
func TestSeverityLevels(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	gw := f.gw

	levels, err := gw.ListSeverityLevels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name string
		rank int
	}{{"info", 10}, {"warning", 20}, {"average", 30}, {"high", 40}, {"disaster", 50}}
	if len(levels) != len(want) {
		t.Fatalf("seeded %d levels, want %d: %+v", len(levels), len(want), levels)
	}
	for i, w := range want {
		if levels[i].Name != w.name || levels[i].Rank != w.rank || !levels[i].Official || levels[i].Label == "" || levels[i].Color == "" {
			t.Errorf("level %d = %+v, want official %s at rank %d with a label and color", i, levels[i], w.name, w.rank)
		}
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	if again, _ := gw.ListSeverityLevels(ctx); len(again) != len(want) {
		t.Fatalf("a second seed run left %d levels", len(again))
	}

	// An operator level between average and high, without renumbering.
	major, err := gw.CreateSeverityLevel(ctx, "", storage.SeverityLevel{Name: "major", Label: "Major", Color: "orange", Rank: 35}, all)
	if err != nil || major.Official {
		t.Fatalf("create major: %+v (%v), want an operator level", major, err)
	}
	for _, bad := range []storage.SeverityLevel{
		{Name: "dup-rank", Rank: 35, Color: "red"},
		{Name: "high", Rank: 45, Color: "red"},
		{Name: "no-rank", Rank: 0, Color: "red"},
		{Name: "bad-color", Rank: 36, Color: "chartreuse"},
		{Name: "Bad Name", Rank: 37, Color: "red"},
	} {
		if _, err := gw.CreateSeverityLevel(ctx, "", bad, all); err == nil {
			t.Errorf("level %+v was accepted", bad)
		}
	}
	label := "Hijacked"
	if _, err := gw.UpdateSeverityLevel(ctx, "", "high", storage.SeverityLevelPatch{Label: &label}, all); !errors.Is(err, storage.ErrSeverityLevelOfficial) {
		t.Fatalf("editing an official level: err %v, want ErrSeverityLevelOfficial", err)
	}
	if err := gw.DeleteSeverityLevel(ctx, "", "info", all); !errors.Is(err, storage.ErrSeverityLevelOfficial) {
		t.Fatalf("deleting an official level: err %v, want ErrSeverityLevelOfficial", err)
	}

	// Writing a level takes an all-scope grant, since its rank moves health
	// fleet-wide; reading stays catalog-shaped and takes no scope.
	none := scope.Set{}
	if _, err := gw.CreateSeverityLevel(ctx, "", storage.SeverityLevel{Name: "scoped", Color: "red", Rank: 36}, none); !errors.Is(err, storage.ErrSeverityLevelForbidden) {
		t.Fatalf("a scoped create: err %v, want ErrSeverityLevelForbidden", err)
	}
	if _, err := gw.UpdateSeverityLevel(ctx, "", "major", storage.SeverityLevelPatch{Label: &label}, none); !errors.Is(err, storage.ErrSeverityLevelForbidden) {
		t.Fatalf("a scoped update: err %v, want ErrSeverityLevelForbidden", err)
	}
	if err := gw.DeleteSeverityLevel(ctx, "", "major", none); !errors.Is(err, storage.ErrSeverityLevelForbidden) {
		t.Fatalf("a scoped delete: err %v, want ErrSeverityLevelForbidden", err)
	}
	if l, err := gw.GetSeverityLevel(ctx, "major"); err != nil || l.Label != "Major" {
		t.Fatalf("after the refused writes major reads %+v (%v), want it unchanged", l, err)
	}

	// An alarm names a level that exists.
	if _, err := gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "critical", Message: "old word"}); !errors.Is(err, storage.ErrAlarmSeverity) {
		t.Fatalf("raising at an unknown level: err %v, want ErrAlarmSeverity", err)
	}

	// The verdict reads the rank: an operator level below high degrades.
	a, err := gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "major", Message: "fan noise"})
	if err != nil {
		t.Fatal(err)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "degraded" {
		t.Fatalf("a major (rank 35) alarm left bar-1 %q, want degraded", v)
	}
	// Renaming the operator level carries its open alarm with it.
	renamed := "serious"
	if _, err := gw.UpdateSeverityLevel(ctx, "", "major", storage.SeverityLevelPatch{Name: &renamed}, all); err != nil {
		t.Fatal(err)
	}
	listed, err := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if err != nil || len(listed) != 1 || listed[0].Severity != "serious" {
		t.Fatalf("after the rename the alarm reads %+v (%v), want serious", listed, err)
	}
	if err := gw.DeleteSeverityLevel(ctx, "", "serious", all); !errors.Is(err, storage.ErrSeverityLevelInUse) {
		t.Fatalf("deleting a level an alarm uses: err %v, want ErrSeverityLevelInUse", err)
	}
	if err := gw.ClearAlarm(ctx, "", "bar-1", a.ID); err != nil {
		t.Fatal(err)
	}

	// high and above is an outage.
	if _, err := gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "high", Message: "no route"}); err != nil {
		t.Fatal(err)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "outage" {
		t.Fatalf("a high alarm left bar-1 %q, want outage", v)
	}
}

// Changing a level's declared health effect moves its component's recorded
// health with it, and a re-rank alone does not (#2558).
func TestSeverityEffectChangeMovesHealth(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.CreateSeverityLevel(ctx, "", storage.SeverityLevel{Name: "major", Color: "orange", Rank: 35}, all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "major", Message: "fan noise"}); err != nil {
		t.Fatal(err)
	}
	rank := 45
	if _, err := f.gw.UpdateSeverityLevel(ctx, "", "major", storage.SeverityLevelPatch{Rank: &rank}, all); err != nil {
		t.Fatal(err)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "degraded" {
		t.Fatalf("a re-rank across the old floor left bar-1 %q, want degraded (the effect is declared, not read from rank)", v)
	}
	for _, step := range []struct{ effect, want string }{{"outage", "outage"}, {"none", "healthy"}, {"degraded", "degraded"}} {
		e := step.effect
		if _, err := f.gw.UpdateSeverityLevel(ctx, "", "major", storage.SeverityLevelPatch{HealthEffect: &e}, all); err != nil {
			t.Fatal(err)
		}
		if _, v := f.recorded(t, ctx, "component", "bar-1"); v != step.want {
			t.Fatalf("after declaring major %s bar-1 reads %q, want %s", e, v, step.want)
		}
	}
}

// An open alarm at a none level leaves its component and its system healthy and
// still lists the alarm; a high alarm on the same component takes it out and
// shorts its slot; a custom level created with health_effect none behaves like
// info (#2558).
func TestSeverityNoneEffectDoesNotBadgeTheComponent(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	_, sysBefore := f.recorded(t, ctx, "system", "hq-huddle")

	if _, err := f.gw.CreateSeverityLevel(ctx, "", storage.SeverityLevel{Name: "notice", Color: "blue", Rank: 15, HealthEffect: "none"}, all); err != nil {
		t.Fatal(err)
	}
	for _, level := range []string{"info", "notice"} {
		a, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: level, Message: "firmware out of date"})
		if err != nil {
			t.Fatal(err)
		}
		if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "healthy" {
			t.Fatalf("an open %s alarm left bar-1 %q, want healthy", level, v)
		}
		if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != sysBefore {
			t.Fatalf("an open %s alarm moved the system to %q, want %q", level, v, sysBefore)
		}
		if listed, err := f.gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{}); err != nil || len(listed) == 0 {
			t.Fatalf("the %s alarm is not listed on the component: %+v (%v)", level, listed, err)
		}
		if err := f.gw.ClearAlarm(ctx, "", "bar-1", a.ID); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "info", Message: "firmware out of date"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "high", Message: "no route"}); err != nil {
		t.Fatal(err)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "outage" {
		t.Fatalf("a high alarm beside an info one left bar-1 %q, want outage", v)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v == sysBefore {
		t.Fatalf("a high alarm left the system %q, want its slot short and the verdict moved", v)
	}
}

// The boot seed declaring a level's effect recomputes the components behind it:
// a fleet upgraded with an open info alarm (recorded degraded under the old
// rank rule) reads healthy after the seed runs, with no stuck row (#2558).
func TestSeedDeclaringEffectRecomputesOpenAlarms(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "info", Message: "firmware out of date"}); err != nil {
		t.Fatal(err)
	}
	// Simulate the pre-upgrade state: info degraded its component.
	if _, err := f.conn.Exec(ctx, `update severity_level set health_effect = 'degraded' where name = 'info'`); err != nil {
		t.Fatal(err)
	}
	w, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "warning", Message: "touch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", w.ID); err != nil {
		t.Fatal(err)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "degraded" {
		t.Fatalf("setup: bar-1 reads %q, want the old degraded", v)
	}
	if err := seed.Run(ctx, f.gw); err != nil {
		t.Fatal(err)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "healthy" {
		t.Fatalf("after the seed declared info none bar-1 reads %q, want healthy", v)
	}
}

// An official level never takes over an operator's (#1431): one of the same
// name leaves it alone and the boot goes on; a rank a differently named
// operator level holds is refused with a message naming it, not silently
// overwritten and not a raw constraint error.
func TestSeverityOfficialUpsertLeavesAnOperatorLevelAlone(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.CreateSeverityLevel(ctx, "", storage.SeverityLevel{Name: "p1", Color: "red", Rank: 45}, all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.UpsertSeverityLevel(ctx, storage.SeverityLevel{Name: "p1", Color: "red", Rank: 60, Official: true}); err != nil {
		t.Errorf("an official level named like the operator's: err %v, want nil", err)
	}
	err := f.gw.UpsertSeverityLevel(ctx, storage.SeverityLevel{Name: "catastrophe", Color: "darkred", Rank: 45, Official: true})
	if !errors.Is(err, storage.ErrSeverityLevelExists) || !strings.Contains(err.Error(), "p1") {
		t.Errorf("an official level at the operator's rank: err %v, want ErrSeverityLevelExists naming the operator level p1", err)
	}
	if got, _ := f.gw.GetSeverityLevel(ctx, "p1"); got == nil || got.Official || got.Rank != 45 {
		t.Fatalf("p1 = %+v, want the operator's level untouched", got)
	}
}
