package devseed_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// officialRoles are the role ids the boot seed installs; every fixture grant must
// name one of them, else the grant's role_id foreign key fails at seed time.
var officialRoles = map[string]bool{
	"viewer": true, "operator": true, "deploy": true, "admin": true, "owner": true,
}

// TestFixturesShape is a pure unit check on the embedded fixtures: the tree is
// well formed (every parent named before its children), every user carries a
// password, and every grant references a real role and (when scoped) a location
// declared in the same document. It needs no database, so it runs under -short.
func TestFixturesShape(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if len(doc.Locations) == 0 || len(doc.Users) == 0 {
		t.Fatalf("fixtures empty: %d locations, %d users", len(doc.Locations), len(doc.Users))
	}

	seenLoc := map[string]bool{}
	for _, l := range doc.Locations {
		if l.Name == "" || l.Type == "" {
			t.Errorf("location %+v missing name or type", l)
		}
		if l.Parent != "" && !seenLoc[l.Parent] {
			t.Errorf("location %q references parent %q not declared before it", l.Name, l.Parent)
		}
		seenLoc[l.Name] = true
	}

	for _, u := range doc.Users {
		if u.Username == "" || u.Password == "" {
			t.Errorf("user %+v missing username or password", u)
		}
		if len(u.Grants) == 0 {
			t.Errorf("user %q has no grants (a dev user without access is not useful)", u.Username)
		}
		for _, g := range u.Grants {
			if !officialRoles[g.Role] {
				t.Errorf("user %q grant references unknown role %q", u.Username, g.Role)
			}
			if g.ScopeKind != "all" && !seenLoc[g.ScopeRef] {
				t.Errorf("user %q grant scoped to location %q not in the fixtures", u.Username, g.ScopeRef)
			}
		}
	}

	// A contract line names a product and a property, both resolved by name at seed
	// time against the boot-seed catalogs.
	for _, pp := range doc.ProductProperties {
		if pp.Product == "" || pp.Property == "" {
			t.Errorf("product property %+v missing product or property", pp)
		}
	}

	// Components place a device in the estate; a component that names a location must
	// name one declared in this document (the seed resolves the placement by name).
	seenComp := map[string]bool{}
	for _, c := range doc.Components {
		if c.Name == "" {
			t.Errorf("component %+v missing name", c)
		}
		if c.Location != "" && !seenLoc[c.Location] {
			t.Errorf("component %q placed at location %q not in the fixtures", c.Name, c.Location)
		}
		seenComp[c.Name] = true
	}

	// Property values declare a literal on a component: the component must be declared
	// in this document, else the seed's set fails at run time.
	for _, pv := range doc.PropertyValues {
		if !seenComp[pv.Component] {
			t.Errorf("property value references component %q not in the fixtures", pv.Component)
		}
		if pv.Property == "" {
			t.Errorf("property value on %q names no property", pv.Component)
		}
	}
}

// TestFixturesEstateIsAForest is a pure check that the example estate teaches the
// no-root-location rule: the location tree is a forest with more than one
// unparented top, and devices sit under more than one of those tops. With every
// device under a single top, a binding at that top looks like it covers the
// estate, and the reason the install-wide `platform` tier exists (it is the only
// rung that reaches all of the tops) is invisible in the console.
func TestFixturesEstateIsAForest(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}

	parentOf := map[string]string{}
	var tops []string
	for _, l := range doc.Locations {
		parentOf[l.Name] = l.Parent
		if l.Parent == "" {
			tops = append(tops, l.Name)
		}
	}
	if len(tops) < 2 {
		t.Fatalf("unparented tops = %d %v, want at least 2 (the tree is a forest, there is no root location)", len(tops), tops)
	}

	// topOf walks a location up to its unparented ancestor. TestFixturesShape
	// already proves every parent is declared before its child, so the walk
	// terminates.
	topOf := func(name string) string {
		for parentOf[name] != "" {
			name = parentOf[name]
		}
		return name
	}
	occupied := map[string]bool{}
	for _, c := range doc.Components {
		if c.Location != "" {
			occupied[topOf(c.Location)] = true
		}
	}
	if len(occupied) < 2 {
		t.Errorf("fixture components occupy %d of %d tops (%v), want at least 2 so a binding at one top visibly misses the other", len(occupied), len(tops), occupied)
	}
}

// TestRunIdempotent proves devseed.Run lands the example estate (and the worked
// reachability check) through the Storage Gateway and that a second run neither
// duplicates nor errors: make dev runs it on every start. Reference data (roles,
// location types, datapoint types) must exist first, so the boot seed runs ahead of
// it, exactly as bootstrap does. Skipped under -short by the testcontainer harness.
func TestRunIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("boot seed: %v", err)
	}
	// Run twice: idempotency is the property under test.
	for i := 0; i < 2; i++ {
		if err := devseed.Run(ctx, gw, ""); err != nil {
			t.Fatalf("devseed run %d: %v", i, err)
		}
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// Counts prove idempotency: the second Run added nothing.
	var locs, humans, grants int
	if err := conn.QueryRow(ctx, `select count(*) from location`).Scan(&locs); err != nil {
		t.Fatalf("count locations: %v", err)
	}
	if locs != 13 {
		t.Errorf("locations = %d, want 13 (seed not idempotent or incomplete)", locs)
	}
	// A multi-site estate: three campuses, not one.
	var campuses int
	if err := conn.QueryRow(ctx, `select count(*) from location where location_type = (select id from location_type where name = 'campus')`).Scan(&campuses); err != nil {
		t.Fatalf("count campuses: %v", err)
	}
	if campuses != 3 {
		t.Errorf("campuses = %d, want 3 (hq, east, airport)", campuses)
	}
	if err := conn.QueryRow(ctx, `select count(*) from principal where kind = 'human'`).Scan(&humans); err != nil {
		t.Fatalf("count humans: %v", err)
	}
	if humans != 3 {
		t.Errorf("human principals = %d, want 3", humans)
	}
	if err := conn.QueryRow(ctx, `select count(*) from principal_grant`).Scan(&grants); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grants != 3 {
		t.Errorf("grants = %d, want 3", grants)
	}

	// The property primitive seeds one extra contract line on the QM55, one component
	// bound to that product, and two declared overrides. The counts prove the second
	// Run added none of them: the contract, component, and value loops are each
	// idempotent.
	var contractLines, comps, propVals int
	if err := conn.QueryRow(ctx, `
		select count(*) from product_property where product_id = (select id from product where name = 'samsung-qm55')`).Scan(&contractLines); err != nil {
		t.Fatalf("count contract lines: %v", err)
	}
	if contractLines != 4 {
		t.Errorf("qm55 contract lines = %d, want 4 (3 boot-seed + mac_address)", contractLines)
	}
	if err := conn.QueryRow(ctx, `select count(*) from component where name = 'lobby-display'`).Scan(&comps); err != nil {
		t.Fatalf("count property-seed components: %v", err)
	}
	if comps != 1 {
		t.Errorf("property-seed components = %d, want 1 (lobby-display)", comps)
	}
	if err := conn.QueryRow(ctx, `
		select count(*) from property
		where owner_kind = 'component'
		  and component_id = (select id from component where name = 'lobby-display')`).Scan(&propVals); err != nil {
		t.Fatalf("count property values: %v", err)
	}
	if propVals != 2 {
		t.Errorf("property values = %d, want 2 (serial_number, firmware_version)", propVals)
	}

	// The tree links resolve: the west building hangs under the hq campus.
	var parentName string
	if err := conn.QueryRow(ctx, `
		select p.name from location c join location p on p.id = c.parent_id
		where c.name = 'hq-west'`).Scan(&parentName); err != nil {
		t.Fatalf("read hq-west parent: %v", err)
	}
	if parentName != "hq" {
		t.Errorf("hq-west parent = %q, want hq", parentName)
	}

	// Each seeded user has a password credential (so they can sign in to make dev).
	var pwCreds int
	if err := conn.QueryRow(ctx, `
		select count(*) from credential
		where kind = 'password'
		  and principal_id in (select principal_id from human
		    where username in ('operator', 'viewer-hq', 'tech-east'))`).Scan(&pwCreds); err != nil {
		t.Fatalf("count password creds: %v", err)
	}
	if pwCreds != 3 {
		t.Errorf("password credentials for seeded users = %d, want 3", pwCreds)
	}

	// Grant shapes: operator is all-scoped; viewer-hq reads the hq subtree; tech-east
	// deploys under the East campus excluding its root. The region-scoped users name
	// their region; the all-scoped one does not.
	assertGrant(t, conn, ctx, "operator", "operator", "all", "", "subtree")
	assertGrant(t, conn, ctx, "viewer-hq", "viewer", "location", "hq", "subtree")
	assertGrant(t, conn, ctx, "tech-east", "deploy", "location", "east", "subtree_excl_root")

	// The worked reachability check: an enrolled node, a DSP with two protocol-named
	// interfaces (web/http, qrc/tcp) placed on the node, a poll task over each, and the
	// datapoints the panel reads. Every count is over two Runs, so a duplicate here is
	// the seed failing to be idempotent.
	all := scope.Set{All: true}

	// The component the checks hang on, placed under the HQ boardroom.
	var reachComps int
	if err := conn.QueryRow(ctx, `select count(*) from component where name = 'hq-boardroom-dsp'`).Scan(&reachComps); err != nil {
		t.Fatalf("count reachability component: %v", err)
	}
	if reachComps != 1 {
		t.Errorf("reachability component rows = %d, want 1 (seed not idempotent)", reachComps)
	}

	// The node is created, enrolled, and claimed, so it reads as enrolled.
	node, err := gw.GetNode(ctx, "edge-hq", all)
	if err != nil {
		t.Fatalf("get seeded node: %v", err)
	}
	if !node.Enrolled {
		t.Errorf("node edge-hq enrolled = false, want true (created, enrolled, and claimed)")
	}

	// Two interfaces on the DSP, each named by its protocol and typed by its transport:
	// web (http) and qrc (tcp). Interfaces are id-keyed, so resolve the per-component
	// names through the scoped list and keep their ids for the task check.
	ifaces, err := gw.ListInterfaces(ctx, all)
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
	byName := map[string]*storage.Interface{}
	for i := range ifaces {
		if ifaces[i].Component != nil && *ifaces[i].Component == "hq-boardroom-dsp" {
			byName[ifaces[i].Name] = &ifaces[i]
		}
	}
	// The interface is protocol-named: the DSP's two APIs are named by their
	// transport (http and tcp), not a free-text label.
	httpIf, tcpIf := byName["http"], byName["tcp"]
	if httpIf == nil || tcpIf == nil {
		t.Fatalf("seeded http/tcp interfaces not both found on hq-boardroom-dsp: %v", byName)
	}
	if httpIf.Type != "http" {
		t.Errorf("http interface type = %q, want http", httpIf.Type)
	}
	if tcpIf.Type != "tcp" {
		t.Errorf("tcp interface type = %q, want tcp", tcpIf.Type)
	}
	for _, it := range []*storage.Interface{httpIf, tcpIf} {
		if it.Node == nil || *it.Node != "edge-hq" {
			t.Errorf("interface %s node = %v, want edge-hq", it.Name, it.Node)
		}
	}
	var ifaceCount int
	if err := conn.QueryRow(ctx, `select count(*) from interface where component = (select id from component where name = 'hq-boardroom-dsp')`).Scan(&ifaceCount); err != nil {
		t.Fatalf("count reachability interfaces: %v", err)
	}
	if ifaceCount != 2 {
		t.Errorf("reachability interface rows = %d, want 2 (seed not idempotent)", ifaceCount)
	}

	// One poll task per interface (referenced by surrogate id).
	tasks, err := gw.ListTasks(ctx, all)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	reachTasks := map[string]int{}
	for i := range tasks {
		for _, it := range []*storage.Interface{httpIf, tcpIf} {
			if tasks[i].InterfaceID == it.ID {
				reachTasks[it.Name]++
				if tasks[i].Mode != "poll" || !tasks[i].Enabled {
					t.Errorf("%s task = %+v, want mode poll enabled", it.Name, tasks[i])
				}
			}
		}
	}
	if reachTasks["http"] != 1 || reachTasks["tcp"] != 1 {
		t.Errorf("reachability task rows = %v, want one poll task per interface (seed not idempotent)", reachTasks)
	}

	// The datapoints populate the panel: each interface has a fresh "up" verdict and
	// both probe layers green. http reads cleanly up (one transition); tcp carries the
	// up->down->up recovered-blip history (three transitions). The transition counts
	// also prove the datapoints did not double on the second Run (append-only, so the
	// sentinel must have skipped them).
	for _, tc := range []struct {
		iface       string
		transitions int
	}{
		{iface: "http", transitions: 1},
		{iface: "tcp", transitions: 3},
	} {
		verdict, err := gw.LatestState(ctx, "hq-boardroom-dsp", "interface.reachable", tc.iface)
		if err != nil {
			t.Fatalf("latest verdict %s: %v", tc.iface, err)
		}
		if verdict == nil || verdict.Value != "up" {
			t.Fatalf("seeded %s verdict = %+v, want value up", tc.iface, verdict)
		}
		transitions, err := gw.StateTransitions(ctx, "hq-boardroom-dsp", "interface.reachable", tc.iface, time.Time{})
		if err != nil {
			t.Fatalf("state transitions %s: %v", tc.iface, err)
		}
		if len(transitions) != tc.transitions {
			t.Errorf("%s verdict transitions = %d, want %d (idempotent across two Runs)", tc.iface, len(transitions), tc.transitions)
		}
		tcpOpen, err := gw.LatestMetricInstance(ctx, "hq-boardroom-dsp", "tcp.open", tc.iface)
		if err != nil {
			t.Fatalf("latest tcp.open %s: %v", tc.iface, err)
		}
		if tcpOpen == nil || tcpOpen.Value != 1 {
			t.Errorf("seeded %s tcp.open = %+v, want 1", tc.iface, tcpOpen)
		}
		icmpReach, err := gw.LatestMetricInstance(ctx, "hq-boardroom-dsp", "icmp.reachable", tc.iface)
		if err != nil {
			t.Fatalf("latest icmp.reachable %s: %v", tc.iface, err)
		}
		if icmpReach == nil || icmpReach.Value != 1 {
			t.Errorf("seeded %s icmp.reachable = %+v, want 1", tc.iface, icmpReach)
		}
	}

	// The example event log: a handful of log occurrences on the lobby display, so
	// the console's event-log panel comes up populated. The count is over two Runs, so
	// a duplicate here is the seed failing to be idempotent (the event table has an
	// auto id and no natural unique key, so only the sentinel guard keeps a re-run a
	// no-op).
	var events int
	if err := conn.QueryRow(ctx, `select count(*) from event where component_id = (select id from component where name = 'lobby-display')`).Scan(&events); err != nil {
		t.Fatalf("count lobby-display events: %v", err)
	}
	if events != 6 {
		t.Errorf("lobby-display events = %d, want 6 (seed not idempotent or incomplete)", events)
	}
	// One occurrence carries a structured attributes payload (the switched input); the
	// rest are plain messages. Provenance is stamped observed by the insert.
	var withAttrs int
	if err := conn.QueryRow(ctx, `
		select count(*) from event
		where component_id = (select id from component where name = 'lobby-display')
		  and attributes is not null and provenance = 'observed'`).Scan(&withAttrs); err != nil {
		t.Fatalf("count lobby-display events with attributes: %v", err)
	}
	if withAttrs != 1 {
		t.Errorf("lobby-display events with attributes = %d, want 1", withAttrs)
	}
}

// TestSeededEstateTeachesPlatformReach carries the forest into the database and
// proves what it is there to teach: the seeded estate has as many unparented tops
// as the fixture declares (no row stands in for a root), and the platform binding
// is the only rung that reaches all of them. The component under the HQ subtree
// reads that subtree's location override, while the component under a different
// top falls back to the install-wide `platform` value, which is the case a
// synthetic root location would have hidden.
func TestSeededEstateTeachesPlatformReach(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("boot seed: %v", err)
	}
	if err := devseed.Run(ctx, gw, ""); err != nil {
		t.Fatalf("devseed run: %v", err)
	}

	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	wantTops := 0
	for _, l := range doc.Locations {
		if l.Parent == "" {
			wantTops++
		}
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var tops int
	if err := conn.QueryRow(ctx, `select count(*) from location where parent_id is null`).Scan(&tops); err != nil {
		t.Fatalf("count tops: %v", err)
	}
	if tops < 2 || tops != wantTops {
		t.Fatalf("unparented locations = %d, want %d (at least 2: the tree is a forest, nothing seeds a root)", tops, wantTops)
	}

	// The two placed components, one per top. Their effective tags are the whole
	// point: same key, different rung, because the location binding stops at its
	// own top's subtree.
	all := scope.Set{All: true}
	underHQ, err := gw.GetComponent(ctx, "lobby-display", all)
	if err != nil {
		t.Fatalf("get hq component: %v", err)
	}
	underEast, err := gw.GetComponent(ctx, "auditorium-display", all)
	if err != nil {
		t.Fatalf("get east component: %v", err)
	}
	eff, err := gw.EffectiveTags(ctx, "component", []string{underHQ.ID, underEast.ID})
	if err != nil {
		t.Fatalf("effective tags: %v", err)
	}
	if got := eff[underHQ.ID]["environment"]; got != "staging" {
		t.Errorf("lobby-display environment = %q, want staging (the hq-west location binding wins)", got)
	}
	if got := eff[underEast.ID]["environment"]; got != "prod" {
		t.Errorf("auditorium-display environment = %q, want prod (a different top, so only the platform binding reaches it)", got)
	}
}

// assertGrant checks a seeded user holds exactly the expected role at the
// expected scope. scopeName is the location name the grant points at ("" for the
// all scope), resolved to its id for the comparison.
func assertGrant(t *testing.T, conn *pgx.Conn, ctx context.Context, username, role, scopeKind, scopeName, scopeOp string) {
	t.Helper()
	var gotKind, gotOp string
	var gotScopeID *string
	if err := conn.QueryRow(ctx, `
		select g.scope_kind, g.scope_op, g.scope_id
		from principal_grant g join human h on h.principal_id = g.principal_id
		where h.username = $1 and g.role_id = (select id from role where name = $2)`, username, role).Scan(&gotKind, &gotOp, &gotScopeID); err != nil {
		t.Fatalf("read grant for %s/%s: %v", username, role, err)
	}
	if gotKind != scopeKind || gotOp != scopeOp {
		t.Errorf("%s grant = kind %q op %q, want kind %q op %q", username, gotKind, gotOp, scopeKind, scopeOp)
	}
	if scopeName == "" {
		if gotScopeID != nil {
			t.Errorf("%s all-scope grant has scope_id %v, want null", username, *gotScopeID)
		}
		return
	}
	var wantID string
	if err := conn.QueryRow(ctx, `select id from location where name = $1`, scopeName).Scan(&wantID); err != nil {
		t.Fatalf("resolve location %q: %v", scopeName, err)
	}
	if gotScopeID == nil || *gotScopeID != wantID {
		t.Errorf("%s grant scope_id = %v, want %s (%s)", username, gotScopeID, wantID, scopeName)
	}
}
