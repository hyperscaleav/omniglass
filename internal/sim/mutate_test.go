package sim

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

func TestCompileAndApplyMutations(t *testing.T) {
	muts, err := compileMutations(map[string]any{
		"status.channels.{{ .Request.Body.index }}.mute": "{{ .Request.Body.mute }}",
		"status.device.power":                            "standby",
	})
	if err != nil {
		t.Fatal(err)
	}

	root := map[string]any{
		"status": map[string]any{
			"channels": []any{
				map[string]any{"mute": false},
				map[string]any{"mute": false},
				map[string]any{"mute": false},
			},
			"device": map[string]any{"power": "on"},
		},
	}
	data := templateData{
		State: root,
		Request: requestContext{
			Body: map[string]any{"index": float64(2), "mute": true},
		},
	}

	diffs, errs := applyMutations(root, muts, data)
	if len(errs) > 0 {
		t.Fatalf("applyMutations errors: %v", errs)
	}

	// State assertions.
	ch := root["status"].(map[string]any)["channels"].([]any)
	if ch[2].(map[string]any)["mute"] != true {
		t.Errorf("channels[2].mute not set, state = %+v", root)
	}
	if ch[0].(map[string]any)["mute"] != false {
		t.Errorf("channels[0].mute should be untouched")
	}
	if root["status"].(map[string]any)["device"].(map[string]any)["power"] != "standby" {
		t.Errorf("device.power not set")
	}

	// Diff assertions: the return value must match what was written.
	if got := diffs["status.channels.2.mute"]; got != true {
		t.Errorf("diffs[status.channels.2.mute] = %v, want true", got)
	}
	if got := diffs["status.device.power"]; got != "standby" {
		t.Errorf("diffs[status.device.power] = %v, want %q", got, "standby")
	}
	if len(diffs) != 2 {
		t.Errorf("got %d diffs, want 2: %+v", len(diffs), diffs)
	}
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

func TestApplyMutations_FailedMutationNotInDiffs(t *testing.T) {
	muts, err := compileMutations(map[string]any{
		"channels.99.mute": "{{ .Request.Body.mute }}", // out of range
		"status.device.ok": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{
		"channels": []any{map[string]any{"mute": false}},
		"status":   map[string]any{"device": map[string]any{}},
	}
	data := templateData{
		State:   root,
		Request: requestContext{Body: map[string]any{"mute": true}},
	}

	diffs, errs := applyMutations(root, muts, data)
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

// TestApplyMutations_EnumAccepted confirms a substituted value that
// appears in the declared enum applies normally.
func TestApplyMutations_EnumAccepted(t *testing.T) {
	muts, err := compileMutationList([]profile.Mutation{
		{
			Path:  "status.device.power",
			Value: "{{ .Request.Body.power }}",
			Enum:  []string{"on", "off", "standby"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"status": map[string]any{"device": map[string]any{"power": "off"}}}
	data := templateData{
		State:   root,
		Request: requestContext{Body: map[string]any{"power": "standby"}},
	}
	diffs, errs := applyMutations(root, muts, data)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got := diffs["status.device.power"]; got != "standby" {
		t.Errorf("diffs[power] = %v, want standby", got)
	}
	if got := root["status"].(map[string]any)["device"].(map[string]any)["power"]; got != "standby" {
		t.Errorf("state power = %v, want standby", got)
	}
}

// TestApplyMutations_EnumRejected confirms a substituted value missing
// from the enum surfaces as a *ValidationError, leaves diffs empty, and
// leaves state untouched.
func TestApplyMutations_EnumRejected(t *testing.T) {
	muts, err := compileMutationList([]profile.Mutation{
		{
			Path:  "status.device.power",
			Value: "{{ .Request.Body.power }}",
			Enum:  []string{"on", "off", "standby"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"status": map[string]any{"device": map[string]any{"power": "off"}}}
	data := templateData{
		State:   root,
		Request: requestContext{Body: map[string]any{"power": "yolo"}},
	}
	diffs, errs := applyMutations(root, muts, data)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	var vErr *ValidationError
	if !errors.As(errs[0], &vErr) {
		t.Fatalf("expected *ValidationError, got %T: %v", errs[0], errs[0])
	}
	if vErr.Value != "yolo" {
		t.Errorf("ValidationError.Value = %q, want yolo", vErr.Value)
	}
	if vErr.Path != "status.device.power" {
		t.Errorf("ValidationError.Path = %q, want status.device.power", vErr.Path)
	}
	if diffs != nil {
		t.Errorf("diffs should be nil on validation failure, got %v", diffs)
	}
	if got := root["status"].(map[string]any)["device"].(map[string]any)["power"]; got != "off" {
		t.Errorf("state mutated despite validation failure: power = %v", got)
	}
}

// TestApplyMutations_NoValidatorIsNoop confirms the existing legacy
// path (no enum / no pattern) keeps applying any substituted text
// unchanged, including values that would fail an enum if one were set.
func TestApplyMutations_NoValidatorIsNoop(t *testing.T) {
	muts, err := compileMutationList([]profile.Mutation{
		{Path: "status.device.power", Value: "{{ .Request.Body.power }}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"status": map[string]any{"device": map[string]any{"power": "off"}}}
	data := templateData{
		State:   root,
		Request: requestContext{Body: map[string]any{"power": "yolo"}},
	}
	diffs, errs := applyMutations(root, muts, data)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got := diffs["status.device.power"]; got != "yolo" {
		t.Errorf("diffs[power] = %v, want yolo (no validator means anything goes)", got)
	}
}

// TestApplyMutations_PatternAccepted confirms a substituted value
// matching the declared regex applies normally.
func TestApplyMutations_PatternAccepted(t *testing.T) {
	muts, err := compileMutationList([]profile.Mutation{
		{
			Path:    "status.display.brightness",
			Value:   "{{ .Request.Body.brightness }}",
			Pattern: `^\d{1,3}$`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"status": map[string]any{"display": map[string]any{"brightness": float64(0)}}}
	data := templateData{
		State:   root,
		Request: requestContext{Body: map[string]any{"brightness": float64(75)}},
	}
	diffs, errs := applyMutations(root, muts, data)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// Integer-shaped renders preserve int64 type so a mutation that
	// writes a whole number doesn't silently flip the field to float64.
	if got := diffs["status.display.brightness"]; got != int64(75) {
		t.Errorf("diffs[brightness] = %v (%T), want 75 (int64)", got, got)
	}
}

// TestApplyMutations_PatternRejected confirms a substituted value not
// matching the regex surfaces as a *ValidationError.
func TestApplyMutations_PatternRejected(t *testing.T) {
	muts, err := compileMutationList([]profile.Mutation{
		{
			Path:    "status.display.brightness",
			Value:   "{{ .Request.Body.brightness }}",
			Pattern: `^\d{1,3}$`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"status": map[string]any{"display": map[string]any{"brightness": float64(0)}}}
	data := templateData{
		State:   root,
		Request: requestContext{Body: map[string]any{"brightness": "loud"}},
	}
	diffs, errs := applyMutations(root, muts, data)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	var vErr *ValidationError
	if !errors.As(errs[0], &vErr) {
		t.Fatalf("expected *ValidationError, got %T: %v", errs[0], errs[0])
	}
	if vErr.Pattern != `^\d{1,3}$` {
		t.Errorf("ValidationError.Pattern = %q, want the source regex", vErr.Pattern)
	}
	if diffs != nil {
		t.Errorf("diffs should be nil on validation failure, got %v", diffs)
	}
}

// TestCompileMutationList_BadPatternFails confirms a malformed regex
// fails loudly at profile-load time, not silently per-request.
func TestCompileMutationList_BadPatternFails(t *testing.T) {
	_, err := compileMutationList([]profile.Mutation{
		{
			Path:    "status.x",
			Value:   "{{ .Request.Body.x }}",
			Pattern: `[unterminated`,
		},
	})
	if err == nil {
		t.Fatal("expected compile error for invalid regex, got nil")
	}
}

// TestCompileMerged_BothShapes confirms a route declaring both the
// legacy map and the new list ends up with both sets of mutations
// compiled into one slice.
func TestCompileMerged_BothShapes(t *testing.T) {
	muts, err := compileMerged(
		map[string]any{"status.legacy.flag": true},
		[]profile.Mutation{{Path: "status.new.flag", Value: false}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(muts) != 2 {
		t.Fatalf("expected 2 compiled mutations, got %d", len(muts))
	}
}

// TestApplyMutations_DefaultsScopeRendersDeclaredDefault confirms a
// mutation template can read `.Defaults.<path>` and applies the value
// declared in the profile's defaults block. This is the wiring that
// lets reboot and factory-reset operations express "reset this path
// to whatever the profile says is its default" without restating the
// literal value in every operation.
func TestApplyMutations_DefaultsScopeRendersDeclaredDefault(t *testing.T) {
	muts, err := compileMutationList([]profile.Mutation{
		{Path: "status.device.uptime", Value: "{{ .Defaults.status.device.uptime }}"},
		{Path: "status.audio.volume", Value: "{{ .Defaults.status.audio.volume }}"},
		{Path: "status.audio.mic_mute", Value: "{{ .Defaults.status.audio.mic_mute }}"},
	})
	if err != nil {
		t.Fatal(err)
	}
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
	// State must reflect the defaults, not the previous live values.
	if got := state["status"].(map[string]any)["device"].(map[string]any)["uptime"]; got != int64(0) {
		t.Errorf("state uptime = %v, want int64(0)", got)
	}
	if got := state["status"].(map[string]any)["audio"].(map[string]any)["volume"]; got != int64(50) {
		t.Errorf("state volume = %v, want int64(50)", got)
	}
}

func TestResolveValue_JSONDecoding(t *testing.T) {
	cases := []struct {
		name     string
		template string
		body     map[string]any
		want     any
	}{
		{"bool true", "{{ .Request.Body.v }}", map[string]any{"v": true}, true},
		{"bool false", "{{ .Request.Body.v }}", map[string]any{"v": false}, false},
		// Integer-shaped renders preserve int64. JSON-decoding alone
		// would coerce every number to float64 and silently flip the
		// field type after a mutation; coerceValue special-cases the
		// no-decimal case to keep an int field an int.
		{"integer", "{{ .Request.Body.v }}", map[string]any{"v": int64(42)}, int64(42)},
		{"float", "{{ .Request.Body.v }}", map[string]any{"v": float64(4.2)}, float64(4.2)},
		// Text that isn't valid JSON should fall back to the rendered string.
		{"plain string fallback", "hdmi1", nil, "hdmi1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			muts, err := compileMutations(map[string]any{"k": c.template})
			if err != nil {
				t.Fatal(err)
			}
			got, err := resolveValue(muts[0], templateData{Request: requestContext{Body: c.body}})
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got %v (%T), want %v (%T)", got, got, c.want, c.want)
			}
		})
	}
}

// TestKestrelRebootRouteAppliesDefaults loads the actual kestrel-vroom
// profile from disk, builds a Device with `defaults` installed by the
// engine wiring, applies the POST /api/admin/reboot route's mutations,
// and asserts the volatile paths come back to their declared defaults.
// This is the cross-package check that the .Defaults plumbing reaches
// the route mutation pipeline end-to-end without booting a live
// listener.
func TestKestrelRebootRouteAppliesDefaults(t *testing.T) {
	root := findLabProfilesRoot(t)
	store, err := profile.LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	dp := store.Devices["kestrel-vroom"]
	if dp == nil {
		t.Fatal("kestrel-vroom profile not loaded")
	}
	var rebootRoute *profile.Route
	for i := range dp.Interfaces {
		for j := range dp.Interfaces[i].Routes {
			if dp.Interfaces[i].Routes[j].Match == "POST /api/admin/reboot" {
				rebootRoute = &dp.Interfaces[i].Routes[j]
				break
			}
		}
		if rebootRoute != nil {
			break
		}
	}
	if rebootRoute == nil {
		t.Fatal("reboot route not found on kestrel-vroom profile")
	}

	// Pre-mutation state: uptime advanced, audio.volume bumped, mic muted.
	state := map[string]any{
		"status": map[string]any{
			"device": map[string]any{"uptime": int64(987654), "power": "on"},
			"call":   map[string]any{"active": true, "participants": int64(3)},
			"audio":  map[string]any{"volume": int64(80), "mic_mute": true, "speaker_level": int64(80)},
			"video":  map[string]any{"camera_on": true, "resolution": "3840x2160"},
		},
	}

	dev := NewDevice("kestrel-1", "kestrel-1.lab", net.IPv4(127, 0, 0, 1),
		"kestrel-vroom", 1, state)
	dev.SetDefaults(dp.Defaults)

	muts, err := compileMerged(rebootRoute.Mutate, rebootRoute.Mutations)
	if err != nil {
		t.Fatalf("compileMerged: %v", err)
	}

	data := templateData{
		State:    state,
		Defaults: dev.defaults,
		Hostname: dev.Hostname,
		DeviceID: dev.DeviceID,
		IP:       dev.LoopbackIP.String(),
	}
	diffs, errs := applyMutations(state, muts, data)
	if len(errs) != 0 {
		t.Fatalf("applyMutations errors: %v", errs)
	}

	// reboot re-anchors uptime by setting boot_epoch to "{{ now }}" so the
	// computed elapsed value drops back to ~0. The diff carries the current
	// Unix epoch (int64), not a literal 0, and uptime itself is no longer
	// mutated directly (it is computed on read).
	nowEpoch := time.Now().Unix()
	switch v := diffs["status.device.boot_epoch"].(type) {
	case int64:
		if v < nowEpoch-5 || v > nowEpoch+5 {
			t.Errorf("status.device.boot_epoch = %d, want ~%d (now)", v, nowEpoch)
		}
	default:
		t.Errorf("status.device.boot_epoch = %v (%T), want int64 epoch ~%d", v, v, nowEpoch)
	}
	if got, ok := diffs["status.device.uptime"]; ok {
		t.Errorf("reboot mutated status.device.uptime = %v; it should be computed on read, not written", got)
	}
	// audio.volume comes from .Defaults.status.audio.volume = 50 (int64
	// after profile YAML parse). After template render + coerce the
	// value lands as int64 because the integer-shaped render preserves
	// the int type.
	if got := diffs["status.audio.volume"]; got != int64(50) {
		t.Errorf("status.audio.volume = %v (%T), want int64(50) (declared default)", got, got)
	}
	// audio.mic_mute defaults to false. JSON-decoded bool round-trip
	// yields bool(false) on coerce.
	if got := diffs["status.audio.mic_mute"]; got != false {
		t.Errorf("status.audio.mic_mute = %v (%T), want false (declared default)", got, got)
	}
}
