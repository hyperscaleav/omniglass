package storage_test

import (
	"context"
	"encoding/json"
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

	st, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "kiosk", Label: "Kiosk"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if st.Official {
		t.Fatalf("new standard official=true, want false")
	}
	if _, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "kiosk", Label: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// A variant parents onto an existing standard; an unknown parent is the
	// dedicated sentinel, not a raw FK error.
	variant, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "kiosk-outdoor", Label: "Outdoor Kiosk", ParentStandardID: strptr("kiosk")})
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	// The arc stores the parent's uuid; ParentStandardName carries its handle.
	if variant.ParentStandardName == nil || *variant.ParentStandardName != "kiosk" {
		t.Fatalf("variant parent = %v, want kiosk", variant.ParentStandardName)
	}
	if _, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "orphan", Label: "Orphan", ParentStandardID: strptr("nope")}); !errors.Is(err, storage.ErrParentStandardNotFound) {
		t.Fatalf("unknown parent err = %v, want ErrParentStandardNotFound", err)
	}
	if err := gw.DeleteStandard(ctx, "", "kiosk-outdoor"); err != nil {
		t.Fatalf("delete variant: %v", err)
	}

	name := "Info Kiosk"
	if _, err := gw.UpdateStandard(ctx, "", "kiosk", storage.StandardPatch{Label: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := gw.GetStandard(ctx, "kiosk")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Label != name {
		t.Fatalf("label = %q, want %q", got.Label, name)
	}

	// A system conforming to kiosk holds the delete off (in use).
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "k1", StandardID: strptr("kiosk")}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if err := gw.DeleteStandard(ctx, "", "kiosk"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete in-use err = %v, want ErrTypeInUse", err)
	}

	// A shipped standard is operator-owned example content, not authoritative, so
	// it is freely editable: that is what makes forking a template into your own
	// standard useful.
	if _, err := gw.UpdateStandard(ctx, "", "meeting-room", storage.StandardPatch{Label: &name}); err != nil {
		t.Fatalf("update a shipped standard: %v, want it editable", err)
	}

	// The official read-only guard still stands for a row that IS official (the
	// canonical catalogs rely on it), so prove the mechanism on one.
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "canon", Official: true, Label: "Canonical"}); err != nil {
		t.Fatalf("seed an official standard: %v", err)
	}
	if _, err := gw.UpdateStandard(ctx, "", "canon", storage.StandardPatch{Label: &name}); !errors.Is(err, storage.ErrTypeOfficial) {
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

	oneOff, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "one-off"}, all, all)
	if err != nil {
		t.Fatalf("create one-off: %v", err)
	}
	if oneOff.StandardID != nil {
		t.Fatalf("one-off standard = %q, want nil", *oneOff.StandardID)
	}
	conforming, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "boardroom", StandardID: strptr("meeting-room")}, all, all)
	if err != nil {
		t.Fatalf("create conforming: %v", err)
	}
	if conforming.StandardName == nil || *conforming.StandardName != "meeting-room" {
		t.Fatalf("conforming standard = %v, want meeting-room", conforming.StandardName)
	}

	// The patch retargets the standard; the label it does not carry is
	// left alone (the coalesce), which is also the placeholder-order check on the
	// UPDATE.
	display := "Boardroom"
	if _, err := gw.UpdateSystem(ctx, "", "boardroom", storage.SystemPatch{Label: &display}, all, all); err != nil {
		t.Fatalf("update label: %v", err)
	}
	after, err := gw.UpdateSystem(ctx, "", "boardroom", storage.SystemPatch{StandardID: strptr("classroom")}, all, all)
	if err != nil {
		t.Fatalf("update standard: %v", err)
	}
	if after.StandardName == nil || *after.StandardName != "classroom" {
		t.Fatalf("patched standard = %v, want classroom", after.StandardName)
	}
	if after.Label != display || after.Name != "boardroom" {
		t.Fatalf("patched row = %+v, want label %q and name boardroom kept", after, display)
	}

	// A classified system converts BACK to a one-off. An omitted standard leaves it
	// alone; an explicit empty string clears it (the house patch convention). Without
	// the distinction, standard_id is a one-way door and "one-off" is only reachable
	// at create time, which would gut the point of making it optional.
	kept, err := gw.UpdateSystem(ctx, "", "boardroom", storage.SystemPatch{Label: &display}, all, all)
	if err != nil {
		t.Fatalf("patch without standard: %v", err)
	}
	if kept.StandardName == nil || *kept.StandardName != "classroom" {
		t.Fatalf("omitted standard = %v, want classroom kept", kept.StandardName)
	}
	cleared, err := gw.UpdateSystem(ctx, "", "boardroom", storage.SystemPatch{StandardID: strptr("")}, all, all)
	if err != nil {
		t.Fatalf("clear standard: %v", err)
	}
	if cleared.StandardID != nil {
		t.Fatalf("cleared standard = %q, want nil (a one-off system)", *cleared.StandardID)
	}
	if cleared.Label != display {
		t.Fatalf("clearing the standard also changed label to %q", cleared.Label)
	}
}

// The standard's map declaration (#791, ADR-0128): normalized 2D positions
// per role position against a declared aspect, data on the standard so every
// conforming system renders the same room for free. The map is a display
// declaration, validated in Go (bounds, duplicate positions), stored as one
// jsonb value, nil when the standard declares none.
func TestStandardMapDeclaration(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	st, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "mapped-room", Label: "Mapped Room"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if st.Map != nil {
		t.Fatalf("a new standard declares no map, got %s", st.Map)
	}

	mapJSON := json.RawMessage(`{"aspect":1.6,"positions":[{"role":"room-mic","position":1,"x":0.3,"y":0.5},{"role":"room-mic","position":2,"x":0.7,"y":0.5},{"role":"main-display","position":1,"x":0.5,"y":0.05}]}`)
	patched, err := gw.UpdateStandard(ctx, "", st.ID, storage.StandardPatch{Map: &mapJSON})
	if err != nil {
		t.Fatalf("declare map: %v", err)
	}
	var m storage.StandardMap
	if err := json.Unmarshal(patched.Map, &m); err != nil {
		t.Fatalf("read back map: %v", err)
	}
	if m.Aspect != 1.6 || len(m.Positions) != 3 || m.Positions[1].X != 0.7 {
		t.Fatalf("map round-trip = %+v", m)
	}

	// Out-of-bounds coordinates and duplicate (role, position) pairs are
	// refused as validation, not stored garbage.
	for _, bad := range []string{
		`{"aspect":1.6,"positions":[{"role":"r","position":1,"x":1.2,"y":0.5}]}`,
		`{"aspect":0,"positions":[{"role":"r","position":1,"x":0.5,"y":0.5}]}`,
		`{"aspect":1,"positions":[{"role":"r","position":1,"x":0.1,"y":0.1},{"role":"r","position":1,"x":0.2,"y":0.2}]}`,
	} {
		b := json.RawMessage(bad)
		if _, err := gw.UpdateStandard(ctx, "", st.ID, storage.StandardPatch{Map: &b}); !errors.Is(err, storage.ErrStandardMapInvalid) {
			t.Fatalf("bad map %s err = %v, want ErrStandardMapInvalid", bad, err)
		}
	}

	// Explicit null clears the declaration; a patch that says nothing leaves it.
	null := json.RawMessage(`null`)
	cleared, err := gw.UpdateStandard(ctx, "", st.ID, storage.StandardPatch{Map: &null})
	if err != nil {
		t.Fatalf("clear map: %v", err)
	}
	if cleared.Map != nil {
		t.Fatalf("cleared map still present: %s", cleared.Map)
	}
}
