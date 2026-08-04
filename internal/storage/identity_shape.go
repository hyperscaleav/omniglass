package storage

import "encoding/json"

// Identity across the platform is three things:
//
//	id    a uuid. immutable, the primary key and every foreign key target.
//	key   the human-readable machine identifier, what a URL, a CLI argument, and a
//	      topic carry. mutable. today the column is `name`.
//	name  an optional free-text label. today the column is `display_name`.
//
// Every exception is declared here, with its reason, because an exception nobody
// wrote down is indistinguishable from an oversight. This is the single source: the
// guard in identity_shape_test.go checks it against the live schema, and identitygen
// renders it into the docs, so the prose cannot drift from the code the way a
// hand-written table would.

// IdentityShape names how a table is identified.
type IdentityShape string

const (
	// ShapeKeyBearing: an operator types its key, on the entity-key rule
	// (^[a-z0-9][a-z0-9-]*$), enforced by ValidateEntityKey.
	ShapeKeyBearing IdentityShape = "key-bearing"

	// ShapeKeyspace: an operator types its key, but on the other key rule, the one
	// internal/key owns (lowercase snake_case, optionally dot-hierarchied). The two
	// are not merged because each legitimately carries a character the other forbids.
	ShapeKeyspace IdentityShape = "keyspace"

	// ShapeHumanNotAKey: it carries a human-readable identifier that is NOT a key and
	// must never acquire the key rule. Each of these looks key-shaped from a distance.
	ShapeHumanNotAKey IdentityShape = "human identifier, not a key"

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
// that is missing from it or declared twice, so it stays complete by construction.
var IdentityShapes = map[string]TableIdentity{
	// Key-bearing. The shape is the whole explanation.
	"capability": {Shape: ShapeKeyBearing}, "component": {Shape: ShapeKeyBearing},
	"driver": {Shape: ShapeKeyBearing}, "interface": {Shape: ShapeKeyBearing},
	"interface_type": {Shape: ShapeKeyBearing}, "location": {Shape: ShapeKeyBearing},
	"location_type": {Shape: ShapeKeyBearing}, "node": {Shape: ShapeKeyBearing},
	"principal_group": {Shape: ShapeKeyBearing}, "product": {Shape: ShapeKeyBearing},
	"role": {Shape: ShapeKeyBearing}, "secret_type": {Shape: ShapeKeyBearing},
	"standard": {Shape: ShapeKeyBearing}, "system": {Shape: ShapeKeyBearing},
	"system_role": {Shape: ShapeKeyBearing}, "vendor": {Shape: ShapeKeyBearing},

	// Keyspace: a key, on the other rule.
	"property_type": {ShapeKeyspace, "icmp.rtt-avg, a signal key referenced from drivers and templates"},
	"event_type":    {ShapeKeyspace, "call.started, an occurrence key"},
	"command_type":  {ShapeKeyspace, "set-input, a command key"},
	"tag":           {ShapeKeyspace, "asset-id, a tag key; the console has always called it a key"},
	"variable":      {ShapeKeyspace, "poll-interval, a cascade key referenced from expressions"},
	"secret":        {ShapeKeyspace, "og-session, a cascade key referenced from expressions"},

	// A human identifier that is not a key. These are the exceptions worth stating.
	"human": {ShapeHumanNotAKey, "a username: its own rule, its own uniqueness, and not an " +
		"address, since a principal is addressed by uuid"},
	"file": {ShapeHumanNotAKey, "a filename with an extension (codec-firmware-2.1.4.txt): not " +
		"unique, already the label, and addressed by uuid"},
	"task": {ShapeHumanNotAKey, "content-addressed, the id IS hash(interface, kind, schedule, " +
		"params), so a key would be a second identity for the same row"},
	"blob": {ShapeHumanNotAKey, "content-addressed by sha256, for the same reason"},

	// Id only.
	"alarm": {Shape: ShapeIDOnly}, "alarm_capability": {Shape: ShapeIDOnly},
	"audit_log": {Shape: ShapeIDOnly}, "command": {Shape: ShapeIDOnly},
	"component_capability": {Shape: ShapeIDOnly}, "credential": {Shape: ShapeIDOnly},
	"event": {Shape: ShapeIDOnly}, "impersonation_session": {Shape: ShapeIDOnly},
	"location_type_property": {Shape: ShapeIDOnly}, "log_line": {Shape: ShapeIDOnly},
	"metric": {Shape: ShapeIDOnly}, "principal": {Shape: ShapeIDOnly},
	"principal_grant": {Shape: ShapeIDOnly}, "principal_group_member": {Shape: ShapeIDOnly},
	"product_capability": {Shape: ShapeIDOnly}, "product_property": {Shape: ShapeIDOnly},
	"property": {Shape: ShapeIDOnly}, "service": {Shape: ShapeIDOnly},
	"setting_override": {Shape: ShapeIDOnly}, "standard_property": {Shape: ShapeIDOnly},
	"state": {Shape: ShapeIDOnly}, "system_member": {Shape: ShapeIDOnly},
	"system_role_assignment": {Shape: ShapeIDOnly}, "system_role_capability": {Shape: ShapeIDOnly},
	"tag_binding": {Shape: ShapeIDOnly},
}

// KeyProvedElsewhere excuses a key-bearing table from the behavioural create sweep,
// with the reason it cannot be reached that way. Classification without proof is a
// claim and not a guard, so the excuse is written down rather than inferred.
var KeyProvedElsewhere = map[string]string{
	"interface_type": "seeded only, no create path on the gateway",
	"role":           "seeded only, no create path on the gateway",
	"secret_type":    "seeded only, no create path on the gateway",
	"interface": "the key is server-derived, not operator-typed: InterfaceSpec carries no Name " +
		"and the column is set from spec.Type, an already-validated interface_type key",
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
