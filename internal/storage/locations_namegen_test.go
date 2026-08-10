package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationPenIsOperatorOwnedAndFrozenByRename proves the half of #686 that
// lands on locations: the pen exists on this tier and every location holds it,
// because a location has no generator yet. A rename writes false over false,
// which is the point rather than a no-op: the row an operator names TODAY is
// already frozen when #687 gives its type a name rule, so a generator arriving
// later can never re-mint a name someone typed before it existed.
func TestLocationPenIsOperatorOwnedAndFrozenByRename(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "pen-campus", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if l.NameGenerated {
		t.Error("a created location reports NameGenerated = true, want false: nothing generates a location name yet")
	}
	renamed, err := gw.RenameLocation(ctx, "", l.ID, "pen-campus-west", all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.NameGenerated {
		t.Error("a renamed location reports NameGenerated = true, want false: typing a name claims the pen for good")
	}
}

// TestResetLocationNameRefuses pins what the verb does on this tier today: it
// exists, it is gated exactly as :rename is, and it refuses with the reason
// (ErrLocationTypeNoNameRule) rather than reporting a reset that did not
// happen. A location_type carries no stem, so there is nothing to regenerate
// from until #687's per-type name rule, and this is the test that flips when it
// lands.
func TestResetLocationNameRefuses(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "reset-campus", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := gw.ResetLocationName(ctx, "", l.ID, all, all); !errors.Is(err, storage.ErrLocationTypeNoNameRule) {
		t.Fatalf("ResetLocationName = %v, want ErrLocationTypeNoNameRule", err)
	}
	// The name is untouched by the refusal: a refusal is not a partial write.
	after, err := gw.GetLocation(ctx, l.ID, all)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.Name != "reset-campus" {
		t.Fatalf("name after a refused reset = %q, want reset-campus", after.Name)
	}

	// An unknown location is the ordinary non-disclosing 404, resolved before
	// the refusal, so the verb never reports a type's shortcoming about a row
	// the caller cannot even see.
	if _, err := gw.ResetLocationName(ctx, "", "no-such-location", all, all); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("ResetLocationName on an unknown location = %v, want ErrLocationNotFound", err)
	}
}
