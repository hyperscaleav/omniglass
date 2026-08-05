package erd

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestIntrospectRealSchema runs Introspect against the real, migrated schema and
// pins load-bearing facts: the estate's component table exists, the property series
// carries a foreign key to its property_type catalog, and the Subsystems cluster
// map covers exactly the live tables (no unmapped table, no stale map entry). The
// last check is what keeps the ERD legible: a new table with no home surfaces here.
func TestIntrospectRealSchema(t *testing.T) {
	dsn := storagetest.NewDSN(t) // skips under -short
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	s, err := Introspect(ctx, conn)
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	byName := make(map[string]Table, len(s.Tables))
	for _, tb := range s.Tables {
		byName[tb.Name] = tb
	}

	if _, ok := byName["component"]; !ok {
		t.Error("component table not introspected")
	}

	// `state` is gone (#588): the sample series took the freed bare noun
	// `property` and carries the catalog FK.
	if _, ok := byName["state"]; ok {
		t.Error("the renamed `state` table was introspected; #588 renamed it to property")
	}
	st, ok := byName["property"]
	if !ok {
		t.Fatal("property table not introspected")
	}
	hasTypeFK := false
	for _, fk := range st.FKs {
		if fk.RefTable == "property_type" {
			hasTypeFK = true
		}
	}
	if !hasTypeFK {
		t.Errorf("property has no foreign key to property_type; FKs=%+v", st.FKs)
	}

	mapped := map[string]bool{}
	for _, c := range Subsystems {
		for _, name := range c.Tables {
			mapped[name] = true
		}
	}
	var orphans []string
	for _, tb := range s.Tables {
		if !mapped[tb.Name] {
			orphans = append(orphans, tb.Name)
		}
	}
	if len(orphans) > 0 {
		t.Errorf("live tables missing from the Subsystems map (add them): %v", orphans)
	}

	exists := map[string]bool{}
	for _, tb := range s.Tables {
		exists[tb.Name] = true
	}
	var stale []string
	for _, c := range Subsystems {
		for _, name := range c.Tables {
			if !exists[name] {
				stale = append(stale, name)
			}
		}
	}
	if len(stale) > 0 {
		t.Errorf("Subsystems names tables absent from the schema (remove them): %v", stale)
	}
}
