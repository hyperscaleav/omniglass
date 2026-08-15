package storage_test

import (
	"context"
	"testing"

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
	// The seeded estate carries rows of its own, so the assertion is on the
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
