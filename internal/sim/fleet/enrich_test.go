package fleet

import (
	"net"
	"testing"
)

func TestEnrich(t *testing.T) {
	state := map[string]any{
		"configuration": map[string]any{
			"device":  map[string]any{"serial": "", "model": "Edge 86"},
			"network": map[string]any{"hostname": ""},
		},
		"status": map[string]any{
			"network": map[string]any{
				"ipv4": map[string]any{"address": "", "mac": ""},
			},
		},
	}
	Enrich(state, "BOI-17C-415B-DS1", "boreal-edge-a1b2c3", net.ParseIP("10.40.1.7"))

	cfg := state["configuration"].(map[string]any)
	if got := cfg["network"].(map[string]any)["hostname"]; got != "BOI-17C-415B-DS1" {
		t.Errorf("hostname = %q, want the generated hostname", got)
	}
	if got := cfg["device"].(map[string]any)["serial"].(string); got == "" {
		t.Error("serial still blank after enrich")
	}
	if got := cfg["device"].(map[string]any)["model"]; got != "Edge 86" {
		t.Errorf("model = %q, enrich must not stomp non-blank fields", got)
	}
	ipv4 := state["status"].(map[string]any)["network"].(map[string]any)["ipv4"].(map[string]any)
	if ipv4["address"] != "10.40.1.7" {
		t.Errorf("ipv4.address = %q, want 10.40.1.7", ipv4["address"])
	}
	if mac := ipv4["mac"].(string); mac == "" || mac[:3] != "02:" {
		t.Errorf("mac = %q, want locally-administered 02:* MAC", mac)
	}

	// Determinism: same host id -> same serial and MAC.
	if generateSerial("boreal-edge-a1b2c3") != generateSerial("boreal-edge-a1b2c3") {
		t.Error("serial not deterministic")
	}
	if generateMAC("boreal-edge-a1b2c3") != generateMAC("boreal-edge-a1b2c3") {
		t.Error("MAC not deterministic")
	}
	if generateSerial("a") == generateSerial("b") {
		t.Error("serial should vary by host id")
	}

	// Missing path: a profile that omits a field gets nothing injected.
	sparse := map[string]any{"configuration": map[string]any{"device": map[string]any{}}}
	Enrich(sparse, "", "x", nil)
	if _, ok := sparse["configuration"].(map[string]any)["device"].(map[string]any)["serial"]; ok {
		t.Error("setBlankLeaf created a key that the profile did not declare")
	}
}
