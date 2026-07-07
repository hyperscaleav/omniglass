package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type auditEvent struct {
	Verb          string `json:"verb"`
	Resource      string `json:"resource"`
	Actor         string `json:"actor"`
	ActorName     string `json:"actor_name"`
	RealActor     string `json:"real_actor"`
	RealActorName string `json:"real_actor_name"`
}

// TestAuditLogAPI drives the audit read surface end to end: a login is captured
// as an auth event, GET /audit-log lists it (gated by audit:read, so a viewer is
// 403 and the owner 200), a resource filter narrows to auth events, and an action
// taken while impersonating records the real actor, which the surface returns.
func TestAuditLogAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	list := func(tok, query string) []auditEvent {
		var out struct {
			Events []auditEvent `json:"events"`
		}
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/audit-log"+query, nil, http.StatusOK), &out); err != nil {
			t.Fatalf("decode audit-log: %v", err)
		}
		return out.Events
	}
	has := func(evs []auditEvent, pred func(auditEvent) bool) bool {
		for _, e := range evs {
			if pred(e) {
				return true
			}
		}
		return false
	}

	// Create a human with a password (an audited create), then sign it in.
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(c.do(ownerTok, http.MethodPost, "/principals", map[string]any{"username": "alice", "password": "alice-s3cret"}, http.StatusCreated), &created)
	loginBody, _ := json.Marshal(map[string]string{"username": "alice", "password": "alice-s3cret"})
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("alice login: err %v, status %v", err, resp.StatusCode)
	}
	resp.Body.Close()

	// The owner sees the login event (captured as verb=login, resource=auth,
	// attributed to alice) and the create-principal event.
	all := list(ownerTok, "")
	if !has(all, func(e auditEvent) bool { return e.Verb == "login" && e.Resource == "auth" && e.ActorName == "alice" }) {
		t.Fatalf("no login event for alice in %+v", all)
	}
	if !has(all, func(e auditEvent) bool { return e.Verb == "create" && e.Resource == "principal" }) {
		t.Fatalf("no create-principal event in the audit log")
	}

	// The resource filter narrows to auth events only.
	authOnly := list(ownerTok, "?resource=auth")
	if len(authOnly) == 0 {
		t.Fatal("resource=auth filter returned nothing")
	}
	for _, e := range authOnly {
		if e.Resource != "auth" {
			t.Fatalf("resource=auth filter leaked a %q event", e.Resource)
		}
	}

	// audit:read gates the surface: a viewer (read on everything else) cannot read it.
	viewerTok := principalWithGrants(t, ctx, dsn, "an-auditor-not", []grant{{role: "viewer", scopeKind: "all"}})
	if code, _ := c.send(viewerTok, http.MethodGet, "/audit-log", nil); code != http.StatusForbidden {
		t.Fatalf("viewer reading audit-log: want 403 (no audit:read), got %d", code)
	}

	// An action taken while impersonating records the real actor. The owner acts as
	// alice and edits alice's profile; the audit row names alice as actor and the
	// owner (root) as the real actor.
	code, body := c.send(ownerTok, http.MethodPost, "/principals/"+created.ID+":impersonate", map[string]any{"mode": "act_as"})
	if code != http.StatusCreated {
		t.Fatalf("impersonate alice: %d (%s)", code, body)
	}
	var imp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(body, &imp)
	c.do(imp.Token, http.MethodPatch, "/auth/me", map[string]any{"display_name": "Alice A."}, http.StatusOK)

	impersonated := list(ownerTok, "")
	if !has(impersonated, func(e auditEvent) bool { return e.RealActorName == "root" && e.ActorName == "alice" }) {
		t.Fatalf("no impersonated event with real_actor=root, actor=alice in the audit log")
	}
}
