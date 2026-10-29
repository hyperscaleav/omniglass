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

// TestSeededFleetHoldsAPusherOnOneSystem proves the simulated fleet carries
// its first-party pusher (#1370): the booking service, holding the pusher
// role (telemetry:push alone) on the HQ boardroom's system alone, with no
// token until the lab mints one. A second run adds no grant.
func TestSeededFleetHoldsAPusherOnOneSystem(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	ctx, conn, gw := seededFleet(t)
	grants := func() []string {
		t.Helper()
		rows, err := conn.Query(ctx, `
			select r.name || ' ' || r.permissions::text || ' ' || g.scope_kind || ' ' || g.scope_op || ' ' || l.name
			from service s join principal_grant g on g.principal_id = s.principal_id
			join role r on r.id = g.role_id
			left join system y on y.id::text = g.scope_id::text left join location l on l.id = y.location_id
			where s.name = 'sim-booking-service'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var g string
			if err := rows.Scan(&g); err != nil {
				t.Fatal(err)
			}
			out = append(out, g)
		}
		return out
	}
	want := devseed.PusherRole + " {telemetry:push} system self boardroom-a"
	if got := grants(); len(got) != 1 || got[0] != want {
		t.Fatalf("the pusher's grants = %v, want [%s]", got, want)
	}
	var tokens int
	if err := conn.QueryRow(ctx, `select count(*) from credential c join service s on s.principal_id = c.principal_id where s.name = 'sim-booking-service'`).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != 0 {
		t.Fatalf("the seeded pusher holds %d tokens, want none until the lab mints one", tokens)
	}
	if err := devseed.Run(ctx, gw, ""); err != nil {
		t.Fatal(err)
	}
	if got := grants(); len(got) != 1 {
		t.Fatalf("a second run changed the pusher's grants: %v", got)
	}
}
