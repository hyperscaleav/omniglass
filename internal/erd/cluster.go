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
		"role", "system_role", "system_role_assignment", "system_role_capability",
		"capability",
	}},
	{Name: "estate", Tables: []string{
		"location", "location_type", "location_type_property",
		"system", "system_member",
		"component", "component_capability",
		"interface", "interface_type",
	}},
	{Name: "catalog", Tables: []string{
		"vendor", "product", "product_capability", "product_property",
		"driver", "standard", "standard_property",
	}},
	{Name: "telemetry", Tables: []string{
		"property_type", "property", "metric", "state",
		"event_type", "event", "log_line", "alarm", "alarm_capability",
		"command_type", "command",
	}},
	{Name: "collection", Tables: []string{
		"node", "task",
	}},
	{Name: "config", Tables: []string{
		"platform_setting", "setting_override", "variable",
		"secret", "secret_type", "credential",
	}},
	{Name: "content", Tables: []string{
		"blob", "file", "tag", "tag_binding",
	}},
	{Name: "audit", Tables: []string{
		"audit_log",
	}},
}
