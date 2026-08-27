package storage_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestCreateGeneratesFromTypeStem proves the base rule: a create with no name
// mints "<stem>-<n>" from the product's component_type, ordinal always
// present, incrementing per sibling in the same placement bucket.
// boreal-edge-55 classifies under component_type "display" (stem "display").
func TestCreateGeneratesFromTypeStem(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	edge55 := "boreal-edge-55"
	first, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &edge55}, all, all, all, all)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if first.Name != "display-1" {
		t.Fatalf("first generated name = %q, want display-1", first.Name)
	}
	if !first.NameGenerated {
		t.Fatalf("first.NameGenerated = false, want true")
	}

	second, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &edge55}, all, all, all, all)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if second.Name != "display-2" {
		t.Fatalf("second generated name = %q, want display-2", second.Name)
	}

	// A DIFFERENT placement bucket restarts the ordinal at 1: the scope is
	// per-placement, not fleet-wide.
	root := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "namegen-root"}, all)
	third, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &edge55, ParentName: &root.Name}, all, all, all, all)
	if err != nil {
		t.Fatalf("create third (under a parent): %v", err)
	}
	if third.Name != "display-1" {
		t.Fatalf("third generated name (new placement bucket) = %q, want display-1", third.Name)
	}

	// The LOCATION bucket (unparented, placed) is its own scope too, not
	// just the parent and orphan buckets exercised above.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "namegen-loc", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create location: %v", err)
	}
	locName := "namegen-loc"
	fourth, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &edge55, LocationName: &locName}, all, all, all, all)
	if err != nil {
		t.Fatalf("create fourth (at a location): %v", err)
	}
	if fourth.Name != "display-1" {
		t.Fatalf("fourth generated name (location bucket) = %q, want display-1", fourth.Name)
	}
	fifth, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &edge55, LocationName: &locName}, all, all, all, all)
	if err != nil {
		t.Fatalf("create fifth (same location): %v", err)
	}
	if fifth.Name != "display-2" {
		t.Fatalf("fifth generated name (same location bucket) = %q, want display-2", fifth.Name)
	}
}

// TestSubtypeInheritsStem proves a product classified under a CHILD type with
// no stem of its own (interactive-display, a child of display in the seed
// taxonomy) inherits the parent's stem, exactly as ResolveTypeFacts walks.
func TestSubtypeInheritsStem(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	prod, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "subtype-test-panel", Label: "Subtype Test Panel", Kind: "device", ComponentType: "interactive-display",
	})
	if err != nil {
		t.Fatalf("create product under interactive-display: %v", err)
	}

	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &prod.Name}, all, all, all, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Name != "display-1" {
		t.Fatalf("name for an interactive-display instance = %q, want display-1 (inherited stem)", c.Name)
	}
}

// TestRenameFreezes proves :rename's name_generated=false is permanent: a
// generated component that gets an operator-typed name never has that name
// disturbed again, even by an act (a product reclassify) that would
// otherwise recompute a still-platform-owned name.
func TestRenameFreezes(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	edge55 := "boreal-edge-55"
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &edge55}, all, all, all, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !c.NameGenerated {
		t.Fatalf("precondition: NameGenerated = false, want true")
	}

	renamed, err := gw.RenameComponent(ctx, "", c.ID, "operator-chosen", all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "operator-chosen" {
		t.Fatalf("name after rename = %q, want operator-chosen", renamed.Name)
	}
	if renamed.NameGenerated {
		t.Fatalf("NameGenerated after rename = true, want false (an operator rename claims the pen)")
	}

	// A reclassify would normally recompute a still-generated name; it must
	// leave this one alone now that the operator owns it.
	mic := "lyra-arc-a2"
	after, err := gw.UpdateComponent(ctx, "", c.ID, storage.ComponentPatch{ProductName: &mic}, all, all)
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if after.Name != "operator-chosen" {
		t.Fatalf("name after reclassify of a renamed component = %q, want unchanged operator-chosen", after.Name)
	}
}

// TestResetReturnsPen proves :resetName hands the pen back: an operator-typed
// name is regenerated and marked name_generated again. It also proves the
// reverse round trip (generate, rename to freeze, reset to hand the pen back
// again) lands on a fresh ordinal in the same bucket, not the frozen name.
func TestResetReturnsPen(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	edge55 := "boreal-edge-55"

	// An operator-typed component from the start.
	typed, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "operator-panel", ProductName: &edge55}, all, all, all, all)
	if err != nil {
		t.Fatalf("create typed: %v", err)
	}
	if typed.NameGenerated {
		t.Fatalf("precondition: an explicitly-named create reports NameGenerated = true")
	}

	reset, err := gw.ResetComponentName(ctx, "", typed.ID, all, all)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.Name != "display-1" {
		t.Fatalf("name after reset = %q, want display-1", reset.Name)
	}
	if !reset.NameGenerated {
		t.Fatalf("NameGenerated after reset = false, want true (the platform holds the pen again)")
	}

	// Resetting again is idempotent in effect (same stem, same bucket, its
	// own old name freed by the update in the same statement): still
	// display-1, not display-2.
	resetAgain, err := gw.ResetComponentName(ctx, "", reset.ID, all, all)
	if err != nil {
		t.Fatalf("reset again: %v", err)
	}
	if resetAgain.Name != "display-1" {
		t.Fatalf("name after a second reset = %q, want display-1 (its own old name is excluded from the scan)", resetAgain.Name)
	}

	// A missing component is the ordinary not-found.
	if _, err := gw.ResetComponentName(ctx, "", "no-such-component", all, all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Fatalf("reset of a missing component = %v, want ErrComponentNotFound", err)
	}
}

// TestReclassifyRecomputes proves a product reclassify (UpdateComponent's
// Product field) recomputes a still-platform-owned name to the NEW stem,
// within the SAME placement, while leaving an operator-typed name alone
// (covered by TestRenameFreezes) and never touching label.
func TestReclassifyRecomputes(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	edge55 := "boreal-edge-55"
	mic := "lyra-arc-a2"

	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &edge55, Label: "My Display"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Name != "display-1" {
		t.Fatalf("precondition name = %q, want display-1", c.Name)
	}

	after, err := gw.UpdateComponent(ctx, "", c.ID, storage.ComponentPatch{ProductName: &mic}, all, all)
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if after.Name != "mic-1" {
		t.Fatalf("name after reclassify = %q, want mic-1 (lyra-arc-a2 classifies under ceiling-mic)", after.Name)
	}
	if !after.NameGenerated {
		t.Fatalf("NameGenerated after reclassify = false, want true")
	}
	if after.Label != "My Display" {
		t.Fatalf("label after reclassify = %q, want unchanged %q (never generated, never touched)", after.Label, "My Display")
	}

	// A patch that does not touch product at all (only label) must not
	// recompute the name, even though the component is still platform-owned.
	untouched, err := gw.UpdateComponent(ctx, "", after.ID, storage.ComponentPatch{Label: strptr("Renamed Label")}, all, all)
	if err != nil {
		t.Fatalf("label-only patch: %v", err)
	}
	if untouched.Name != "mic-1" {
		t.Fatalf("name after a label-only patch = %q, want unchanged mic-1", untouched.Name)
	}
}

// TestComponentTypeStemEditDoesNotRecomputeExistingNames proves what
// deliberately does NOT happen: editing a component_type's stem after
// components already generated names from it never touches those existing
// component rows. Nothing wires a component_type write to a component name
// recompute, only a component-level trigger does (a move or a reclassify of
// THAT component, UpdateComponent/MoveComponent's own recompute above); this
// pins that absence so a future "helpful" cascade from the registry side
// does not silently become the norm. Uses a CUSTOM component_type, not a
// seeded official one: official rows are read-only (ErrTypeOfficial), so
// this is the only way to reach UpdateComponentType's stem branch at all.
func TestComponentTypeStemEditDoesNotRecomputeExistingNames(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ct, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "registry-edit-type", Label: "Registry Edit Type", Stem: strp("orig-stem"),
	})
	if err != nil {
		t.Fatalf("create custom component_type: %v", err)
	}
	prod, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "registry-edit-product", Label: "Registry Edit Product", Kind: "device", ComponentType: ct.Name,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &prod.Name}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if c.Name != "orig-stem-1" {
		t.Fatalf("precondition name = %q, want orig-stem-1", c.Name)
	}

	newStem := "changed-stem"
	if _, err := gw.UpdateComponentType(ctx, "", ct.Name, storage.ComponentTypePatch{Stem: &newStem}); err != nil {
		t.Fatalf("update the type's stem: %v", err)
	}

	got, err := gw.GetComponent(ctx, c.ID, all)
	if err != nil {
		t.Fatalf("get component after registry edit: %v", err)
	}
	if got.Name != "orig-stem-1" {
		t.Fatalf("name after an unrelated component_type stem edit = %q, want unchanged orig-stem-1 (a registry edit recomputes nothing)", got.Name)
	}
	if !got.NameGenerated {
		t.Fatalf("NameGenerated after registry edit = false, want unchanged true")
	}
}

// TestConcurrentCreateSerialisesOrdinals fires two concurrent creates for the
// same stem in the same placement bucket, no name given to either. The
// transaction-scoped advisory lock must serialize the two ordinal picks (no
// retry, no duplicate, no error): the whole point of keying the lock on
// stem+scope rather than trusting the unique index to sort it out after the
// fact via a 23505 retry.
func TestConcurrentCreateSerialisesOrdinals(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	edge55 := "boreal-edge-55"
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]*storage.Component, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &edge55}, all, all, all, all)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent create %d: %v", i, err)
		}
	}
	names := map[string]bool{results[0].Name: true, results[1].Name: true}
	if len(names) != 2 {
		t.Fatalf("concurrent creates produced names %q and %q, want two distinct ordinals", results[0].Name, results[1].Name)
	}
	if !names["display-1"] || !names["display-2"] {
		t.Fatalf("concurrent creates produced %q and %q, want exactly {display-1, display-2}", results[0].Name, results[1].Name)
	}
}
