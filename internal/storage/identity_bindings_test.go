package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// An identity binding (#1385, ADR-1282) ties one value a conduit component
// reports under a match key to exactly one owner, a system or a component. It
// is a relationship with two ends, so the gateway scopes both: the conduit in
// component read then update, the owner in its own tier's read then update.

type bindingFixture struct {
	gw  *storage.PG
	dsn string
	all scope.Set
	// ids of the rows the fixture made, for building rooted scopes.
	ids map[string]string
}

func newBindingFixture(t *testing.T, ctx context.Context) *bindingFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f := &bindingFixture{gw: gw, dsn: dsn, all: scope.Set{All: true}, ids: map[string]string{}}
	for _, s := range []string{"room-a", "room-b"} {
		sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s}, f.all, f.all)
		if err != nil {
			t.Fatalf("system %s: %v", s, err)
		}
		f.ids[s] = sys.ID
	}
	product := storagetest.MintProduct(t, ctx, gw, "").Name
	for _, c := range []string{"ucc-cloud", "other-cloud", "panel-1"} {
		comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c, ProductName: &product}, f.all, f.all, f.all, f.all)
		if err != nil {
			t.Fatalf("component %s: %v", c, err)
		}
		f.ids[c] = comp.ID
	}
	return f
}

// allScopes is every end in the all scope.
func (f *bindingFixture) allScopes() storage.IdentityBindingScopes {
	return storage.IdentityBindingScopes{ComponentRead: f.all, ComponentUpdate: f.all, SystemRead: f.all, SystemUpdate: f.all}
}

func rooted(ids ...string) scope.Set { return scope.Set{IDs: ids} }

func (f *bindingFixture) count(t *testing.T, ctx context.Context, sql string, args ...any) int {
	t.Helper()
	conn, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func sysOwner(name string) storage.IdentityBindingOwner {
	return storage.IdentityBindingOwner{Kind: "system", Name: name}
}

// A put round-trips through the list, naming both ends; a re-put to the same
// owner is a no-op that writes no second audit row.
func TestIdentityBindingPutListsAndAuditsOnce(t *testing.T) {
	ctx := context.Background()
	f := newBindingFixture(t, ctx)

	b, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-100", sysOwner("room-a"), f.allScopes())
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if b.ConduitName != "ucc-cloud" || b.Key != "ucc-room-id" || b.Value != "R-100" || b.OwnerKind != "system" || b.OwnerName != "room-a" || b.OwnerID != f.ids["room-a"] {
		t.Fatalf("put = %+v, want ucc-cloud/ucc-room-id/R-100 owned by system room-a", b)
	}
	if _, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-100", sysOwner(f.ids["room-a"]), f.allScopes()); err != nil {
		t.Fatalf("re-put by uuid: %v", err)
	}
	list, err := f.gw.ListIdentityBindings(ctx, "ucc-cloud", f.all, f.all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Bindings) != 1 || list.Hidden != 0 || list.Bindings[0].OwnerName != "room-a" {
		t.Fatalf("list = %+v, want the one binding to room-a", list)
	}
	if n := f.count(t, ctx, `select count(*) from audit_log where resource = 'identity_binding'`); n != 1 {
		t.Fatalf("audit rows = %d, want 1: a re-put to the same owner is a no-op", n)
	}
	var auditNew string
	conn, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if err := conn.QueryRow(ctx, `select new::text from audit_log where resource = 'identity_binding'`).Scan(&auditNew); err != nil {
		t.Fatalf("audit: %v", err)
	}
	for _, want := range []string{"ucc-cloud", "room-a", "R-100"} {
		if !strings.Contains(auditNew, want) {
			t.Errorf("audit new = %s, want it to name %q (both ends and the identity)", auditNew, want)
		}
	}
}

// One identity on one conduit has at most one owner: a put naming another
// owner moves the binding rather than adding a second.
func TestIdentityBindingHasOneOwnerAndMoves(t *testing.T) {
	ctx := context.Background()
	f := newBindingFixture(t, ctx)
	if _, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-100", sysOwner("room-a"), f.allScopes()); err != nil {
		t.Fatalf("put: %v", err)
	}
	b, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-100", storage.IdentityBindingOwner{Kind: "component", Name: "panel-1"}, f.allScopes())
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if b.OwnerKind != "component" || b.OwnerName != "panel-1" {
		t.Fatalf("moved = %+v, want component panel-1", b)
	}
	if n := f.count(t, ctx, `select count(*) from identity_binding`); n != 1 {
		t.Fatalf("bindings = %d, want 1", n)
	}
	// The same identity on another conduit is another binding.
	if _, err := f.gw.PutIdentityBinding(ctx, "", "other-cloud", "ucc-room-id", "R-100", sysOwner("room-b"), f.allScopes()); err != nil {
		t.Fatalf("put on another conduit: %v", err)
	}
	if n := f.count(t, ctx, `select count(*) from identity_binding`); n != 2 {
		t.Fatalf("bindings = %d, want 2: the key is per conduit", n)
	}
}

// The table itself holds one owner per row: the exclusive arc's CHECK refuses
// a row naming both or neither.
func TestIdentityBindingArcIsExclusive(t *testing.T) {
	ctx := context.Background()
	f := newBindingFixture(t, ctx)
	conn, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	insert := `insert into identity_binding (conduit_id, property_type_id, value, owner_kind, component_id, system_id)
		values ($1, (select id from property_type where name = 'ucc-room-id'), $2, $3, $4, $5)`
	// The control: a row naming exactly one owner lands, so the refusals below
	// are the CHECK and not a missing table.
	if _, err := conn.Exec(ctx, insert, f.ids["ucc-cloud"], "one", "system", nil, f.ids["room-a"]); err != nil {
		t.Fatalf("a row naming exactly one owner was refused: %v", err)
	}
	if _, err := conn.Exec(ctx, insert, f.ids["ucc-cloud"], "kind-mismatch", "component", nil, f.ids["room-a"]); err == nil {
		t.Error("a row whose owner_kind does not match its set column was accepted")
	}
	if _, err := conn.Exec(ctx, insert, f.ids["ucc-cloud"], "both", "system", f.ids["panel-1"], f.ids["room-a"]); err == nil {
		t.Error("a row naming a component and a system was accepted")
	}
	if _, err := conn.Exec(ctx, insert, f.ids["ucc-cloud"], "neither", "system", nil, nil); err == nil {
		t.Error("a row naming no owner was accepted")
	}
	if _, err := conn.Exec(ctx, insert, f.ids["ucc-cloud"], "", "system", nil, f.ids["room-a"]); err == nil {
		t.Error("an empty identity value was accepted")
	}
}

// Owner kinds other than component and system are refused before anything
// resolves, and the match key must be a registered property type.
func TestIdentityBindingRefusesPlacesNodesAndUnknownKeys(t *testing.T) {
	ctx := context.Background()
	f := newBindingFixture(t, ctx)
	for _, kind := range []string{"location", "node", "platform"} {
		_, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", storage.IdentityBindingOwner{Kind: kind, Name: "anything"}, f.allScopes())
		if !errors.Is(err, storage.ErrIdentityBindingOwnerKind) {
			t.Errorf("owner kind %s = %v, want ErrIdentityBindingOwnerKind", kind, err)
		}
	}
	_, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "no-such-key", "R-1", sysOwner("room-a"), f.allScopes())
	if !errors.Is(err, storage.ErrIdentityBindingKeyUnregistered) {
		t.Errorf("unregistered key = %v, want ErrIdentityBindingKeyUnregistered", err)
	}
	if n := f.count(t, ctx, `select count(*) from identity_binding`); n != 0 {
		t.Fatalf("bindings = %d, want none", n)
	}
}

// Both ends are scoped: the conduit in component read then update, the owner
// in its tier's read then update. Out of read is not found, readable but not
// writable is forbidden; an owner refusal says it is the owner's.
func TestIdentityBindingScopesBothEnds(t *testing.T) {
	ctx := context.Background()
	f := newBindingFixture(t, ctx)
	conduit, roomA := f.ids["ucc-cloud"], f.ids["room-a"]

	cases := []struct {
		name      string
		sc        storage.IdentityBindingScopes
		wantOwner bool
		want      error
	}{
		{"conduit outside component read", storage.IdentityBindingScopes{ComponentRead: rooted(f.ids["panel-1"]), ComponentUpdate: rooted(f.ids["panel-1"]), SystemRead: f.all, SystemUpdate: f.all}, false, storage.ErrComponentNotFound},
		{"conduit readable, not writable", storage.IdentityBindingScopes{ComponentRead: f.all, ComponentUpdate: rooted(f.ids["panel-1"]), SystemRead: f.all, SystemUpdate: f.all}, false, storage.ErrComponentForbidden},
		{"system outside system read", storage.IdentityBindingScopes{ComponentRead: rooted(conduit), ComponentUpdate: rooted(conduit), SystemRead: rooted(f.ids["room-b"]), SystemUpdate: rooted(f.ids["room-b"])}, true, storage.ErrSystemNotFound},
		{"system readable, not writable", storage.IdentityBindingScopes{ComponentRead: rooted(conduit), ComponentUpdate: rooted(conduit), SystemRead: f.all, SystemUpdate: rooted(f.ids["room-b"])}, true, storage.ErrSystemForbidden},
	}
	for _, c := range cases {
		_, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", sysOwner("room-a"), c.sc)
		if !errors.Is(err, c.want) {
			t.Errorf("%s = %v, want %v", c.name, err, c.want)
		}
		var ownerErr *storage.IdentityBindingOwnerError
		if got := errors.As(err, &ownerErr); got != c.wantOwner {
			t.Errorf("%s: owner-end error = %v, want %v (%v)", c.name, got, c.wantOwner, err)
		}
	}
	// A component owner resolves on the component tier.
	_, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", storage.IdentityBindingOwner{Kind: "component", Name: "panel-1"},
		storage.IdentityBindingScopes{ComponentRead: f.all, ComponentUpdate: rooted(conduit), SystemRead: f.all, SystemUpdate: f.all})
	if !errors.Is(err, storage.ErrComponentForbidden) {
		t.Errorf("component owner readable, not writable = %v, want ErrComponentForbidden", err)
	}
	if n := f.count(t, ctx, `select count(*) from identity_binding`); n != 0 {
		t.Fatalf("bindings = %d, want none after every refusal", n)
	}
	// Inside both ends it is served.
	if _, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", sysOwner("room-a"),
		storage.IdentityBindingScopes{ComponentRead: rooted(conduit), ComponentUpdate: rooted(conduit), SystemRead: rooted(roomA), SystemUpdate: rooted(roomA)}); err != nil {
		t.Fatalf("put inside both scopes: %v", err)
	}
}

// Moving a binding costs update on the owner it leaves as well as the one it
// joins; deleting one costs update on its owner.
func TestIdentityBindingMoveAndDeleteNeedTheCurrentOwner(t *testing.T) {
	ctx := context.Background()
	f := newBindingFixture(t, ctx)
	conduit, roomA, roomB := f.ids["ucc-cloud"], f.ids["room-a"], f.ids["room-b"]
	if _, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", sysOwner("room-a"), f.allScopes()); err != nil {
		t.Fatalf("put: %v", err)
	}
	onlyB := storage.IdentityBindingScopes{ComponentRead: rooted(conduit), ComponentUpdate: rooted(conduit), SystemRead: f.all, SystemUpdate: rooted(roomB)}
	if _, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", sysOwner("room-b"), onlyB); !errors.Is(err, storage.ErrIdentityBindingMoveForbidden) {
		t.Errorf("move without update on the owner it leaves = %v, want ErrIdentityBindingMoveForbidden", err)
	}
	if err := f.gw.DeleteIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", onlyB); !errors.Is(err, storage.ErrSystemForbidden) {
		t.Errorf("delete without update on the owner = %v, want ErrSystemForbidden", err)
	}
	// An owner the caller cannot read is not disclosed by a delete.
	blind := storage.IdentityBindingScopes{ComponentRead: rooted(conduit), ComponentUpdate: rooted(conduit), SystemRead: rooted(roomB), SystemUpdate: rooted(roomB)}
	if err := f.gw.DeleteIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", blind); !errors.Is(err, storage.ErrIdentityBindingNotFound) {
		t.Errorf("delete of a binding to an unreadable owner = %v, want ErrIdentityBindingNotFound", err)
	}
	both := storage.IdentityBindingScopes{ComponentRead: rooted(conduit), ComponentUpdate: rooted(conduit), SystemRead: f.all, SystemUpdate: rooted(roomA, roomB)}
	if _, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", sysOwner("room-b"), both); err != nil {
		t.Fatalf("move with update on both owners: %v", err)
	}
	if err := f.gw.DeleteIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", both); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := f.gw.DeleteIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", both); !errors.Is(err, storage.ErrIdentityBindingNotFound) {
		t.Errorf("second delete = %v, want ErrIdentityBindingNotFound", err)
	}
	if n := f.count(t, ctx, `select count(*) from audit_log where resource = 'identity_binding'`); n != 3 {
		t.Errorf("audit rows = %d, want 3 (create, move, delete)", n)
	}
}

// Deleting either end deletes the binding.
func TestIdentityBindingGoesWithEitherEnd(t *testing.T) {
	ctx := context.Background()
	f := newBindingFixture(t, ctx)
	for _, v := range []string{"R-1", "R-2"} {
		if _, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", v, sysOwner("room-a"), f.allScopes()); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if _, err := f.gw.PutIdentityBinding(ctx, "", "other-cloud", "ucc-room-id", "R-3", storage.IdentityBindingOwner{Kind: "component", Name: "panel-1"}, f.allScopes()); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := f.gw.DeleteSystem(ctx, "", "room-a", f.all, f.all); err != nil {
		t.Fatalf("delete owner system: %v", err)
	}
	if n := f.count(t, ctx, `select count(*) from identity_binding where conduit_id = $1`, f.ids["ucc-cloud"]); n != 0 {
		t.Errorf("bindings to a deleted system = %d, want 0", n)
	}
	if err := f.gw.DeleteComponent(ctx, "", "other-cloud", f.all, f.all); err != nil {
		t.Fatalf("delete conduit: %v", err)
	}
	if n := f.count(t, ctx, `select count(*) from identity_binding`); n != 0 {
		t.Errorf("bindings after deleting the conduit = %d, want 0", n)
	}
}

// The list names an owner only inside the caller's read on its tier, and
// counts the rest.
func TestIdentityBindingListNamesOnlyReadableOwners(t *testing.T) {
	ctx := context.Background()
	f := newBindingFixture(t, ctx)
	puts := []struct {
		value string
		owner storage.IdentityBindingOwner
	}{
		{"R-A", sysOwner("room-a")},
		{"R-B", sysOwner("room-b")},
		{"P-1", storage.IdentityBindingOwner{Kind: "component", Name: "panel-1"}},
	}
	for _, p := range puts {
		if _, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", p.value, p.owner, f.allScopes()); err != nil {
			t.Fatalf("put %s: %v", p.value, err)
		}
	}
	conduit := f.ids["ucc-cloud"]
	list, err := f.gw.ListIdentityBindings(ctx, "ucc-cloud", rooted(conduit), rooted(f.ids["room-a"]))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Bindings) != 1 || list.Bindings[0].Value != "R-A" || list.Hidden != 2 {
		t.Fatalf("list = %+v, want R-A named and two counted", list)
	}
	if _, err := f.gw.ListIdentityBindings(ctx, "ucc-cloud", rooted(f.ids["panel-1"]), f.all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Fatalf("list on an unreadable conduit = %v, want ErrComponentNotFound", err)
	}
}

// A fanned value never lands on its own conduit (an unidentified sample
// already does), so a binding whose owner is the conduit is refused.
func TestIdentityBindingRefusesTheConduitAsOwner(t *testing.T) {
	ctx := context.Background()
	f := newBindingFixture(t, ctx)
	for _, ref := range []string{"ucc-cloud", f.ids["ucc-cloud"]} {
		_, err := f.gw.PutIdentityBinding(ctx, "", "ucc-cloud", "ucc-room-id", "R-1", storage.IdentityBindingOwner{Kind: "component", Name: ref}, f.allScopes())
		if !errors.Is(err, storage.ErrIdentityBindingOwnerIsConduit) {
			t.Errorf("owner %s (the conduit) = %v, want ErrIdentityBindingOwnerIsConduit", ref, err)
		}
	}
	if n := f.count(t, ctx, `select count(*) from identity_binding`); n != 0 {
		t.Fatalf("bindings = %d, want none", n)
	}
}
