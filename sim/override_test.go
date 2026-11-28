package sim

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// TestSetBehaviorOverrides_ValidationRejects pins the rejection surface
// on the apply path. A bad expression / missing path / non-positive
// interval must leave the device's existing override set untouched,
// otherwise a malformed scenario edit would silently wipe the working
// overrides the operator already applied.
func TestSetBehaviorOverrides_ValidationRejects(t *testing.T) {
	d := newTestDevice(t, "d-1", map[string]any{"x": int64(0)})
	// Install a good override first.
	if err := d.SetBehaviorOverrides([]BehaviorOverrideSpec{{
		OverrideID: "good",
		Path:       "x",
		Expression: "1",
		IntervalMs: 10000,
	}}); err != nil {
		t.Fatalf("baseline SetBehaviorOverrides: %v", err)
	}
	defer d.StopBehaviorOverrides()

	cases := []struct {
		name string
		spec BehaviorOverrideSpec
	}{
		{"missing path", BehaviorOverrideSpec{OverrideID: "bad", Path: "", Expression: "1", IntervalMs: 10000}},
		{"missing expression", BehaviorOverrideSpec{OverrideID: "bad", Path: "x", Expression: "", IntervalMs: 10000}},
		{"zero interval", BehaviorOverrideSpec{OverrideID: "bad", Path: "x", Expression: "1", IntervalMs: 0}},
		{"negative interval", BehaviorOverrideSpec{OverrideID: "bad", Path: "x", Expression: "1", IntervalMs: -100}},
		{"malformed template", BehaviorOverrideSpec{OverrideID: "bad", Path: "x", Expression: "{{ ", IntervalMs: 10000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := d.SetBehaviorOverrides([]BehaviorOverrideSpec{tc.spec})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			// Pre-existing good override must still be active.
			ids := d.OverrideIDs()
			if len(ids) != 1 || ids[0] != "good" {
				t.Errorf("after rejected set, ids = %v, want [good]", ids)
			}
		})
	}
}

// TestSetBehaviorOverrides_FullReplace covers the contract from omniglass-lab#102:
// an override write is always a full set replacement, so calling
// SetBehaviorOverrides with [B, C] when [A, B] is currently running
// must result in exactly [B, C] active.
func TestSetBehaviorOverrides_FullReplace(t *testing.T) {
	d := newTestDevice(t, "d-1", map[string]any{
		"a": int64(0), "b": int64(0), "c": int64(0),
	})
	defer d.StopBehaviorOverrides()

	apply := func(specs ...BehaviorOverrideSpec) {
		t.Helper()
		if err := d.SetBehaviorOverrides(specs); err != nil {
			t.Fatalf("SetBehaviorOverrides: %v", err)
		}
	}

	apply(
		BehaviorOverrideSpec{OverrideID: "A", Path: "a", Expression: "1", IntervalMs: 10000},
		BehaviorOverrideSpec{OverrideID: "B", Path: "b", Expression: "1", IntervalMs: 10000},
	)
	if ids := d.OverrideIDs(); !stringSliceEqual(ids, []string{"A", "B"}) {
		t.Fatalf("after first apply, ids = %v, want [A B]", ids)
	}

	apply(
		BehaviorOverrideSpec{OverrideID: "B", Path: "b", Expression: "1", IntervalMs: 10000},
		BehaviorOverrideSpec{OverrideID: "C", Path: "c", Expression: "1", IntervalMs: 10000},
	)
	if ids := d.OverrideIDs(); !stringSliceEqual(ids, []string{"B", "C"}) {
		t.Errorf("after second apply, ids = %v, want [B C]", ids)
	}

	// Empty set clears.
	apply()
	if ids := d.OverrideIDs(); len(ids) != 0 {
		t.Errorf("after clear, ids = %v, want []", ids)
	}
}

// TestOverrideTickWritesState applies a single override, fires one
// synchronous tick, and verifies the state mutation lands. Mirrors the
// existing TestBehaviorTickAppliesMutation pattern for confidence that
// the override pipeline is wired the same way the profile-behavior
// pipeline is.
func TestOverrideTickWritesState(t *testing.T) {
	d := newTestDevice(t, "d-1", map[string]any{
		"status": map[string]any{
			"dsp": map[string]any{
				"cpu_load": float64(20),
			},
		},
	})
	defer d.StopBehaviorOverrides()

	if err := d.SetBehaviorOverrides([]BehaviorOverrideSpec{{
		OverrideID: "cpu-spike",
		Path:       "status.dsp.cpu_load",
		// Fixed value so the test doesn't need to seed rng.
		Expression: "90",
		IntervalMs: 10000,
	}}); err != nil {
		t.Fatalf("SetBehaviorOverrides: %v", err)
	}

	d.overrideRuntimeTickAll()

	got := d.State()["status"].(map[string]any)["dsp"].(map[string]any)["cpu_load"]
	if !numericEquals(got, 90) {
		t.Errorf("cpu_load = %v (%T), want 90", got, got)
	}
}

// TestOverridePublishesCauseAttribution pins the SOURCE-column
// attribution path: every diff a scenario override writes must carry
// the "override:<override_id>" cause so the console can render scenario
// activity distinctly from profile-behavior, route, and command
// changes. Without this, operators can't tell scenario-driven state
// changes apart from anything else in the event log.
func TestOverridePublishesCauseAttribution(t *testing.T) {
	d := newTestDevice(t, "d-1", map[string]any{
		"status": map[string]any{
			"device": map[string]any{
				"counter": int64(0),
			},
		},
	})
	defer d.StopBehaviorOverrides()

	var seenCauses []string
	d.DiffPublisher = func(c StateChange) { seenCauses = append(seenCauses, c.Cause) }

	if err := d.SetBehaviorOverrides([]BehaviorOverrideSpec{{
		OverrideID: "spike-cpu",
		Path:       "status.device.counter",
		Expression: "42",
		IntervalMs: 10000,
	}}); err != nil {
		t.Fatalf("SetBehaviorOverrides: %v", err)
	}
	d.overrideRuntimeTickAll()
	if len(seenCauses) != 1 {
		t.Fatalf("expected 1 published diff, got %d (%v)", len(seenCauses), seenCauses)
	}
	if seenCauses[0] != "override:spike-cpu" {
		t.Errorf("cause = %q, want override:spike-cpu", seenCauses[0])
	}
}

// TestTickSkipsOverriddenPath is the path-overlap contract from
// omniglass-lab#102: while a scenario override is active on a path, a family's
// tick that would otherwise write the same path skips that write. The rest of
// the tick continues, only the overlapping change is dropped, not the whole tick.
func TestTickSkipsOverriddenPath(t *testing.T) {
	d := newTestDevice(t, "d-1", map[string]any{
		"status": map[string]any{
			"device": map[string]any{
				"uptime":   int64(0),
				"cpu_load": float64(20),
			},
		},
	})
	defer d.StopBehaviorOverrides()

	// The family's tick writes both uptime and cpu_load every time it runs.
	count := int64(0)
	if err := d.addHandlers(spec.Handlers{Ticks: []spec.Tick{{
		Name:     "uptime_and_cpu",
		Interval: time.Second,
		Writes:   []string{"status.device.uptime", "status.device.cpu_load"},
		Run: func(_ spec.Device, _ spec.TickContext) []surface.Change {
			count++
			return []surface.Change{
				{Path: "status.device.uptime", Value: 5 * count},
				{Path: "status.device.cpu_load", Value: float64(20 + count)},
			}
		},
	}}}, spec.Model{}); err != nil {
		t.Fatalf("addHandlers: %v", err)
	}

	// Scenario override claims cpu_load.
	if err := d.SetBehaviorOverrides([]BehaviorOverrideSpec{{
		OverrideID: "freeze-cpu",
		Path:       "status.device.cpu_load",
		Expression: "90",
		IntervalMs: 10000,
	}}); err != nil {
		t.Fatalf("SetBehaviorOverrides: %v", err)
	}

	// Tick: uptime advances, cpu_load is skipped.
	d.TickBehavior("uptime_and_cpu")
	s := d.State()["status"].(map[string]any)["device"].(map[string]any)
	if !numericEquals(s["uptime"], 5) {
		t.Errorf("uptime = %v, want 5 (the tick should write the paths no override holds)", s["uptime"])
	}
	if !numericEquals(s["cpu_load"], 20) {
		t.Errorf("cpu_load = %v, want 20 (the tick should skip the overridden path)", s["cpu_load"])
	}

	// Override tick: cpu_load advances to the override's value.
	d.overrideRuntimeTickAll()
	s = d.State()["status"].(map[string]any)["device"].(map[string]any)
	if !numericEquals(s["cpu_load"], 90) {
		t.Errorf("after override tick: cpu_load = %v, want 90", s["cpu_load"])
	}

	// Once the override is removed, the next tick writes cpu_load again: the
	// skip is scoped to the active override window, not permanent.
	if err := d.SetBehaviorOverrides(nil); err != nil {
		t.Fatalf("clear SetBehaviorOverrides: %v", err)
	}
	d.TickBehavior("uptime_and_cpu")
	s = d.State()["status"].(map[string]any)["device"].(map[string]any)
	if !numericEquals(s["cpu_load"], 22) {
		t.Errorf("after override cleared: cpu_load = %v, want 22", s["cpu_load"])
	}
}

// numericEquals tolerates the difference between int64 / float64
// representations of the same numeric value. The override pipeline can
// produce either depending on whether the template renders an
// integer-shaped string ("90" → int64) or a float-shaped one ("90.0"
// → float64); tests want to assert on numeric equality, not Go type.
func numericEquals(got any, want float64) bool {
	switch v := got.(type) {
	case int64:
		return float64(v) == want
	case float64:
		return v == want
	case int:
		return float64(v) == want
	}
	return false
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
