package storage_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The acronym list on the storage path (#684).
//
// The dictionary only bites on the bottom rung of the ladder. A type's
// display_name is already correctly-cased English out of the catalog ("DSP",
// "Video Bar"), and a rule that renders one is right without any dictionary at
// all. What the list is for is the rung below that, where a rule title-cases a
// name the OPERATOR typed, which is why these tests drive locations: a location
// has no product and no catalog display name to fall back on.

// titleTheName is the rule these tests install at the global tier: the shape an
// operator reaches for when they want the machine name read as words.
const titleTheName = "{{.Name | title}}"

// A shipped acronym renders correctly cased with no operator configuration at
// all, which is the whole point of shipping a list rather than an empty one.
func TestAShippedAcronymRendersCorrectlyCased(t *testing.T) {
	gw, ctx := seededGateway(t)
	setLocationRule(t, gw, ctx, titleTheName)

	l := makeRoomIn(t, gw, ctx, "dsp-closet")
	if l.DisplayName != "DSP-Closet" {
		t.Fatalf("label = %q, want %q (the shipped dictionary did not reach the engine)", l.DisplayName, "DSP-Closet")
	}
}

// The headline acceptance: an operator adds an acronym and it takes effect
// WITHOUT a restart, including for a row written before the change. The row
// created first is proof the engine is not captured once per process; the row
// created second is proof the new dictionary is what later writes see.
func TestAnOperatorAddedAcronymTakesEffectWithoutARestart(t *testing.T) {
	gw, ctx := seededGateway(t)
	setLocationRule(t, gw, ctx, titleTheName)

	before := makeRoomIn(t, gw, ctx, "qm55-wall")
	if before.DisplayName != "Qm55-Wall" {
		t.Fatalf("label before the edit = %q, want %q", before.DisplayName, "Qm55-Wall")
	}

	addAcronyms(t, gw, ctx, "QM55")

	// The row that already existed: its next write re-renders it, on this same
	// process, with no restart in between.
	updated, err := gw.UpdateLocation(ctx, "", before.Name, storage.LocationPatch{}, all, all)
	if err != nil {
		t.Fatalf("update location: %v", err)
	}
	if updated.DisplayName != "QM55-Wall" {
		t.Fatalf("label after the edit = %q, want %q (the engine kept the dictionary it booted with)", updated.DisplayName, "QM55-Wall")
	}

	// And a row created after the edit.
	fresh := makeRoomIn(t, gw, ctx, "qm55-lobby")
	if fresh.DisplayName != "QM55-Lobby" {
		t.Fatalf("new row label = %q, want %q", fresh.DisplayName, "QM55-Lobby")
	}
}

// An operator's list REPLACES the shipped one rather than extending it, which is
// the settings engine's own rule for a list-valued setting and the only way a
// shipped entry can ever be removed. It is asserted here because it is the
// behavior an operator meets, not an implementation detail: the console edits
// the effective list, so the shipped entries are in the box being edited.
func TestAnOperatorListReplacesTheShippedOne(t *testing.T) {
	gw, ctx := seededGateway(t)
	setLocationRule(t, gw, ctx, titleTheName)

	addAcronyms(t, gw, ctx, "QM55")

	l := makeRoomIn(t, gw, ctx, "dsp-closet")
	if l.DisplayName != "Dsp-Closet" {
		t.Fatalf("label = %q, want %q: an operator's list is the list, so a shipped entry it omits stops applying", l.DisplayName, "Dsp-Closet")
	}
}

// A rule that does not title-case anything is unaffected by the list, so an
// operator editing the dictionary cannot reach a rule that never asked for it.
func TestARuleThatDoesNotTitleIsUnaffectedByTheList(t *testing.T) {
	gw, ctx := seededGateway(t)
	setLocationRule(t, gw, ctx, "{{.Name}}")

	addAcronyms(t, gw, ctx, "DSP")

	l := makeRoomIn(t, gw, ctx, "dsp-closet")
	if l.DisplayName != "dsp-closet" {
		t.Fatalf("label = %q, want the name untouched %q", l.DisplayName, "dsp-closet")
	}
}

// The operator settings FILE is a layer of the same cascade, so a dictionary set
// there has to reach the engine too. It is the layer easiest to wire halfway
// (the gateway would resolve the declared defaults and the database override
// and quietly skip the file), and the failure is silent: the settings read shows
// the operator's list while every label renders from another one.
func TestTheSettingsFileLayerReachesTheEngine(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t),
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))),
		storage.WithSettingsFile(settings.Doc{"label": {"acronyms": []any{"QM55"}}}))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	setLocationRule(t, gw, ctx, titleTheName)

	l := makeRoomIn(t, gw, ctx, "qm55-wall")
	if l.DisplayName != "QM55-Wall" {
		t.Fatalf("label = %q, want %q (the file layer did not reach the engine)", l.DisplayName, "QM55-Wall")
	}
}

// --- helpers -----------------------------------------------------------------

// setLocationRule installs a global label rule for locations, the tier these
// tests drive because a location's label has no catalog display name under it.
func setLocationRule(t *testing.T, gw *storage.PG, ctx context.Context, rule string) {
	t.Helper()
	if _, err := gw.SetLabelRule(ctx, "", "location", rule); err != nil {
		t.Fatalf("set location label rule: %v", err)
	}
}

// addAcronyms writes the operator's dictionary the way the API write path does,
// through the merge patch on the platform override.
func addAcronyms(t *testing.T, gw *storage.PG, ctx context.Context, acronyms ...string) {
	t.Helper()
	list := make([]any, len(acronyms))
	for i, a := range acronyms {
		list[i] = a
	}
	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "label",
		map[string]any{"acronyms": list}); err != nil {
		t.Fatalf("patch acronym list: %v", err)
	}
}

// makeRoomIn creates a room under the shared building and returns the row, so a
// test can read the label the write stamped.
func makeRoomIn(t *testing.T, gw *storage.PG, ctx context.Context, name string) *storage.Location {
	t.Helper()
	return mustGetLocation(t, gw, ctx, makeRoom(t, gw, ctx, name))
}

func mustGetLocation(t *testing.T, gw *storage.PG, ctx context.Context, name string) *storage.Location {
	t.Helper()
	l, err := gw.GetLocation(ctx, name, all)
	if err != nil {
		t.Fatalf("get location %q: %v", name, err)
	}
	return l
}
