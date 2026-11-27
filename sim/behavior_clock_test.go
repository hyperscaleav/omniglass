package sim

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/profile"
)

// pinned is a clock that reads whatever *at holds.
func pinned(at *time.Time) func() time.Time { return func() time.Time { return *at } }

// A behavior reads the wall clock through its device's clock (#2754), so a
// room's meeting schedule can sit on the hour and a test can pin the hour.
func TestABehaviorsNowIsItsDevicesClock(t *testing.T) {
	at := time.Date(2026, 11, 26, 10, 7, 30, 0, time.UTC)
	funcs := behaviorFuncs(newDeviceRand("bar"), pinned(&at))
	now := funcs["now"].(func() int64)
	if got := now(); got != at.Unix() {
		t.Fatalf("now = %d, want the pinned %d", got, at.Unix())
	}
	at = at.Add(time.Minute)
	if got := now(); got != at.Unix() {
		t.Fatalf("now after the clock moved = %d, want %d", got, at.Unix())
	}

	d := newTestDevice(t, "bar", map[string]any{"t": int64(0)})
	d.SetClock(pinned(&at))
	if err := d.AddBehaviorFromProfile(&profile.Behavior{Name: "stamp", Schedule: profile.Schedule{Interval: "60s"},
		Mutations: []profile.Mutation{{Path: "t", Value: "{{ now }}"}}}); err != nil {
		t.Fatal(err)
	}
	d.TickBehavior("stamp")
	if got := d.ReadPath("t"); got != at.Unix() {
		t.Fatalf("a tick stamped %v, want the device clock's %d", got, at.Unix())
	}
}

// nextSlot is the next wall-clock boundary of a slot strictly after t: a
// room booked in 15, 30 or 60 minute slots starts its meetings on them.
func TestNextSlotIsTheNextWallClockBoundary(t *testing.T) {
	next := behaviorFuncs(newDeviceRand("bar"), time.Now)["nextSlot"].(func(any, any) (int64, error))
	at := func(h, m, s int) int64 { return time.Date(2026, 11, 26, h, m, s, 0, time.UTC).Unix() }
	for _, tc := range []struct {
		t       int64
		minutes int
		want    int64
	}{
		{at(10, 7, 30), 15, at(10, 15, 0)},
		{at(10, 7, 30), 30, at(10, 30, 0)},
		{at(10, 7, 30), 60, at(11, 0, 0)},
		{at(10, 30, 0), 30, at(11, 0, 0)}, // on a boundary: the next one
		{at(23, 59, 59), 60, at(24, 0, 0)},
	} {
		got, err := next(tc.t, tc.minutes)
		if err != nil || got != tc.want {
			t.Errorf("nextSlot(%s, %d) = %d (%v), want %s", time.Unix(tc.t, 0).UTC(), tc.minutes, got, err, time.Unix(tc.want, 0).UTC())
		}
	}
	if _, err := next(at(10, 0, 0), 0); err == nil {
		t.Error("nextSlot with a zero slot, want an error")
	}
}

// randInt and chance draw from the device's own source (#2754): inclusive
// bounds, the probability honoured, and the same device drawing the same
// sequence on every run.
func TestRandIntAndChanceDrawFromTheDevicesSource(t *testing.T) {
	draw := func(id string) ([]int64, int) {
		f := behaviorFuncs(newDeviceRand(id), time.Now)
		randInt := f["randInt"].(func(any, any) (int64, error))
		chance := f["chance"].(func(float64) bool)
		var ints []int64
		hits := 0
		for range 2000 {
			n, err := randInt(-2, 5)
			if err != nil {
				t.Fatal(err)
			}
			ints = append(ints, n)
			if chance(0.5) {
				hits++
			}
		}
		return ints, hits
	}
	a, hits := draw("bar")
	seen := map[int64]bool{}
	for _, n := range a {
		if n < -2 || n > 5 {
			t.Fatalf("randInt(-2, 5) = %d", n)
		}
		seen[n] = true
	}
	if len(seen) != 8 {
		t.Errorf("randInt(-2, 5) drew %v, want every value from -2 to 5", seen)
	}
	if hits < 900 || hits > 1100 {
		t.Errorf("chance(0.5) hit %d of 2000", hits)
	}
	b, _ := draw("bar")
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("the same device drew a different sequence")
		}
	}
	f := behaviorFuncs(newDeviceRand("bar"), time.Now)
	chance := f["chance"].(func(float64) bool)
	for range 100 {
		if chance(0) || !chance(1) {
			t.Fatal("chance(0) hit or chance(1) missed")
		}
	}
}
