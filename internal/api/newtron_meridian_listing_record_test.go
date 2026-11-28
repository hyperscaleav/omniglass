package api

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// roomListing is what the simulation routes list of a room control and switching definition: its
// surfaces (every protocol, port, auth, route with its quick sets, and OID)
// and the behaviors it declares (#2833).
type roomListing struct {
	Surface   []simSurfaceBody  `json:"surface"`
	Behaviors []simBehaviorBody `json:"behaviors"`
}

const roomListingGolden = "testdata/room_listing.golden.json"

// TestRecordRoomListing writes the listing of each room control and switching YAML profile as
// the console shows it. It runs only when OMNIGLASS_RECORD_GOLDEN is set,
// against a tree that still holds the profiles, and is deleted with them.
func TestRecordRoomListing(t *testing.T) {
	if os.Getenv("OMNIGLASS_RECORD_GOLDEN") == "" {
		t.Skip("set OMNIGLASS_RECORD_GOLDEN to record")
	}
	cat, err := simCatalog()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]roomListing{}
	for _, product := range []string{"newtron-core-3", "newtron-panel-7", "newtron-slate-5", "meridian-mx-84", "meridian-xl-1"} {
		def, ok := cat.ProfileFor(product)
		if !ok {
			t.Fatalf("no definition for %s", product)
		}
		out[product] = roomListing{Surface: simSurface(def, 10000), Behaviors: simBehaviors(def)}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roomListingGolden, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
