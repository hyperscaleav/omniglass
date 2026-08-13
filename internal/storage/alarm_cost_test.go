package storage_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
)

// The round-trip cost of the alarm WRITE path (#674), the one hot path with a
// two-digit statement count and, until this file, no instrument on it.
//
// Why counting and not timing. A benchmark of this chain was written during
// #651, measured at roughly 21.7 to 23.4 ms over its round trips, and
// deliberately dropped: against the measured 262 us round-trip floor it is about
// three-quarters transport, so doubling every query plan inside it would move the
// number by ~25%, barely above its own 14-18% spread. A benchmark whose signal is
// buried under transport is a number nobody can act on. Counting is deterministic
// and does not care that transport dominates. ADR-0094 is the entry; bench_test.go
// carries the tombstone where the benchmark would have been.
//
// # What the count actually grows along, measured rather than guessed
//
// #674 named two candidates, the number of alarms and the number of roles the
// component fills, and asked which. Measured over seven fixtures, two of them
// held back as out-of-sample predictions:
//
//	systems staffed (S) | distinct ancestor locations (L) | prior alarms | raise | clear
//	         1          |               3                 |       0      |   29  |   29
//	         1          |               3                 |      20      |   29  |   29
//	         3          |               3                 |       0      |   39  |   39
//	         6          |               3                 |       0      |   54  |   54
//	         3          |               5                 |       0      |   47  |   47
//	         6          |               5                 |       0      |   62  |   62   (predicted)
//	         2          |               4                 |       5      |   38  |   38   (predicted)
//
// One closed form fits all seven, and predicted the last two before they were
// run:
//
//	raise = clear = 12 + 5*S + 4*L
//
// So the first candidate is FALSE and the second is TRUE. The count is flat in
// the number of alarms, because activeAlarmSeverities folds a component's open
// alarms with one array_agg; it grows at five statements per SLOT the component
// fills (each staffed system takes its advisory lock, resolves its roles, and
// records a verdict, which re-takes the lock) and at four per distinct ancestor
// LOCATION above those systems (lock, verdict, record, which locks again).
//
// The 58 in the issue is this fixture's minimum, S=1 and L=3, counted as the
// raise and the clear together: 29 + 29.
//
// That growth is PINNED here rather than papered over with a comfortable ceiling,
// exactly as #671's defect was pinned before it was fixed, and reducing it is
// explicitly out of scope: a reduction with no measurement in front of it cannot
// be shown to have worked. This file is that measurement.

// alarmProbe is an estate whose two growth dimensions are set independently:
// `staffs` systems, spread across `roomsN` rooms under one building under one
// campus, with ONE component staffed into the single role each system inherits
// from the shared standard. So the slots the component fills is exactly staffs,
// and the distinct ancestor locations above those systems is roomsN + 2.
func alarmProbe(t *testing.T, staffs, roomsN int) (storage.Gateway, *querycount.Counter) {
	t.Helper()
	gw, counter := storagetest.NewCountingDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	sc := scope.Set{All: true}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, sc); err != nil {
		t.Fatalf("create campus: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-b1", LocationType: "building", ParentName: strptr("hq")}, sc); err != nil {
		t.Fatalf("create building: %v", err)
	}
	roomNames := make([]string, 0, roomsN)
	for i := range roomsN {
		n := fmt.Sprintf("hq-r%d", i)
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: n, LocationType: "room", ParentName: strptr("hq-b1")}, sc); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
		roomNames = append(roomNames, n)
	}
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "probe-std", DisplayName: "Probe"}); err != nil {
		t.Fatalf("upsert standard: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", "probe-std", storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table microphone", Quorum: 1,
		AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &bar}, sc, sc, sc, sc); err != nil {
		t.Fatalf("create component: %v", err)
	}
	std := "probe-std"
	for i := range staffs {
		name := fmt.Sprintf("sys-%d", i)
		if _, err := gw.CreateSystem(ctx, "",
			storage.SystemSpec{Name: name, StandardID: &std, LocationName: strptr(roomNames[i%len(roomNames)])}, sc, sc); err != nil {
			t.Fatalf("create system %d: %v", i, err)
		}
		if err := gw.AssignRole(ctx, "", name, "table-mic", "bar-1", sc); err != nil {
			t.Fatalf("assign role %d: %v", i, err)
		}
	}
	counter.Reset()
	return gw, counter
}

// measureRaiseClear opens `prior` alarms on the component, then counts the raise
// and the clear of one more. The fixture writes are excluded: the counter is
// reset immediately before each of the two measured calls, so a growing pile of
// prior alarms cannot inflate the number it is supposed to leave alone.
func measureRaiseClear(t *testing.T, gw storage.Gateway, c *querycount.Counter, prior int) (raise, clear int, open int) {
	t.Helper()
	ctx := context.Background()
	for i := range prior {
		if _, err := gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
			Severity: "warning", Message: "prior", DedupKey: fmt.Sprintf("prior-%d", i),
		}); err != nil {
			t.Fatalf("raise prior alarm %d: %v", i, err)
		}
	}
	c.Reset()
	a, err := gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "critical", Message: "measured", DedupKey: "measured",
	})
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	raise = c.N()
	raiseStmts := c.Summary()

	// Read the open set BEFORE clearing, and outside the measured window, so the
	// flatness case can prove the dimension it claims flatness in actually varied.
	alarms, err := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if err != nil {
		t.Fatalf("list alarms: %v", err)
	}
	open = len(alarms)

	c.Reset()
	if err := gw.ClearAlarm(ctx, "", "bar-1", a.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	clear = c.N()
	if raise <= 1 || clear <= 1 {
		t.Fatalf("raise cost %d statements and clear %d: a write path that begins and commits a transaction cannot cost this little, so the counter is not on the seam these calls use\n  %q",
			raise, clear, raiseStmts)
	}
	return raise, clear, open
}

// TestAlarmRecomputeCostIsFlatInAlarmCount holds the property that DOES hold: a
// component's open alarms are folded by one array_agg, so piling more of them on
// costs the recompute nothing.
//
// This is the half of #674 that could most easily have been flat by accident, and
// the guard against that is explicit: the two measurements are required to have
// been taken over genuinely different open-alarm sets before either count is
// compared, because a fixture that quietly deduplicated its alarms would report a
// beautifully flat number while varying nothing at all.
func TestAlarmRecomputeCostIsFlatInAlarmCount(t *testing.T) {
	const ceiling = 29

	quiet, qc := alarmProbe(t, 1, 1)
	lonelyRaise, lonelyClear, lonelyOpen := measureRaiseClear(t, quiet, qc, 0)

	noisy, nc := alarmProbe(t, 1, 1)
	loudRaise, loudClear, loudOpen := measureRaiseClear(t, noisy, nc, 20)

	if loudOpen <= lonelyOpen {
		t.Fatalf("the noisy component held %d open alarms and the quiet one %d: the dimension this claims flatness in never varied, so nothing was measured",
			loudOpen, lonelyOpen)
	}
	if lonelyRaise != loudRaise || lonelyClear != loudClear {
		t.Errorf("the recompute cost %d/%d statements (raise/clear) over %d open alarms and %d/%d over %d: the alarm write path does per-alarm work",
			lonelyRaise, lonelyClear, lonelyOpen, loudRaise, loudClear, loudOpen)
	}
	if loudRaise > ceiling || loudClear > ceiling {
		t.Errorf("the recompute cost %d/%d statements (raise/clear) at this estate's smallest shape, want at most %d each",
			loudRaise, loudClear, ceiling)
	}
}

// TestAlarmRecomputeCostGrowsWithTheOwnersItTouches PINS A DEFECT, not a target,
// and the name says so the way #671's pin did.
//
// The alarm write path is NOT constant. It costs 12 + 5*S + 4*L statements per
// raise and the same per clear, where S is the number of slots the component
// fills and L the number of distinct locations above the systems holding them.
// Every staffed system pays an advisory lock, a role resolution and a recorded
// verdict (which re-takes the lock); every ancestor location pays a lock, a
// verdict query and a record.
//
// Whether that is reducible is deliberately NOT decided here: some of it is
// genuinely per-owner work, and #674 puts a reduction explicitly out of scope
// because a reduction with no measurement in front of it cannot be shown to have
// worked. The number is pinned as an EQUATION rather than as a table of constants
// so that a change is reported as which term moved, and so that a ceiling
// generous enough to look comfortable can never hide the slope underneath it.
func TestAlarmRecomputeCostGrowsWithTheOwnersItTouches(t *testing.T) {
	// A room's ancestors are the building and the campus above it, so L is the
	// room count plus two.
	for _, tc := range []struct{ staffs, rooms int }{
		{1, 1}, {3, 1}, {6, 1}, {3, 3},
	} {
		t.Run(fmt.Sprintf("%d-slots-%d-rooms", tc.staffs, tc.rooms), func(t *testing.T) {
			gw, counter := alarmProbe(t, tc.staffs, tc.rooms)
			raise, clear, _ := measureRaiseClear(t, gw, counter, 0)
			want := 12 + 5*tc.staffs + 4*(tc.rooms+2)
			if raise != want || clear != want {
				t.Errorf("the alarm recompute cost %d statements to raise and %d to clear with %d slots filled over %d ancestor locations, want %d each (12 + 5*slots + 4*locations, #674).\n"+
					"If this failed because the write path was REDUCED, that is a fix and this pin should move with it, stating the new equation. If a term grew, the write path got worse in a dimension nothing else measures.",
					raise, clear, tc.staffs, tc.rooms+2, want)
			}
		})
	}
}
