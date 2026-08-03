package storage_test

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// Six tables went unvalidated for as long as they did because the only thing
// listing which tables carry a segment was a set of hand-written call sites. A
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
// takes the segment rule and gets `$`, `.`, `*`, and `>` refused. A keyspace key
// is a namespaced identifier referenced from drivers, templates, and expressions,
// so it keeps its own character set: `icmp.rtt_avg` is a legitimate key and an
// illegal segment, and running the segment rule over the key tables would reject
// data the seed corpus ships today.
var (
	// segmentBearing: `name` is the token this entity contributes to an address.
	// Every one of these must reject an illegal segment on its create path; the
	// behaviour is proved by TestEverySegmentBearingTableValidates.
	segmentBearing = map[string]bool{
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
			_, isSeg := segmentBearing[table]
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
		t.Errorf("%q is in BOTH segmentBearing and keyspaceOrOther; it is one or the other", table)
	}
	for _, table := range unclassified {
		t.Errorf("%q carries a `name` column and is in neither segmentBearing nor keyspaceOrOther.\n"+
			"Decide which it is. If the name is one token of an address, add it to segmentBearing "+
			"AND give its create path a ValidateSegment call (and a case in this package's create "+
			"map, so the behaviour is proved). If it is a namespaced key referenced from drivers, "+
			"templates, or expressions, add it to keyspaceOrOther with the reason.", table)
	}

	// The reverse direction: a classification naming a table that no longer exists
	// is stale, and stale entries are how a list stops describing the schema.
	live := map[string]bool{}
	for _, table := range named {
		live[table] = true
	}
	for table := range segmentBearing {
		if !live[table] {
			t.Errorf("segmentBearing lists %q, which has no `name` column in the generated schema", table)
		}
	}
	for table := range keyspaceOrOther {
		if !live[table] {
			t.Errorf("keyspaceOrOther lists %q, which has no `name` column in the generated schema", table)
		}
	}
}
