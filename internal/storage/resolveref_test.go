package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// fakeErrQuerier satisfies querier without a database: Query always fails
// cleanly, so loadByRef returns an ordinary wrapped error rather than
// crashing, which is what lets a test drive resolveRef PAST the tier guard
// without a testcontainer.
type fakeErrQuerier struct{}

func (fakeErrQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (fakeErrQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("fake: no db in this test")
}

// TestResolveRefPanicsOnTierMismatch pins the guard a review asked for after
// this task's own history shipped the tier mismatch it catches twice
// (CreateComponent's system/location binds, ResolveTags' forSystem filter):
// a scope resolved for one resource checked against a DIFFERENT tree tier's
// config is a caller bug, not a runtime condition, so resolveRef panics
// immediately rather than silently denying (or, worse, silently narrowing
// nothing) every caller. The panic fires before any query runs (the fake
// querier's Query is never reached), which is what proves the guard is the
// very first thing resolveRef does, not a check racing a real lookup.
func TestResolveRefPanicsOnTierMismatch(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("resolveRef with a mismatched resource did not panic")
		}
	}()
	mismatched := scope.Set{IDs: []string{"019fe173-6633-71c6-a100-5a48017ab4b9"}}
	// "component" cannot cover the system table: applicableKinds("component")
	// is {component: true} alone, so this is exactly the shape Critical A
	// shipped (a component-tier scope checked against systemConfig).
	_, _ = resolveRef(context.Background(), fakeErrQuerier{}, systemConfig, "whatever", "component", mismatched, refPolicyHide)
}

// TestResolveRefAllScopeNeverMismatches proves the "or All-pass" half of the
// same guard: an All scope is universal by construction (inScopeTree's own
// short-circuit treats every row as in scope regardless of table), so it
// never trips the tier check no matter what resource string names it. This
// is what lets scopedByName degenerate cleanly into resolveRef with an All
// set rather than needing a separate policy value. The fake querier's clean
// error return (not a crash) is what lets this drive resolveRef PAST the
// guard to prove it did not fire, without a real database.
func TestResolveRefAllScopeNeverMismatches(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("resolveRef with an All scope panicked: %v", r)
		}
	}()
	// A resource that covers nothing at all (not even itself, per
	// applicableKinds' default empty map) would panic under any non-all
	// scope; All must still pass clean through to the fake querier's own
	// ordinary error, which this test does not otherwise assert on.
	_, _ = resolveRef(context.Background(), fakeErrQuerier{}, systemConfig, "", "nonsense-resource", scope.Set{All: true}, refPolicyHide)
}

// scopeResolvedFor resolves a non-all set for resource from one component-tier
// grant whose role carries every action, the shape a scoped operator's request
// arrives with: whether it holds any roots depends only on whether resource is
// registered on the component tier.
func scopeResolvedFor(resource, action string) scope.Set {
	idx := rbac.NewRoleIndex([]rbac.Role{{ID: "operator", Permissions: []string{"*:*"}}})
	g := []scope.Grant{{Role: "operator", ScopeKind: "component", ScopeID: "019fe173-6633-71c6-a100-5a48017ab4b9"}}
	return scope.Resolve(g, idx, resource, action)
}

// TestResolveScopedRefusesASetResolvedForAnInapplicableKind pins the defence in
// depth #754 added under the registration test: a non-all set resolved for a
// resource that cannot be checked against the table (one no tier registers, or
// one registered on a different tier) is a wiring mistake, and resolveScoped
// returns ErrScopeWiring for it rather than cfg.notFound or cfg.forbidden, so
// it surfaces as a 500 instead of a silent denial indistinguishable from a
// legitimately empty grant. The guard runs before any query (the fake querier
// is never reached), on the read leg and the action leg alike.
func TestResolveScopedRefusesASetResolvedForAnInapplicableKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	component := scopeResolvedFor("component", "read")
	cases := []struct {
		name         string
		read, action scope.Set
	}{
		{"an unregistered kind on the action leg", component, scopeResolvedFor("widget", "issue")},
		{"an unregistered kind on the read leg", scopeResolvedFor("widget", "read"), component},
		{"another tier's kind on the action leg", component, scopeResolvedFor("location", "update")},
	}
	for _, tc := range cases {
		_, err := resolveScoped(ctx, fakeErrQuerier{}, componentConfig, "display-1", tc.read, tc.action)
		if !errors.Is(err, ErrScopeWiring) {
			t.Errorf("%s: err = %v, want ErrScopeWiring", tc.name, err)
		}
		if errors.Is(err, componentConfig.notFound) || errors.Is(err, componentConfig.forbidden) {
			t.Errorf("%s: err = %v reads as a refusal; a wiring mistake must not look like a denial", tc.name, err)
		}
	}

	// The placement resolvers make the same check on the set they are given.
	if _, err := resolvePlacementRef(ctx, fakeErrQuerier{}, locationConfig, "hq", component); !errors.Is(err, ErrScopeWiring) {
		t.Errorf("resolvePlacementRef with a component-tier set on the location table: err = %v, want ErrScopeWiring", err)
	}
	if _, err := resolvePlacementWriteRef(ctx, fakeErrQuerier{}, systemConfig, "av-1", component, component); !errors.Is(err, ErrScopeWiring) {
		t.Errorf("resolvePlacementWriteRef with a component-tier set on the system table: err = %v, want ErrScopeWiring", err)
	}
}

// TestResolveScopedPassesALegitimatelyEmptyGrant is the other half of the
// distinction: a set resolved for the table's own resource that simply holds no
// roots (the caller's grants carry no update) is not a wiring mistake. It passes
// the guard and reaches the lookup (the fake querier's own error here), where a
// real database would answer the ordinary refusal.
func TestResolveScopedPassesALegitimatelyEmptyGrant(t *testing.T) {
	t.Parallel()
	empty := scope.Resolve(nil, rbac.NewRoleIndex(nil), "component", "update")
	if !empty.Empty() {
		t.Fatalf("no grants resolved to %+v, want the empty set", empty)
	}
	_, err := resolveScoped(context.Background(), fakeErrQuerier{}, componentConfig, "display-1", scopeResolvedFor("component", "read"), empty)
	if err == nil || errors.Is(err, ErrScopeWiring) {
		t.Fatalf("err = %v, want the lookup's own error, not ErrScopeWiring", err)
	}
	// A hand-built or all set carries no resolution to check, and passes too.
	if _, err := resolveScoped(context.Background(), fakeErrQuerier{}, componentConfig, "display-1", scope.Set{All: true}, scope.Set{}); errors.Is(err, ErrScopeWiring) {
		t.Fatalf("an all read and an unresolved action set tripped the wiring guard: %v", err)
	}
}
