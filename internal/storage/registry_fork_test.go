package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// officialRow is the raw component_type row as the table holds it, read
// straight from Postgres so a test can assert the platform's copy is untouched
// without going through the resolve that would hide a fork.
type officialRow struct {
	label              string
	stem, icon, abbrev *string
	tags               []string
}

func readOfficialRow(t *testing.T, ctx context.Context, dsn, name string) officialRow {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var row officialRow
	if err := conn.QueryRow(ctx,
		`select label, stem, icon, abbrev, default_tags from component_type where name = $1`, name).
		Scan(&row.label, &row.stem, &row.icon, &row.abbrev, &row.tags); err != nil {
		t.Fatalf("read official %s: %v", name, err)
	}
	return row
}

func setOfficialLabel(t *testing.T, ctx context.Context, dsn, name, label string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	tag, err := conn.Exec(ctx, `update component_type set label = $2 where name = $1`, name, label)
	if err != nil {
		t.Fatalf("scuff official %s: %v", name, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("scuff official %s affected %d rows, want 1", name, tag.RowsAffected())
	}
}

func countShadows(t *testing.T, ctx context.Context, dsn string) int {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from registry_shadow`).Scan(&n); err != nil {
		t.Fatalf("count shadows: %v", err)
	}
	return n
}

func str(s string) *string { return &s }

// TestForkLeavesTheShippedRowPristine is the headline behavior: an operator
// patches a shipped component_type, the patch takes effect on every read, and
// the platform's own row is unchanged underneath it.
func TestForkLeavesTheShippedRowPristine(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	before := readOfficialRow(t, ctx, dsn, "mic")
	shipped, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get shipped mic: %v", err)
	}
	if !shipped.Official || shipped.Forked {
		t.Fatalf("shipped mic = official:%v forked:%v, want official and unforked", shipped.Official, shipped.Forked)
	}

	forked, err := gw.UpdateComponentType(ctx, "", "mic", storage.ComponentTypePatch{
		Label: str("House Microphone"), Stem: str("housemic"),
	})
	if err != nil {
		t.Fatalf("patch shipped mic: %v", err)
	}
	if forked.Label != "House Microphone" || forked.Stem == nil || *forked.Stem != "housemic" {
		t.Fatalf("forked mic = %+v, want the operator's label and stem", forked)
	}
	if !forked.Forked {
		t.Error("forked mic Forked = false, want true (the console shows yours-overriding-shipped)")
	}
	if !forked.Official {
		t.Error("forked mic Official = false; a fork does not change where the row came from")
	}
	if forked.ID != shipped.ID {
		t.Errorf("forked mic id = %v, want the shipped id %v (a fork never re-addresses the row)", forked.ID, shipped.ID)
	}

	// The official row underneath is byte-identical.
	after := readOfficialRow(t, ctx, dsn, "mic")
	if after.label != before.label {
		t.Errorf("official label = %q, want %q untouched", after.label, before.label)
	}
	if after.stem == nil || before.stem == nil || *after.stem != *before.stem {
		t.Errorf("official stem = %v, want %v untouched", after.stem, before.stem)
	}

	// Every read resolves the operator's version, by name and by uuid alike:
	// the namespace never reaches the address.
	for _, ref := range []string{"mic", shipped.ID.String()} {
		got, err := gw.GetComponentType(ctx, ref)
		if err != nil {
			t.Fatalf("get %q: %v", ref, err)
		}
		if got.Label != "House Microphone" || got.ID != shipped.ID || !got.Forked {
			t.Errorf("get %q = %+v, want the forked row under the shipped id", ref, got)
		}
	}
	listed, err := gw.ListComponentTypes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var seen int
	for _, ct := range listed {
		if ct.Name == "mic" {
			seen++
			if ct.Label != "House Microphone" || !ct.Forked {
				t.Errorf("listed mic = %+v, want the forked row", ct)
			}
		}
	}
	if seen != 1 {
		t.Errorf("mic appears %d times in the list, want exactly 1 (a shadow is not a second row)", seen)
	}
}

// TestRestoreDiscardsTheShadow proves restore-to-defaults: the shadow row goes
// away and reads return the shipped values again.
func TestRestoreDiscardsTheShadow(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	shipped, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get shipped mic: %v", err)
	}
	if _, err := gw.UpdateComponentType(ctx, "", "mic", storage.ComponentTypePatch{Label: str("House Microphone")}); err != nil {
		t.Fatalf("fork mic: %v", err)
	}
	if n := countShadows(t, ctx, dsn); n != 1 {
		t.Fatalf("shadow rows after fork = %d, want 1", n)
	}

	restored, err := gw.RestoreComponentType(ctx, "", "mic")
	if err != nil {
		t.Fatalf("restore mic: %v", err)
	}
	if restored.Label != shipped.Label || restored.Forked {
		t.Fatalf("restored mic = %+v, want the shipped label %q and forked=false", restored, shipped.Label)
	}
	if n := countShadows(t, ctx, dsn); n != 0 {
		t.Fatalf("shadow rows after restore = %d, want 0 (restore IS the shadow delete)", n)
	}
	again, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get mic after restore: %v", err)
	}
	if again.Label != shipped.Label {
		t.Errorf("mic label after restore = %q, want the shipped %q", again.Label, shipped.Label)
	}

	// Restoring a row that was never forked is refused rather than silently
	// succeeding, so the console can tell an operator there is nothing to undo.
	if _, err := gw.RestoreComponentType(ctx, "", "camera"); !errors.Is(err, storage.ErrTypeNotForked) {
		t.Errorf("restore an unforked row err = %v, want ErrTypeNotForked", err)
	}
	if _, err := gw.RestoreComponentType(ctx, "", "no-such-type"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Errorf("restore an unknown row err = %v, want ErrTypeNotFound", err)
	}
}

// TestReseedNeitherClobbersNorResurrectsAShadow runs the seed on both sides of
// a fork and of a restore.
//
// The fixture makes the seed's ON CONFLICT branch actually fire on the row
// under test: before each re-seed it scuffs the official row's label
// directly in SQL, and asserts afterwards that the seed put the shipped value
// back. Without that, "the fork survived the re-seed" would pass even if the
// seed had skipped the row entirely, which is the vacuous version of this test.
func TestReseedNeitherClobbersNorResurrectsAShadow(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	shipped, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get shipped mic: %v", err)
	}

	if _, err := gw.UpdateComponentType(ctx, "", "mic", storage.ComponentTypePatch{
		Label: str("House Microphone"), Abbrev: str("hm"),
	}); err != nil {
		t.Fatalf("fork mic: %v", err)
	}

	setOfficialLabel(t, ctx, dsn, "mic", "scuffed-before-reseed")
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	// The conflict fired on this row: the seed wrote the shipped value back
	// over the scuff.
	if raw := readOfficialRow(t, ctx, dsn, "mic"); raw.label != shipped.Label {
		t.Fatalf("official label after re-seed = %q, want the seeded %q; the seed did not touch this row, so the rest of this test proves nothing",
			raw.label, shipped.Label)
	}
	// And the fork rode over it untouched.
	forked, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get mic after re-seed: %v", err)
	}
	if forked.Label != "House Microphone" || forked.Abbrev == nil || *forked.Abbrev != "hm" || !forked.Forked {
		t.Fatalf("mic after re-seed = %+v, want the operator's fork intact", forked)
	}
	if n := countShadows(t, ctx, dsn); n != 1 {
		t.Fatalf("shadow rows after re-seed = %d, want 1", n)
	}

	// Restore, then seed again: the shadow does not come back from the dead.
	if _, err := gw.RestoreComponentType(ctx, "", "mic"); err != nil {
		t.Fatalf("restore mic: %v", err)
	}
	setOfficialLabel(t, ctx, dsn, "mic", "scuffed-before-third-seed")
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("third seed: %v", err)
	}
	if raw := readOfficialRow(t, ctx, dsn, "mic"); raw.label != shipped.Label {
		t.Fatalf("official label after third seed = %q, want the seeded %q", raw.label, shipped.Label)
	}
	if n := countShadows(t, ctx, dsn); n != 0 {
		t.Fatalf("shadow rows after the third seed = %d, want 0 (a restored fork stays restored)", n)
	}
	after, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get mic after third seed: %v", err)
	}
	if after.Label != shipped.Label || after.Forked {
		t.Fatalf("mic after the third seed = %+v, want the shipped row, unforked", after)
	}
}

// TestForkedAncestorReachesAnUnshadowedDescendant is decision 3, asserted
// directly: the inheritance walk resolves PER NODE, in official structure
// space. A shadow changes a node's facts, never the chain, so an unshadowed
// child inherits its shadowed parent's values, and a shadowed child still
// inherits everything it does not itself override.
func TestForkedAncestorReachesAnUnshadowedDescendant(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The seeded chain: mic (stem/icon/abbrev set) -> wireless-mic and
	// ceiling-mic (all three null, so all three inherit).
	mic, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get mic: %v", err)
	}
	wireless, err := gw.GetComponentType(ctx, "wireless-mic")
	if err != nil {
		t.Fatalf("get wireless-mic: %v", err)
	}
	ceiling, err := gw.GetComponentType(ctx, "ceiling-mic")
	if err != nil {
		t.Fatalf("get ceiling-mic: %v", err)
	}
	if wireless.Stem != nil || ceiling.Stem != nil {
		t.Fatalf("fixture assumption broken: wireless-mic/ceiling-mic must both inherit stem, got %v/%v", wireless.Stem, ceiling.Stem)
	}

	// Fork the ANCESTOR only.
	if _, err := gw.UpdateComponentType(ctx, "", "mic", storage.ComponentTypePatch{
		Stem: str("housemic"), Icon: str("mic-house"),
	}); err != nil {
		t.Fatalf("fork mic: %v", err)
	}

	// An unshadowed descendant inherits the shadowed ancestor's facts.
	stem, icon, _, _, err := gw.ResolveTypeFacts(ctx, ceiling.ID)
	if err != nil {
		t.Fatalf("resolve ceiling-mic: %v", err)
	}
	if stem != "housemic" || icon != "mic-house" {
		t.Errorf("ceiling-mic resolved (stem,icon) = (%q,%q), want the forked ancestor's (housemic,mic-house)", stem, icon)
	}

	// Now shadow the DESCENDANT too, overriding only its abbrev. The mixed
	// chain (shadowed leaf over shadowed root) still resolves per node: the
	// leaf's own override wins for abbrev, and stem still walks up into the
	// forked ancestor rather than falling back to the shipped one.
	if _, err := gw.UpdateComponentType(ctx, "", "wireless-mic", storage.ComponentTypePatch{
		Abbrev: str("wm"),
	}); err != nil {
		t.Fatalf("fork wireless-mic: %v", err)
	}
	stem, icon, abbrev, _, err := gw.ResolveTypeFacts(ctx, wireless.ID)
	if err != nil {
		t.Fatalf("resolve wireless-mic: %v", err)
	}
	if abbrev != "wm" {
		t.Errorf("wireless-mic abbrev = %q, want its own fork's %q", abbrev, "wm")
	}
	if stem != "housemic" || icon != "mic-house" {
		t.Errorf("wireless-mic resolved (stem,icon) = (%q,%q), want the forked ancestor's (housemic,mic-house); a shadowed node must not stop inheriting", stem, icon)
	}

	// Structure is official space and a fork never moves a row: the child's
	// parent link and the subtree test are unchanged.
	stillWireless, err := gw.GetComponentType(ctx, "wireless-mic")
	if err != nil {
		t.Fatalf("re-get wireless-mic: %v", err)
	}
	if stillWireless.ParentID == nil || *stillWireless.ParentID != mic.ID {
		t.Errorf("wireless-mic parent = %v, want mic %v after both forks", stillWireless.ParentID, mic.ID)
	}
	within, err := gw.TypeIsWithin(ctx, wireless.ID, mic.ID)
	if err != nil {
		t.Fatalf("TypeIsWithin: %v", err)
	}
	if !within {
		t.Error("TypeIsWithin(wireless-mic, mic) = false after forking both, want true")
	}

	// Restoring the ancestor puts the shipped facts back under the still-forked
	// descendant, which keeps only its own override.
	if _, err := gw.RestoreComponentType(ctx, "", "mic"); err != nil {
		t.Fatalf("restore mic: %v", err)
	}
	stem, _, abbrev, _, err = gw.ResolveTypeFacts(ctx, wireless.ID)
	if err != nil {
		t.Fatalf("resolve wireless-mic after restore: %v", err)
	}
	if stem != "mic" {
		t.Errorf("wireless-mic stem after restoring mic = %q, want the shipped %q", stem, "mic")
	}
	if abbrev != "wm" {
		t.Errorf("wireless-mic abbrev after restoring mic = %q, want its own fork's %q still", abbrev, "wm")
	}
}

// TestForkDoesNotOpenTheOfficialRowToDeleteOrRename keeps the rest of the
// official lock intact: a shipped row still cannot be deleted, forked or not,
// and a custom row still patches in place instead of growing a shadow.
func TestForkDoesNotOpenTheOfficialRowToDelete(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := gw.UpdateComponentType(ctx, "", "camera", storage.ComponentTypePatch{Label: str("Cam")}); err != nil {
		t.Fatalf("fork camera: %v", err)
	}
	if err := gw.DeleteComponentType(ctx, "", "camera"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete a forked shipped row err = %v, want ErrTypeOfficial", err)
	}

	// A custom row is not a fork: it patches in place and grows no shadow.
	custom, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "fk-custom", Label: "Custom", Stem: str("custom"),
	})
	if err != nil {
		t.Fatalf("create custom: %v", err)
	}
	updated, err := gw.UpdateComponentType(ctx, "", "fk-custom", storage.ComponentTypePatch{Label: str("Custom Two")})
	if err != nil {
		t.Fatalf("patch custom: %v", err)
	}
	if updated.Forked || updated.Official {
		t.Errorf("patched custom row = official:%v forked:%v, want both false", updated.Official, updated.Forked)
	}
	if n := countShadows(t, ctx, dsn); n != 1 {
		t.Errorf("shadow rows = %d, want 1 (only the camera fork; a custom row writes itself)", n)
	}
	if raw := readOfficialRow(t, ctx, dsn, "fk-custom"); raw.label != "Custom Two" {
		t.Errorf("custom row label in the table = %q, want %q written in place", raw.label, "Custom Two")
	}
	if err := gw.DeleteComponentType(ctx, "", custom.Name); err != nil {
		t.Fatalf("delete custom: %v", err)
	}
}

// TestForkAuditsAgainstTheShippedRowsID keeps the audit trail keyed on the
// entity: a fork and its restore both record against the shipped row's uuid,
// which is the only id the operator ever sees.
func TestForkAuditsAgainstTheShippedRowsID(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mic, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get mic: %v", err)
	}
	if _, err := gw.UpdateComponentType(ctx, "", "mic", storage.ComponentTypePatch{Label: str("House Microphone")}); err != nil {
		t.Fatalf("fork mic: %v", err)
	}
	if _, err := gw.RestoreComponentType(ctx, "", "mic"); err != nil {
		t.Fatalf("restore mic: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	rows, err := conn.Query(ctx,
		`select verb, resource_id from audit_log where resource = 'component_type' order by ts, id`)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action, resID string
		if err := rows.Scan(&action, &resID); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		if resID != mic.ID.String() {
			t.Errorf("audit %q resource_id = %v, want the shipped row's id %v", action, resID, mic.ID)
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	if len(actions) != 2 || actions[0] != "update" || actions[1] != "restore" {
		t.Errorf("audit actions = %v, want [update restore]", actions)
	}
}
