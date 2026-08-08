package storage

import "encoding/json"

// Identity across the platform is three things:
//
//	id            a uuid. immutable, the primary key and every foreign key target.
//	name          the human-readable machine identifier, what a URL, a CLI argument,
//	              and a topic carry. renameable.
//	display_name  an optional friendly string a human reads.
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
)

// TableIdentity is one table's declared shape and, where the shape alone does not
// explain it, the reason.
type TableIdentity struct {
	Shape  IdentityShape `json:"shape"`
	Reason string        `json:"reason,omitempty"`
}

// IdentityShapes is every table in the schema, by shape. The guard fails on a table
// that is missing from it, so it stays complete by construction, and ValidateName
// reads it to pick the rule, so the declaration is load-bearing and not a comment.
var IdentityShapes = map[string]TableIdentity{
	// Key-bearing. The shape is the whole explanation.
	"component": {Shape: ShapeKeyBearing},
	"driver":    {Shape: ShapeKeyBearing}, "interface": {Shape: ShapeKeyBearing},
	"interface_type": {Shape: ShapeKeyBearing}, "location": {Shape: ShapeKeyBearing},
	"location_type": {Shape: ShapeKeyBearing}, "node": {Shape: ShapeKeyBearing},
	"principal_group": {Shape: ShapeKeyBearing}, "product": {Shape: ShapeKeyBearing},
	"role": {Shape: ShapeKeyBearing}, "secret": {Shape: ShapeKeyBearing},
	"secret_type": {Shape: ShapeKeyBearing}, "standard": {Shape: ShapeKeyBearing},
	"system": {Shape: ShapeKeyBearing}, "system_role": {Shape: ShapeKeyBearing},
	"tag": {Shape: ShapeKeyBearing}, "variable": {Shape: ShapeKeyBearing},
	"vendor": {Shape: ShapeKeyBearing}, "component_type": {Shape: ShapeKeyBearing},
	// role_choice is named the same way system_role is: within its owner
	// arc (owner_kind, standard_id, system_id). choice_alternate is named
	// within its choice (choice_id, name), one level narrower, the same
	// relationship component_type's root/child split already has.
	"role_choice": {Shape: ShapeKeyBearing}, "choice_alternate": {Shape: ShapeKeyBearing},

	// Keyspace: a name, on the other rule.
	"property_type": {ShapeKeyspace, "serial-number, a signal name referenced from drivers and templates"},
	"metric_type":   {ShapeKeyspace, "icmp-rtt-avg, a numeric-series name referenced from drivers and templates"},
	"event_type":    {ShapeKeyspace, "call-started, an occurrence name"},
	"command_type":  {ShapeKeyspace, "set-input, a command name"},

	// A human identifier that is not a name. These are the exceptions worth stating.
	"human": {ShapeHumanNotAKey, "a username: its own rule, its own uniqueness, and not an " +
		"address, since a principal is addressed by uuid"},
	"file": {ShapeHumanNotAKey, "a filename with an extension (codec-firmware-2.1.4.txt): not " +
		"unique, already the label, and addressed by uuid"},
	"task": {ShapeHumanNotAKey, "content-addressed, the id IS hash(interface, kind, schedule, " +
		"params), so a name would be a second identity for the same row"},
	"blob": {ShapeHumanNotAKey, "content-addressed by sha256, for the same reason"},

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
	"property": {Shape: ShapeIDOnly}, "service": {Shape: ShapeIDOnly},
	"setting_override": {Shape: ShapeIDOnly}, "standard_metric": {Shape: ShapeIDOnly},
	"standard_property": {Shape: ShapeIDOnly}, "system_member": {Shape: ShapeIDOnly},
	"system_role_assignment": {Shape: ShapeIDOnly},
	"system_role_product":    {Shape: ShapeIDOnly}, "system_role_type": {Shape: ShapeIDOnly},
	"tag_binding": {Shape: ShapeIDOnly},
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
	}
	out := make([]row, 0, len(IdentityShapes))
	for table, id := range IdentityShapes {
		out = append(out, row{
			Table:          table,
			Shape:          string(id.Shape),
			Reason:         id.Reason,
			ProvedElsewhen: KeyProvedElsewhere[table],
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
