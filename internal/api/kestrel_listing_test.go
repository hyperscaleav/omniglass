package api

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

// inSeconds is a behavior's interval as a family's tick is listed: whole
// seconds, whatever unit the profile wrote it in. The status line's 15m is the
// one place the profiles' spelling differed (ADR-2834); the duration is the same.
func inSeconds(t *testing.T, bs []simBehaviorBody) []simBehaviorBody {
	t.Helper()
	out := append([]simBehaviorBody(nil), bs...)
	for i := range out {
		d, err := time.ParseDuration(out[i].Interval)
		if err != nil {
			t.Fatalf("%s: interval %q: %v", out[i].Name, out[i].Interval, err)
		}
		out[i].Interval = tickInterval(d)
	}
	return out
}

// kestrelListing is what the simulation routes list of a Kestrel definition: its
// surfaces and the behaviors it declares.
type kestrelListing struct {
	Surface   []simSurfaceBody  `json:"surface"`
	Behaviors []simBehaviorBody `json:"behaviors"`
}

// The console lists a Go family's product as it listed the YAML profile it
// replaced (#2834): every surface (protocol, port, auth, route with its summary,
// example, private mark and quick sets) and every behavior (name, interval, the
// paths it writes). The golden is the listing the profiles gave, recorded before
// they were deleted.
func TestKestrelFamiliesAreListedAsTheirProfilesWere(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/kestrel_listing.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]kestrelListing
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
		got := kestrelListing{Surface: simSurface(def, 10000), Behaviors: simBehaviors(def)}
		gotJSON, _ := json.Marshal(got)
		var back kestrelListing
		_ = json.Unmarshal(gotJSON, &back)
		if !reflect.DeepEqual(back.Surface, want.Surface) {
			t.Errorf("%s: surface differs:\n got %s\nwant %s", product, mustJSON(back.Surface), mustJSON(want.Surface))
		}
		if wantBehaviors := inSeconds(t, want.Behaviors); !reflect.DeepEqual(back.Behaviors, wantBehaviors) {
			t.Errorf("%s: behaviors differ:\n got %s\nwant %s", product, mustJSON(back.Behaviors), mustJSON(wantBehaviors))
		}
	}
}
