package devseed_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// plantedSecret is the name the dev fleet plants at all three tiers of the
// secret cascade (platform -> location -> component; secrets have no system
// band), so the effective-secrets view comes up with a winner and shadowed
// candidates to read (#168).
const plantedSecret = "sim-panel-login"

// effectiveCopies resolves a component's effective secrets and returns the
// copies of the planted name, winner first (the resolver orders them so).
func effectiveCopies(t *testing.T, ctx context.Context, gw storage.Gateway, componentID string) []storage.ResolvedSecret {
	t.Helper()
	all, err := gw.ResolveSecrets(ctx, componentID, scope.Set{All: true}, true)
	if err != nil {
		t.Fatalf("resolve secrets: %v", err)
	}
	var out []storage.ResolvedSecret
	for _, r := range all {
		if r.Name == plantedSecret {
			out = append(out, r)
		}
	}
	return out
}

// wantCascade asserts the copies are exactly the given owner kinds, in
// winner-first order, with only the first marked the winner.
func wantCascade(t *testing.T, what string, got []storage.ResolvedSecret, kinds ...string) {
	t.Helper()
	if len(got) != len(kinds) {
		t.Fatalf("%s: %d copies of %s, want %d (%v)", what, len(got), plantedSecret, len(kinds), got)
	}
	for i, k := range kinds {
		if got[i].OwnerKind != k {
			t.Errorf("%s: copy %d is owned at %q, want %q", what, i, got[i].OwnerKind, k)
		}
		if got[i].Winner != (i == 0) {
			t.Errorf("%s: copy %d (%s) winner = %v, want %v", what, i, got[i].OwnerKind, got[i].Winner, i == 0)
		}
	}
}

// fieldValue is a resolved field's display value, "" when absent.
func fieldValue(r storage.ResolvedSecret, name string) string {
	for _, f := range r.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}

// TestDevFleetPlantsASecretCascade: the dev scenario seeds one secret name at
// the platform, on a room, and on one component in it, so the component sees
// its own copy win over the room's and the platform's, a sibling in the room
// sees the room's copy win over the platform's, and anywhere else the platform
// copy stands alone.
func TestDevFleetPlantsASecretCascade(t *testing.T) {
	t.Parallel()
	ctx, conn, gw := seededFleet(t)

	// panel-1 (touch-a) is the component carrying its own copy, a sibling in the room
	// (the video bar) takes the room's, and the huddle (whose panel console
	// is what uses this secret) takes the platform's alone.
	override := componentIn(t, ctx, conn, gw, "boardroom-a", "panel-1")
	sibling := componentIn(t, ctx, conn, gw, "boardroom-a", "videobar-1")
	elsewhere := componentIn(t, ctx, conn, gw, "huddle", "display-1")

	got := effectiveCopies(t, ctx, gw, override.ID)
	wantCascade(t, "boardroom-a touch panel", got, "component", "location", "platform")
	if got[0].OwnerName != override.Name {
		t.Errorf("component copy owned by %q, want %q", got[0].OwnerName, override.Name)
	}
	if got[1].OwnerName != "boardroom-a" {
		t.Errorf("location copy owned by %q, want boardroom-a", got[1].OwnerName)
	}
	// The winner is visible by its field values: the component copy's login
	// is not the room's.
	if a, b := fieldValue(got[0], "username"), fieldValue(got[1], "username"); a == "" || a == b {
		t.Errorf("component copy username %q is not distinguishable from the room's %q", a, b)
	}

	wantCascade(t, "boardroom-a video bar", effectiveCopies(t, ctx, gw, sibling.ID), "location", "platform")
	wantCascade(t, "huddle display-1", effectiveCopies(t, ctx, gw, elsewhere.ID), "platform")
}

// TestPlantedSecretsSeedOnce: a second dev-seed run finds each copy at each
// tier already there (ErrSecretExists is a no-op) and creates no secret.
func TestPlantedSecretsSeedOnce(t *testing.T) {
	t.Parallel()
	ctx, conn, gw := seededFleet(t)
	count := func() (n int) {
		if err := conn.QueryRow(ctx, `select count(*) from secret`).Scan(&n); err != nil {
			t.Fatalf("count secrets: %v", err)
		}
		return n
	}
	before := count()
	if err := devseed.Run(ctx, gw, ""); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if after := count(); after != before {
		t.Errorf("secrets: %d after the second run, %d before", after, before)
	}
}
