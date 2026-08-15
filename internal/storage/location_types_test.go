package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestLocationTypeCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a custom type; it is official=false.
	lt, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: "wing", Label: "Wing", Icon: "layers"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if lt.Official {
		t.Fatalf("new type official=true, want false")
	}

	// Duplicate id is ErrTypeExists.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: "wing", Label: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// "root" is reserved for the allowed_parent_types sentinel: creating a type
	// with that id is refused, so a real type can never collide with it.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: "root", Label: "Root"}); !errors.Is(err, storage.ErrReservedTypeID) {
		t.Fatalf("create root-id type err = %v, want ErrReservedTypeID", err)
	}

	// Update mutates label; icon unchanged when omitted.
	name := "West Wing"
	if _, err := gw.UpdateLocationType(ctx, "", "wing", storage.LocationTypePatch{Label: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// A shipped location type is still editable, by a different mechanism: the
	// estate shapes its own place vocabulary through a FORK now rather than by
	// owning the row (#703, ADR-0095), and TestShippedLocationTypeForksAndRestores
	// holds the fork's own behavior.
	if _, err := gw.UpdateLocationType(ctx, "", "campus", storage.LocationTypePatch{Label: &name}); err != nil {
		t.Fatalf("update a shipped location type: %v, want it editable", err)
	}

	// The two halves of the official guard, converted rather than dropped. An
	// UPDATE of an official row used to be ErrTypeOfficial and is now a FORK
	// (#703, ADR-0095): the row is untouched and the operator's version resolves
	// over it. A DELETE is still refused, which is where the guard belongs, since
	// a fork is an overlay and not ownership.
	if err := gw.UpsertLocationType(ctx, storage.LocationType{Name: "canon", Official: true, Label: "Canonical"}); err != nil {
		t.Fatalf("seed an official location type: %v", err)
	}
	forked, err := gw.UpdateLocationType(ctx, "", "canon", storage.LocationTypePatch{Label: &name})
	if err != nil {
		t.Fatalf("update official err = %v, want a fork", err)
	}
	if !forked.Forked || forked.Label != name {
		t.Fatalf("update official gave %+v, want the patched value marked forked", forked)
	}
	if err := gw.DeleteLocationType(ctx, "", "canon"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// In-use delete is refused: place a location of type wing, then delete.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "w1", LocationType: "wing"}, all); err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := gw.DeleteLocationType(ctx, "", "wing"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete in-use err = %v, want ErrTypeInUse", err)
	}

	// Unknown id is ErrTypeNotFound.
	if err := gw.DeleteLocationType(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}
}

// TestCreateLocationTypeReturnsAndAuditsItsID is the regression for a defect
// this registry carried from the start and #687 surfaced by editing the
// statement: the insert said `returning id` and ran through Exec, so the value
// was discarded and nothing ever assigned it. A create answered with an empty
// id (the field its own wire schema calls "the stable handle that survives a
// rename"), and the audit row was keyed on the empty string, which commits
// because resource_id is text.
//
// The static audit-key guard could not catch it: it reads the EXPRESSION at the
// call site, and `lt.ID` is exactly the shape it wants. Only a round trip can
// tell that the expression was never filled in.
func TestCreateLocationTypeReturnsAndAuditsItsID(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	lt, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: "wing", Label: "Wing"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, parseErr := uuid.Parse(lt.ID); parseErr != nil {
		t.Fatalf("the created type's id = %q, want the uuid the database minted: %v", lt.ID, parseErr)
	}
	// And it is the id the registry actually holds, not merely a well-shaped one.
	types, err := gw.ListLocationTypes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var stored string
	for _, t := range types {
		if t.Name == "wing" {
			stored = t.ID
		}
	}
	if stored != lt.ID {
		t.Fatalf("create returned id %q, the registry holds %q", lt.ID, stored)
	}

	entries, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "location_type"})
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	var keyed int
	for _, e := range entries {
		if e.Verb == "create" && e.ResourceID == lt.ID {
			keyed++
		}
		if e.ResourceID == "" {
			t.Errorf("a %s audit row for location_type is keyed on the empty string", e.Verb)
		}
	}
	if keyed != 1 {
		t.Fatalf("%d location_type create audit rows are keyed on %q, want 1", keyed, lt.ID)
	}
}
