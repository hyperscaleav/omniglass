package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The four tables that gained a label last (#613): tag, variable, secret and
// interface. What is proved here is what a rename could not have carried across,
// because none of it is a property of the column's name:
//
//   - the ORDER a list comes back in, which is a property of the VALUE. Postgres
//     sorts NULL last under a default ASC and the empty string FIRST, and #756
//     found seven registries already shipping their unlabelled rows at the top
//     of the picker because of exactly that. A new list is guilty until a test
//     says otherwise.
//   - the INDEPENDENCE of the label from the address, which is the whole point
//     of the pair: ADR-0076 froze these names on the entity name rule and this
//     epic is the pressure valve it was missing, so a label that disturbed the
//     name would have re-created the problem it exists to solve.
//   - the VERBATIM fallback: an unset label reads back as empty, and every
//     surface then renders the name exactly, never a re-cased or prettified
//     version of it.
//
// The unset spelling itself (NULL, never "") is label_unset_test.go's sweep,
// which the four join as provers there rather than being asserted again here.

// The three rows every ordering case creates. The names are chosen so that
// ordering by NAME, or by an unset label that sorts first, puts the unlabelled
// row at the top; ordering by the label with unset last puts it at the bottom.
const (
	fourUnlabelled = "aaa-unlabelled"
	fourLabelledZ  = "mmm-labelled-z"
	fourLabelledA  = "zzz-labelled-a"
)

// fourWantOrder is "Alpha", then "Zulu", then the unlabelled row.
var fourWantOrder = []string{fourLabelledA, fourLabelledZ, fourUnlabelled}

func assertLabelOrder(t *testing.T, got []string) {
	t.Helper()
	// The seeded fleet carries rows of its own, so the assertion is on the
	// relative order of the three this case made, not on the whole list.
	var seen []string
	for _, name := range got {
		for _, want := range fourWantOrder {
			if name == want {
				seen = append(seen, name)
			}
		}
	}
	if len(seen) != len(fourWantOrder) {
		t.Fatalf("found %v, want all of %v", seen, fourWantOrder)
	}
	for i := range seen {
		if seen[i] != fourWantOrder[i] {
			t.Fatalf("order = %v, want %v.\n"+
				"An unlabelled row sorts LAST: `order by label nulls last, name`, which holds only "+
				"while unset really is SQL NULL (#613, ADR-0118).", seen, fourWantOrder)
		}
	}
}

// TestAnUnlabelledTagSortsLast is D4 on the one name-addressed table of the
// four: the list is ordered the way it is READ, by the label, with the rows that
// have none at the end rather than floated to the top.
func TestAnUnlabelledTagSortsLast(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()

	for _, r := range []struct{ name, label string }{
		{fourUnlabelled, ""},
		{fourLabelledZ, "Zulu"},
		{fourLabelledA, "Alpha"},
	} {
		if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: r.name, Label: r.label}, all); err != nil {
			t.Fatalf("create tag %q: %v", r.name, err)
		}
	}
	tags, err := gw.ListTags(ctx)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	got := make([]string, 0, len(tags))
	for _, tg := range tags {
		got = append(got, tg.Name)
	}
	assertLabelOrder(t, got)
}

// TestATagsLabelAndItsNameDoNotDisturbEachOther is the pair contract on the only
// name-ADDRESSED table of the four (`/tags/{name}`), where a label that leaked
// into the name would move the row's address out from under every binding that
// references it.
//
// A tag's name is fixed at creation: `UpdateTag` replaces the governance fields
// and the name is not among them, and there is no `:rename` custom method the
// way component, system, location and principal_group have one. So the rename
// half of the contract is proved the only way this table permits, by driving the
// update that DOES exist and asserting the name it lands on is the one it
// started with. The label half is the same statement from the other side: an
// update that says nothing about the label leaves it exactly as it was, which is
// what makes the two independent rather than merely adjacent.
func TestATagsLabelAndItsNameDoNotDisturbEachOther(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()

	made, err := gw.CreateTag(ctx, "", storage.TagSpec{
		Name: "cost-center", Label: "Cost Center", AppliesTo: []string{"component"},
	}, all)
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if made.Label != "Cost Center" || made.Name != "cost-center" {
		t.Fatalf("created (%q, %q), want (cost-center, Cost Center)", made.Name, made.Label)
	}

	// A governance update that says nothing about the label leaves it alone. A
	// nil pointer is "leave", which is a different instruction from "clear" and
	// has to stay different, or every applies_to edit silently wipes the label.
	kept, err := gw.UpdateTag(ctx, "", "cost-center", storage.TagPatch{
		AppliesTo: []string{"component", "system"}, Propagates: true,
	}, all)
	if err != nil {
		t.Fatalf("update governance: %v", err)
	}
	if kept.Label != "Cost Center" {
		t.Errorf("label = %q after a governance update that did not mention it, want it untouched", kept.Label)
	}
	if kept.Name != "cost-center" || kept.ID != made.ID {
		t.Errorf("(name, id) = (%q, %q), want (cost-center, %s): the address is not the label's to move",
			kept.Name, kept.ID, made.ID)
	}

	// And the other direction: relabelling moves nothing else. The label takes
	// the spaces and capitals the name rule refuses, which is the point of it.
	relabelled := "Cost Centre (EMEA)"
	moved, err := gw.UpdateTag(ctx, "", "cost-center", storage.TagPatch{
		AppliesTo: []string{"component", "system"}, Propagates: true, Label: &relabelled,
	}, all)
	if err != nil {
		t.Fatalf("relabel: %v", err)
	}
	if moved.Label != relabelled {
		t.Errorf("label = %q, want %q", moved.Label, relabelled)
	}
	if moved.Name != "cost-center" || moved.ID != made.ID {
		t.Errorf("(name, id) = (%q, %q) after a relabel, want (cost-center, %s)", moved.Name, moved.ID, made.ID)
	}
	// The row is still where it was addressed: a tag is reached by name.
	if _, err := gw.ListTags(ctx); err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if _, err := gw.UpdateTag(ctx, "", "cost-center", storage.TagPatch{Propagates: true}, all); err != nil {
		t.Errorf("addressing %q after a relabel: %v", "cost-center", err)
	}
}

// TestAnUnlabelledTagReadsBackVerbatim pins the fallback contract at the layer
// that can actually be wrong about it: the gateway hands back an EMPTY label for
// a row that has none, never the name and never a prettified version of it. The
// re-casing, if any, is the console's single renderer (entityLabel), and it
// cannot fall back to something the gateway has already substituted.
func TestAnUnlabelledTagReadsBackVerbatim(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()

	made, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "cost-center"}, all)
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if made.Label != "" {
		t.Errorf("label = %q on a tag created without one, want empty: nothing here guesses a label "+
			"from the name, and a surface with none renders `cost-center` exactly", made.Label)
	}
	tags, err := gw.ListTags(ctx)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	for _, tg := range tags {
		if tg.Name == "cost-center" && tg.Label != "" {
			t.Errorf("listed label = %q, want empty", tg.Label)
		}
	}
}

// TestAnUnlabelledVariableSortsLast is D4 on the simplest of the four. Same
// ordering, same reason, and pinned separately because ListVariables is a
// different statement that could drift on its own.
func TestAnUnlabelledVariableSortsLast(t *testing.T) {
	gw := variableGateway(t)
	ctx := context.Background()

	for _, r := range []struct{ name, label string }{
		{fourUnlabelled, ""},
		{fourLabelledZ, "Zulu"},
		{fourLabelledA, "Alpha"},
	} {
		if _, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
			Name: r.name, Label: r.label, ValueType: "int", OwnerKind: "platform",
			Value: json.RawMessage(`1`),
		}, all); err != nil {
			t.Fatalf("create variable %q: %v", r.name, err)
		}
	}
	vars, err := gw.ListVariables(ctx, all)
	if err != nil {
		t.Fatalf("list variables: %v", err)
	}
	got := make([]string, 0, len(vars))
	for _, v := range vars {
		got = append(got, v.Name)
	}
	assertLabelOrder(t, got)
}

// TestAVariablesLabelIsIndependentOfItsValue is the uuid-addressed case, where
// the interesting collision is not with the address (a uuid cannot be disturbed)
// but with the OTHER thing this table's PATCH carries. A variable's update path
// took one argument, the value, and now takes two instructions that must not
// contaminate each other: relabelling must not rewrite the value, and setting a
// value must not clear the label.
func TestAVariablesLabelIsIndependentOfItsValue(t *testing.T) {
	gw := variableGateway(t)
	ctx := context.Background()

	made, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "poll-interval", Label: "Poll Interval", ValueType: "int", OwnerKind: "platform",
		Value: json.RawMessage(`30`),
	}, all)
	if err != nil {
		t.Fatalf("create variable: %v", err)
	}
	if made.Label != "Poll Interval" {
		t.Fatalf("label = %q, want Poll Interval", made.Label)
	}

	// A value edit that says nothing about the label leaves it alone.
	valued, err := gw.UpdateVariable(ctx, "", made.ID, storage.VariablePatch{
		Value: json.RawMessage(`60`),
	}, all, all, true)
	if err != nil {
		t.Fatalf("update value: %v", err)
	}
	if valued.Label != "Poll Interval" || string(valued.Value) != "60" {
		t.Errorf("after a value edit: (label, value) = (%q, %s), want (Poll Interval, 60)",
			valued.Label, valued.Value)
	}

	// And a relabel leaves the value, the name, the type and the id alone.
	relabelled := "Polling interval (seconds)"
	moved, err := gw.UpdateVariable(ctx, "", made.ID, storage.VariablePatch{Label: &relabelled}, all, all, true)
	if err != nil {
		t.Fatalf("relabel: %v", err)
	}
	if moved.Label != relabelled || string(moved.Value) != "60" {
		t.Errorf("after a relabel: (label, value) = (%q, %s), want (%q, 60)", moved.Label, moved.Value, relabelled)
	}
	if moved.Name != "poll-interval" || moved.ValueType != "int" || moved.ID != made.ID {
		t.Errorf("relabel moved something else: %+v", moved)
	}
}

// TestAnUnlabelledSecretSortsLast is D4 on the secret directory, which is the
// one of the four whose list statement is built twice (an all-scope arm and a
// scope-predicate arm) and could therefore order two different ways.
func TestAnUnlabelledSecretSortsLast(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()

	for _, r := range []struct{ name, label string }{
		{fourUnlabelled, ""},
		{fourLabelledZ, "Zulu"},
		{fourLabelledA, "Alpha"},
	} {
		if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
			Name: r.name, Label: r.label, SecretType: "snmp-community", OwnerKind: "platform",
			Fields: map[string]string{"community": "public"},
		}, all, true); err != nil {
			t.Fatalf("create secret %q: %v", r.name, err)
		}
	}
	secrets, err := gw.ListSecrets(ctx, all, true)
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	got := make([]string, 0, len(secrets))
	for _, s := range secrets {
		got = append(got, s.Name)
	}
	assertLabelOrder(t, got)
}

// TestASecretsLabelReachesEveryProjectionThatCarriesItsName is the sweep the
// brief asked for by name. A secret is read through more shapes than any other
// of the four, and a label that appears in the directory but not in the cascade
// is exactly the gap this epic exists to close: the operator relabels a secret,
// sees the new words in one list, and the effective-secrets panel on the
// component still shows the kebab name.
//
// Every projection that carries the secret's NAME carries its label, and the two
// that carry neither are named here rather than left to inference: the reveal
// and the copy return the decrypted FIELD MAP and nothing else, no id, no name,
// no type, so there is no identity in them for a label to be missing from.
func TestASecretsLabelReachesEveryProjectionThatCarriesItsName(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()

	// The placement rules want a campus over a building over a room.
	for _, l := range []storage.LocationSpec{
		{Name: "campus", LocationType: "campus"},
		{Name: "bldg", LocationType: "building", ParentName: strptr("campus")},
		{Name: "room", LocationType: "room", ParentName: strptr("bldg")},
	} {
		if _, err := gw.CreateLocation(ctx, "", l, all); err != nil {
			t.Fatalf("create location %q: %v", l.Name, err)
		}
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "codec-1", LocationName: strptr("room"),
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}

	const label = "Polling community"
	made, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "poll-community", Label: label, SecretType: "snmp-community", OwnerKind: "location",
		OwnerName: strptr("room"), Fields: map[string]string{"community": "public"},
	}, all, true)
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}
	if made.Label != label {
		t.Errorf("create returned label %q, want %q", made.Label, label)
	}

	// The directory.
	listed, err := gw.ListSecrets(ctx, all, true)
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	found := false
	for _, s := range listed {
		if s.Name == "poll-community" {
			found = true
			if s.Label != label {
				t.Errorf("directory label = %q, want %q", s.Label, label)
			}
		}
	}
	if !found {
		t.Fatal("the secret is missing from the directory")
	}

	// The per-component cascade, which is the projection an operator actually
	// reads on the component page.
	resolved, err := gw.ResolveSecrets(ctx, comp.ID, all, true)
	if err != nil {
		t.Fatalf("resolve secrets: %v", err)
	}
	if len(resolved) == 0 {
		t.Fatal("the cascade resolved nothing")
	}
	for _, r := range resolved {
		if r.Name == "poll-community" && r.Label != label {
			t.Errorf("cascade label = %q, want %q.\n"+
				"A label in the directory and not in the cascade is the gap this epic exists to close.",
				r.Label, label)
		}
	}

	// The update, which returns the same shape the directory does.
	updated, err := gw.UpdateSecret(ctx, "", made.ID, storage.SecretPatch{
		Fields: map[string]string{"community": "private"},
	}, all, all, true, true)
	if err != nil {
		t.Fatalf("update secret: %v", err)
	}
	if updated.Label != label {
		t.Errorf("label = %q after a FIELD update that did not mention it, want it untouched", updated.Label)
	}

	// The reveal and the copy carry no identity at all, so there is nothing for
	// the label to be absent from. Pinned so a later change that adds identity to
	// either one has to decide about the label deliberately.
	fields, err := gw.RevealSecret(ctx, "", made.ID, all, all, true)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if _, ok := fields["label"]; ok {
		t.Error("the reveal returned a `label` key: it returns the decrypted field map and nothing else, " +
			"so a label here would be indistinguishable from a field called label")
	}
	if fields["community"] != "private" {
		t.Errorf("revealed community = %q, want private", fields["community"])
	}

	// And a relabel moves nothing else, including the sealed value: the fields
	// are bound to (owner, name, field) as AAD, so a write that touched the name
	// would make them undecryptable.
	relabelled := "SNMP community (read-only)"
	moved, err := gw.UpdateSecret(ctx, "", made.ID, storage.SecretPatch{Label: &relabelled}, all, all, true, true)
	if err != nil {
		t.Fatalf("relabel: %v", err)
	}
	if moved.Label != relabelled || moved.Name != "poll-community" || moved.ID != made.ID {
		t.Errorf("relabel moved something else: %+v", moved)
	}
	after, err := gw.RevealSecret(ctx, "", made.ID, all, all, true)
	if err != nil {
		t.Fatalf("reveal after relabel: %v", err)
	}
	if after["community"] != "private" {
		t.Errorf("revealed community = %q after a relabel, want private: the sealed value is bound to the "+
			"NAME, and a relabel is not a rename", after["community"])
	}
}

// TestAnInterfacesLabelIsItsOnlyOperatorTypedString is the case D2 was decided
// on, and it is the strongest of the four rather than the weakest.
//
// An interface's name is SERVER-derived: InterfaceSpec carries no Name and the
// column is set from spec.Type, an already-validated interface_type name. So an
// interface's only string is the protocol it speaks, which says what it talks
// and nothing about what it is FOR, and the declared name exemption in
// KeyProvedElsewhere is not something the label replaces: it is the ARGUMENT for
// the label.
//
// One correction to the premise this was argued from, measured here rather than
// assumed: a component can hold at most ONE interface per protocol today, since
// the unique index is (component, name) and the name IS the protocol. The
// three-`ssh`-interfaces case the epic argued from is not reachable, and the
// conflict is pinned below so the claim cannot quietly become true either way.
// What IS true today is narrower and enough: every SSH interface in the fleet
// reads `ssh`, on its component and in the fleet-wide list, so the label is the
// only place an operator can say which device role this connection plays.
//
// It is settable AT CREATE for the same reason. An interface that can only be
// labelled by a following call is unlabelled at the moment it is made, which is
// when the operator knows what they made it for.
func TestAnInterfacesLabelIsItsOnlyOperatorTypedString(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()

	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}

	first, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{
		Type: "ssh", Component: strptr(comp.Name), Label: "Control processor",
	}, all)
	if err != nil {
		t.Fatalf("create first interface: %v", err)
	}
	if first.Label != "Control processor" {
		t.Errorf("label = %q on the CREATE return, want it set at create: an interface labelled only by a "+
			"following patch is one an operator cannot tell apart at the moment they make it", first.Label)
	}

	// Read it back, since a create return can be right while the column is not.
	loaded, err := gw.GetInterface(ctx, first.ID, all)
	if err != nil {
		t.Fatalf("get interface: %v", err)
	}
	if loaded.Label != "Control processor" {
		t.Errorf("stored label = %q, want Control processor", loaded.Label)
	}
	if loaded.Name != first.Name {
		t.Errorf("name = %q, want the server-derived %q: the label does not name the row", loaded.Name, first.Name)
	}

	// The patch half, on the same row, and it moves nothing else.
	relabelled := "Control processor (rack A)"
	moved, err := gw.UpdateInterface(ctx, "", first.ID, storage.InterfacePatch{Label: &relabelled}, all, all)
	if err != nil {
		t.Fatalf("relabel: %v", err)
	}
	if moved.Label != relabelled || moved.Name != first.Name || moved.Type != "ssh" || moved.ID != first.ID {
		t.Errorf("relabel moved something else: %+v", moved)
	}

	// A patch that says nothing about the label leaves it alone, which is what
	// makes a node reassignment safe.
	kept, err := gw.UpdateInterface(ctx, "", first.ID, storage.InterfacePatch{Params: []byte(`{"target":"10.0.0.9"}`)}, all, all)
	if err != nil {
		t.Fatalf("retarget: %v", err)
	}
	if kept.Label != relabelled {
		t.Errorf("label = %q after a params-only patch, want it untouched", kept.Label)
	}

	// A SECOND interface of the same protocol is refused, which is the fact the
	// D2 argument got wrong and the reason the label's case is the narrower one
	// stated above rather than "tell three ssh interfaces apart".
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{
		Type: "ssh", Component: strptr(comp.Name), Label: "Second control processor",
	}, all); !errors.Is(err, storage.ErrInterfaceExists) {
		t.Errorf("a second ssh interface on one component = %v, want ErrInterfaceExists.\n"+
			"If this now succeeds, the label is what tells the two apart and the tests and docs "+
			"that say a component holds one interface per protocol are stale (#613).", err)
	}

	// An interface created with no label reads back empty and renders its
	// derived name verbatim, never a prettified version of it.
	bare, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "http", Component: strptr(comp.Name)}, all)
	if err != nil {
		t.Fatalf("create bare interface: %v", err)
	}
	if bare.Label != "" {
		t.Errorf("label = %q on an interface created without one, want empty", bare.Label)
	}
}

// TestAnUnlabelledInterfaceSortsLast is D4 on the last of the four. Both arms of
// ListInterfaces are ordered, the all-scope one and the component-subtree one,
// so both are driven here.
func TestAnUnlabelledInterfaceSortsLast(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()

	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	// Three types, so the derived names are distinct and the ordering is about
	// the label rather than about a disambiguated name.
	for _, r := range []struct{ kind, label string }{
		{"tcp", ""},
		{"ssh", "Zulu"},
		{"icmp", "Alpha"},
	} {
		if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{
			Type: r.kind, Component: strptr(comp.Name), Label: r.label,
		}, all); err != nil {
			t.Fatalf("create %s interface: %v", r.kind, err)
		}
	}

	want := []string{"icmp", "ssh", "tcp"} // Alpha, Zulu, then the unlabelled
	check := func(label string, got []storage.Interface) {
		t.Helper()
		var seen []string
		for _, it := range got {
			for _, w := range want {
				if it.Name == w {
					seen = append(seen, it.Name)
				}
			}
		}
		if len(seen) != len(want) {
			t.Fatalf("%s: found %v, wanted all of %v", label, seen, want)
		}
		for i := range seen {
			if seen[i] != want[i] {
				t.Fatalf("%s: order = %v, want %v (unlabelled last, #613)", label, seen, want)
			}
		}
	}

	allScope, err := gw.ListInterfaces(ctx, all)
	if err != nil {
		t.Fatalf("list interfaces (all): %v", err)
	}
	check("all scope", allScope)

	scoped, err := gw.ListInterfaces(ctx, scope.Set{IDs: []string{comp.ID}})
	if err != nil {
		t.Fatalf("list interfaces (scoped): %v", err)
	}
	check("component scope", scoped)
}
