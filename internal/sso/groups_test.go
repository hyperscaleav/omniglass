package sso_test

import (
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/sso"
)

func claims(sub string, groups any) *auth.JWTClaims {
	c := &auth.JWTClaims{Issuer: "https://idp.example", Subject: sub, Raw: map[string]any{}}
	if groups != nil {
		c.Raw["groups"] = groups
	}
	return c
}

// A sign-in's token always syncs: no groups claim is the empty set (fail
// closed), so a provider that stops sending it removes every idp membership.
func TestSignInGroupsReadsAMissingClaimAsTheEmptySet(t *testing.T) {
	g := sso.SignInGroups(claims("alice", []any{"av"}), "groups")
	if g.Issuer != "https://idp.example" || !slices.Equal(g.Values, []string{"av"}) {
		t.Fatalf("with the claim = %+v", g)
	}
	g = sso.SignInGroups(claims("alice", nil), "groups")
	if g == nil || g.Values == nil || len(g.Values) != 0 {
		t.Fatalf("without the claim = %+v, want the empty set", g)
	}
	if g := sso.SignInGroups(claims("alice", []any{"av"}), "roles"); len(g.Values) != 0 {
		t.Fatalf("a configured claim the token lacks = %+v, want the empty set", g)
	}
}

// A refresh's token syncs only when it carries the claim and is for the
// link's own subject.
func TestRefreshGroupsSyncsOnlyTheLinksSubjectWithTheClaim(t *testing.T) {
	const link = "https://idp.example|alice"
	if g := sso.RefreshGroups(claims("alice", []any{}), link, "groups"); g == nil || len(g.Values) != 0 {
		t.Fatalf("an empty claim = %+v, want a sync to the empty set", g)
	}
	if g := sso.RefreshGroups(claims("alice", []any{"av"}), link, "groups"); g == nil || !slices.Equal(g.Values, []string{"av"}) {
		t.Fatalf("the claim = %+v", g)
	}
	for name, c := range map[string]*auth.JWTClaims{
		"no id_token":     nil,
		"no groups claim": claims("alice", nil),
		"another subject": claims("mallory", []any{"av"}),
	} {
		if g := sso.RefreshGroups(c, link, "groups"); g != nil {
			t.Errorf("%s = %+v, want no sync", name, g)
		}
	}
}
