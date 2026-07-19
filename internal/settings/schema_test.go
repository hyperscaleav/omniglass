package settings

import (
	"reflect"
	"testing"
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
