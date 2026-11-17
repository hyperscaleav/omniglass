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

// newChoiceTestGateway opens a fresh, seeded gateway for the choice write
// surface tests, the same setup TestEffectiveSlotsAndAssignment uses. dsn is
// returned too so a caller that needs a raw connection (asserting a column
// no Gateway read exposes, such as system_slot.alternate_id) can open one
// against the same database.
func newChoiceTestGateway(t *testing.T) (*storage.PG, context.Context, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw, ctx, dsn
}

// plantChoice creates the standard std with one choice through the audited
// create (CreateStandardFromTemplate) and returns the choice's alternate ids
// by name, resolved the way an operator reference resolves.
func plantChoice(t *testing.T, ctx context.Context, gw *storage.PG, std string, choice storage.SlotChoiceSpec) map[string]string {
	t.Helper()
	if _, err := gw.CreateStandardFromTemplate(ctx, "", storage.StandardFromTemplate{
		Template: "test", Name: std, Label: std, Choices: []storage.SlotChoiceSpec{choice},
	}); err != nil {
		t.Fatalf("create standard %s with choice %s: %v", std, choice.Name, err)
	}
	alts := make(map[string]string, len(choice.Alternates))
	for _, a := range choice.Alternates {
		id, err := gw.ResolveAlternate(ctx, "standard", std, choice.Name+"/"+a.Name)
		if err != nil {
			t.Fatalf("resolve alternate %s/%s: %v", choice.Name, a.Name, err)
		}
		alts[a.Name] = id
	}
	return alts
}

// TestUnknownOwnerRefused pins the system_slot owner-arc repair (#626,
// memo amendment 7.5): slotOwnerExpr resolves an unknown standard or system
// name to NULL, and before the migration's system_slot_owner_arc_check
// nothing refused the resulting insert, silently creating an ownerless slot
// that a second identically-typo'd write would then update under
// system_slot_name_key's NULLS NOT DISTINCT.
func TestUnknownOwnerRefused(t *testing.T) {
	t.Parallel()
	gw, ctx, _ := newChoiceTestGateway(t)

	_, err := gw.SetSystemSlot(ctx, "", "standard", "no-such-standard-at-all", storage.SystemSlotSpec{
		Name: "ghost", Label: "Ghost", Quorum: 1,
	})
	if !errors.Is(err, storage.ErrSlotRefNotFound) {
		t.Fatalf("SetSystemSlot with a bogus standard id = %v, want ErrSlotRefNotFound", err)
	}

	// A second, differently-bogus owner must not silently collide with (and
	// update) the first refused attempt: neither should have landed a row
	// at all.
	_, err = gw.SetSystemSlot(ctx, "", "standard", "also-no-such-standard", storage.SystemSlotSpec{
		Name: "ghost", Label: "Ghost", Quorum: 1,
	})
	if !errors.Is(err, storage.ErrSlotRefNotFound) {
		t.Fatalf("second bogus owner = %v, want ErrSlotRefNotFound (no orphan to collide with)", err)
	}
}

// TestSlotCannotJoinForeignAlternate is memo amendment 7.2's blocking
// correction: system_slot_alternate_fk is a composite FK over (alternate_id,
// owner_kind, standard_id, system_id), so a slot cannot join an alternate
// that belongs to a different owner. The refusal must be the same
// ErrSlotRefNotFound (422) a bad accepted type or pinned product already
// gets, not a bare constraint violation.
func TestSlotCannotJoinForeignAlternate(t *testing.T) {
	t.Parallel()
	gw, ctx, _ := newChoiceTestGateway(t)

	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "foreign-std-b", Label: "foreign-std-b"}); err != nil {
		t.Fatalf("create standard foreign-std-b: %v", err)
	}
	alts := plantChoice(t, ctx, gw, "foreign-std-a", storage.SlotChoiceSpec{
		Name: "conferencing", Label: "Conferencing",
		Alternates: []storage.AlternateSpec{{Name: "all-in-one", Label: "All-in-one"}},
	})
	foreignAlt := alts["all-in-one"]
	if foreignAlt == "" {
		t.Fatalf("plantChoice returned no id for all-in-one")
	}

	_, err := gw.SetSystemSlot(ctx, "", "standard", "foreign-std-b", storage.SystemSlotSpec{
		Name: "video-bar", Label: "Video Bar", Quorum: 1, AlternateID: strp(foreignAlt),
	})
	if !errors.Is(err, storage.ErrSlotRefNotFound) {
		t.Fatalf("slot on std-b joining std-a's alternate = %v, want ErrSlotRefNotFound (422, not 500)", err)
	}

	// The matching owner is unaffected: the same alternate id under its own
	// standard succeeds.
	if _, err := gw.SetSystemSlot(ctx, "", "standard", "foreign-std-a", storage.SystemSlotSpec{
		Name: "video-bar", Label: "Video Bar", Quorum: 1, AlternateID: strp(foreignAlt),
	}); err != nil {
		t.Fatalf("slot joining its own owner's alternate: %v", err)
	}
}

// TestDeletingAlternateWithSlotsRefused is memo amendment 7.1's blocking
// correction: alternate_id is ON DELETE RESTRICT, never SET NULL, because a
// slot in no alternate reads as unconditional. DeleteAlternate and
// DeleteChoice refuse with ChoiceInUseShortfall naming the slots still
// attached, rather than let the constraint surface as a bare violation (or,
// worse, silently promote every slot of a deleted alternate to mandatory).
func TestDeletingAlternateWithSlotsRefused(t *testing.T) {
	t.Parallel()
	gw, ctx, _ := newChoiceTestGateway(t)

	std := "delete-refused-std"
	alts := plantChoice(t, ctx, gw, std, storage.SlotChoiceSpec{
		Name: "conferencing", Label: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", Label: "All-in-one"},
			{Name: "component-system", Label: "Component System"},
		},
	})
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "video-bar", Label: "Video Bar", Quorum: 1, AlternateID: strp(alts["all-in-one"]),
	}); err != nil {
		t.Fatalf("declare slot: %v", err)
	}

	err := gw.DeleteAlternate(ctx, "", "standard", std, "conferencing", "all-in-one")
	var shortfall *storage.ChoiceInUseShortfall
	if !errors.As(err, &shortfall) {
		t.Fatalf("delete an alternate with a slot attached = %v, want ChoiceInUseShortfall", err)
	}
	if shortfall.Choice != "conferencing" || shortfall.Alternate != "all-in-one" ||
		len(shortfall.Slots) != 1 || shortfall.Slots[0] != "video-bar" {
		t.Fatalf("shortfall = %+v, want conferencing/all-in-one naming video-bar", shortfall)
	}

	// The untouched alternate has no slots, so deleting it succeeds.
	if err := gw.DeleteAlternate(ctx, "", "standard", std, "conferencing", "component-system"); err != nil {
		t.Fatalf("delete unattached alternate: %v", err)
	}

	// DeleteChoice mirrors the same refusal at the group level.
	err = gw.DeleteChoice(ctx, "", "standard", std, "conferencing")
	if !errors.As(err, &shortfall) {
		t.Fatalf("delete a choice with a slot attached = %v, want ChoiceInUseShortfall", err)
	}
	if shortfall.Choice != "conferencing" || shortfall.Alternate != "" || len(shortfall.Slots) != 1 {
		t.Fatalf("choice-level shortfall = %+v, want conferencing naming video-bar with no alternate", shortfall)
	}

	// Detach the slot, then both deletes succeed.
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "video-bar", Label: "Video Bar", Quorum: 1, AlternateID: strp(""),
	}); err != nil {
		t.Fatalf("detach slot: %v", err)
	}
	if err := gw.DeleteChoice(ctx, "", "standard", std, "conferencing"); err != nil {
		t.Fatalf("delete choice once empty: %v", err)
	}
}

// TestDetachedSlotBecomesUnconditional is memo amendment 7.1's explicit
// path, the mirror of the forbidden implicit one: re-declaring a slot
// without its alternate (rather than deleting the alternate out from under
// it) moves it from conditional to unconditional, and it then counts on its
// own regardless of whether its former choice is satisfied.
func TestDetachedSlotBecomesUnconditional(t *testing.T) {
	t.Parallel()
	gw, ctx, _ := newChoiceTestGateway(t)
	all := scopeAll()

	std := "detach-std"
	alts := plantChoice(t, ctx, gw, std, storage.SlotChoiceSpec{
		Name: "conferencing", Label: "Conferencing",
		Alternates: []storage.AlternateSpec{{Name: "a", Label: "A"}, {Name: "b", Label: "B"}},
	})
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "slot-a", Label: "Slot A", Quorum: 1, Impact: "outage", AlternateID: strp(alts["a"]),
	}); err != nil {
		t.Fatalf("declare slot-a: %v", err)
	}
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "slot-b", Label: "Slot B", Quorum: 1, Impact: "outage", AlternateID: strp(alts["b"]),
	}); err != nil {
		t.Fatalf("declare slot-b: %v", err)
	}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "detach-sys", StandardID: &std}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	product := storagetest.MintProduct(t, ctx, gw, "").Name
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "detach-comp", ProductName: &product}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignSlot(ctx, "", "detach-sys", "slot-b", "detach-comp", all, all, all, all); err != nil {
		t.Fatalf("assign slot-b: %v", err)
	}

	// Alternate b is satisfied, slot-a is unstaffed but grouped: the choice
	// is answered through b, so the system reads healthy.
	rep, err := gw.SystemHealth(ctx, "detach-sys", 0, all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if rep.Verdict != "healthy" {
		t.Fatalf("verdict with the choice satisfied via b = %q, want healthy", rep.Verdict)
	}

	// Detach slot-a explicitly (re-declare with no alternate), the sanctioned
	// path #626 keeps open now that deleting the alternate itself is refused.
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "slot-a", Label: "Slot A", Quorum: 1, Impact: "outage", AlternateID: strp(""),
	}); err != nil {
		t.Fatalf("detach slot-a: %v", err)
	}

	rep, err = gw.SystemHealth(ctx, "detach-sys", 0, all)
	if err != nil {
		t.Fatalf("system health after detach: %v", err)
	}
	if rep.Verdict != "incomplete" {
		t.Fatalf("verdict after detaching slot-a = %q, want incomplete: unstaffed, it must now count unconditionally "+
			"even though the choice it left is still satisfied via b", rep.Verdict)
	}
}

// TestEditingSlotLeavesAlternateAlone is the fix-round regression for the
// critical finding: SetSystemSlot used to wholesale-replace alternate_id on
// every write, and no API route ever populated it, so ANY edit through the
// existing PUT routes (including the console's own slot-editor save, which
// sends label, quorum, accepted_types, pinned_products and impact but
// has no concept of a choice) silently wrote NULL over a slot's alternate
// and promoted it from conditional to mandatory. This mirrors the console's
// save exactly: a second SetSystemSlot call that changes only label
// and omits AlternateID (nil, "the caller did not mention this field") must
// leave the stored alternate_id untouched and the system's verdict
// unmoved, not just the recorded string but the live column too.
func TestEditingSlotLeavesAlternateAlone(t *testing.T) {
	t.Parallel()
	gw, ctx, dsn := newChoiceTestGateway(t)
	all := scopeAll()

	std := "edit-preserve-std"
	alts := plantChoice(t, ctx, gw, std, storage.SlotChoiceSpec{
		Name: "conferencing", Label: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", Label: "All-in-one"},
			{Name: "component-system", Label: "Component System"},
		},
	})
	// component-system has no slots declared in this test at all, so
	// all-in-one is the only real candidate and wins the tie by default;
	// what matters here is only that conf-bar stays grouped under it.
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "conf-bar", Label: "Conferencing Bar", Quorum: 1, Impact: "outage",
		AcceptedTypes: []string{"video-bar"}, AlternateID: strp(alts["all-in-one"]),
	}); err != nil {
		t.Fatalf("declare slot: %v", err)
	}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edit-sys", StandardID: &std}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar").Name
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "edit-comp", ProductName: &bar}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignSlot(ctx, "", "edit-sys", "conf-bar", "edit-comp", all, all, all, all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	before, err := gw.SystemHealth(ctx, "edit-sys", 0, all)
	if err != nil {
		t.Fatalf("system health before edit: %v", err)
	}
	if before.Verdict != "healthy" {
		t.Fatalf("verdict before edit = %q, want healthy", before.Verdict)
	}

	// The regression: an edit that changes only label and does not
	// mention AlternateID at all (the zero value of *string is nil, exactly
	// what slotSpec builds from a request body whose "alternate" field was
	// omitted).
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "conf-bar", Label: "Conferencing Video Bar", Quorum: 1, Impact: "outage",
		AcceptedTypes: []string{"video-bar"},
	}); err != nil {
		t.Fatalf("edit label: %v", err)
	}

	after, err := gw.SystemHealth(ctx, "edit-sys", 0, all)
	if err != nil {
		t.Fatalf("system health after edit: %v", err)
	}
	if after.Verdict != "healthy" {
		t.Fatalf("verdict after an unrelated label edit = %q, want healthy: "+
			"alternate_id must survive an edit that does not mention it", after.Verdict)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var altID, altName string
	if err := conn.QueryRow(ctx, `
		select sr.alternate_id, ca.name from system_slot sr
		join choice_alternate ca on ca.id = sr.alternate_id
		where sr.name = 'conf-bar' and sr.standard_id = (select id from standard where name = $1)`,
		std).Scan(&altID, &altName); err != nil {
		t.Fatalf("read conf-bar alternate_id after edit: %v (a NULL alternate_id, the regression, "+
			"fails this join and scans no row)", err)
	}
	if altID != alts["all-in-one"] || altName != "all-in-one" {
		t.Fatalf("conf-bar alternate after edit = %s (%s), want %s (all-in-one)", altID, altName, alts["all-in-one"])
	}
}

// TestHealthSlotReportsChoiceAndActive is the fix-round regression for the
// second important finding: SystemHealth used to compute the choice-aware
// verdict beside a slot list with no choice or alternate identity at all, so
// the served report contradicted itself on a system with a choice: healthy
// beside impaired: true rows and nothing to say those rows were not why. A
// consumer (or an operator reading HealthPanel) has to be able to tell "this
// slot is impaired AND that mattered" apart from "this slot is impaired but
// its alternate lost", which is exactly what Active answers.
func TestHealthSlotReportsChoiceAndActive(t *testing.T) {
	t.Parallel()
	gw, ctx, _ := newChoiceTestGateway(t)
	all := scopeAll()

	std := "report-active-std"
	alts := plantChoice(t, ctx, gw, std, storage.SlotChoiceSpec{
		Name: "conferencing", Label: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", Label: "All-in-one"},
			{Name: "component-system", Label: "Component System"},
		},
	})
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "conf-bar", Label: "Conferencing Bar", Quorum: 1, Impact: "outage",
		AcceptedTypes: []string{"video-bar"}, AlternateID: strp(alts["all-in-one"]),
	}); err != nil {
		t.Fatalf("declare all-in-one slot: %v", err)
	}
	for _, name := range []string{"conf-codec", "conf-camera"} {
		if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
			Name: name, Label: name, Quorum: 1, Impact: "outage", AlternateID: strp(alts["component-system"]),
		}); err != nil {
			t.Fatalf("declare component-system slot %s: %v", name, err)
		}
	}
	// An unconditional slot beside the choice: it must report Active true
	// and empty Choice/Alternate, the same as before #626 existed.
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "screen", Label: "Screen", Quorum: 1, Impact: "outage",
	}); err != nil {
		t.Fatalf("declare unconditional slot: %v", err)
	}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "report-sys", StandardID: &std}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar").Name
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "report-bar", ProductName: &bar}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignSlot(ctx, "", "report-sys", "conf-bar", "report-bar", all, all, all, all); err != nil {
		t.Fatalf("assign: %v", err)
	}

	rep, err := gw.SystemHealth(ctx, "report-sys", 0, all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	// screen is unstaffed and unconditional, so the verdict is incomplete
	// (#631: a box nobody installed, not a box that broke), not healthy: this
	// pins that an unconditional slot beside a satisfied choice still counts,
	// the same assertion TestSystemVerdictWithChoices makes at the pure level.
	// It pinned outage (screen's declared impact) before the incomplete
	// verdict landed; impact describes failure only now.
	if rep.Verdict != "incomplete" {
		t.Fatalf("verdict = %q, want incomplete (screen, unconditional and unstaffed, still counts)", rep.Verdict)
	}

	byName := map[string]storage.HealthSlot{}
	for _, r := range rep.Slots {
		byName[r.Name] = r
	}
	if len(byName) != 4 {
		t.Fatalf("slots reported = %d, want 4 (conf-bar, conf-codec, conf-camera, screen): %+v", len(byName), rep.Slots)
	}

	confBar := byName["conf-bar"]
	if confBar.Choice != "conferencing" || confBar.Alternate != "all-in-one" || !confBar.Active || confBar.Impaired {
		t.Fatalf("conf-bar = %+v, want choice conferencing, alternate all-in-one, active true, not impaired", confBar)
	}
	for _, name := range []string{"conf-codec", "conf-camera"} {
		r := byName[name]
		if r.Choice != "conferencing" || r.Alternate != "component-system" || r.Active || !r.Impaired {
			t.Fatalf("%s = %+v, want choice conferencing, alternate component-system, active FALSE, impaired true "+
				"(its alternate lost to all-in-one, so it must not read as the reason for the verdict)", name, r)
		}
	}
	screen := byName["screen"]
	if screen.Choice != "" || screen.Alternate != "" || !screen.Active || !screen.Impaired {
		t.Fatalf("screen = %+v, want empty choice/alternate, active true, impaired true "+
			"(unconditional and unstaffed: it IS the reason for the incomplete verdict)", screen)
	}
}
