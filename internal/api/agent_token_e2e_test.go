package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Agent tokens (#1179): a person mints a token for an agent that acts on their
// behalf. It holds only what its own roles AND its person hold, never a reveal
// or use action whatever either holds, cannot mint a token, change a password
// or impersonate, is accepted only on its channels, needs an explicit expiry,
// and every row it writes names the agent. Skipped under -short.

type mintedAgent struct {
	Token     string   `json:"token"`
	Prefix    string   `json:"prefix"`
	ExpiresAt string   `json:"expires_at"`
	Agent     string   `json:"agent"`
	Roles     []string `json:"roles"`
	Channels  []string `json:"channels"`
}

func mintAgent(t *testing.T, c *apiClient, tok string, body map[string]any) mintedAgent {
	t.Helper()
	var m mintedAgent
	raw := c.do(tok, http.MethodPost, "/auth/me/tokens", body, http.StatusCreated)
	if err := json.Unmarshal(raw, &m); err != nil || m.Token == "" {
		t.Fatalf("mint agent = %s (%v)", raw, err)
	}
	return m
}

// revealStamps reads the generated spec for every operation whose permission
// is a reveal or use action, so a new one is covered the day it is registered.
func revealStamps(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read openapi.json (run make gen): %v", err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Permission string `json:"x-omniglass-permission"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}
	param := regexp.MustCompile(`\{[^}]+\}`)
	out := map[string]string{}
	for path, ops := range spec.Paths {
		for method, op := range ops {
			tokens := strings.Split(op.Permission, ":")
			if len(tokens) < 2 || (tokens[1] != "reveal" && tokens[1] != "use") {
				continue
			}
			out[strings.ToUpper(method)+" "+param.ReplaceAllString(strings.TrimPrefix(path, "/api/v1"), "lab-community")] = op.Permission
		}
	}
	return out
}

// An owner's agent token never reaches a reveal, though its owner does: the
// ceiling holds on every registered reveal route.
func TestAgentTokenCannotReveal(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	c, ownerTok := auditImagesStack(t)
	agent := mintAgent(t, c, ownerTok, map[string]any{"description": "claude", "agent": "claude", "ttl_days": 7})
	if agent.Agent != "claude" || agent.ExpiresAt == "" {
		t.Fatalf("minted = %+v, want the agent named and an expiry", agent)
	}

	c.do(ownerTok, http.MethodPost, "/secrets/lab-community:reveal", nil, http.StatusOK)

	stamps := revealStamps(t)
	if len(stamps) == 0 {
		t.Fatal("no reveal route in the spec; the ceiling test covers nothing")
	}
	for route, perm := range stamps {
		method, path, _ := strings.Cut(route, " ")
		if code, body := c.send(agent.Token, method, path, map[string]any{}); code != http.StatusForbidden {
			t.Errorf("%s (%s) as an agent = %d %s, want 403", route, perm, code, body)
		}
	}

	var me struct {
		Agent       string   `json:"agent"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(c.do(agent.Token, http.MethodGet, "/auth/me", nil, http.StatusOK), &me); err != nil {
		t.Fatal(err)
	}
	if me.Agent != "claude" {
		t.Errorf("/auth/me agent = %q, want claude", me.Agent)
	}
	for _, p := range me.Permissions {
		if strings.Contains(p, "reveal") || p == ">" {
			t.Errorf("/auth/me advertises %q to an agent", p)
		}
	}
}

// An agent token's grants are its roles intersected with its person's, read
// afresh on every request: a viewer agent cannot write what its owner may, and
// an operator agent loses a write the moment its person loses the grant.
func TestAgentTokenIntersectsItsPerson(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	c, ownerTok := auditImagesStack(t)

	viewer := mintAgent(t, c, ownerTok, map[string]any{"description": "reader", "agent": "reader", "ttl_days": 1, "roles": []string{"viewer"}})
	c.do(viewer.Token, http.MethodGet, "/components", nil, http.StatusOK)
	c.do(viewer.Token, http.MethodPost, "/components", map[string]any{"name": "nope", "product": "generic-device"}, http.StatusForbidden)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "yes", "product": "generic-device"}, http.StatusCreated)

	// Alice is an operator; her agent asks for operator too.
	var alice struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/principals", map[string]string{"username": "alice", "password": "orange-boat-42x"}, http.StatusCreated), &alice); err != nil {
		t.Fatal(err)
	}
	var grant struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/principals/"+alice.ID+"/grants", map[string]any{"role": "operator", "scope_kind": "all"}, http.StatusCreated), &grant); err != nil {
		t.Fatal(err)
	}
	aliceSession := loginBearer(t, c, "alice", "orange-boat-42x")

	// Alice cannot hand her agent a role she does not cover.
	if code, body := c.send(aliceSession, http.MethodPost, "/auth/me/tokens", map[string]any{"description": "x", "agent": "x", "ttl_days": 1, "roles": []string{"admin"}}); code != http.StatusForbidden {
		t.Errorf("alice minting an admin agent = %d %s, want 403", code, body)
	}
	if code, body := c.send(aliceSession, http.MethodPost, "/auth/me/tokens", map[string]any{"description": "x", "agent": "x", "ttl_days": 1, "roles": []string{"no-such-role"}}); code != http.StatusUnprocessableEntity {
		t.Errorf("minting an agent with an unknown role = %d %s, want 422", code, body)
	}

	op := mintAgent(t, c, aliceSession, map[string]any{"description": "ops", "agent": "ops-bot", "ttl_days": 1, "roles": []string{"operator"}})
	c.do(op.Token, http.MethodGet, "/components", nil, http.StatusOK)
	c.do(op.Token, http.MethodPatch, "/components/amp-9", map[string]any{"label": "agent was here"}, http.StatusOK)

	// Alice loses operator: her agent loses it with her, on the next request.
	c.do(ownerTok, http.MethodDelete, "/principals/"+alice.ID+"/grants/"+grant.ID, nil, http.StatusNoContent)
	if code, body := c.send(op.Token, http.MethodPatch, "/components/amp-9", map[string]any{"label": "again"}); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Errorf("the agent writes after its person lost the grant: %d %s", code, body)
	}

	// The write it made is marked as the agent's, on alice's behalf.
	var trail struct {
		Events []struct {
			Verb      string `json:"verb"`
			Resource  string `json:"resource"`
			ActorName string `json:"actor_name"`
			Agent     string `json:"agent"`
		} `json:"events"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/audit-log?agent=ops-bot", nil, http.StatusOK), &trail); err != nil {
		t.Fatal(err)
	}
	if len(trail.Events) != 1 || trail.Events[0].Agent != "ops-bot" || trail.Events[0].ActorName != "alice" || trail.Events[0].Resource != "component" {
		t.Errorf("agent trail = %+v, want alice's one component write marked ops-bot", trail.Events)
	}
}

// An agent token cannot extend itself: no minting, no password change, no
// impersonation. Expiry is explicit, and roles and channels belong to an agent.
func TestAgentTokenRefusals(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	c, ownerTok := auditImagesStack(t)

	if code, body := c.send(ownerTok, http.MethodPost, "/auth/me/tokens", map[string]any{"description": "x", "agent": "x"}); code != http.StatusUnprocessableEntity {
		t.Errorf("an agent token with no ttl_days = %d %s, want 422", code, body)
	}
	if code, body := c.send(ownerTok, http.MethodPost, "/auth/me/tokens", map[string]any{"description": "x", "roles": []string{"viewer"}}); code != http.StatusUnprocessableEntity {
		t.Errorf("roles on a personal token = %d %s, want 422", code, body)
	}

	c.do(ownerTok, http.MethodPost, "/principals", map[string]string{"username": "bob", "password": "orange-boat-42x"}, http.StatusCreated)
	agent := mintAgent(t, c, ownerTok, map[string]any{"description": "a", "agent": "a", "ttl_days": 1})
	for _, r := range []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodPost, "/auth/me/tokens", map[string]any{"description": "child"}},
		{http.MethodPost, "/auth/me/tokens", map[string]any{"description": "child", "agent": "child", "ttl_days": 1}},
		{http.MethodPost, "/auth/me:changePassword", map[string]any{"current_password": "x", "new_password": "a-long-new-password"}},
		{http.MethodPost, "/principals/bob:impersonate", map[string]any{"mode": "view_as"}},
	} {
		if code, body := c.send(agent.Token, r.method, r.path, r.body); code != http.StatusForbidden {
			t.Errorf("%s %s as an agent = %d %s, want 403", r.method, r.path, code, body)
		}
	}

	var sessions struct {
		Sessions []struct {
			ID     string `json:"id"`
			Prefix string `json:"prefix"`
			Agent  string `json:"agent"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/auth/me/sessions", nil, http.StatusOK), &sessions); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sessions.Sessions {
		if s.Prefix == agent.Prefix {
			found = s.Agent == "a"
		}
	}
	if !found {
		t.Errorf("the session list does not mark the agent token: %+v", sessions.Sessions)
	}

	// Revoking the agent token stops the agent and leaves its person working.
	var agentID string
	for _, s := range sessions.Sessions {
		if s.Prefix == agent.Prefix {
			agentID = s.ID
		}
	}
	c.do(ownerTok, http.MethodPost, "/auth/me/sessions/"+agentID+":revoke", nil, http.StatusNoContent)
	if code, _ := c.send(agent.Token, http.MethodGet, "/auth/me", nil); code != http.StatusUnauthorized {
		t.Errorf("a revoked agent token reads /auth/me = %d, want 401", code)
	}
	c.do(ownerTok, http.MethodGet, "/auth/me", nil, http.StatusOK)
}

// An agent token minted for one channel is refused on the other, except for
// reading who it is.
func TestAgentTokenChannels(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	mcpOnly := mintAgent(t, s.c, s.owner, map[string]any{"description": "m", "agent": "m", "ttl_days": 1, "channels": []string{"mcp"}})
	apiOnly := mintAgent(t, s.c, s.owner, map[string]any{"description": "p", "agent": "p", "ttl_days": 1, "channels": []string{"api"}})

	s.c.do(mcpOnly.Token, http.MethodGet, "/components", nil, http.StatusForbidden)
	s.c.do(mcpOnly.Token, http.MethodGet, "/auth/me", nil, http.StatusOK)
	s.c.do(apiOnly.Token, http.MethodGet, "/components", nil, http.StatusOK)

	cs := s.session(t, mcpOnly.Token, "mcp-only")
	if res, text := callTool(t, cs, "list-components", nil); res.IsError {
		t.Errorf("an mcp agent listing components over MCP: %s", text)
	}
	if cs2, err := s.connect(t, apiOnly.Token, "api-only"); err == nil {
		res, text := callTool(t, cs2, "list-components", nil)
		if !res.IsError {
			t.Errorf("an api-only agent listed components over MCP: %s", text)
		}
		_ = cs2.Close()
	}
}

// TestAgentTokenCannotEscalateThroughIAM: the never-reveal ceiling is not
// escaped by handing a credential-revealing role to a principal the agent
// controls. Every escalation guard (granting a role, adding a group member,
// resetting a password) measures the agent's bounded permissions, not its
// person's.
func TestAgentTokenCannotEscalateThroughIAM(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	c, ownerTok := auditImagesStack(t)
	agent := mintAgent(t, c, ownerTok, map[string]any{"description": "iam", "agent": "iam-bot", "ttl_days": 1})

	c.do(agent.Token, http.MethodPost, "/principals", map[string]string{"username": "mallory", "password": "orange-boat-42x"}, http.StatusCreated)
	for _, role := range []string{"owner", "operator"} {
		if code, body := c.send(agent.Token, http.MethodPost, "/principals/mallory/grants", map[string]any{"role": role, "scope_kind": "all"}); code != http.StatusForbidden {
			t.Errorf("an agent granting %s = %d %s, want 403", role, code, body)
		}
	}
	c.do(agent.Token, http.MethodPost, "/principals/mallory/grants", map[string]any{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)

	c.do(ownerTok, http.MethodPost, "/principal-groups", map[string]any{"name": "ops"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/principal-groups/ops/grants", map[string]any{"role": "operator", "scope_kind": "all"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/principal-groups", map[string]any{"name": "readers"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/principal-groups/readers/grants", map[string]any{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)
	if code, body := c.send(agent.Token, http.MethodPost, "/principal-groups/readers/grants", map[string]any{"role": "operator", "scope_kind": "all"}); code != http.StatusForbidden {
		t.Errorf("an agent granting operator to a group = %d %s, want 403", code, body)
	}
	if code, body := c.send(agent.Token, http.MethodPost, "/principal-groups/ops/members", map[string]any{"principal_id": "mallory"}); code != http.StatusForbidden {
		t.Errorf("an agent adding a member to a revealing group = %d %s, want 403", code, body)
	}
	c.do(agent.Token, http.MethodPost, "/principal-groups/readers/members", map[string]any{"principal_id": "mallory"}, http.StatusNoContent)
	// The person, unconstrained, still may.
	c.do(ownerTok, http.MethodPost, "/principal-groups/ops/members", map[string]any{"principal_id": "mallory"}, http.StatusNoContent)

	c.do(ownerTok, http.MethodPost, "/principals", map[string]string{"username": "carol", "password": "orange-boat-42x"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/principals/carol/grants", map[string]any{"role": "operator", "scope_kind": "all"}, http.StatusCreated)
	if code, body := c.send(agent.Token, http.MethodPost, "/principals/carol:resetPassword", map[string]any{"password": "a-brand-new-pass-77"}); code != http.StatusForbidden {
		t.Errorf("an agent resetting a revealer's password = %d %s, want 403", code, body)
	}
	c.do(ownerTok, http.MethodPost, "/principals/carol:resetPassword", map[string]any{"password": "a-brand-new-pass-77"}, http.StatusNoContent)
}

// loginBearer logs a person in and returns the web session's secret, which is
// a bearer credential like any other.
func loginBearer(t *testing.T, c *apiClient, user, password string) string {
	t.Helper()
	req, _ := http.NewRequestWithContext(c.ctx, http.MethodPost, c.base+"/api/v1/auth/login", strings.NewReader(`{"username":"`+user+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login %s: %v %v", user, err, resp)
	}
	defer resp.Body.Close()
	for _, ck := range resp.Cookies() {
		if ck.Name == "og-session" {
			return ck.Value
		}
	}
	t.Fatalf("login %s: no session cookie", user)
	return ""
}
