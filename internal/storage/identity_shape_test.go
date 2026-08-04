package storage_test

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The declaration is in internal/storage/identity_shape.go, and it is the only copy:
// cmd/identitygen renders it into the docs, so the prose cannot restate it and drift.
// This file's job is the half a declaration cannot do for itself, which is agreeing
// with the live schema.
//
// Two guards. One says every table has a declared shape, reading the generated schema
// (docs/src/generated/schema.json, emitted by cmd/erdgen) so a new table is a
// compile-clean, review-clean, FAILING test until somebody classifies it. The other
// says every table declared key-bearing actually has its refusal proved.
//
// An earlier version only inspected tables carrying a `name` column, which made it
// blind to 28 of the 51: a table identified by a username or a content hash escaped
// it entirely. Absence of a `name` is not evidence of absence of an identifier.
//
// Declaring a table in two shapes is not guarded, because the declaration is a map
// keyed by table and cannot express it.

func liveTables(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../docs/src/generated/schema.json")
	if err != nil {
		t.Fatalf("read generated schema: %v", err)
	}
	var doc struct {
		Subsystems map[string]map[string]json.RawMessage `json:"subsystems"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse generated schema: %v", err)
	}
	live := map[string]bool{}
	for _, tables := range doc.Subsystems {
		for table := range tables {
			live[table] = true
		}
	}
	if len(live) == 0 {
		t.Fatal("found no tables, which means this guard is not reading the schema it thinks it is")
	}
	return live
}

func TestEveryTableHasADeclaredIdentityShape(t *testing.T) {
	live := liveTables(t)

	var undeclared []string
	for table := range live {
		if _, ok := storage.IdentityShapes[table]; !ok {
			undeclared = append(undeclared, table)
		}
	}
	sort.Strings(undeclared)
	for _, table := range undeclared {
		t.Errorf("%q has no declared identity shape in internal/storage/identity_shape.go.\n"+
			"Pick one and, for the last two, say why:\n"+
			"  ShapeKeyBearing    an operator types its key, on the entity-key rule\n"+
			"  ShapeKeyspace      an operator types its key, on internal/key's rule\n"+
			"  ShapeHumanNotAKey  a human identifier that is not a key: a username, a filename,\n"+
			"                     a content hash. Each looks key-shaped from a distance\n"+
			"  ShapeIDOnly        nobody names it; it is addressed by uuid", table)
	}

	// A declaration naming a table that no longer exists is stale, and a stale entry
	// is how a list stops describing the schema.
	var ghosts []string
	for table := range storage.IdentityShapes {
		if !live[table] {
			ghosts = append(ghosts, table)
		}
	}
	for table := range storage.KeyProvedElsewhere {
		if !live[table] {
			ghosts = append(ghosts, table)
		}
	}
	sort.Strings(ghosts)
	for _, table := range ghosts {
		t.Errorf("the identity declaration names %q, which is not a table in the generated schema", table)
	}

	// The shapes that carry a reason must carry one: "it is an exception" without the
	// reason is the state this whole declaration exists to end.
	for table, id := range storage.IdentityShapes {
		if id.Shape == storage.ShapeKeyspace || id.Shape == storage.ShapeHumanNotAKey {
			if id.Reason == "" {
				t.Errorf("%q is declared %q with no reason; an exception without one reads as an oversight",
					table, id.Shape)
			}
		}
	}
}

// TestEveryKeyBearingTableIsProved is the cross-check between classification and
// behaviour: a table declared key-bearing is either driven through the gateway by the
// create sweep or excused in KeyProvedElsewhere, and adding one without doing either
// fails the build.
//
// This exists because the two used to be independent lists. Sixteen tables were
// classified, ten were proved, and nothing noticed the six in between.
func TestEveryKeyBearingTableIsProved(t *testing.T) {
	proved := provedByCreateTables()
	for table, id := range storage.IdentityShapes {
		if id.Shape != storage.ShapeKeyBearing {
			continue
		}
		_, excused := storage.KeyProvedElsewhere[table]
		switch {
		case proved[table] && excused:
			t.Errorf("%q is both proved by the create sweep and excused from it; pick one", table)
		case !proved[table] && !excused:
			t.Errorf("%q is declared key-bearing but nothing proves it rejects an illegal key.\n"+
				"Either add it to provedByCreate in entity_key_validation_test.go so the behaviour is "+
				"proved, or name it in KeyProvedElsewhere with the reason it cannot be.", table)
		}
	}
	for table := range storage.KeyProvedElsewhere {
		if storage.IdentityShapes[table].Shape != storage.ShapeKeyBearing {
			t.Errorf("KeyProvedElsewhere names %q, which is not declared key-bearing", table)
		}
	}
}
