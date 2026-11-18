package erd

// Subsystems is the table-to-subsystem map: the reading order of the ERD and the
// grouping that keeps a 40-plus-table diagram legible. It is hand-maintained on
// purpose; a table left out of it renders in the "unclustered" container and
// trips the ERD drift gate, which is the prompt to add it here. Every table in
// the live schema must be named here (the integration test enforces it).
var Subsystems = []Cluster{
	{Name: "identity", Tables: []string{
		"users", "person", "machine", "user_grant", "user_group",
		"user_group_member", "impersonation_session",
		"role", "system_slot", "system_slot_assignment",
		"system_slot_type", "system_slot_product",
		"slot_choice", "choice_alternate",
	}},
	{Name: "fleet", Tables: []string{
		"location", "location_type", "location_type_property", "location_type_metric",
		"system", "system_member",
		"component", "component_dependency",
		"endpoint", "endpoint_live",
	}},
	{Name: "catalog", Tables: []string{
		"vendor", "product", "product_property", "product_metric",
		"component_type", "system_type", "driver", "standard", "standard_property", "standard_metric",
		// catalog_shadow holds an operator's version of a shipped catalog row
		// (#655, ADR-0095). It sits with the catalog because component_type is
		// its first adopter, though it is catalog-agnostic by design and the
		// other catalogs carrying `official` join it here as they adopt.
		"catalog_shadow",
	}},
	{Name: "telemetry", Tables: []string{
		"property_type", "metric_type", "metric", "metric_series", "metric_declared", "metric_trend", "metric_trend_dirty", "metric_trend_mark", "metric_block", "metric_block_mark", "property",
		"event_type", "event", "log_line", "alarm", "alarm_snooze", "severity_level", "event_rule", "event_rule_state", "event_rule_live",
		"event_rule_firing", "calc_rule", "calc_rule_input", "calc_pending", "calc_rule_state",
		"command_type", "command",
		// The outbound notification lane (#618): what a message says.
		"notification_type", "notification_target", "action_rule", "action_rule_target", "action_rule_opt_out", "notification",
		// telemetry_receipt is each ingest batch that landed (#311,
		// ADR-0311), held to a horizon: what makes a redelivered batch land
		// once.
		"telemetry_receipt",
		// reserved_key mirrors the keys writerclass reserves (#1447), which
		// the signal catalogs' search path reads.
		"reserved_key",
	}},
	{Name: "collection", Tables: []string{
		"outpost", "task", "outpost_log",
		// platform_server maps a server's identity to the platform component
		// its own measures land on (#1627), as an outpost's row does an outpost's.
		"platform_server",
		// sim_impairment is what an operator broke on a simulated device (#889),
		// applied by the simulator beside its outpost.
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
		"setting_binding", "variable",
		// label_rule is the global tier of the label rules (#682, ADR-0098),
		// one row per labelled entity kind. It sits with config rather than
		// with the fleet it labels: it is platform-wide policy an operator
		// tunes, not a row any entity points at.
		"label_rule",
		"secret", "secret_type", "credential",
		// oauth_client is an MCP client that registered itself and
		// oauth_code its code between consent and exchange (#2048); a
		// token it earns is a credential row linked to the client, which
		// oauth_refresh renews (#2073).
		"oauth_client", "oauth_code", "oauth_refresh",
		// assistant_conversation is a person's stored omnibot conversation
		// and assistant_message its transcript (#2113), read by their owner
		// alone.
		"assistant_conversation", "assistant_message",
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
