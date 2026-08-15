package storage

import (
	"encoding/json"
	"sort"
)

// Identity across the platform is three things:
//
//	id            a uuid. immutable, the primary key and every foreign key target.
//	name          the human-readable machine identifier, what a URL, a CLI argument,
//	              and a topic carry. renameable.
//	label  an optional friendly string a human reads.
//
// The columns have always been called this. An earlier pass renamed the console's
// words instead and briefly called the identifier a "key", which is why some history
// reads that way.
//
// Every exception is declared here, with its reason, because an exception nobody
// wrote down is indistinguishable from an oversight. This is the single source: the
// guard in identity_shape_test.go checks it against the live schema, and identitygen
// renders it into the docs, so the prose cannot drift from the code the way a
// hand-written table would.

// IdentityShape names how a table is identified.
type IdentityShape string

const (
	// ShapeKeyBearing: an operator types its name, on the entity name rule
	// (^[a-z0-9][a-z0-9-]*$), enforced by ValidateName.
	ShapeKeyBearing IdentityShape = "key-bearing"

	// ShapeKeyspace: an operator types its name, on the same one kebab rule as
	// every other name (the dotted path rule retired with the one-name-rule
	// collapse, #586). The shape survives as a declaration of what the name is
	// for: estate-wide vocabulary a telemetry record is typed by (icmp-rtt-avg),
	// where an entity name addresses one row under a parent.
	ShapeKeyspace IdentityShape = "keyspace"

	// ShapeHumanNotAKey: it carries a human-readable identifier that is NOT a name in
	// the triad sense and must never acquire the name rule. Each looks name-shaped
	// from a distance: a username, a filename, a content hash.
	ShapeHumanNotAKey IdentityShape = "human identifier, not a name"

	// ShapeIDOnly: nobody names it, so it is addressed by uuid. A join row and a
	// telemetry row: an operator names the component, not the metric.
	ShapeIDOnly IdentityShape = "id only"

	// ShapeFixedKey: nobody names it AND there is no uuid either, because the
	// primary key is one of a closed vocabulary the PLATFORM declares and a
	// CHECK enforces. An operator neither types a new key nor addresses a row
	// by a surrogate: the key set is the schema.
	//
	// It is not ShapeIDOnly, which promises a uuid this table does not have,
	// and a generated reference publishes that promise. It is not
	// ShapeHumanNotAKey either: that shape is for an identifier a human
	// AUTHORS on its own rule (a username, a filename, a hash), and nobody
	// authors these.
	ShapeFixedKey IdentityShape = "fixed key, declared by the platform"
)

// TableIdentity is one table's declared shape and, where the shape alone does not
// explain it, the reason.
type TableIdentity struct {
	Shape  IdentityShape `json:"shape"`
	Reason string        `json:"reason,omitempty"`

	// NoLabel is why a table an operator NAMES carries no label, and it is the
	// half of the triad this declaration used to leave unsaid.
	//
	// A shape says what the identifier is. It said nothing about the friendly
	// string beside it, so four key-bearing tables (tag, variable, secret,
	// interface) went without one and nothing noticed until an operator had
	// nowhere to type the words they would actually say (#613). Every
	// name-bearing table is now expected to carry a label, and a table that does
	// not says so here, with its reason, exactly as the name exemptions in
	// KeyProvedElsewhere do.
	//
	// Empty means "carries one", and the guard checks that against the live
	// schema BOTH ways: a declared label that the schema lacks fails, and a
	// reason on a table that has the column fails as stale. A table nobody names
	// (ShapeIDOnly, ShapeFixedKey) is outside the rule and leaves this empty:
	// there is no name for an unset label to fall back to, so a label on one is
	// a defect of its own and the guard says so.
	NoLabel string `json:"no_label,omitempty"`
}

// NameBearing reports whether an operator (or, for the human-identifier shapes, a
// human of some kind) names a row of this table. It is what decides whether the
// label expectation applies: a label is the string an operator reads INSTEAD of
// the name, so a table with no name has nothing to render when it is unset.
func (s IdentityShape) NameBearing() bool {
	switch s {
	case ShapeKeyBearing, ShapeKeyspace, ShapeHumanNotAKey:
		return true
	}
	return false
}

// LabelledTables is every table declared to carry a label, sorted. It is the one
// list, read by the schema guard (label_column_migration_test.go) and by the
// unset-label sweep (label_unset_test.go), so a table gaining a label is
// declared once rather than added to three lists and forgotten in a fourth.
func LabelledTables() []string {
	out := make([]string, 0, len(IdentityShapes))
	for table, id := range IdentityShapes {
		if id.Shape.NameBearing() && id.NoLabel == "" {
			out = append(out, table)
		}
	}
	sort.Strings(out)
	return out
}

// IdentityShapes is every table in the schema, by shape. The guard fails on a table
// that is missing from it, so it stays complete by construction, and ValidateName
// reads it to pick the rule, so the declaration is load-bearing and not a comment.
var IdentityShapes = map[string]TableIdentity{
	// Key-bearing. The shape is the whole explanation.
	"component": {Shape: ShapeKeyBearing},
	"driver":    {Shape: ShapeKeyBearing}, "interface": {Shape: ShapeKeyBearing},
	"location":      {Shape: ShapeKeyBearing},
	"location_type": {Shape: ShapeKeyBearing}, "node": {Shape: ShapeKeyBearing},
	"principal_group": {Shape: ShapeKeyBearing}, "product": {Shape: ShapeKeyBearing},
	"role": {Shape: ShapeKeyBearing}, "secret": {Shape: ShapeKeyBearing},
	"secret_type": {Shape: ShapeKeyBearing}, "standard": {Shape: ShapeKeyBearing},
	"system": {Shape: ShapeKeyBearing}, "system_role": {Shape: ShapeKeyBearing},
	"tag": {Shape: ShapeKeyBearing}, "variable": {Shape: ShapeKeyBearing},
	"vendor": {Shape: ShapeKeyBearing}, "component_type": {Shape: ShapeKeyBearing},
	"system_type": {Shape: ShapeKeyBearing},
	// role_choice is named the same way system_role is: within its owner
	// arc (owner_kind, standard_id, system_id). choice_alternate is named
	// within its choice (choice_id, name), one level narrower, the same
	// relationship component_type's root/child split already has.
	"role_choice": {Shape: ShapeKeyBearing}, "choice_alternate": {Shape: ShapeKeyBearing},
	// The one type registry with no label, and the only key-bearing table left
	// without one after #613. It is not an oversight and not a gap to fill: the
	// table retires with the interface.type FK (ADR-0073), so a column added
	// here would be dropped with the table it sits on.
	"interface_type": {Shape: ShapeKeyBearing,
		NoLabel: "retires with the interface.type FK (ADR-0073); a label added here would be " +
			"dropped with the table"},

	// Keyspace: a name, on the other rule.
	"property_type": {Shape: ShapeKeyspace, Reason: "serial-number, a signal name referenced from drivers and templates"},
	"metric_type":   {Shape: ShapeKeyspace, Reason: "icmp-rtt-avg, a numeric-series name referenced from drivers and templates"},
	"event_type":    {Shape: ShapeKeyspace, Reason: "call-started, an occurrence name"},
	"command_type":  {Shape: ShapeKeyspace, Reason: "set-input, a command name"},

	// A human identifier that is not a name. These are the exceptions worth stating.
	"human": {Shape: ShapeHumanNotAKey, Reason: "a username: its own rule, its own uniqueness, and not an " +
		"address, since a principal is addressed by uuid"},
	"service": {Shape: ShapeHumanNotAKey, Reason: "the username analogue for kind=service: unique like one " +
		"(#563), on its own rule rather than the entity name rule, and not an address, since a " +
		"principal is addressed by uuid",
		NoLabel: "the identifier IS what every surface shows: principalIdent resolves a service " +
			"principal to service.name, and it cannot resolve to a label, which is optional and not unique"},
	"file": {Shape: ShapeHumanNotAKey, Reason: "a filename with an extension (codec-firmware-2.1.4.txt): not " +
		"unique, already the label, and addressed by uuid",
		NoLabel: "its name is already the label, so a second string would be a second label rather " +
			"than the identifier a label sits beside (#613 out of scope, D1 deferred to #755)"},
	"task": {Shape: ShapeHumanNotAKey, Reason: "content-addressed, the id IS hash(interface, kind, schedule, " +
		"params), so a name would be a second identity for the same row"},
	"blob": {Shape: ShapeHumanNotAKey, Reason: "content-addressed by sha256, for the same reason",
		NoLabel: "no operator surface of its own: a blob is reached through the file that references " +
			"it, and nothing renders a blob row for a label to appear on"},

	// Id only.
	"alarm":     {Shape: ShapeIDOnly},
	"audit_log": {Shape: ShapeIDOnly}, "command": {Shape: ShapeIDOnly},
	"credential": {Shape: ShapeIDOnly},
	"event":      {Shape: ShapeIDOnly}, "impersonation_session": {Shape: ShapeIDOnly},
	"location_type_metric": {Shape: ShapeIDOnly}, "location_type_property": {Shape: ShapeIDOnly},
	"log_line": {Shape: ShapeIDOnly}, "metric": {Shape: ShapeIDOnly},
	"node_log":  {Shape: ShapeIDOnly},
	"principal": {Shape: ShapeIDOnly}, "principal_grant": {Shape: ShapeIDOnly},
	"principal_group_member": {Shape: ShapeIDOnly},
	"product_metric":         {Shape: ShapeIDOnly}, "product_property": {Shape: ShapeIDOnly},
	"property":         {Shape: ShapeIDOnly},
	"setting_override": {Shape: ShapeIDOnly}, "standard_metric": {Shape: ShapeIDOnly},
	"standard_property": {Shape: ShapeIDOnly}, "system_member": {Shape: ShapeIDOnly},
	"system_role_assignment": {Shape: ShapeIDOnly},
	"system_role_product":    {Shape: ShapeIDOnly}, "system_role_type": {Shape: ShapeIDOnly},
	"tag_binding": {Shape: ShapeIDOnly},
	// registry_shadow holds an operator's version of a shipped registry row
	// (#655, ADR-0095). Nobody names it: it is addressed by the row it shadows,
	// (registry, row_id), and row_id is that row's uuid. Operators never see it
	// at all, since every read resolves it over the official row.
	"registry_shadow": {Shape: ShapeIDOnly, Reason: "addressed by the registry row it shadows, never named"},
	// label_rule holds the global tier of the label rules (#682, ADR-0098), one
	// row per labelled entity kind, and it is the only table in the schema with
	// a text primary key and no uuid at all.
	"label_rule": {Shape: ShapeFixedKey, Reason: "keyed by the entity kind it applies to (component, system, location), a closed set the table's own CHECK enforces"},
}

// KeyProvedElsewhere excuses a name-bearing table from the behavioural create sweep,
// with the reason it cannot be reached that way. Classification without proof is a
// claim and not a guard, so the excuse is written down rather than inferred.
var KeyProvedElsewhere = map[string]string{
	"interface_type":   "seeded only, no create path on the gateway",
	"role":             "seeded only, no create path on the gateway",
	"secret_type":      "seeded only, no create path on the gateway",
	"role_choice":      "seeded only, no create path on the gateway",
	"choice_alternate": "seeded only, no create path on the gateway",
	"interface": "the name is server-derived, not operator-typed: InterfaceSpec carries no Name " +
		"and the column is set from spec.Type, an already-validated interface_type name",
}

// IdentityShapesJSON renders the declaration for the docs, the same way
// config.FactsJSON renders the env-var reference. The docs then have no hand-written
// list of which table is which, which is the class of artifact that always drifts.
func IdentityShapesJSON() ([]byte, error) {
	type row struct {
		Table          string `json:"table"`
		Shape          string `json:"shape"`
		Reason         string `json:"reason,omitempty"`
		ProvedElsewhen string `json:"proved_elsewhere,omitempty"`
		// Labelled is the third column of the triad, published so the docs can
		// render which tables carry the friendly string rather than restate it.
		// A table nobody names is false with no reason: the shape is the reason.
		Labelled bool   `json:"labelled"`
		NoLabel  string `json:"no_label,omitempty"`
	}
	out := make([]row, 0, len(IdentityShapes))
	for table, id := range IdentityShapes {
		out = append(out, row{
			Table:          table,
			Shape:          string(id.Shape),
			Reason:         id.Reason,
			ProvedElsewhen: KeyProvedElsewhere[table],
			Labelled:       id.Shape.NameBearing() && id.NoLabel == "",
			NoLabel:        id.NoLabel,
		})
	}
	// Sorted so a regeneration is a no-op unless a fact changed.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Table < out[j-1].Table; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return json.MarshalIndent(map[string]any{
		"//":     "generated by make gen (cmd/identitygen); do not edit",
		"tables": out,
	}, "", "  ")
}
