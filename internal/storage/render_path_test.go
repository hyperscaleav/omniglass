package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Review finding 2 (task-15-review.md #1): the ten pure render tests
// (render_test.go) are all fed by parseOrFail, a test-local reimplementation
// of the segment shape that never calls PathOf, ancestorNames, or
// planeRootLocationID. A reversed ancestor order or a plane root read from
// the wrong location_id would leave every one of those tests green while
// corrupting path/path_segments/renders on every real GET and LIST. These
// tests close that gap by asserting PathOf's actual output (via the same
// GetComponent/GetSystem/GetLocation entry points scopedGet calls in
// production, since PathOf itself takes unexported parameter types and is
// unreachable from this external test package) against the real fixture
// tree, then round-tripping the returned path back through the resolver.
//
// danteCard1 is the case that most directly proves planeRootLocationID
// walks to the PLANE ROOT's location, not the target row's own: it has no
// location_id of its own at all (buildAddressFixture places it under dsp-1
// by ParentName only), so reading its own location_id would produce an
// empty Root instead of [boi 17c 415a].

func TestPathOfComponentPlacedAtALocation(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	ctx := context.Background()
	all := scope.Set{All: true}

	got, err := gw.GetComponent(ctx, fx.display1.ID, all)
	if err != nil {
		t.Fatalf("GetComponent(display1.ID) = %v", err)
	}
	wantSegs := []string{"boi", "17c", "415a", "$comp", "display-1"}
	if !equalSegs(got.PathSegments, wantSegs) {
		t.Fatalf("PathSegments = %v, want %v", got.PathSegments, wantSegs)
	}
	wantPath := "boi.17c.415a.$comp.display-1"
	if got.Path != wantPath {
		t.Errorf("Path = %q, want %q", got.Path, wantPath)
	}

	// Round trip: the exact inverse of resolvePath, made into an assertion
	// rather than left as path.go's own doc comment.
	back, err := gw.GetComponent(ctx, got.Path, all)
	if err != nil {
		t.Fatalf("GetComponent(%q) = %v", got.Path, err)
	}
	if back.ID != fx.display1.ID {
		t.Errorf("round trip resolved id = %s, want %s", back.ID, fx.display1.ID)
	}
}

// TestPathOfNestedComponentCrossesIntoTheLocationTree is the regression case
// named above: dante-card-1 has no location_id of its own, so its Root can
// only be correct if planeRootLocationID walked up to dsp-1 (its plane
// root, parent_id null) and read THAT row's location_id, exactly mirroring
// resolvePlaneRoot's own forward-direction bind.
func TestPathOfNestedComponentCrossesIntoTheLocationTree(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	ctx := context.Background()
	all := scope.Set{All: true}

	got, err := gw.GetComponent(ctx, fx.danteCard1.ID, all)
	if err != nil {
		t.Fatalf("GetComponent(danteCard1.ID) = %v", err)
	}
	wantSegs := []string{"boi", "17c", "415a", "$comp", "dsp-1", "dante-card-1"}
	if !equalSegs(got.PathSegments, wantSegs) {
		t.Fatalf("PathSegments = %v, want %v (a wrong plane-root lookup would leave Root empty or wrong)", got.PathSegments, wantSegs)
	}

	back, err := gw.GetComponent(ctx, got.Path, all)
	if err != nil {
		t.Fatalf("GetComponent(%q) = %v", got.Path, err)
	}
	if back.ID != fx.danteCard1.ID {
		t.Errorf("round trip resolved id = %s, want %s", back.ID, fx.danteCard1.ID)
	}

	// The dash render is asserted here too (not just in the pure suite),
	// because render_test.go's inputs never come from a real row: this is
	// the one place the render functions run against PathOf's actual output.
	wantDash := "boi-17c-415a-dsp-1-dante-card-1"
	if got.Renders.Dash != wantDash {
		t.Errorf("Renders.Dash = %+v, want %q", got.Renders, wantDash)
	}
}

// TestPathOfSystemPlacedAtALocation covers the $sys accessor plane, the
// second of the two non-location tables PathOf's plane-root branch serves.
func TestPathOfSystemPlacedAtALocation(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	ctx := context.Background()
	all := scope.Set{All: true}

	got, err := gw.GetSystem(ctx, fx.av.ID, all)
	if err != nil {
		t.Fatalf("GetSystem(av.ID) = %v", err)
	}
	wantSegs := []string{"boi", "17c", "$sys", "av"}
	if !equalSegs(got.PathSegments, wantSegs) {
		t.Fatalf("PathSegments = %v, want %v", got.PathSegments, wantSegs)
	}

	back, err := gw.GetSystem(ctx, got.Path, all)
	if err != nil {
		t.Fatalf("GetSystem(%q) = %v", got.Path, err)
	}
	if back.ID != fx.av.ID {
		t.Errorf("round trip resolved id = %s, want %s", back.ID, fx.av.ID)
	}
}

// TestPathOfLocationHasNoAccessor covers the plain location-tree address
// (AddressLocation): no accessor, and PathOf's own short-circuit for
// locationTable (path.go's tbl == locationTable branch), never reached by
// the two tests above.
func TestPathOfLocationHasNoAccessor(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	ctx := context.Background()
	all := scope.Set{All: true}

	got, err := gw.GetLocation(ctx, fx.r415a.ID, all)
	if err != nil {
		t.Fatalf("GetLocation(r415a.ID) = %v", err)
	}
	wantSegs := []string{"boi", "17c", "415a"}
	if !equalSegs(got.PathSegments, wantSegs) {
		t.Fatalf("PathSegments = %v, want %v", got.PathSegments, wantSegs)
	}

	back, err := gw.GetLocation(ctx, got.Path, all)
	if err != nil {
		t.Fatalf("GetLocation(%q) = %v", got.Path, err)
	}
	if back.ID != fx.r415a.ID {
		t.Errorf("round trip resolved id = %s, want %s", back.ID, fx.r415a.ID)
	}

	// The root location itself: an empty ancestor walk, one segment, no
	// accessor. The shallow end of the same branch.
	root, err := gw.GetLocation(ctx, fx.boi.ID, all)
	if err != nil {
		t.Fatalf("GetLocation(boi.ID) = %v", err)
	}
	if !equalSegs(root.PathSegments, []string{"boi"}) {
		t.Fatalf("PathSegments = %v, want [boi]", root.PathSegments)
	}
}

// TestListComponentsSkipsAbbrevGetCompactsFully is review finding 3
// (task-15-review.md #2): attachComponentPaths' abbrev resolution
// (componentTypeIDForProduct plus resolveTypeFacts' own ancestor walk) costs
// two more queries per distinct product on the page, for renders.bare, a
// field no console surface reads. ListComponents must skip that resolution
// (RenderBare falls back to its own no-abbrev concatenation, still a real
// value, just not abbrev-compacted); GetComponent, the one-row case, still
// resolves and substitutes it in full. Both come from the identical scanned
// row, so a difference here can only be the attach's own full flag, not
// fixture drift.
func TestListComponentsSkipsAbbrevGetCompactsFully(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	fx := buildAddressFixture(t, gw)
	ctx := context.Background()
	all := scope.Set{All: true}

	list, err := gw.ListComponents(ctx, all)
	if err != nil {
		t.Fatalf("ListComponents: %v", err)
	}
	var listed *storage.Component
	for i := range list {
		if list[i].ID == fx.display1.ID {
			listed = &list[i]
		}
	}
	if listed == nil {
		t.Fatalf("display-1 not found in ListComponents")
	}
	wantListBare := "boi17c415adisplay-1" // no abbrev substitution: the raw segment concatenated as-is
	if listed.Renders.Bare != wantListBare {
		t.Errorf("ListComponents Renders.Bare = %q, want %q (LIST must not pay the abbrev queries)", listed.Renders.Bare, wantListBare)
	}

	// display-1 is a HAND-TYPED name on a component whose type is
	// generic-device (stem "device", abbrev "dev"), so no substitution is
	// legitimate on it in either direction: the type never minted this name
	// (#654, and since #681 the row says so itself by carrying no ordinal).
	// Asserting that on the GET path too keeps this test honest, since the
	// compaction it exercises below has to come from a name the type actually
	// owns.
	gotHand, err := gw.GetComponent(ctx, fx.display1.ID, all)
	if err != nil {
		t.Fatalf("GetComponent(display1.ID) = %v", err)
	}
	if want := "boi17c415adisplay-1"; gotHand.Renders.Bare != want {
		t.Errorf("GetComponent Renders.Bare = %q, want %q (a foreign stem is never compacted)", gotHand.Renders.Bare, want)
	}
	// Dash and the address itself are NOT gated: they cost nothing beyond
	// PathOf, which LIST already pays for pathRender.
	if listed.Renders.Dash == "" {
		t.Error("ListComponents Renders.Dash is empty, want the dash render (not gated by full)")
	}
	if listed.Path == "" {
		t.Error("ListComponents Path is empty, want the dotted address (not gated by full)")
	}

	// A component the type actually minted, which is what makes the compaction
	// legitimate: generic-device's seeded stem is "device" and its abbrev is
	// "dev", so a generated name here is "device-1", it records ordinal 1, and
	// it compacts to dev1 on a GET while staying whole on a LIST. This is the
	// pair the test exists to tell apart.
	//
	// The name is left empty rather than typed (#681). Typing "device-1" used
	// to be the only way to put a name of this shape in this room, and it
	// passed because the render matched the type's stem against the string.
	// The render reads the row's own ordinal now, and a typed name has none,
	// so a hand-typed "device-1" is a row the platform never named and is left
	// whole. Generating it asks for what this test always meant.
	owned, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		LocationName: &fx.r415a.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create device-1: %v", err)
	}
	if owned.Name != "device-1" {
		t.Fatalf("generated name = %q, want device-1 (generic-device's seeded stem)", owned.Name)
	}

	list2, err := gw.ListComponents(ctx, all)
	if err != nil {
		t.Fatalf("ListComponents (owned): %v", err)
	}
	for i := range list2 {
		if list2[i].ID == owned.ID {
			if want := "boi17c415adevice-1"; list2[i].Renders.Bare != want {
				t.Errorf("ListComponents Renders.Bare = %q, want %q (LIST must not pay the abbrev queries)", list2[i].Renders.Bare, want)
			}
		}
	}

	got, err := gw.GetComponent(ctx, owned.ID, all)
	if err != nil {
		t.Fatalf("GetComponent(device-1) = %v", err)
	}
	wantGetBare := "boi17c415adev1" // generic-device's seeded stem is "device", abbrev "dev"
	if got.Renders.Bare != wantGetBare {
		t.Errorf("GetComponent Renders.Bare = %q, want %q", got.Renders.Bare, wantGetBare)
	}
}

func equalSegs(got, want []string) bool {
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
