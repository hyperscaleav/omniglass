package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestAViewerSubscribesThemselvesToWhatTheyCanSee drives personal rules over
// HTTP (#1353): a viewer subscribes (notification:subscribe rides viewer), a
// role without it cannot, a personal rule names no target, another user's
// rule is a 404 on every verb, the platform list never shows one, the rule
// tells its owner alone about alarms they can read, and :test judges the
// owner's own recent alarms.
func TestAViewerSubscribesThemselvesToWhatTheyCanSee(t *testing.T) {
	p := newPeople(t)
	p.human("alice", "viewer", "all", "")
	p.human("bob", "viewer", "component", p.comps["display-2"])
	p.human("rita", "reader", "all", "")

	rule := map[string]any{"name": "my-displays", "on": []string{"raised"}, "when": "severity_rank >= 40", "channels": []string{"inbox"}}
	p.c.do(p.toks["rita"], http.MethodPost, "/auth/me/action-rules", rule, http.StatusForbidden)
	p.c.do(p.toks["bob"], http.MethodPost, "/auth/me/action-rules", map[string]any{"name": "x", "on": []string{"raised"},
		"channels": []string{"inbox"}, "targets": []string{"ops-chat"}}, http.StatusUnprocessableEntity)
	var bobs struct{ ID string }
	_ = json.Unmarshal(p.c.do(p.toks["bob"], http.MethodPost, "/auth/me/action-rules", rule, http.StatusCreated), &bobs)
	p.c.do(p.toks["alice"], http.MethodPost, "/auth/me/action-rules", rule, http.StatusCreated)

	// bob's rule is not alice's to read, change, test or delete.
	p.c.do(p.toks["alice"], http.MethodGet, "/auth/me/action-rules/"+bobs.ID, nil, http.StatusNotFound)
	p.c.do(p.toks["alice"], http.MethodPatch, "/auth/me/action-rules/"+bobs.ID, map[string]any{"enabled": false}, http.StatusNotFound)
	p.c.do(p.toks["alice"], http.MethodPost, "/auth/me/action-rules/"+bobs.ID+":test", nil, http.StatusNotFound)
	p.c.do(p.toks["alice"], http.MethodDelete, "/auth/me/action-rules/"+bobs.ID, nil, http.StatusNotFound)
	if list := string(p.c.do(p.owner, http.MethodGet, "/action-rules", nil, http.StatusOK)); strings.Contains(list, "my-displays") {
		t.Fatalf("the platform list shows a personal rule: %s", list)
	}
	p.c.do(p.owner, http.MethodGet, "/action-rules/"+bobs.ID, nil, http.StatusNotFound)

	p.c.do(p.owner, http.MethodPost, "/components/display-1/alarms", map[string]any{"severity": "high", "message": "no video"}, http.StatusCreated)
	p.c.do(p.owner, http.MethodPost, "/components/display-2/alarms", map[string]any{"severity": "high", "message": "no audio"}, http.StatusCreated)
	if a := p.inbox("alice"); len(a.Notifications) != 2 {
		t.Fatalf("alice's rule told her %d, want both", len(a.Notifications))
	}
	if b := p.inbox("bob"); len(b.Notifications) != 1 || !strings.Contains(b.Notifications[0].Subject, "no audio") {
		t.Fatalf("bob's rule told him %+v, want display-2 alone", b)
	}

	var tested struct {
		Matches []struct {
			Component string `json:"component"`
			Matched   bool   `json:"matched"`
		} `json:"matches"`
	}
	_ = json.Unmarshal(p.c.do(p.toks["bob"], http.MethodPost, "/auth/me/action-rules/my-displays:test", map[string]any{"when": "severity_rank >= 50"}, http.StatusOK), &tested)
	if len(tested.Matches) != 1 || tested.Matches[0].Component != "display-2" || tested.Matches[0].Matched {
		t.Fatalf("bob's draft :test = %+v, want his display-2 alarm, not matched at disaster", tested)
	}
	p.c.do(p.toks["bob"], http.MethodPatch, "/auth/me/action-rules/my-displays", map[string]any{"enabled": false}, http.StatusOK)
	p.c.do(p.toks["bob"], http.MethodDelete, "/auth/me/action-rules/my-displays", nil, http.StatusNoContent)
}

// TestAMemberOptsOutOfAGroupPageOnlyWhereAllowed drives group opt-out over
// HTTP (#1354).
func TestAMemberOptsOutOfAGroupPageOnlyWhereAllowed(t *testing.T) {
	p := newPeople(t)
	p.human("alice", "viewer", "all", "")
	p.human("bob", "viewer", "all", "")
	p.c.do(p.owner, http.MethodPost, "/principal-groups", map[string]any{"name": "on-call"}, http.StatusCreated)
	for _, u := range []string{"alice", "bob"} {
		p.c.do(p.owner, http.MethodPost, "/principal-groups/on-call/members", map[string]any{"principal_id": u}, http.StatusNoContent)
	}
	p.c.do(p.owner, http.MethodPost, "/notification-targets", map[string]any{"name": "on-call-page", "kind": "principal_group",
		"principal_group": "on-call", "channels": []string{"inbox"}}, http.StatusCreated)
	p.c.do(p.owner, http.MethodPost, "/action-rules", map[string]any{"name": "page-on-call", "on": []string{"raised"},
		"targets": []string{"on-call-page"}}, http.StatusCreated)
	p.c.do(p.toks["alice"], http.MethodPost, "/action-rules/page-on-call:optOut", nil, http.StatusConflict)
	p.c.do(p.owner, http.MethodPatch, "/action-rules/page-on-call", map[string]any{"opt_out_allowed": true}, http.StatusOK)
	p.c.do(p.toks["alice"], http.MethodPost, "/action-rules/page-on-call:optOut", nil, http.StatusOK)
	p.c.do(p.owner, http.MethodPost, "/components/display-1/alarms", map[string]any{"severity": "high", "message": "no video"}, http.StatusCreated)
	if a, b := p.inbox("alice"), p.inbox("bob"); len(a.Notifications) != 0 || len(b.Notifications) != 1 {
		t.Fatalf("with alice opted out: alice %d, bob %d; want 0 and 1", len(a.Notifications), len(b.Notifications))
	}
	p.c.do(p.toks["alice"], http.MethodPost, "/action-rules/page-on-call:optIn", nil, http.StatusOK)
	p.c.do(p.owner, http.MethodPost, "/components/display-2/alarms", map[string]any{"severity": "high", "message": "no audio"}, http.StatusCreated)
	if a := p.inbox("alice"); len(a.Notifications) != 1 {
		t.Fatalf("after opting back in alice has %d, want 1", len(a.Notifications))
	}
}

// A draft is tested before it is ever saved, by a person who owns no rule.
func TestADraftSubscriptionIsTestedBeforeItIsSaved(t *testing.T) {
	p := newPeople(t)
	p.human("bob", "viewer", "component", p.comps["display-2"])
	p.c.do(p.owner, http.MethodPost, "/components/display-1/alarms", map[string]any{"severity": "high", "message": "no video"}, http.StatusCreated)
	p.c.do(p.owner, http.MethodPost, "/components/display-2/alarms", map[string]any{"severity": "high", "message": "no audio"}, http.StatusCreated)
	var tested struct {
		Matches []struct {
			Component string `json:"component"`
			Matched   bool   `json:"matched"`
		} `json:"matches"`
	}
	_ = json.Unmarshal(p.c.do(p.toks["bob"], http.MethodPost, "/auth/me/action-rules:testDraft", map[string]any{"when": "severity_rank >= 40"}, http.StatusOK), &tested)
	if len(tested.Matches) != 1 || tested.Matches[0].Component != "display-2" || !tested.Matches[0].Matched {
		t.Fatalf("bob's draft :test = %+v, want his display-2 alarm, matched", tested)
	}
}
