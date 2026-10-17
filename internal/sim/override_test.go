package sim

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
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

// TestProfileBehaviorSkipsOverriddenPath is the path-overlap contract
// from omniglass-lab#102: while a scenario override is active on a path, the
// profile-declared behavior that would otherwise write the same path
// skips that write. The rest of the profile behavior continues, only
// the overlapping mutation is dropped, not the whole behavior.
func TestProfileBehaviorSkipsOverriddenPath(t *testing.T) {
	d := newTestDevice(t, "d-1", map[string]any{
		"status": map[string]any{
			"device": map[string]any{
				"uptime":   int64(0),
				"cpu_load": float64(20),
			},
		},
	})
	defer d.StopBehaviorOverrides()

	// Profile behavior writes both uptime and cpu_load every tick.
	if err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:     "uptime_and_cpu",
		Schedule: profile.Schedule{Interval: "1s"},
		Mutations: []profile.Mutation{
			{Path: "status.device.uptime", Value: "{{ add .State.status.device.uptime 5 }}"},
			{Path: "status.device.cpu_load", Value: "{{ add .State.status.device.cpu_load 1 }}"},
		},
	}); err != nil {
		t.Fatalf("AddBehaviorFromProfile: %v", err)
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

	// Profile-behavior tick: uptime advances, cpu_load is skipped.
	d.tickBehavior(&d.behaviors[0])
	s := d.State()["status"].(map[string]any)["device"].(map[string]any)
	if !numericEquals(s["uptime"], 5) {
		t.Errorf("uptime = %v, want 5 (profile behavior should fire normally for non-overlapping paths)", s["uptime"])
	}
	if !numericEquals(s["cpu_load"], 20) {
		t.Errorf("cpu_load = %v, want 20 (profile behavior should skip overridden path)", s["cpu_load"])
	}

	// Override tick: cpu_load advances to the override's value.
	d.overrideRuntimeTickAll()
	s = d.State()["status"].(map[string]any)["device"].(map[string]any)
	if !numericEquals(s["cpu_load"], 90) {
		t.Errorf("after override tick: cpu_load = %v, want 90", s["cpu_load"])
	}

	// Once the override is removed, the next profile tick must write
	// cpu_load again, the skip is scoped to the active override
	// window, not permanent.
	if err := d.SetBehaviorOverrides(nil); err != nil {
		t.Fatalf("clear SetBehaviorOverrides: %v", err)
	}
	d.tickBehavior(&d.behaviors[0])
	s = d.State()["status"].(map[string]any)["device"].(map[string]any)
	if !numericEquals(s["cpu_load"], 91) {
		t.Errorf("after override cleared: cpu_load = %v, want 91", s["cpu_load"])
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

// TestFilterOverriddenMutations_LiteralPathSkip covers the helper that
// the profile-behavior tick uses to drop overridden writes. Literal
// paths are skipped; mutations with no path template are passed through
// unchanged (defensive, production code never produces those).
func TestFilterOverriddenMutations_LiteralPathSkip(t *testing.T) {
	d := newTestDevice(t, "d-1", nil)

	// Compile two mutations: one on a literal path that should be
	// skipped, one on a literal path that should not.
	muts, err := compileBehaviorMutationList([]profile.Mutation{
		{Path: "status.dsp.cpu_load", Value: "90"},
		{Path: "status.device.uptime", Value: "5"},
	}, behaviorFuncs(newDeviceRand(d.DeviceID)))
	if err != nil {
		t.Fatalf("compileBehaviorMutationList: %v", err)
	}

	overridePaths := map[string]struct{}{
		"status.dsp.cpu_load": {},
	}
	got := filterOverriddenMutations(muts, overridePaths)
	if len(got) != 1 {
		t.Fatalf("filterOverriddenMutations returned %d mutations, want 1", len(got))
	}
	// Verify the survivor is the uptime mutation by rendering its path.
	raw, _ := rawPathSource(got[0].path)
	if raw != "status.device.uptime" {
		t.Errorf("kept mutation path = %q, want status.device.uptime", raw)
	}

	// Empty overrides set returns the input unchanged.
	if out := filterOverriddenMutations(muts, nil); len(out) != len(muts) {
		t.Errorf("empty override set: returned %d, want %d", len(out), len(muts))
	}
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
