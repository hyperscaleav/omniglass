package storage

import (
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// TestRulingEmits pins the worklist's pure strip: every emit the ruling
// refuses (a node-probe key in any casing, the platform's health, a command's
// lifecycle event, a calc rule's output) leaves a baked spec, the rest stay in
// order, and a spec with nothing to drop, or one that does not parse, comes
// back byte for byte.
func TestRulingEmits(t *testing.T) {
	calcOwned := map[string]bool{"avg-uptime": true}
	spec := []byte(`{"driver":"legacy","function":"status","emits":[
		{"name":"video-input","lane":"property","extract":{"regex":"^I (\\S+)$"}},
		{"name":"Endpoint-Reachable","lane":"property","extract":{"regex":"^R (\\S+)$"}},
		{"name":"health","lane":"property","extract":{"regex":"^H (\\S+)$"}},
		{"name":"command-issued","lane":"property","extract":{"regex":"^C (\\S+)$"}},
		{"name":"avg-uptime","lane":"metric","extract":{"regex":"^A (\\S+)$"}},
		{"name":"uptime","lane":"metric","extract":{"regex":"^U (\\S+)$"}}]}`)
	out, dropped := rulingEmits(spec, calcOwned)
	if want := []string{"Endpoint-Reachable", "health", "command-issued", "avg-uptime"}; !slices.Equal(dropped, want) {
		t.Fatalf("dropped %v, want %v", dropped, want)
	}
	fn, err := driver.ParseBaked(out)
	if err != nil {
		t.Fatalf("the stripped spec does not parse: %v", err)
	}
	var kept []string
	for _, em := range fn.Emits {
		kept = append(kept, em.Name)
	}
	if want := []string{"video-input", "uptime"}; !slices.Equal(kept, want) {
		t.Fatalf("kept %v, want %v", kept, want)
	}
	if fn.Driver != "legacy" || fn.Function != "status" {
		t.Fatalf("the strip lost the function's identity: %+v", fn)
	}

	honest := []byte(`{"driver":"d","function":"f","emits":[{"name":"video-input","lane":"property","extract":{"regex":"x"}}]}`)
	if out, dropped := rulingEmits(honest, calcOwned); len(dropped) != 0 || string(out) != string(honest) {
		t.Fatalf("an honest spec came back %s, dropping %v", out, dropped)
	}
	broken := []byte(`{"driver":"d","function":"f","emits":[],"surprise":1}`)
	if out, dropped := rulingEmits(broken, calcOwned); len(dropped) != 0 || string(out) != string(broken) {
		t.Fatalf("an unparsable spec came back %s, dropping %v", out, dropped)
	}
}
