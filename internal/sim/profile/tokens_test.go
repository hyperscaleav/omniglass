package profile

import "testing"

func TestExpandIdentityTokens(t *testing.T) {
	peers := map[string]string{"newtron-panel-7": "BOI-17C-300-TP1"}
	in := map[string]any{
		"status": map[string]any{
			"app":         map[string]any{"ucc_signin": "room-{site}-{building}-{room}@shinra.lan"},
			"peripherals": map[string]any{"touch_pair": "{room-device:newtron-panel-7}"},
			"plain":       "no tokens here",
			"count":       42,
		},
	}

	out := ExpandIdentityTokens(in, "BOI", "17C", "300", peers)

	if got, want := digStr(t, out, "status", "app", "ucc_signin"), "room-BOI-17C-300@shinra.lan"; got != want {
		t.Errorf("ucc_signin = %q, want %q", got, want)
	}
	if got, want := digStr(t, out, "status", "peripherals", "touch_pair"), "BOI-17C-300-TP1"; got != want {
		t.Errorf("touch_pair = %q, want %q", got, want)
	}
	if got := digStr(t, out, "status", "plain"); got != "no tokens here" {
		t.Errorf("plain mangled: %q", got)
	}
	if got := out["status"].(map[string]any)["count"]; got != 42 {
		t.Errorf("non-string not preserved: %v", got)
	}

	// A {room-device:...} with no matching peer resolves to empty.
	out2 := ExpandIdentityTokens(in, "BOI", "17C", "301", map[string]string{})
	if got := digStr(t, out2, "status", "peripherals", "touch_pair"); got != "" {
		t.Errorf("missing peer touch_pair = %q, want empty", got)
	}

	// Input must not be mutated (profile defaults are shared).
	if got := digStr(t, in, "status", "app", "ucc_signin"); got != "room-{site}-{building}-{room}@shinra.lan" {
		t.Errorf("input was mutated: %q", got)
	}

	if ExpandIdentityTokens(nil, "BOI", "17C", "300", peers) != nil {
		t.Error("nil defaults should return nil")
	}
}

func digStr(t *testing.T, m map[string]any, path ...string) string {
	t.Helper()
	cur := any(m)
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	s, _ := cur.(string)
	return s
}
