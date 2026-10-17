package sim

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// newTestDevice constructs a Device with state seeded from defaults.
// Behaviors mutate this state via the same applyMutations pipeline
// routes use, so test setup mirrors a real simulator load.
func newTestDevice(t *testing.T, deviceID string, state map[string]any) *Device {
	t.Helper()
	if state == nil {
		state = map[string]any{}
	}
	return NewDevice(deviceID, deviceID+".lab", net.IPv4(127, 0, 0, 1), "test-profile", 1, state)
}

// TestAddBehaviorFromProfile_ValidationRejectsBadShapes pins the
// guards in AddBehaviorFromProfile. Each rejection corresponds to a
// foot-gun a profile author might leave behind: zero interval, missing
// name, no mutations, malformed duration string.
func TestAddBehaviorFromProfile_ValidationRejectsBadShapes(t *testing.T) {
	d := newTestDevice(t, "d-1", nil)
	cases := []struct {
		name     string
		behavior profile.Behavior
		wantPart string
	}{
		{
			name: "missing_name",
			behavior: profile.Behavior{
				Schedule:  profile.Schedule{Interval: "1s"},
				Mutations: []profile.Mutation{{Path: "x", Value: "1"}},
			},
			wantPart: "name",
		},
		{
			name: "missing_interval",
			behavior: profile.Behavior{
				Name:      "b",
				Mutations: []profile.Mutation{{Path: "x", Value: "1"}},
			},
			wantPart: "interval",
		},
		{
			name: "unparseable_interval",
			behavior: profile.Behavior{
				Name:      "b",
				Schedule:  profile.Schedule{Interval: "not-a-duration"},
				Mutations: []profile.Mutation{{Path: "x", Value: "1"}},
			},
			wantPart: "interval",
		},
		{
			name: "zero_interval",
			behavior: profile.Behavior{
				Name:      "b",
				Schedule:  profile.Schedule{Interval: "0s"},
				Mutations: []profile.Mutation{{Path: "x", Value: "1"}},
			},
			wantPart: "must be positive",
		},
		{
			name: "no_mutations",
			behavior: profile.Behavior{
				Name:     "b",
				Schedule: profile.Schedule{Interval: "1s"},
			},
			wantPart: "no mutations",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := d.AddBehaviorFromProfile(&tc.behavior)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantPart)
			}
			if !contains(err.Error(), tc.wantPart) {
				t.Errorf("error %q does not contain %q", err, tc.wantPart)
			}
		})
	}
}

// TestBehaviorTickAppliesMutation runs a single tick directly via
// tickBehavior (bypassing the goroutine + ticker) and verifies the
// state mutation lands. Bypasses the scheduler so the test is fast and
// deterministic; the scheduler timing is covered separately.
func TestBehaviorTickAppliesMutation(t *testing.T) {
	state := map[string]any{
		"status": map[string]any{
			"device": map[string]any{
				"uptime": int64(0),
			},
		},
	}
	d := newTestDevice(t, "d-1", state)

	err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:     "uptime_tick",
		Schedule: profile.Schedule{Interval: "1s"},
		Mutations: []profile.Mutation{
			{Path: "status.device.uptime", Value: "{{ add .State.status.device.uptime 5 }}"},
		},
	})
	if err != nil {
		t.Fatalf("AddBehaviorFromProfile: %v", err)
	}

	// Directly fire the tick three times. Each call should add 5 to
	// uptime since the mutation reads the current value.
	for i := 1; i <= 3; i++ {
		d.tickBehavior(&d.behaviors[0])
		s := d.State()
		got := s["status"].(map[string]any)["device"].(map[string]any)["uptime"]
		want := int64(5 * i)
		if got != want {
			t.Fatalf("after tick %d: uptime = %v (%T), want %v", i, got, got, want)
		}
	}
}

// TestStartStopBehaviors_RunsAndStops covers the goroutine lifecycle:
// StartBehaviors spawns a runner that ticks at the configured interval;
// StopBehaviors closes the stop channel and waits for the runner to
// exit. Uses a short interval (50ms) and waits 200ms so at least 2-3
// ticks fire before stopping.
func TestStartStopBehaviors_RunsAndStops(t *testing.T) {
	state := map[string]any{
		"counter": int64(0),
	}
	d := newTestDevice(t, "d-1", state)

	var publishedTicks atomic.Int32
	d.DiffPublisher = func(StateChange) {
		publishedTicks.Add(1)
	}

	if err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:     "ticker",
		Schedule: profile.Schedule{Interval: "100ms"},
		Mutations: []profile.Mutation{
			{Path: "counter", Value: "{{ add .State.counter 1 }}"},
		},
	}); err != nil {
		t.Fatalf("AddBehaviorFromProfile: %v", err)
	}

	d.StartBehaviors()
	// 200ms = up to 100ms of phase offset plus enough margin for at
	// least one tick. Kept tight to minimize total package runtime
	// under -race, where a long Sleep here pressures unrelated
	// timing-sensitive tests like TestStreamClient_Reconnects.
	time.Sleep(200 * time.Millisecond)
	d.StopBehaviors()

	got := d.State()["counter"]
	gotInt, ok := got.(int64)
	if !ok {
		t.Fatalf("counter is %T, want int64", got)
	}
	if gotInt < 1 {
		t.Errorf("counter = %d, want >= 1 (behavior didn't tick at all)", gotInt)
	}
	if int(publishedTicks.Load()) != int(gotInt) {
		t.Errorf("DiffPublisher fired %d times but counter is %d (publish/state mismatch)",
			publishedTicks.Load(), gotInt)
	}
}

// TestBehaviorRandStep_DeterministicAcrossRuns verifies that two
// devices with the same id, running the same behavior across the same
// number of ticks, produce identical state. This is the determinism
// the issue calls out: "behaviors are repeatable across runs."
func TestBehaviorRandStep_DeterministicAcrossRuns(t *testing.T) {
	makeDevice := func(id string) *Device {
		state := map[string]any{
			"value": float64(50),
		}
		d := newTestDevice(t, id, state)
		err := d.AddBehaviorFromProfile(&profile.Behavior{
			Name:     "drift",
			Schedule: profile.Schedule{Interval: "1s"},
			Mutations: []profile.Mutation{
				{Path: "value", Value: "{{ clamp 0.0 100.0 (randStep 5.0 .State.value) }}"},
			},
		})
		if err != nil {
			t.Fatalf("AddBehaviorFromProfile: %v", err)
		}
		return d
	}

	a := makeDevice("kestrel-001")
	b := makeDevice("kestrel-001")
	for i := 0; i < 5; i++ {
		a.tickBehavior(&a.behaviors[0])
		b.tickBehavior(&b.behaviors[0])
	}

	av := a.State()["value"]
	bv := b.State()["value"]
	if av != bv {
		t.Errorf("same device id produced divergent state after 5 ticks: a=%v b=%v", av, bv)
	}

	// Sanity: a different device id must produce different state, or
	// the seed isn't actually device-keyed.
	c := makeDevice("kestrel-002")
	for i := 0; i < 5; i++ {
		c.tickBehavior(&c.behaviors[0])
	}
	if av == c.State()["value"] {
		t.Errorf("different device ids produced identical state; seed isn't device-keyed")
	}
}

// TestStopBehaviors_IdempotentWithoutStart calling stop on a device
// that never started behaviors must not block or panic.
func TestStopBehaviors_IdempotentWithoutStart(t *testing.T) {
	d := newTestDevice(t, "d-1", nil)
	if err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:     "tick",
		Schedule: profile.Schedule{Interval: "1s"},
		Mutations: []profile.Mutation{
			{Path: "x", Value: "1"},
		},
	}); err != nil {
		t.Fatalf("AddBehaviorFromProfile: %v", err)
	}
	// Never call Start; Stop should be a no-op.
	done := make(chan struct{})
	go func() {
		d.StopBehaviors()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StopBehaviors blocked when no behaviors were started")
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
