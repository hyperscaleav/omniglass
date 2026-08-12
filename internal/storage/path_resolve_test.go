package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// addressFixture builds a small location/component/system tree exactly the
// shape the task brief's own examples use, so the integration tests below
// read against real Postgres, not a fake:
//
//	boi (campus, root)
//	  17c (building, under boi)
//	    415a (room, under 17c)
//	      display-1 (component, placed at 415a, top-level)
//	      dsp-1 (component, placed at 415a, top-level)
//	        dante-card-1 (component, child of dsp-1)
//	    av (system, placed at 17c, top-level)
//
// campus/building/room is the seeded allowed_parent_types chain
// (internal/seed/location_types.yaml); a flat "campus" per level, as several
// other fixtures in this package use, would 422 on the second nesting.
type addressFixture struct {
	boi, c17, r415a            *storage.Location
	display1, dsp1, danteCard1 *storage.Component
	av                         *storage.System
}

func buildAddressFixture(t *testing.T, gw storage.Gateway) addressFixture {
	t.Helper()
	ctx := context.Background()
	all := scope.Set{All: true}

	boi, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "boi", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create boi: %v", err)
	}
	c17, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "17c", LocationType: "building", ParentName: &boi.Name,
	}, all)
	if err != nil {
		t.Fatalf("create 17c: %v", err)
	}
	r415a, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "415a", LocationType: "room", ParentName: &c17.Name,
	}, all)
	if err != nil {
		t.Fatalf("create 415a: %v", err)
	}

	display1, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "display-1", LocationName: &r415a.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create display-1: %v", err)
	}
	dsp1, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "dsp-1", LocationName: &r415a.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create dsp-1: %v", err)
	}
	danteCard1, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "dante-card-1", ParentName: &dsp1.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create dante-card-1: %v", err)
	}

	av, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "av", LocationName: &c17.Name,
	}, all, all)
	if err != nil {
		t.Fatalf("create av: %v", err)
	}

	return addressFixture{
		boi: boi, c17: c17, r415a: r415a,
		display1: display1, dsp1: dsp1, danteCard1: danteCard1,
		av: av,
	}
}

// TestResolvePathLocationPlane resolves a nested location address with no
// accessor: the root plane. Reverting resolvePath's root-walk (falling back
// to a literal `where name = $1`) fails this, because "boi.17c.415a" is
// never a location's actual name column value.
func TestResolvePathLocationPlane(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	all := scope.Set{All: true}

	got, err := gw.GetLocation(context.Background(), "boi.17c.415a", all)
	if err != nil {
		t.Fatalf("GetLocation(boi.17c.415a) = %v", err)
	}
	if got.ID != fx.r415a.ID {
		t.Errorf("resolved id = %s, want %s (415a)", got.ID, fx.r415a.ID)
	}
}

// TestResolvePathComponentLocationBucket is the task brief's own full
// component path.
func TestResolvePathComponentLocationBucket(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	all := scope.Set{All: true}

	got, err := gw.GetComponent(context.Background(), "boi.17c.415a.$comp.display-1", all)
	if err != nil {
		t.Fatalf("GetComponent(boi.17c.415a.$comp.display-1) = %v", err)
	}
	if got.ID != fx.display1.ID {
		t.Errorf("resolved id = %s, want %s (display-1)", got.ID, fx.display1.ID)
	}
}

// TestResolvePathSubComponent is the task brief's sub-component path: a
// second $comp-plane hop, via component_parent_name_key rather than
// component_location_name_key.
func TestResolvePathSubComponent(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	all := scope.Set{All: true}

	got, err := gw.GetComponent(context.Background(), "boi.17c.415a.$comp.dsp-1.dante-card-1", all)
	if err != nil {
		t.Fatalf("GetComponent(boi.17c.415a.$comp.dsp-1.dante-card-1) = %v", err)
	}
	if got.ID != fx.danteCard1.ID {
		t.Errorf("resolved id = %s, want %s (dante-card-1)", got.ID, fx.danteCard1.ID)
	}
}

// TestResolvePathSystemPlane covers the $sys accessor.
func TestResolvePathSystemPlane(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	all := scope.Set{All: true}

	got, err := gw.GetSystem(context.Background(), "boi.17c.$sys.av", all)
	if err != nil {
		t.Fatalf("GetSystem(boi.17c.$sys.av) = %v", err)
	}
	if got.ID != fx.av.ID {
		t.Errorf("resolved id = %s, want %s (av)", got.ID, fx.av.ID)
	}
}

// TestResolvePathStructuralMissIsErrPathNotFound proves a mid-walk miss (a
// segment that does not exist) is ErrPathNotFound, not a 500 and not a
// silent wrong answer, when the caller has every scope in the world to see
// the row if it existed.
func TestResolvePathStructuralMissIsErrPathNotFound(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	buildAddressFixture(t, gw)
	all := scope.Set{All: true}

	_, err := gw.GetComponent(context.Background(), "boi.17c.999z.$comp.display-1", all)
	var notFound *storage.ErrPathNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("GetComponent with a missing segment = %v, want *storage.ErrPathNotFound", err)
	}
}

// TestResolvePathKindMismatchIsErrPathNotFound proves a location address
// (no accessor) handed to the component resolver is refused the same way a
// missing segment is, rather than silently resolving to the location row
// under a different table's cfg (which the shared column list would let
// scan corrupt, if the mismatch were not caught first).
func TestResolvePathKindMismatchIsErrPathNotFound(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	buildAddressFixture(t, gw)
	all := scope.Set{All: true}

	_, err := gw.GetComponent(context.Background(), "boi.17c.415a", all)
	var notFound *storage.ErrPathNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("GetComponent(a location address) = %v, want *storage.ErrPathNotFound", err)
	}
}

// TestResolvePathOutOfScopeIsNonDisclosing proves the scope check that runs
// AFTER a dotted address resolves structurally behaves exactly like it does
// for a uuid or a bare name: a caller scoped to a different subtree gets the
// entity's own non-disclosing not-found, never storage.ErrPathNotFound
// (which would leak that the address resolved to a real, merely
// out-of-scope, row) and never a 500.
func TestResolvePathOutOfScopeIsNonDisclosing(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	ctx := context.Background()

	// A caller scoped only to a sibling campus's own subtree: unrelated to
	// boi/17c/415a entirely.
	other, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "elsewhere", LocationType: "campus"}, scope.Set{All: true})
	if err != nil {
		t.Fatalf("create elsewhere: %v", err)
	}
	narrow := scope.Set{IDs: []string{other.ID}}

	_, err = gw.GetComponent(ctx, "boi.17c.415a.$comp.display-1", narrow)
	if !errors.Is(err, storage.ErrComponentNotFound) {
		t.Fatalf("GetComponent from an unrelated scope = %v, want storage.ErrComponentNotFound", err)
	}
	var pathNotFound *storage.ErrPathNotFound
	if errors.As(err, &pathNotFound) {
		t.Fatalf("GetComponent from an unrelated scope leaked *storage.ErrPathNotFound (%v) instead of folding into the entity's own non-disclosing sentinel", err)
	}
	_ = fx
}

// TestResolvePathUnderNonAllScope closes the coverage gap task-12-review.md
// named: TestResolvePathOutOfScopeIsNonDisclosing proves non-disclosure holds
// under a scope that can never admit the target (a location root, which
// cannot appear in a component's own parent_id ancestor chain, per
// inScopeTree's own doc), but that test would pass even if resolvePath's
// wiring into loadByRef were entirely absent, because the literal address
// string never matches a `name` column value either way. What was never
// exercised anywhere in the suite is a non-All principal SCOPED TO A REAL
// SUBTREE of the address's own target tier resolving successfully, and the
// SAME scope correctly denying a sibling outside it: exactly the shape that
// produced two criticals in Task 11 which three green full-suite runs missed
// (progress.md: "cross-tier scope filtering denies EVERY non-all-scoped
// caller", found only once a scoped-caller write-path test existed).
//
// The scope root is dsp-1's own id (a component-tier grant, the tier
// dante-card-1 and display-1 both belong to), not a location id: component
// scope is own-tier only today (no location-to-component cascade), so a
// location-rooted scope could never positively admit either of these and
// would prove nothing about the ancestor walk.
func TestResolvePathUnderNonAllScope(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	ctx := context.Background()
	scopedToDsp1 := scope.Set{IDs: []string{fx.dsp1.ID}}

	// Positive control: dante-card-1 is dsp-1's own child (component_parent_
	// name_key), so a scope rooted at dsp-1 covers it via the ancestor walk,
	// and the dotted sub-component address must still resolve to the right
	// row, not merely fail to disclose.
	got, err := gw.GetComponent(ctx, "boi.17c.415a.$comp.dsp-1.dante-card-1", scopedToDsp1)
	if err != nil {
		t.Fatalf("GetComponent(dante-card-1) under a scope rooted at its own parent = %v, want success", err)
	}
	if got.ID != fx.danteCard1.ID {
		t.Errorf("resolved id = %s, want %s (dante-card-1)", got.ID, fx.danteCard1.ID)
	}

	// Negative control, same scope: display-1 is dsp-1's SIBLING (both placed
	// at 415a, neither the other's ancestor), so the identical scope that
	// just admitted dante-card-1 must deny display-1, non-disclosingly.
	_, err = gw.GetComponent(ctx, "boi.17c.415a.$comp.display-1", scopedToDsp1)
	if !errors.Is(err, storage.ErrComponentNotFound) {
		t.Fatalf("GetComponent(display-1) under a scope rooted at its sibling's parent = %v, want storage.ErrComponentNotFound", err)
	}
	var pathNotFound *storage.ErrPathNotFound
	if errors.As(err, &pathNotFound) {
		t.Fatalf("GetComponent(display-1) leaked *storage.ErrPathNotFound (%v) instead of the non-disclosing sentinel", err)
	}
}

// TestResolvePathDisambiguatesWhatABareNameCannot is the point of the whole
// feature: two locations legitimately share a name (component_parent_name_key
// / here location_parent_name_key, #627 Task 10) under different parents, so
// the bare name is genuinely ambiguous, but a dotted address naming one
// parent or the other resolves deterministically to the right row. If
// resolvePath's wiring into loadByRef were reverted, both dotted lookups
// below would instead try a literal `where name = $1` against a name that
// never exists as a column value and 404, rather than distinguishing the two
// rooms the way the bare-name assertion right above them proves they need to
// be distinguished.
func TestResolvePathDisambiguatesWhatABareNameCannot(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	bldgA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "bldg-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("bldg-a: %v", err)
	}
	bldgB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "bldg-b", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("bldg-b: %v", err)
	}
	roomInA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "216b", LocationType: "room", ParentName: &bldgA.Name,
	}, all)
	if err != nil {
		t.Fatalf("bldg-a/216b: %v", err)
	}
	roomInB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "216b", LocationType: "room", ParentName: &bldgB.Name,
	}, all)
	if err != nil {
		t.Fatalf("bldg-b/216b: %v", err)
	}
	if roomInA.ID == roomInB.ID {
		t.Fatal("the two rooms did not actually land on different rows")
	}

	// The bare name alone cannot tell them apart.
	_, err = gw.GetLocation(ctx, "216b", all)
	var ambig *storage.ErrAmbiguousName
	if !errors.As(err, &ambig) {
		t.Fatalf("GetLocation(216b) = %v, want *storage.ErrAmbiguousName", err)
	}

	// A dotted address naming the parent resolves each one deterministically.
	gotA, err := gw.GetLocation(ctx, "bldg-a.216b", all)
	if err != nil {
		t.Fatalf("GetLocation(bldg-a.216b) = %v", err)
	}
	if gotA.ID != roomInA.ID {
		t.Errorf("bldg-a.216b resolved to %s, want %s", gotA.ID, roomInA.ID)
	}
	gotB, err := gw.GetLocation(ctx, "bldg-b.216b", all)
	if err != nil {
		t.Fatalf("GetLocation(bldg-b.216b) = %v", err)
	}
	if gotB.ID != roomInB.ID {
		t.Errorf("bldg-b.216b resolved to %s, want %s", gotB.ID, roomInB.ID)
	}
}
