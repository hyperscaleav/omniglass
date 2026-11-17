package settings

import "testing"

func TestAgentDefaultsLeaveEveryClassUnrefused(t *testing.T) {
	d := Defaults()["agent"]
	want := map[string]any{"read": "always", "write": "ask", "destructive": "ask", "credential_destination": "ask"}
	for k, v := range want {
		if d[k] != v {
			t.Errorf("agent.%s default = %v, want %v", k, d[k], v)
		}
	}
	for _, ns := range Namespaces() {
		if ns.Name == "agent" && (ns.Domain != "platform" || ns.ClientVisible) {
			t.Errorf("agent namespace = %+v, want platform and not client-visible", ns)
		}
	}
}

func TestAgentPolicyNamesEachClass(t *testing.T) {
	a := AgentSettings{Read: "always", Write: "never", Destructive: "ask", CredentialDestination: "never"}
	for class, want := range map[string]string{"read": "always", "write": "never", "destructive": "ask", "credential_destination": "never"} {
		if got := a.Policy(class); got != want {
			t.Errorf("Policy(%q) = %q, want %q", class, got, want)
		}
	}
}

func TestAgentPolicyFallsBackToTheDefault(t *testing.T) {
	var a AgentSettings
	if got := a.Policy("read"); got != AgentAlways {
		t.Errorf("zero Policy(read) = %q, want always", got)
	}
	if got := a.Policy("credential_destination"); got != AgentAsk {
		t.Errorf("zero Policy(credential_destination) = %q, want ask", got)
	}
	if got := a.Policy("bogus"); got != AgentAsk {
		t.Errorf("Policy(bogus) = %q, want ask", got)
	}
}

func TestAgentPatchRefusesAnUnknownPolicy(t *testing.T) {
	if err := Validate("agent", map[string]any{"write": "sometimes"}); err == nil {
		t.Fatal("agent.write=sometimes validated, want an enum error")
	}
	if err := Validate("agent", map[string]any{"write": "never"}); err != nil {
		t.Fatalf("agent.write=never: %v", err)
	}
}

func TestAgentPatchTakesRoleAndToolOverrides(t *testing.T) {
	ok := map[string]any{
		"roles": map[string]any{"operator": map[string]any{"write": "never", "destructive": nil}},
		"tools": map[string]any{"delete-component": "never", "list-components": nil},
	}
	if err := Validate("agent", ok); err != nil {
		t.Fatalf("overrides with nested deletes: %v", err)
	}
	for name, bad := range map[string]map[string]any{
		"bad role policy":   {"roles": map[string]any{"operator": map[string]any{"write": "sometimes"}}},
		"unknown role key":  {"roles": map[string]any{"operator": map[string]any{"writes": "never"}}},
		"bad tool policy":   {"tools": map[string]any{"delete-component": "sometimes"}},
		"tool not a string": {"tools": map[string]any{"delete-component": 3}},
	} {
		if err := Validate("agent", bad); err == nil {
			t.Errorf("%s validated, want a field error", name)
		}
	}
}

func TestAgentResolveTakesTheStrictestHeldRole(t *testing.T) {
	a := AgentSettings{
		Write: "ask",
		Roles: map[string]AgentClassOverride{
			"operator": {Write: "never"},
			"viewer":   {Write: "always", Read: "ask"},
		},
	}
	if d := a.ResolveClass("write", []string{"viewer"}); d != (AgentDecision{Policy: "ask", Source: AgentSourcePlatform}) {
		t.Errorf("viewer write = %+v, want the platform's ask (a role never widens)", d)
	}
	if d := a.ResolveClass("write", []string{"viewer", "operator"}); d != (AgentDecision{Policy: "never", Source: AgentSourceRole, Role: "operator"}) {
		t.Errorf("viewer+operator write = %+v, want never from operator", d)
	}
	if d := a.ResolveClass("write", nil); d != (AgentDecision{Policy: "ask", Source: AgentSourcePlatform}) {
		t.Errorf("no roles write = %+v, want the platform's ask", d)
	}
	if d := a.ResolveClass("read", []string{"viewer", "viewer"}); d != (AgentDecision{Policy: "ask", Source: AgentSourceRole, Role: "viewer"}) {
		t.Errorf("viewer read = %+v, want ask from viewer", d)
	}
}

func TestAgentResolveRoleTieNamesTheFirstRoleByName(t *testing.T) {
	a := AgentSettings{Roles: map[string]AgentClassOverride{"zeta": {Write: "never"}, "alpha": {Write: "never"}}}
	if d := a.ResolveClass("write", []string{"zeta", "alpha"}); d.Role != "alpha" {
		t.Errorf("tie = %+v, want alpha", d)
	}
}

func TestAgentToolOverrideNarrowsButNeverWidens(t *testing.T) {
	a := AgentSettings{
		Write: "ask",
		Roles: map[string]AgentClassOverride{"operator": {Write: "never"}},
		Tools: map[string]string{"create-component": "never", "create-location": "always", "create-system": "ask"},
	}
	if d := a.ResolveTool("create-component", "write", nil); d != (AgentDecision{Policy: "never", Source: AgentSourceTool}) {
		t.Errorf("create-component = %+v, want never from the tool", d)
	}
	if d := a.ResolveTool("create-location", "write", nil); d != (AgentDecision{Policy: "ask", Source: AgentSourcePlatform}) {
		t.Errorf("create-location = %+v, want the platform's ask (a tool never widens)", d)
	}
	if d := a.ResolveTool("create-system", "write", nil); d.Source != AgentSourcePlatform {
		t.Errorf("create-system = %+v, want the platform to keep a tie", d)
	}
	if d := a.ResolveTool("create-component", "write", []string{"operator"}); d != (AgentDecision{Policy: "never", Source: AgentSourceRole, Role: "operator"}) {
		t.Errorf("operator create-component = %+v, want the role to keep a tie", d)
	}
	if d := a.ResolveTool("list-components", "read", nil); d != (AgentDecision{Policy: "always", Source: AgentSourcePlatform}) {
		t.Errorf("list-components = %+v, want the platform's always", d)
	}
}
