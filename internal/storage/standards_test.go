package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestStandardCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	st, err := gw.CreateStandard(ctx, "", storage.Standard{ID: "kiosk", DisplayName: "Kiosk"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if st.Official {
		t.Fatalf("new standard official=true, want false")
	}
	if _, err := gw.CreateStandard(ctx, "", storage.Standard{ID: "kiosk", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// A variant parents onto an existing standard; an unknown parent is the
	// dedicated sentinel, not a raw FK error.
	variant, err := gw.CreateStandard(ctx, "", storage.Standard{ID: "kiosk-outdoor", DisplayName: "Outdoor Kiosk", ParentStandardID: strptr("kiosk")})
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	if variant.ParentStandardID == nil || *variant.ParentStandardID != "kiosk" {
		t.Fatalf("variant parent = %v, want kiosk", variant.ParentStandardID)
	}
	if _, err := gw.CreateStandard(ctx, "", storage.Standard{ID: "orphan", DisplayName: "Orphan", ParentStandardID: strptr("nope")}); !errors.Is(err, storage.ErrParentStandardNotFound) {
		t.Fatalf("unknown parent err = %v, want ErrParentStandardNotFound", err)
	}
	if err := gw.DeleteStandard(ctx, "", "kiosk-outdoor"); err != nil {
		t.Fatalf("delete variant: %v", err)
	}

	name := "Info Kiosk"
	if _, err := gw.UpdateStandard(ctx, "", "kiosk", storage.StandardPatch{DisplayName: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := gw.GetStandard(ctx, "kiosk")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != name {
		t.Fatalf("display_name = %q, want %q", got.DisplayName, name)
	}

	// A system conforming to kiosk holds the delete off (in use).
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "k1", StandardID: strptr("kiosk")}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if err := gw.DeleteStandard(ctx, "", "kiosk"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete in-use err = %v, want ErrTypeInUse", err)
	}

	// A shipped standard is operator-owned example content, not authoritative, so
	// it is freely editable: that is what makes forking a template into your own
	// standard useful.
	if _, err := gw.UpdateStandard(ctx, "", "meeting-room", storage.StandardPatch{DisplayName: &name}); err != nil {
		t.Fatalf("update a shipped standard: %v, want it editable", err)
	}

	// The official read-only guard still stands for a row that IS official (the
	// canonical catalogs rely on it), so prove the mechanism on one.
	if err := gw.UpsertStandard(ctx, storage.Standard{ID: "canon", Official: true, DisplayName: "Canonical"}); err != nil {
		t.Fatalf("seed an official standard: %v", err)
	}
	if _, err := gw.UpdateStandard(ctx, "", "canon", storage.StandardPatch{DisplayName: &name}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteStandard(ctx, "", "canon"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// Unknown id is ErrTypeNotFound, on both read and delete.
	if _, err := gw.GetStandard(ctx, "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.DeleteStandard(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}
}

// A system conforms to a standard optionally: a one-off carries none, and the
// column round-trips as nil rather than an empty string.
func TestSystemStandardOptional(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	oneOff, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "one-off"}, all)
	if err != nil {
		t.Fatalf("create one-off: %v", err)
	}
	if oneOff.StandardID != nil {
		t.Fatalf("one-off standard = %q, want nil", *oneOff.StandardID)
	}
	conforming, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "boardroom", StandardID: strptr("meeting-room")}, all)
	if err != nil {
		t.Fatalf("create conforming: %v", err)
	}
	if conforming.StandardID == nil || *conforming.StandardID != "meeting-room" {
		t.Fatalf("conforming standard = %v, want meeting-room", conforming.StandardID)
	}

	// The patch retargets the standard; the display_name it does not carry is
	// left alone (the coalesce), which is also the placeholder-order check on the
	// UPDATE.
	display := "Boardroom"
	if _, err := gw.UpdateSystem(ctx, "", "boardroom", storage.SystemPatch{DisplayName: &display}, all, all); err != nil {
		t.Fatalf("update display_name: %v", err)
	}
	after, err := gw.UpdateSystem(ctx, "", "boardroom", storage.SystemPatch{StandardID: strptr("classroom")}, all, all)
	if err != nil {
		t.Fatalf("update standard: %v", err)
	}
	if after.StandardID == nil || *after.StandardID != "classroom" {
		t.Fatalf("patched standard = %v, want classroom", after.StandardID)
	}
	if after.DisplayName != display || after.Name != "boardroom" {
		t.Fatalf("patched row = %+v, want display_name %q and name boardroom kept", after, display)
	}
}
