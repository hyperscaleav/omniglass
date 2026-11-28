package api

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// audioListing is what the simulation routes list of a room audio definition:
// its surfaces and the behaviors it declares.
type audioListing struct {
	Surface   []simSurfaceBody  `json:"surface"`
	Behaviors []simBehaviorBody `json:"behaviors"`
}

// The console lists a room audio family's product as it listed the YAML profile
// it replaced (#2835): every surface (protocol, port, auth, route with its
// summary, example and quick sets, OID with the state path it reads) and every
// behavior (name, interval, the paths it writes). The golden is the listing the
// profiles gave, recorded before they were deleted.
func TestAudioFamiliesAreListedAsTheirProfilesWere(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/audio_listing.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]audioListing
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
		got := audioListing{Surface: simSurface(def, 10000), Behaviors: simBehaviors(def)}
		gotJSON, _ := json.Marshal(got)
		var back audioListing
		_ = json.Unmarshal(gotJSON, &back)
		if len(back.Surface) != len(want.Surface) {
			t.Errorf("%s: %d surfaces, want %d", product, len(back.Surface), len(want.Surface))
			continue
		}
		for i, w := range want.Surface {
			if !reflect.DeepEqual(back.Surface[i], w) {
				t.Errorf("%s: the %s surface differs:\n got %s\nwant %s", product, w.Name, mustJSON(back.Surface[i]), mustJSON(w))
			}
		}
		if !reflect.DeepEqual(back.Behaviors, want.Behaviors) {
			t.Errorf("%s: behaviors differ:\n got %s\nwant %s", product, mustJSON(back.Behaviors), mustJSON(want.Behaviors))
		}
	}
}
