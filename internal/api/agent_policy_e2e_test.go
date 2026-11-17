package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The install's agent policy (#2063): an admin sets always, ask or never per
// operation class, and the server refuses a never class to every agent request,
// an agent token over REST and any token over MCP alike, auditing the refusal
// as the agent's. A fresh install refuses nothing. Skipped under -short.

type agentPolicy struct {
	Applies bool `json:"applies"`
	Classes []struct {
		Class  string `json:"class"`
		Policy string `json:"policy"`
	} `json:"classes"`
}

func (p agentPolicy) of(class string) string {
	for _, c := range p.Classes {
		if c.Class == class {
			return c.Policy
		}
	}
	return ""
}

func readAgentPolicy(t *testing.T, s *mcpStack, tok string) agentPolicy {
	t.Helper()
	var p agentPolicy
	if err := json.Unmarshal(s.c.do(tok, http.MethodGet, "/auth/me/agent-policy", nil, http.StatusOK), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAgentPolicyDefaultsRefuseNothing(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	agent := mintAgent(t, s.c, s.owner, map[string]any{"description": "claude", "agent": "claude", "ttl_days": 1})

	p := readAgentPolicy(t, s, agent.Token)
	if !p.Applies || p.of("read") != "always" || p.of("write") != "ask" || p.of("destructive") != "ask" || p.of("credential_destination") != "ask" {
		t.Errorf("agent's default policy = %+v, want applies, read always and the rest ask", p)
	}
	if readAgentPolicy(t, s, s.owner).Applies {
		t.Error("the policy applies to the owner's own REST call")
	}
	// ask is the client's to confirm, not the server's: the write reaches the
	// handler, which refuses the empty body on its own terms.
	s.c.do(agent.Token, http.MethodPost, "/components", map[string]any{}, http.StatusUnprocessableEntity)
}

func TestAgentPolicyNeverRefusesTheClassOnEveryChannel(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	agent := mintAgent(t, s.c, s.owner, map[string]any{"description": "claude", "agent": "claude", "ttl_days": 1})

	s.c.do(s.owner, http.MethodPatch, "/settings/agent", map[string]any{"write": "never"}, http.StatusOK)
	s.c.do(s.owner, http.MethodPatch, "/settings/agent", map[string]any{"write": "sometimes"}, http.StatusUnprocessableEntity)

	// A write is refused to the agent before its handler runs; a read is not,
	// and the person's own write is untouched.
	s.c.do(agent.Token, http.MethodPost, "/components", map[string]any{}, http.StatusForbidden)
	s.c.do(agent.Token, http.MethodGet, "/components", nil, http.StatusOK)
	s.c.do(s.owner, http.MethodPost, "/components", map[string]any{}, http.StatusUnprocessableEntity)

	// Refusing reads leaves who you are and why readable, so the MCP bearer
	// probe and the client's policy check still answer.
	s.c.do(s.owner, http.MethodPatch, "/settings/agent", map[string]any{"read": "never"}, http.StatusOK)
	s.c.do(agent.Token, http.MethodGet, "/components", nil, http.StatusForbidden)
	s.c.do(agent.Token, http.MethodGet, "/auth/me", nil, http.StatusOK)
	if p := readAgentPolicy(t, s, agent.Token); p.of("read") != "never" || p.of("write") != "never" {
		t.Errorf("agent's policy after the patch = %+v, want read and write never", p)
	}

	// Over MCP even the person's own token is an agent's hand.
	cs := s.session(t, s.owner, "policy-client")
	if res, text := callTool(t, cs, "list-components", nil); !res.IsError {
		t.Errorf("a read tool ran over MCP with reads set to never: %s", text)
	}
	if res, text := callTool(t, cs, "get-auth-me-agent-policy", nil); res.IsError {
		t.Errorf("reading the policy over MCP: %s", text)
	}

	var trail struct {
		Events []struct {
			Resource   string `json:"resource"`
			ResourceID string `json:"resource_id"`
			Agent      string `json:"agent"`
			Channel    string `json:"channel"`
		} `json:"events"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/audit-log?verb=agent_refused", nil, http.StatusOK), &trail); err != nil {
		t.Fatal(err)
	}
	var restWrite, restRead, mcpRead bool
	for _, e := range trail.Events {
		if e.Resource != "operation" {
			t.Errorf("refusal resource = %q, want operation", e.Resource)
		}
		switch {
		case e.ResourceID == "create-component" && e.Agent == "claude" && e.Channel == "":
			restWrite = true
		case e.ResourceID == "list-components" && e.Agent == "claude" && e.Channel == "":
			restRead = true
		case e.ResourceID == "list-components" && e.Channel == "mcp":
			mcpRead = true
		}
	}
	if !restWrite || !restRead || !mcpRead || len(trail.Events) != 3 {
		t.Errorf("agent_refused trail = %+v, want the agent's REST write and read and the MCP read", trail.Events)
	}
}

// The policy binds agents, so no agent may rewrite it: changing or dropping
// the agent namespace, or the factory reset that drops it with the rest, is
// refused to every agent request whatever the classes say, and audited as the
// agent's. Its person may; another namespace stays the agent's to change.
func TestAnAgentCannotLoosenItsOwnPolicy(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	agent := mintAgent(t, s.c, s.owner, map[string]any{"description": "claude", "agent": "claude", "ttl_days": 1})

	s.c.do(s.owner, http.MethodPatch, "/settings/agent", map[string]any{"write": "always", "destructive": "never"}, http.StatusOK)
	s.c.do(agent.Token, http.MethodPatch, "/settings/agent", map[string]any{"destructive": "always"}, http.StatusForbidden)
	s.c.do(agent.Token, http.MethodDelete, "/settings/agent", nil, http.StatusForbidden)
	s.c.do(agent.Token, http.MethodPost, "/settings:restoreDefaults", nil, http.StatusForbidden)
	s.c.do(agent.Token, http.MethodPatch, "/settings/ui", map[string]any{"theme": "omniglass-light"}, http.StatusOK)
	if p := readAgentPolicy(t, s, agent.Token); p.of("destructive") != "never" {
		t.Errorf("destructive after the agent's attempts = %q, want never", p.of("destructive"))
	}

	var trail struct {
		Events []struct {
			ResourceID string `json:"resource_id"`
			Agent      string `json:"agent"`
		} `json:"events"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/audit-log?verb=agent_refused", nil, http.StatusOK), &trail); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range trail.Events {
		got[e.Agent+"/"+e.ResourceID] = true
	}
	for _, want := range []string{"claude/patch-settings-namespace", "claude/delete-settings-namespace", "claude/restore-settings-defaults"} {
		if !got[want] {
			t.Errorf("agent_refused trail = %v, missing %s", got, want)
		}
	}
	if len(trail.Events) != 3 {
		t.Errorf("agent_refused trail has %d rows, want 3", len(trail.Events))
	}

	s.c.do(s.owner, http.MethodDelete, "/settings/agent", nil, http.StatusNoContent)
}

// listedPolicy is each listed tool's _meta agent policy, "class/policy", by
// tool name.
func listedPolicy(t *testing.T, s *mcpStack, cs *sdk.ClientSession) map[string]string {
	t.Helper()
	got := map[string]string{}
	for tool, err := range cs.Tools(s.ctx, nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		m, _ := tool.Meta["omniglass/agent_policy"].(map[string]any)
		class, _ := m["class"].(string)
		policy, _ := m["policy"].(string)
		got[tool.Name] = class + "/" + policy
	}
	return got
}

// A listed tool says what the install lets an agent do with it, so a client
// can hide a never tool and confirm an ask one; the listing follows the
// policy as an admin changes it.
func TestAgentPolicyMarksEveryListedTool(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	cs := s.session(t, s.owner, "policy-list")

	want := map[string]string{
		"list-components":             "read/always",
		"delete-component-dependency": "destructive/ask",
		"update-endpoint":             "credential_destination/ask",
		"get-auth-me-agent-policy":    "read/always",
	}
	got := listedPolicy(t, s, cs)
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s listed as %q, want %q", name, got[name], w)
		}
	}
	for name, p := range got {
		if p == "/" {
			t.Errorf("%s listed with no agent policy", name)
		}
	}

	s.c.do(s.owner, http.MethodPatch, "/settings/agent", map[string]any{"destructive": "never"}, http.StatusOK)
	if p := listedPolicy(t, s, cs)["delete-component-dependency"]; p != "destructive/never" {
		t.Errorf("after setting destructive to never, delete-component-dependency listed as %q", p)
	}
}
