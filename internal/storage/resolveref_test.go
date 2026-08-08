package storage

import (
	"context"
	"errors"
	"testing"

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
