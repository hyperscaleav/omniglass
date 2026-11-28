package api

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// kestrelListing is what the simulation routes list of a Kestrel definition: its
// surfaces (every protocol, port, auth, route with its quick sets) and the
// behaviors it declares (#2834).
type kestrelListing struct {
	Surface   []simSurfaceBody  `json:"surface"`
	Behaviors []simBehaviorBody `json:"behaviors"`
}

const kestrelListingGolden = "testdata/kestrel_listing.golden.json"

// TestRecordKestrelListing writes the listing of each Kestrel YAML profile as
// the console shows it. It runs only when OMNIGLASS_RECORD_GOLDEN is set,
// against a tree that still holds the profiles, and is deleted with them.
func TestRecordKestrelListing(t *testing.T) {
	if os.Getenv("OMNIGLASS_RECORD_GOLDEN") == "" {
		t.Skip("set OMNIGLASS_RECORD_GOLDEN to record")
	}
	cat, err := simCatalog()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]kestrelListing{}
	for _, product := range []string{"kestrel-vroom", "kestrel-relay-c1", "kestrel-gaze-p2"} {
		def, ok := cat.ProfileFor(product)
		if !ok {
			t.Fatalf("no definition for %s", product)
		}
		out[product] = kestrelListing{Surface: simSurface(def, 10000), Behaviors: simBehaviors(def)}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kestrelListingGolden, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
