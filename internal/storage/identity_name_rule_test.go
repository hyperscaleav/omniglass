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

// There is ONE name rule, and a dot is illegal everywhere (#586). There used to be
// two: an entity name was a single segment and a keyspace name was a dot-joined path
// of them, selected by the table's declared shape. The dotted rule is gone, so a name
// is always exactly one token and `icmp.rtt-avg` is now `icmp-rtt-avg`.
func TestNoTableAcceptsADottedName(t *testing.T) {
	for _, table := range []string{"component", "property_type", "event_type", "command_type"} {
		if err := storage.ValidateName(table, "icmp.rtt-avg"); err == nil {
			t.Errorf("ValidateName(%q, \"icmp.rtt-avg\") accepted a dotted name; there is one rule and it has no dots", table)
		}
	}
}

// The kebab form of every name that used to carry a dot is legal, so the backfill has
// somewhere to land.
func TestTheKebabFormOfAFormerlyDottedNameIsLegal(t *testing.T) {
	for _, table := range []string{"property_type", "event_type", "command_type"} {
		for _, name := range []string{"icmp-rtt-avg", "call-started", "video-input", "command-issued"} {
			if err := storage.ValidateName(table, name); err != nil {
				t.Errorf("ValidateName(%q, %q) = %v, want nil", table, name, err)
			}
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

// One rule, one ceiling. The keyspace ceiling of 128 existed because that name was a
// path; with no paths there is one number.
func TestOneCeilingAppliesToEveryTable(t *testing.T) {
	atMax := strings.Repeat("a", storage.MaxEntityNameLen)
	for _, table := range []string{"component", "property_type", "event_type", "command_type"} {
		if err := storage.ValidateName(table, atMax); err != nil {
			t.Errorf("ValidateName(%q, ...) refused a name at exactly the ceiling: %v", table, err)
		}
		if err := storage.ValidateName(table, atMax+"a"); err == nil {
			t.Errorf("ValidateName(%q, ...) accepted a name one over the ceiling", table)
		}
	}
}

// The ordinary case, so the refusals above are not passing for the wrong reason.
func TestValidateNameAcceptsRealNames(t *testing.T) {
	for table, name := range map[string]string{
		"component":     "hq-boardroom-dsp",
		"location":      "rm215a",
		"vendor":        "crestron",
		"node":          "node-1",
		"property_type": "endpoint-reachable",
		"event_type":    "call-started",
		"command_type":  "set-input",
		"tag":           "asset-id",
	} {
		if err := storage.ValidateName(table, name); err != nil {
			t.Errorf("ValidateName(%q, %q) = %v, want nil", table, name, err)
		}
	}
}
