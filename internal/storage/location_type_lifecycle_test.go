package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/updatemask"
)

func locationTypeNamed(t *testing.T, gw storage.Gateway, name string) *storage.LocationType {
	t.Helper()
	lt, err := gw.GetLocationType(context.Background(), name)
	if err != nil {
		t.Fatalf("get location_type %q: %v", name, err)
	}
	return lt
}

// TestShippedLocationTypeForksAndRestores is the acceptance behavior of #703 on
// the operator's side: a shipped location type is still editable, an edit
// survives a re-seed, and :restore hands the row back to the platform.
//
// The mechanism changed, not the capability. Before, the row itself was
// operator-owned (official=false), which is precisely why the seed could not
// rewrite it and why a withdrawn value could never leave an estate.
func TestShippedLocationTypeForksAndRestores(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	campus := locationTypeNamed(t, gw, "campus")
	if !campus.Official {
		t.Fatalf("a shipped location type is official=%v, want true: the platform owns it now", campus.Official)
	}
	if campus.Forked {
		t.Fatalf("a freshly seeded row reports forked=true, want false")
	}

	label := "Site"
	forked, err := gw.UpdateLocationType(ctx, "", "campus", storage.LocationTypePatch{DisplayName: &label})
	if err != nil {
		t.Fatalf("edit a shipped location type: %v (it must fork, not refuse)", err)
	}
	if forked.DisplayName != "Site" || !forked.Forked || !forked.Official {
		t.Fatalf("the forked row = %+v, want display_name Site with official and forked both true", forked)
	}
	if forked.ID != campus.ID {
		t.Fatalf("the fork answered with id %q, the shipped row is %q: one logical row has one address", forked.ID, campus.ID)
	}

	// The official row is byte-identical: the fork is an overlay, and this is
	// what lets the next release improve or withdraw what it ships.
	conn := connectDSN(t, dsn)
	var stored string
	if err := conn.QueryRow(ctx, `select display_name from location_type where name = 'campus'`).Scan(&stored); err != nil {
		t.Fatalf("read the official row: %v", err)
	}
	if stored != "Campus" {
		t.Fatalf("the official row now reads %q, want the shipped Campus: a fork never writes the row", stored)
	}

	// A re-seed (every boot) writes the official row authoritatively and leaves
	// the operator's version resolving on top of it.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if got := locationTypeNamed(t, gw, "campus"); got.DisplayName != "Site" {
		t.Fatalf("after a re-seed the type reads %q, want the operator's Site", got.DisplayName)
	}

	// The list resolves too, and reports one row rather than two.
	types, err := gw.ListLocationTypes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var seen int
	for _, lt := range types {
		if lt.Name == "campus" {
			seen++
			if lt.DisplayName != "Site" || !lt.Forked {
				t.Errorf("the listed row = %+v, want the resolved Site marked forked", lt)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("the registry lists campus %d times, want 1: a shadow is an overlay, not a second row", seen)
	}

	// A shipped row stays undeletable, forked or not: a fork is an overlay, not
	// ownership.
	if err := gw.DeleteLocationType(ctx, "", "campus"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete a forked shipped type = %v, want ErrTypeOfficial", err)
	}

	restored, err := gw.RestoreLocationType(ctx, "", "campus")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.DisplayName != "Campus" || restored.Forked {
		t.Fatalf("the restored row = %+v, want the shipped Campus with forked false", restored)
	}
	if _, err := gw.RestoreLocationType(ctx, "", "campus"); !errors.Is(err, storage.ErrTypeNotForked) {
		t.Fatalf("a second restore = %v, want ErrTypeNotForked: nothing to undo beats a silent success", err)
	}
	if _, err := gw.RestoreLocationType(ctx, "", "no-such-type"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("restore an unknown type = %v, want ErrTypeNotFound", err)
	}
}

// TestOperatorLocationTypeIsUntouchedByTheFork is the other half: a type an
// operator created is theirs outright, written in place with no shadow, and
// restore has nothing to give it back.
func TestOperatorLocationTypeIsUntouchedByTheFork(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: "wing", DisplayName: "Wing", Icon: "layers"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	label := "West Wing"
	updated, err := gw.UpdateLocationType(ctx, "", "wing", storage.LocationTypePatch{DisplayName: &label})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Official || updated.Forked {
		t.Fatalf("an operator's own row = %+v, want neither official nor forked", updated)
	}

	conn := connectDSN(t, dsn)
	var shadows int
	if err := conn.QueryRow(ctx, `select count(*) from registry_shadow where registry = 'location_type'`).Scan(&shadows); err != nil {
		t.Fatalf("count shadows: %v", err)
	}
	if shadows != 0 {
		t.Fatalf("editing an operator's own type wrote %d shadow rows, want 0: it is written in place", shadows)
	}
	if _, err := gw.RestoreLocationType(ctx, "", "wing"); !errors.Is(err, storage.ErrTypeNotForked) {
		t.Fatalf("restore an operator's own type = %v, want ErrTypeNotForked", err)
	}
	// A re-seed does not reach it either: the seed writes the four names it
	// ships and nothing else.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if got := locationTypeNamed(t, gw, "wing"); got.DisplayName != "West Wing" {
		t.Fatalf("after a re-seed the operator's type reads %q, want West Wing", got.DisplayName)
	}
}

// TestWithdrawnShippedValueLeavesTheEstate is the acceptance behavior #703 was
// filed for, and the only place the withdrawal is observable: a rule a release
// SHIPPED and a later release removed is gone after one boot.
//
// The estate it describes is staged with a RAW write, deliberately not through
// the gateway's own upsert: the seed write is the thing under test, and a setup
// that goes through it would report a broken seed as a broken setup instead of
// as the missed withdrawal. What the raw statement leaves behind is exactly what
// an estate that booted the release carrying floor's rule holds today.
func TestWithdrawnShippedValueLeavesTheEstate(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	conn := connectDSN(t, dsn)
	mustExec(t, conn, `update location_type set name_rule = '{"stem":"","bare_first":false}' where name = 'floor'`)
	if got := locationTypeNamed(t, gw, "floor"); got.NameRule == nil {
		t.Fatalf("the staged estate carries no rule; the withdrawal has nothing to withdraw")
	}

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("boot the release that withdrew it: %v", err)
	}
	if got := locationTypeNamed(t, gw, "floor"); got.NameRule != nil {
		t.Fatalf("floor still carries %+v after a boot of the release that removed it: a shipped value must be withdrawable", got.NameRule)
	}
}

// TestClearNameRuleOnBothTypeKinds is #692's acceptance behavior, and the
// composition with the fork: one operator-visible gesture, two write paths
// underneath. Clearing on a SHIPPED type is a fork whose image carries no rule;
// clearing on an operator's own type is a plain write of null. The proof that
// both landed is the generator, which is what a rule is FOR: a nameless create
// mints a name while the rule stands and refuses once it is cleared.
func TestClearNameRuleOnBothTypeKinds(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	clear := storage.LocationTypePatch{Write: updatemask.Of(storage.LocationTypeFieldNameRule)}

	for _, tc := range []struct {
		kind, typeName string
		shipped        bool
	}{
		{kind: "an operator's own type", typeName: "deck"},
		// campus rather than floor only because a campus may sit at root, so
		// the nameless create below needs no parent to hang on: the leg under
		// test is fork-versus-direct-write, not placement.
		{kind: "a shipped type", typeName: "campus", shipped: true},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			if !tc.shipped {
				if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{
					Name: tc.typeName, DisplayName: "Deck", NameRule: &storage.NameRule{},
				}); err != nil {
					t.Fatalf("create: %v", err)
				}
			} else {
				if _, err := gw.UpdateLocationType(ctx, "", tc.typeName, storage.LocationTypePatch{
					NameRule: &storage.NameRule{},
				}); err != nil {
					t.Fatalf("set a rule on the shipped type: %v", err)
				}
			}
			// While the rule stands, the platform names a nameless create.
			l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: tc.typeName}, all)
			if err != nil {
				t.Fatalf("nameless create under a rule: %v", err)
			}
			if !l.NameGenerated {
				t.Fatalf("the created location reports NameGenerated=false, want true")
			}

			// An OMITTED rule is still unchanged: only the mask clears.
			label := "Renamed"
			if _, err := gw.UpdateLocationType(ctx, "", tc.typeName, storage.LocationTypePatch{DisplayName: &label}); err != nil {
				t.Fatalf("unrelated patch: %v", err)
			}
			if got := locationTypeNamed(t, gw, tc.typeName); got.NameRule == nil {
				t.Fatalf("an unrelated patch cleared the rule; an omitted key means unchanged")
			}

			cleared, err := gw.UpdateLocationType(ctx, "", tc.typeName, clear)
			if err != nil {
				t.Fatalf("clear the rule: %v", err)
			}
			if cleared.NameRule != nil {
				t.Fatalf("the rule after a masked clear = %+v, want nil (operator-named)", cleared.NameRule)
			}
			if got := locationTypeNamed(t, gw, tc.typeName); got.NameRule != nil {
				t.Fatalf("the stored rule after a masked clear = %+v, want nil", got.NameRule)
			}
			if got := locationTypeNamed(t, gw, tc.typeName); got.Forked != tc.shipped {
				t.Fatalf("forked = %v on %s, want %v: a shipped type forks, an operator's own is written in place", got.Forked, tc.kind, tc.shipped)
			}
			// And the generator sees it: this is the behavior an operator was
			// stuck with, auto-naming they could not turn off.
			if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: tc.typeName}, all); !errors.Is(err, storage.ErrLocationTypeNoNameRule) {
				t.Fatalf("a nameless create after the clear = %v, want ErrLocationTypeNoNameRule", err)
			}
		})
	}
}

// TestShippedLocationTypeContractStaysWritable guards a capability the
// ownership flip would otherwise have withdrawn silently. A location type's
// property and metric contracts are rows in their own tables, not columns of
// the registry row the fork covers, and nothing seeds one: every line is an
// operator's. The official read-only guard was dormant on this registry (no row
// was official), and activating it would have taken the contract editor away
// for the four types an estate actually uses.
func TestShippedLocationTypeContractStaysWritable(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := gw.SetLocationTypeProperty(ctx, "", "room", storage.LocationTypePropertySpec{
		PropertyTypeName: "serial-number",
	}); err != nil {
		t.Fatalf("declare a property on a shipped location type: %v", err)
	}
	if err := gw.DeleteLocationTypeProperty(ctx, "", "room", "serial-number"); err != nil {
		t.Fatalf("withdraw it again: %v", err)
	}
	if _, err := gw.SetLocationTypeProperty(ctx, "", "no-such-type", storage.LocationTypePropertySpec{
		PropertyTypeName: "serial-number",
	}); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("declare on an unknown type = %v, want ErrTypeNotFound", err)
	}
}
