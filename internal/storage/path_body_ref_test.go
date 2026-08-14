package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Fix round 1 (task-12-review.md findings 1 and 2): a dotted address resolved
// inside a create/update BODY reference (a parent, a location, a system, an
// owner) must land on the SAME domain sentinel and the SAME HTTP status as
// the bare-name form, not the entity's own non-disclosing 404. Each test
// below drives both forms of the same missing reference through the same
// storage call and asserts they fold to the identical errors.Is target,
// which is what proves ErrPathNotFound's new Unwrap (carrying cfg.notFound)
// and withoutCandidates' new fold (replacing the concrete *ErrPathNotFound
// before it can reach mapRefErr) actually closed the gap end to end, not
// just at the sentinel-construction site.

// TestCreateComponentDottedMissingParentMatchesBareNameForm covers the
// explicit errors.Is(err, ErrComponentNotFound)-then-substitute pattern
// (components.go's create-time ParentName resolve, via resolveScopedRef).
func TestCreateComponentDottedMissingParentMatchesBareNameForm(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	_, bareErr := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "c-bare-parent", ParentName: strptr("ghost-parent"),
	}, all, all, all, all)
	if !errors.Is(bareErr, storage.ErrParentComponentNotFound) {
		t.Fatalf("bare-name missing parent = %v, want ErrParentComponentNotFound", bareErr)
	}

	_, dottedErr := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "c-dotted-parent", ParentName: strptr("$comp.ghost-parent"),
	}, all, all, all, all)
	if !errors.Is(dottedErr, storage.ErrParentComponentNotFound) {
		t.Fatalf("dotted missing parent = %v, want ErrParentComponentNotFound (same as bare-name form)", dottedErr)
	}
	assertNotLeakedPathNotFound(t, dottedErr)
}

// TestUpdateComponentDottedMissingParentMatchesBareNameForm is the :move
// twin: a different call site (resolveScopedRef inside MoveComponent) but
// the same explicit-fold pattern.
func TestUpdateComponentDottedMissingParentMatchesBareNameForm(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "patch-target"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}

	_, bareErr := gw.MoveComponent(ctx, "", c.Name, storage.ComponentMove{ParentName: strptr("ghost-parent")}, all, all, all)
	if !errors.Is(bareErr, storage.ErrParentComponentNotFound) {
		t.Fatalf("bare-name missing move parent = %v, want ErrParentComponentNotFound", bareErr)
	}

	_, dottedErr := gw.MoveComponent(ctx, "", c.Name, storage.ComponentMove{ParentName: strptr("$comp.ghost-parent")}, all, all, all)
	if !errors.Is(dottedErr, storage.ErrParentComponentNotFound) {
		t.Fatalf("dotted missing move parent = %v, want ErrParentComponentNotFound (same as bare-name form)", dottedErr)
	}
	assertNotLeakedPathNotFound(t, dottedErr)
}

// TestCreateComponentDottedMissingLocationMatchesBareNameForm covers the
// OTHER pattern: an existence-only cross-tier bind that routes through
// withoutCandidates rather than an explicit errors.Is check. Uses a real
// parent segment ("boi") so the dotted ref is a genuine structural miss
// (a child that does not exist), not just a kind mismatch, matching the
// review's own POST /components{"location":"boi.nope"} scenario exactly.
func TestCreateComponentDottedMissingLocationMatchesBareNameForm(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "boi", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create boi: %v", err)
	}

	_, bareErr := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "c-bare-loc", LocationName: strptr("ghost-loc"),
	}, all, all, all, all)
	if !errors.Is(bareErr, storage.ErrLocationNotFound) {
		t.Fatalf("bare-name missing location = %v, want ErrLocationNotFound", bareErr)
	}

	_, dottedErr := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "c-dotted-loc", LocationName: strptr("boi.nope"),
	}, all, all, all, all)
	if !errors.Is(dottedErr, storage.ErrLocationNotFound) {
		t.Fatalf("dotted missing location (boi.nope) = %v, want ErrLocationNotFound (same as bare-name form)", dottedErr)
	}
	assertNotLeakedPathNotFound(t, dottedErr)
}

// TestCreateComponentDottedMissingSystemMatchesBareNameForm is the system
// bind's twin of the location case above, same withoutCandidates pattern.
func TestCreateComponentDottedMissingSystemMatchesBareNameForm(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	_, bareErr := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "c-bare-sys", SystemName: strptr("ghost-sys"),
	}, all, all, all, all)
	if !errors.Is(bareErr, storage.ErrSystemNotFound) {
		t.Fatalf("bare-name missing system = %v, want ErrSystemNotFound", bareErr)
	}

	_, dottedErr := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "c-dotted-sys", SystemName: strptr("$sys.ghost-sys"),
	}, all, all, all, all)
	if !errors.Is(dottedErr, storage.ErrSystemNotFound) {
		t.Fatalf("dotted missing system = %v, want ErrSystemNotFound (same as bare-name form)", dottedErr)
	}
	assertNotLeakedPathNotFound(t, dottedErr)
}

// TestCreateSystemDottedMissingParentMatchesBareNameForm covers systems.go's
// own explicit-fold pattern (ErrParentSystemNotFound).
func TestCreateSystemDottedMissingParentMatchesBareNameForm(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	_, bareErr := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "s-bare-parent", ParentName: strptr("ghost-parent"),
	}, all, all)
	if !errors.Is(bareErr, storage.ErrParentSystemNotFound) {
		t.Fatalf("bare-name missing system parent = %v, want ErrParentSystemNotFound", bareErr)
	}

	_, dottedErr := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "s-dotted-parent", ParentName: strptr("$sys.ghost-parent"),
	}, all, all)
	if !errors.Is(dottedErr, storage.ErrParentSystemNotFound) {
		t.Fatalf("dotted missing system parent = %v, want ErrParentSystemNotFound (same as bare-name form)", dottedErr)
	}
	assertNotLeakedPathNotFound(t, dottedErr)
}

// TestCreateLocationDottedMissingParentMatchesBareNameForm covers
// locations.go's explicit-fold pattern (ErrParentNotFound).
func TestCreateLocationDottedMissingParentMatchesBareNameForm(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	_, bareErr := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "l-bare-parent", LocationType: "room", ParentName: strptr("ghost-parent"),
	}, all)
	if !errors.Is(bareErr, storage.ErrParentNotFound) {
		t.Fatalf("bare-name missing location parent = %v, want ErrParentNotFound", bareErr)
	}

	_, dottedErr := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "l-dotted-parent", LocationType: "room", ParentName: strptr("$sys.ghost-parent"),
	}, all)
	if !errors.Is(dottedErr, storage.ErrParentNotFound) {
		t.Fatalf("dotted missing location parent = %v, want ErrParentNotFound (same as bare-name form)", dottedErr)
	}
	assertNotLeakedPathNotFound(t, dottedErr)
}

// TestCreateNodeDottedMissingLocationMatchesBareNameForm covers nodes.go's
// nodeLocationID, another explicit-fold call site, using a genuine
// structural miss under a real root ("boi.nope") like the location test.
func TestCreateNodeDottedMissingLocationMatchesBareNameForm(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "boi", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create boi: %v", err)
	}

	_, bareErr := gw.CreateNode(ctx, "", storage.NodeSpec{
		Name: "node-bare-loc", LocationName: strptr("ghost-loc"),
	}, all, all)
	if !errors.Is(bareErr, storage.ErrLocationNotFound) {
		t.Fatalf("bare-name missing node location = %v, want ErrLocationNotFound", bareErr)
	}

	_, dottedErr := gw.CreateNode(ctx, "", storage.NodeSpec{
		Name: "node-dotted-loc", LocationName: strptr("boi.nope"),
	}, all, all)
	if !errors.Is(dottedErr, storage.ErrLocationNotFound) {
		t.Fatalf("dotted missing node location (boi.nope) = %v, want ErrLocationNotFound (same as bare-name form)", dottedErr)
	}
	assertNotLeakedPathNotFound(t, dottedErr)
}

// TestCreateVariableDottedMissingOwnerMatchesBareNameForm covers
// resolveVariableOwner's withoutCandidates-routed pattern, cited in the
// review as its own reachable shape (variables.go:381).
func TestCreateVariableDottedMissingOwnerMatchesBareNameForm(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "boi", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create boi: %v", err)
	}

	_, bareErr := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "v-bare", ValueType: "int", OwnerKind: "location", OwnerName: strptr("ghost-owner"), Value: json.RawMessage(`1`),
	}, all)
	if !errors.Is(bareErr, storage.ErrVariableOwnerNotFound) {
		t.Fatalf("bare-name missing variable owner = %v, want ErrVariableOwnerNotFound", bareErr)
	}

	_, dottedErr := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "v-dotted", ValueType: "int", OwnerKind: "location", OwnerName: strptr("boi.nope"), Value: json.RawMessage(`1`),
	}, all)
	if !errors.Is(dottedErr, storage.ErrVariableOwnerNotFound) {
		t.Fatalf("dotted missing variable owner (boi.nope) = %v, want ErrVariableOwnerNotFound (same as bare-name form)", dottedErr)
	}
	assertNotLeakedPathNotFound(t, dottedErr)
}

// TestRoleOwnerArgPassesThroughAnUnresolvableDottedSystemName covers the
// review's third named shape: role_declarations.go's roleOwnerArg
// deliberately passes an unresolvable owner name through UNRESOLVED (rather
// than erroring early) so the write falls through to the
// system_role_owner_arc_check CHECK constraint, which produces the stable
// ErrRoleRefNotFound sentinel. Before this fix round, a dotted owner name
// hard-errored with *ErrPathNotFound instead of passing through, because
// roleOwnerArg's own errors.Is(err, ErrSystemNotFound) check could not see
// through the unwrapped sentinel.
func TestRoleOwnerArgPassesThroughAnUnresolvableDottedSystemName(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	spec := storage.SystemRoleSpec{Name: "seat", DisplayName: "Seat", Quorum: 1}

	if _, err := gw.SetSystemRole(ctx, "", "system", "ghost-bare-system", spec); !errors.Is(err, storage.ErrRoleRefNotFound) {
		t.Fatalf("bare-name unresolvable system owner = %v, want ErrRoleRefNotFound", err)
	}
	dottedErr := func() error {
		_, err := gw.SetSystemRole(ctx, "", "system", "$sys.ghost-dotted-system", spec)
		return err
	}()
	if !errors.Is(dottedErr, storage.ErrRoleRefNotFound) {
		t.Fatalf("dotted unresolvable system owner = %v, want ErrRoleRefNotFound (same as bare-name form)", dottedErr)
	}
	assertNotLeakedPathNotFound(t, dottedErr)
}

// assertNotLeakedPathNotFound confirms the fold actually REPLACED the
// concrete *ErrPathNotFound rather than merely making errors.Is succeed
// through an Unwrap chain: if this fires, mapRefErr's ErrPathNotFound case
// (matched by errors.As on the concrete type, unconditionally, before any
// entity mapper's own switch) would still intercept the error at the API
// layer and produce the wrong status regardless of what errors.Is reports
// here.
func assertNotLeakedPathNotFound(t *testing.T, err error) {
	t.Helper()
	var leaked *storage.ErrPathNotFound
	if errors.As(err, &leaked) {
		t.Fatalf("error still carries *storage.ErrPathNotFound (%v): mapRefErr would still intercept it as a raw 404 at the API layer", err)
	}
}
