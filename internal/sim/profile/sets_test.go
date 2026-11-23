package profile

import (
	"reflect"
	"testing"
)

func TestRouteSets(t *testing.T) {
	r := &Route{Mutations: []Mutation{
		{Path: "status.device.power", Value: "{{ .Request.Body.power }}", Enum: []string{"on", "off", "standby"}},
		{Path: "status.content.sharing", Value: true},
		{Path: "status.display.volume", Value: "{{ .Request.Body.volume }}"},
		{Path: "status.switcher.signal", Value: `{{ .State | get "x" }}`, Enum: []string{"present", "absent"}},
		{Path: "status.mic.mute", Value: "{{- .Request.Body.mute -}}", Enum: []string{"true", "false"}},
	}}
	want := []Set{
		{Path: "status.device.power", Field: "power", Values: []string{"on", "off", "standby"}},
		{Path: "status.mic.mute", Field: "mute", Values: []string{"true", "false"}},
	}
	if got := r.Sets(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Sets() = %+v, want %+v", got, want)
	}
}

func TestDefaultAt(t *testing.T) {
	d := map[string]any{"status": map[string]any{"device": map[string]any{"power": "on"}, "audio": map[string]any{"mute": false}}}
	for path, want := range map[string]string{"status.device.power": "on", "status.audio.mute": "false"} {
		if got, ok := DefaultAt(d, path); !ok || got != want {
			t.Errorf("DefaultAt(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
	if _, ok := DefaultAt(d, "status.device.nope"); ok {
		t.Error("a missing path read as present")
	}
	if _, ok := DefaultAt(d, "status.device.power.x"); ok {
		t.Error("a path through a scalar read as present")
	}
}
