package api

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/docslint"
	"github.com/hyperscaleav/omniglass/internal/sim/impair"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func TestSimulationBodyConnectedWindow(t *testing.T) {
	t.Parallel()
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

// The simulator's list of impaired devices covers its whole outpost; a caller
// sees only the ids of devices in its own view.
func TestSimulationBodyKeepsImpairedToVisibleDevices(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
				if b.Matches(text) {
					t.Errorf("%s: %q: %s (%s)", p.Name, text, b.Replacement, b.Origin)
				}
			}
		}
	}
}

// A simulated component's status, most urgent first: offline when its
// simulator stopped reporting, starting when added since the simulator's
// last pull, failed when handed over but not started, running otherwise.
func TestSimulationBodyStates(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0)
	devs := []storage.SimDevice{{ComponentID: "on", Name: "a"}, {ComponentID: "new", Name: "b"}, {ComponentID: "broken", Name: "c"}, {ComponentID: "fresh", Name: "d"}}
	st := simhub.Status{LastSeen: now.Add(-5 * time.Second),
		Listed:      map[string]bool{"on": true, "broken": true, "fresh": true},
		ListedSince: map[string]time.Time{"on": now.Add(-time.Minute), "broken": now.Add(-time.Minute), "fresh": now.Add(-time.Second)},
		Serving:     map[string]bool{"on": true}}
	got := map[string]string{}
	for _, d := range simulationBody(devs, st, true, now).Body.Devices {
		got[d.ComponentID] = d.Status
	}
	// Just handed over and not yet reported: still starting, not failed.
	if got["on"] != "running" || got["new"] != "starting" || got["broken"] != "failed" || got["fresh"] != "starting" {
		t.Fatalf("states = %v", got)
	}
	stale := simulationBody(devs, st, true, now.Add(10*time.Minute)).Body.Devices
	never := simulationBody(devs, simhub.Status{}, false, now).Body.Devices
	for _, d := range append(stale, never...) {
		if d.Status != "offline" {
			t.Fatalf("%s with no live simulator reads %q, want offline", d.ComponentID, d.Status)
		}
	}
}

// The simulator's own account of what a device holds off its defaults reaches
// the device's row with when it began, and an impairment carries when it was
// set, so the console's fault chip shows both from the server (#2462).
func TestSimulationBodyCarriesFaultsAndImpairmentAge(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0)
	began := now.Add(-90 * time.Second)
	set := now.Add(-time.Hour)
	devs := []storage.SimDevice{
		{ComponentID: "dark", Name: "a", Impairment: &impair.Impairment{Unplugged: true}, ImpairedSince: &set},
		{ComponentID: "off", Name: "b"},
		{ComponentID: "fine", Name: "c"},
	}
	st := simhub.Status{LastSeen: now, Listed: map[string]bool{"dark": true, "off": true, "fine": true},
		Faults: map[string][]simhub.DeviceFault{"off": {{SimFault: collection.SimFault{Path: "status.device.power", Value: "off", Default: "on"}, Since: began}}}}
	got := map[string]simDeviceBody{}
	for _, d := range simulationBody(devs, st, true, now).Body.Devices {
		got[d.ComponentID] = d
	}
	if f := got["off"].Faults; len(f) != 1 || f[0].Path != "status.device.power" || f[0].Value != "off" || f[0].Default != "on" || !f[0].Since.Equal(began) {
		t.Fatalf("off faults = %+v", f)
	}
	if len(got["fine"].Faults) != 0 || got["fine"].ImpairmentSince != nil {
		t.Fatalf("a healthy device carries %+v", got["fine"])
	}
	if s := got["dark"].ImpairmentSince; s == nil || !s.Equal(set) {
		t.Fatalf("dark impairment since = %v, want %v", s, set)
	}
}

// A route's quick sets are what its profile declares: the state path, the
// body field and the values it accepts, with the profile's default for that
// path, so the console offers each value without parsing a template (#2462).
func TestSimSurfaceCarriesTheRoutesQuickSets(t *testing.T) {
	t.Parallel()
	p := &profile.DeviceProfile{
		Defaults: map[string]any{"status": map[string]any{"device": map[string]any{"power": "on"}}},
		Interfaces: []profile.Interface{{Name: "web", Protocol: "http", Port: 80, Routes: []profile.Route{{
			Match:     "POST /api/display/power",
			Mutations: []profile.Mutation{{Path: "status.device.power", Value: "{{ .Request.Body.power }}", Enum: []string{"on", "off", "standby"}}},
		}}}},
	}
	var power *simRouteBody
	for _, s := range simSurface(p, 0) {
		for i, r := range s.Routes {
			if r.Method == "POST" && r.Path == "/api/display/power" {
				power = &s.Routes[i]
			}
		}
	}
	if power == nil || len(power.Sets) != 1 {
		t.Fatalf("power route sets = %+v", power)
	}
	got := power.Sets[0]
	if got.Path != "status.device.power" || got.Field != "power" || !reflect.DeepEqual(got.Values, []string{"on", "off", "standby"}) || got.Default != "on" {
		t.Fatalf("power set = %+v", got)
	}
}
