package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// newChoiceTestGateway opens a fresh, seeded gateway for the choice write
// surface tests, the same setup TestEffectiveRolesAndAssignment uses.
func newChoiceTestGateway(t *testing.T) (*storage.PG, context.Context) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw, ctx
}

// TestUnknownOwnerRefused pins the system_role owner-arc repair (#626,
// memo amendment 7.5): roleOwnerExpr resolves an unknown standard or system
// name to NULL, and before the migration's system_role_owner_arc_check
// nothing refused the resulting insert, silently creating an ownerless role
// that a second identically-typo'd write would then update under
// system_role_name_key's NULLS NOT DISTINCT.
func TestUnknownOwnerRefused(t *testing.T) {
	gw, ctx := newChoiceTestGateway(t)

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
	gw, ctx := newChoiceTestGateway(t)

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
		Name: "video-bar", DisplayName: "Video Bar", Quorum: 1, AlternateID: foreignAlt,
	})
	if !errors.Is(err, storage.ErrRoleRefNotFound) {
		t.Fatalf("role on std-b joining std-a's alternate = %v, want ErrRoleRefNotFound (422, not 500)", err)
	}

	// The matching owner is unaffected: the same alternate id under its own
	// standard succeeds.
	if _, err := gw.SetSystemRole(ctx, "", "standard", "foreign-std-a", storage.SystemRoleSpec{
		Name: "video-bar", DisplayName: "Video Bar", Quorum: 1, AlternateID: foreignAlt,
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
	gw, ctx := newChoiceTestGateway(t)

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
		Name: "video-bar", DisplayName: "Video Bar", Quorum: 1, AlternateID: alts["all-in-one"],
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
		Name: "video-bar", DisplayName: "Video Bar", Quorum: 1, AlternateID: "",
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
	gw, ctx := newChoiceTestGateway(t)
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
		Name: "role-a", DisplayName: "Role A", Quorum: 1, Impact: "outage", AlternateID: alts["a"],
	}); err != nil {
		t.Fatalf("declare role-a: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "role-b", DisplayName: "Role B", Quorum: 1, Impact: "outage", AlternateID: alts["b"],
	}); err != nil {
		t.Fatalf("declare role-b: %v", err)
	}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "detach-sys", StandardID: &std}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "detach-comp", ProductName: &bar}, all); err != nil {
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
		Name: "role-a", DisplayName: "Role A", Quorum: 1, Impact: "outage", AlternateID: "",
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
