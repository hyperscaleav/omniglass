package api

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// borealListing is what the simulation routes list of a Boreal definition: its
// surfaces and the behaviors it declares.
type borealListing struct {
	Surface   []simSurfaceBody  `json:"surface"`
	Behaviors []simBehaviorBody `json:"behaviors"`
}

// The console lists a Go family's product as it listed the YAML profile it
// replaced (#2832): every surface (protocol, port, auth, route with its
// summary, example and quick sets, OID with the state path it reads) and every
// behavior (name, interval, the paths it writes). The golden is the listing the
// profiles gave, recorded before they were deleted.
func TestBorealFamiliesAreListedAsTheirProfilesWere(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/boreal_listing.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]borealListing
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	cat, err := simCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for product, want := range golden {
		def, ok := cat.ProfileFor(product)
		if !ok {
			t.Fatalf("%s: no definition", product)
		}
		got := borealListing{Surface: simSurface(def, 10000), Behaviors: simBehaviors(def)}
		gotJSON, _ := json.Marshal(got)
		var back borealListing
		_ = json.Unmarshal(gotJSON, &back)
		if !reflect.DeepEqual(back.Surface, want.Surface) {
			t.Errorf("%s: surface differs:\n got %s\nwant %s", product, mustJSON(back.Surface), mustJSON(want.Surface))
		}
		if !reflect.DeepEqual(back.Behaviors, want.Behaviors) {
			t.Errorf("%s: behaviors differ:\n got %s\nwant %s", product, mustJSON(back.Behaviors), mustJSON(want.Behaviors))
		}
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
