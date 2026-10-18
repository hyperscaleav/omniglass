package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestSeverityLevels proves severity is a registry of named, ranked levels:
// the boot seed lands five spaced official levels and a re-run changes
// nothing; an operator inserts a level between two without renumbering, may
// rename it (its alarms follow), and may not touch an official one; an alarm
// names a level that exists; and a component's verdict reads the level's
// rank, so high and above is an outage and anything lower degrades.
func TestSeverityLevels(t *testing.T) {
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
	major, err := gw.CreateSeverityLevel(ctx, "", storage.SeverityLevel{Name: "major", Label: "Major", Color: "orange", Rank: 35})
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
		if _, err := gw.CreateSeverityLevel(ctx, "", bad); err == nil {
			t.Errorf("level %+v was accepted", bad)
		}
	}
	label := "Hijacked"
	if _, err := gw.UpdateSeverityLevel(ctx, "", "high", storage.SeverityLevelPatch{Label: &label}); !errors.Is(err, storage.ErrSeverityLevelOfficial) {
		t.Fatalf("editing an official level: err %v, want ErrSeverityLevelOfficial", err)
	}
	if err := gw.DeleteSeverityLevel(ctx, "", "info"); !errors.Is(err, storage.ErrSeverityLevelOfficial) {
		t.Fatalf("deleting an official level: err %v, want ErrSeverityLevelOfficial", err)
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
	if _, err := gw.UpdateSeverityLevel(ctx, "", "major", storage.SeverityLevelPatch{Name: &renamed}); err != nil {
		t.Fatal(err)
	}
	listed, err := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if err != nil || len(listed) != 1 || listed[0].Severity != "serious" {
		t.Fatalf("after the rename the alarm reads %+v (%v), want serious", listed, err)
	}
	if err := gw.DeleteSeverityLevel(ctx, "", "serious"); !errors.Is(err, storage.ErrSeverityLevelInUse) {
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

// Re-ranking a level an open alarm uses moves its component's recorded
// health with it: across the outage floor and back.
func TestSeverityReRankMovesHealth(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.CreateSeverityLevel(ctx, "", storage.SeverityLevel{Name: "major", Color: "orange", Rank: 35}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "major", Message: "fan noise"}); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		rank int
		want string
	}{{45, "outage"}, {35, "degraded"}} {
		r := step.rank
		if _, err := f.gw.UpdateSeverityLevel(ctx, "", "major", storage.SeverityLevelPatch{Rank: &r}); err != nil {
			t.Fatal(err)
		}
		if _, v := f.recorded(t, ctx, "component", "bar-1"); v != step.want {
			t.Fatalf("after re-ranking major to %d bar-1 reads %q, want %s", r, v, step.want)
		}
	}
}

// An official level never takes over an operator's: a name or a rank an
// operator level already holds is refused with a message naming it, not
// silently overwritten and not a raw constraint error.
func TestSeverityOfficialUpsertRefusesAnOperatorLevel(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.CreateSeverityLevel(ctx, "", storage.SeverityLevel{Name: "p1", Color: "red", Rank: 45}); err != nil {
		t.Fatal(err)
	}
	for _, l := range []storage.SeverityLevel{
		{Name: "p1", Color: "red", Rank: 60, Official: true},
		{Name: "catastrophe", Color: "darkred", Rank: 45, Official: true},
	} {
		err := f.gw.UpsertSeverityLevel(ctx, l)
		if !errors.Is(err, storage.ErrSeverityLevelExists) || !strings.Contains(err.Error(), "p1") {
			t.Errorf("official %+v: err %v, want ErrSeverityLevelExists naming the operator level p1", l, err)
		}
	}
	if got, _ := f.gw.GetSeverityLevel(ctx, "p1"); got == nil || got.Official || got.Rank != 45 {
		t.Fatalf("p1 = %+v, want the operator's level untouched", got)
	}
}
