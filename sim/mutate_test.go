package sim

import (
	"testing"
)

// compileAll compiles the (path, value) pairs an override's expression takes.
func compileAll(t *testing.T, pairs ...[2]any) []mutation {
	t.Helper()
	var muts []mutation
	for _, p := range pairs {
		m, err := compileOneFuncs(p[0].(string), p[1], false, templateFuncs)
		if err != nil {
			t.Fatal(err)
		}
		muts = append(muts, m)
	}
	return muts
}

func TestApplyMutations_EmptyReturnsNil(t *testing.T) {
	diffs, errs := applyMutations(map[string]any{}, nil, templateData{})
	if diffs != nil {
		t.Errorf("expected nil diffs, got %v", diffs)
	}
	if errs != nil {
		t.Errorf("expected nil errs, got %v", errs)
	}
}

// A failed mutation is absent from the diffs and the rest still apply: the
// per-mutation semantics an override batch keeps.
func TestApplyMutations_FailedMutationNotInDiffs(t *testing.T) {
	muts := compileAll(t,
		[2]any{"channels.99.mute", "true"}, // out of range
		[2]any{"status.device.ok", true},
	)
	root := map[string]any{
		"channels": []any{map[string]any{"mute": false}},
		"status":   map[string]any{"device": map[string]any{}},
	}

	diffs, errs := applyMutations(root, muts, templateData{State: root})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if _, ok := diffs["channels.99.mute"]; ok {
		t.Errorf("failed mutation must not appear in diffs")
	}
	if got := diffs["status.device.ok"]; got != true {
		t.Errorf("successful mutation missing from diffs, got %v", diffs)
	}
}

// An expression reaches the device's declared defaults, so a pin can put a
// path back to what the family declared without restating the literal.
func TestApplyMutations_DefaultsScopeRendersDeclaredDefault(t *testing.T) {
	muts := compileAll(t,
		[2]any{"status.device.uptime", "{{ .Defaults.status.device.uptime }}"},
		[2]any{"status.audio.volume", "{{ .Defaults.status.audio.volume }}"},
		[2]any{"status.audio.mic_mute", "{{ .Defaults.status.audio.mic_mute }}"},
	)
	state := map[string]any{
		"status": map[string]any{
			"device": map[string]any{"uptime": int64(123456)},
			"audio":  map[string]any{"volume": int64(80), "mic_mute": true},
		},
	}
	defaults := map[string]any{
		"status": map[string]any{
			"device": map[string]any{"uptime": int64(0)},
			"audio":  map[string]any{"volume": int64(50), "mic_mute": false},
		},
	}
	diffs, errs := applyMutations(state, muts, templateData{State: state, Defaults: defaults})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got := diffs["status.device.uptime"]; got != int64(0) {
		t.Errorf("diffs[status.device.uptime] = %v (%T), want int64(0)", got, got)
	}
	if got := diffs["status.audio.volume"]; got != int64(50) {
		t.Errorf("diffs[status.audio.volume] = %v (%T), want int64(50)", got, got)
	}
	if got := diffs["status.audio.mic_mute"]; got != false {
		t.Errorf("diffs[status.audio.mic_mute] = %v (%T), want false", got, got)
	}
	if got := state["status"].(map[string]any)["audio"].(map[string]any)["volume"]; got != int64(50) {
		t.Errorf("state volume = %v, want int64(50)", got)
	}
}

func TestResolveMutationValue_JSONDecoding(t *testing.T) {
	cases := []struct {
		name     string
		template string
		state    map[string]any
		want     any
	}{
		{"bool true", "{{ .State.v }}", map[string]any{"v": true}, true},
		{"bool false", "{{ .State.v }}", map[string]any{"v": false}, false},
		// Integer-shaped renders preserve int64: JSON-decoding alone would
		// coerce every number to float64 and silently flip the field type.
		{"integer", "{{ .State.v }}", map[string]any{"v": int64(42)}, int64(42)},
		{"float", "{{ .State.v }}", map[string]any{"v": float64(4.2)}, float64(4.2)},
		// Text that isn't valid JSON falls back to the rendered string.
		{"plain string fallback", "hdmi1", nil, "hdmi1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			muts := compileAll(t, [2]any{"k", c.template})
			got, err := resolveMutationValue(muts[0], templateData{State: c.state})
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got %v (%T), want %v (%T)", got, got, c.want, c.want)
			}
		})
	}
}
