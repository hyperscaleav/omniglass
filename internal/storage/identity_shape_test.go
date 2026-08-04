package storage_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

// TestIdentityShapesMatchesCommitted is the drift gate for the generated reference,
// mirroring TestConfigFactsMatchesCommitted: the committed
// docs/src/generated/identity.json must equal a fresh render of the declaration.
//
// CI already diffs docs/src/generated/ after make gen, so a stale file cannot merge.
// This catches it in make test instead, because the repo validates locally rather
// than leaning on CI, and because a contributor who edits the declaration should
// learn it needs regenerating before they push.
func TestIdentityShapesMatchesCommitted(t *testing.T) {
	fresh, err := storage.IdentityShapesJSON()
	if err != nil {
		t.Fatalf("IdentityShapesJSON: %v", err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/identity.json")
	if err != nil {
		t.Fatalf("read committed identity.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh)+"\n" {
		t.Error("docs/src/generated/identity.json drifted from internal/storage/identity_shape.go: " +
			"run make gen and commit the result")
	}
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
		// Both name-bearing shapes, not just one. Checking only key-bearing left the
		// identical hole on the other rule: a new ShapeKeyspace table was skipped
		// here and never reached by the keyspace sweep either, so it shipped with no
		// name validation and a green suite.
		if id.Shape != storage.ShapeKeyBearing && id.Shape != storage.ShapeKeyspace {
			continue
		}
		_, excused := storage.KeyProvedElsewhere[table]
		switch {
		case proved[table] && excused:
			t.Errorf("%q is both proved by the create sweep and excused from it; pick one", table)
		case !proved[table] && !excused:
			t.Errorf("%q is declared %q but nothing proves it rejects an illegal name.\n"+
				"Either add it to provedByCreate (entity rule) or provedByCreateKeyspace "+
				"(dotted rule) in entity_key_validation_test.go so the behaviour is proved, "+
				"or name it in KeyProvedElsewhere with the reason it cannot be.", table, id.Shape)
		}
	}
	for table := range storage.KeyProvedElsewhere {
		shape := storage.IdentityShapes[table].Shape
		if shape != storage.ShapeKeyBearing && shape != storage.ShapeKeyspace {
			t.Errorf("KeyProvedElsewhere names %q, which bears no operator-typed name", table)
		}
	}
}

// TestEveryValidateNameCallSiteNamesADeclaredTable closes the one gap the runtime
// cannot: ValidateName takes the table as a string, so a typo compiles, passes vet,
// and fails closed at runtime with ErrTableNotNameBearing. No mapper knows that
// sentinel, so it surfaces as a 500 on a legal request.
//
// Most call sites are covered by the create sweep, which would fail on the wrong
// sentinel. The ones not driven by it (a rename leg, a path a test does not reach)
// would ship the 500 with a green suite. This reads the literals out of the source
// instead, so coverage does not depend on which paths happen to have tests.
func TestEveryValidateNameCallSiteNamesADeclaredTable(t *testing.T) {
	call := regexp.MustCompile(`ValidateName\("([^"]*)"`)

	var checked int
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			// Production call sites only. A test may pass an undeclared table on
			// purpose, which is exactly what identity_name_rule_test.go does to pin
			// that an unclassified table cannot validate by default.
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range call.FindAllStringSubmatch(string(src), -1) {
				table := m[1]
				checked++
				id, declared := storage.IdentityShapes[table]
				switch {
				case !declared:
					t.Errorf("%s calls ValidateName(%q, ...), which is not a table in the identity "+
						"declaration. This compiles and fails closed at runtime as a 500.", path, table)
				case id.Shape != storage.ShapeKeyBearing && id.Shape != storage.ShapeKeyspace:
					t.Errorf("%s calls ValidateName(%q, ...), but that table is declared %q and bears "+
						"no operator-typed name, so the call can only ever fail.", path, table, id.Shape)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	// If the scan finds nothing it is passing vacuously, which is worse than failing.
	if checked == 0 {
		t.Fatal("found no ValidateName call sites, so this guard is not reading the tree it thinks it is")
	}
	t.Logf("checked %d ValidateName call sites", checked)
}
