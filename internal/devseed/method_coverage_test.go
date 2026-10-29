package devseed_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/transport"
)

// methodGap is a collection method the lab does not exercise yet, and the
// issue that adds it. An unbuilt one is not in the binary at all (no driver,
// no registry transport) and is listed so the gap is said out loud.
type methodGap struct {
	issue   string
	unbuilt bool
}

// methodGaps are the collection methods no healthy lab component exercises,
// each with its issue (#1370). A method the lab gains fails here as stale.
var methodGaps = map[string]methodGap{
	"websocket":     {issue: "#1414"},                // a plain-ws face and a driver
	"push-listener": {issue: "#1148", unbuilt: true}, // the shared push listener
	"syslog":        {issue: "#1149", unbuilt: true},
	"snmp-trap":     {issue: "#1150", unbuilt: true},
}

// TestEveryCollectionMethodRunsOnAHealthyLabComponent holds the lab to "every
// built collection method runs healthy" (#1370, #1266), the per-method twin of
// the per-product coverage test. With the simulated fleet seeded, every
// seeded driver and every registry transport is attached by an endpoint that
// is not a planted fault, on a component a simulator profile serves. The
// coverage rule (ruled 2026-10-29): a driver counts only when it reads
// collected values (a poll or a listener that emits); a driverless transport
// (icmp, a bare probe) counts on its connection tier. A gap is listed with its
// issue; a listed method the lab exercises now fails as stale.
func TestEveryCollectionMethodRunsOnAHealthyLabComponent(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	ctx, _, gw := seededFleet(t)
	all := scope.Set{All: true}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := devseed.LookupScenario("dev")
	if err != nil {
		t.Fatal(err)
	}
	faults, err := devseed.PlantedFaults(dev)
	if err != nil {
		t.Fatal(err)
	}
	planted := map[string]bool{}
	for _, f := range faults {
		planted[f.Label] = true
	}
	comps, err := gw.ListComponents(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	simulated := map[string]bool{}
	for _, c := range comps {
		if c.ProductHandle == nil {
			continue
		}
		if _, _, ok := store.ForProduct(*c.ProductHandle); ok {
			simulated[c.ID] = true
		}
	}
	drivers, err := gw.ListDrivers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	collects := map[string]bool{}
	for _, d := range drivers {
		if len(d.Spec) == 0 {
			continue // a stub cannot be attached
		}
		spec, err := driver.Parse(d.Spec)
		if err != nil {
			t.Fatalf("driver %s: %v", d.Name, err)
		}
		collects[d.Name] = readsCollectedValues(spec)
	}
	endpoints, err := gw.ListEndpoints(ctx, all)
	if err != nil {
		t.Fatal(err)
	}

	// What each method is exercised by: the healthy simulated endpoints.
	by := map[string][]string{}
	for _, e := range endpoints {
		if e.ComponentID == nil || !simulated[*e.ComponentID] || planted[e.Label] {
			continue
		}
		where := fmt.Sprintf("%s on %s", e.Name, *e.ComponentID)
		if e.Driver == nil {
			by[e.Transport] = append(by[e.Transport], where)
			continue
		}
		if !collects[*e.Driver] {
			continue // a driver that only connects does not count
		}
		by[*e.Driver] = append(by[*e.Driver], where)
		by[e.Transport] = append(by[e.Transport], where)
	}

	methods := map[string]string{}
	for name := range collects {
		methods[name] = "driver"
	}
	for _, tr := range transport.All() {
		methods[tr.Name] = "transport"
	}
	names := make([]string, 0, len(methods))
	for n := range methods {
		names = append(names, n)
	}
	sort.Strings(names)
	exercised := 0
	for _, n := range names {
		gap, gapped := methodGaps[n]
		switch {
		case len(by[n]) > 0 && gapped:
			t.Errorf("%s %s is exercised now (%s): drop it from methodGaps (%s)", methods[n], n, strings.Join(by[n], ", "), gap.issue)
		case len(by[n]) > 0:
			exercised++
		case gapped && gap.unbuilt:
			t.Errorf("%s %s is built now: methodGaps still calls it unbuilt (%s)", methods[n], n, gap.issue)
		case !gapped:
			t.Errorf("%s %s runs on no healthy simulated lab component: attach it in the dev fleet (fixtures.yaml sim_endpoints), or list it in methodGaps with its issue", methods[n], n)
		}
	}
	for n, gap := range methodGaps {
		if _, known := methods[n]; !known && !gap.unbuilt {
			t.Errorf("methodGaps lists %s (%s), which is no driver or transport: delete its line", n, gap.issue)
		}
	}
	t.Logf("%d of %d collection methods run healthy in the dev lab; %d listed as gaps", exercised, len(methods), len(methodGaps))
}

// readsCollectedValues reports a driver that reads values off the device: a
// poll or a listener that emits.
func readsCollectedValues(spec *driver.Spec) bool {
	for _, p := range spec.Polls {
		if len(p.Emits) > 0 {
			return true
		}
	}
	for _, l := range spec.Listeners {
		if len(l.Emits) > 0 {
			return true
		}
	}
	return false
}
