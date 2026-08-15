package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestShippedLocationTypesFlipToPlatformOwned is the part of #703 an empty
// database cannot prove: the ownership flip has to land on rows that already
// exist, seeded operator-owned by the shape this slice replaces, and it has to
// land on those four and nothing else.
//
// So it stands the database one migration back (the location_type table exists,
// the fork adoption has not run), writes the rows the way an installed estate
// holds them, and migrates forward over them, running the real migration file
// rather than a copy of its SQL. Then it boots the seed, which is what the flip
// buys: a shipped value the release withdrew can finally leave a row that
// already carried it.
//
// The flip is the whole migration, and the shadow count below is what says so.
// An earlier draft captured operator edits into registry_shadow ahead of the
// flip, discriminating by the audit trail; that half was removed because there
// are no releases and no operator data for it to protect (ADR-0106, as amended).
// The first real release changes that calculus and owes its estates a migration
// of its own; this one may not quietly grow the machinery back.
func TestShippedLocationTypesFlipToPlatformOwned(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260812093000"); err != nil {
		t.Fatalf("roll back below the fork adoption: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()

	// The shape an installed estate holds: shipped rows seeded operator-owned
	// (official=false, which is what insert-if-absent wrote), one of them
	// carrying a rule a release shipped and ADR-0103 withdrew.
	rows := []struct {
		name, label, icon string
		parents           string
		rule              any
	}{
		{name: "campus", label: "Campus", icon: "landmark", parents: "{root}"},
		{name: "building", label: "Building", icon: "building", parents: "{root,campus}"},
		{name: "floor", label: "Floor", icon: "layers", parents: "{building,campus}", rule: `{"stem":"","bare_first":false}`},
		{name: "room", label: "Room", icon: "door-open", parents: "{floor,building,campus}"},
	}
	for _, r := range rows {
		// The column is `display_name` at this point in the chain: #613's rename
		// (20260814100000_label_is_the_column.sql) is above the version this test
		// stands the database at.
		mustExec(t, conn, `insert into location_type (name, official, display_name, icon, allowed_parent_types, name_rule)
		                   values ($1, false, $2, $3, $4, $5)`, r.name, r.label, r.icon, r.parents, r.rule)
	}
	// An operator's own type, which the flip may not touch: nothing seeds it, so
	// marking it official would take it away from the operator who made it.
	mustExec(t, conn, `insert into location_type (name, official, display_name, icon, allowed_parent_types)
	                   values ('wing', false, 'West Wing', 'layers', '{root}')`)

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over the pre-fork rows: %v", err)
	}

	official := func(name string) bool {
		var v bool
		if err := conn.QueryRow(ctx, `select official from location_type where name = $1`, name).Scan(&v); err != nil {
			t.Fatalf("official of %q: %v", name, err)
		}
		return v
	}
	for _, want := range []struct {
		name     string
		official bool
		why      string
	}{
		{"campus", true, "a shipped name: the platform takes the row so the seed can write it"},
		{"building", true, "same"},
		{"floor", true, "same, and this is the row the withdrawal has to reach"},
		{"room", true, "same"},
		{"wing", false, "an operator's own type: nothing seeds it, so nothing may claim it"},
	} {
		if got := official(want.name); got != want.official {
			t.Errorf("%s official = %v after the migration, want %v (%s)", want.name, got, want.official, want.why)
		}
	}

	// Nothing else moved. The flip is a single update, and a migration that
	// wrote a shadow would be preserving data that does not exist yet while
	// masking the row's next write behind an overlay nobody asked for.
	var shadows int
	if err := conn.QueryRow(ctx, `select count(*) from registry_shadow`).Scan(&shadows); err != nil {
		t.Fatalf("count shadows: %v", err)
	}
	if shadows != 0 {
		t.Errorf("the migration wrote %d registry_shadow rows, want 0: the ownership flip is the whole migration", shadows)
	}

	// The down leg mirrors the up: rolling back hands the rows to the operator
	// again, and rolling forward reclaims them. Driven as a round trip rather
	// than asserted by reading the file, because a `migrate:down` nobody
	// executes is the half that rots.
	if err := migrate.RollbackBelow(dsn, "20260812093000"); err != nil {
		t.Fatalf("roll back the flip: %v", err)
	}
	for _, name := range []string{"campus", "building", "floor", "room"} {
		if official(name) {
			t.Errorf("%s is still official after the rollback, want the operator's row back", name)
		}
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward a second time: %v", err)
	}
	for _, name := range []string{"campus", "building", "floor", "room"} {
		if !official(name) {
			t.Errorf("%s is not official after the second forward run", name)
		}
	}
	if official("wing") {
		t.Error("the round trip claimed the operator's own type")
	}

	// Then the boot the estate actually takes, which is the point of the flip:
	// under the old ownership the seed could not write these rows at all, so a
	// withdrawn value stayed forever.
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
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
	if wing.Label != "West Wing" || wing.Official || wing.Forked {
		t.Errorf("the operator's own type = %+v, want West Wing, not official, not forked", wing)
	}
}
