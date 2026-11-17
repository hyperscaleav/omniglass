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
	person := &storage.User{}
	if agentCeiling(person, "") {
		t.Error("a personal token over REST is not ceilinged")
	}
	if !agentCeiling(person, storage.AuditChannelMCP) {
		t.Error("a personal token over MCP is ceilinged")
	}
	agent := &storage.User{Agent: &storage.AgentCredential{Name: "claude"}}
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
	// The ceiling is plaintext-out only (#2047): an agent binds a credential
	// it never reads, but never reveals one or mints an outpost's enrollment token.
	ceilinged := constrainPerms(owner, idx, nil, true)
	if !ceilinged.Allows("secret", "bind") {
		t.Error("a ceilinged owner binds a secret it cannot read")
	}
	if ceilinged.Allows("outpost", "enroll") {
		t.Error("a ceilinged owner must not mint an enrollment token")
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
		{[]string{"omnibot"}, "", false},
		{[]string{"omnibot"}, storage.AuditChannelOmnibot, true},
	}
	for _, c := range cases {
		if got := agentChannelAccepted(c.allowed, c.channel); got != c.want {
			t.Errorf("agentChannelAccepted(%v, %q) = %v, want %v", c.allowed, c.channel, got, c.want)
		}
	}
}

// An agent token minted for omnibot alone (#1166) is omnibot's on the MCP
// channel, so its rows read channel omnibot; nothing else is restamped, so no
// other token or declared client name obtains that channel.
func TestRequestChannel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		agent   *storage.AgentCredential
		channel storage.AuditChannel
		want    storage.AuditChannel
	}{
		{nil, storage.AuditChannelMCP, storage.AuditChannelMCP},
		{nil, "", ""},
		{&storage.AgentCredential{Name: "omnibot"}, storage.AuditChannelMCP, storage.AuditChannelMCP},
		{&storage.AgentCredential{Name: "x", Channels: []string{"mcp"}}, storage.AuditChannelMCP, storage.AuditChannelMCP},
		{&storage.AgentCredential{Name: "x", Channels: []string{"omnibot"}}, storage.AuditChannelMCP, storage.AuditChannelOmnibot},
		{&storage.AgentCredential{Name: "x", Channels: []string{"omnibot"}}, "", ""},
	}
	for _, c := range cases {
		if got := requestChannel(c.agent, c.channel); got != c.want {
			t.Errorf("requestChannel(%+v, %q) = %q, want %q", c.agent, c.channel, got, c.want)
		}
	}
	if !agentCeiling(&storage.User{}, storage.AuditChannelOmnibot) {
		t.Error("a request on the omnibot channel is not held to the agent ceiling")
	}
}
