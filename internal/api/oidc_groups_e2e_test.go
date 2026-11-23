package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/sso"
)

// Identity provider groups map to Omniglass groups (#2363, ADR-2361 point 4):
// an admin maps a value of the groups claim, keyed by issuer, to a user group,
// and each SSO sign-in (and each re-check refresh whose id_token carries the
// claim) sets the person's idp memberships from that issuer to exactly the
// mapped set, so the group's scoped grants follow the provider's groups.

// groupFleet places one component on each of two campuses and gives the
// group bldg-support a viewer grant over the first, mapped from av-support.
func groupFleet(t *testing.T, s *ssoStack) {
	t.Helper()
	for _, loc := range []string{"hq", "annex"} {
		s.c.do(s.owner, http.MethodPost, "/locations", map[string]any{"name": loc, "location_type": "campus"}, http.StatusCreated)
		s.c.do(s.owner, http.MethodPost, "/components", map[string]any{"name": "cam-" + loc, "product": "generic-device", "location": loc}, http.StatusCreated)
	}
	s.c.do(s.owner, http.MethodPost, "/user-groups", map[string]any{"name": "bldg-support"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/user-groups/bldg-support/grants",
		map[string]any{"role": "viewer", "scope_kind": "location", "scope_id": "hq"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/user-groups/bldg-support/idp-mappings", map[string]any{"claim_value": "av-support"}, http.StatusCreated)
}

type groupMember struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Issuer string `json:"issuer"`
}

// members is a group's roster keyed by member name.
func (s *ssoStack) members(t *testing.T, group string) map[string]groupMember {
	t.Helper()
	var out struct {
		Members []groupMember `json:"members"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/user-groups/"+group+"/members", nil, http.StatusOK), &out); err != nil {
		t.Fatal(err)
	}
	m := map[string]groupMember{}
	for _, x := range out.Members {
		m[x.Name] = x
	}
	return m
}

// reads is the status each of the two components answers tok.
func (s *ssoStack) reads(t *testing.T, tok string) (hq, annex int) {
	t.Helper()
	hq, _ = s.c.send(tok, http.MethodGet, "/components/cam-hq", nil)
	annex, _ = s.c.send(tok, http.MethodGet, "/components/cam-annex", nil)
	return hq, annex
}

func TestSSOGroupsClaimMapsToAGroupAndItsScopedGrantsFollow(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	groupFleet(t, s)
	alice := s.person(t, "alice")
	s.link(t, alice, "alice-sub")
	// A local membership stands in for an idp one and is never touched.
	s.c.do(s.owner, http.MethodPost, "/user-groups", map[string]any{"name": "local-team"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/user-groups/local-team/members", map[string]any{"user_id": "alice"}, http.StatusNoContent)
	s.c.do(s.owner, http.MethodPost, "/user-groups/local-team/idp-mappings", map[string]any{"claim_value": "av-support"}, http.StatusCreated)

	var maps struct {
		Mappings []struct {
			Issuer     string `json:"issuer"`
			ClaimValue string `json:"claim_value"`
		} `json:"mappings"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/user-groups/bldg-support/idp-mappings", nil, http.StatusOK), &maps); err != nil ||
		len(maps.Mappings) != 1 || maps.Mappings[0].Issuer != s.idp.Issuer() || maps.Mappings[0].ClaimValue != "av-support" {
		t.Fatalf("mappings = %+v (%v), want av-support keyed by the configured issuer", maps.Mappings, err)
	}

	// Signed in with the mapped group: an idp membership, and the group's
	// location-scoped read follows.
	s.idp.SignIn("alice-sub", map[string]any{"groups": []any{"av-support", "unmapped"}})
	b := s.browser(t)
	if to := b.signIn(); to != s.base+"/web/" {
		t.Fatalf("sign-in landed on %q", to)
	}
	m := s.members(t, "bldg-support")["alice"]
	if m.Source != "idp" || m.Issuer != s.idp.Issuer() {
		t.Fatalf("bldg-support member alice = %+v, want source idp from the provider", m)
	}
	if hq, annex := s.reads(t, b.session()); hq != http.StatusOK || annex != http.StatusNotFound {
		t.Fatalf("reads with the mapped group = hq %d, annex %d; want 200 in scope, 404 outside", hq, annex)
	}
	if lm := s.members(t, "local-team")["alice"]; lm.Source != "local" {
		t.Fatalf("local-team member alice = %+v, want the local row standing", lm)
	}
	if err := s.query(t, `select 1 from audit_log where resource = 'user_group_member' and verb = 'create' and new->>'source' = 'idp' and new->>'user_id' = $1`, alice).Scan(new(int)); err != nil {
		t.Fatalf("the idp membership is not audited: %v", err)
	}

	// Signed in again with no groups claim at all: the empty set (fail
	// closed), so the idp membership and its access go; the local row stays.
	s.idp.SignIn("alice-sub", nil)
	b2 := s.browser(t)
	b2.signIn()
	if _, ok := s.members(t, "bldg-support")["alice"]; ok {
		t.Fatal("alice is still a bldg-support member after the claim dropped the group")
	}
	for name, tok := range map[string]string{"old session": b.session(), "new session": b2.session()} {
		// No grant is left at all, so component:read itself is refused.
		if hq, _ := s.reads(t, tok); hq != http.StatusForbidden {
			t.Errorf("%s reads cam-hq: %d after the group dropped, want 403", name, hq)
		}
	}
	if lm, ok := s.members(t, "local-team")["alice"]; !ok || lm.Source != "local" {
		t.Fatalf("local-team member alice = %+v (present %v), want the local row untouched", lm, ok)
	}
	if err := s.query(t, `select 1 from audit_log where resource = 'user_group_member' and verb = 'delete' and old->>'source' = 'idp' and old->>'user_id' = $1`, alice).Scan(new(int)); err != nil {
		t.Fatalf("the idp membership's removal is not audited: %v", err)
	}
}

func TestSSOGroupMappingMayNotReachAnOwner(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	// A mapping to a group that carries an owner grant, at any scope, is refused.
	s.c.do(s.owner, http.MethodPost, "/user-groups", map[string]any{"name": "owners"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/user-groups/owners/grants", map[string]any{"role": "owner", "scope_kind": "all"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/user-groups/owners/idp-mappings", map[string]any{"claim_value": "it-admins"}, http.StatusConflict)

	// And an owner grant on a group that has a mapping is refused, while a
	// lesser grant is not.
	s.c.do(s.owner, http.MethodPost, "/user-groups", map[string]any{"name": "mapped"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/user-groups/mapped/idp-mappings", map[string]any{"claim_value": "av-support"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/user-groups/mapped/grants", map[string]any{"role": "owner", "scope_kind": "all"}, http.StatusConflict)
	s.c.do(s.owner, http.MethodPost, "/user-groups/mapped/grants", map[string]any{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)
	if err := s.query(t, `select 1 from user_grant g join role r on r.id = g.role_id join user_group ug on ug.id = g.group_id
		where ug.name = 'mapped' and r.name = 'owner'`).Scan(new(int)); err == nil {
		t.Fatal("the refused owner grant was stored")
	}
}

func TestSSORecheckRefreshDropsAGroupTheProviderRemoved(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	groupFleet(t, s)
	alice := s.person(t, "alice")
	s.link(t, alice, "alice-sub")
	s.idp.SignIn("alice-sub", map[string]any{"groups": []any{"av-support"}})
	b := s.browser(t)
	b.signIn()
	if hq, _ := s.reads(t, b.session()); hq != http.StatusOK {
		t.Fatalf("read in scope after sign-in: %d", hq)
	}
	sw := sso.NewSweeper(s.gw, egress.Options{RootCAs: s.idp.Roots()}, nil)

	// A refresh whose id_token still carries the group keeps the membership.
	if p, _, err := sw.RunOnce(s.ctx); err != nil || p.Accepted != 1 {
		t.Fatalf("first pass = %+v, %v", p, err)
	}
	if m := s.members(t, "bldg-support")["alice"]; m.Source != "idp" {
		t.Fatalf("alice after an unchanged refresh = %+v, want the idp membership", m)
	}

	// The provider removes the group: the next refresh's id_token lacks it.
	s.idp.Regroup("alice-sub", map[string]any{"groups": []any{"other"}})
	if p, _, err := sw.RunOnce(s.ctx); err != nil || p.Accepted != 1 {
		t.Fatalf("pass after the group removal = %+v, %v", p, err)
	}
	if _, ok := s.members(t, "bldg-support")["alice"]; ok {
		t.Fatal("alice is still a member after a refresh whose id_token dropped the group")
	}
	if hq, _ := s.reads(t, b.session()); hq != http.StatusForbidden {
		t.Fatalf("read after the refresh dropped the group: %d, want 403", hq)
	}
}

func TestSSOGroupMappingRemovalAndAnAdminAddMakeTheMembershipLocal(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	groupFleet(t, s)
	alice := s.person(t, "alice")
	bob := s.person(t, "bob")
	s.link(t, alice, "alice-sub")
	s.link(t, bob, "bob-sub")
	for _, sub := range []string{"alice-sub", "bob-sub"} {
		s.idp.SignIn(sub, map[string]any{"groups": []any{"av-support"}})
		s.browser(t).signIn()
	}
	// An admin adding a member the provider brought makes the row local, so
	// it no longer follows the provider.
	s.c.do(s.owner, http.MethodPost, "/user-groups/bldg-support/members", map[string]any{"user_id": "bob"}, http.StatusNoContent)
	if m := s.members(t, "bldg-support")["bob"]; m.Source != "local" || m.Issuer != "" {
		t.Fatalf("bob after an admin add = %+v, want local", m)
	}
	// Removing the group's last mapping from an issuer removes that issuer's
	// idp memberships at once: nothing can justify them any more.
	var maps struct {
		Mappings []struct {
			ID string `json:"id"`
		} `json:"mappings"`
	}
	_ = json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/user-groups/bldg-support/idp-mappings", nil, http.StatusOK), &maps)
	if len(maps.Mappings) != 1 {
		t.Fatalf("mappings = %+v", maps.Mappings)
	}
	s.c.do(s.owner, http.MethodDelete, "/user-groups/bldg-support/idp-mappings/"+maps.Mappings[0].ID, nil, http.StatusNoContent)
	got := s.members(t, "bldg-support")
	if _, ok := got["alice"]; ok {
		t.Fatal("alice's idp membership outlived the group's last mapping")
	}
	if got["bob"].Source != "local" {
		t.Fatalf("bob = %+v, want the local row untouched", got["bob"])
	}
}
