package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The batch path walk (#643). These tests are internal (package storage, the
// established pattern of render_test.go and five siblings) because PathsOf
// takes the unexported querier and scopeTable, and because the query-count
// test has to substitute its own querier for the pool, which no exported
// entry point allows.
//
// HarnessDSN is how they reach real Postgres: internal/storage/storagetest
// imports storage, so an internal test file that imported the harness back
// would be an import cycle in test. The external test package's TestMain,
// which already routes this package through that harness, assigns it here
// instead. Nothing outside the test binary can see it (the declaration lives
// in a _test.go file).
//
// storagetest/querycount, imported below, is the same rule read the other way:
// the counting primitive deliberately imports nothing of storage's (only pgx),
// so it sits in its own package UNDER the harness rather than in it, and an
// internal test file can consume it without the cycle the harness itself would
// bring. That is why the counter lives one directory down (#650).
var HarnessDSN func(t testing.TB) string

// batchPathPool opens a pool on a fresh migrated database. Not boot-seeded:
// the fixture below inserts the one reference row it needs (a location_type)
// itself, so the tree it builds is exactly the shape being asserted and
// nothing else.
func batchPathPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if HarnessDSN == nil {
		t.Fatal("HarnessDSN is nil: the external TestMain no longer assigns it")
	}
	pool, err := pgxpool.New(context.Background(), HarnessDSN(t))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// batchPathFixture is render_path_test.go's tree (a component placed at a
// room, a nested component whose address crosses into the location tree, a
// system placed at a building, a three-deep location chain) plus the two
// shapes that file has no case for: an UNPLACED plane root in each of the
// two accessor planes, which is the branch where the address opens on the
// accessor with no location prefix at all.
//
//	boi (root)
//	  17c
//	    415a
//	      display-1 (component, placed at 415a)
//	      dsp-1     (component, placed at 415a)
//	        dante-card-1 (component, child of dsp-1, no location of its own)
//	    av (system, placed at 17c)
//	spare-1 (component, unplaced root)
//	  spare-card-1 (component, child of spare-1, WITH its own location: 415a)
//	spare-sys (system, unplaced root)
//
// spare-card-1's own location_id is the fixture's load-bearing row, and the
// reason it is set to a location its plane root is not at. PathOf's doc
// comment singles out one subtle rule: only a PLANE ROOT's own location_id
// participates in an address, never a descendant's, even though a descendant
// may legitimately carry one (CreateComponent and MoveComponent both allow
// it, and the two columns are written independently). Without a descendant
// whose own location_id differs from its plane root's, "the plane root's
// location_id" and "the deepest non-null location_id in the chain" are the
// same function over the fixture, and a walker that implemented the second
// would pass every assertion here. spare-card-1 separates them: its plane
// root is unplaced, so the correct address carries NO location prefix, while
// the deepest-non-null rule would prefix it with boi.17c.415a.
//
// Plus one shape no gateway write can produce: two components that are each
// other's parent. The recursive walks stop on it by their CYCLE clause, and
// the batch walker must stop in exactly the same place as the single-row one,
// which is why neither filters is_cycle out. Malformed data is where two
// implementations of the same walk are most likely to diverge unnoticed.
type batchPathFixture struct {
	productID                                      string
	boi, c17, r415a                                string
	display1, dsp1, danteCard1, spare1, spareCard1 string
	av, spareSys                                   string
	loopA, loopB                                   string
}

func buildBatchPathFixture(t *testing.T, pool *pgxpool.Pool) batchPathFixture {
	t.Helper()
	ctx := context.Background()
	scan1 := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("fixture %q: %v", sql, err)
		}
		return id
	}

	locType := scan1(`insert into location_type (name, display_name) values ('room', 'Room') returning id`)
	// generic-device exists on any migrated database (the product/type floor
	// backfill creates it), and product_id is NOT NULL on component, so the
	// fixture classifies against that rather than inventing a product.
	product := scan1(`select id from product where name = 'generic-device'`)

	loc := func(name string, parent *string) string {
		return scan1(`insert into location (name, location_type, parent_id) values ($1, $2, $3) returning id`, name, locType, parent)
	}
	comp := func(name string, parent, location *string) string {
		return scan1(`insert into component (name, product_id, parent_id, location_id) values ($1, $2, $3, $4) returning id`,
			name, product, parent, location)
	}
	sys := func(name string, parent, location *string) string {
		return scan1(`insert into system (name, parent_id, location_id) values ($1, $2, $3) returning id`, name, parent, location)
	}

	fx := batchPathFixture{productID: product}
	fx.boi = loc("boi", nil)
	fx.c17 = loc("17c", &fx.boi)
	fx.r415a = loc("415a", &fx.c17)
	fx.display1 = comp("display-1", nil, &fx.r415a)
	fx.dsp1 = comp("dsp-1", nil, &fx.r415a)
	fx.danteCard1 = comp("dante-card-1", &fx.dsp1, nil)
	fx.spare1 = comp("spare-1", nil, nil)
	// A descendant carrying its own location, under a plane root that has
	// none: the row that tells the plane-root rule apart from a
	// deepest-non-null one. See the fixture doc comment.
	fx.spareCard1 = comp("spare-card-1", &fx.spare1, &fx.r415a)
	fx.av = sys("av", nil, &fx.c17)
	fx.spareSys = sys("spare-sys", nil, nil)

	// The cycle, written directly: inserted as two roots, then pointed at
	// each other, since neither could name the other as its parent before
	// the other existed (and no gateway write path would allow it at all).
	fx.loopA = comp("loop-a", nil, nil)
	fx.loopB = comp("loop-b", nil, nil)
	for _, pair := range [][2]string{{fx.loopA, fx.loopB}, {fx.loopB, fx.loopA}} {
		if _, err := pool.Exec(ctx, `update component set parent_id = $2 where id = $1`, pair[0], pair[1]); err != nil {
			t.Fatalf("fixture cycle: %v", err)
		}
	}
	return fx
}

// allIDs returns every id in tbl, so the equivalence test below asserts over
// the whole fixture rather than a hand-listed subset that a later fixture
// row could quietly escape.
func allIDs(t *testing.T, pool *pgxpool.Pool, tbl scopeTable) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `select id from `+string(tbl)+` order by id`)
	if err != nil {
		t.Fatalf("list %s ids: %v", tbl, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan %s id: %v", tbl, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list %s ids: %v", tbl, err)
	}
	return ids
}

// TestPathsOfMatchesPathOfRowForRow is the correctness gate for #643: the
// batch walker and PathOf must be indistinguishable, row for row, over a
// tree with real depth in both accessor planes and in the plain location
// plane. PathOf stays the reference implementation precisely so this
// assertion has an oracle to compare against.
func TestPathsOfMatchesPathOfRowForRow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	pool := batchPathPool(t)
	fx := buildBatchPathFixture(t, pool)
	ctx := context.Background()

	// Named cases first, so a failure names the shape that broke rather than
	// only a uuid: the placed component, the nested component that crosses
	// into the location tree, the unplaced plane root and its child (empty
	// root, the branch PathOf leaves the prefix off entirely), the placed
	// and unplaced systems, and the location chain.
	//
	// spare-card-1 is the discriminating one: it carries location 415a
	// itself, and its address still opens on the accessor, because only its
	// plane root's location_id (none) counts. A walk that read the deepest
	// non-null location_id in the chain would prefix it boi.17c.415a.
	want := map[string][]string{
		fx.display1:   {"boi", "17c", "415a", "$comp", "display-1"},
		fx.danteCard1: {"boi", "17c", "415a", "$comp", "dsp-1", "dante-card-1"},
		fx.spare1:     {"$comp", "spare-1"},
		fx.spareCard1: {"$comp", "spare-1", "spare-card-1"},
		fx.av:         {"boi", "17c", "$sys", "av"},
		fx.spareSys:   {"$sys", "spare-sys"},
		fx.r415a:      {"boi", "17c", "415a"},
		fx.boi:        {"boi"},
	}

	for _, tbl := range []scopeTable{componentTable, systemTable, locationTable} {
		ids := allIDs(t, pool, tbl)
		batch, err := PathsOf(ctx, pool, tbl, ids)
		if err != nil {
			t.Fatalf("PathsOf(%s): %v", tbl, err)
		}
		for _, id := range ids {
			single, err := PathOf(ctx, pool, tbl, id)
			if err != nil {
				t.Fatalf("PathOf(%s, %s): %v", tbl, id, err)
			}
			if !equalPathSegs(batch[id], single) {
				t.Errorf("%s %s: PathsOf = %v, PathOf = %v", tbl, id, batch[id], single)
			}
			if w, ok := want[id]; ok && !equalPathSegs(single, w) {
				t.Errorf("%s %s: address = %v, want %v (the fixture shape itself is wrong)", tbl, id, single, w)
			}
		}
		if len(batch) != len(ids) {
			t.Errorf("PathsOf(%s) returned %d addresses, want %d", tbl, len(batch), len(ids))
		}
	}

	// The empty case, which must not query at all.
	empty, err := PathsOf(ctx, pool, componentTable, nil)
	if err != nil {
		t.Fatalf("PathsOf(nil): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("PathsOf(nil) = %v, want an empty map", empty)
	}
}

// countAttach runs one attach over vs and returns how many queries it cost.
//
// The counter is querycount's, not a local one (#650): the wrapper seam is
// honest HERE, and only here, because an attach takes the querier it uses as a
// parameter. The gateway's list paths do not, which is why their own count
// assertions (list_cost_test.go) take the pool-tracer seam instead. That
// distinction is the primitive's doc comment, and the reason it is a primitive.
func countAttach[T any](t *testing.T, pool *pgxpool.Pool, attach func(context.Context, querier, []*T, bool) error, vs []*T) int {
	t.Helper()
	c := querycount.New()
	if err := attach(context.Background(), c.Wrap(pool), vs, false); err != nil {
		t.Fatalf("attach over %d rows: %v", len(vs), err)
	}
	return c.N()
}

// TestAttachPathsCostIsFlatInPageSize is the #643 defect itself, made
// measurable: attaching a LIST's addresses must cost the same number of
// queries for twenty rows as for one. The assertion is EQUALITY, not
// improvement, because a per-row walk with a smaller constant is still the
// N+1 this change exists to remove.
//
// All THREE of the batch queries are held to it, which is why the wide page
// spans several rooms: the own-chain walk and the plane-root lookup grow
// with row count, but the third query walks the distinct plane-root
// LOCATIONS, and a page confined to one room would leave a per-root loop
// looking exactly as flat as a batch.
func TestAttachPathsCostIsFlatInPageSize(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	pool := batchPathPool(t)
	fx := buildBatchPathFixture(t, pool)
	ctx := context.Background()

	// Twenty more components and systems, every one of them PLACED (the
	// three-query branch: own chain, plane roots, the plane roots' location
	// chain), spread across SEVERAL rooms rather than sharing one.
	//
	// The spread is the point. Page size is not the only dimension the third
	// query can grow along: it walks the DISTINCT plane-root locations, so a
	// page whose rows all sit in one room holds that set at size one and a
	// per-root loop would look just as flat as a batch. Rooms here grow with
	// the page, so only a walk that batches them stays at three.
	const wide = 20
	const rooms = 5
	var roomIDs []string
	for i := range rooms {
		var id string
		if err := pool.QueryRow(ctx,
			`insert into location (name, location_type, parent_id) values ($1, (select location_type from location where id = $2), $2) returning id`,
			fmt.Sprintf("bulk-room-%d", i), fx.c17).Scan(&id); err != nil {
			t.Fatalf("insert bulk room %d: %v", i, err)
		}
		roomIDs = append(roomIDs, id)
	}
	var compIDs, sysIDs []string
	for i := range wide {
		room := roomIDs[i%rooms]
		var id string
		if err := pool.QueryRow(ctx,
			`insert into component (name, product_id, location_id) values ($1, $2, $3) returning id`,
			fmt.Sprintf("bulk-comp-%d", i), fx.productID, room).Scan(&id); err != nil {
			t.Fatalf("insert bulk component %d: %v", i, err)
		}
		compIDs = append(compIDs, id)
		if err := pool.QueryRow(ctx,
			`insert into system (name, location_id) values ($1, $2) returning id`,
			fmt.Sprintf("bulk-sys-%d", i), room).Scan(&id); err != nil {
			t.Fatalf("insert bulk system %d: %v", i, err)
		}
		sysIDs = append(sysIDs, id)
	}
	locIDs := allIDs(t, pool, locationTable)
	for i := range wide {
		var id string
		if err := pool.QueryRow(ctx,
			`insert into location (name, location_type, parent_id) values ($1, (select location_type from location where id = $2), $2) returning id`,
			fmt.Sprintf("bulk-loc-%d", i), roomIDs[i%rooms]).Scan(&id); err != nil {
			t.Fatalf("insert bulk location %d: %v", i, err)
		}
		locIDs = append(locIDs, id)
	}
	// The wide component and system pages must actually span the rooms, or
	// the flatness this test claims to measure is not being measured.
	if len(compIDs) < rooms || len(sysIDs) < rooms {
		t.Fatalf("wide page of %d components / %d systems cannot span %d rooms", len(compIDs), len(sysIDs), rooms)
	}

	comps := func(ids []string) []*Component {
		out := make([]*Component, 0, len(ids))
		for _, id := range ids {
			out = append(out, &Component{ID: id})
		}
		return out
	}
	systems := func(ids []string) []*System {
		out := make([]*System, 0, len(ids))
		for _, id := range ids {
			out = append(out, &System{ID: id})
		}
		return out
	}
	locations := func(ids []string) []*Location {
		out := make([]*Location, 0, len(ids))
		for _, id := range ids {
			out = append(out, &Location{ID: id})
		}
		return out
	}

	// The ceiling is the batch design's own worst case: the own-chain walk,
	// the plane-root lookup, and the plane roots' location chain, once each
	// for the whole page (a location pays only the first).
	const maxQueries = 3

	t.Run("component", func(t *testing.T) {
		one := countAttach(t, pool, attachComponentPaths, comps(compIDs[:1]))
		many := countAttach(t, pool, attachComponentPaths, comps(compIDs))
		assertFlat(t, one, many, len(compIDs), maxQueries)
	})
	t.Run("system", func(t *testing.T) {
		one := countAttach(t, pool, attachSystemPaths, systems(sysIDs[:1]))
		many := countAttach(t, pool, attachSystemPaths, systems(sysIDs))
		assertFlat(t, one, many, len(sysIDs), maxQueries)
	})
	t.Run("location", func(t *testing.T) {
		one := countAttach(t, pool, attachLocationPaths, locations(locIDs[:1]))
		many := countAttach(t, pool, attachLocationPaths, locations(locIDs))
		assertFlat(t, one, many, len(locIDs), maxQueries)
	})
}

func assertFlat(t *testing.T, one, many, n, max int) {
	t.Helper()
	if one != many {
		t.Errorf("attach cost %d queries for 1 row and %d for %d rows: still per-row", one, many, n)
	}
	if many > max {
		t.Errorf("attach cost %d queries for %d rows, want at most %d", many, n, max)
	}
}

func equalPathSegs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
