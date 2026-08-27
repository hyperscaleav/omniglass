package devseed_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

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

	// Components place a device in the fleet; a component that names a location must
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

// TestFixtureLocationNamesAreUniqueFleetWide is the pure guard on a seed
// footgun the volume rows tripped twice: the seed tolerates an existing
// location by BARE NAME, so two fixture rows sharing a name under different
// parents make the second one silently resolve to the first, and the fleet
// comes up one location short with every count test pointing at the wrong
// place. Names are legally placement-scoped in the model, but this fixture
// keeps them fleet-unique so the lookup cannot be fooled.
func TestFixtureLocationNamesAreUniqueFleetWide(t *testing.T) {
	doc := fixturesDoc(t)
	seen := map[string]string{}
	for _, l := range doc.Locations {
		if prev, ok := seen[l.Name]; ok {
			t.Errorf("location name %q is used by keys %q and %q; the seed resolves existing rows by bare name, so the second silently becomes the first", l.Name, prev, l.Key)
		}
		seen[l.Name] = l.Key
	}
}

// TestFixturesLetThePlatformNameTheFleet is the pure guard on what this fleet
// is FOR: it demonstrates the name generator, so a fixture row must not hand the
// platform a name it was supposed to mint. It is the test that fails the moment
// somebody goes back to hand-writing names, which is the failure that otherwise
// looks like success (every other assertion still passes while the feature is no
// longer exercised at all).
//
// The exceptions are enumerated rather than counted, because each one is a
// separate argument: every LOCATION carries a name, because no shipped
// location_type carries a name rule (a campus, a building, a floor and a room
// each have a real-world name the platform cannot produce, so a nameless create
// is refused outright), and nothing else in the fixture may carry one.
//
// The location half was the other way around for two slices, when `floor` shipped
// positional and the two floors were the fleet's only generated location names.
// ADR-0103 reversed that: a floor's designation is B2, LG, 12A, not an ordinal.
// So the location tier's generator ships DORMANT, demonstrated by nothing in a
// shipped fleet, and this test now asserts the absence rather than papering over
// it by inventing a positional type to keep the demo alive.
func TestFixturesLetThePlatformNameTheFleet(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}

	// Every shipped location type is nominal: a nameless create of one is
	// refused outright, so a fixture row without a name would fail `make dev`.
	nominal := map[string]bool{"campus": true, "building": true, "floor": true, "room": true}
	for _, l := range doc.Locations {
		switch {
		case !nominal[l.Type]:
			t.Errorf("location %q is a %s, which is not a shipped location_type; the fixture references the boot seed by id", l.Key, l.Type)
		case l.Name == "":
			t.Errorf("location %q is a %s and carries no name, which the platform cannot mint (that type has no name rule)", l.Key, l.Type)
		}
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
// same guard. A set label takes the pen from the platform (#682), so a
// fixture that sets one everywhere demonstrates the opposite of the label rules.
// The survivors are pinned by key, each for a stated reason, so setting one on a
// row that could render its own is a deliberate edit here rather than a quiet
// one that nothing notices.
func TestFixturesKeepLabelsOnlyWhereTheOverrideIsThePoint(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}

	// The component with no product: with no classification to read, the
	// component rule can only render "Generic Device 1", so the operator's own
	// words are the only thing that says what the box is. The recording service
	// is pinned for the fleet-wide fact its generic-service classification
	// cannot render: "Service 1" says nothing about what depends on it.
	wantComponentLabels := map[string]bool{"power": true, "recording-service": true}
	for _, c := range doc.Components {
		if (c.Label != "") != wantComponentLabels[c.Key] {
			t.Errorf("component %q label = %q, want set = %v (everything else lets the shipped component rule render)",
				c.Key, c.Label, wantComponentLabels[c.Key])
		}
	}

	// No system, which is the reverse of what this loop asserted for two slices.
	// Both halves of the divisible boardroom were pinned ("Boardroom A" and
	// "Boardroom B") because the shipped system rule read the type alone and
	// rendered "Boardroom" for each of them. It reads the ordinal now (#693), so
	// the rule tells them apart on its own and a pin would be the hand-typed copy
	// of its output this test exists to catch. The rendered pair is pinned
	// instead, in TestSeededLabelsRenderFromTheirRules: releasing a pin leaves
	// nothing behind unless the rendered value is asserted in its place.
	for _, s := range doc.Systems {
		if s.Label != "" {
			t.Errorf("system %q label = %q, want none (the shipped system rule renders it, ordinal and all)", s.Key, s.Label)
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
	//
	// The two floors were pinned here for two slices, because the platform
	// allocated their names (`1`) and the designation was signage the rule could
	// not reach. ADR-0103 reversed that: they are named level-2 and level-1 now,
	// for the designations they actually carry, so the rule renders "Level 2" and
	// "Level 1" and the pins are exactly the hand-typed copies of its own output
	// that this test exists to catch. Released.
	//
	// boardroom, auditorium, annex, the media lab and the two floors carry none,
	// which is what makes the fleet demonstrate the rule instead of masking it.
	wantLocationLabels := map[string]bool{
		"hq": true, "west": true, "east": true, "airport": true,
		"huddle": true, "briefing": true, "hall": true,
		// depot: the rule renders "Depot"; the qualifying word ("Service
		// Depot") is what the site is called and is not in the name.
		"depot": true,
		// harbor: same shape ("Harbor Point" vs the rule's "Harbor").
		"harbor": true,
	}
	for _, l := range doc.Locations {
		if (l.Label != "") != wantLocationLabels[l.Key] {
			t.Errorf("location %q label = %q, want set = %v (everything else lets the shipped location rule render)",
				l.Key, l.Label, wantLocationLabels[l.Key])
		}
	}
}

// TestFixturesFleetIsAForest is a pure check that the example fleet teaches the
// no-root-location rule: the location tree is a forest with more than one
// unparented top, and devices sit under more than one of those tops. With every
// device under a single top, a binding at that top looks like it covers the
// fleet, and the reason the install-wide `platform` tier exists (it is the only
// rung that reaches all of them) is invisible in the console.
func TestFixturesFleetIsAForest(t *testing.T) {
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

// TestRunIdempotent proves devseed.Run lands the example fleet (and the worked
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

	// Counts prove idempotency: the second Run added nothing. The expected
	// number is DERIVED from the fixture rather than written here as a literal.
	// A literal has to be edited every time the example fleet grows, which
	// makes a fixture change look like a test failure and invites bumping the
	// number without reading why it moved. Derived, it asserts something
	// stronger anyway: everything the fixture declares landed, and the second
	// run added none of it twice. It is also the assertion that catches a seed
	// which cannot recognise its own platform-named rows, because that failure
	// doubles the fleet rather than erroring.
	fixtures, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	var locs, humans, grants int
	if err := conn.QueryRow(ctx, `select count(*) from location`).Scan(&locs); err != nil {
		t.Fatalf("count locations: %v", err)
	}
	if locs != len(fixtures.Locations) {
		t.Errorf("locations = %d, want %d (seed not idempotent or incomplete)", locs, len(fixtures.Locations))
	}
	var comps, systems, alarms int
	if err := conn.QueryRow(ctx, `select count(*) from component`).Scan(&comps); err != nil {
		t.Fatalf("count components: %v", err)
	}
	if want := len(fixtures.Components) + 1; comps != want {
		t.Errorf("components = %d, want %d (the fixture devices, all platform-named, plus the operator-named DSP)", comps, want)
	}
	if err := conn.QueryRow(ctx, `select count(*) from system`).Scan(&systems); err != nil {
		t.Fatalf("count systems: %v", err)
	}
	if systems != len(fixtures.Systems) {
		t.Errorf("systems = %d, want %d (seed not idempotent or incomplete)", systems, len(fixtures.Systems))
	}
	// Alarms matter most and were the ones going unchecked: the fixtures
	// section leans entirely on RaiseAlarm's dedup_key to be idempotent, with
	// no read-before-write of its own, and devseed runs on every `make dev`
	// start. An untested dedup would stack one more alarm per restart and drag
	// the seeded verdicts along with it.
	if err := conn.QueryRow(ctx, `select count(*) from alarm`).Scan(&alarms); err != nil {
		t.Fatalf("count alarms: %v", err)
	}
	// The breadth block adds one raised-then-cleared pair (#796) beside the
	// fixture's standing alarms; a second run must add nothing to either.
	if alarms != len(fixtures.Alarms)+1 {
		t.Errorf("alarms = %d, want %d: a second run re-raised instead of finding the standing condition", alarms, len(fixtures.Alarms)+1)
	}
	// Membership and staffing are counted too, because they are where a resolver
	// that zipped the fixture onto the fleet in the wrong order shows up: every
	// name and label would still be right, and the second Run would staff a
	// second device into a role the first Run already filled. The expected
	// membership count is the distinct (system, component) pairs the fixture
	// implies: an assignment creates the membership as a side effect, so a
	// component both listed and staffed in one system is one row, and the
	// shared bar staffed in both halves is two.
	pairs := map[string]bool{}
	for _, m := range fixtures.Members {
		pairs[m.System+"\x00"+m.Component] = true
	}
	for _, ra := range fixtures.RoleAssignments {
		pairs[ra.System+"\x00"+ra.Component] = true
	}
	var members, assignments int
	if err := conn.QueryRow(ctx, `select count(*) from system_member`).Scan(&members); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if members != len(pairs) {
		t.Errorf("system members = %d, want %d (the distinct system-component pairs the fixture implies)", members, len(pairs))
	}
	if err := conn.QueryRow(ctx, `select count(*) from system_role_assignment`).Scan(&assignments); err != nil {
		t.Fatalf("count role assignments: %v", err)
	}
	if assignments != len(fixtures.RoleAssignments) {
		t.Errorf("role assignments = %d, want %d (the fixture's own, unchanged by the second Run)", assignments, len(fixtures.RoleAssignments))
	}

	// A multi-site fleet: three campuses, not one.
	var campuses int
	if err := conn.QueryRow(ctx, `select count(*) from location where location_type = (select id from location_type where name = 'campus')`).Scan(&campuses); err != nil {
		t.Fatalf("count campuses: %v", err)
	}
	if campuses != 4 {
		t.Errorf("campuses = %d, want 4 (hq, east, airport, harbor)", campuses)
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
		select count(*) from product_property where product_id = (select id from product where name = 'boreal-edge-55')`).Scan(&contractLines); err != nil {
		t.Fatalf("count contract lines: %v", err)
	}
	if contractLines != 4 {
		t.Errorf("edge-55 contract lines = %d, want 4 (3 boot-seed + mac-address)", contractLines)
	}
	if err := conn.QueryRow(ctx, `
		select count(*) from component where location_id = (select id from location where name = 'huddle')`).Scan(&huddleComps); err != nil {
		t.Fatalf("count huddle components: %v", err)
	}
	if huddleComps != 3 {
		t.Errorf("huddle room components = %d, want 3 (the display carrying the property overrides, the bar staffing the all-in-one leg, and the idle touch panel)", huddleComps)
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
	// its parent rather than across the fleet.
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
	// component in the fleet an operator named, so it is addressable by that
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
	ifaces, err := gw.ListEndpoints(ctx, all)
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
	byName := map[string]*storage.Endpoint{}
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
	if httpIf.Transport != "http" {
		t.Errorf("http interface type = %q, want http", httpIf.Transport)
	}
	if tcpIf.Transport != "tcp" {
		t.Errorf("tcp interface type = %q, want tcp", tcpIf.Transport)
	}
	for _, it := range []*storage.Endpoint{httpIf, tcpIf} {
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
		for _, it := range []*storage.Endpoint{httpIf, tcpIf} {
			if tasks[i].EndpointID == it.ID {
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
		verdict, err := gw.LatestProperty(ctx, "dsp", "endpoint-reachable", tc.iface)
		if err != nil {
			t.Fatalf("latest verdict %s: %v", tc.iface, err)
		}
		if verdict == nil || verdict.Value != "up" {
			t.Fatalf("seeded %s verdict = %+v, want value up", tc.iface, verdict)
		}
		transitions, err := gw.PropertyTransitions(ctx, "dsp", "endpoint-reachable", tc.iface, 0)
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
// minted, since a bare `display-1` matches three rows in this fleet.
const (
	huddleDisplaySQL = `(select id from component where name = 'display-1'
		and location_id = (select id from location where name = 'huddle'))`
	barSQL = `(select id from component where name = 'videobar-1'
		and location_id = (select id from location where name = 'boardroom-a'))`
)

// TestSeededNamesComeFromTheGenerator is the acceptance this slice exists for.
// It asserts every seeded name BY VALUE, so a fixture that went back to
// hand-writing them fails here on the pen (a typed name clears name_generated
// and stores no ordinal) as well as on the value.
//
// The names are not incidental. `display-1` appears once per room holding a
// display, which is the placement-scoped unique index doing its job; and `boardroom`
// carries no ordinal while storing 1, which is the first-of-its-stem suppression
// (ADR-0101).
//
// No LOCATION is generated any more (ADR-0103), so the location rows here are
// the negative half: they carry the operator's own name and no ordinal, and a
// fixture that quietly went back to letting the platform name a floor fails on
// the pen.
func TestSeededNamesComeFromTheGenerator(t *testing.T) {
	ctx, conn, _ := seededFleet(t)

	for _, tc := range []struct {
		what      string
		table     string
		place     string
		name      string
		ordinal   int
		generated bool
	}{
		// Locations: no shipped type generates, so all thirteen carry the
		// operator's own name. The two floors are the rows that moved.
		{what: "the floor under the west building", table: "location", place: "west", name: "level-2", generated: false},
		{what: "the floor under innovation hall", table: "location", place: "hall", name: "level-1", generated: false},
		{what: "the west building", table: "location", place: "hq", name: "west", generated: false},

		// Components: the stem comes from the product's component_type, the
		// ordinal from the room.
		{what: "the huddle display", table: "component", place: "huddle", name: "display-1", ordinal: 1, generated: true},
		{what: "the video bar (one per room, #802)", table: "component", place: "boardroom-a", name: "videobar-1", ordinal: 1, generated: true},
		{what: "the second ceiling mic", table: "component", place: "boardroom-a", name: "mic-2", ordinal: 2, generated: true},
		{what: "room A's panel", table: "component", place: "boardroom-a", name: "display-1", ordinal: 1, generated: true},
		// Room B's panel is the first display in ITS room: the ordinal resets
		// across the air wall because the bucket is the room.
		{what: "room B's panel", table: "component", place: "boardroom-b", name: "display-1", ordinal: 1, generated: true},
		{what: "the power conditioner", table: "component", place: "boardroom-a", name: "device-1", ordinal: 1, generated: true},
		{what: "the auditorium display", table: "component", place: "auditorium", name: "display-1", ordinal: 1, generated: true},
		{what: "the DSP", table: "component", place: "boardroom-a", name: "dsp", generated: false},

		// Systems: the first of its stem in a bucket carries no ordinal while
		// storing one. The boardroom halves are one bucket each now, so both
		// mint plain `boardroom`; the lab pods share a bucket and show the
		// ordinal.
		{what: "the first boardroom half", table: "system", place: "boardroom-a", name: "boardroom", ordinal: 1, generated: true},
		{what: "the second boardroom half", table: "system", place: "boardroom-b", name: "boardroom", ordinal: 1, generated: true},
		{what: "the first lab pod", table: "system", place: "media-lab", name: "classroom", ordinal: 1, generated: true},
		{what: "the second lab pod", table: "system", place: "media-lab", name: "classroom-2", ordinal: 2, generated: true},
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

	// The whole location chain is the operator's: `boardroom` sits under the
	// floor they called `level-2`, under the building they called `west`. The
	// join is kept (it used to prove a typed name nesting under a minted one)
	// because it is what catches a fixture resolver that reparented the fleet.
	var roomGenerated bool
	var roomOrdinal *int
	if err := conn.QueryRow(ctx, `
		select r.name_generated, r.ordinal
		from location r
		join location f on f.id = r.parent_id
		join location b on b.id = f.parent_id
		where r.name = 'boardroom-a' and f.name = 'level-2' and b.name = 'west'`).Scan(&roomGenerated, &roomOrdinal); err != nil {
		t.Fatalf("read boardroom A under the west building's floor: %v", err)
	}
	if roomGenerated || roomOrdinal != nil {
		t.Errorf("the boardroom reads (generated %v, ordinal %v), want (false, absent): a room's name is ground truth the platform cannot mint", roomGenerated, roomOrdinal)
	}

	// A bare `display-1` is seven rows, one per room that holds a first
	// display, which is the direct proof that the fixture could not have used
	// a generated name as its own identity. The count is derived from the
	// fixture (the rooms holding at least one display-classified device)
	// rather than restated, so the fleet can grow without this reading as a
	// failure.
	roomsWithDisplay := map[string]bool{}
	for _, c := range fixturesDoc(t).Components {
		if strings.HasPrefix(c.Product, "boreal-edge-") && c.Location != "" {
			roomsWithDisplay[c.Location] = true
		}
	}
	var displays int
	if err := conn.QueryRow(ctx, `select count(*) from component where name = 'display-1'`).Scan(&displays); err != nil {
		t.Fatalf("count display-1: %v", err)
	}
	if displays != len(roomsWithDisplay) {
		t.Errorf("components named display-1 = %d, want %d (one per room holding a display; a name is unique within its placement, not across the fleet)", displays, len(roomsWithDisplay))
	}
}

// TestASeededComponentNameAgreesWithItsProductsStem derives the expected stem
// from the catalog rather than restating it: it walks each component's
// product -> component_type chain for the first stem set on it, exactly as the
// generator's resolveTypeFacts does, and checks the name the row actually
// carries was minted from that. A stem edited in the boot seed moves this
// assertion with it, where a hand-written list of names would drift.
func TestASeededComponentNameAgreesWithItsProductsStem(t *testing.T) {
	ctx, conn, _ := seededFleet(t)

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
	fixtures, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	// Every fixture component counts, the deliberately product-free power
	// conditioner included: a component created with no product classifies as
	// the generic-device product, so its stem resolves like any other row's.
	if seen != len(fixtures.Components) {
		t.Errorf("platform-named components with a resolvable stem = %d, want %d (every fixture device)", seen, len(fixtures.Components))
	}
}

// TestSeededLabelsRenderFromTheirRules is the label half of the demonstration.
// A component's label comes from the shipped rule over its resolved type and the
// ordinal the generator allocated, so it reads back what the thing is and which
// one it is without anybody typing it. The rows that DO carry a typed label are
// asserted to still own it, since that is the other half of what the fleet
// teaches.
func TestSeededLabelsRenderFromTheirRules(t *testing.T) {
	ctx, conn, _ := seededFleet(t)

	for _, tc := range []struct {
		table    string
		place    string
		name     string
		label    string
		platform bool
	}{
		{table: "component", place: "huddle", name: "display-1", label: "Display 1", platform: true},
		{table: "component", place: "boardroom-a", name: "videobar-1", label: "Video Bar 1", platform: true},
		{table: "component", place: "boardroom-a", name: "mic-2", label: "Ceiling Microphone 2", platform: true},
		{table: "component", place: "boardroom-a", name: "display-1", label: "Display 1", platform: true},
		// Room B's panel is the first display in ITS room, so it reads
		// display-1 too: the placement-scoped name index doing its job across
		// the air wall.
		{table: "component", place: "boardroom-b", name: "display-1", label: "Display 1", platform: true},
		{table: "component", place: "auditorium", name: "display-1", label: "Display 1", platform: true},
		// The unclassified box: the rule can only say "Generic Device 1", so
		// the operator's words win and keep the pen.
		{table: "component", place: "boardroom-a", name: "device-1", label: "Power Conditioner", platform: false},
		{table: "component", place: "boardroom-a", name: "dsp", label: "Boardroom DSP", platform: false},
		// The two halves of the divisible room are two ROOMS now, so each half
		// is the first `board` system in its own bucket and both mint plain
		// `boardroom` (ADR-0101): same name, two rooms.
		{table: "system", place: "boardroom-a", name: "boardroom", label: "Boardroom", platform: true},
		{table: "system", place: "boardroom-b", name: "boardroom", label: "Boardroom", platform: true},
		// The same-BUCKET same-type siblings live in the media lab: the shipped
		// rule reads the type AND the ordinal (#693), so the first pod reads
		// "Classroom" beside its `classroom` name and the second "Classroom 2"
		// beside `classroom-2`.
		{table: "system", place: "media-lab", name: "classroom", label: "Classroom", platform: true},
		{table: "system", place: "media-lab", name: "classroom-2", label: "Classroom 2", platform: true},
		// The two floors, which used to be here as PINS over a generated name
		// (`1` labelled Level 2). They are named for their designations now, so
		// the rule renders those designations and the platform holds the pen.
		// Looked up under their parent because a floor's name is unique within
		// its building rather than across the fleet.
		{table: "location", place: "west", name: "level-2", label: "Level 2", platform: true},
		{table: "location", place: "hall", name: "level-1", label: "Level 1", platform: true},
	} {
		placeCol := "location_id"
		if tc.table == "location" {
			placeCol = "parent_id"
		}
		var label string
		var platform bool
		err := conn.QueryRow(ctx, `select label, label_generated from `+tc.table+`
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
			t.Errorf("%s %q under %q: label_generated = %v, want %v", tc.table, tc.name, tc.place, platform, tc.platform)
		}
	}

	// The locations the shipped rule labels (#657), addressed by name alone
	// because each of these is fleet-unique (the two floors above are looked up
	// under their parent instead, since a floor's name is unique only within its
	// building). This is the half of the demonstration that would otherwise be
	// asserted by nothing: releasing a pin leaves no test behind unless the
	// rendered value is pinned instead.
	for _, tc := range []struct{ name, label string }{
		{"auditorium", "Auditorium"},
		{"annex", "Annex"},
		// The two-word ones: the rule reads the separator as a space, which is
		// the whole of what `words` does. The boardroom halves are the
		// designation case: the name carries the signage (ADR-0103) and the
		// rule titles it.
		{"boardroom-a", "Boardroom A"},
		{"boardroom-b", "Boardroom B"},
		{"media-lab", "Media Lab"},
	} {
		var label string
		var platform bool
		if err := conn.QueryRow(ctx,
			`select label, label_generated from location where name = $1`,
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
// fleet in the wrong order: every name and label would still be right, and the
// wrong panel would be staffing the wrong half of the room.
//
// The staffing is also the fleet's health story: both halves are fully
// deployed and read healthy (#785), with the divisible pair's ONE audio rack
// (dsp-1, amp-1, physically racked in A) counting in both halves (#802): the
// shared-component case, standing where the impossible shared bar used to.
func TestSeededStaffingLandsOnTheRightDevices(t *testing.T) {
	ctx, conn, _ := seededFleet(t)

	for _, tc := range []struct {
		room string
		want map[string]string // role -> the component names filling it, joined
	}{
		// Both halves mint `boardroom` in their own rooms, so the room is the
		// address here. The shared rack staffs dsp and amplifier in BOTH,
		// from its physical home in A, so both halves name dsp-1 and amp-1.
		{room: "boardroom-a", want: map[string]string{
			"video-bar": "videobar-1", "main-display": "display-1,display-2", "room-mic": "mic-1,mic-2",
			"touch-control": "panel-1", "scheduling-panel": "scheduler-1", "dsp": "dsp-1", "amplifier": "amp-1",
		}},
		// B's own devices are the first OF THEIR OWN ROOM (the
		// placement-scoped index at work, not duplicates), while the rack's
		// names stay the ones it minted in A.
		{room: "boardroom-b", want: map[string]string{
			"video-bar": "videobar-1", "main-display": "display-1,display-2", "room-mic": "mic-1,mic-2",
			"touch-control": "panel-1", "scheduling-panel": "scheduler-1", "dsp": "dsp-1", "amplifier": "amp-1",
		}},
	} {
		rows, err := conn.Query(ctx, `
			select r.name, c.name
			from system_role_assignment a
			join system s on s.id = a.system_id
			join location l on l.id = s.location_id
			join system_role r on r.id = a.role_id
			join component c on c.id = a.component_id
			where l.name = $1
			order by r.name, c.name`, tc.room)
		if err != nil {
			t.Fatalf("read staffing for %q: %v", tc.room, err)
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
				t.Errorf("the system in %q: role %q is filled by %q, want %q", tc.room, role, got[role], want)
			}
		}
		if len(got) != len(tc.want) {
			t.Errorf("the system in %q: staffing = %v, want %v", tc.room, got, tc.want)
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

// TestAFloorIsNamedForItsDesignation is what ADR-0103's worked example became
// when the architect reversed it. The fleet used to ship two floors both named
// `1`, one labelled Level 1 and one Level 2, to teach that a positional name is
// ALLOCATION ORDER rather than a designation. The sharper reading is that a
// floor's designation is not an integer at all (B2, LG, G, M, 12A), so an
// ordinal is the wrong kind of value for it, not merely an imprecise one.
//
// So a floor is nominal: the operator names it for the designation on the
// signage, the shipped location rule renders that designation as the label, and
// the two AGREE because they are one fact rather than two. This test is the
// statement of that, and it fails if a floor ever goes back to being minted.
func TestAFloorIsNamedForItsDesignation(t *testing.T) {
	ctx, conn, _ := seededFleet(t)

	rows, err := conn.Query(ctx, `
		select p.name, f.name, coalesce(f.label, ''), f.name_generated, f.ordinal, f.label_generated
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
		var generated, labelGenerated bool
		var ordinal *int
		if err := rows.Scan(&building, &name, &label, &generated, &ordinal, &labelGenerated); err != nil {
			t.Fatalf("scan floor: %v", err)
		}
		if generated || ordinal != nil {
			t.Errorf("the floor under %q reads (generated %v, ordinal %v), want (false, absent): a floor is named by the operator, and the platform owns no number for it",
				building, generated, ordinal)
		}
		if !labelGenerated {
			t.Errorf("the floor under %q holds the label pen; the shipped rule renders its designation from its name, so a pin would be a hand-typed copy of that output", building)
		}
		got[building+"/"+name] = [2]string{name, label}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate floors: %v", err)
	}

	// Name and label say the same thing, which is the whole change: the name is
	// the designation in kebab, the label is what the rule renders from it.
	// The two floors that carry the argument, keyed by building and name
	// since a building holds several floors now (the volume rows add more, all
	// held to the same rule by the loop above).
	want := map[string][2]string{
		"hall/level-1": {"level-1", "Level 1"},
		"west/level-2": {"level-2", "Level 2"},
	}
	for key, w := range want {
		if got[key] != w {
			t.Errorf("the floor %q reads (name %q, label %q), want (name %q, label %q)",
				key, got[key][0], got[key][1], w[0], w[1])
		}
	}
}

// TestSeededFleetTeachesPlatformReach carries the forest into the database and
// proves what it is there to teach: the seeded fleet has as many unparented tops
// as the fixture declares (no row stands in for a root), and the platform binding
// is the only rung that reaches all of them. The component under the HQ subtree
// reads that subtree's location override, while the component under a different
// top falls back to the install-wide `platform` value, which is the case a
// synthetic root location would have hidden.
func TestSeededFleetTeachesPlatformReach(t *testing.T) {
	ctx, conn, gw := seededFleet(t)

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

// seededFleet brings up a database with the boot seed and one devseed Run, and
// hands back the raw connection and the gateway. One Run rather than two: the
// idempotency property has its own test, and every other case here is about what
// the fleet IS.
func seededFleet(t *testing.T) (context.Context, *pgx.Conn, storage.Gateway) {
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
// bare `display-1` is ambiguous by design in this fleet, so the id is the
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

// TestFixturesMakeAnFleetWorthLookingAt guards what the fleet canvas (#630)
// needs from the dev fleet, which is a different bar from what the earlier
// fixtures were built to clear. Those existed to teach one thing each (a shared
// component, a member with no role, the platform-reach lesson) and a handful of
// rows says all of that. A canvas is judged on whether an operator can read a
// real fleet in it, and a dozen dots in one room cannot answer that either way.
//
// These are fixture-shape assertions, deliberately pure: they parse the YAML and
// reason about it, so the whole guard runs with no Postgres and cannot rot into a
// slow test nobody runs.
func TestFixturesMakeAnFleetWorthLookingAt(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}

	parentOf := map[string]string{}
	typeOf := map[string]string{}
	for _, l := range doc.Locations {
		parentOf[l.Key] = l.Parent
		typeOf[l.Key] = l.Type
	}
	depth := func(name string) int {
		n := 1
		for parentOf[name] != "" {
			name = parentOf[name]
			n++
		}
		return n
	}
	topOf := func(name string) string {
		for parentOf[name] != "" {
			name = parentOf[name]
		}
		return name
	}

	// No two roots alike. The location tree is arbitrary-depth by design and
	// nothing in the model requires a building or a floor, but a seed whose
	// every root is campus > building > floor > room quietly teaches the
	// opposite and lets a fixed-ladder assumption ship green.
	rootTypes := map[string]bool{}
	for _, l := range doc.Locations {
		if l.Parent == "" {
			rootTypes[l.Type] = true
		}
	}
	if len(rootTypes) < 2 {
		t.Errorf("every root is the same type %v, so the seed teaches a fixed ladder the model does not have", rootTypes)
	}
	depths := map[int]bool{}
	for name := range parentOf {
		depths[depth(name)] = true
	}
	if len(depths) < 3 {
		t.Errorf("leaf depths %v, want at least three distinct depths so the canvas is read against an uneven fleet", depths)
	}

	// Enough to paint. A dot grid is the fleet zoom's whole claim, and it
	// cannot be judged on a handful of squares in one room.
	if len(doc.Systems) < 6 {
		t.Errorf("systems = %d, want at least 6 spread across the fleet", len(doc.Systems))
	}
	if len(doc.Components) < 20 {
		t.Errorf("components = %d, want at least 20 so a cluster reads as a cluster", len(doc.Components))
	}
	placed := map[string]bool{}
	for _, s := range doc.Systems {
		if s.Location != "" {
			placed[topOf(s.Location)] = true
		}
	}
	if len(placed) < 3 {
		t.Errorf("systems occupy %d roots, want at least 3 so the fleet zoom has bands to compare", len(placed))
	}

	// A hole: a leaf nobody has put a system in. The canvas draws these, and
	// naming a gap is half of what it is for.
	hasChild := map[string]bool{}
	for _, l := range doc.Locations {
		if l.Parent != "" {
			hasChild[l.Parent] = true
		}
	}
	withSystem := map[string]bool{}
	for _, s := range doc.Systems {
		withSystem[s.Location] = true
	}
	holes := 0
	for _, l := range doc.Locations {
		if !hasChild[l.Key] && !withSystem[l.Key] {
			holes++
		}
	}
	if holes == 0 {
		t.Error("no leaf is without a system, so the canvas has no hole to draw")
	}

	// The ruling: EVERY system carries its components (broken is fine, absent
	// is not), so no system with a standard is left without a single
	// assignment. The one provisioning system (briefing-av, short its
	// scheduling panel) is short-staffed, never bare, and it alone keeps
	// #631's incomplete verdict on screen.
	for _, s := range doc.Systems {
		if s.Standard == "" {
			continue
		}
		any := false
		for _, a := range doc.RoleAssignments {
			if a.System == s.Key {
				any = true
				break
			}
		}
		if !any {
			t.Errorf("system %s is entirely unstaffed; the ruling is every system carries its components, with briefing-av the one short-staffed exception", s.Key)
		}
	}

	// A component shared across two DIFFERENT roots. Sharing inside one room
	// already had a fixture; the fleet zoom's ring-and-ghost rule is about a
	// box two bands apart depending on each other, which is the case an
	// operator cannot see any other way.
	rootsOf := map[string]map[string]bool{}
	systemRoot := map[string]string{}
	for _, s := range doc.Systems {
		systemRoot[s.Key] = topOf(s.Location)
	}
	note := func(component, system string) {
		if rootsOf[component] == nil {
			rootsOf[component] = map[string]bool{}
		}
		rootsOf[component][systemRoot[system]] = true
	}
	for _, m := range doc.Members {
		note(m.Component, m.System)
	}
	for _, a := range doc.RoleAssignments {
		note(a.Component, a.System)
	}
	crossRoot := false
	for _, roots := range rootsOf {
		if len(roots) > 1 {
			crossRoot = true
		}
	}
	if !crossRoot {
		t.Error("no component is shared across two roots, so the fleet zoom's ghost rule is never exercised")
	}
}

// TestSeededFleetShowsEveryVerdict traces the example fleet through the real
// health rollup rather than trusting the fixture's shape. The fixture guard
// above proves a system was left unstaffed; only this proves that the platform
// then reads it as INCOMPLETE, which is the claim the fleet canvas is coloured
// by and is a different fact from "no assignment row exists".
//
// It is also the regression that catches a seed drifting into monochrome. The
// canvas's whole argument is that an operator can tell a commissioning gap from
// an outage at a glance, and a dev fleet where every room reads healthy
// demonstrates nothing and hides a broken rollup behind a green screen.
func TestSeededFleetShowsEveryVerdict(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("boot seed: %v", err)
	}
	if err := devseed.Run(ctx, gw, ""); err != nil {
		t.Fatalf("devseed: %v", err)
	}

	all := scope.Set{All: true}
	verdictOf := func(system string) string {
		t.Helper()
		rep, err := gw.SystemHealth(ctx, system, 0, all)
		if err != nil {
			t.Fatalf("system health %s: %v", system, err)
		}
		return rep.Verdict
	}

	// Systems are platform-named, so the fixture key is not an address, and a
	// minted name is not one either: both boardroom halves mint plain
	// `boardroom` in their own rooms, so the bare name is ambiguous
	// fleet-wide. A system is resolved to its uuid through the room that
	// holds it instead, and a room may hold more than one (the lab's two
	// pods), so the verdict is asserted over every system in the room.
	conn2, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn2.Close(ctx)
	systemsIn := func(room string) []string {
		t.Helper()
		rows, err := conn2.Query(ctx,
			`select s.id from system s join location l on s.location_id = l.id where l.name = $1 order by s.name`,
			room)
		if err != nil {
			t.Fatalf("resolve systems in room %q: %v", room, err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan system id in room %q: %v", room, err)
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			t.Fatalf("no system in room %q", room)
		}
		return ids
	}

	for _, tc := range []struct{ room, want, why string }{
		{"media-lab", "healthy", "both pods fully staffed and quiet; the ordinal demo lives in their names, not in a gap"},
		{"boardroom-a", "healthy", "fully staffed, the shared audio rack counted where it physically sits"},
		{"boardroom-b", "healthy", "fully staffed: its own bar, displays, mics, and panels, plus the divisible pair's shared DSP and amplifier (#802)"},
		{"briefing", "incomplete", "deployed except the scheduling panel nobody has installed: short one commissioning item"},
		{"auditorium", "degraded", "fully staffed, but a critical alarm took the DSP down: a real failure in the middle of the chain, not a gap"},
		{"bay-1", "healthy", "panel and player staffed and quiet"},
		{"huddle", "healthy", "built all-in-one, so the component-build alternate it never staffed does not impair it"},
	} {
		for _, id := range systemsIn(tc.room) {
			if got := verdictOf(id); got != tc.want {
				t.Errorf("a system in %s reads %q, want %q: %s", tc.room, got, tc.want, tc.why)
			}
		}
	}

	// The fleet must not be monochrome: an operator judging the canvas needs
	// more than one colour on it.
	seen := map[string]bool{}
	for _, room := range []string{"media-lab", "boardroom-a", "boardroom-b", "auditorium", "huddle", "bay-1", "briefing"} {
		for _, id := range systemsIn(room) {
			seen[verdictOf(id)] = true
		}
	}
	if len(seen) < 3 {
		t.Errorf("the seeded fleet shows %d distinct verdicts (%v), want at least 3 so the canvas is judged against a real spread", len(seen), seen)
	}
}

// fixturesDoc parses the embedded fixtures, failing the test on error, so a
// derived expectation reads as one call.
func fixturesDoc(t *testing.T) devseed.Doc {
	t.Helper()
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	return doc
}

// fixtureComponentNames is the fixture's own component list, for counting only
// the rows the fixture declares: seedReachability creates one of its own beside
// them, and a bare count(*) would fold the two together and stop meaning
// "everything declared landed exactly once".
func fixtureComponentNames(doc devseed.Doc) []string {
	out := make([]string, 0, len(doc.Components))
	for _, c := range doc.Components {
		out = append(out, c.Name)
	}
	return out
}

// The deployed-fleet invariant (#785): a real estate is mostly rooms whose
// every role is staffed to quorum; commissioning is the exception that
// carries the teaching, not the norm. This pins the ruling so a future
// volume block cannot quietly regress the fleet back to a wall of
// incomplete: every system in the fixture is staffed to its standard's
// quorum, with exactly one named provisioning exception.
func TestFixtureFleetReadsDeployed(t *testing.T) {
	doc := fixturesDoc(t)
	teaching := map[string]string{
		"briefing-av": "the ONE provisioning system: deployed except its scheduling panel",
	}
	staffed := map[string]map[string]int{}
	for _, ra := range doc.RoleAssignments {
		if staffed[ra.System] == nil {
			staffed[ra.System] = map[string]int{}
		}
		staffed[ra.System][ra.Role]++
	}
	// The archetype quorums (#796): what "deployed" means per standard.
	mr := map[string]int{"video-bar": 1, "main-display": 1, "touch-control": 1, "scheduling-panel": 1}
	quorums := map[string]map[string]int{
		"mr55": mr, "mr65": mr, "mr75": mr, "mr86": mr, "mr98": mr,
		"divisible-conference": {"video-bar": 1, "main-display": 2, "room-mic": 2, "dsp": 1, "amplifier": 1, "touch-control": 1, "scheduling-panel": 1},
		"classroom":            {"class-display": 1, "instructor-mic": 1, "touch-control": 1},
		"training-room": {
			"front-display": 2, "ceiling-mic": 2, "presenter-mic": 1, "podium-mic": 1,
			"dsp": 1, "amplifier": 1, "speakers": 4, "video-switcher": 1, "display-endpoint": 2,
			"control-system": 1, "touch-control": 1, "scheduling-panel": 1, "people-counter": 1, "camera": 2,
		},
		"auditorium": {
			"projection": 1, "confidence-display": 1, "stage-mic": 2, "podium-mic": 1,
			"dsp": 1, "amplifier": 2, "speakers": 4, "video-switcher": 1,
			"control-system": 1, "touch-control": 1, "camera": 2,
		},
		"ds55": {"panel": 1, "player": 1},
		"ds75": {"panel": 1, "player": 1},
	}
	for _, s := range doc.Systems {
		want, known := quorums[s.Standard]
		if !known {
			continue
		}
		if _, ok := teaching[s.Key]; ok {
			continue
		}
		got := staffed[s.Key]
		for role, q := range want {
			if got[role] < q {
				t.Errorf("system %q (%s) staffs %s %d/%d; a deployed room fills every role (#785), and only the named teaching cases stay short", s.Key, s.Standard, role, got[role], q)
			}
		}
	}

	// The archetype coherence guard (#796): a space's standard matches its
	// type, so no auditorium ever renders as a meeting room again.
	// A size-serialized family pairs by prefix: any mrNN is a meeting room,
	// any dsNN a digital sign (#802).
	pair := map[string]string{"meeting": "mr", "conference": "mr", "board": "divisible-conference", "training": "training-room", "huddle": "huddle-room", "class": "classroom", "auditorium": "auditorium", "sign": "ds"}
	for _, s := range doc.Systems {
		if want, ok := pair[s.SystemType]; ok && !strings.HasPrefix(s.Standard, want) {
			t.Errorf("system %q is a %q on the %q standard; the archetype pairs %q*", s.Key, s.SystemType, s.Standard, want)
		}
	}
}

// The KPI contract (#790): the standards declare the room series, the huddle
// carries live samples, and every other conforming room reads the contract
// unsampled. Pinned here because the system zoom's tiles render exactly this
// read and nothing else.
func TestSeededStandardsDeclareTheRoomKPIs(t *testing.T) {
	ctx, conn, gw := seededFleet(t)
	all := scope.Set{All: true}

	var huddleID string
	if err := conn.QueryRow(ctx, `
		select s.id::text from system s join location l on l.id = s.location_id
		 where l.name = 'huddle'`).Scan(&huddleID); err != nil {
		t.Fatalf("resolve the huddle system: %v", err)
	}
	eff, err := gw.EffectiveMetrics(ctx, "system", huddleID, all)
	if err != nil {
		t.Fatalf("effective system metrics: %v", err)
	}
	got := map[string]bool{}
	for _, m := range eff {
		got[m.MetricTypeName] = m.IsSampled
	}
	for name, wantSampled := range map[string]bool{"room-temperature": true, "occupancy-count": true} {
		sampled, ok := got[name]
		if !ok {
			t.Errorf("the huddle system's effective metrics lack %q; the standard should declare it", name)
			continue
		}
		if sampled != wantSampled {
			t.Errorf("%q sampled = %v, want %v", name, sampled, wantSampled)
		}
	}

	// Every occupied-room archetype carries the contract; the signage bays do
	// not (a display board measures neither occupancy nor temperature).
	var bayID string
	if err := conn.QueryRow(ctx, `
		select s.id::text from system s join location l on l.id = s.location_id
		 where l.name = 'bay-1'`).Scan(&bayID); err != nil {
		t.Fatalf("resolve the bay system: %v", err)
	}
	eff, err = gw.EffectiveMetrics(ctx, "system", bayID, all)
	if err != nil {
		t.Fatalf("effective bay metrics: %v", err)
	}
	for _, m := range eff {
		if m.MetricTypeName == "room-temperature" {
			t.Errorf("the signage bay declares room-temperature; a display board measures neither room metric (#796)")
		}
	}
}
