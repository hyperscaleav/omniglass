package wire

import "testing"

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
