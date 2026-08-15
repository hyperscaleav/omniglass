package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"strings"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
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
	_ = json.Unmarshal(c.do(ownerTok, http.MethodPost, "/principals", map[string]any{"username": "alice", "password": "orange-boat-42x"}, http.StatusCreated), &created)
	loginBody, _ := json.Marshal(map[string]string{"username": "alice", "password": "orange-boat-42x"})
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

	// A wrong password on a REAL account is audited as login_failed, attributed to
	// that account; an attempt on an UNKNOWN username is not (no flood from scanning).
	postLogin := func(user, pw string) {
		b, _ := json.Marshal(map[string]string{"username": user, "password": pw})
		r, e := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(b))
		if e == nil {
			r.Body.Close()
		}
	}
	postLogin("alice", "wrong-password") // real account, wrong pw -> audited
	postLogin("ghost", "whatever")       // unknown user -> not audited
	failed := 0
	for _, e := range list(ownerTok, "?verb=login_failed") {
		failed++
		if e.ActorName != "alice" {
			t.Fatalf("login_failed event not attributed to alice: %+v", e)
		}
	}
	if failed != 1 {
		t.Fatalf("want exactly one login_failed (alice), got %d (an unknown-user attempt must not be audited)", failed)
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
	c.do(imp.Token, http.MethodPatch, "/auth/me", map[string]any{"label": "Alice A."}, http.StatusOK)

	impersonated := list(ownerTok, "")
	if !has(impersonated, func(e auditEvent) bool { return e.RealActorName == "root" && e.ActorName == "alice" }) {
		t.Fatalf("no impersonated event with real_actor=root, actor=alice in the audit log")
	}
}

type auditDiffEvent struct {
	Verb       string         `json:"verb"`
	Resource   string         `json:"resource"`
	ResourceID string         `json:"resource_id"`
	Old        map[string]any `json:"old"`
	New        map[string]any `json:"new"`
}

// TestAuditDiffRoundTrip pins the second half of "who changed this, and to
// what?": the audit read carries the old/new images every estate mutation has
// always written (#473). A node create/update round-trips through GET
// /audit-log as a one-sided create image plus a full before/after pair, and a
// secret create's audit body carries metadata only, never the sealed field
// material (the redaction the write side promises).
func TestAuditDiffRoundTrip(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	prov := secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(prov))
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

	c.do(ownerTok, http.MethodPost, "/nodes", map[string]any{"name": "probe-1", "label": "Probe"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/nodes/probe-1", map[string]any{"label": "Probe One"}, http.StatusOK)
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "comm", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "hunter2-sealed-material"},
	}, scope.Set{All: true}, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	list := func(query string) []auditDiffEvent {
		t.Helper()
		var out struct {
			Events []auditDiffEvent `json:"events"`
		}
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/audit-log"+query, nil, http.StatusOK), &out); err != nil {
			t.Fatalf("decode audit-log: %v", err)
		}
		return out.Events
	}

	var created, updated *auditDiffEvent
	for _, e := range list("?resource=node") {
		switch e.Verb {
		case "create":
			created = &e
		case "update":
			updated = &e
		}
	}
	if created == nil || updated == nil {
		t.Fatal("node create/update events not found")
	}
	if created.Old != nil {
		t.Errorf("node create carries an old image: %v", created.Old)
	}
	if created.New == nil || created.New["Name"] != "probe-1" {
		t.Errorf("node create new image missing or wrong: %v", created.New)
	}
	if updated.Old == nil || updated.New == nil {
		t.Fatalf("node update must carry both images, got old=%v new=%v", updated.Old, updated.New)
	}
	if updated.Old["Label"] != "Probe" || updated.New["Label"] != "Probe One" {
		t.Errorf("node update diff wrong: old=%v new=%v", updated.Old["Label"], updated.New["Label"])
	}

	secrets := list("?resource=secret")
	if len(secrets) == 0 {
		t.Fatal("secret create event not found")
	}
	raw, _ := json.Marshal(secrets[0])
	if strings.Contains(string(raw), "hunter2") || strings.Contains(string(raw), "fields") {
		t.Errorf("secret audit body leaks sealed material: %s", raw)
	}
	if secrets[0].New == nil || secrets[0].New["name"] != "comm" {
		t.Errorf("secret audit new image missing metadata: %v", secrets[0].New)
	}
}
