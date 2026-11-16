package api

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestAgentCeilingCoversTheMCPChannel: a personal token used over MCP is an
// agent's hand on the fleet, so it meets the same ceiling as an agent token
// (#1179); the same token over REST does not. The MCP catalog has no reveal
// tool, so this is the only place the channel rule is observable.
func TestAgentCeilingCoversTheMCPChannel(t *testing.T) {
	t.Parallel()
	person := &storage.Principal{}
	if agentCeiling(person, "") {
		t.Error("a personal token over REST is not ceilinged")
	}
	if !agentCeiling(person, storage.AuditChannelMCP) {
		t.Error("a personal token over MCP is ceilinged")
	}
	agent := &storage.Principal{Agent: &storage.AgentCredential{Name: "claude"}}
	if !agentCeiling(agent, "") {
		t.Error("an agent token is ceilinged on every channel")
	}

	idx := rbac.NewRoleIndex([]rbac.Role{
		{ID: "owner", Permissions: []string{">"}},
		{ID: "viewer", Permissions: []string{"*:read"}},
	})
	owner := idx.Flatten([]string{"owner"})
	if constrainPerms(owner, idx, nil, true).Allows("secret", "reveal") {
		t.Error("a ceilinged owner must not reveal")
	}
	if !constrainPerms(owner, idx, nil, false).Allows("secret", "reveal") {
		t.Error("an unceilinged owner reveals")
	}
	viewerAgent := &storage.AgentCredential{Name: "bot", Roles: []string{"viewer"}}
	got := constrainPerms(owner, idx, viewerAgent, true)
	if got.Allows("location", "update") || !got.Allows("location", "read") {
		t.Error("an agent holds its own roles within its person, nothing more")
	}
}

func TestAgentChannelAccepted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		allowed []string
		channel storage.AuditChannel
		want    bool
	}{
		{nil, "", true},
		{nil, storage.AuditChannelMCP, true},
		{[]string{"api"}, "", true},
		{[]string{"api"}, storage.AuditChannelMCP, false},
		{[]string{"mcp"}, "", false},
		{[]string{"mcp"}, storage.AuditChannelMCP, true},
	}
	for _, c := range cases {
		if got := agentChannelAccepted(c.allowed, c.channel); got != c.want {
			t.Errorf("agentChannelAccepted(%v, %q) = %v, want %v", c.allowed, c.channel, got, c.want)
		}
	}
}
