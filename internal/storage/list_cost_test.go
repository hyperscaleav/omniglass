package storage_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
)

// The round-trip cost of the gateway's LIST paths (#650), pinned at an exact
// number rather than timed. The Gateway's dominant cost is round trips to
// Postgres and the regression that hurts at estate scale is the N+1: fifteen
// thousand components paying two or three queries each. A count is
// deterministic, needs no baseline, and fails with a number that names the
// defect; a wall-clock measurement on a laptop has variance that swamps
// anything short of a catastrophe.
//
// The seam is the POOL, not a wrapped argument. Every exported List takes a
// scope.Set and queries p.pool directly, so a counting querier handed to one
// would be ignored and would report a flat, fictional number forever
// (storagetest/querycount's doc comment opens on that hazard). NewCountingDB
// hangs a pgx query tracer on the pool's connection config, which sees every
// statement whichever gateway method issued it.
//
// #643's own assertion, on the attach layer that three of these paths share,
// stays where it is (path_batch_test.go, over the internal querier those
// functions take as a parameter). These measure the whole read: the list query
// plus whatever it attaches.

// reading is one measured call: what the list returned, what it cost, and the
// statements it issued (so a failure names them rather than only counting them,
// which is the difference between "24 queries" and a visible N+1).
type reading struct {
	rows  int
	n     int
	stmts []string
}

// measure resets the counter, runs one list, and reports the cost. list returns
// the number of rows the call produced, which assertFlatCost needs to prove the
// two pages actually differed in size.
func measure(t *testing.T, c *querycount.Counter, list func() (int, error)) reading {
	t.Helper()
	c.Reset()
	rows, err := list()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return reading{rows: rows, n: c.N(), stmts: c.Summary()}
}

// assertFlatCost holds a read to BOTH properties, because neither alone has
// teeth.
//
// Flatness alone passes a read that is constant but wasteful, thirty statements
// for any page; the ceiling catches that. The ceiling alone is only ever as tight
// as its calibration, and a generous one passes a read that is still growing
// under it; flatness catches that. And both together measure nothing at all if
// the two pages are secretly the same size, which is exactly how #643's review
// found a test that could not fail, so the pages are checked to have differed
// before either property is asserted at all.
func assertFlatCost(t *testing.T, small, large reading, ceiling int) {
	t.Helper()
	if large.rows <= small.rows {
		t.Fatalf("the wide page returned %d rows and the small one %d: the dimension this claims flatness in never varied, so nothing was measured",
			large.rows, small.rows)
	}
	if small.n != large.n {
		t.Errorf("cost %d statements for %d rows and %d for %d rows: the read does per-row work\n  small page: %q\n  wide page:  %q",
			small.n, small.rows, large.n, large.rows, small.stmts, large.stmts)
	}
	if large.n > ceiling {
		t.Errorf("cost %d statements for %d rows, want at most %d\n  %q", large.n, large.rows, ceiling, large.stmts)
	}
}

// countingEstate is a seeded gateway whose pool counts, one per subtest: each
// case measures a page of one and then a page of twenty-one, so it needs a
// database no other case has already filled.
func countingEstate(t *testing.T) (storage.Gateway, *querycount.Counter) {
	t.Helper()
	gw, counter := storagetest.NewCountingDB(t)
	if err := seed.Run(context.Background(), gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	counter.Reset()
	return gw, counter
}

// widePage is the larger page size every case grows to, and rooms is how many
// distinct places its rows are spread across. The spread is not decoration: the
// address walk's third query walks the DISTINCT plane-root locations, so a page
// whose rows all sit in one room holds that set at size one and a per-root loop
// would look exactly as flat as a batch (#643's review finding, in full).
const (
	widePage = 20
	rooms    = 5
)

// mustRooms creates the root building and its rooms, the placement every tree
// case spreads its rows across.
func mustRooms(t *testing.T, gw storage.Gateway) []string {
	t.Helper()
	ctx := context.Background()
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "boi", LocationType: "building"}, all); err != nil {
		t.Fatalf("create building: %v", err)
	}
	names := make([]string, 0, rooms)
	for i := range rooms {
		name := fmt.Sprintf("room-%d", i)
		if _, err := gw.CreateLocation(ctx, "",
			storage.LocationSpec{Name: name, LocationType: "room", ParentName: strptr("boi")}, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		names = append(names, name)
	}
	return names
}

// TestListComponentsCostIsFlatInPageSize: the component LIST is the scoped-list
// query plus the batch address attach, and must cost the same for twenty-one
// components as for one.
//
// The one-row page is PLACED, deliberately. The attach's third query walks the
// plane roots' locations and skips itself when the page has no placed row at
// all, so a small page of one unplaced component would take a different branch
// and the comparison would be measuring branches rather than page sizes.
func TestListComponentsCostIsFlatInPageSize(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()
	roomNames := mustRooms(t, gw)

	list := func() (int, error) {
		cs, err := gw.ListComponents(ctx, all)
		return len(cs), err
	}
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "dsp-0", LocationName: strptr(roomNames[0])}, all)
	small := measure(t, counter, list)

	// Twenty more, spread across the rooms, and every fifth one nested under
	// the component before it so the page has real depth as well as spread.
	for i := 1; i <= widePage; i++ {
		spec := storage.ComponentSpec{Name: fmt.Sprintf("dsp-%d", i), LocationName: strptr(roomNames[i%rooms])}
		if i%5 == 0 {
			spec = storage.ComponentSpec{Name: fmt.Sprintf("card-%d", i), ParentName: strptr(fmt.Sprintf("dsp-%d", i-1))}
		}
		mustCreateComponent(t, gw, spec, all)
	}
	large := measure(t, counter, list)

	// One scoped-list query, then the address walk's three: the page's own
	// chains, its plane roots, and those roots' location chains.
	assertFlatCost(t, small, large, 4)
}

// TestListSystemsCostIsFlatInPageSize: the same shape for the system plane,
// which shares the scoped-list and attach machinery but its own accessor.
func TestListSystemsCostIsFlatInPageSize(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()
	roomNames := mustRooms(t, gw)

	list := func() (int, error) {
		ss, err := gw.ListSystems(ctx, all)
		return len(ss), err
	}
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "av-0", LocationName: strptr(roomNames[0])}, all)
	small := measure(t, counter, list)

	for i := 1; i <= widePage; i++ {
		spec := storage.SystemSpec{Name: fmt.Sprintf("av-%d", i), LocationName: strptr(roomNames[i%rooms])}
		if i%5 == 0 {
			spec = storage.SystemSpec{Name: fmt.Sprintf("sub-%d", i), ParentName: strptr(fmt.Sprintf("av-%d", i-1))}
		}
		mustCreateSystem(t, gw, spec, all)
	}
	large := measure(t, counter, list)

	assertFlatCost(t, small, large, 4)
}

// TestListLocationsCostIsFlatInPageSize: a location's address is its own
// ancestor chain with no plane to cross, so its attach is one query rather than
// three, and the whole read is two.
func TestListLocationsCostIsFlatInPageSize(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()

	list := func() (int, error) {
		ls, err := gw.ListLocations(ctx, all)
		return len(ls), err
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "boi", LocationType: "building"}, all); err != nil {
		t.Fatalf("create building: %v", err)
	}
	small := measure(t, counter, list)

	// A tree, not a flat list: floors under the building and rooms under the
	// floors, so the page varies in DEPTH (what the recursive walk grows along)
	// as well as in size.
	for f := range rooms {
		floor := fmt.Sprintf("floor-%d", f)
		if _, err := gw.CreateLocation(ctx, "",
			storage.LocationSpec{Name: floor, LocationType: "floor", ParentName: strptr("boi")}, all); err != nil {
			t.Fatalf("create %s: %v", floor, err)
		}
		for r := range widePage / rooms {
			room := fmt.Sprintf("room-%d-%d", f, r)
			if _, err := gw.CreateLocation(ctx, "",
				storage.LocationSpec{Name: room, LocationType: "room", ParentName: strptr(floor)}, all); err != nil {
				t.Fatalf("create %s: %v", room, err)
			}
		}
	}
	large := measure(t, counter, list)

	assertFlatCost(t, small, large, 2)
}

// TestListMembersCostIsFlatInMemberCount: a system's membership list resolves
// the system once and then reads its members in one join, so twenty-one members
// cost what one does. The members are components spread across several rooms, so
// a per-member address walk (the shape the scoped list once had) would grow.
func TestListMembersCostIsFlatInMemberCount(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()
	roomNames := mustRooms(t, gw)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "av", LocationName: strptr(roomNames[0])}, all)

	join := func(i int) {
		t.Helper()
		name := fmt.Sprintf("dsp-%d", i)
		mustCreateComponent(t, gw, storage.ComponentSpec{Name: name, LocationName: strptr(roomNames[i%rooms])}, all)
		if err := gw.AddMember(ctx, "", "av", name, all); err != nil {
			t.Fatalf("add member %s: %v", name, err)
		}
	}
	list := func() (int, error) {
		ms, err := gw.ListMembers(ctx, "av", all)
		return len(ms), err
	}

	join(0)
	small := measure(t, counter, list)
	for i := 1; i <= widePage; i++ {
		join(i)
	}
	large := measure(t, counter, list)

	// Resolving the system by name costs its own load plus the address attach a
	// single GET pays (three), then one join for the whole membership.
	assertFlatCost(t, small, large, 5)
}

// TestListInterfacesCostIsFlatInPageSize: the interface list is one query, a
// recursive component-subtree walk joined onto interface, however many
// interfaces on however many components.
func TestListInterfacesCostIsFlatInPageSize(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()
	roomNames := mustRooms(t, gw)

	// One interface per component, on components spread across the rooms: the
	// owning component is the dimension a per-interface lookup would grow along.
	add := func(i int) {
		t.Helper()
		name := fmt.Sprintf("dsp-%d", i)
		mustCreateComponent(t, gw, storage.ComponentSpec{Name: name, LocationName: strptr(roomNames[i%rooms])}, all)
		if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr(name)}, all); err != nil {
			t.Fatalf("create interface on %s: %v", name, err)
		}
	}
	list := func() (int, error) {
		is, err := gw.ListInterfaces(ctx, all)
		return len(is), err
	}

	add(0)
	small := measure(t, counter, list)
	for i := 1; i <= widePage; i++ {
		add(i)
	}
	large := measure(t, counter, list)

	assertFlatCost(t, small, large, 1)
}

// TestListAlarmsCostIsFlatInAlarmCount: resolving the component by name, then
// one query for its alarms, whatever the depth of its history.
func TestListAlarmsCostIsFlatInAlarmCount(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()
	roomNames := mustRooms(t, gw)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "dsp", LocationName: strptr(roomNames[0])}, all)

	raise := func(i int) {
		t.Helper()
		if _, err := gw.RaiseAlarm(ctx, "", "dsp", storage.AlarmSpec{
			Severity: "warning",
			Message:  fmt.Sprintf("probe %d", i),
			DedupKey: fmt.Sprintf("probe-%d", i),
		}); err != nil {
			t.Fatalf("raise alarm %d: %v", i, err)
		}
	}
	list := func() (int, error) {
		as, err := gw.ListAlarms(ctx, "dsp", true)
		return len(as), err
	}

	raise(0)
	small := measure(t, counter, list)
	for i := 1; i <= widePage; i++ {
		raise(i)
	}
	large := measure(t, counter, list)

	// The component reference resolve, then the alarm read.
	assertFlatCost(t, small, large, 2)
}

// TestListTagsCostIsFlatInPageSize: the key vocabulary is one query, not one per
// key (its allowed values live on the row, not in a child table, which is
// exactly the design decision a per-key read would undo).
func TestListTagsCostIsFlatInPageSize(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()

	mint := func(i int) {
		t.Helper()
		if _, err := gw.CreateTag(ctx, "", storage.TagSpec{
			Name:          fmt.Sprintf("probe-key-%d", i),
			AppliesTo:     []string{"component"},
			AllowedValues: []string{"a", "b"},
		}, all); err != nil {
			t.Fatalf("create tag %d: %v", i, err)
		}
	}
	list := func() (int, error) {
		ts, err := gw.ListTags(ctx)
		return len(ts), err
	}

	mint(0)
	small := measure(t, counter, list)
	for i := 1; i <= widePage; i++ {
		mint(i)
	}
	large := measure(t, counter, list)

	assertFlatCost(t, small, large, 1)
}

// TestListRolesCostIsFlatInPageSize: the role index is read whole on every
// permission refresh, and a role carries its permissions and inheritance as
// array columns, so the read is one query however many roles the estate defines.
func TestListRolesCostIsFlatInPageSize(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()

	list := func() (int, error) {
		rs, err := gw.ListRoles(ctx)
		return len(rs), err
	}
	// The small page is the seeded official set; the wide page adds twenty
	// operator roles on top of it.
	small := measure(t, counter, list)
	for i := range widePage {
		if err := gw.UpsertRole(ctx, storage.Role{
			Name:        fmt.Sprintf("probe-role-%d", i),
			Permissions: []string{"component:read", "system:read"},
		}); err != nil {
			t.Fatalf("upsert role %d: %v", i, err)
		}
	}
	large := measure(t, counter, list)

	assertFlatCost(t, small, large, 1)
}

// TestListPrincipalsCostIsFlatInPageSize: the admin directory is four
// statements however many principals the organisation holds (#671).
//
// This replaces TestListPrincipalsCostIsPerRowToday, which pinned the defect at
// its measured 1+3N (four statements for one principal, sixty-four for
// twenty-one) and said in its own failure message that the fix was to delete it.
// ListPrincipals now drains its base query and hands every id to loadPrincipals:
// one union over the three kind-profile tables, one over the effective grants,
// one over the group memberships. Four, and four is the ceiling here for the
// same reason it is exact: the profile read is a UNION rather than a branch per
// kind, so a directory of humans, service accounts and nodes costs what a
// directory of humans costs, and this fixture holds a node beside its humans to
// keep that honest rather than assumed.
//
// The fixture grows the grants and the group memberships with the page as well
// as the principals, because both are read through the same union: a page whose
// rows carry no grants would leave the grant assembly untested and flat by
// accident.
func TestListPrincipalsCostIsFlatInPageSize(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()

	// One group holding a grant, so every member's effective grants include an
	// inherited one and the read exercises both halves of the union.
	grp, err := gw.CreateGroup(ctx, "", storage.GroupSpec{Name: "probes", DisplayName: "Probes"}, all)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := gw.CreateGroupGrant(ctx, "", grp.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, all); err != nil {
		t.Fatalf("create group grant: %v", err)
	}
	// A node principal, so the page is not single-kind. A per-kind branch would
	// cost one more statement here than in a humans-only directory; the union
	// costs the same, which is what makes the ceiling below a real number.
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "probe-node"}, all); err != nil {
		t.Fatalf("create node: %v", err)
	}

	list := func() (int, error) {
		ps, err := gw.ListPrincipals(ctx, all, false)
		return len(ps), err
	}
	add := func(i int) {
		t.Helper()
		pr, err := gw.CreateHumanPrincipal(ctx, "", storage.HumanSpec{
			Username: fmt.Sprintf("probe-%d", i), Email: fmt.Sprintf("probe-%d@example.test", i), DisplayName: "Probe",
		}, all)
		if err != nil {
			t.Fatalf("create principal %d: %v", i, err)
		}
		if _, err := gw.CreateGrant(ctx, "", pr.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, all); err != nil {
			t.Fatalf("grant principal %d: %v", i, err)
		}
		if err := gw.AddGroupMember(ctx, "", grp.ID, pr.ID, all); err != nil {
			t.Fatalf("add principal %d to group: %v", i, err)
		}
	}

	add(0)
	small := measure(t, counter, list)
	for i := 1; i <= widePage; i++ {
		add(i)
	}
	large := measure(t, counter, list)

	// The base query, the kind-profile union, the grant union, the memberships.
	assertFlatCost(t, small, large, 4)
}

// all-scope reads are what these measure: an ABAC-narrowed read takes the same
// code path with a different WHERE, and the count is the same either way. Named
// here rather than left implicit because "the count is scope-independent" is a
// claim, and this is the one that checks it: the same component page, read once
// under an all scope and once under a subtree scope, costs the same.
func TestListCostDoesNotDependOnScopeShape(t *testing.T) {
	gw, counter := countingEstate(t)
	ctx := context.Background()
	roomNames := mustRooms(t, gw)

	root := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "dsp-root", LocationName: strptr(roomNames[0])}, all)
	for i := 1; i <= widePage; i++ {
		mustCreateComponent(t, gw, storage.ComponentSpec{
			Name: fmt.Sprintf("card-%d", i), ParentName: strptr("dsp-root"),
		}, all)
	}

	allRead := measure(t, counter, func() (int, error) {
		cs, err := gw.ListComponents(ctx, all)
		return len(cs), err
	})
	subtree := measure(t, counter, func() (int, error) {
		cs, err := gw.ListComponents(ctx, scope.Set{IDs: []string{root.ID}})
		return len(cs), err
	})

	if allRead.n != subtree.n {
		t.Errorf("all-scope list cost %d statements and a subtree-scope list %d: the scope shape should change the WHERE, not the number of round trips\n  all:     %q\n  subtree: %q",
			allRead.n, subtree.n, allRead.stmts, subtree.stmts)
	}
	if subtree.rows == 0 {
		t.Errorf("the subtree-scoped read returned no rows, so it measured an empty page")
	}
}
