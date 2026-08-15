package storage_test

import (
	"context"
	"sort"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// Unset is SQL NULL and nothing else (#613, ADR-0118).
//
// The migration is the smaller half of that: it converts the rows that exist.
// What keeps NULL the ONLY spelling is `labelOrNull` / `labelPatch` in
// label_unset.go, and a helper is only worth as much as the number of write
// paths that actually call it. This is the sweep that checks the calling: every
// path that accepts a label is driven with a whitespace-only one and the RAW
// column is read back, not the projection, because every projection coalesces
// NULL to "" and would report the two as identical.
//
// It is complete by construction: a labelled table with neither a prover nor a
// declared reason fails, so the next table to gain a label cannot quietly skip
// this. That property is not decoration. Writing this test found CreateStandard
// still binding its label raw after every other path had been converted, and it
// found it as an ordering failure two registries away from the defect.

// labelUnsetExempt names the labelled tables no operator write path reaches
// with a label of their own, and why. Anything not here needs a prover.
var labelUnsetExempt = map[string]string{
	"secret_type":      "seeded only: the boot phase upserts the shipped set and there is no create path",
	"role":             "seeded only, same as secret_type",
	"task":             "content-addressed and derived; its label is minted with the row, never operator-typed",
	"role_choice":      "written as part of a role declaration, covered by the system_role prover on the same write",
	"choice_alternate": "written inside its choice, covered by the same write as role_choice",
	"system_role":      "written by SetStandardRole with a label the declaration carries; the arc is keyed by owner, not by a bare name",
	"human":            "keyed by username rather than name; covered by the human profile prover below",
	"principal_group":  "covered by the group prover below, which is keyed by name like the rest",
	// The three PENNED tables. A create with no label does not leave the column
	// unset on these: the platform holds the pen and a label rule renders one,
	// so the row lands with "Generic Device" rather than with NULL and this
	// sweep would be asserting the wrong thing. What their unset case looks
	// like is TestARuleRenderingNothingLeavesTheLabelUnsetRatherThanBlank,
	// which drives a rule that renders nothing and reads the same raw column.
	"component": "a label rule renders one at create; the unset case is proved by the rule-renders-nothing test",
	"system":    "same: the pen means a create with no label still stores what the rule rendered",
	"location":  "same as component and system",
}

// TestEveryWritePathStoresNullForAnUnsetLabel drives each create with a label
// that is whitespace only, which is the hardest case: it is neither absent nor
// obviously empty, and it renders as a blank row on every surface.
func TestEveryWritePathStoresNullForAnUnsetLabel(t *testing.T) {
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
	conn := connectDSN(t, dsn)

	// The label every prover writes: whitespace, not "". An implementation that
	// only checks for the empty string passes with "" and fails here, which is
	// the point of choosing it.
	const blank = "   "

	rawLabelOf := func(t *testing.T, table, keyCol, key string) *string {
		t.Helper()
		var got *string
		q := "select label from " + pgx.Identifier{table}.Sanitize() + " where " + keyCol + " = $1"
		if err := conn.QueryRow(ctx, q, key).Scan(&got); err != nil {
			t.Fatalf("read raw %s.label: %v", table, err)
		}
		return got
	}

	stem := "probe"
	provers := map[string]func(t *testing.T) (keyCol, key string){
		"variable": func(t *testing.T) (string, string) {
			if _, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
				Name: "lbl-var", Label: blank, ValueType: "int", OwnerKind: "platform",
				Value: []byte(`1`),
			}, all); err != nil {
				t.Fatalf("create variable: %v", err)
			}
			return "name", "lbl-var"
		},
		"tag": func(t *testing.T) (string, string) {
			if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "lbl-tag", Label: blank}, all); err != nil {
				t.Fatalf("create tag: %v", err)
			}
			return "name", "lbl-tag"
		},
		"node": func(t *testing.T) (string, string) {
			if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "lbl-node", Label: blank}, all, all); err != nil {
				t.Fatalf("create node: %v", err)
			}
			return "name", "lbl-node"
		},
		"location_type": func(t *testing.T) (string, string) {
			if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: "lbl-lt", Label: blank}); err != nil {
				t.Fatalf("create location_type: %v", err)
			}
			return "name", "lbl-lt"
		},
		"standard": func(t *testing.T) (string, string) {
			if _, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "lbl-std", Label: blank}); err != nil {
				t.Fatalf("create standard: %v", err)
			}
			return "name", "lbl-std"
		},
		"vendor": func(t *testing.T) (string, string) {
			if _, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: "lbl-ven", Label: blank, Kind: "manufacturer"}); err != nil {
				t.Fatalf("create vendor: %v", err)
			}
			return "name", "lbl-ven"
		},
		"driver": func(t *testing.T) (string, string) {
			if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "lbl-drv", Label: blank}); err != nil {
				t.Fatalf("create driver: %v", err)
			}
			return "name", "lbl-drv"
		},
		"product": func(t *testing.T) (string, string) {
			if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "lbl-prod", Label: blank}); err != nil {
				t.Fatalf("create product: %v", err)
			}
			return "name", "lbl-prod"
		},
		"component_type": func(t *testing.T) (string, string) {
			if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "lbl-ct", Label: blank, Stem: &stem}); err != nil {
				t.Fatalf("create component_type: %v", err)
			}
			return "name", "lbl-ct"
		},
		"system_type": func(t *testing.T) (string, string) {
			if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "lbl-st", Label: blank, Stem: &stem}); err != nil {
				t.Fatalf("create system_type: %v", err)
			}
			return "name", "lbl-st"
		},
		"property_type": func(t *testing.T) (string, string) {
			if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "lbl-prop", Label: blank, DataType: "string"}); err != nil {
				t.Fatalf("create property_type: %v", err)
			}
			return "name", "lbl-prop"
		},
		"metric_type": func(t *testing.T) (string, string) {
			if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "lbl-metric", Label: blank, DataType: "int"}); err != nil {
				t.Fatalf("create metric_type: %v", err)
			}
			return "name", "lbl-metric"
		},
		"event_type": func(t *testing.T) (string, string) {
			if _, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{Name: "lbl-event", Label: blank}); err != nil {
				t.Fatalf("create event_type: %v", err)
			}
			return "name", "lbl-event"
		},
		"command_type": func(t *testing.T) (string, string) {
			if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{Name: "lbl-cmd", Label: blank}); err != nil {
				t.Fatalf("create command_type: %v", err)
			}
			return "name", "lbl-cmd"
		},
	}

	// Completeness: every labelled table is proved or declared, and no
	// declaration is stale. labelledTables is the schema's own list
	// (label_column_migration_test.go).
	var missing []string
	for _, table := range labelledTables {
		_, proved := provers[table]
		_, excused := labelUnsetExempt[table]
		switch {
		case proved && excused:
			t.Errorf("%q is both proved and exempt from the unset-label sweep; pick one", table)
		case !proved && !excused:
			missing = append(missing, table)
		}
	}
	sort.Strings(missing)
	for _, table := range missing {
		t.Errorf("%q carries a label and nothing proves its write path stores NULL for an unset one.\n"+
			"Add it to provers, or name it in labelUnsetExempt with the reason no write path reaches it.", table)
	}
	for table := range labelUnsetExempt {
		found := false
		for _, l := range labelledTables {
			if l == table {
				found = true
			}
		}
		if !found {
			t.Errorf("labelUnsetExempt names %q, which carries no label", table)
		}
	}

	for table, prove := range provers {
		t.Run(table, func(t *testing.T) {
			keyCol, key := prove(t)
			if got := rawLabelOf(t, table, keyCol, key); got != nil {
				t.Errorf("%s.label = %q after a create with a whitespace label, want SQL NULL.\n"+
					"Bind the write through labelOrNull (internal/storage/label_unset.go): unset has one "+
					"spelling and it is NULL (#613).", table, *got)
			}
		})
	}

	// The other half: the human profile, keyed by username rather than name.
	t.Run("human", func(t *testing.T) {
		if _, err := gw.CreateHumanPrincipal(ctx, "", storage.HumanSpec{Username: "lbl-human", Label: blank}, all); err != nil {
			t.Fatalf("create human: %v", err)
		}
		if got := rawLabelOf(t, "human", "username", "lbl-human"); got != nil {
			t.Errorf("human.label = %q after a create with a whitespace label, want SQL NULL", *got)
		}
	})

	t.Run("principal_group", func(t *testing.T) {
		if _, err := gw.CreateGroup(ctx, "", storage.GroupSpec{Name: "lbl-group", Label: blank}, all); err != nil {
			t.Fatalf("create group: %v", err)
		}
		if got := rawLabelOf(t, "principal_group", "name", "lbl-group"); got != nil {
			t.Errorf("principal_group.label = %q after a create with a whitespace label, want SQL NULL", *got)
		}
	})
}

// TestClearingALabelStoresNullRatherThanEmpty is the PATCH half. It is a
// separate behaviour from the create: a patch has three states, not two, and
// the empty string is how an operator says "clear it" rather than "leave it".
// A `coalesce($n, label)` cannot tell those apart, which is why every one of
// these statements carries a set flag beside the value.
func TestClearingALabelStoresNullRatherThanEmpty(t *testing.T) {
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
	conn := connectDSN(t, dsn)

	empty := ""
	stem := "probe"

	cases := []struct {
		table   string
		keyCol  string
		prepare func(t *testing.T) string // creates a LABELLED row, returns its key
		clear   func(t *testing.T, key string)
	}{
		{"variable", "name", func(t *testing.T) string {
			v, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
				Name: "clr-var", Label: "Labelled", ValueType: "int", OwnerKind: "platform",
				Value: []byte(`1`),
			}, all)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			return v.Name
		}, func(t *testing.T, key string) {
			v := variableNamed(t, gw, key)
			if _, err := gw.UpdateVariable(ctx, "", v.ID, storage.VariablePatch{Label: &empty}, all, all, true); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}},
		{"tag", "name", func(t *testing.T) string {
			if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "clr-tag", Label: "Labelled"}, all); err != nil {
				t.Fatalf("create: %v", err)
			}
			return "clr-tag"
		}, func(t *testing.T, key string) {
			if _, err := gw.UpdateTag(ctx, "", key, storage.TagPatch{Label: &empty}, all); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}},
		{"vendor", "name", func(t *testing.T) string {
			if _, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: "clr-ven", Label: "Labelled", Kind: "manufacturer"}); err != nil {
				t.Fatalf("create: %v", err)
			}
			return "clr-ven"
		}, func(t *testing.T, key string) {
			if _, err := gw.UpdateVendor(ctx, "", key, storage.VendorPatch{Label: &empty}); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}},
		{"driver", "name", func(t *testing.T) string {
			if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "clr-drv", Label: "Labelled"}); err != nil {
				t.Fatalf("create: %v", err)
			}
			return "clr-drv"
		}, func(t *testing.T, key string) {
			if _, err := gw.UpdateDriver(ctx, "", key, storage.DriverPatch{Label: &empty}); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}},
		{"product", "name", func(t *testing.T) string {
			if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "clr-prod", Label: "Labelled"}); err != nil {
				t.Fatalf("create: %v", err)
			}
			return "clr-prod"
		}, func(t *testing.T, key string) {
			if _, err := gw.UpdateProduct(ctx, "", key, storage.ProductPatch{Label: &empty}); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}},
		{"standard", "name", func(t *testing.T) string {
			if _, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "clr-std", Label: "Labelled"}); err != nil {
				t.Fatalf("create: %v", err)
			}
			return "clr-std"
		}, func(t *testing.T, key string) {
			if _, err := gw.UpdateStandard(ctx, "", key, storage.StandardPatch{Label: &empty}); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}},
		{"component_type", "name", func(t *testing.T) string {
			if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "clr-ct", Label: "Labelled", Stem: &stem}); err != nil {
				t.Fatalf("create: %v", err)
			}
			return "clr-ct"
		}, func(t *testing.T, key string) {
			if _, err := gw.UpdateComponentType(ctx, "", key, storage.ComponentTypePatch{Label: &empty}); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}},
		{"system_type", "name", func(t *testing.T) string {
			if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "clr-st", Label: "Labelled", Stem: &stem}); err != nil {
				t.Fatalf("create: %v", err)
			}
			return "clr-st"
		}, func(t *testing.T, key string) {
			if _, err := gw.UpdateSystemType(ctx, "", key, storage.SystemTypePatch{Label: &empty}); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}},
		{"node", "name", func(t *testing.T) string {
			if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "clr-node", Label: "Labelled"}, all, all); err != nil {
				t.Fatalf("create: %v", err)
			}
			return "clr-node"
		}, func(t *testing.T, key string) {
			if _, err := gw.UpdateNode(ctx, "", key, storage.NodePatch{Label: &empty}, all, all, all); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			key := tc.prepare(t)
			var before *string
			q := "select label from " + pgx.Identifier{tc.table}.Sanitize() + " where " + tc.keyCol + " = $1"
			if err := conn.QueryRow(ctx, q, key).Scan(&before); err != nil {
				t.Fatalf("read before: %v", err)
			}
			if before == nil || *before != "Labelled" {
				t.Fatalf("precondition: %s.label = %v, want %q", tc.table, before, "Labelled")
			}
			tc.clear(t, key)
			var after *string
			if err := conn.QueryRow(ctx, q, key).Scan(&after); err != nil {
				t.Fatalf("read after: %v", err)
			}
			if after != nil {
				t.Errorf("%s.label = %q after an explicit clear, want SQL NULL.\n"+
					"An empty string in a PATCH means CLEAR, and the cleared state is NULL (#613).", tc.table, *after)
			}
		})
	}
}

// variableNamed finds a variable by name for the clear cases, which key on the
// name like every other row here while the write path takes the uuid.
func variableNamed(t *testing.T, gw *storage.PG, name string) storage.Variable {
	t.Helper()
	vars, err := gw.ListVariables(context.Background(), all)
	if err != nil {
		t.Fatalf("list variables: %v", err)
	}
	for _, v := range vars {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("no variable named %q", name)
	return storage.Variable{}
}
