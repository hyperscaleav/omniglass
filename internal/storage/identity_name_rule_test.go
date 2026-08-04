package storage_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// ValidateName is the one entry point for "is this a legal name for this table".
// Before it, a caller picked the rule by hand: storage.ValidateEntityKey in ten
// places and key.ValidateKey in three, with nothing checking that the choice matched
// what the table actually is. Picking wrongly is silent in both directions, and
// forgetting to pick at all is how system_role shipped with no validation.
//
// So the table decides the rule, and the table's shape is already declared in
// identity_shape.go. These tests pin that the declaration is what selects, which is
// the whole point of the primitive: a new table is a failing test until somebody
// classifies it, rather than an unvalidated column nobody noticed.

// The load-bearing case. An unknown table must be an error and never a silent pass,
// because a silent pass is exactly the failure this replaces.
func TestValidateNameRefusesAnUndeclaredTable(t *testing.T) {
	err := storage.ValidateName("table_that_does_not_exist", "perfectly-legal")
	if err == nil {
		t.Fatal("an undeclared table accepted a name; a table nobody classified must not validate by default")
	}
	if !errors.Is(err, storage.ErrTableNotNameBearing) {
		t.Errorf("got %v, want ErrTableNotNameBearing", err)
	}
}

// Asking whether a name is legal for a table that has no name is a programming
// mistake, not a question with an answer. Returning nil would let a caller believe
// it had validated something.
func TestValidateNameRefusesATableThatBearsNoName(t *testing.T) {
	for _, table := range []string{
		"metric",      // id only
		"tag_binding", // id only
		"human",       // a username, its own rule
		"blob",        // content-addressed
	} {
		if err := storage.ValidateName(table, "anything"); !errors.Is(err, storage.ErrTableNotNameBearing) {
			t.Errorf("ValidateName(%q, ...) = %v, want ErrTableNotNameBearing", table, err)
		}
	}
}

// The one difference between the two rules that survived c973a19: a keyspace name is
// a dot-joined path (icmp.rtt-avg) and an entity name is a single segment. Both use
// the same character set for a segment.
func TestTheDotIsTheDifferenceBetweenTheTwoRules(t *testing.T) {
	if err := storage.ValidateName("component", "boi.17c.rm215a"); err == nil {
		t.Error("a key-bearing table accepted a dotted name; the dot separates segments in a topic, " +
			"so a name carrying one would split into two")
	}
	for _, table := range []string{"property_type", "event_type", "command_type"} {
		if err := storage.ValidateName(table, "icmp.rtt-avg"); err != nil {
			t.Errorf("ValidateName(%q, \"icmp.rtt-avg\") = %v, want nil", table, err)
		}
	}
}

// Everything both rules refuse, refused the same way whichever table asks. These are
// the nine the create sweep already drives through the gateway; here they are pinned
// as pure logic so a rule change is caught without a database.
func TestBothRulesRefuseTheSameIllegalShapes(t *testing.T) {
	illegal := map[string]string{
		"empty":            "",
		"uppercase":        "BadName",
		"whitespace":       "bad name",
		"leading hyphen":   "-leading",
		"underscore":       "bad_name",
		"slash":            "bad/name",
		"dollar":           "$comp",
		"nats wildcard":    "bad>name",
		"nats star":        "bad*name",
		"leading dot":      ".leading",
		"trailing dot":     "trailing.",
		"consecutive dots": "two..dots",
	}
	for _, table := range []string{"component", "property_type"} {
		for name, bad := range illegal {
			if err := storage.ValidateName(table, bad); err == nil {
				t.Errorf("ValidateName(%q, %q) accepted a %s", table, bad, name)
			}
		}
	}
}

// A name may not BE a uuid, on either rule. storage.ValidateEntityKey enforced this
// and key.ValidateKey did not, so a uuid-shaped property key was legal until now.
// The reason is the same for both: a reference resolves the uuid first, so a name
// that takes this shape makes the resolution a property of the data rather than of
// the request.
func TestNoRuleAcceptsAUUIDShapedName(t *testing.T) {
	const uuid = "019f8754-3a1b-7c2d-8e4f-5a6b7c8d9e0f"
	for _, table := range []string{"component", "system", "property_type", "event_type"} {
		if err := storage.ValidateName(table, uuid); err == nil {
			t.Errorf("ValidateName(%q, uuid) accepted a name shaped exactly like a uuid", table)
		}
	}
}

// The ceilings differ because a keyspace name is a path and an entity name is one
// segment of one. They are declared, not incidental.
func TestEachRuleHasItsOwnCeiling(t *testing.T) {
	entity := strings.Repeat("a", storage.MaxEntityNameLen)
	if err := storage.ValidateName("component", entity); err != nil {
		t.Errorf("a name at exactly the entity ceiling was refused: %v", err)
	}
	if err := storage.ValidateName("component", entity+"a"); err == nil {
		t.Error("a name one over the entity ceiling was accepted")
	}

	keyspace := strings.Repeat("a", storage.MaxKeyspaceNameLen)
	if err := storage.ValidateName("property_type", keyspace); err != nil {
		t.Errorf("a name at exactly the keyspace ceiling was refused: %v", err)
	}
	if err := storage.ValidateName("property_type", keyspace+"a"); err == nil {
		t.Error("a name one over the keyspace ceiling was accepted")
	}
}

// The ordinary case, so the refusals above are not passing for the wrong reason.
func TestValidateNameAcceptsRealNames(t *testing.T) {
	for table, name := range map[string]string{
		"component":     "hq-boardroom-dsp",
		"location":      "rm215a",
		"vendor":        "crestron",
		"node":          "node-1",
		"property_type": "interface.reachable",
		"event_type":    "call.started",
		"command_type":  "set-input",
		"tag":           "asset-id",
	} {
		if err := storage.ValidateName(table, name); err != nil {
			t.Errorf("ValidateName(%q, %q) = %v, want nil", table, name, err)
		}
	}
}
