package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// Every registry list orders by the label an operator reads, and an UNLABELLED
// row sorts last rather than first (#613).
//
// This is the one behaviour in the display_name-to-label sweep that a rename
// cannot carry across on its own, because it is a property of the VALUE and not
// of the column name. Postgres sorts NULL last under a default ASC and the empty
// string FIRST, so a list ordered by a bare `order by label, name` puts
// unlabelled rows at the bottom while the column is nullable and at the top the
// moment it is not. Two registries (component_type, system_type) had already
// crossed that line before this test was written: both are NOT NULL with an
// empty-string default today, so both already float their unlabelled rows to
// the top of the picker. Normalizing the other eighteen tables to the same shape
// would have moved every remaining registry the same way, silently, with no test
// failing.
//
// The fix is to spell the ordering so it does not care which of the two spellings
// of "unset" the column happens to use: a nullif over the empty string, ordered
// nulls last. It reads the same on a nullable column, on a NOT NULL one, and on
// the mix that exists mid-migration.
//
// This is deliberately NOT D4 (ordering by the RENDERED label, falling back to
// the name). That is ruled and belongs to the slice that gives tag, variable,
// secret and interface a label. What is pinned here is the ordering the
// registries have TODAY, made insensitive to the nullability change.
func TestAnUnlabelledRegistryRowSortsLast(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The three rows every case creates. The names are chosen so that ordering by
	// NAME, or by an unset label that sorts first, puts `aaa-unlabelled` at the
	// top; ordering by the label with unset last puts it at the bottom.
	const (
		unlabelled = "aaa-unlabelled"
		labelledZ  = "mmm-labelled-z"
		labelledA  = "zzz-labelled-a"
	)
	wantOrder := []string{labelledA, labelledZ, unlabelled} // "Alpha", "Zulu", then unset

	type row struct{ name, label string }

	cases := []struct {
		registry string
		create   func(t *testing.T, name, label string)
		list     func(t *testing.T) []row
	}{
		{
			registry: "vendor",
			create: func(t *testing.T, name, label string) {
				if _, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: name, Label: label, Kind: "manufacturer"}); err != nil {
					t.Fatalf("create vendor %q: %v", name, err)
				}
			},
			list: func(t *testing.T) []row {
				all, err := gw.ListVendors(ctx)
				if err != nil {
					t.Fatalf("list vendors: %v", err)
				}
				out := make([]row, 0, len(all))
				for _, v := range all {
					out = append(out, row{v.Name, v.Label})
				}
				return out
			},
		},
		{
			registry: "driver",
			create: func(t *testing.T, name, label string) {
				if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: name, Label: label}); err != nil {
					t.Fatalf("create driver %q: %v", name, err)
				}
			},
			list: func(t *testing.T) []row {
				all, err := gw.ListDrivers(ctx)
				if err != nil {
					t.Fatalf("list drivers: %v", err)
				}
				out := make([]row, 0, len(all))
				for _, d := range all {
					out = append(out, row{d.Name, d.Label})
				}
				return out
			},
		},
		{
			registry: "product",
			create: func(t *testing.T, name, label string) {
				if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: name, Label: label}); err != nil {
					t.Fatalf("create product %q: %v", name, err)
				}
			},
			list: func(t *testing.T) []row {
				all, err := gw.ListProducts(ctx)
				if err != nil {
					t.Fatalf("list products: %v", err)
				}
				out := make([]row, 0, len(all))
				for _, p := range all {
					out = append(out, row{p.Name, p.Label})
				}
				return out
			},
		},
		{
			registry: "standard",
			create: func(t *testing.T, name, label string) {
				if _, err := gw.CreateStandard(ctx, "", storage.Standard{Name: name, Label: label}); err != nil {
					t.Fatalf("create standard %q: %v", name, err)
				}
			},
			list: func(t *testing.T) []row {
				all, err := gw.ListStandards(ctx)
				if err != nil {
					t.Fatalf("list standards: %v", err)
				}
				out := make([]row, 0, len(all))
				for _, s := range all {
					out = append(out, row{s.Name, s.Label})
				}
				return out
			},
		},
		{
			registry: "system_type",
			create: func(t *testing.T, name, label string) {
				stem := name
				if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: name, Label: label, Stem: &stem}); err != nil {
					t.Fatalf("create system_type %q: %v", name, err)
				}
			},
			list: func(t *testing.T) []row {
				all, err := gw.ListSystemTypes(ctx)
				if err != nil {
					t.Fatalf("list system_types: %v", err)
				}
				out := make([]row, 0, len(all))
				for _, s := range all {
					out = append(out, row{s.Name, s.Label})
				}
				return out
			},
		},
		{
			registry: "component_type",
			create: func(t *testing.T, name, label string) {
				stem := name
				if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: name, Label: label, Stem: &stem}); err != nil {
					t.Fatalf("create component_type %q: %v", name, err)
				}
			},
			list: func(t *testing.T) []row {
				all, err := gw.ListComponentTypes(ctx)
				if err != nil {
					t.Fatalf("list component_types: %v", err)
				}
				out := make([]row, 0, len(all))
				for _, c := range all {
					out = append(out, row{c.Name, c.Label})
				}
				return out
			},
		},
		{
			registry: "location_type",
			create: func(t *testing.T, name, label string) {
				if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: name, Label: label}); err != nil {
					t.Fatalf("create location_type %q: %v", name, err)
				}
			},
			list: func(t *testing.T) []row {
				all, err := gw.ListLocationTypes(ctx)
				if err != nil {
					t.Fatalf("list location_types: %v", err)
				}
				out := make([]row, 0, len(all))
				for _, l := range all {
					out = append(out, row{l.Name, l.Label})
				}
				return out
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.registry, func(t *testing.T) {
			tc.create(t, unlabelled, "")
			tc.create(t, labelledZ, "Zulu")
			tc.create(t, labelledA, "Alpha")

			all := tc.list(t)

			// The three rows this case created, in the order the registry returned
			// them. Everything the seed installed is ignored here; the next
			// assertion is the one that covers it.
			mine := make([]string, 0, 3)
			for _, r := range all {
				switch r.name {
				case unlabelled, labelledZ, labelledA:
					mine = append(mine, r.name)
				}
			}
			if len(mine) != len(wantOrder) {
				t.Fatalf("%s: found %d of the 3 created rows in the list (%v)", tc.registry, len(mine), mine)
			}
			for i := range wantOrder {
				if mine[i] != wantOrder[i] {
					t.Fatalf("%s list order = %v, want %v.\n"+
						"An unlabelled row must sort LAST, not first. Postgres sorts '' before every "+
						"other string, so a bare `order by label, name` floats every unlabelled row to "+
						"the top of the picker the moment the column stops being nullable. Spell it "+
						"`order by nullif(label, '') nulls last, name`.", tc.registry, mine, wantOrder)
				}
			}

			// The invariant, over the whole list and not just the three fixtures:
			// no labelled row ever appears after an unlabelled one.
			seenUnlabelled := ""
			for _, r := range all {
				if r.label == "" {
					seenUnlabelled = r.name
					continue
				}
				if seenUnlabelled != "" {
					t.Fatalf("%s: labelled row %q (%q) sorts after unlabelled row %q; "+
						"every unlabelled row belongs at the end of the list", tc.registry, r.name, r.label, seenUnlabelled)
				}
			}
		})
	}
}
