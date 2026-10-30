package collection

import (
	"encoding/json"
	"io/fs"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// profileProtocols is the interface protocols the simulator's device profile
// schema allows: the simulator's own declared list, which a profile's
// interfaces draw from.
func profileProtocols(t *testing.T) []string {
	t.Helper()
	raw, err := fs.ReadFile(profiles.FS, "devices/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var found [][]string
	var walk func(v any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if prop, ok := n["protocol"].(map[string]any); ok {
				if enum, ok := prop["enum"].([]any); ok {
					var list []string
					for _, e := range enum {
						list = append(list, e.(string))
					}
					found = append(found, list)
				}
			}
			for _, c := range n {
				walk(c)
			}
		case []any:
			for _, c := range n {
				walk(c)
			}
		}
	}
	walk(doc)
	if len(found) != 1 {
		t.Fatalf("the profile schema declares %d protocol enums, want the one interface protocol enum", len(found))
	}
	return found[0]
}

// A hit's protocol is one the hub accepts (SimProtocols), and a simulator
// that gains a protocol must not have its hits dropped by a hub that was never
// told (#1366: websocket was, for as long as the hub kept its own list). The
// vocabulary is the profile schema's interface protocols less two the schema
// lists that never name a hit: icmp, which the kernel answers so the simulator
// sees nothing, and https, which no face serves (https is http with tls, and
// reports as http). A protocol added to the schema fails here until the
// vocabulary names it, and one removed fails until the vocabulary drops it.
// The schema is an editor aid, though, and nothing validates a profile against
// it: the simulator's own sources are held to the vocabulary in
// internal/sim (TestEveryProtocolTheSimulatorReportsAndServesIsOneTheHubCounts).
func TestSimProtocolsAreTheProfileSchemasLessWhatNeverReportsAHit(t *testing.T) {
	want := slices.DeleteFunc(profileProtocols(t), func(p string) bool { return p == "icmp" || p == "https" })
	slices.Sort(want)
	got := SimProtocols()
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("SimProtocols() = %v, want the profile schema's protocols less icmp and https: %v", got, want)
	}
	for _, p := range want {
		if !IsSimProtocol(p) {
			t.Errorf("IsSimProtocol(%q) = false", p)
		}
	}
	for _, p := range []string{"", "https", "icmp", "HTTP", "made-up"} {
		if IsSimProtocol(p) {
			t.Errorf("IsSimProtocol(%q) = true, want only a protocol a device reports a hit by", p)
		}
	}
}

func TestSimProtocolsIsACopy(t *testing.T) {
	a := SimProtocols()
	a[0] = "mutated"
	if SimProtocols()[0] == "mutated" || IsSimProtocol("mutated") {
		t.Error("SimProtocols shares its backing array")
	}
}
