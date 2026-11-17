package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestAConversationIsKeptForItsOwnerAlone drives stored omnibot conversations
// over HTTP (#2113): a viewer keeps one (assistant:use rides viewer), a role
// without it cannot, another person, the platform owner included, finds none
// of it on any verb, an append that names a message it did not see last is a
// conflict, and a paused call round-trips with the transcript.
func TestAConversationIsKeptForItsOwnerAlone(t *testing.T) {
	t.Parallel()
	p := newPeople(t)
	p.human("alice", "viewer", "all", "")
	p.human("bob", "viewer", "all", "")
	p.human("rita", "reader", "all", "")

	p.c.do(p.toks["rita"], http.MethodPost, "/assistant/conversations", map[string]any{"name": "x"}, http.StatusForbidden)
	p.c.do(p.toks["bob"], http.MethodPost, "/assistant/conversations", map[string]any{"name": "Not A Name"}, http.StatusUnprocessableEntity)
	p.c.do(p.toks["bob"], http.MethodPost, "/assistant/conversations", map[string]any{"name": "triage"}, http.StatusCreated)
	p.c.do(p.toks["bob"], http.MethodPost, "/assistant/conversations", map[string]any{"name": "triage"}, http.StatusConflict)

	turn := map[string]any{"after": 0, "messages": []map[string]any{
		{"role": "user", "body": map[string]any{"text": "what is down"}},
		{"role": "assistant", "body": map[string]any{"text": "display-1"}},
	}, "pending": map[string]any{"call_id": "call-1"}, "usage": map[string]any{"input_tokens": 12, "output_tokens": 4}}
	p.c.do(p.toks["bob"], http.MethodPost, "/assistant/conversations/triage:append", turn, http.StatusOK)
	p.c.do(p.toks["bob"], http.MethodPost, "/assistant/conversations/triage:append", turn, http.StatusConflict)

	for _, who := range []string{p.toks["alice"], p.owner} {
		p.c.do(who, http.MethodGet, "/assistant/conversations/triage", nil, http.StatusNotFound)
		p.c.do(who, http.MethodPost, "/assistant/conversations/triage:append", map[string]any{"after": 2, "messages": []map[string]any{
			{"role": "user", "body": map[string]any{"text": "x"}}}}, http.StatusNotFound)
		p.c.do(who, http.MethodDelete, "/assistant/conversations/triage", nil, http.StatusNotFound)
		if list := string(p.c.do(who, http.MethodGet, "/assistant/conversations", nil, http.StatusOK)); strings.Contains(list, "triage") {
			t.Fatalf("another person lists bob's conversation: %s", list)
		}
	}

	// Impersonation is the exception (ADR-2113): a view-as session sees what
	// bob sees, his conversations included, and cannot write to them.
	var imp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/principals/"+p.ids["bob"]+":impersonate", map[string]any{"mode": "view_as"}, http.StatusCreated), &imp); err != nil {
		t.Fatal(err)
	}
	p.c.do(imp.Token, http.MethodGet, "/assistant/conversations/triage", nil, http.StatusOK)
	p.c.do(imp.Token, http.MethodDelete, "/assistant/conversations/triage", nil, http.StatusForbidden)

	var got struct {
		Name    string          `json:"name"`
		Pending json.RawMessage `json:"pending"`
		Usage   struct {
			InputTokens int64 `json:"input_tokens"`
		} `json:"usage"`
		Messages []struct {
			Seq  int             `json:"seq"`
			Role string          `json:"role"`
			Body json.RawMessage `json:"body"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(p.c.do(p.toks["bob"], http.MethodGet, "/assistant/conversations/triage", nil, http.StatusOK), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 || got.Messages[1].Seq != 2 || got.Messages[1].Role != "assistant" ||
		!strings.Contains(string(got.Messages[1].Body), "display-1") || !strings.Contains(string(got.Pending), "call-1") || got.Usage.InputTokens != 12 {
		t.Fatalf("bob's conversation = %+v, want his two messages, the paused call and the usage", got)
	}
	p.c.do(p.toks["bob"], http.MethodDelete, "/assistant/conversations/triage", nil, http.StatusNoContent)
	p.c.do(p.toks["bob"], http.MethodGet, "/assistant/conversations/triage", nil, http.StatusNotFound)
}
