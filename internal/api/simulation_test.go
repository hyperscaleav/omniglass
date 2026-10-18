package api

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/docslint"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func TestSimulationBodyConnectedWindow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	devs := []storage.SimDevice{{ComponentID: "b", Name: "zeta"}, {ComponentID: "a", Name: "alpha"}}
	fresh := simulationBody(devs, simhub.Status{LastSeen: now.Add(-30 * time.Second)}, true, now)
	if !fresh.Body.Connected {
		t.Error("a pull 30s ago reads disconnected")
	}
	stale := simulationBody(devs, simhub.Status{LastSeen: now.Add(-5 * time.Minute)}, true, now)
	if stale.Body.Connected || stale.Body.LastSeen == nil {
		t.Error("a pull 5m ago reads connected, or lost its last-seen time")
	}
	if got := fresh.Body.Devices; got[0].Component != "alpha" || got[0].Hits != 0 || got[0].LastHitAt != nil {
		t.Errorf("devices = %+v, want sorted by name with no hits", got)
	}
}

// The simulator's list of impaired devices covers its whole node; a caller
// sees only the ids of devices in its own view.
func TestSimulationBodyKeepsImpairedToVisibleDevices(t *testing.T) {
	st := simhub.Status{LastSeen: time.Now(), Impairments: collection.SimImpairmentStatus{Available: true, Impaired: []string{"seen", "hidden"}}}
	out := simulationBody([]storage.SimDevice{{ComponentID: "seen", Name: "a"}}, st, true, time.Now())
	if got := out.Body.Impairments.Impaired; len(got) != 1 || got[0] != "seen" {
		t.Fatalf("impaired = %v, want only the visible device", got)
	}
}

// A profile's prose reaches operators through a device's surface (each
// surface's, route's and command's summary), so it speaks the platform's
// vocabulary: no retired noun survives there.
var bareInterface = regexp.MustCompile(`\binterfaces?\b`)

func TestSimSurfaceSpeaksTheVocabulary(t *testing.T) {
	store, err := simCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range store.Devices {
		var texts []string
		texts = append(texts, p.Doc.Summary)
		for _, s := range simSurface(p, 0) {
			texts = append(texts, s.Summary)
			for _, r := range s.Routes {
				texts = append(texts, r.Summary)
			}
			for _, c := range s.Commands {
				texts = append(texts, c.Summary)
			}
		}
		for _, text := range texts {
			// Stricter than the docs lint, whose patterns miss a bare
			// "interface": a profile has no other meaning for the word.
			if bareInterface.MatchString(strings.ReplaceAll(strings.ToLower(text), "user interface", "")) {
				t.Errorf("%s: %q names an interface; say surface, API or the protocol (ADR-0134)", p.Name, text)
			}
			for _, b := range docslint.Banned {
				if b.Pattern.MatchString(text) {
					t.Errorf("%s: %q: %s (%s)", p.Name, text, b.Replacement, b.Origin)
				}
			}
		}
	}
}
