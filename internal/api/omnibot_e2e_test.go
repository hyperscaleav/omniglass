package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/assistant"
	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
)

// omnibot (#1166) end to end: a person's token mints omnibot its own agent
// token, the loop drives the real MCP server with it, every call is the
// route's with the person's grants, its rows read agent and channel omnibot,
// and closing the session revokes the token. Skipped under -short.

func omnibotRun(t *testing.T, s *mcpStack, token, recording string, confirm bool) (assistant.Outcome, *assistant.Session) {
	t.Helper()
	sess, err := assistant.Connect(s.ctx, s.base, token, "test")
	if err != nil {
		t.Fatalf("connect omnibot: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close(context.Background()) })
	p, err := provider.ParseReplay([]byte(recording))
	if err != nil {
		t.Fatal(err)
	}
	answers := []bool{confirm, false}
	loop := &assistant.Loop{Provider: p, Tools: sess, Confirm: func(context.Context, assistant.Proposal) (bool, error) {
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}}
	out, err := loop.Run(s.ctx, "add a lobby display")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out, sess
}

func agentTokens(t *testing.T, s *mcpStack, token string) []string {
	t.Helper()
	var out struct {
		Sessions []struct {
			Description string `json:"description"`
			Prefix      string `json:"prefix"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(s.c.do(token, http.MethodGet, "/auth/me/sessions", nil, http.StatusOK), &out); err != nil {
		t.Fatal(err)
	}
	var p []string
	for _, x := range out.Sessions {
		if x.Description == "omnibot session" {
			p = append(p, x.Prefix)
		}
	}
	return p
}

// The person confirms one write and declines the next: the first lands,
// audited as omnibot's on the omnibot channel under the person's name; the
// second never reaches the server. Closing the session revokes its token.
func TestOmnibotRunsConfirmedWritesAsItself(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	out, sess := omnibotRun(t, s, s.owner, `
turns:
  - tool_calls: [{name: list-components}]
  - tool_calls: [{name: create-component, arguments: {name: omnibot-lobby, product: generic-device}}]
  - tool_calls: [{name: create-component, arguments: {name: omnibot-declined, product: generic-device}}]
  - text: Created omnibot-lobby; you declined omnibot-declined.
`, true)
	var got []string
	for _, c := range out.Calls {
		got = append(got, c.Tool+":"+string(c.Decision))
		if c.Decision == assistant.DecisionCalled && c.IsError {
			t.Errorf("%s errored: %s", c.Tool, c.Text)
		}
	}
	if strings.Join(got, ",") != "list-components:called,create-component:called,create-component:declined" {
		t.Errorf("calls = %v", got)
	}
	s.c.do(s.owner, http.MethodGet, "/components/omnibot-lobby", nil, http.StatusOK)
	s.c.do(s.owner, http.MethodGet, "/components/omnibot-declined", nil, http.StatusNotFound)

	var trail struct {
		Events []auditChannelEvent `json:"events"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/audit-log?channel=omnibot&resource=component", nil, http.StatusOK), &trail); err != nil {
		t.Fatal(err)
	}
	if len(trail.Events) != 1 || trail.Events[0].New.Name != "omnibot-lobby" || trail.Events[0].Channel != "omnibot" {
		t.Fatalf("omnibot rows = %+v, want the one create", trail.Events)
	}
	var agent struct {
		Events []struct {
			Agent     string `json:"agent"`
			ActorName string `json:"actor_name"`
		} `json:"events"`
	}
	_ = json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/audit-log?channel=omnibot&resource=component", nil, http.StatusOK), &agent)
	if agent.Events[0].Agent != "omnibot" || agent.Events[0].ActorName != "root" {
		t.Errorf("the row = %+v, want agent omnibot acting for root", agent.Events[0])
	}

	if live := agentTokens(t, s, s.owner); len(live) != 1 {
		t.Fatalf("open omnibot tokens = %v, want the session's one", live)
	}
	if err := sess.Close(s.ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if live := agentTokens(t, s, s.owner); len(live) != 0 {
		t.Errorf("open omnibot tokens after close = %v, want none", live)
	}
}

// omnibot holds no more than its person: a viewer's confirmed write is the
// route's 403, surfaced to the model as the call's error.
func TestOmnibotHoldsOnlyItsPersonsGrants(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	s.c.do(s.owner, http.MethodPost, "/principals", map[string]string{"username": "vera", "password": "orange-boat-42x"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/principals/vera/grants", map[string]any{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)
	viewer := loginBearer(t, s.c, "vera", "orange-boat-42x")
	out, _ := omnibotRun(t, s, viewer, `
turns:
  - tool_calls: [{name: create-component, arguments: {name: viewer-bot, product: generic-device}}]
  - text: I could not.
`, true)
	if len(out.Calls) != 1 || out.Calls[0].Decision != assistant.DecisionCalled || !out.Calls[0].IsError || !strings.Contains(out.Calls[0].Text, "403") {
		t.Errorf("calls = %+v, want the route's 403", out.Calls)
	}
	s.c.do(s.owner, http.MethodGet, "/components/viewer-bot", nil, http.StatusNotFound)
}

// A token revoked mid-session stops the next call: the run ends with the
// error instead of carrying on.
func TestOmnibotStopsWhenItsTokenIsRevoked(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	sess, err := assistant.Connect(s.ctx, s.base, s.owner, "test")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Sessions []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
		} `json:"sessions"`
	}
	_ = json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/auth/me/sessions", nil, http.StatusOK), &out)
	for _, x := range out.Sessions {
		if x.Description == "omnibot session" {
			s.c.do(s.owner, http.MethodPost, "/auth/me/sessions/"+x.ID+":revoke", nil, http.StatusNoContent)
		}
	}
	if _, err := sess.Call(s.ctx, "list-components", nil); err == nil {
		t.Error("a call on a revoked token went through")
	}
}

// omnibot runs on a person's own token: an agent token cannot mint it one.
func TestOmnibotNeedsAPersonsToken(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	bot := mintAgent(t, s.c, s.owner, map[string]any{"description": "other", "agent": "other", "ttl_days": 1})
	if _, err := assistant.Connect(s.ctx, s.base, bot.Token, "test"); err == nil || strings.Contains(err.Error(), bot.Token) {
		t.Errorf("connect on an agent token = %v, want a refusal that does not echo the token", err)
	}
}
