package erd

// Subsystems is the table-to-subsystem map: the reading order of the ERD and the
// grouping that keeps a 40-plus-table diagram legible. It is hand-maintained on
// purpose; a table left out of it renders in the "unclustered" container and
// trips the ERD drift gate, which is the prompt to add it here. Every table in
// the live schema must be named here (the integration test enforces it).
var Subsystems = []Cluster{
	{Name: "identity", Tables: []string{
		"principal", "human", "service", "principal_grant", "principal_group",
		"principal_group_member", "impersonation_session",
		"role", "system_role", "system_role_assignment",
		"system_role_type", "system_role_product",
		"role_choice", "choice_alternate",
	}},
	{Name: "fleet", Tables: []string{
		"location", "location_type", "location_type_property", "location_type_metric",
		"system", "system_member",
		"component",
		"endpoint", "endpoint_live",
	}},
	{Name: "catalog", Tables: []string{
		"vendor", "product", "product_property", "product_metric",
		"component_type", "system_type", "driver", "standard", "standard_property", "standard_metric",
		// registry_shadow holds an operator's version of a shipped registry row
		// (#655, ADR-0095). It sits with the catalog because component_type is
		// its first adopter, though it is registry-agnostic by design and the
		// other registries carrying `official` join it here as they adopt.
		"registry_shadow",
	}},
	{Name: "telemetry", Tables: []string{
		"property_type", "metric_type", "metric", "property",
		"event_type", "event", "log_line", "alarm", "alarm_snooze", "severity_level", "event_rule", "event_rule_state", "event_rule_live",
		"event_rule_condition", "calc_rule", "calc_rule_input", "calc_pending", "calc_rule_state",
		"command_type", "command",
		// The outbound notification lane (#618): what a message says.
		"notification_type", "notification_target", "action_rule", "action_rule_target", "action_rule_opt_out", "notification",
		// telemetry_receipt is each ingest batch that landed (#311,
		// ADR-0311), held to a horizon: what makes a redelivered batch land
		// once.
		"telemetry_receipt",
	}},
	{Name: "collection", Tables: []string{
		"node", "task", "node_log",
		// sim_impairment is what an operator broke on a simulated device (#889),
		// applied by the simulator beside its node.
		"sim_impairment",
		// The webhook transport (#1024): a webhook endpoint's token binding,
		// and the inbox of senders no endpoint claims.
		"webhook_token", "unclaimed_source",
		// identity_binding ties a conduit component's reported identity to
		// the system or component it names (#1385, ADR-1282); unbound_identity
		// is the conduit's inbox of identities no binding places (#1388).
		"identity_binding", "unbound_identity",
	}},
	{Name: "config", Tables: []string{
		"setting_override", "variable",
		// label_rule is the global tier of the label rules (#682, ADR-0098),
		// one row per labelled entity kind. It sits with config rather than
		// with the fleet it labels: it is platform-wide policy an operator
		// tunes, not a row any entity points at.
		"label_rule",
		"secret", "secret_type", "credential",
		// lab_mark is the one row omniglass lab writes into its own database
		// (#1195), so a lab and a real deployment refuse each other's.
		"lab_mark",
	}},
	{Name: "content", Tables: []string{
		"blob", "file", "tag", "tag_binding",
	}},
	{Name: "audit", Tables: []string{
		"audit_log",
		// retired_sample keeps, for audit, each row the one-writer ruling
		// retired from the sample tables (ADR-1100); no read serves it.
		"retired_sample",
	}},
}
