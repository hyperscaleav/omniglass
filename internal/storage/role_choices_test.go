package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// newChoiceTestGateway opens a fresh, seeded gateway for the choice write
// surface tests, the same setup TestEffectiveRolesAndAssignment uses. dsn is
// returned too so a caller that needs a raw connection (asserting a column
// no Gateway read exposes, such as system_role.alternate_id) can open one
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

// TestUnknownOwnerRefused pins the system_role owner-arc repair (#626,
// memo amendment 7.5): roleOwnerExpr resolves an unknown standard or system
// name to NULL, and before the migration's system_role_owner_arc_check
// nothing refused the resulting insert, silently creating an ownerless role
// that a second identically-typo'd write would then update under
// system_role_name_key's NULLS NOT DISTINCT.
func TestUnknownOwnerRefused(t *testing.T) {
	gw, ctx, _ := newChoiceTestGateway(t)

	_, err := gw.SetSystemRole(ctx, "", "standard", "no-such-standard-at-all", storage.SystemRoleSpec{
		Name: "ghost", DisplayName: "Ghost", Quorum: 1,
	})
	if !errors.Is(err, storage.ErrRoleRefNotFound) {
		t.Fatalf("SetSystemRole with a bogus standard id = %v, want ErrRoleRefNotFound", err)
	}

	// A second, differently-bogus owner must not silently collide with (and
	// update) the first refused attempt: neither should have landed a row
	// at all.
	_, err = gw.SetSystemRole(ctx, "", "standard", "also-no-such-standard", storage.SystemRoleSpec{
		Name: "ghost", DisplayName: "Ghost", Quorum: 1,
	})
	if !errors.Is(err, storage.ErrRoleRefNotFound) {
		t.Fatalf("second bogus owner = %v, want ErrRoleRefNotFound (no orphan to collide with)", err)
	}
}

// TestRoleCannotJoinForeignAlternate is memo amendment 7.2's blocking
// correction: system_role_alternate_fk is a composite FK over (alternate_id,
// owner_kind, standard_id, system_id), so a role cannot join an alternate
// that belongs to a different owner. The refusal must be the same
// ErrRoleRefNotFound (422) a bad accepted type or pinned product already
// gets, not a bare constraint violation.
func TestRoleCannotJoinForeignAlternate(t *testing.T) {
	gw, ctx, _ := newChoiceTestGateway(t)

	for _, name := range []string{"foreign-std-a", "foreign-std-b"} {
		if err := gw.UpsertStandard(ctx, storage.Standard{Name: name, DisplayName: name}); err != nil {
			t.Fatalf("create standard %s: %v", name, err)
		}
	}
	alts, err := gw.SeedRoleChoice(ctx, "standard", "foreign-std-a", storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{{Name: "all-in-one", DisplayName: "All-in-one"}},
	})
	if err != nil {
		t.Fatalf("seed choice on std-a: %v", err)
	}
	foreignAlt := alts["all-in-one"]
	if foreignAlt == "" {
		t.Fatalf("seed choice returned no id for all-in-one")
	}

	_, err = gw.SetSystemRole(ctx, "", "standard", "foreign-std-b", storage.SystemRoleSpec{
		Name: "video-bar", DisplayName: "Video Bar", Quorum: 1, AlternateID: strp(foreignAlt),
	})
	if !errors.Is(err, storage.ErrRoleRefNotFound) {
		t.Fatalf("role on std-b joining std-a's alternate = %v, want ErrRoleRefNotFound (422, not 500)", err)
	}

	// The matching owner is unaffected: the same alternate id under its own
	// standard succeeds.
	if _, err := gw.SetSystemRole(ctx, "", "standard", "foreign-std-a", storage.SystemRoleSpec{
		Name: "video-bar", DisplayName: "Video Bar", Quorum: 1, AlternateID: strp(foreignAlt),
	}); err != nil {
		t.Fatalf("role joining its own owner's alternate: %v", err)
	}
}

// TestDeletingAlternateWithRolesRefused is memo amendment 7.1's blocking
// correction: alternate_id is ON DELETE RESTRICT, never SET NULL, because a
// role in no alternate reads as unconditional. DeleteAlternate and
// DeleteChoice refuse with ChoiceInUseShortfall naming the roles still
// attached, rather than let the constraint surface as a bare violation (or,
// worse, silently promote every role of a deleted alternate to mandatory).
func TestDeletingAlternateWithRolesRefused(t *testing.T) {
	gw, ctx, _ := newChoiceTestGateway(t)

	std := "delete-refused-std"
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: std, DisplayName: "Delete Refused"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	alts, err := gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", DisplayName: "All-in-one"},
			{Name: "component-system", DisplayName: "Component System"},
		},
	})
	if err != nil {
		t.Fatalf("seed choice: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "video-bar", DisplayName: "Video Bar", Quorum: 1, AlternateID: strp(alts["all-in-one"]),
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	err = gw.DeleteAlternate(ctx, "", "standard", std, "conferencing", "all-in-one")
	var shortfall *storage.ChoiceInUseShortfall
	if !errors.As(err, &shortfall) {
		t.Fatalf("delete an alternate with a role attached = %v, want ChoiceInUseShortfall", err)
	}
	if shortfall.Choice != "conferencing" || shortfall.Alternate != "all-in-one" ||
		len(shortfall.Roles) != 1 || shortfall.Roles[0] != "video-bar" {
		t.Fatalf("shortfall = %+v, want conferencing/all-in-one naming video-bar", shortfall)
	}

	// The untouched alternate has no roles, so deleting it succeeds.
	if err := gw.DeleteAlternate(ctx, "", "standard", std, "conferencing", "component-system"); err != nil {
		t.Fatalf("delete unattached alternate: %v", err)
	}

	// DeleteChoice mirrors the same refusal at the group level.
	err = gw.DeleteChoice(ctx, "", "standard", std, "conferencing")
	if !errors.As(err, &shortfall) {
		t.Fatalf("delete a choice with a role attached = %v, want ChoiceInUseShortfall", err)
	}
	if shortfall.Choice != "conferencing" || shortfall.Alternate != "" || len(shortfall.Roles) != 1 {
		t.Fatalf("choice-level shortfall = %+v, want conferencing naming video-bar with no alternate", shortfall)
	}

	// Detach the role, then both deletes succeed.
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "video-bar", DisplayName: "Video Bar", Quorum: 1, AlternateID: strp(""),
	}); err != nil {
		t.Fatalf("detach role: %v", err)
	}
	if err := gw.DeleteChoice(ctx, "", "standard", std, "conferencing"); err != nil {
		t.Fatalf("delete choice once empty: %v", err)
	}
}

// TestDetachedRoleBecomesUnconditional is memo amendment 7.1's explicit
// path, the mirror of the forbidden implicit one: re-declaring a role
// without its alternate (rather than deleting the alternate out from under
// it) moves it from conditional to unconditional, and it then counts on its
// own regardless of whether its former choice is satisfied.
func TestDetachedRoleBecomesUnconditional(t *testing.T) {
	gw, ctx, _ := newChoiceTestGateway(t)
	all := scopeAll()

	std := "detach-std"
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: std, DisplayName: "Detach"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	alts, err := gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{{Name: "a", DisplayName: "A"}, {Name: "b", DisplayName: "B"}},
	})
	if err != nil {
		t.Fatalf("seed choice: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "role-a", DisplayName: "Role A", Quorum: 1, Impact: "outage", AlternateID: strp(alts["a"]),
	}); err != nil {
		t.Fatalf("declare role-a: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "role-b", DisplayName: "Role B", Quorum: 1, Impact: "outage", AlternateID: strp(alts["b"]),
	}); err != nil {
		t.Fatalf("declare role-b: %v", err)
	}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "detach-sys", StandardID: &std}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "detach-comp", ProductName: &bar}, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "detach-sys", "role-b", "detach-comp", all); err != nil {
		t.Fatalf("assign role-b: %v", err)
	}

	// Alternate b is satisfied, role-a is unstaffed but grouped: the choice
	// is answered through b, so the system reads healthy.
	rep, err := gw.SystemHealth(ctx, "detach-sys", time.Time{}, all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if rep.Verdict != "healthy" {
		t.Fatalf("verdict with the choice satisfied via b = %q, want healthy", rep.Verdict)
	}

	// Detach role-a explicitly (re-declare with no alternate), the sanctioned
	// path #626 keeps open now that deleting the alternate itself is refused.
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "role-a", DisplayName: "Role A", Quorum: 1, Impact: "outage", AlternateID: strp(""),
	}); err != nil {
		t.Fatalf("detach role-a: %v", err)
	}

	rep, err = gw.SystemHealth(ctx, "detach-sys", time.Time{}, all)
	if err != nil {
		t.Fatalf("system health after detach: %v", err)
	}
	if rep.Verdict != "outage" {
		t.Fatalf("verdict after detaching role-a = %q, want outage: it must now count unconditionally "+
			"even though the choice it left is still satisfied via b", rep.Verdict)
	}
}

// TestEditingRoleLeavesAlternateAlone is the fix-round regression for the
// critical finding: SetSystemRole used to wholesale-replace alternate_id on
// every write, and no API route ever populated it, so ANY edit through the
// existing PUT routes (including the console's own role-editor save, which
// sends display_name, quorum, accepted_types, pinned_products and impact but
// has no concept of a choice) silently wrote NULL over a role's alternate
// and promoted it from conditional to mandatory. This mirrors the console's
// save exactly: a second SetSystemRole call that changes only display_name
// and omits AlternateID (nil, "the caller did not mention this field") must
// leave the stored alternate_id untouched and the system's verdict
// unmoved, not just the recorded string but the live column too.
func TestEditingRoleLeavesAlternateAlone(t *testing.T) {
	gw, ctx, dsn := newChoiceTestGateway(t)
	all := scopeAll()

	std := "edit-preserve-std"
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: std, DisplayName: "Edit Preserve"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	alts, err := gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", DisplayName: "All-in-one"},
			{Name: "component-system", DisplayName: "Component System"},
		},
	})
	if err != nil {
		t.Fatalf("seed choice: %v", err)
	}
	// component-system has no roles declared in this test at all, so
	// all-in-one is the only real candidate and wins the tie by default;
	// what matters here is only that conf-bar stays grouped under it.
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "conf-bar", DisplayName: "Conferencing Bar", Quorum: 1, Impact: "outage",
		AcceptedTypes: []string{"video-bar"}, AlternateID: strp(alts["all-in-one"]),
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edit-sys", StandardID: &std}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "edit-comp", ProductName: &bar}, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "edit-sys", "conf-bar", "edit-comp", all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	before, err := gw.SystemHealth(ctx, "edit-sys", time.Time{}, all)
	if err != nil {
		t.Fatalf("system health before edit: %v", err)
	}
	if before.Verdict != "healthy" {
		t.Fatalf("verdict before edit = %q, want healthy", before.Verdict)
	}

	// The regression: an edit that changes only display_name and does not
	// mention AlternateID at all (the zero value of *string is nil, exactly
	// what roleSpec builds from a request body whose "alternate" field was
	// omitted).
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "conf-bar", DisplayName: "Conferencing Video Bar", Quorum: 1, Impact: "outage",
		AcceptedTypes: []string{"video-bar"},
	}); err != nil {
		t.Fatalf("edit display_name: %v", err)
	}

	after, err := gw.SystemHealth(ctx, "edit-sys", time.Time{}, all)
	if err != nil {
		t.Fatalf("system health after edit: %v", err)
	}
	if after.Verdict != "healthy" {
		t.Fatalf("verdict after an unrelated display_name edit = %q, want healthy: "+
			"alternate_id must survive an edit that does not mention it", after.Verdict)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var altID, altName string
	if err := conn.QueryRow(ctx, `
		select sr.alternate_id, ca.name from system_role sr
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

// TestHealthRoleReportsChoiceAndActive is the fix-round regression for the
// second important finding: SystemHealth used to compute the choice-aware
// verdict beside a role list with no choice or alternate identity at all, so
// the served report contradicted itself on a system with a choice: healthy
// beside impaired: true rows and nothing to say those rows were not why. A
// consumer (or an operator reading HealthPanel) has to be able to tell "this
// role is impaired AND that mattered" apart from "this role is impaired but
// its alternate lost", which is exactly what Active answers.
func TestHealthRoleReportsChoiceAndActive(t *testing.T) {
	gw, ctx, _ := newChoiceTestGateway(t)
	all := scopeAll()

	std := "report-active-std"
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: std, DisplayName: "Report Active"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	alts, err := gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", DisplayName: "All-in-one"},
			{Name: "component-system", DisplayName: "Component System"},
		},
	})
	if err != nil {
		t.Fatalf("seed choice: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "conf-bar", DisplayName: "Conferencing Bar", Quorum: 1, Impact: "outage",
		AcceptedTypes: []string{"video-bar"}, AlternateID: strp(alts["all-in-one"]),
	}); err != nil {
		t.Fatalf("declare all-in-one role: %v", err)
	}
	for _, name := range []string{"conf-codec", "conf-camera"} {
		if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
			Name: name, DisplayName: name, Quorum: 1, Impact: "outage", AlternateID: strp(alts["component-system"]),
		}); err != nil {
			t.Fatalf("declare component-system role %s: %v", name, err)
		}
	}
	// An unconditional role beside the choice: it must report Active true
	// and empty Choice/Alternate, the same as before #626 existed.
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "screen", DisplayName: "Screen", Quorum: 1, Impact: "outage",
	}); err != nil {
		t.Fatalf("declare unconditional role: %v", err)
	}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "report-sys", StandardID: &std}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "report-bar", ProductName: &bar}, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "report-sys", "conf-bar", "report-bar", all); err != nil {
		t.Fatalf("assign: %v", err)
	}

	rep, err := gw.SystemHealth(ctx, "report-sys", time.Time{}, all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	// screen is unstaffed and unconditional, so the verdict is outage
	// (screen's own impact), not healthy: this pins that an unconditional
	// role beside a satisfied choice still counts, the same assertion
	// TestSystemVerdictWithChoices makes at the pure level.
	if rep.Verdict != "outage" {
		t.Fatalf("verdict = %q, want outage (screen, unconditional and unstaffed, still counts)", rep.Verdict)
	}

	byName := map[string]storage.HealthRole{}
	for _, r := range rep.Roles {
		byName[r.Name] = r
	}
	if len(byName) != 4 {
		t.Fatalf("roles reported = %d, want 4 (conf-bar, conf-codec, conf-camera, screen): %+v", len(byName), rep.Roles)
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
			"(unconditional and unstaffed: it IS the reason for the outage verdict)", screen)
	}
}

// TestSeedRoleChoiceConvergesOnReorderedPositions is the fix-round
// regression for the third important finding: SeedRoleChoice used to derive
// an alternate's position from its 1-based index in spec.Alternates on
// every call, arbitrating the upsert only on (choice_id, name). A release
// that inserts a new alternate ahead of existing ones (or reorders them)
// then collides on choice_alternate_position_key and aborts server boot,
// because the new name's insert lands on a position an existing row still
// holds. This seeds one shape, then a genuinely reordered one (a new
// alternate inserted at the head) into the SAME database, asserting boot
// succeeds and that every alternate, new and existing, ends up at its
// newly-declared position.
func TestSeedRoleChoiceConvergesOnReorderedPositions(t *testing.T) {
	gw, ctx, dsn := newChoiceTestGateway(t)

	std := "reorder-std"
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: std, DisplayName: "Reorder"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	alts1, err := gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", DisplayName: "All-in-one"},
			{Name: "component-system", DisplayName: "Component System"},
		},
	})
	if err != nil {
		t.Fatalf("seed first shape: %v", err)
	}

	// A later release inserts a new alternate at the HEAD of the list,
	// pushing the two existing ones back a position each: a genuine,
	// non-append reorder. The pre-fix version aborted server boot here.
	alts2, err := gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "hybrid", DisplayName: "Hybrid"},
			{Name: "all-in-one", DisplayName: "All-in-one"},
			{Name: "component-system", DisplayName: "Component System"},
		},
	})
	if err != nil {
		t.Fatalf("seed reordered shape: %v", err)
	}

	// The two names that already existed keep their ids: reconciling
	// position must not have deleted and recreated them.
	if alts2["all-in-one"] != alts1["all-in-one"] || alts2["component-system"] != alts1["component-system"] {
		t.Fatalf("reorder changed existing alternate ids: before %v, after %v", alts1, alts2)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	want := map[string]int{"hybrid": 1, "all-in-one": 2, "component-system": 3}
	for name, wantPos := range want {
		var pos int
		if err := conn.QueryRow(ctx, `select position from choice_alternate where id = $1`, alts2[name]).Scan(&pos); err != nil {
			t.Fatalf("read %s position: %v", name, err)
		}
		if pos != wantPos {
			t.Errorf("%s position = %d, want %d (converged on the new declared order)", name, pos, wantPos)
		}
	}
}

// TestSeedRoleChoiceReconcilesRenamesAndDrops is the second round of the
// third finding's fix: converging position for the DECLARED set is not
// enough, because a rename or a drop against an already-seeded database is
// a name dropping OUT of spec.Alternates, and the pre-fix SeedRoleChoice
// never touched a stored row whose name was no longer declared. That row
// kept its old position, and the newly-declared name (the rename's other
// half, or any alternate whose declared position happened to land where the
// orphan still sat) collided with it at commit: a rename is a drop plus an
// add on this lane, indistinguishable from a genuine removal without
// "renamed from" tracking the seed does not have, so both must reconcile
// the same way. Covers three cases against a database that already ran the
// first seed: a pure drop, a rename, and a drop that is refused because a
// role still points at the alternate being removed (the FK is RESTRICT;
// refusing and naming the role is more honest than a silent cascade that
// would detach it).
func TestSeedRoleChoiceReconcilesRenamesAndDrops(t *testing.T) {
	gw, ctx, dsn := newChoiceTestGateway(t)

	std := "reconcile-std"
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: std, DisplayName: "Reconcile"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	first, err := gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", DisplayName: "All-in-one"},
			{Name: "component-system", DisplayName: "Component System"},
			{Name: "legacy", DisplayName: "Legacy"},
		},
	})
	if err != nil {
		t.Fatalf("seed first shape: %v", err)
	}

	// A role holds "legacy": dropping it must be refused, not silently
	// detach the role or crash on the FK.
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "legacy-role", DisplayName: "Legacy Role", Quorum: 1, AlternateID: strp(first["legacy"]),
	}); err != nil {
		t.Fatalf("declare legacy role: %v", err)
	}

	// Drop "legacy" (still holds a role) and rename "component-system" to
	// "hybrid", keeping "all-in-one" untouched.
	_, err = gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", DisplayName: "All-in-one"},
			{Name: "hybrid", DisplayName: "Hybrid"},
		},
	})
	var shortfall *storage.ChoiceInUseShortfall
	if !errors.As(err, &shortfall) {
		t.Fatalf("reseed dropping legacy (still holds a role) = %v, want ChoiceInUseShortfall", err)
	}
	if shortfall.Alternate != "legacy" || len(shortfall.Roles) != 1 || shortfall.Roles[0] != "legacy-role" {
		t.Fatalf("shortfall = %+v, want conferencing/legacy naming legacy-role", shortfall)
	}

	// The refusal rolled back the whole call: legacy is still there,
	// unchanged, and nothing about component-system moved either.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var stillThere int
	if err := conn.QueryRow(ctx, `select count(*) from choice_alternate where id = $1`, first["legacy"]).Scan(&stillThere); err != nil {
		t.Fatalf("count legacy: %v", err)
	}
	if stillThere != 1 {
		t.Fatalf("legacy rows after refused reseed = %d, want 1 (the refusal must not have deleted it)", stillThere)
	}

	// Detach the role, then the same reseed (drop legacy, rename
	// component-system to hybrid) must succeed.
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "legacy-role", DisplayName: "Legacy Role", Quorum: 1, AlternateID: strp(""),
	}); err != nil {
		t.Fatalf("detach legacy-role: %v", err)
	}
	second, err := gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", DisplayName: "All-in-one"},
			{Name: "hybrid", DisplayName: "Hybrid"},
		},
	})
	if err != nil {
		t.Fatalf("reseed after detaching legacy-role: %v", err)
	}

	// all-in-one kept its id (untouched by the reorder/rename around it);
	// legacy and the old component-system name are both gone; hybrid is a
	// fresh row, not component-system reused under a new name (the seed has
	// no rename tracking, so this is the honest outcome).
	if second["all-in-one"] != first["all-in-one"] {
		t.Fatalf("all-in-one id changed across reseed: %s -> %s", first["all-in-one"], second["all-in-one"])
	}
	if second["hybrid"] == first["component-system"] {
		t.Fatalf("hybrid reused component-system's id; the seed has no rename tracking, this must be a fresh row")
	}
	for _, id := range []string{first["legacy"], first["component-system"]} {
		var count int
		if err := conn.QueryRow(ctx, `select count(*) from choice_alternate where id = $1`, id).Scan(&count); err != nil {
			t.Fatalf("count dropped row %s: %v", id, err)
		}
		if count != 0 {
			t.Errorf("dropped alternate %s still has %d row(s), want 0", id, count)
		}
	}
	var allInOnePos, hybridPos int
	if err := conn.QueryRow(ctx, `select position from choice_alternate where id = $1`, second["all-in-one"]).Scan(&allInOnePos); err != nil {
		t.Fatalf("read all-in-one position: %v", err)
	}
	if err := conn.QueryRow(ctx, `select position from choice_alternate where id = $1`, second["hybrid"]).Scan(&hybridPos); err != nil {
		t.Fatalf("read hybrid position: %v", err)
	}
	if allInOnePos != 1 || hybridPos != 2 {
		t.Errorf("positions after reconcile: all-in-one=%d, hybrid=%d, want 1, 2", allInOnePos, hybridPos)
	}
}
