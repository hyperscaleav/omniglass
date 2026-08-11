package devseed_test

import (
	"context"
	"strconv"
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

// TestFixturesShape is a pure unit check on the embedded fixtures: every row
// carries a key, the tree is well formed (every parent named before its
// children), every reference resolves to a key declared in the same document,
// every user carries a password, and every grant references a real role. It
// needs no database, so it runs under -short.
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
		if l.Key == "" || l.Type == "" {
			t.Errorf("location %+v missing key or type", l)
		}
		if seenLoc[l.Key] {
			t.Errorf("location key %q is declared twice (a key is the fixture's identity)", l.Key)
		}
		if l.Parent != "" && !seenLoc[l.Parent] {
			t.Errorf("location %q references parent key %q not declared before it", l.Key, l.Parent)
		}
		seenLoc[l.Key] = true
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
				t.Errorf("user %q grant scoped to location key %q not in the fixtures", u.Username, g.ScopeRef)
			}
		}
	}

	for _, b := range doc.TagBindings {
		if !seenLoc[b.Location] {
			t.Errorf("tag %q binds at location key %q not in the fixtures", b.Key, b.Location)
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
	// name one declared in this document (the seed resolves the placement by key).
	seenComp := map[string]bool{}
	for _, c := range doc.Components {
		if c.Key == "" {
			t.Errorf("component %+v missing key", c)
		}
		if seenComp[c.Key] {
			t.Errorf("component key %q is declared twice", c.Key)
		}
		if c.Location != "" && !seenLoc[c.Location] {
			t.Errorf("component %q placed at location key %q not in the fixtures", c.Key, c.Location)
		}
		seenComp[c.Key] = true
	}

	seenSys := map[string]bool{}
	for _, s := range doc.Systems {
		if s.Key == "" {
			t.Errorf("system %+v missing key", s)
		}
		if seenSys[s.Key] {
			t.Errorf("system key %q is declared twice", s.Key)
		}
		if s.Location != "" && !seenLoc[s.Location] {
			t.Errorf("system %q placed at location key %q not in the fixtures", s.Key, s.Location)
		}
		seenSys[s.Key] = true
	}

	for _, m := range doc.Members {
		if !seenSys[m.System] || !seenComp[m.Component] {
			t.Errorf("member %+v references a key not in the fixtures", m)
		}
	}
	for _, ra := range doc.RoleAssignments {
		if !seenSys[ra.System] || !seenComp[ra.Component] {
			t.Errorf("role assignment %+v references a key not in the fixtures", ra)
		}
		if ra.Role == "" {
			t.Errorf("role assignment %+v names no role", ra)
		}
	}

	// Property values declare a literal on a component: the component must be declared
	// in this document, else the seed's set fails at run time.
	for _, pv := range doc.PropertyValues {
		if !seenComp[pv.Component] {
			t.Errorf("property value references component key %q not in the fixtures", pv.Component)
		}
		if pv.Property == "" {
			t.Errorf("property value on %q names no property", pv.Component)
		}
	}
}

// TestFixturesLetThePlatformNameTheEstate is the pure guard on what this estate
// is FOR: it demonstrates the name generator, so a fixture row must not hand the
// platform a name it was supposed to mint. It is the test that fails the moment
// somebody goes back to hand-writing names, which is the failure that otherwise
// looks like success (every other assertion still passes while the feature is no
// longer exercised at all).
//
// The exceptions are enumerated rather than counted, because each one is a
// separate argument: a campus, a building and a room have a real-world name the
// platform cannot produce (no name rule on those location types, so a nameless
// create is refused outright), and nothing else in the fixture may carry one.
func TestFixturesLetThePlatformNameTheEstate(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}

	// The location types whose rows an operator must name: they carry no name
	// rule in the boot seed, on purpose.
	nominal := map[string]bool{"campus": true, "building": true, "room": true}
	generated := 0
	for _, l := range doc.Locations {
		switch {
		case nominal[l.Type] && l.Name == "":
			t.Errorf("location %q is a %s and carries no name, which the platform cannot mint (that type has no name rule)", l.Key, l.Type)
		case !nominal[l.Type] && l.Name != "":
			t.Errorf("location %q is a %s and hand-writes the name %q; a positional type is the one the platform names", l.Key, l.Type, l.Name)
		case l.Name == "":
			generated++
		}
	}
	if generated == 0 {
		t.Errorf("no seeded location asks the platform for a name, so the estate demonstrates the location-tier generator not at all")
	}

	for _, c := range doc.Components {
		if c.Name != "" {
			t.Errorf("component %q hand-writes the name %q; every seeded component's name comes from its product's stem", c.Key, c.Name)
		}
	}
	for _, s := range doc.Systems {
		if s.Name != "" {
			t.Errorf("system %q hand-writes the name %q; every seeded system's name comes from its type's stem", s.Key, s.Name)
		}
	}
}

// TestFixturesKeepLabelsOnlyWhereTheOverrideIsThePoint is the label half of the
// same guard. A set display_name takes the pen from the platform (#682), so a
// fixture that sets one everywhere demonstrates the opposite of the label rules.
// The survivors are pinned by key, each for a stated reason, so setting one on a
// row that could render its own is a deliberate edit here rather than a quiet
// one that nothing notices.
func TestFixturesKeepLabelsOnlyWhereTheOverrideIsThePoint(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}

	// The one component with no product: with no classification to read, the
	// component rule can only render "Generic Device 1", so the operator's own
	// words are the only thing that says what the box is.
	wantComponentLabels := map[string]bool{"power": true}
	for _, c := range doc.Components {
		if (c.DisplayName != "") != wantComponentLabels[c.Key] {
			t.Errorf("component %q display_name = %q, want set = %v (everything else lets the shipped component rule render)",
				c.Key, c.DisplayName, wantComponentLabels[c.Key])
		}
	}

	// Both systems: which half of a divisible boardroom is A and which is B is a
	// fact about the air wall, and the shipped system rule (which reads the
	// type) renders the same label for both of them.
	for _, s := range doc.Systems {
		if s.DisplayName == "" {
			t.Errorf("system %q has no display_name; the shipped system rule renders the type, so both halves would read alike", s.Key)
		}
	}

	// Locations, which the shipped rule reaches as of #657: it reads the name as
	// words and titles it, so a pin restating what the rule would render hides
	// the feature behind a hand-typed copy of its own output. The survivors are
	// pinned by key with the reason in the fixture beside each:
	//
	//	hq, west, east, airport   the name is a bearing or an abbreviation, and
	//	                          the label is the place ("West" is a direction)
	//	huddle, briefing          the room's noun is not in its name
	//	hall                      "Innovation" is not derivable from "hall"
	//	west-l2, hall-l1          ADR-0103: a positional name is allocation order
	//	                          and the designation is signage. Releasing these
	//	                          two deletes that worked example from the estate.
	//
	// boardroom, auditorium, annex and the media lab carry none, which is what
	// makes the estate demonstrate the rule instead of masking it.
	wantLocationLabels := map[string]bool{
		"hq": true, "west": true, "east": true, "airport": true,
		"huddle": true, "briefing": true, "hall": true,
		"west-l2": true, "hall-l1": true,
	}
	for _, l := range doc.Locations {
		if (l.DisplayName != "") != wantLocationLabels[l.Key] {
			t.Errorf("location %q display_name = %q, want set = %v (everything else lets the shipped location rule render)",
				l.Key, l.DisplayName, wantLocationLabels[l.Key])
		}
	}
}

// TestFixturesEstateIsAForest is a pure check that the example estate teaches the
// no-root-location rule: the location tree is a forest with more than one
// unparented top, and devices sit under more than one of those tops. With every
// device under a single top, a binding at that top looks like it covers the
// estate, and the reason the install-wide `platform` tier exists (it is the only
// rung that reaches all of them) is invisible in the console.
func TestFixturesEstateIsAForest(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}

	parentOf := map[string]string{}
	var tops []string
	for _, l := range doc.Locations {
		parentOf[l.Key] = l.Parent
		if l.Parent == "" {
			tops = append(tops, l.Key)
		}
	}
	if len(tops) < 2 {
		t.Fatalf("unparented tops = %d %v, want at least 2 (the tree is a forest, there is no root location)", len(tops), tops)
	}

	// topOf walks a location up to its unparented ancestor. TestFixturesShape
	// already proves every parent is declared before its child, so the walk
	// terminates.
	topOf := func(key string) string {
		for parentOf[key] != "" {
			key = parentOf[key]
		}
		return key
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
// location types, sample types) must exist first, so the boot seed runs ahead of
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

	// Counts prove idempotency: the second Run added nothing. They are the
	// assertion that catches a seed which cannot recognise its own
	// platform-named rows, because that failure doubles the estate rather than
	// erroring.
	var locs, humans, grants int
	if err := conn.QueryRow(ctx, `select count(*) from location`).Scan(&locs); err != nil {
		t.Fatalf("count locations: %v", err)
	}
	if locs != 13 {
		t.Errorf("locations = %d, want 13 (seed not idempotent or incomplete)", locs)
	}
	var comps, systems int
	if err := conn.QueryRow(ctx, `select count(*) from component`).Scan(&comps); err != nil {
		t.Fatalf("count components: %v", err)
	}
	if comps != 8 {
		t.Errorf("components = %d, want 8 (7 fixture devices, all platform-named, plus the operator-named DSP)", comps)
	}
	if err := conn.QueryRow(ctx, `select count(*) from system`).Scan(&systems); err != nil {
		t.Fatalf("count systems: %v", err)
	}
	if systems != 2 {
		t.Errorf("systems = %d, want 2 (the two halves of the divisible boardroom)", systems)
	}
	// Membership and staffing are counted too, because they are where a resolver
	// that zipped the fixture onto the estate in the wrong order shows up: every
	// name and label would still be right, and the second Run would staff a
	// second device into a role the first Run already filled.
	var members, assignments int
	if err := conn.QueryRow(ctx, `select count(*) from system_member`).Scan(&members); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if members != 6 {
		t.Errorf("system members = %d, want 6 (four in the first half including the unstaffed conditioner, two in the second, the shared bar in both)", members)
	}
	if err := conn.QueryRow(ctx, `select count(*) from system_role_assignment`).Scan(&assignments); err != nil {
		t.Fatalf("count role assignments: %v", err)
	}
	if assignments != 5 {
		t.Errorf("role assignments = %d, want 5 (the fixture's five, unchanged by the second Run)", assignments)
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
	// in the huddle room bound to that product, and two declared overrides. The counts
	// prove the second Run added none of them: the contract, component, and value loops
	// are each idempotent.
	var contractLines, huddleComps, propVals int
	if err := conn.QueryRow(ctx, `
		select count(*) from product_property where product_id = (select id from product where name = 'samsung-qm55')`).Scan(&contractLines); err != nil {
		t.Fatalf("count contract lines: %v", err)
	}
	if contractLines != 4 {
		t.Errorf("qm55 contract lines = %d, want 4 (3 boot-seed + mac-address)", contractLines)
	}
	if err := conn.QueryRow(ctx, `
		select count(*) from component where location_id = (select id from location where name = 'huddle')`).Scan(&huddleComps); err != nil {
		t.Fatalf("count huddle components: %v", err)
	}
	if huddleComps != 1 {
		t.Errorf("huddle room components = %d, want 1 (the display carrying the property overrides)", huddleComps)
	}
	if err := conn.QueryRow(ctx, `
		select count(*) from property
		where owner_kind = 'component'
		  and component_id = `+huddleDisplaySQL+`
		  and provenance = 'declared'`).Scan(&propVals); err != nil {
		t.Fatalf("count declared values: %v", err)
	}
	if propVals != 2 {
		t.Errorf("declared value rows = %d, want 2 (serial-number, firmware-version; a re-run re-declares the same values, which appends nothing)", propVals)
	}

	// The tree links resolve: the west building hangs under the hq campus, and
	// its name carries no ancestry, because a location's name is unique within
	// its parent rather than across the estate.
	var parentName string
	if err := conn.QueryRow(ctx, `
		select p.name from location c join location p on p.id = c.parent_id
		where c.name = 'west'`).Scan(&parentName); err != nil {
		t.Fatalf("read west parent: %v", err)
	}
	if parentName != "hq" {
		t.Errorf("west parent = %q, want hq", parentName)
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
	// samples the panel reads. Every count is over two Runs, so a duplicate here is
	// the seed failing to be idempotent.
	all := scope.Set{All: true}

	// The component the checks hang on, placed under the boardroom. It is the one
	// component in the estate an operator named, so it is addressable by that
	// name where its platform-named siblings are not.
	var reachComps int
	if err := conn.QueryRow(ctx, `select count(*) from component where name = 'dsp'`).Scan(&reachComps); err != nil {
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
		if ifaces[i].Component != nil && *ifaces[i].Component == "dsp" {
			byName[ifaces[i].Name] = &ifaces[i]
		}
	}
	// The interface is protocol-named: the DSP's two APIs are named by their
	// transport (http and tcp), not a free-text label.
	httpIf, tcpIf := byName["http"], byName["tcp"]
	if httpIf == nil || tcpIf == nil {
		t.Fatalf("seeded http/tcp interfaces not both found on the dsp: %v", byName)
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
	if err := conn.QueryRow(ctx, `select count(*) from interface where component = (select id from component where name = 'dsp')`).Scan(&ifaceCount); err != nil {
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

	// The samples populate the panel: each interface has a fresh "up" verdict and
	// both probe layers green. http reads cleanly up (one transition); tcp carries the
	// up->down->up recovered-blip history (three transitions). The transition counts
	// also prove the samples did not double on the second Run (append-only, so the
	// sentinel must have skipped them).
	for _, tc := range []struct {
		iface       string
		transitions int
	}{
		{iface: "http", transitions: 1},
		{iface: "tcp", transitions: 3},
	} {
		verdict, err := gw.LatestProperty(ctx, "dsp", "interface-reachable", tc.iface)
		if err != nil {
			t.Fatalf("latest verdict %s: %v", tc.iface, err)
		}
		if verdict == nil || verdict.Value != "up" {
			t.Fatalf("seeded %s verdict = %+v, want value up", tc.iface, verdict)
		}
		transitions, err := gw.PropertyTransitions(ctx, "dsp", "interface-reachable", tc.iface, time.Time{})
		if err != nil {
			t.Fatalf("property transitions %s: %v", tc.iface, err)
		}
		if len(transitions) != tc.transitions {
			t.Errorf("%s verdict transitions = %d, want %d (idempotent across two Runs)", tc.iface, len(transitions), tc.transitions)
		}
		tcpOpen, err := gw.LatestMetricInstance(ctx, "dsp", "tcp-open", tc.iface)
		if err != nil {
			t.Fatalf("latest tcp-open %s: %v", tc.iface, err)
		}
		if tcpOpen == nil || tcpOpen.Value != 1 {
			t.Errorf("seeded %s tcp-open = %+v, want 1", tc.iface, tcpOpen)
		}
		icmpReach, err := gw.LatestMetricInstance(ctx, "dsp", "icmp-reachable", tc.iface)
		if err != nil {
			t.Fatalf("latest icmp-reachable %s: %v", tc.iface, err)
		}
		if icmpReach == nil || icmpReach.Value != 1 {
			t.Errorf("seeded %s icmp-reachable = %+v, want 1", tc.iface, icmpReach)
		}
	}

	// The example events: a handful of native call-started occurrences on a boardroom
	// video bar, so the console's event panel comes up populated. The count is over two
	// Runs, so a duplicate here is the seed failing to be idempotent (the event table
	// has an auto id and no natural unique key, so only the sentinel guard keeps a re-run
	// a no-op). The bar is addressed the way the seed addresses it, by placement and
	// platform-minted name, because nothing typed one for it.
	var events int
	if err := conn.QueryRow(ctx, `select count(*) from event where component_id = `+barSQL).Scan(&events); err != nil {
		t.Fatalf("count video bar events: %v", err)
	}
	if events != 4 {
		t.Errorf("video bar events = %d, want 4 (seed not idempotent or incomplete)", events)
	}
	// Two occurrences carry a structured attributes payload (the call's peer and
	// protocol); the rest are plain messages. Provenance is stamped observed by the insert.
	var withAttrs int
	if err := conn.QueryRow(ctx, `
		select count(*) from event
		where component_id = `+barSQL+`
		  and attributes is not null and provenance = 'observed'`).Scan(&withAttrs); err != nil {
		t.Fatalf("count video bar events with attributes: %v", err)
	}
	if withAttrs != 2 {
		t.Errorf("video bar events with attributes = %d, want 2", withAttrs)
	}

	// The raw-log lane (ADR-0066): the huddle room's display carries its seeded log
	// lines, so the console log panel comes up populated. Counted over two Runs, so a
	// duplicate is the seed failing to be idempotent.
	var logs int
	if err := conn.QueryRow(ctx, `select count(*) from log_line where component_id = `+huddleDisplaySQL).Scan(&logs); err != nil {
		t.Fatalf("count huddle display log lines: %v", err)
	}
	if logs != 6 {
		t.Errorf("huddle display log lines = %d, want 6 (seed not idempotent or incomplete)", logs)
	}
}

// huddleDisplaySQL and barSQL address two platform-named components the way
// anything outside the seed has to: by placement plus the name the generator
// minted, since a bare `display-1` matches three rows in this estate.
const (
	huddleDisplaySQL = `(select id from component where name = 'display-1'
		and location_id = (select id from location where name = 'huddle'))`
	barSQL = `(select id from component where name = 'videobar-2'
		and location_id = (select id from location where name = 'boardroom'))`
)

// TestSeededNamesComeFromTheGenerator is the acceptance this slice exists for.
// It asserts every seeded name BY VALUE, so a fixture that went back to
// hand-writing them fails here on the pen (a typed name clears name_generated
// and stores no ordinal) as well as on the value.
//
// The names are not incidental. `display-1` appears three times, once per room,
// which is the placement-scoped unique index doing its job; `boardroom` carries
// no ordinal while storing 1, which is the first-of-its-stem suppression
// (ADR-0101); and the two floors are both `1`, each in its own building.
func TestSeededNamesComeFromTheGenerator(t *testing.T) {
	ctx, conn, _ := seededEstate(t)

	for _, tc := range []struct {
		what      string
		table     string
		place     string
		name      string
		ordinal   int
		generated bool
	}{
		// Locations: only a positional type generates, so the two floors do and
		// the eleven nominal rows carry the operator's own name.
		{what: "the floor under the west building", table: "location", place: "west", name: "1", ordinal: 1, generated: true},
		{what: "the floor under innovation hall", table: "location", place: "hall", name: "1", ordinal: 1, generated: true},
		{what: "the west building", table: "location", place: "hq", name: "west", generated: false},

		// Components: the stem comes from the product's component_type, the
		// ordinal from the room.
		{what: "the huddle display", table: "component", place: "huddle", name: "display-1", ordinal: 1, generated: true},
		{what: "the shared video bar", table: "component", place: "boardroom", name: "videobar-1", ordinal: 1, generated: true},
		{what: "the second video bar", table: "component", place: "boardroom", name: "videobar-2", ordinal: 2, generated: true},
		{what: "the first boardroom panel", table: "component", place: "boardroom", name: "display-1", ordinal: 1, generated: true},
		{what: "the second boardroom panel", table: "component", place: "boardroom", name: "display-2", ordinal: 2, generated: true},
		{what: "the power conditioner", table: "component", place: "boardroom", name: "device-1", ordinal: 1, generated: true},
		{what: "the auditorium display", table: "component", place: "auditorium", name: "display-1", ordinal: 1, generated: true},
		{what: "the DSP", table: "component", place: "boardroom", name: "dsp", generated: false},

		// Systems: the first of its stem in a bucket carries no ordinal while
		// storing one.
		{what: "the first boardroom half", table: "system", place: "boardroom", name: "boardroom", ordinal: 1, generated: true},
		{what: "the second boardroom half", table: "system", place: "boardroom", name: "boardroom-2", ordinal: 2, generated: true},
	} {
		placeCol := "location_id"
		if tc.table == "location" {
			placeCol = "parent_id"
		}
		var gotGenerated bool
		var gotOrdinal *int
		err := conn.QueryRow(ctx, `select name_generated, ordinal from `+tc.table+`
			where name = $1 and `+placeCol+` = (select id from location where name = $2)`,
			tc.name, tc.place).Scan(&gotGenerated, &gotOrdinal)
		if err != nil {
			t.Errorf("%s: no %s named %q under %q: %v", tc.what, tc.table, tc.name, tc.place, err)
			continue
		}
		if gotGenerated != tc.generated {
			t.Errorf("%s (%s %q): name_generated = %v, want %v", tc.what, tc.table, tc.name, gotGenerated, tc.generated)
		}
		switch {
		case tc.generated && (gotOrdinal == nil || *gotOrdinal != tc.ordinal):
			t.Errorf("%s (%s %q): ordinal = %v, want %d (the platform owns the name, so it owns the number)", tc.what, tc.table, tc.name, gotOrdinal, tc.ordinal)
		case !tc.generated && gotOrdinal != nil:
			t.Errorf("%s (%s %q): ordinal = %d, want absent (an operator typed this name, so the platform owns no number for it)", tc.what, tc.table, tc.name, *gotOrdinal)
		}
	}

	// A room is operator-named and its PARENT is not, so the estate nests a
	// typed name under a minted one: `boardroom` sits under the floor the
	// platform called `1`, under the building an operator called `west`.
	var roomGenerated bool
	var roomOrdinal *int
	if err := conn.QueryRow(ctx, `
		select r.name_generated, r.ordinal
		from location r
		join location f on f.id = r.parent_id
		join location b on b.id = f.parent_id
		where r.name = 'boardroom' and f.name = '1' and b.name = 'west'`).Scan(&roomGenerated, &roomOrdinal); err != nil {
		t.Fatalf("read the boardroom under the west building's floor: %v", err)
	}
	if roomGenerated || roomOrdinal != nil {
		t.Errorf("the boardroom reads (generated %v, ordinal %v), want (false, absent): a room's name is ground truth the platform cannot mint", roomGenerated, roomOrdinal)
	}

	// A bare `display-1` is three rows, which is the direct proof that the
	// fixture could not have used a generated name as its own identity.
	var displays int
	if err := conn.QueryRow(ctx, `select count(*) from component where name = 'display-1'`).Scan(&displays); err != nil {
		t.Fatalf("count display-1: %v", err)
	}
	if displays != 3 {
		t.Errorf("components named display-1 = %d, want 3 (one per room; a name is unique within its placement, not across the estate)", displays)
	}
}

// TestASeededComponentNameAgreesWithItsProductsStem derives the expected stem
// from the catalog rather than restating it: it walks each component's
// product -> component_type chain for the first stem set on it, exactly as the
// generator's resolveTypeFacts does, and checks the name the row actually
// carries was minted from that. A stem edited in the boot seed moves this
// assertion with it, where a hand-written list of names would drift.
func TestASeededComponentNameAgreesWithItsProductsStem(t *testing.T) {
	ctx, conn, _ := seededEstate(t)

	rows, err := conn.Query(ctx, `
		with recursive chain as (
			select c.id as component_id, c.name, c.ordinal, ct.id as type_id, ct.parent_id, ct.stem, 0 as depth
			from component c
			join product p on p.id = c.product_id
			join component_type ct on ct.id = p.component_type_id
			where c.name_generated
			union all
			select chain.component_id, chain.name, chain.ordinal, up.id, up.parent_id, up.stem, chain.depth + 1
			from chain join component_type up on up.id = chain.parent_id
		)
		select distinct on (component_id) component_id, name, ordinal, coalesce(stem, '')
		from chain
		where stem is not null and stem <> ''
		order by component_id, depth`)
	if err != nil {
		t.Fatalf("resolve component stems: %v", err)
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var id, name, stem string
		var ordinal *int
		if err := rows.Scan(&id, &name, &ordinal, &stem); err != nil {
			t.Fatalf("scan component stem: %v", err)
		}
		seen++
		if ordinal == nil {
			t.Errorf("component %q is platform-named but records no ordinal", name)
			continue
		}
		// A component mint never suppresses: a rack is counted, so the first
		// display in a room is display-1 and not display (ADR-0101).
		if want := stem + "-" + strconv.Itoa(*ordinal); name != want {
			t.Errorf("component %q was minted from stem %q at ordinal %d, so its name should be %q", name, stem, *ordinal, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate component stems: %v", err)
	}
	if seen != 7 {
		t.Errorf("platform-named components with a resolvable stem = %d, want 7 (every fixture device)", seen)
	}
}

// TestSeededLabelsRenderFromTheirRules is the label half of the demonstration.
// A component's label comes from the shipped rule over its resolved type and the
// ordinal the generator allocated, so it reads back what the thing is and which
// one it is without anybody typing it. The rows that DO carry a typed label are
// asserted to still own it, since that is the other half of what the estate
// teaches.
func TestSeededLabelsRenderFromTheirRules(t *testing.T) {
	ctx, conn, _ := seededEstate(t)

	for _, tc := range []struct {
		table    string
		place    string
		name     string
		label    string
		platform bool
	}{
		{table: "component", place: "huddle", name: "display-1", label: "Display 1", platform: true},
		{table: "component", place: "boardroom", name: "videobar-1", label: "Video Bar 1", platform: true},
		{table: "component", place: "boardroom", name: "videobar-2", label: "Video Bar 2", platform: true},
		{table: "component", place: "boardroom", name: "display-1", label: "Display 1", platform: true},
		{table: "component", place: "boardroom", name: "display-2", label: "Display 2", platform: true},
		{table: "component", place: "auditorium", name: "display-1", label: "Display 1", platform: true},
		// The unclassified box: the rule can only say "Generic Device 1", so
		// the operator's words win and keep the pen.
		{table: "component", place: "boardroom", name: "device-1", label: "Power Conditioner", platform: false},
		{table: "component", place: "boardroom", name: "dsp", label: "Boardroom DSP", platform: false},
		// The two halves of the divisible room, and the two floors: facts a
		// rule has no way to know.
		{table: "system", place: "boardroom", name: "boardroom", label: "Boardroom A", platform: false},
		{table: "system", place: "boardroom", name: "boardroom-2", label: "Boardroom B", platform: false},
		{table: "location", place: "west", name: "1", label: "Level 2", platform: false},
		{table: "location", place: "hall", name: "1", label: "Level 1", platform: false},
	} {
		placeCol := "location_id"
		if tc.table == "location" {
			placeCol = "parent_id"
		}
		var label string
		var platform bool
		err := conn.QueryRow(ctx, `select coalesce(display_name, ''), display_name_generated from `+tc.table+`
			where name = $1 and `+placeCol+` = (select id from location where name = $2)`,
			tc.name, tc.place).Scan(&label, &platform)
		if err != nil {
			t.Errorf("no %s named %q under %q: %v", tc.table, tc.name, tc.place, err)
			continue
		}
		if label != tc.label {
			t.Errorf("%s %q under %q: label = %q, want %q", tc.table, tc.name, tc.place, label, tc.label)
		}
		if platform != tc.platform {
			t.Errorf("%s %q under %q: display_name_generated = %v, want %v", tc.table, tc.name, tc.place, platform, tc.platform)
		}
	}

	// The locations the shipped rule labels (#657), addressed by name alone
	// because each of these four is estate-unique (the two floors above are not,
	// which is why they are looked up under their parent). This is the half of
	// the demonstration that would otherwise be asserted by nothing: releasing a
	// pin leaves no test behind unless the rendered value is pinned instead.
	for _, tc := range []struct{ name, label string }{
		{"boardroom", "Boardroom"},
		{"auditorium", "Auditorium"},
		{"annex", "Annex"},
		// The two-word one: the rule reads the separator as a space, which is
		// the whole of what `words` does and the only row here that shows it.
		{"media-lab", "Media Lab"},
	} {
		var label string
		var platform bool
		if err := conn.QueryRow(ctx,
			`select coalesce(display_name, ''), display_name_generated from location where name = $1`,
			tc.name).Scan(&label, &platform); err != nil {
			t.Errorf("no location named %q: %v", tc.name, err)
			continue
		}
		if label != tc.label || !platform {
			t.Errorf("location %q: label = %q generated = %v, want %q rendered by the shipped rule", tc.name, label, platform, tc.label)
		}
	}
}

// TestSeededStaffingLandsOnTheRightDevices proves the fixture's references still
// point where they say they do now that the keys and the names have come apart.
// It is the assertion that catches a resolver which zips the fixture onto the
// estate in the wrong order: every name and label would still be right, and the
// wrong panel would be staffing the wrong half of the room.
//
// The staffing is also the estate's health story: boardroom-a holds two mics and
// a display and reads healthy; boardroom-b is one mic short of its quorum of two
// and reads degraded, with the shared bar in both.
func TestSeededStaffingLandsOnTheRightDevices(t *testing.T) {
	ctx, conn, _ := seededEstate(t)

	for _, tc := range []struct {
		system string
		want   map[string]string // role -> the component names filling it, joined
	}{
		{system: "boardroom", want: map[string]string{"room-mic": "videobar-1,videobar-2", "main-display": "display-1"}},
		{system: "boardroom-2", want: map[string]string{"room-mic": "videobar-1", "main-display": "display-2"}},
	} {
		rows, err := conn.Query(ctx, `
			select r.name, c.name
			from system_role_assignment a
			join system s on s.id = a.system_id
			join system_role r on r.id = a.role_id
			join component c on c.id = a.component_id
			where s.name = $1
			order by r.name, c.name`, tc.system)
		if err != nil {
			t.Fatalf("read staffing for %q: %v", tc.system, err)
		}
		got := map[string]string{}
		for rows.Next() {
			var role, comp string
			if err := rows.Scan(&role, &comp); err != nil {
				t.Fatalf("scan staffing: %v", err)
			}
			if got[role] != "" {
				got[role] += ","
			}
			got[role] += comp
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate staffing: %v", err)
		}
		rows.Close()
		for role, want := range tc.want {
			if got[role] != want {
				t.Errorf("system %q role %q is filled by %q, want %q", tc.system, role, got[role], want)
			}
		}
		if len(got) != len(tc.want) {
			t.Errorf("system %q staffing = %v, want %v", tc.system, got, tc.want)
		}
	}

	// The power conditioner is a member of the first half and fills nothing,
	// which is the case an assignment cannot produce.
	var unstaffed int
	if err := conn.QueryRow(ctx, `
		select count(*) from system_member m
		join system s on s.id = m.system_id
		join component c on c.id = m.component_id
		where s.name = 'boardroom' and c.name = 'device-1'
		  and not exists (select 1 from system_role_assignment a
		    where a.system_id = m.system_id and a.component_id = m.component_id)`).Scan(&unstaffed); err != nil {
		t.Fatalf("read the unstaffed member: %v", err)
	}
	if unstaffed != 1 {
		t.Errorf("the power conditioner is a member of boardroom filling no role in %d rows, want 1", unstaffed)
	}
}

// TestTheSameFloorNumberMeansTwoDifferentThings is the friction this slice found
// and chose to keep rather than hide (ADR-0103). A positional name is ALLOCATION
// ORDER, not a real-world designation, and the estate seeds both cases side by
// side on one type: the floor under Innovation Hall is named 1 and is Level 1,
// and the floor under the West Building is also named 1 and is Level 2. The
// second is why the label exists.
func TestTheSameFloorNumberMeansTwoDifferentThings(t *testing.T) {
	ctx, conn, _ := seededEstate(t)

	rows, err := conn.Query(ctx, `
		select p.name, f.name, coalesce(f.display_name, ''), f.name_generated
		from location f join location p on p.id = f.parent_id
		where f.location_type = (select id from location_type where name = 'floor')
		order by p.name`)
	if err != nil {
		t.Fatalf("read floors: %v", err)
	}
	defer rows.Close()
	got := map[string][2]string{}
	for rows.Next() {
		var building, name, label string
		var generated bool
		if err := rows.Scan(&building, &name, &label, &generated); err != nil {
			t.Fatalf("scan floor: %v", err)
		}
		if !generated {
			t.Errorf("the floor under %q was named by hand; a floor is the one seeded location type the platform names", building)
		}
		got[building] = [2]string{name, label}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate floors: %v", err)
	}

	want := map[string][2]string{
		"hall": {"1", "Level 1"},
		"west": {"1", "Level 2"},
	}
	for building, w := range want {
		if got[building] != w {
			t.Errorf("the floor under %q reads (name %q, label %q), want (name %q, label %q)",
				building, got[building][0], got[building][1], w[0], w[1])
		}
	}
	if len(got) != len(want) {
		t.Errorf("seeded floors = %d %v, want %d", len(got), got, len(want))
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
	ctx, conn, gw := seededEstate(t)

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

	var tops int
	if err := conn.QueryRow(ctx, `select count(*) from location where parent_id is null`).Scan(&tops); err != nil {
		t.Fatalf("count tops: %v", err)
	}
	if tops < 2 || tops != wantTops {
		t.Fatalf("unparented locations = %d, want %d (at least 2: the tree is a forest, nothing seeds a root)", tops, wantTops)
	}

	// The two placed components, one per top. Their effective tags are the whole
	// point: same key, different rung, because the location binding stops at its
	// own top's subtree. Both are called display-1, so both are addressed by the
	// id their room resolves them to.
	underHQ := componentIn(t, ctx, conn, gw, "huddle", "display-1")
	underEast := componentIn(t, ctx, conn, gw, "auditorium", "display-1")
	eff, err := gw.EffectiveTags(ctx, "component", []string{underHQ.ID, underEast.ID})
	if err != nil {
		t.Fatalf("effective tags: %v", err)
	}
	if got := eff[underHQ.ID]["environment"]; got != "staging" {
		t.Errorf("huddle display environment = %q, want staging (the west building's location binding wins)", got)
	}
	if got := eff[underEast.ID]["environment"]; got != "prod" {
		t.Errorf("auditorium display environment = %q, want prod (a different top, so only the platform binding reaches it)", got)
	}
}

// seededEstate brings up a database with the boot seed and one devseed Run, and
// hands back the raw connection and the gateway. One Run rather than two: the
// idempotency property has its own test, and every other case here is about what
// the estate IS.
func seededEstate(t *testing.T) (context.Context, *pgx.Conn, storage.Gateway) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("boot seed: %v", err)
	}
	if err := devseed.Run(ctx, gw, ""); err != nil {
		t.Fatalf("devseed run: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return ctx, conn, gw
}

// componentIn resolves a component by its room and its platform-minted name, and
// then reads it back through the gateway by ID. The two steps are the point: a
// bare `display-1` is ambiguous by design in this estate, so the id is the
// reference, exactly as devseed itself resolves one.
func componentIn(t *testing.T, ctx context.Context, conn *pgx.Conn, gw storage.Gateway, room, name string) *storage.Component {
	t.Helper()
	var id string
	if err := conn.QueryRow(ctx, `
		select c.id from component c join location l on l.id = c.location_id
		where l.name = $1 and c.name = $2`, room, name).Scan(&id); err != nil {
		t.Fatalf("resolve component %q in %q: %v", name, room, err)
	}
	c, err := gw.GetComponent(ctx, id, scope.Set{All: true})
	if err != nil {
		t.Fatalf("get component %q in %q: %v", name, room, err)
	}
	return c
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
