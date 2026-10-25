package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Minting a token and revoking it each land on GET /audit-log (#1162): a token
// is how an MCP client or a script is given access, so the trail names who
// minted it and who ended it, by the credential's id, and never carries the
// token. Skipped under -short.
func TestMintingAndRevokingATokenAreAudited(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	c, ownerTok := auditImagesStack(t)

	var minted struct {
		Token  string `json:"token"`
		Prefix string `json:"prefix"`
	}
	raw := c.do(ownerTok, http.MethodPost, "/auth/me/tokens", map[string]any{"description": "an agent's token"}, http.StatusCreated)
	if err := json.Unmarshal(raw, &minted); err != nil || minted.Token == "" {
		t.Fatalf("mint = %s (%v)", raw, err)
	}

	type event struct {
		Verb       string         `json:"verb"`
		ActorName  string         `json:"actor_name"`
		ResourceID string         `json:"resource_id"`
		Old        map[string]any `json:"old"`
		New        map[string]any `json:"new"`
		Channel    string         `json:"channel"`
	}
	trail := func(verb string) []event {
		t.Helper()
		var out struct {
			Events []event `json:"events"`
		}
		raw := c.do(ownerTok, http.MethodGet, "/audit-log?resource=credential&verb="+verb, nil, http.StatusOK)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode audit-log: %v (%s)", err, raw)
		}
		return out.Events
	}

	// The newest row is the token just minted; the owner's own bootstrap token
	// was minted (with no actor, from the trusted lane) before it.
	created := trail("create")
	if len(created) != 2 || created[1].ActorName != "" || created[1].New["description"] != "bootstrap token" {
		t.Fatalf("credential create rows = %+v, want the minted token's and the bootstrap token's", created)
	}
	if created[0].ActorName != "root" || created[0].ResourceID == "" || created[0].New["prefix"] != minted.Prefix {
		t.Errorf("create row = %+v, want root minting the token with prefix %s", created[0], minted.Prefix)
	}
	if created[0].Channel != "" {
		t.Errorf("a direct API mint reads channel %q, want none", created[0].Channel)
	}
	if containsToken(created[0].New, minted.Token) {
		t.Errorf("the create row carries the token itself: %+v", created[0].New)
	}

	c.do(ownerTok, http.MethodPost, "/auth/me/sessions/"+created[0].ResourceID+":revoke", nil, http.StatusNoContent)
	revoked := trail("revoke")
	if len(revoked) != 1 || revoked[0].ResourceID != created[0].ResourceID || revoked[0].ActorName != "root" {
		t.Fatalf("credential revoke rows = %+v, want root ending the minted token", revoked)
	}
	if code, _ := c.send(minted.Token, http.MethodGet, "/auth/me", nil); code != http.StatusUnauthorized {
		t.Errorf("the revoked token still authenticates: %d", code)
	}
}

func containsToken(img map[string]any, token string) bool {
	for _, v := range img {
		if s, ok := v.(string); ok && s == token {
			return true
		}
	}
	return false
}
