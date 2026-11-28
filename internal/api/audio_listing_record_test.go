package api

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// audioListing is what the simulation routes list of a room audio definition:
// its surfaces (every protocol, port, auth, route and command with its quick
// sets, and OID) and the behaviors it declares (#2835).
type audioListing struct {
	Surface   []simSurfaceBody  `json:"surface"`
	Behaviors []simBehaviorBody `json:"behaviors"`
}

const audioListingGolden = "testdata/audio_listing.golden.json"

var audioProducts = []string{"lyra-arc-a2", "lyra-hx-1", "polaris-dsp-16", "polaris-amp-4100"}

// TestRecordAudioListing writes the listing of each room audio YAML profile as
// the console shows it. It runs only when OMNIGLASS_RECORD_GOLDEN is set,
// against a tree that still holds the profiles, and is deleted with them.
func TestRecordAudioListing(t *testing.T) {
	if os.Getenv("OMNIGLASS_RECORD_GOLDEN") == "" {
		t.Skip("set OMNIGLASS_RECORD_GOLDEN to record")
	}
	cat, err := simCatalog()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]audioListing{}
	for _, product := range audioProducts {
		def, ok := cat.ProfileFor(product)
		if !ok {
			t.Fatalf("no definition for %s", product)
		}
		out[product] = audioListing{Surface: simSurface(def, 10000), Behaviors: simBehaviors(def)}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioListingGolden, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
