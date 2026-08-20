package storage_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// Wall-clock benchmarks over the real Storage Gateway (#651). They are the
// SECOND instrument, and they do not gate anything.
//
// The first is round-trip counting (storagetest/querycount, pinned in
// list_cost_test.go). Counting is deterministic and catches the N+1 exactly, so
// it is the one that gates. It is blind to everything that happens INSIDE a
// single statement: a dropped index, a plan flip from index scan to sequential
// scan, a recursive CTE that stops being bounded, a predicate that stops being
// sargable. All of those get slower without issuing one extra query, and the
// count never moves. That blind spot is what these measure.
//
// They are diagnostic, run deliberately (`make bench`) before and after a change
// that claims a performance effect and compared with benchstat. They are inert
// under `make test`, because a Go benchmark does not run without -bench. There
// is no timing ASSERTION anywhere in this file and there must never be one: this
// box is WSL2 and its wall clock flakes, which is the whole reason the counting
// instrument exists.
//
// # Two sizes, and why
//
// Every benchmark runs on a small fleet and a large one, about a factor of ten
// apart. A single size cannot tell a constant apart from a linear cost, and the
// growth curve is the entire question here: ListComponents SHOULD grow with the
// fleet (it returns every row) while ResolveTags should NOT (it walks one
// component's ancestors), and only the pair of numbers says whether that is
// still true.
//
// # Setup is never inside the timed section
//
// Every benchmark below calls b.ResetTimer after its fixture is in hand, and the
// fixture itself is built once per size for the whole binary (benchFleet) and
// shared. A benchmark that provisions inside its own timed loop measures the
// harness, which is the mistake to avoid: a database create plus a boot seed
// costs far more than any query here, so it would swamp every signal and every
// number would move together whenever the harness changed.
//
// The shared fleet is why the read benchmarks in this file are read-ONLY. A
// benchmark that wrote to it would change what the next one measures, and the
// numbers would depend on the order -bench happened to select.
//
// # Subtract the floor before believing a number
//
// A statement to a containerized Postgres is not free, and on this box it is not
// cheap either. BenchmarkRoundTripFloor measures that, and every other number
// here contains at least one copy of it per statement the call issues. A read of
// two statements therefore cannot go much below twice the floor however perfect
// its plan is, and the fraction of a benchmark that a plan can even influence
// falls as its statement count rises. That is the ceiling on what this instrument
// can see, it is why the floor is measured rather than assumed, and it is why one
// candidate benchmark was measured and then deliberately not shipped (see the
// note above warmRows).

// benchSize is one fleet shape. The two are about a factor of ten apart in every
// dimension that a query here grows along, with component density per room held
// roughly constant so the difference between them is fleet SIZE and not fleet
// shape.
type benchSize struct {
	name       string
	buildings  int // under one campus
	roomsPer   int // rooms per building
	systems    int
	components int
}

var benchSizes = []benchSize{
	{name: "small", buildings: 2, roomsPer: 3, systems: 5, components: 25},
	{name: "large", buildings: 6, roomsPer: 10, systems: 50, components: 250},
}

// benchFleet is a built fleet plus the handles a benchmark needs to address
// it: the gateway, the ids a batch read takes, and the names a single-row read
// resolves.
type benchFleet struct {
	gw         *storage.PG
	campus     string   // the root location's name
	rooms      []string // every room's name
	compIDs    []string // every component's id, list order
	deepComp   string   // id of the component whose tag cascade is measured
	deepCompNm string   // the same component's name, for the bindings
	sysName    string   // the system deepComp belongs to
	compScope  scope.Set
}

// benchFleets caches one built fleet per size for the life of the test binary.
//
// Correctness does not depend on this (ResetTimer already excludes the build
// from the timing); the run's length does. Every benchmark in this file reads
// the same two fleets, and `make bench` passes -count=5, so a fleet built
// inside the benchmark function would be provisioned dozens of times per run for
// no measurable difference in the answer. Built once here, each size is
// provisioned exactly once.
//
// This works only because every benchmark here is read-ONLY, which is a real
// constraint on what may be added to this file rather than an incidental
// property: a benchmark that wrote to a shared fleet would change what the next
// one measures, and the numbers would depend on the order -bench selected.
//
// The gateway is deliberately never closed. It outlives every b that touches it,
// and the container it points at is reclaimed by storagetest.Main on exit, so
// there is nothing left behind that a b.Cleanup would have caught.
var (
	benchMu     sync.Mutex
	benchFleets = map[string]*benchFleet{}
)

func fleetFor(b *testing.B, size benchSize) *benchFleet {
	b.Helper()
	benchMu.Lock()
	defer benchMu.Unlock()
	if e, ok := benchFleets[size.name]; ok {
		return e
	}
	e := buildBenchFleet(b, size)
	benchFleets[size.name] = e
	return e
}

// buildBenchFleet provisions one fleet through the real gateway write paths,
// not raw INSERTs. The writes are what put the rows in the shape the reads
// assume (placement columns, membership rows, the product classification every
// component carries), and a hand-rolled INSERT fixture drifts from that shape
// the first time a write path changes.
func buildBenchFleet(b *testing.B, size benchSize) *benchFleet {
	b.Helper()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(b))
	if err != nil {
		b.Fatalf("open gateway: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		b.Fatalf("seed: %v", err)
	}
	e := &benchFleet{gw: gw, campus: "bench-campus"}

	mustLocation(b, gw, e.campus, "campus", nil)
	for i := range size.buildings {
		bldg := fmt.Sprintf("bench-b%d", i)
		mustLocation(b, gw, bldg, "building", strptr(e.campus))
		for j := range size.roomsPer {
			room := fmt.Sprintf("bench-r%d-%d", i, j)
			mustLocation(b, gw, room, "room", strptr(bldg))
			e.rooms = append(e.rooms, room)
		}
	}

	// One standard carrying one role, so the systems below are classified and
	// the health reads have something to resolve rather than an empty set.
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "bench-standard", Label: "Bench Standard"}); err != nil {
		b.Fatalf("upsert standard: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", "bench-standard", storage.SystemRoleSpec{
		Name: "bench-role", Label: "Bench Role", Quorum: 1,
		AcceptedTypes: []string{"generic-device"}, Impact: "degraded",
	}); err != nil {
		b.Fatalf("declare role: %v", err)
	}

	std := "bench-standard"
	sysNames := make([]string, 0, size.systems)
	for i := range size.systems {
		name := fmt.Sprintf("bench-sys-%d", i)
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
			Name: name, StandardID: &std, LocationName: strptr(e.rooms[i%len(e.rooms)]),
		}, all, all); err != nil {
			b.Fatalf("create system %s: %v", name, err)
		}
		sysNames = append(sysNames, name)
	}

	// Components: placed in a room and joined to a system, except every fifth,
	// which is a CHILD of the one before it and carries no location of its own.
	// That is the branch the address walk actually has to climb (a nested
	// component's address comes from its plane root's placement, not its own),
	// so a page without one would never exercise it.
	compNames := make([]string, 0, size.components)
	for i := range size.components {
		spec := storage.ComponentSpec{
			Name:         fmt.Sprintf("bench-c-%d", i),
			LocationName: strptr(e.rooms[i%len(e.rooms)]),
			SystemName:   strptr(sysNames[i%len(sysNames)]),
		}
		if i%5 == 4 {
			spec = storage.ComponentSpec{
				Name:       fmt.Sprintf("bench-card-%d", i),
				ParentName: strptr(compNames[i-1]),
				SystemName: strptr(sysNames[i%len(sysNames)]),
			}
		}
		c, err := gw.CreateComponent(ctx, "", spec, all, all, all, all)
		if err != nil {
			b.Fatalf("create component %d: %v", i, err)
		}
		compNames = append(compNames, c.Name)
		e.compIDs = append(e.compIDs, c.ID)
	}

	// Staff every system's first member into the role, so a system verdict has
	// a real occupant to resolve and the recompute chain a real system to climb.
	for i, sys := range sysNames {
		if err := gw.AssignRole(ctx, "", sys, "bench-role", compNames[i], all, all); err != nil {
			b.Fatalf("assign role on %s: %v", sys, err)
		}
	}

	// Four tag keys bound across every band, so the cascade resolves a real
	// override chain rather than a single row: platform is shadowed by the
	// campus, which is shadowed by the room, which is shadowed by the system.
	// A cascade with one binding in it would plan differently from a real one.
	for _, k := range []struct {
		name       string
		propagates bool
	}{
		{"bench-environment", true},
		{"bench-compliance", true},
		{"bench-tier", true},
		{"bench-asset-id", false},
	} {
		if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: k.name, Propagates: k.propagates}, all); err != nil {
			b.Fatalf("create tag %s: %v", k.name, err)
		}
	}
	e.deepCompNm = compNames[0]
	e.deepComp = e.compIDs[0]
	e.sysName = sysNames[0]
	deepRoom := e.rooms[0]
	bind := func(key, ownerKind string, owner *string, value string) {
		b.Helper()
		if _, err := gw.SetTagBinding(ctx, "", key, ownerKind, owner, value, all, all); err != nil {
			b.Fatalf("bind %s at %s: %v", key, ownerKind, err)
		}
	}
	bind("bench-environment", "platform", nil, "prod")
	bind("bench-environment", "location", strptr(e.campus), "staging")
	bind("bench-environment", "location", strptr(deepRoom), "lab")
	bind("bench-environment", "system", strptr(e.sysName), "dev")
	bind("bench-compliance", "location", strptr(e.campus), "pci")
	bind("bench-tier", "location", strptr(deepRoom), "gold")
	bind("bench-tier", "component", strptr(e.deepCompNm), "platinum")
	bind("bench-asset-id", "component", strptr(e.deepCompNm), "A-1")

	// The ABAC scope a narrowed component read runs under. The root is a
	// COMPONENT id, not a location one: a scoped list expands the roots down
	// the LISTED table's own parent_id chain, so a location root would match no
	// component and the benchmark would time an empty page (which is how the
	// first draft of this fixture read zero rows). compIDs[3] is the parent of
	// the nested every-fifth component, so its subtree is a real two-row page.
	e.compScope = scope.Set{IDs: []string{e.compIDs[3]}}
	return e
}

func mustLocation(b *testing.B, gw storage.Gateway, name, kind string, parent *string) {
	b.Helper()
	if _, err := gw.CreateLocation(context.Background(), "", storage.LocationSpec{
		Name: name, LocationType: kind, ParentName: parent,
	}, all); err != nil {
		b.Fatalf("create location %s: %v", name, err)
	}
}

// eachSize runs body against both fleets as sub-benchmarks, and reports the row
// count each one read as a custom metric.
//
// rows/op is not decoration. A benchmark whose ns/op grew because the fleet
// grew is doing its job; one whose ns/op grew while rows/op stayed put is the
// regression. Without the row count in the output, benchstat comparing two runs
// cannot tell the reader which of the two happened, and the number is only
// interpretable by someone who remembers the fixture.
func eachSize(b *testing.B, body func(b *testing.B, e *benchFleet) int) {
	for _, size := range benchSizes {
		b.Run(size.name, func(b *testing.B) {
			e := fleetFor(b, size)
			rows := body(b, e)
			b.ReportMetric(float64(rows), "rows/op")
		})
	}
}

// BenchmarkRoundTripFloor is the calibration constant, and it is first on
// purpose: it measures no query of ours at all.
//
// One pool acquire and one empty query is what EVERY number below pays before it
// does any work, and on this box that floor is a large fraction of the cheapest
// reads. Without it a reader looks at 800us for a two-statement list and cannot
// tell a slow query from a fast one behind a slow transport, which is the exact
// confusion that produced a wrong attribution once already in this repo (#649's
// estimate charged a whole per-test floor to provisioning when it also contained
// each test's own work). Subtract this before believing any query is expensive,
// and compare it across runs before believing any regression is real: if the
// FLOOR moved, the machine moved, not the code.
//
// Read it as a LOWER bound on transport, not an exact per-statement constant.
// pgxpool's Ping reaches past the query layer to pgconn (which is also why the
// querycount tracer records zero for it), so this is the simple protocol with no
// parameters, where a real gateway statement is the extended one. The true
// per-statement cost is somewhat above this; the point of the number is its order
// of magnitude and its stability, not arithmetic to three digits.
func BenchmarkRoundTripFloor(b *testing.B) {
	e := fleetFor(b, benchSizes[0])
	ctx := context.Background()
	if err := e.gw.Ping(ctx); err != nil {
		b.Fatalf("warm ping: %v", err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := e.gw.Ping(ctx); err != nil {
			b.Fatalf("ping: %v", err)
		}
	}
}

// BenchmarkListComponents: the unpaginated fleet read, all-scope. One scoped
// list query plus the batch address attach, which is the recursive walk #648
// rewrote. Every row in the fleet comes back, so this one is EXPECTED to grow
// with the fleet; what would be a regression is growing faster than it does.
func BenchmarkListComponents(b *testing.B) {
	eachSize(b, func(b *testing.B, e *benchFleet) int {
		ctx := context.Background()
		rows := warmRows(b, func() (int, error) {
			cs, err := e.gw.ListComponents(ctx, all)
			return len(cs), err
		})
		b.ResetTimer()
		for b.Loop() {
			if _, err := e.gw.ListComponents(ctx, all); err != nil {
				b.Fatalf("list components: %v", err)
			}
		}
		return rows
	})
}

// BenchmarkListSystems: the same read on the system plane, which shares the
// scoped-list and attach machinery but its own accessor and its own table.
func BenchmarkListSystems(b *testing.B) {
	eachSize(b, func(b *testing.B, e *benchFleet) int {
		ctx := context.Background()
		rows := warmRows(b, func() (int, error) {
			ss, err := e.gw.ListSystems(ctx, all)
			return len(ss), err
		})
		b.ResetTimer()
		for b.Loop() {
			if _, err := e.gw.ListSystems(ctx, all); err != nil {
				b.Fatalf("list systems: %v", err)
			}
		}
		return rows
	})
}

// BenchmarkListLocations: the location plane, whose address is its own ancestor
// chain with no plane to cross, so it pays one walk rather than three. The
// cheapest of the three lists, and the baseline the other two are read against.
func BenchmarkListLocations(b *testing.B) {
	eachSize(b, func(b *testing.B, e *benchFleet) int {
		ctx := context.Background()
		rows := warmRows(b, func() (int, error) {
			ls, err := e.gw.ListLocations(ctx, all)
			return len(ls), err
		})
		b.ResetTimer()
		for b.Loop() {
			if _, err := e.gw.ListLocations(ctx, all); err != nil {
				b.Fatalf("list locations: %v", err)
			}
		}
		return rows
	})
}

// BenchmarkListComponentsScoped: the same list under a NARROW ABAC scope, one
// component subtree.
//
// This is a different QUERY from the all-scope one, not the same query with a
// smaller result. An all-scope list is a plain select; a scoped one expands the
// scope roots through a recursive walk of the listed table and filters on the
// result, so the two have different plans and only one of them is the plan every
// non-admin operator actually runs. It is the predicate injected into every
// applicable read in the Gateway, which makes a plan flip inside it a
// whole-product regression that the all-scope benchmark above cannot see.
//
// It should be roughly flat across the sizes: the walk is seeded from one root
// and descends, so it is bounded by the subtree, not by the table.
func BenchmarkListComponentsScoped(b *testing.B) {
	eachSize(b, func(b *testing.B, e *benchFleet) int {
		ctx := context.Background()
		rows := warmRows(b, func() (int, error) {
			cs, err := e.gw.ListComponents(ctx, e.compScope)
			return len(cs), err
		})
		if rows == 0 {
			b.Fatal("the scoped read returned no rows, so it measured an empty page")
		}
		b.ResetTimer()
		for b.Loop() {
			if _, err := e.gw.ListComponents(ctx, e.compScope); err != nil {
				b.Fatalf("list components (scoped): %v", err)
			}
		}
		return rows
	})
}

// BenchmarkPathsOfComponents: the batch address walk on its own, over the same
// page ListComponents reads.
//
// Measured apart from the list so the two subtract: the difference is the
// scoped-list query, and this is the recursive-CTE half. A page of components
// costs three recursive walks here however many rows it has (#643/#648), so the
// number should grow with the page but far more slowly than a per-row walk
// would, and much of what remains is scanning rows rather than planning.
func BenchmarkPathsOfComponents(b *testing.B) {
	eachSize(b, func(b *testing.B, e *benchFleet) int {
		ctx := context.Background()
		if _, err := e.gw.BenchPathsOf(ctx, "component", e.compIDs); err != nil {
			b.Fatalf("paths of components: %v", err)
		}
		b.ResetTimer()
		for b.Loop() {
			if _, err := e.gw.BenchPathsOf(ctx, "component", e.compIDs); err != nil {
				b.Fatalf("paths of components: %v", err)
			}
		}
		return len(e.compIDs)
	})
}

// BenchmarkResolveTags: the tag cascade for ONE component, the most complex
// single statement in the tree (three recursive chain walks unioned into one
// owner set, a window function ranking per key, and three left joins for the
// owners' labels).
//
// This one should be FLAT across the two sizes, and that is the point of running
// it at both. It walks one component's three ancestor chains, and neither chain
// gets deeper when the fleet gets wider. A version of this that tracked fleet
// size would mean a chain walk had turned into a table scan, which is exactly
// the class of regression counting cannot see: the statement count would not
// move.
func BenchmarkResolveTags(b *testing.B) {
	eachSize(b, func(b *testing.B, e *benchFleet) int {
		ctx := context.Background()
		rows := warmRows(b, func() (int, error) {
			ts, err := e.gw.ResolveTags(ctx, e.deepComp, e.sysName, all)
			return len(ts), err
		})
		b.ResetTimer()
		for b.Loop() {
			if _, err := e.gw.ResolveTags(ctx, e.deepComp, e.sysName, all); err != nil {
				b.Fatalf("resolve tags: %v", err)
			}
		}
		return rows
	})
}

// BenchmarkEffectiveTags: the same cascade BATCHED over every component in the
// fleet, which is the read that feeds the directory's Tags column.
//
// The batch form is where a cascade regression actually hurts, because it runs
// the whole thing once per listed page rather than once per drill-down, and it
// is the one whose cost is supposed to be sublinear in the page. Paired with
// ResolveTags above, the two say whether the batch is really batching.
func BenchmarkEffectiveTags(b *testing.B) {
	eachSize(b, func(b *testing.B, e *benchFleet) int {
		ctx := context.Background()
		if _, err := e.gw.EffectiveTags(ctx, "component", e.compIDs); err != nil {
			b.Fatalf("effective tags: %v", err)
		}
		b.ResetTimer()
		for b.Loop() {
			if _, err := e.gw.EffectiveTags(ctx, "component", e.compIDs); err != nil {
				b.Fatalf("effective tags: %v", err)
			}
		}
		return len(e.compIDs)
	})
}

// BenchmarkLocationHealth: the campus rollup, which is a recursive walk of the
// whole location subtree joined to every system under it, and then a correlated
// subquery per system for that system's last recorded verdict.
//
// The correlated subquery is the reason this is here rather than assumed cheap:
// it is a per-system lookup into the property table, ordered and limited, and
// property is the table that grows without bound as a fleet runs. It issues
// one statement however many systems it covers, so counting sees a flat 3 and
// always will; the cost lives entirely inside the plan.
func BenchmarkLocationHealth(b *testing.B) {
	eachSize(b, func(b *testing.B, e *benchFleet) int {
		ctx := context.Background()
		window := time.Duration(0)
		rows := warmRows(b, func() (int, error) {
			rep, err := e.gw.LocationHealth(ctx, e.campus, window, all)
			if err != nil {
				return 0, err
			}
			return len(rep.Systems), nil
		})
		b.ResetTimer()
		for b.Loop() {
			if _, err := e.gw.LocationHealth(ctx, e.campus, window, all); err != nil {
				b.Fatalf("location health: %v", err)
			}
		}
		return rows
	})
}

// BenchmarkSystemHealth: one system's report, the role-resolution side of the
// same subsystem. It resolves the system's declared and inherited roles, folds
// the verdict, and explains each role.
//
// Like ResolveTags this should be flat across the sizes: a system's roles do not
// multiply because the fleet got bigger. Unlike ResolveTags it is a small
// constant of statements plus one alarm read per IMPAIRED role, so on a healthy
// system most of the number is the resolve, and on a broken one counting and
// timing would move together. It is here because nothing else in the file
// exercises role resolution at all.
func BenchmarkSystemHealth(b *testing.B) {
	eachSize(b, func(b *testing.B, e *benchFleet) int {
		ctx := context.Background()
		window := time.Duration(0)
		rows := warmRows(b, func() (int, error) {
			rep, err := e.gw.SystemHealth(ctx, e.sysName, window, all)
			if err != nil {
				return 0, err
			}
			return len(rep.Roles), nil
		})
		b.ResetTimer()
		for b.Loop() {
			if _, err := e.gw.SystemHealth(ctx, e.sysName, window, all); err != nil {
				b.Fatalf("system health: %v", err)
			}
		}
		return rows
	})
}

// There is deliberately NO benchmark of the alarm write path here, and the
// omission is a measurement result rather than an oversight.
//
// It was built, run, and dropped. A raise-and-clear pair over a staffed
// component is the health RECOMPUTE chain, which is the recursive, lock-taking
// half of the health subsystem and the obvious fourth path to measure. On this
// box it costs about 22ms, and it issues 58 statements to do it. At the floor
// BenchmarkRoundTripFloor reports, that is roughly three quarters transport and
// one quarter anything a query planner decides, while its run-to-run spread is
// about three times the reads' (it commits twice, and commit latency here is
// disk-variable). A halving of every plan in it would move the number by less
// than its own noise, so it could not detect the regression it would appear to
// be watching, and a benchmark that cannot fail is worse than no benchmark for
// the same reason a test that cannot fail is.
//
// The honest instrument for a 58-statement path is the FIRST one: count the
// round trips, deterministically, the way list_cost_test.go does for the reads.
// That work is now done, in alarm_cost_test.go (#674), and it found what a
// benchmark could not have: the 58 is not a constant. It is the minimum of
// 12 + 5*S + 4*L per raise and the same per clear, over the slots the component
// fills and the locations above them. The count is flat only in the number of
// alarms.

// warmRows runs the read once before the timer starts and reports how many rows
// it produced.
//
// The warm call is not politeness. The first read on a fresh pool pays a
// connection open and Postgres's first-touch of every page it needs, and pgx's
// statement cache is empty, so an unwarmed first iteration is several times the
// steady-state cost. At small b.N that single outlier is a large fraction of the
// mean, which shows up as variance rather than as the constant it is.
func warmRows(b *testing.B, read func() (int, error)) int {
	b.Helper()
	rows, err := read()
	if err != nil {
		b.Fatalf("warm read: %v", err)
	}
	return rows
}
