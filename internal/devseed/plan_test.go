package devseed_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/sim/simfleet"
)

// SeedPlan lands everything the plan planned: the systems in their rooms, the
// components in those rooms, each slot staffed by its product's components, and
// the simulated endpoints on the components.
func TestSeedPlanLandsThePlannedRows(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	p := corporatePlan(t, 20)
	res, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{Endpoints: true})
	if err != nil {
		t.Fatal(err)
	}

	var wantSystems, wantComponents, wantSlots int
	type slotKey struct{ system, slot, product string }
	wantStaffing := map[slotKey]int{}
	wantRoom := map[string]string{}
	for _, site := range p.Sites {
		for _, s := range site.Systems {
			wantSystems++
			wantRoom[s.Name] = s.Room
			for _, fill := range s.Slots {
				wantComponents += fill.Count
				wantSlots += fill.Count
				wantStaffing[slotKey{s.Name, fill.Slot, fill.Product}] = fill.Count
			}
		}
	}
	if wantSystems != 20 || wantComponents != p.Totals.Components {
		t.Fatalf("plan has %d systems and %d slot fills, want 20 and %d components", wantSystems, wantComponents, p.Totals.Components)
	}
	// Each platform (#1747) and each polled service (#2765, #2790) is a
	// component of its own, staffing no slot.
	for _, site := range p.Sites {
		for _, ps := range site.Pushers {
			if ps.IsComponent() {
				wantComponents++
			}
		}
	}
	if res.Systems != wantSystems || res.Components != wantComponents || res.Slots != wantSlots {
		t.Fatalf("result = %d systems, %d components, %d slots; want %d, %d, %d", res.Systems, res.Components, res.Slots, wantSystems, wantComponents, wantSlots)
	}

	// Systems: each planned system exists, in its planned room, and no other
	// system was made.
	gotRoom := map[string]string{}
	rows, err := conn.Query(ctx, `select s.name, l.name from system s join location l on l.id = s.location_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name, room string
		if err := rows.Scan(&name, &room); err != nil {
			t.Fatal(err)
		}
		gotRoom[name] = room
	}
	rows.Close()
	if fmt.Sprint(sortedPairs(gotRoom)) != fmt.Sprint(sortedPairs(wantRoom)) {
		t.Fatalf("systems and rooms = %v, want %v", sortedPairs(gotRoom), sortedPairs(wantRoom))
	}

	// Slot assignments: each (system, slot) is staffed by the planned number of
	// components of the planned product.
	gotStaffing := map[slotKey]int{}
	rows, err = conn.Query(ctx, `
		select s.name, r.name, p.name, count(*)
		from system_slot_assignment a
		join system s on s.id = a.system_id
		join system_slot r on r.id = a.slot_id
		join component c on c.id = a.component_id
		join product p on p.id = c.product_id
		group by 1, 2, 3`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k slotKey
		var n int
		if err := rows.Scan(&k.system, &k.slot, &k.product, &n); err != nil {
			t.Fatal(err)
		}
		gotStaffing[k] = n
	}
	rows.Close()
	if len(gotStaffing) != len(wantStaffing) {
		t.Fatalf("%d staffed slots, want %d", len(gotStaffing), len(wantStaffing))
	}
	for k, want := range wantStaffing {
		if gotStaffing[k] != want {
			t.Errorf("%s slot %s: %d x %s, want %d", k.system, k.slot, gotStaffing[k], k.product, want)
		}
	}

	// Components: only the planned ones, each a member of the system it staffs
	// and placed in that system's room.
	var components, misplaced, unmembered int
	if err := conn.QueryRow(ctx, `select count(*) from component`).Scan(&components); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `
		select count(*) from system_slot_assignment a
		join system s on s.id = a.system_id
		join component c on c.id = a.component_id
		where c.location_id <> s.location_id`).Scan(&misplaced); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `
		select count(*) from system_slot_assignment a
		where not exists (select 1 from system_member m where m.system_id = a.system_id and m.component_id = a.component_id)`).Scan(&unmembered); err != nil {
		t.Fatal(err)
	}
	if components != wantComponents || misplaced != 0 || unmembered != 0 {
		t.Errorf("%d components (want %d), %d staffing a system from another room, %d staffing without membership", components, wantComponents, misplaced, unmembered)
	}

	// Endpoints: the simulated fleet's and one webhook per platform are the
	// count in the database, and every one sits on a planned component.
	var endpoints, orphaned int
	if err := conn.QueryRow(ctx, `select count(*) from endpoint`).Scan(&endpoints); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `select count(*) from endpoint e where not exists (select 1 from component c where c.id = e.component)`).Scan(&orphaned); err != nil {
		t.Fatal(err)
	}
	if res.Endpoints == 0 || endpoints != res.Endpoints+res.Senders || orphaned != 0 {
		t.Errorf("%d endpoints in the database, result says %d and %d webhooks, %d on no component; want equal, non-zero, none", endpoints, res.Endpoints, res.Senders, orphaned)
	}

	// Control faces (#2649, #2754): one second face on each controlled slot's
	// device (the display's web admin, the bar's control socket), and on no
	// other component.
	wantControl := map[string]int{}
	for _, site := range p.Sites {
		for _, s := range site.Systems {
			for _, fill := range s.Slots {
				if fill.Control {
					wantControl[s.Name+"/"+fill.Slot] += fill.Count
				}
			}
		}
	}
	if len(wantControl) == 0 {
		t.Fatal("the plan controls no slot; the corporate boardrooms' displays should take commands")
	}
	gotControl := map[string]int{}
	rows, err = conn.Query(ctx, `
		select s.name || '/' || r.name
		from endpoint e
		join driver d on d.id = e.driver_id
		join system_slot_assignment a on a.component_id = e.component
		join system s on s.id = a.system_id
		join system_slot r on r.id = a.slot_id
		where d.name = any($1)`, slices.Collect(maps.Values(simfleet.Control)))
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		gotControl[k]++
	}
	rows.Close()
	if fmt.Sprint(sortedCounts(gotControl)) != fmt.Sprint(sortedCounts(wantControl)) {
		t.Errorf("control faces = %v, want %v", sortedCounts(gotControl), sortedCounts(wantControl))
	}
}

func sortedCounts(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(out)
	return out
}

// The same plan seeded into two fresh databases mints the same component
// names and the same endpoint addresses, so a scale lab run is repeatable.
func TestSeedPlanIsDeterministic(t *testing.T) {
	t.Parallel()
	read := func() []string {
		ctx, gw, conn := openSeededGateway(t)
		if _, err := devseed.SeedPlan(ctx, gw, "", corporatePlan(t, 5), devseed.PlanOptions{Endpoints: true}); err != nil {
			t.Fatal(err)
		}
		rows, err := conn.Query(ctx, `
			select s.name || '/' || r.name || '/' || c.name || '/' || coalesce(e.params->>'target', '')
			from system_slot_assignment a
			join system s on s.id = a.system_id
			join system_slot r on r.id = a.slot_id
			join component c on c.id = a.component_id
			left join endpoint e on e.component = c.id
			order by 1`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	a, b := read(), read()
	if len(a) == 0 || fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("two seeds of one plan differ (%d vs %d rows)", len(a), len(b))
	}
}

func sortedPairs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// planOutposts creates one outpost per name, the outposts a seed's endpoints are placed
// on (an outpost must exist before an endpoint names it).
func planOutposts(t *testing.T, ctx context.Context, gw storage.Gateway, names ...string) {
	t.Helper()
	all := scope.Set{All: true}
	for _, n := range names {
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: n, Label: n}, all, all); err != nil {
			t.Fatalf("create outpost %s: %v", n, err)
		}
	}
}

// endpointOutposts reads the outpost every endpoint of a seeded plan is on, by the
// site its component is in (the root of the component's location chain). A
// platform's webhook (#1747) is server hosted, on no outpost by design, so it is
// left out.
func endpointOutposts(t *testing.T, ctx context.Context, conn *pgx.Conn) map[string]map[string]int {
	t.Helper()
	rows, err := conn.Query(ctx, `
		with recursive up as (
			select id as start_id, id, name, parent_id from location
			union all
			select up.start_id, l.id, l.name, l.parent_id from location l join up on up.parent_id = l.id
		)
		select site.name, coalesce(n.name, ''), count(*)
		from endpoint e
		join component c on c.id = e.component
		join up site on site.start_id = c.location_id and site.parent_id is null
		left join outpost n on n.user_id = e.outpost_id
		where not exists (select 1 from webhook_token w where w.endpoint_id = e.id)
		group by 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]map[string]int{}
	for rows.Next() {
		var site, outpost string
		var n int
		if err := rows.Scan(&site, &outpost, &n); err != nil {
			t.Fatal(err)
		}
		if out[site] == nil {
			out[site] = map[string]int{}
		}
		out[site][outpost] = n
	}
	return out
}

// OutpostFor places each site's endpoints on that site's outpost, and takes
// precedence over the single Outpost, which a caller that sets only it still gets.
func TestSeedPlanPlacesEachSitesEndpointsOnItsOutpost(t *testing.T) {
	t.Parallel()
	p := corporatePlan(t, 20)
	if len(p.Sites) < 2 {
		t.Fatalf("plan has %d sites, want at least two", len(p.Sites))
	}
	outpostOf := func(site string) string { return "edge-" + site }

	t.Run("OutpostFor wins over Outpost", func(t *testing.T) {
		t.Parallel()
		ctx, gw, conn := openSeededGateway(t)
		names := []string{"fallback"}
		for _, s := range p.Sites {
			names = append(names, outpostOf(s.Name))
		}
		planOutposts(t, ctx, gw, names...)
		res, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{Endpoints: true, Outpost: "fallback", OutpostFor: outpostOf})
		if err != nil {
			t.Fatal(err)
		}
		got, total := endpointOutposts(t, ctx, conn), 0
		if len(got) != len(p.Sites) {
			t.Fatalf("endpoints in %d sites, want %d: %v", len(got), len(p.Sites), got)
		}
		for site, byOutpost := range got {
			if len(byOutpost) != 1 || byOutpost[outpostOf(site)] == 0 {
				t.Errorf("site %s endpoints by outpost = %v, want all on %s", site, byOutpost, outpostOf(site))
			}
			total += byOutpost[outpostOf(site)]
		}
		if total != res.Endpoints {
			t.Errorf("%d endpoints on their site's outpost, result says %d", total, res.Endpoints)
		}
	})

	t.Run("a caller that sets only Outpost keeps it", func(t *testing.T) {
		t.Parallel()
		ctx, gw, conn := openSeededGateway(t)
		planOutposts(t, ctx, gw, "only")
		res, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{Endpoints: true, Outpost: "only"})
		if err != nil {
			t.Fatal(err)
		}
		total := 0
		for site, byOutpost := range endpointOutposts(t, ctx, conn) {
			if len(byOutpost) != 1 {
				t.Errorf("site %s endpoints by outpost = %v, want all on only", site, byOutpost)
			}
			total += byOutpost["only"]
		}
		if total == 0 || total != res.Endpoints {
			t.Errorf("%d endpoints on the one outpost, result says %d", total, res.Endpoints)
		}
	})
}

// SeedPlan seeds each pusher the plan declares (#1743) as its own service
// user holding the pusher role (telemetry:push alone) on exactly the
// systems the plan scoped it to, and no token: the lab mints one per run.
func TestSeedPlanGrantsEachPlannedPusherItsSystems(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	p := corporatePlan(t, 20)
	want := map[string][]string{}
	for _, site := range p.Sites {
		for _, ps := range site.Pushers {
			if ps.IsComponent() {
				continue // a platform or a service speaks as its component, holding no grant (#1747)
			}
			want[ps.Name] = append([]string(nil), ps.Systems...)
			sort.Strings(want[ps.Name])
		}
	}
	if len(want) == 0 {
		t.Fatal("the corporate plan declares no pushers")
	}
	res, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pushers != len(want) {
		t.Errorf("result = %d pushers, want %d", res.Pushers, len(want))
	}
	rows, err := conn.Query(ctx, `
		select s.name, r.name, r.permissions::text, g.scope_kind, g.scope_op, y.name
		from machine s join user_grant g on g.user_id = s.user_id
		join role r on r.id = g.role_id
		left join system y on y.id::text = g.scope_id::text`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string][]string{}
	for rows.Next() {
		var svc, role, perms, kind, op string
		var sys *string
		if err := rows.Scan(&svc, &role, &perms, &kind, &op, &sys); err != nil {
			t.Fatal(err)
		}
		if role != devseed.PusherRole || perms != "{telemetry:push}" || kind != "system" || op != "self" || sys == nil {
			t.Errorf("pusher %s holds %s %s on %s %s, want the pusher role on a system", svc, role, perms, kind, op)
			continue
		}
		got[svc] = append(got[svc], *sys)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name := range got {
		sort.Strings(got[name])
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("pusher grants = %v, want %v", got, want)
	}
	var tokens int
	if err := conn.QueryRow(ctx, `select count(*) from credential c join machine s on s.user_id = c.user_id`).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != 0 {
		t.Errorf("the seeded pushers hold %d tokens, want none until the lab mints them", tokens)
	}
}

// SeedPlan seeds the fleet's one meeting platform (#1747, #2790) as a
// component at the headquarters holding one webhook endpoint (server hosted,
// so on no outpost), signing with a secret minted for this seed, and bound as
// an installer binds it: every system of every site by its name on
// ucc-room-id, and every component a meeting platform lists by the world's
// device id on ucc-device-id, so its posts and its lists land on the system or
// component each id names.
func TestSeedPlanSeedsTheMeetingPlatformItsWebhookRoomsAndDevices(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	p := corporatePlan(t, 20)
	wantSite, _ := senderSites(p)
	res, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Senders != len(wantSite) || res.Services != 3 {
		t.Errorf("result = %d senders and %d services, want %d and 3", res.Senders, res.Services, len(wantSite))
	}

	// One ucc-webhook endpoint, on the platform component at the headquarters,
	// on no outpost, with a token minted and a signing secret of its own.
	var comp, site, secretName, secretType string
	var noOutpost, tokened bool
	if err := conn.QueryRow(ctx, `
		select c.id::text, l.name, e.outpost_id is null, e.inputs->>'signing-key', coalesce(t.name, ''),
		       exists (select 1 from webhook_token w where w.endpoint_id = e.id)
		from endpoint e
		join driver d on d.id = e.driver_id
		join component c on c.id = e.component
		join location l on l.id = c.location_id
		left join secret s on s.name = e.inputs->>'signing-key'
		left join secret_type t on t.id = s.secret_type
		where d.name = 'ucc-webhook'`).Scan(&comp, &site, &noOutpost, &secretName, &secretType, &tokened); err != nil {
		t.Fatalf("want exactly one ucc-webhook endpoint: %v", err)
	}
	if site != p.Sites[0].Name || !noOutpost || !tokened || secretType != "hmac-key" {
		t.Errorf("platform at %s: no outpost %v, token %v, secret %q of type %q; want the headquarters, no outpost, a token, an hmac-key", site, noOutpost, tokened, secretName, secretType)
	}

	// Its bindings: each system by name on ucc-room-id, each listed component by
	// the world's device id on ucc-device-id.
	rooms, devices := map[string]string{}, map[string]string{}
	rows, err := conn.Query(ctx, `
		select pt.name, b.value, b.owner_kind, coalesce(s.name, c.name)
		from identity_binding b
		join property_type pt on pt.id = b.property_type_id
		left join system s on s.id = b.system_id
		left join component c on c.id = b.component_id
		where b.conduit_id = $1::uuid`, comp)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var key, value, kind, owner string
		if err := rows.Scan(&key, &value, &kind, &owner); err != nil {
			t.Fatal(err)
		}
		switch {
		case (key == "ucc-room-id" || key == "sched-room-id") && kind == "system":
			rooms[value] = owner
		case key == "ucc-device-id" && kind == "component":
			devices[value] = owner
		default:
			t.Errorf("binding %s=%s owned by a %s %s, want a room on a system or a device on a component", key, value, kind, owner)
		}
	}
	rows.Close()
	var fleet []string
	for _, st := range p.Sites {
		for _, sys := range st.Systems {
			fleet = append(fleet, sys.Name)
		}
	}
	if len(rooms) != len(fleet) {
		t.Errorf("the platform binds %d rooms, want all %d systems of the fleet", len(rooms), len(fleet))
	}
	for _, sys := range fleet {
		if rooms[sys] != sys {
			t.Errorf("room %s is bound to %q, want the system of that name", sys, rooms[sys])
		}
	}
	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	worlds, err := scenario.SiteWorlds(m, cat, p)
	if err != nil {
		t.Fatal(err)
	}
	listed := worlds[scenario.SiteOutpost(p.Sites[0].Name)].Listed(p.Sites[0].Name + "-meeting-1")
	if len(listed) == 0 || len(devices) != len(listed) {
		t.Fatalf("the platform binds %d devices, its world lists %d", len(devices), len(listed))
	}
	for _, l := range listed {
		if devices[l.ID] != l.Name {
			t.Errorf("device %s is bound to component %q, want %s", l.ID, devices[l.ID], l.Name)
		}
	}
}

// SeedPlan binds the fleet's one recording service (#2757) as an installer
// does: a component at the headquarters, every system of every site by its name
// on recording-room-id, and no slot of any standard names a recorder, so a
// room's recordings land on its system from the account's one component.
func TestSeedPlanBindsTheRecordingServiceToEveryRoom(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	p := corporatePlan(t, 20)
	if _, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{}); err != nil {
		t.Fatal(err)
	}
	var profile string
	for _, ps := range p.Sites[0].Pushers {
		if ps.Service && strings.HasSuffix(ps.Name, "-recording-1") {
			profile = ps.Profile
		}
	}
	if profile == "" {
		t.Fatal("the plan places no recording service at the headquarters")
	}
	var comp, site string
	var n int
	if err := conn.QueryRow(ctx, `
		select c.id::text, l.name from component c
		join location l on l.id = c.location_id
		join product pr on pr.id = c.product_id
		where pr.name = $1`, profile).Scan(&comp, &site); err != nil {
		t.Fatalf("want exactly one recording service component: %v", err)
	}
	if site != p.Sites[0].Name {
		t.Errorf("the recording service sits at %s, want the headquarters %s", site, p.Sites[0].Name)
	}
	rows, err := conn.Query(ctx, `
		select b.value, coalesce(s.name, '') from identity_binding b
		join property_type pt on pt.id = b.property_type_id
		left join system s on s.id = b.system_id
		where b.conduit_id = $1::uuid and pt.name = 'recording-room-id' and b.owner_kind = 'system'`, comp)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	bound := map[string]string{}
	for rows.Next() {
		var v, owner string
		if err := rows.Scan(&v, &owner); err != nil {
			t.Fatal(err)
		}
		bound[v] = owner
	}
	for _, st := range p.Sites {
		for _, sys := range st.Systems {
			n++
			if bound[sys.Name] != sys.Name {
				t.Errorf("room %s is bound to %q, want the system of that name", sys.Name, bound[sys.Name])
			}
		}
	}
	if len(bound) != n {
		t.Errorf("the service binds %d rooms, want all %d systems of the fleet", len(bound), n)
	}
	var slots int
	if err := conn.QueryRow(ctx, `select count(*) from system_slot where name = 'recorder'`).Scan(&slots); err != nil {
		t.Fatal(err)
	}
	if slots != 0 {
		t.Errorf("%d systems carry a recorder slot, want none: the service is the account's one component", slots)
	}
}

// inducedTasks counts the tasks whose baked spec names the induced key.
func inducedTasks(t *testing.T, ctx context.Context, conn *pgx.Conn) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from task where spec::text like '%' || $1 || '%'`, devseed.InducedKey).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The induced refusal (#1698): with the knob on, one simulated device's
// endpoint is attached through a driver that also answers a key, and that key
// is then taken out of every catalog. The task still carries it, so the outpost
// collects it and the server has no type to land it on: a refusal by design.
func TestSeedPlanInducesOneUnregisteredKey(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	res, err := devseed.SeedPlan(ctx, gw, "", corporatePlan(t, 3), devseed.PlanOptions{Endpoints: true, InduceUnregistered: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Induced == nil || res.Induced.Key != devseed.InducedKey || res.Induced.Component == "" || res.Induced.Driver == "" {
		t.Fatalf("induced = %+v, want the component, driver and key named", res.Induced)
	}
	t.Logf("induced %+v", *res.Induced)
	if n := inducedTasks(t, ctx, conn); n != 1 {
		t.Errorf("%d tasks carry the induced key, want 1", n)
	}
	if _, err := gw.GetPropertyType(ctx, devseed.InducedKey); !errors.Is(err, storage.ErrPropertyTypeNotFound) {
		t.Errorf("the induced key's type: %v, want it in no catalog", err)
	}
	var drivers int
	if err := conn.QueryRow(ctx, `select count(distinct d.name) from endpoint e join driver d on d.id = e.driver_id where not d.official`).Scan(&drivers); err != nil {
		t.Fatal(err)
	}
	if drivers != 1 {
		t.Errorf("%d custom drivers attached, want the one induced", drivers)
	}
}

// Off by default: a plain seed answers no unregistered key.
func TestSeedPlanInducesNothingByDefault(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	res, err := devseed.SeedPlan(ctx, gw, "", corporatePlan(t, 3), devseed.PlanOptions{Endpoints: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Induced != nil {
		t.Errorf("induced = %+v by default", res.Induced)
	}
	if n := inducedTasks(t, ctx, conn); n != 0 {
		t.Errorf("%d tasks carry the induced key by default", n)
	}
}

// The world owns component names (#2773): the seed onboards every planned
// component under exactly the name its site's world gives it, in the room the
// world puts it in. Seed a plan, then read each outpost's sim device list, the
// list a simulator pulls: every device it names is a device of its site's
// world, placed by an exact lookup of its name, and the platform minted no name
// of its own for a component of the plan.
func TestSeedPlanOnboardsEveryComponentUnderTheWorldsName(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	p := corporatePlan(t, 20)
	outpostOf := func(site string) string { return "edge-" + site }
	var names []string
	for _, s := range p.Sites {
		names = append(names, outpostOf(s.Name))
	}
	planOutposts(t, ctx, gw, names...)
	if _, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{Endpoints: true, OutpostFor: outpostOf}); err != nil {
		t.Fatal(err)
	}
	worlds, err := scenario.SiteWorlds(m, cat, p)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := conn.Query(ctx, `select c.id::text, c.name, l.name, c.name_generated from component c join location l on l.id = c.location_id`)
	if err != nil {
		t.Fatal(err)
	}
	type seeded struct {
		name, room string
		generated  bool
	}
	byID := map[string]seeded{}
	for rows.Next() {
		var id string
		var c seeded
		if err := rows.Scan(&id, &c.name, &c.room, &c.generated); err != nil {
			t.Fatal(err)
		}
		byID[id] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, w := range worlds {
		for _, r := range w.Sites[0].Rooms {
			want += len(r.Devices)
		}
		for _, svc := range w.Services {
			if svc.Webhook || svc.Polled {
				want++
			}
		}
	}
	if len(byID) != want {
		t.Fatalf("the plan seeded %d components, the worlds name %d", len(byID), want)
	}
	for id, c := range byID {
		if c.generated {
			t.Errorf("component %s (%s) carries a platform-minted name, the world's is owed", id, c.name)
		}
	}
	listed := 0
	for _, s := range p.Sites {
		w := worlds[outpostOf(s.Name)]
		devices, err := gw.OutpostSimDevices(ctx, outpostOf(s.Name), scope.Set{All: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range devices {
			listed++
			c := byID[d.ComponentID]
			if c.name != d.Name {
				t.Fatalf("the list names %s, the component is %s", d.Name, c.name)
			}
			place, ok := w.PlaceDevice(d.Name)
			if !ok {
				// A webhook sender sits at its site, in no room: it is a service.
				if c.room == s.Name && w.Covered(d.Name) != nil {
					continue
				}
				t.Fatalf("%s lists %s (%s in %s), which its world does not name", outpostOf(s.Name), d.Name, d.Product, c.room)
			}
			if place.Room != c.room || place.Site != s.Name {
				t.Fatalf("%s: the seed put %s in %s, the world in %s/%s", outpostOf(s.Name), d.Name, c.room, place.Site, place.Room)
			}
		}
	}
	if listed == 0 {
		t.Fatal("no outpost listed a device")
	}
}

// A component's label tells its siblings apart (#2757): the world names a
// component `<system>-<slot>-<n>`, a name the operator typed as far as the
// platform is concerned, so the platform's label rule finds no number of its
// own and two displays of one room would both read "Display". The seed gives a
// slot's components their ordinal in the label, as a dev fleet's read
// "Display 1" and "Display 2", and leaves the name as the world gave it. A
// slot holding one component reads bare. An account-wide service reads as the
// manifest labels it, not as its component type's "Generic Service".
func TestSeedPlanLabelsComponentsWithTheirOrdinalAndServicesByManifest(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	p := corporatePlan(t, 20)
	if _, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{}); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.Query(ctx, `select c.name, coalesce(c.label, '') from component c`)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for rows.Next() {
		var name, label string
		if err := rows.Scan(&name, &label); err != nil {
			t.Fatal(err)
		}
		labels[name] = label
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	multi := 0
	for _, site := range p.Sites {
		for _, s := range site.Systems {
			seen := map[string]string{}
			for _, fill := range s.Slots {
				for n := 1; n <= fill.Count; n++ {
					name := scenario.ComponentName(s.Name, fill.Slot, n)
					label, ok := labels[name]
					if !ok {
						t.Fatalf("component %s was not seeded under the world's name", name)
					}
					if other, dup := seen[label]; dup && fill.Count > 1 {
						t.Errorf("%s and %s of %s both read %q, want the ordinal to tell them apart", other, name, s.Name, label)
					}
					seen[label] = name
					if fill.Count > 1 {
						multi++
						if want := fmt.Sprintf(" %d", n); !strings.HasSuffix(label, want) {
							t.Errorf("%s reads %q, want its slot's ordinal %q at the end", name, label, want)
						}
					} else if strings.HasSuffix(label, " 1") {
						t.Errorf("%s is the only one of its slot and reads %q, want it bare", name, label)
					}
				}
			}
		}
	}
	if multi == 0 {
		t.Fatal("the corporate plan has no slot of several components to judge the ordinal on")
	}
	for _, svc := range m.Services {
		if svc.Label == "" {
			t.Errorf("service %s declares no label", svc.Name.Name)
		}
		name := fmt.Sprintf("%s-%s-1", p.Sites[0].Name, svc.Name.Name)
		if got := labels[name]; got != svc.Label {
			t.Errorf("service %s reads %q, want the manifest's %q", name, got, svc.Label)
		}
	}
}

// A corporate room's occupancy-count follows its people counter (#2757). The
// standard declares occupancy-count on the system, and nothing but a rule can
// carry a member's reading up to it: the counter's raw-TCP feed reports
// people-count on the component, and the plan seeds the one calc rule that
// reads it from the system's slot-bound members. Land a count on a conference
// room's counter and the room's own occupancy-count follows it, by that rule.
func TestSeedPlanRoomOccupancyFollowsItsPeopleCounter(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	if _, err := devseed.SeedPlan(ctx, gw, "", corporatePlan(t, 20), devseed.PlanOptions{Endpoints: true}); err != nil {
		t.Fatal(err)
	}
	const system = "hq-divisible-conference-1"
	var counter, sysID string
	if err := conn.QueryRow(ctx, `select c.id::text, s.id::text from component c, system s where c.name = $1 and s.name = $2`,
		scenario.ComponentName(system, "people-counter", 1), system).Scan(&counter, &sysID); err != nil {
		t.Fatalf("the plan seeds no people counter in %s: %v", system, err)
	}
	if err := gw.InsertMetricSamples(ctx, storage.PushWriter(), []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: counter, Key: "people-count", Value: 7, Source: "test", TS: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.DrainCalcPending(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	eff, err := gw.EffectiveMetrics(ctx, "system", sysID, scope.Set{All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range eff {
		if m.MetricTypeName != "occupancy-count" {
			continue
		}
		if !m.IsSampled || string(m.Value) != "7" || m.SourceRule != "room-occupancy" {
			t.Fatalf("%s occupancy-count = %s (sampled %v, by rule %q), want the counter's 7 by room-occupancy", system, m.Value, m.IsSampled, m.SourceRule)
		}
		return
	}
	t.Fatalf("%s declares no occupancy-count", system)
}

// A video bar counts people too (#2757): its camera reports people-count, so
// a huddle room, which has a bar and no separate counter, fills its
// occupancy-count by the same one rule, with no rule of its own.
func TestSeedPlanHuddleOccupancyFollowsItsVideoBar(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	plan := corporatePlan(t, 20)
	if _, err := devseed.SeedPlan(ctx, gw, "", plan, devseed.PlanOptions{Endpoints: true}); err != nil {
		t.Fatal(err)
	}
	var system string
	for _, sp := range plan.Sites {
		for _, sys := range sp.Systems {
			if sys.Standard == "huddle-room" && system == "" {
				system = sys.Name
			}
		}
	}
	if system == "" {
		t.Fatal("the corporate plan holds no huddle room")
	}
	var bar, sysID string
	if err := conn.QueryRow(ctx, `select c.id::text, s.id::text from component c, system s where c.name = $1 and s.name = $2`,
		scenario.ComponentName(system, "conf-bar", 1), system).Scan(&bar, &sysID); err != nil {
		t.Fatalf("the plan seeds no video bar in %s: %v", system, err)
	}
	if err := gw.InsertMetricSamples(ctx, storage.PushWriter(), []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: bar, Key: "people-count", Value: 4, Source: "test", TS: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.DrainCalcPending(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	eff, err := gw.EffectiveMetrics(ctx, "system", sysID, scope.Set{All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range eff {
		if m.MetricTypeName != "occupancy-count" {
			continue
		}
		if !m.IsSampled || string(m.Value) != "4" || m.SourceRule != "room-occupancy" {
			t.Fatalf("%s occupancy-count = %s (sampled %v, by rule %q), want the bar's 4 by room-occupancy", system, m.Value, m.IsSampled, m.SourceRule)
		}
		return
	}
	t.Fatalf("%s declares no occupancy-count", system)
}

// A corporate room's room-temperature follows its control processor (#2757).
// The standard declares room-temperature on the system, and the processor
// reports it on its component, so the plan seeds the one calc rule that
// carries it up: the same primitive as the room's occupancy, with no second
// mechanism. Land a reading on a conference room's processor and the room's
// own room-temperature follows it, by that rule.
func TestSeedPlanRoomTemperatureFollowsItsControlProcessor(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	if _, err := devseed.SeedPlan(ctx, gw, "", corporatePlan(t, 20), devseed.PlanOptions{Endpoints: true}); err != nil {
		t.Fatal(err)
	}
	const system = "hq-divisible-conference-1"
	var processor, sysID string
	if err := conn.QueryRow(ctx, `select c.id::text, s.id::text from component c, system s where c.name = $1 and s.name = $2`,
		scenario.ComponentName(system, "control-system", 1), system).Scan(&processor, &sysID); err != nil {
		t.Fatalf("the plan seeds no control processor in %s: %v", system, err)
	}
	if err := gw.InsertMetricSamples(ctx, storage.PushWriter(), []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: processor, Key: "room-sensor-temperature", Value: 23.4, Source: "test", TS: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.DrainCalcPending(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	eff, err := gw.EffectiveMetrics(ctx, "system", sysID, scope.Set{All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range eff {
		if m.MetricTypeName != "room-temperature" {
			continue
		}
		if !m.IsSampled || string(m.Value) != "23.4" || m.SourceRule != "room-temperature" {
			t.Fatalf("%s room-temperature = %s (sampled %v, by rule %q), want the processor's 23.4 by room-temperature", system, m.Value, m.IsSampled, m.SourceRule)
		}
		return
	}
	t.Fatalf("%s declares no room-temperature", system)
}

// The counter's feed rides the counter's own second face: a corporate
// conference room's people counter has the http API's endpoint and the raw-TCP
// feed's, the one that reports the head-count (#2757).
func TestSeedPlanWiresThePeopleCountersFeed(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	if _, err := devseed.SeedPlan(ctx, gw, "", corporatePlan(t, 20), devseed.PlanOptions{Endpoints: true}); err != nil {
		t.Fatal(err)
	}
	name := scenario.ComponentName("hq-divisible-conference-1", "people-counter", 1)
	rows, err := conn.Query(ctx, `select d.name from endpoint e join driver d on d.id = e.driver_id join component c on c.id = e.component where c.name = $1 order by 1`, name)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		got = append(got, d)
	}
	rows.Close()
	if want := []string{"tessellate-census-feed", "tessellate-census-http"}; !slices.Equal(got, want) {
		t.Errorf("%s has endpoints on drivers %v, want %v", name, got, want)
	}
}
