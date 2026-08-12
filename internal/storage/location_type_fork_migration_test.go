package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationTypeForkAdoptionBackfill is the part of #703 an empty database
// cannot prove. Estates carry operator edits ON the shipped rows themselves,
// because until this slice a shipped location type was operator-owned; flip
// those rows to official and re-seed authoritatively without moving the edits
// first and every one of them is silently reverted.
//
// So it stands the database one migration back (the location_type table exists,
// the fork adoption has not run), writes the rows the way an UPGRADED estate
// holds them, and migrates forward over them, running the real migration file
// rather than a copy of its SQL. Then it boots the seed, which is where the
// two outcomes actually diverge: the operator's edit has to survive the
// authoritative write, and the withdrawn shipped value has to be gone.
//
// The four rows are the four cases the migration has to tell apart, and the
// discriminator is the audit trail rather than a comparison against what this
// release ships. A row holding a value a PREVIOUS release shipped and this one
// withdrew is indistinguishable from an edit by inspection, which is the whole
// difficulty; the audit knows because every operator write records itself.
func TestLocationTypeForkAdoptionBackfill(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260812093000"); err != nil {
		t.Fatalf("roll back below the fork adoption: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()

	// The shape an upgraded estate holds: shipped rows seeded operator-owned
	// (official=false, which is what insert-if-absent wrote), one of them
	// carrying a rule a release shipped and ADR-0103 withdrew.
	rows := []struct {
		name, displayName, icon string
		parents                 string
		rule                    any
	}{
		{name: "campus", displayName: "Campus", icon: "landmark", parents: "{root}"},
		{name: "building", displayName: "Building", icon: "building", parents: "{root,campus}"},
		{name: "floor", displayName: "Floor", icon: "layers", parents: "{building,campus}", rule: `{"stem":"","bare_first":false}`},
		{name: "room", displayName: "Room", icon: "door-open", parents: "{floor,building,campus}"},
	}
	for _, r := range rows {
		mustExec(t, conn, `insert into location_type (name, official, display_name, icon, allowed_parent_types, name_rule)
		                   values ($1, false, $2, $3, $4, $5)`, r.name, r.displayName, r.icon, r.parents, r.rule)
	}
	// An operator's own type, which nothing here may touch: no shadow, no
	// official flag, and the seed does not know its name.
	mustExec(t, conn, `insert into location_type (name, official, display_name, icon, allowed_parent_types)
	                   values ('wing', false, 'West Wing', 'layers', '{root}')`)

	id := func(name string) string {
		var v string
		if err := conn.QueryRow(ctx, `select id from location_type where name = $1`, name).Scan(&v); err != nil {
			t.Fatalf("id of %q: %v", name, err)
		}
		return v
	}
	// The operator edited `room` (its label and its placement set) and created
	// `wing`, each recorded the way the gateway records it: in the audit trail,
	// keyed on the row's own uuid.
	mustExec(t, conn, `update location_type set display_name = 'Space', allowed_parent_types = '{floor}' where name = 'room'`)
	mustExec(t, conn, `insert into audit_log (verb, resource, resource_id) values ('update', 'location_type', $1)`, id("room"))
	mustExec(t, conn, `insert into audit_log (verb, resource, resource_id) values ('create', 'location_type', $1)`, id("wing"))
	mustExec(t, conn, `insert into audit_log (verb, resource, resource_id) values ('update', 'location_type', $1)`, id("wing"))
	// A read of an unrelated resource is not an edit of this one.
	mustExec(t, conn, `insert into audit_log (verb, resource, resource_id) values ('update', 'location', $1)`, id("floor"))

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over the pre-fork rows: %v", err)
	}

	shadowed := func(name string) bool {
		var n int
		if err := conn.QueryRow(ctx,
			`select count(*) from registry_shadow s join location_type lt on lt.id = s.row_id
			  where s.registry = 'location_type' and lt.name = $1`, name).Scan(&n); err != nil {
			t.Fatalf("count shadows of %q: %v", name, err)
		}
		return n > 0
	}
	official := func(name string) bool {
		var v bool
		if err := conn.QueryRow(ctx, `select official from location_type where name = $1`, name).Scan(&v); err != nil {
			t.Fatalf("official of %q: %v", name, err)
		}
		return v
	}
	for _, want := range []struct {
		name             string
		official, shadow bool
		why              string
	}{
		{"campus", true, false, "shipped and untouched: the platform takes the row and nothing is preserved"},
		{"building", true, false, "same"},
		{"floor", true, false, "it holds a WITHDRAWN shipped value, not an edit, so preserving it would defeat the withdrawal"},
		{"room", true, true, "the operator edited it, and their edit moves into a shadow before the seed rewrites the row"},
		{"wing", false, false, "an operator's own type: the seed never names it, and a shadow would mask its next write"},
	} {
		if got := official(want.name); got != want.official {
			t.Errorf("%s official = %v after the migration, want %v (%s)", want.name, got, want.official, want.why)
		}
		if got := shadowed(want.name); got != want.shadow {
			t.Errorf("%s shadowed = %v after the migration, want %v (%s)", want.name, got, want.shadow, want.why)
		}
	}

	// The edit is intact in the shadow, whole-row rather than per-column.
	var image map[string]any
	if err := conn.QueryRow(ctx,
		`select s.image from registry_shadow s join location_type lt on lt.id = s.row_id
		  where s.registry = 'location_type' and lt.name = 'room'`).Scan(&image); err != nil {
		t.Fatalf("read the shadow image: %v", err)
	}
	for _, want := range []struct {
		key string
		val any
	}{
		{"display_name", "Space"},
		{"icon", "door-open"},
		{"label_rule", nil},
		{"name_rule", nil},
	} {
		if got := image[want.key]; got != want.val {
			t.Errorf("the captured image's %s = %v, want %v: a fork captures the WHOLE mutable row", want.key, got, want.val)
		}
	}
	if _, ok := image["allowed_parent_types"]; !ok {
		t.Error("the captured image drops allowed_parent_types")
	}

	// Then the boot the estate actually takes, which is where the two outcomes
	// diverge and where the old shape reverted nothing and withdrew nothing.
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	room, err := gw.GetLocationType(ctx, "room")
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if room.DisplayName != "Space" || len(room.AllowedParentTypes) != 1 || room.AllowedParentTypes[0] != "floor" {
		t.Errorf("the operator's edit reads back as %+v after the migration and a boot, want display_name Space under {floor}", room)
	}
	if !room.Forked {
		t.Error("the preserved edit does not report forked=true, so the console cannot offer to restore it")
	}
	floor, err := gw.GetLocationType(ctx, "floor")
	if err != nil {
		t.Fatalf("get floor: %v", err)
	}
	if floor.NameRule != nil {
		t.Errorf("floor still carries %+v after the migration and a boot, want nil: a withdrawn shipped value must leave the estate", floor.NameRule)
	}
	wing, err := gw.GetLocationType(ctx, "wing")
	if err != nil {
		t.Fatalf("get wing: %v", err)
	}
	if wing.DisplayName != "West Wing" || wing.Official || wing.Forked {
		t.Errorf("the operator's own type = %+v, want West Wing, not official, not forked", wing)
	}

	// Idempotent: a second forward run is a no-op, and in particular does not
	// re-capture a shadow over a row the operator has since restored.
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward a second time: %v", err)
	}
	if got := shadowed("floor"); got {
		t.Error("a second run shadowed floor")
	}
}
