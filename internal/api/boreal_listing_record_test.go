package api

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// borealListing is what the simulation routes list of a Boreal definition: its
// surfaces (every protocol, port, auth, route with its quick sets, and OID)
// and the behaviors it declares (#2832).
type borealListing struct {
	Surface   []simSurfaceBody  `json:"surface"`
	Behaviors []simBehaviorBody `json:"behaviors"`
}

const borealListingGolden = "testdata/boreal_listing.golden.json"

// TestRecordBorealListing writes the listing of each Boreal YAML profile as
// the console shows it. It runs only when OMNIGLASS_RECORD_GOLDEN is set,
// against a tree that still holds the profiles, and is deleted with them.
func TestRecordBorealListing(t *testing.T) {
	if os.Getenv("OMNIGLASS_RECORD_GOLDEN") == "" {
		t.Skip("set OMNIGLASS_RECORD_GOLDEN to record")
	}
	cat, err := simCatalog()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]borealListing{}
	for _, product := range []string{"boreal-edge-86", "boreal-beam-340", "boreal-cast-m1"} {
		def, ok := cat.ProfileFor(product)
		if !ok {
			t.Fatalf("no definition for %s", product)
		}
		out[product] = borealListing{Surface: simSurface(def, 10000), Behaviors: simBehaviors(def)}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(borealListingGolden, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
