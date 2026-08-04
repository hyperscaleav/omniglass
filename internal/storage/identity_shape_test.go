package storage_test

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// Identity across the platform is three things:
//
//	id    a uuid. immutable, the primary key and every foreign key target.
//	key   the human-readable machine identifier, what a URL, a CLI argument, and a
//	      topic carry. mutable. today the column is `name`.
//	name  an optional free-text label. today the column is `display_name`.
//
// Every exception to that is named here, explicitly, with its reason. That is the
// whole point of this file: an exception nobody wrote down is indistinguishable from
// an oversight, and six tables went unvalidated for exactly as long as the only
// record of which tables carry a key was a scatter of hand-written call sites.
//
// The guard reads the generated schema (docs/src/generated/schema.json, emitted by
// cmd/erdgen, so it cannot drift from the live DDL) and requires EVERY table to sit
// in exactly one shape below. A new table is a compile-clean, review-clean, FAILING
// test until somebody classifies it.
//
// An earlier version only looked at tables carrying a `name` column, which made it
// blind to 28 of the 51: a table identified by a username or a content hash escaped
// it entirely. Absence of a `name` is not evidence of absence of an identifier.
var (
	// keyBearing: carries a key an operator reads and types, on the entity-key rule
	// (`^[a-z0-9][a-z0-9-]*$`). TestEveryKeyBearingTableIsProved requires each of
	// these to be driven through the gateway by the create sweep, or excused in
	// provedElsewhere with a reason.
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

	// keyspace: carries a key, but on the OTHER key rule, the one internal/key owns:
	// lowercase snake_case, optionally dot-hierarchied. The two rules are not merged
	// because each legitimately carries a character the other forbids, and running
	// the entity-key rule over these would reject data the seed corpus ships today.
	// The console still says Key for both, because an operator types one identifier
	// either way and the differing character set surfaces as a validation message
	// rather than as a second word.
	keyspace = map[string]string{
		"property_type": "icmp.rtt_avg, a signal key referenced from drivers and templates",
		"event_type":    "call.started, an occurrence key",
		"command_type":  "set_input, a command key",
		"tag":           "asset_id, a tag key; the console has always called it a key",
		"variable":      "poll_interval, a cascade key referenced from expressions",
		"secret":        "og_session, a cascade key referenced from expressions",
	}

	// humanIdentifierNotAKey: carries a human-readable identifier that is NOT a key
	// and must never acquire the key rule. These are the exceptions worth stating out
	// loud, because every one of them looks key-shaped from a distance.
	humanIdentifierNotAKey = map[string]string{
		"human": "a username. its own rule, its own uniqueness, and not an address: a " +
			"principal is addressed by uuid",
		"file": "a filename with an extension (codec-firmware-2.1.4.txt). not unique, " +
			"already the label, and addressed by uuid",
		"task": "content-addressed. the id IS hash(interface, kind, schedule, params), so a " +
			"key would be a second identity for the same row",
		"blob": "content-addressed by sha256, for the same reason",
	}

	// idOnly: no human identifier at all, and correctly so. A join row and a
	// telemetry row are addressed by uuid because nobody names them: an operator
	// names the component, not the metric.
	idOnly = map[string]bool{
		"alarm": true, "alarm_capability": true, "audit_log": true, "command": true,
		"component_capability": true, "credential": true, "event": true,
		"impersonation_session": true, "location_type_property": true, "log_line": true,
		"metric": true, "principal": true, "principal_grant": true,
		"principal_group_member": true, "product_capability": true, "product_property": true,
		"property": true, "service": true, "setting_override": true, "standard_property": true,
		"state": true, "system_member": true, "system_role_assignment": true,
		"system_role_capability": true, "tag_binding": true,
	}
)

// provedElsewhere excuses a keyBearing table from the behavioural create sweep, with
// the reason it cannot be reached that way. Classification without proof is a claim
// and not a guard, so the excuse is written down rather than inferred from a gap.
var provedElsewhere = map[string]string{
	"interface_type": "seeded only, no create path on the gateway",
	"role":           "seeded only, no create path on the gateway",
	"secret_type":    "seeded only, no create path on the gateway",
	"interface": "the key is server-derived, not operator-typed: InterfaceSpec carries no Name " +
		"and the column is set from spec.Type, an already-validated interface_type key",
	"principal_group": "validated at the API layer with a looser pattern (^[a-z0-9][a-z0-9._-]*$), " +
		"which admits the . and _ the address grammar reads as separators; tightening it is a " +
		"behaviour change for existing groups, tracked with the rename work",
}

// shapes returns every shape a table is declared in. More than one is worse than
// none: it reads as classified while the entries disagree about what the table is.
func shapes(table string) []string {
	var in []string
	if keyBearing[table] {
		in = append(in, "keyBearing")
	}
	if _, ok := keyspace[table]; ok {
		in = append(in, "keyspace")
	}
	if _, ok := humanIdentifierNotAKey[table]; ok {
		in = append(in, "humanIdentifierNotAKey")
	}
	if idOnly[table] {
		in = append(in, "idOnly")
	}
	return in
}

func TestEveryTableHasADeclaredIdentityShape(t *testing.T) {
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

	live := map[string]bool{}
	for _, tables := range doc.Subsystems {
		for table := range tables {
			live[table] = true
		}
	}
	if len(live) == 0 {
		t.Fatal("found no tables, which means this guard is not reading the schema it thinks it is")
	}

	var unclassified []string
	for table := range live {
		switch in := shapes(table); len(in) {
		case 1:
			// the only correct case
		case 0:
			unclassified = append(unclassified, table)
		default:
			t.Errorf("%q is declared in %v; a table has exactly one identity shape", table, in)
		}
	}
	sort.Strings(unclassified)
	for _, table := range unclassified {
		t.Errorf("%q has no declared identity shape.\n"+
			"Pick one and say why:\n"+
			"  keyBearing              an operator types its key, on the entity-key rule\n"+
			"  keyspace                an operator types its key, on internal/key's rule\n"+
			"  humanIdentifierNotAKey  it has a human identifier that is not a key (a username,\n"+
			"                          a filename, a content hash), with the reason\n"+
			"  idOnly                  nobody names it; it is addressed by uuid", table)
	}

	// A classification naming a table that no longer exists is stale, and a stale
	// entry is how a list stops describing the schema.
	for _, m := range []map[string]bool{keyBearing, idOnly} {
		for table := range m {
			if !live[table] {
				t.Errorf("a shape lists %q, which is not a table in the generated schema", table)
			}
		}
	}
	for _, m := range []map[string]string{keyspace, humanIdentifierNotAKey, provedElsewhere} {
		for table := range m {
			if !live[table] {
				t.Errorf("a shape lists %q, which is not a table in the generated schema", table)
			}
		}
	}
}

// TestEveryKeyBearingTableIsProved is the cross-check between classification and
// behaviour: a table declared keyBearing is either driven through the gateway by the
// create sweep or excused above, and adding one without doing either fails the build.
//
// This exists because the two used to be independent lists. Sixteen tables were
// classified, ten were proved, and nothing noticed the six in between.
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
				"proved, or name it in provedElsewhere with the reason it cannot be.", table)
		}
	}
	for table := range provedElsewhere {
		if !keyBearing[table] {
			t.Errorf("provedElsewhere names %q, which is not classified key-bearing", table)
		}
	}
}
