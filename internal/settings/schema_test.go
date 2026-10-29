package settings

import (
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/logline"
)

func TestCoerceDefaultPreservesKind(t *testing.T) {
	cases := []struct {
		in   string
		kind reflect.Kind
		want any
	}{
		{"hi", reflect.String, "hi"},
		{"true", reflect.Bool, true},
		{"42", reflect.Int, int(42)},
		{"42", reflect.Int8, int8(42)},
		{"42", reflect.Int16, int16(42)},
		{"42", reflect.Int32, int32(42)},
		{"42", reflect.Int64, int64(42)},
		{"3.14", reflect.Float32, float32(3.14)},
		{"3.14", reflect.Float64, float64(3.14)},
	}
	for _, c := range cases {
		got, err := coerceDefault(c.in, c.kind)
		if err != nil {
			t.Fatalf("coerceDefault(%q, %v) error: %v", c.in, c.kind, err)
		}
		if got != c.want {
			t.Fatalf("coerceDefault(%q, %v) = %#v (%T), want %#v (%T)", c.in, c.kind, got, got, c.want, c.want)
		}
	}
}

func TestReflectedDefaults(t *testing.T) {
	d := Defaults()
	if d["ui"]["theme"] != "omniglass-dark" {
		t.Fatalf("ui.theme default = %v, want omniglass-dark", d["ui"]["theme"])
	}
	if d["ui"]["default_landing"] != "/" {
		t.Fatalf("ui.default_landing default = %v, want /", d["ui"]["default_landing"])
	}
	if d["keybindings"]["open_detail"] != "d" {
		t.Fatalf("keybindings.open_detail default = %v, want d", d["keybindings"]["open_detail"])
	}
}

func TestReflectedNamespaces(t *testing.T) {
	byName := map[string]Namespace{}
	for _, n := range Namespaces() {
		byName[n.Name] = n
	}
	if byName["ui"].Domain != "profile" || !byName["ui"].ClientVisible {
		t.Fatalf("ui namespace = %+v, want profile + client-visible", byName["ui"])
	}
	if _, ok := byName["keybindings"]; !ok {
		t.Fatalf("keybindings namespace missing")
	}
}

// Every log retention band has a window (#417): a band added to logline with
// no setting behind it would never be pruned.
func TestEveryLogBandHasARetentionWindow(t *testing.T) {
	w := RetentionSettings{DebugLogDays: 1, InfoLogDays: 2, WarningLogDays: 3, ErrorLogDays: 4}
	for _, band := range logline.RetentionBands() {
		if w.LogDays(band) == 0 {
			t.Errorf("band %q has no retention window", band)
		}
	}
}

// A node's own logs follow the shared band window until an admin sets a node
// override (#1350): the override declares no default, so the effective
// document leaves it out and every band resolves to its shared window. A set
// override moves only its own band.
func TestANodeLogBandFollowsTheSharedWindowUntilOverridden(t *testing.T) {
	for _, key := range []string{"node_log_debug_days", "node_log_info_days", "node_log_warning_days", "node_log_error_days"} {
		if v, ok := Defaults()["retention"][key]; ok {
			t.Errorf("retention.%s defaults to %v, want no default (unset follows the shared band)", key, v)
		}
	}
	typed, err := Typed(Defaults())
	if err != nil {
		t.Fatal(err)
	}
	w := typed.Retention
	for _, band := range logline.RetentionBands() {
		if got, want := w.NodeLogDays(band), w.LogDays(band); got != want || got == 0 {
			t.Errorf("node_log %s window %d, want the shared %d", band, got, want)
		}
	}
	w.NodeDebugLogDays = 1
	if got := w.NodeLogDays("debug"); got != 1 {
		t.Errorf("node_log debug window %d with the override at 1, want 1", got)
	}
	if got := w.LogDays("debug"); got != 3 {
		t.Errorf("shared debug window %d after a node override, want it untouched at 3", got)
	}
	if got := w.NodeLogDays("error"); got != w.ErrorLogDays {
		t.Errorf("node_log error window %d with only debug overridden, want the shared %d", got, w.ErrorLogDays)
	}
}

// Every node log band has an override behind it (#1350): a band added to
// logline with no node setting would silently ignore an admin's override.
func TestEveryLogBandHasANodeOverride(t *testing.T) {
	w := RetentionSettings{NodeDebugLogDays: 5, NodeInfoLogDays: 6, NodeWarningLogDays: 7, NodeErrorLogDays: 8}
	seen := map[int]bool{}
	for _, band := range logline.RetentionBands() {
		d := w.NodeLogDays(band)
		if d < 5 || seen[d] {
			t.Errorf("band %q resolves to %d, want its own node override", band, d)
		}
		seen[d] = true
	}
}
