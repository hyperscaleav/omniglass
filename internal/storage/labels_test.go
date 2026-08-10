package storage_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"bytes"

	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// The generated label (#682). Everything below asserts against display_name and
// its PEN together, because the pair is the unit: a value with no pen is an
// operator's, a value with the pen is the rule's, and the two disagreeing is
// the defect the invariant test at the bottom of this file exists to catch.
//
// The seeded global rule for a component is
// "{{.TypeName}}{{if .Ordinal}} {{.Ordinal}}{{end}}", and samsung-qm55 is
// classified under the "display" component_type whose display_name is
// "Display", so the first display in an empty bucket labels "Display 1".

const qm55 = "samsung-qm55"

// A component created with a product and a location gets a label without the
// operator typing one: the first acceptance behavior.
func TestCreatedComponentGetsALabelNobodyTyped(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoom(t, gw, ctx, "room-a")

	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.DisplayName != "Display 1" {
		t.Fatalf("label = %q, want %q", c.DisplayName, "Display 1")
	}
	if !c.DisplayNameGenerated {
		t.Fatal("the platform generated the label but does not hold the pen")
	}
	// The read path carries it, not just the write's returned row.
	got, err := gw.GetComponent(ctx, c.ID, all)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != "Display 1" || !got.DisplayNameGenerated {
		t.Fatalf("on read: label = %q generated = %v, want %q true", got.DisplayName, got.DisplayNameGenerated, "Display 1")
	}
	// The second display in the same bucket takes the next ordinal, and the
	// label follows it: proof the label reads the row's own facts rather than
	// being a constant per product.
	second, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if second.DisplayName != "Display 2" {
		t.Fatalf("second label = %q, want %q", second.DisplayName, "Display 2")
	}
}

// Setting a label by hand clears its pen; clearing the field returns it and the
// rule renders again. Both halves of the second acceptance behavior, in the
// order an operator performs them.
func TestSettingALabelTakesThePenAndClearingItGivesItBack(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoom(t, gw, ctx, "room-a")
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	typed, err := gw.UpdateComponent(ctx, "", c.ID, storage.ComponentPatch{DisplayName: strptr("Front of House")}, all, all)
	if err != nil {
		t.Fatalf("set label: %v", err)
	}
	if typed.DisplayName != "Front of House" {
		t.Fatalf("label after typing = %q, want %q", typed.DisplayName, "Front of House")
	}
	if typed.DisplayNameGenerated {
		t.Fatal("an operator typed the label and the platform still holds the pen")
	}

	// A rename would re-render a platform-owned label. It must leave this one
	// alone, because the operator holds the pen.
	renamed, err := gw.RenameComponent(ctx, "", typed.ID, "big-screen", all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.DisplayName != "Front of House" {
		t.Fatalf("label after rename = %q, want the operator's %q", renamed.DisplayName, "Front of House")
	}

	cleared, err := gw.UpdateComponent(ctx, "", renamed.ID, storage.ComponentPatch{DisplayName: strptr("")}, all, all)
	if err != nil {
		t.Fatalf("clear label: %v", err)
	}
	if !cleared.DisplayNameGenerated {
		t.Fatal("the operator cleared the label and the platform did not take the pen back")
	}
	// The rename above took the name's pen and nulled the ordinal, so the rule
	// renders without one: "Display", not "Display 1".
	if cleared.DisplayName != "Display" {
		t.Fatalf("label after clearing = %q, want the rule's %q", cleared.DisplayName, "Display")
	}
}

// A rule producing an empty string leaves the label UNSET, not blank. Asserted
// against the raw column, since the read projection coalesces NULL to "" and
// would report the two as identical.
//
// The pen stays with the platform. A row marked operator-owned because a rule
// happened to render nothing would never be reconsidered by the recompute that
// exists to fix exactly that, so "nothing to say today" must not read as
// "someone else's field".
func TestARuleRenderingNothingLeavesTheLabelUnsetRatherThanBlank(t *testing.T) {
	gw, ctx, dsn := seededGatewayDSN(t)

	// First the create case, where the row starts with no label and the rule
	// adds none.
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{LabelRule: strptr("{{.NoSuchFact}}")}); err != nil {
		t.Fatalf("set empty-producing rule: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55)}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !c.DisplayNameGenerated {
		t.Fatal("an empty render took the pen away from the platform")
	}
	if raw := rawLabel(t, ctx, dsn, c.ID); raw != nil {
		t.Fatalf("on create the stored label is %q, want SQL NULL rather than a blank string", *raw)
	}

	// Then the case that actually WRITES the empty value, which is the one a
	// create cannot reach: a row that already carries a label, whose rule then
	// stops producing one. Without this the assertion above passes because
	// nothing was written at all, which proves nothing about what an empty
	// render stores.
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{LabelRule: strptr("Panel {{.Ordinal}}")}); err != nil {
		t.Fatalf("set a producing rule: %v", err)
	}
	labelled, err := gw.ResetComponentName(ctx, "", c.ID, all, all)
	if err != nil {
		t.Fatalf("reset to pick up the producing rule: %v", err)
	}
	if labelled.DisplayName != "Panel 1" {
		t.Fatalf("precondition: label = %q, want %q", labelled.DisplayName, "Panel 1")
	}
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{LabelRule: strptr("{{.NoSuchFact}}")}); err != nil {
		t.Fatalf("back to the empty-producing rule: %v", err)
	}
	emptied, err := gw.RenameComponent(ctx, "", c.ID, "hand-picked", all, all)
	if err != nil {
		t.Fatalf("rename to re-render: %v", err)
	}
	if emptied.DisplayName != "" {
		t.Fatalf("label = %q, want none", emptied.DisplayName)
	}
	if !emptied.DisplayNameGenerated {
		t.Fatal("an empty re-render took the pen away from the platform")
	}
	if raw := rawLabel(t, ctx, dsn, c.ID); raw != nil {
		t.Fatalf("after an empty re-render the stored label is %q, want SQL NULL rather than a blank string", *raw)
	}
}

// An unparseable rule is refused at rule-EDIT time and cannot be stored. The
// second assertion is the one that matters: the refusal is not just a returned
// error, the column still holds what it held before.
func TestAnUnparseableRuleIsRefusedAndNeverStored(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{LabelRule: strptr("{{.TypeName | title}}")}); err != nil {
		t.Fatalf("set a good rule first: %v", err)
	}
	for _, bad := range []string{"{{.TypeName", "{{if .Ordinal}}{{.Ordinal}}", "{{.TypeName | exec}}"} {
		_, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{LabelRule: &bad})
		if !errors.Is(err, storage.ErrInvalidLabelRule) {
			t.Fatalf("UpdateComponentType(%q) = %v, want ErrInvalidLabelRule", bad, err)
		}
	}
	ct, err := gw.GetComponentType(ctx, "display")
	if err != nil {
		t.Fatalf("get component_type: %v", err)
	}
	if ct.LabelRule == nil || *ct.LabelRule != "{{.TypeName | title}}" {
		t.Fatalf("stored rule = %v, want the good rule to have survived every refusal", ct.LabelRule)
	}
}

// A rule that PARSES but fails while executing is only discoverable against one
// entity's facts, with a write already in flight. It degrades: the create
// succeeds, the label is left unset, and the read that follows is a row rather
// than a 500.
func TestARuleThatFailsToRenderDegradesRatherThanFailingTheWrite(t *testing.T) {
	gw, ctx := seededGateway(t)
	// Parses (a field access is valid syntax), fails at execution (a string has
	// no field), which is exactly the class edit-time validation cannot catch.
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{LabelRule: strptr("{{.TypeName.Nope}}")}); err != nil {
		t.Fatalf("store an executable-but-failing rule: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55)}, all)
	if err != nil {
		t.Fatalf("create with a failing rule: %v", err)
	}
	if c.DisplayName != "" {
		t.Fatalf("label = %q, want none", c.DisplayName)
	}
	if _, err := gw.GetComponent(ctx, c.ID, all); err != nil {
		t.Fatalf("read after a failing rule: %v", err)
	}
	if _, err := gw.ListComponents(ctx, all); err != nil {
		t.Fatalf("list after a failing rule: %v", err)
	}
}

// A more specific tier wins: product over component_type over global. Asserted
// by adding one tier at a time to the SAME row, so each step's expected value
// is only reachable if precedence is what changed.
func TestAMoreSpecificTierWins(t *testing.T) {
	gw, ctx := seededGateway(t)
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55)}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.DisplayName != "Display 1" {
		t.Fatalf("global tier label = %q, want %q", c.DisplayName, "Display 1")
	}

	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{LabelRule: strptr("type says {{.Stem}}")}); err != nil {
		t.Fatalf("set type rule: %v", err)
	}
	if got, err := gw.ExportRenderComponentLabel(ctx, c.ID); err != nil || got != "type says display" {
		t.Fatalf("with a type rule = %q (err %v), want %q", got, err, "type says display")
	}

	// A product an operator owns, since a shipped product cannot be edited
	// until the fork primitive reaches that registry.
	mine, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "my-panel", DisplayName: "My Panel", ComponentType: "display",
		LabelRule: strptr("product says {{.ProductName}}"),
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	reclassified, err := gw.UpdateComponent(ctx, "", c.ID, storage.ComponentPatch{ProductName: &mine.Name}, all, all)
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if reclassified.DisplayName != "product says My Panel" {
		t.Fatalf("with a product rule = %q, want %q", reclassified.DisplayName, "product says My Panel")
	}
}

// A rule set on a SHIPPED type forks the row rather than writing it (#655,
// ADR-0095): the operator's rule is what a component resolves, and the official
// row is byte-identical afterwards so a later release can still improve it.
func TestARuleOnAShippedTypeForksItRatherThanWritingIt(t *testing.T) {
	gw, ctx, dsn := seededGatewayDSN(t)
	forked, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{LabelRule: strptr("Panel {{.Ordinal}}")})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if !forked.Forked || !forked.Official {
		t.Fatalf("forked = %v official = %v, want both true", forked.Forked, forked.Official)
	}
	// The official row is untouched: the operator's rule lives in the shadow.
	var official *string
	if err := rawConn(t, dsn).QueryRow(ctx, `select label_rule from component_type where name = 'display'`).Scan(&official); err != nil {
		t.Fatalf("read the official row: %v", err)
	}
	if official != nil {
		t.Fatalf("the official component_type row carries label_rule %q, want it untouched", *official)
	}

	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55)}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.DisplayName != "Panel 1" {
		t.Fatalf("label = %q, want the forked rule's %q", c.DisplayName, "Panel 1")
	}

	// Restoring defaults discards the fork, and the rule goes with it.
	if _, err := gw.RestoreComponentType(ctx, "", "display"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got, err := gw.ExportRenderComponentLabel(ctx, c.ID); err != nil || got != "Display 1" {
		t.Fatalf("after restore = %q (err %v), want the global rule's %q", got, err, "Display 1")
	}
}

// A rule inherits down the component_type chain by the same first-non-null walk
// stem, icon and abbrev follow, and the walk crosses the fork boundary PER NODE
// (ADR-0095 decision 3). interactive-display sets nothing of its own and sits
// under display, so forking the PARENT's rule reaches it, and a rule of its own
// then overrides for itself alone.
func TestATypeRuleInheritsFromItsAncestorAndAChildOverrides(t *testing.T) {
	gw, ctx := seededGateway(t)
	// An operator's product classified under the child type, so the component
	// resolves through interactive-display -> display.
	child, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "my-touch-panel", DisplayName: "My Touch Panel", ComponentType: "interactive-display",
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &child.Name}, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}

	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{LabelRule: strptr("from the parent")}); err != nil {
		t.Fatalf("fork the parent's rule: %v", err)
	}
	if got, err := gw.ExportRenderComponentLabel(ctx, c.ID); err != nil || got != "from the parent" {
		t.Fatalf("inherited = %q (err %v), want %q", got, err, "from the parent")
	}

	if _, err := gw.UpdateComponentType(ctx, "", "interactive-display", storage.ComponentTypePatch{LabelRule: strptr("from the child")}); err != nil {
		t.Fatalf("fork the child's rule: %v", err)
	}
	if got, err := gw.ExportRenderComponentLabel(ctx, c.ID); err != nil || got != "from the child" {
		t.Fatalf("overridden = %q (err %v), want %q", got, err, "from the child")
	}

	// And the parent's own instances are unaffected by the child's override.
	sibling, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55)}, all)
	if err != nil {
		t.Fatalf("create sibling: %v", err)
	}
	if sibling.DisplayName != "from the parent" {
		t.Fatalf("sibling label = %q, want the parent's %q", sibling.DisplayName, "from the parent")
	}
}

// --- the sandbox --------------------------------------------------------

// The security argument for choosing a general template engine is that the
// sandbox is the DATA MAP. This is the test that tries to reach a secret, and
// it asserts the argument in both directions.
//
// First the closed key set, exactly, so widening what a rule can see is a
// deliberate act with a test to change rather than a field someone adds to a
// builder. Then a rule that names credentials by every word it might use, over
// a database that really holds one, rendering nothing.
func TestASecretIsNotReachableFromARule(t *testing.T) {
	gw, ctx := seededGateway(t)

	// A real secret, with a real value, in the same database the rule runs
	// against. The point is that the rule cannot see it, not that there is
	// nothing to see.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "device-admin", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": "hunter2-the-actual-password"},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55)}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	data, err := gw.ExportComponentLabelData(ctx, c.ID)
	if err != nil {
		t.Fatalf("label data: %v", err)
	}
	wantKeys := []string{"Name", "Ordinal", "ProductName", "Stem", "TypeAbbrev", "TypeName", "VendorName"}
	if got := sortedKeys(data); !equalStrings(got, wantKeys) {
		t.Fatalf("the component data map carries %v, want exactly %v. A key added here widens what every operator-authored rule can read; add it deliberately, with this list", got, wantKeys)
	}
	// Nothing in the map is a container or carries a pointer to another row:
	// the value type is a string, so there is nothing to traverse THROUGH.
	// Asserted structurally by the map's type, and behaviorally here.
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{
		LabelRule: strptr("[{{.Secret}}{{.Secrets}}{{.Password}}{{.Value}}{{.Token}}{{.Credential}}{{.APIKey}}]"),
	}); err != nil {
		t.Fatalf("store the prying rule: %v", err)
	}
	got, err := gw.ExportRenderComponentLabel(ctx, c.ID)
	if err != nil {
		t.Fatalf("render the prying rule: %v", err)
	}
	if got != "[]" {
		t.Fatalf("a rule asking for credentials rendered %q, want %q", got, "[]")
	}

	// And the traversal form, which is how a richer environment would have
	// leaked: it fails at execution and the caller degrades.
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{
		LabelRule: strptr("{{.ProductName.Secret}}"),
	}); err != nil {
		t.Fatalf("store the traversing rule: %v", err)
	}
	if got, err := gw.ExportRenderComponentLabel(ctx, c.ID); err != nil || got != "" {
		t.Fatalf("a traversing rule rendered %q (err %v), want nothing", got, err)
	}
}

func TestTheSystemAndLocationDataMapsAreClosedToo(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoom(t, gw, ctx, "room-a")
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-a", LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	sysData, err := gw.ExportSystemLabelData(ctx, sys.ID)
	if err != nil {
		t.Fatalf("system label data: %v", err)
	}
	wantSys := []string{"Name", "StandardName", "Stem", "TypeAbbrev", "TypeName"}
	if got := sortedKeys(sysData); !equalStrings(got, wantSys) {
		t.Fatalf("the system data map carries %v, want exactly %v", got, wantSys)
	}

	loc, err := gw.GetLocation(ctx, room, all)
	if err != nil {
		t.Fatalf("get location: %v", err)
	}
	locData, err := gw.ExportLocationLabelData(ctx, loc.ID)
	if err != nil {
		t.Fatalf("location label data: %v", err)
	}
	wantLoc := []string{"Name", "TypeName"}
	if got := sortedKeys(locData); !equalStrings(got, wantLoc) {
		t.Fatalf("the location data map carries %v, want exactly %v", got, wantLoc)
	}
}

// --- the write paths ----------------------------------------------------

// The five acts that can change a component rule's inputs, each proved to
// re-render. This is the enumeration the data map's contents commit us to: no
// fact in that map changes by any other route, which is what makes the
// invariant below provable rather than hopeful.
func TestALabelFollowsEveryActThatChangesItsInputs(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{
		LabelRule: strptr("{{.TypeName}}/{{.Name}}/{{if .Ordinal}}{{.Ordinal}}{{else}}-{{end}}"),
	}); err != nil {
		t.Fatalf("set a rule that reads every changing fact: %v", err)
	}
	roomA := makeRoom(t, gw, ctx, "room-a")
	roomB := makeRoom(t, gw, ctx, "room-b")

	// create
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &roomA}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.DisplayName != "Display/display-1/1" {
		t.Fatalf("on create = %q, want %q", c.DisplayName, "Display/display-1/1")
	}

	// rename: takes the name's pen and nulls the ordinal, so both halves move
	renamed, err := gw.RenameComponent(ctx, "", c.ID, "front-panel", all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.DisplayName != "Display/front-panel/-" {
		t.Fatalf("after rename = %q, want %q", renamed.DisplayName, "Display/front-panel/-")
	}

	// reset: a fresh generated name and a fresh ordinal
	reset, err := gw.ResetComponentName(ctx, "", renamed.ID, all, all)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.DisplayName != "Display/display-1/1" {
		t.Fatalf("after reset = %q, want %q", reset.DisplayName, "Display/display-1/1")
	}

	// move: a new placement bucket, so a re-minted ordinal
	blocker, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &roomB}, all)
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	_ = blocker
	moved, err := gw.MoveComponent(ctx, "", reset.ID, storage.ComponentMove{LocationName: &roomB}, all, all)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.DisplayName != "Display/display-2/2" {
		t.Fatalf("after move = %q, want %q", moved.DisplayName, "Display/display-2/2")
	}

	// reclassify: a different product under a different type
	other, err := gw.CreateProduct(ctx, "", storage.Product{Name: "my-mic", DisplayName: "My Mic", ComponentType: "mic"})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	reclassified, err := gw.UpdateComponent(ctx, "", moved.ID, storage.ComponentPatch{ProductName: &other.Name}, all, all)
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	// The mic type declares no rule of its own and inherits none, so the
	// global rule applies again.
	if reclassified.DisplayName != "Microphone 1" {
		t.Fatalf("after reclassify = %q, want the global rule's %q", reclassified.DisplayName, "Microphone 1")
	}
}

func TestASystemAndALocationGetLabelsToo(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoom(t, gw, ctx, "room-a")

	// A location ships NO global rule (a nominal location's real-world name is
	// a fact no rule has), so it starts unlabelled and the read ladder falls
	// through to its name. A rule on its TYPE is how an operator opts in.
	loc, err := gw.GetLocation(ctx, room, all)
	if err != nil {
		t.Fatalf("get location: %v", err)
	}
	if loc.DisplayName != "" {
		t.Fatalf("location label = %q, want none shipped", loc.DisplayName)
	}
	lt, err := gw.CreateLocationType(ctx, "", storage.LocationType{
		Name: "pod", DisplayName: "Pod", LabelRule: strptr("{{.TypeName}} {{.Name | upper}}"),
	})
	if err != nil {
		t.Fatalf("create location_type: %v", err)
	}
	pod, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "pod-7", LocationType: lt.Name}, all)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if pod.DisplayName != "Pod POD-7" || !pod.DisplayNameGenerated {
		t.Fatalf("location label = %q generated = %v, want %q true", pod.DisplayName, pod.DisplayNameGenerated, "Pod POD-7")
	}

	// A system's shipped rule is its type's display_name, so an unclassified
	// system renders nothing and a classified one renders its kind of space.
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-a", LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	if sys.DisplayName != "" {
		t.Fatalf("unclassified system label = %q, want none", sys.DisplayName)
	}
	classified, err := gw.UpdateSystem(ctx, "", sys.ID, storage.SystemPatch{SystemTypeID: strptr("board")}, all, all)
	if err != nil {
		t.Fatalf("classify system: %v", err)
	}
	if classified.DisplayName == "" || !classified.DisplayNameGenerated {
		t.Fatalf("classified system label = %q generated = %v, want the type's display name", classified.DisplayName, classified.DisplayNameGenerated)
	}
}

// --- the global tier ----------------------------------------------------

// The global rule is a TABLE with two columns rather than one, and this is what
// the second column buys. A boot seed is authoritative (it must be, or a
// release could never improve a shipped rule), so writing an operator's rule to
// the same column would stomp it on the next restart, and a seed-if-absent
// would freeze the shipped default at whatever the first boot wrote. Both
// columns, resolved one over the other, is the only arrangement where re-seeding
// and operator ownership are both true.
func TestTheGlobalRuleIsSeededAuthoritativelyAndStillOperatorOwned(t *testing.T) {
	gw, ctx := seededGateway(t)

	shipped, err := gw.GetLabelRule(ctx, "component")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if shipped.Template != nil {
		t.Fatalf("a freshly seeded rule carries an operator template %q, want none", *shipped.Template)
	}
	if shipped.Effective != shipped.Default || shipped.Default == "" {
		t.Fatalf("effective = %q default = %q, want the shipped default to be in force", shipped.Effective, shipped.Default)
	}

	mine, err := gw.SetLabelRule(ctx, "", "component", "mine: {{.TypeName}}")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if mine.Effective != "mine: {{.TypeName}}" {
		t.Fatalf("effective = %q, want the operator's rule", mine.Effective)
	}

	// The boot seed runs on every start. It must restate the shipped default and
	// leave the operator's rule exactly where it is.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	after, err := gw.GetLabelRule(ctx, "component")
	if err != nil {
		t.Fatalf("get after re-seed: %v", err)
	}
	if after.Template == nil || *after.Template != "mine: {{.TypeName}}" {
		t.Fatalf("after re-seed the operator's rule is %v, want it untouched", after.Template)
	}
	if after.Default != shipped.Default {
		t.Fatalf("after re-seed the shipped default is %q, want %q restated", after.Default, shipped.Default)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55)}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.DisplayName != "mine: Display" {
		t.Fatalf("label = %q, want the operator's rule to be what renders", c.DisplayName)
	}

	// Clearing the override is restore-to-defaults; no verb of its own, because
	// unlike a name a rule HAS a representable unset state.
	restored, err := gw.SetLabelRule(ctx, "", "component", "")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if restored.Template != nil || restored.Effective != shipped.Default {
		t.Fatalf("after clearing: template %v effective %q, want none and the shipped default", restored.Template, restored.Effective)
	}

	// And an unparseable global rule is refused before it is stored, exactly as
	// a tier rule is.
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.TypeName"); !errors.Is(err, storage.ErrInvalidLabelRule) {
		t.Fatalf("SetLabelRule with a broken template = %v, want ErrInvalidLabelRule", err)
	}
}

// --- the invariant ------------------------------------------------------

// The recompute-and-compare invariant derived data owes in this repo: every
// stored label the platform owns must equal what its rule produces RIGHT NOW.
// A stale value cannot pass unnoticed, which is the whole price of storing the
// label instead of rendering it per read.
//
// The estate below is built to make the comparison capable of failing: three
// tiers of rule in play at once, rows in two placement buckets, rows that have
// been renamed, moved, reclassified and reset, and rows whose label the operator
// owns (which the invariant must skip, not silently rewrite).
func TestEveryStoredLabelEqualsWhatItsRuleProduces(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{
		LabelRule: strptr("{{.TypeName | upper}} {{.Name}}{{if .Ordinal}} #{{.Ordinal}}{{end}}"),
	}); err != nil {
		t.Fatalf("type rule: %v", err)
	}
	mine, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "my-bar", DisplayName: "My Bar", ComponentType: "video-bar",
		LabelRule: strptr("{{.VendorName}} {{.ProductName | slug}} {{.Ordinal}}"),
	})
	if err != nil {
		t.Fatalf("product rule: %v", err)
	}
	roomA := makeRoom(t, gw, ctx, "room-a")
	roomB := makeRoom(t, gw, ctx, "room-b")

	// The bucket contents are chosen so the move below actually SHIFTS an
	// ordinal: room-b already holds display-1 and display-2, so the display-2
	// arriving from room-a is re-minted as display-3. Without that the mover
	// keeps its number, its label does not change, and the invariant would pass
	// whether or not a move re-renders at all.
	var built []string
	for _, spec := range []storage.ComponentSpec{
		{ProductName: strptr(qm55), LocationName: &roomA},           // 0 renamed
		{ProductName: strptr(qm55), LocationName: &roomA},           // 1 moved
		{ProductName: strptr(qm55), LocationName: &roomB},           // 2 reclassified
		{ProductName: strptr(qm55), LocationName: &roomB},           // 3 the blocker
		{ProductName: &mine.Name, LocationName: &roomA},             // 4 reset
		{ProductName: &mine.Name, LocationName: &roomB},             // 5 hand-labelled
		{ProductName: strptr("shure-mxa920"), LocationName: &roomA}, // 6 renamed then reset
		{Name: "hand-named", ProductName: strptr(qm55), LocationName: &roomB},
	} {
		c, err := gw.CreateComponent(ctx, "", spec, all)
		if err != nil {
			t.Fatalf("create %+v: %v", spec, err)
		}
		built = append(built, c.ID)
	}
	// Put every write path through its paces on real rows, so the invariant is
	// checked against an estate that has been edited rather than only created.
	if _, err := gw.RenameComponent(ctx, "", built[0], "kept-by-hand", all, all); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := gw.MoveComponent(ctx, "", built[1], storage.ComponentMove{LocationName: &roomB}, all, all); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := gw.UpdateComponent(ctx, "", built[2], storage.ComponentPatch{ProductName: &mine.Name}, all, all); err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if _, err := gw.ResetComponentName(ctx, "", built[4], all, all); err != nil {
		t.Fatalf("reset: %v", err)
	}
	// Renamed and then reset, so the reset genuinely moves the name and the
	// ordinal back: a reset onto a row that would re-mint what it already holds
	// changes nothing, and would leave this act unchecked.
	if _, err := gw.RenameComponent(ctx, "", built[6], "hand-mic", all, all); err != nil {
		t.Fatalf("rename before reset: %v", err)
	}
	if _, err := gw.ResetComponentName(ctx, "", built[6], all, all); err != nil {
		t.Fatalf("reset after rename: %v", err)
	}
	if _, err := gw.UpdateComponent(ctx, "", built[5], storage.ComponentPatch{DisplayName: strptr("The Operator's Own")}, all, all); err != nil {
		t.Fatalf("hand-label: %v", err)
	}
	// The mover's label must have MOVED with it, or the fixture is not exercising
	// what the invariant below is meant to catch.
	moved, err := gw.GetComponent(ctx, built[1], all)
	if err != nil {
		t.Fatalf("re-read the mover: %v", err)
	}
	if moved.Name != "display-3" {
		t.Fatalf("the mover is named %q, want display-3: the fixture must force a re-mint or the move case proves nothing", moved.Name)
	}

	components, err := gw.ListComponents(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(components) < 8 {
		t.Fatalf("built %d components, want the whole fixture: an invariant over too few rows proves too little", len(components))
	}
	checked := 0
	for i := range components {
		c := components[i]
		if !c.DisplayNameGenerated {
			// The operator owns this one. It must NOT match a rule by
			// accident, or the invariant below would be vacuous for it.
			if c.DisplayName != "The Operator's Own" {
				t.Errorf("component %s: pen with the operator but label %q, want the one they typed", c.Name, c.DisplayName)
			}
			continue
		}
		want, err := gw.ExportRenderComponentLabel(ctx, c.ID)
		if err != nil {
			t.Fatalf("recompute %s: %v", c.Name, err)
		}
		if c.DisplayName != want {
			t.Errorf("component %s: stored label %q, rule now produces %q", c.Name, c.DisplayName, want)
		}
		checked++
	}
	if checked < 7 {
		t.Fatalf("only %d platform-owned labels were compared, want at least 6: an invariant that checks nothing passes for the wrong reason", checked)
	}
}

// --- helpers ------------------------------------------------------------

// seededGateway opens a Gateway over a fresh database with the boot seed
// applied and a deterministic KEK provider wired, the same shape secretGateway
// uses. The provider is not incidental: the sandbox test creates a REAL secret,
// because "a rule cannot reach a secret" is only worth asserting against a
// database that has one.
func seededGateway(t *testing.T) (*storage.PG, context.Context) {
	gw, ctx, _ := seededGatewayDSN(t)
	return gw, ctx
}

// The concrete *storage.PG rather than the Gateway interface, because the
// recompute and data-map reads these tests need are package-internal test
// shims (labels_export_test.go), never gateway methods: nothing in production
// asks "what WOULD this row's label be".
func seededGatewayDSN(t *testing.T) (*storage.PG, context.Context, string) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw, ctx, dsn
}

// makeRoom creates a room under a shared building (the seeded placement rules
// refuse a room at the root) and returns its name, the placement bucket most of
// these tests need two of.
func makeRoom(t *testing.T, gw *storage.PG, ctx context.Context, name string) string {
	t.Helper()
	if _, err := gw.GetLocation(ctx, "hq", all); err != nil {
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "building"}, all); err != nil {
			t.Fatalf("create building: %v", err)
		}
	}
	hq := "hq"
	l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: name, LocationType: "room", ParentName: &hq}, all)
	if err != nil {
		t.Fatalf("create location %s: %v", name, err)
	}
	return l.Name
}

// rawLabel reads display_name as the column actually holds it, since every read
// projection coalesces NULL to "" and would report unset and blank as the same
// answer, which is the distinction one of these tests exists to make.
func rawLabel(t *testing.T, ctx context.Context, dsn, id string) *string {
	t.Helper()
	var label *string
	if err := rawConn(t, dsn).QueryRow(ctx, `select display_name from component where id = $1`, id).Scan(&label); err != nil {
		t.Fatalf("read raw label: %v", err)
	}
	return label
}

// rawConn opens a second connection to the given database, for the assertions
// that have to see a column rather than a projection of it.
func rawConn(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func sortedKeys(d map[string]string) []string {
	out := make([]string, 0, len(d))
	for k := range d {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
