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
