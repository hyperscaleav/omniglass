package api

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// accessKey is what a watch's heartbeat compares a caller's two resolutions
// by: the same grants in another order are the same access, and any change to
// a grant or to an agent's bounds is not.
func TestAccessKeyComparesGrantsAndAgentBounds(t *testing.T) {
	t.Parallel()
	loc := "loc-a"
	group := "ops"
	base := func() *storage.User {
		return &storage.User{ID: "u", Grants: []storage.Grant{
			{ID: "g1", Role: "viewer", ScopeKind: "location", ScopeID: &loc, ScopeOp: "subtree"},
			{ID: "g2", Role: "operator", ScopeKind: "all", ScopeOp: "subtree", GroupID: &group},
		}}
	}
	reordered := base()
	reordered.Grants[0], reordered.Grants[1] = reordered.Grants[1], reordered.Grants[0]
	if accessKey(base()) != accessKey(reordered) {
		t.Fatal("the same grants in another order read as a change")
	}
	other := "loc-b"
	agent := &storage.AgentCredential{CredentialID: "c", Roles: []string{"viewer", "operator"}, Channels: []string{"mcp"}}
	changes := map[string]func(u *storage.User){
		"a grant dropped":           func(u *storage.User) { u.Grants = u.Grants[:1] },
		"a role changed":            func(u *storage.User) { u.Grants[0].Role = "admin" },
		"a scope moved":             func(u *storage.User) { u.Grants[0].ScopeID = &other },
		"a scope op narrowed":       func(u *storage.User) { u.Grants[0].ScopeOp = "self" },
		"a group grant made direct": func(u *storage.User) { u.Grants[1].GroupID = nil },
		"an agent bound added":      func(u *storage.User) { u.Agent = agent },
	}
	for what, change := range changes {
		u := base()
		change(u)
		if accessKey(u) == accessKey(base()) {
			t.Errorf("%s reads as the same access", what)
		}
	}
	withAgent := base()
	withAgent.Agent = agent
	narrowed := base()
	narrowed.Agent = &storage.AgentCredential{CredentialID: "c", Roles: []string{"viewer"}, Channels: []string{"mcp"}}
	if accessKey(withAgent) == accessKey(narrowed) {
		t.Error("an agent's roles narrowed reads as the same access")
	}
}
