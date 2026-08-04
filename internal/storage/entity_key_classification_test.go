package storage_test

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// Six tables went unvalidated for as long as they did because the only thing
// listing which tables carry a key was a set of hand-written call sites. A
// table added later joined no list and tripped no test, so it simply never
// acquired the rule.
//
// This closes that. It reads the generated schema (docs/src/generated/schema.json,
// emitted by cmd/erdgen, so it cannot drift from the live DDL), finds every table
// carrying a `name` column, and requires each one to be classified below. A new
// table with a name is a compile-clean, review-clean, FAILING test until somebody
// says which bucket it is in. The decision stops being implicit.
//
// The two buckets are not stylistic. A segment is one token of an address, so it
// takes the key rule and gets `$`, `.`, `*`, and `>` refused. A keyspace key
// is a namespaced identifier referenced from drivers, templates, and expressions,
// so it keeps its own character set: `icmp.rtt_avg` is a legitimate key and an
// illegal key, and running the key rule over the key tables would reject
// data the seed corpus ships today.
var (
	// keyBearing: `name` is the token this entity contributes to an address.
	// Every one of these must reject an illegal key on its create path; the
	// behaviour is proved by TestEveryKeyBearingTableValidates.
	keyBearing = map[string]bool{
		"capability":      true,
		"component":       true,
		"driver":          true,
		"interface":       true,
		"interface_type":  true,
		"location":        true,
		"location_type":   true,
		"node":            true,
		"principal_group": true,
		"product":         true,
		"role":            true,
		"secret_type":     true,
		"standard":        true,
		"system":          true,
		"system_role":     true,
		"vendor":          true,
	}

	// keyspaceOrOther: `name` is deliberately NOT a segment, with the reason.
	keyspaceOrOther = map[string]string{
		"property_type": "a keyspace key, snake_case with a dot hierarchy (icmp.rtt_avg)",
		"event_type":    "a keyspace key (call.started)",
		"command_type":  "a keyspace key (set_input)",
		"tag":           "a tag key, snake_case (asset_id); the console calls it a key",
		"variable":      "a variable key referenced from expressions (poll_interval)",
		"secret":        "a secret key referenced from expressions (og_session)",
		"file":          "a filename with an extension, not unique, already the label",
	}
)

// keyBearing declares which tables carry a key. TestEveryKeyBearingTableValidates
// PROVES the behaviour for the ones it can reach through the gateway. Those are two
// lists, and two lists that must agree with nothing making them agree is the exact
// shape that let six tables go unvalidated in the first place.
//
// So this cross-checks them. A table classified key-bearing is either proved by the
// behavioural test or named here with the reason it cannot be, and adding one to
// keyBearing without doing either fails the build.
var provedElsewhere = map[string]string{
	"interface_type": "seeded only, no create path on the gateway",
	"role":           "seeded only, no create path on the gateway",
	"secret_type":    "seeded only, no create path on the gateway",
	"interface": "the name is server-derived from the interface type (interfaces.go sets it from " +
		"spec.Type, an already-validated interface_type key); InterfaceSpec carries no Name, so an " +
		"operator never types one",
	"principal_group": "validated at the API layer with a looser pattern (^[a-z0-9][a-z0-9._-]*$), " +
		"which admits the . and _ the address grammar reads as separators; tightening it is a " +
		"behaviour change for existing groups, tracked with the rename work",
}

// TestEveryKeyBearingTableIsProved is the cross-check: classification without proof
// is a claim, not a guard.
func TestEveryKeyBearingTableIsProved(t *testing.T) {
	for table := range keyBearing {
		proved := provedByCreateTables()[table]
		_, excused := provedElsewhere[table]
		switch {
		case proved && excused:
			t.Errorf("%q is both proved by the create sweep and excused from it; pick one", table)
		case !proved && !excused:
			t.Errorf("%q is classified key-bearing but nothing proves it rejects an illegal key.\n"+
				"Either add it to the create map in entity_key_validation_test.go so the behaviour is "+
				"proved, or name it in provedElsewhere with the reason it cannot be reached that way.", table)
		}
	}
	for table := range provedElsewhere {
		if !keyBearing[table] {
			t.Errorf("provedElsewhere names %q, which is not classified key-bearing", table)
		}
	}
}

func TestEveryNamedTableIsClassified(t *testing.T) {
	raw, err := os.ReadFile("../../docs/src/generated/schema.json")
	if err != nil {
		t.Fatalf("read generated schema: %v", err)
	}
	var doc struct {
		Subsystems map[string]map[string]struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
		} `json:"subsystems"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse generated schema: %v", err)
	}

	var named, unclassified, both []string
	for _, tables := range doc.Subsystems {
		for table, spec := range tables {
			hasName := false
			for _, c := range spec.Columns {
				if c.Name == "name" {
					hasName = true
					break
				}
			}
			if !hasName {
				continue
			}
			named = append(named, table)
			_, isSeg := keyBearing[table]
			_, isKey := keyspaceOrOther[table]
			switch {
			case isSeg && isKey:
				both = append(both, table)
			case !isSeg && !isKey:
				unclassified = append(unclassified, table)
			}
		}
	}
	sort.Strings(named)
	sort.Strings(unclassified)

	if len(named) == 0 {
		t.Fatal("found no table with a name column, which means this guard is not reading the schema it thinks it is")
	}
	for _, table := range both {
		t.Errorf("%q is in BOTH keyBearing and keyspaceOrOther; it is one or the other", table)
	}
	for _, table := range unclassified {
		t.Errorf("%q carries a `name` column and is in neither keyBearing nor keyspaceOrOther.\n"+
			"Decide which it is. If the name is one token of an address, add it to keyBearing "+
			"AND give its create path a ValidateEntityKey call (and a case in this package's create "+
			"map, so the behaviour is proved). If it is a namespaced key referenced from drivers, "+
			"templates, or expressions, add it to keyspaceOrOther with the reason.", table)
	}

	// The reverse direction: a classification naming a table that no longer exists
	// is stale, and stale entries are how a list stops describing the schema.
	live := map[string]bool{}
	for _, table := range named {
		live[table] = true
	}
	for table := range keyBearing {
		if !live[table] {
			t.Errorf("keyBearing lists %q, which has no `name` column in the generated schema", table)
		}
	}
	for table := range keyspaceOrOther {
		if !live[table] {
			t.Errorf("keyspaceOrOther lists %q, which has no `name` column in the generated schema", table)
		}
	}
}
