package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSecretAPI drives the secret surface over HTTP: an owner seals secrets at
// several scopes, reads them masked in the directory, reveals and updates them
// (plaintext only through the audited reveal / copy), while a component-scoped
// viewer is forbidden every secret surface.
func TestSecretAPI(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Estate: a room in a building, a system, and a codec at both.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "bldg", "location_type": "building"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "room", "location_type": "room", "parent": "bldg"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "sys", "system_type": "meeting-room"}, http.StatusCreated)
	compRaw := c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "codec-1", "system": "sys", "location": "room"}, http.StatusCreated)
	var comp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(compRaw, &comp)

	// The create form's shape list.
	types := c.do(ownerTok, http.MethodGet, "/types/secret", nil, http.StatusOK)
	if !bytes.Contains(types, []byte("snmp-community")) {
		t.Fatalf("secret types missing snmp-community: %s", types)
	}

	// Seal "poll" at global, room, and the component; distinct values.
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("poll", "global", "", "global-community"), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("poll", "location", "room", "room-community"), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("poll", "component", "codec-1", "codec-community"), http.StatusCreated)
	// A global secret with no all-scope grant would be forbidden; the owner has all, so it works.

	// Duplicate at the same owner is a conflict.
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("poll", "component", "codec-1", "dup"), http.StatusConflict)
	// An unknown owner is a 422.
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("poll", "location", "ghost", "x"), http.StatusUnprocessableEntity)

	// Owner directory lists all three, masked over the wire: a secret field never
	// carries plaintext in a read; the clear value comes only through the audited
	// reveal / copy below.
	var listed struct {
		Secrets []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			OwnerKind string `json:"owner_kind"`
			Fields    []struct {
				Name   string `json:"name"`
				Value  string `json:"value"`
				Secret bool   `json:"secret"`
			} `json:"fields"`
		} `json:"secrets"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/secrets", nil, http.StatusOK), &listed)
	if len(listed.Secrets) != 3 {
		t.Fatalf("owner list = %d, want 3", len(listed.Secrets))
	}
	for _, s := range listed.Secrets {
		for _, f := range s.Fields {
			if f.Secret && f.Value != secret.Masked {
				t.Errorf("secret field %s/%s not masked: %q", s.Name, f.Name, f.Value)
			}
			if f.Value == "codec-community" {
				t.Errorf("plaintext leaked in directory read: %s/%s", s.Name, f.Name)
			}
		}
	}
	// Find the component-owned poll so we know its plaintext.
	var compPollID string
	for _, s := range listed.Secrets {
		if s.Name == "poll" && s.OwnerKind == "component" {
			compPollID = s.ID
		}
	}
	if compPollID == "" {
		t.Fatal("component poll not in list")
	}

	// The owner (secret:* via >) reveals the plaintext; the decrypt round-trips.
	var revealed struct {
		Fields map[string]string `json:"fields"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/secrets/"+compPollID+":reveal", nil, http.StatusOK), &revealed)
	if revealed.Fields["community"] != "codec-community" {
		t.Errorf("revealed community = %q, want codec-community", revealed.Fields["community"])
	}
	// Copy decrypts the same plaintext, gated identically (audited under the copy verb).
	var copied struct {
		Fields map[string]string `json:"fields"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/secrets/"+compPollID+":copy", nil, http.StatusOK), &copied)
	if copied.Fields["community"] != "codec-community" {
		t.Errorf("copied community = %q, want codec-community", copied.Fields["community"])
	}

	// Update the value; the re-sealed field reveals the new plaintext.
	c.do(ownerTok, http.MethodPatch, "/secrets/"+compPollID, map[string]any{"fields": map[string]string{"community": "rotated-ro"}}, http.StatusOK)
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/secrets/"+compPollID+":reveal", nil, http.StatusOK), &revealed)
	if revealed.Fields["community"] != "rotated-ro" {
		t.Errorf("revealed community after update = %q, want rotated-ro", revealed.Fields["community"])
	}

	// A component-scoped viewer: secret is off the *:read floor, so viewer has no
	// secret:read at all. It reads none of the secret surfaces: not the directory,
	// and it cannot create, reveal, copy, or update. This is the reported IAM leak
	// closed for secrets too.
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-codec", "viewer", "component", comp.ID)
	c.do(viewerTok, http.MethodPost, "/secrets", secretReq("nope", "component", "codec-1", "x"), http.StatusForbidden)
	c.do(viewerTok, http.MethodGet, "/secrets", nil, http.StatusForbidden)
	c.do(viewerTok, http.MethodPost, "/secrets/"+compPollID+":reveal", nil, http.StatusForbidden)
	c.do(viewerTok, http.MethodPost, "/secrets/"+compPollID+":copy", nil, http.StatusForbidden)
	c.do(viewerTok, http.MethodPatch, "/secrets/"+compPollID, map[string]any{"fields": map[string]string{"community": "x"}}, http.StatusForbidden)

	// A component-scoped operator: may seal, edit, and reveal the operational
	// (non-admin-sensitive) secrets in its subtree (secret:create,update,read,reveal),
	// and its /secrets directory is scoped to that subtree. Delete stays off its role
	// (admin only), so that is 403.
	opTok := setupScopedViewer(t, ctx, dsn, "operator-codec", "operator", "component", comp.ID)
	var opCreated struct {
		ID string `json:"id"`
	}
	json.Unmarshal(c.do(opTok, http.MethodPost, "/secrets", secretReq("op-poll", "component", "codec-1", "op-community"), http.StatusCreated), &opCreated)
	if opCreated.ID == "" {
		t.Fatal("operator create returned no id")
	}
	c.do(opTok, http.MethodPatch, "/secrets/"+opCreated.ID, map[string]any{"fields": map[string]string{"community": "op-rotated"}}, http.StatusOK)
	// The operator reveals and copies its own operational secret (both in scope).
	var opRevealed struct {
		Fields map[string]string `json:"fields"`
	}
	json.Unmarshal(c.do(opTok, http.MethodPost, "/secrets/"+opCreated.ID+":reveal", nil, http.StatusOK), &opRevealed)
	if opRevealed.Fields["community"] != "op-rotated" {
		t.Errorf("operator revealed community = %q, want op-rotated", opRevealed.Fields["community"])
	}
	c.do(opTok, http.MethodPost, "/secrets/"+opCreated.ID+":copy", nil, http.StatusOK)
	// The operator's directory is scoped to its subtree: it sees the two
	// component-owned secrets (the owner's poll and its own op-poll), not the
	// global or room-owned poll placed above it.
	var opList struct {
		Secrets []struct {
			OwnerKind string `json:"owner_kind"`
		} `json:"secrets"`
	}
	json.Unmarshal(c.do(opTok, http.MethodGet, "/secrets", nil, http.StatusOK), &opList)
	if len(opList.Secrets) != 2 {
		t.Fatalf("operator scoped list = %d, want 2 (the two component-owned)", len(opList.Secrets))
	}
	for _, s := range opList.Secrets {
		if s.OwnerKind != "component" {
			t.Errorf("operator saw an out-of-subtree secret: %s", s.OwnerKind)
		}
	}
	// Delete stays admin-only.
	c.do(opTok, http.MethodDelete, "/secrets/"+opCreated.ID, nil, http.StatusForbidden)
}

// TestSecretAdminSensitive proves the second visibility axis: a per-secret
// admin_sensitive flag flips a secret to the :admin tier, so a platform credential
// stays admin/owner-only even at the same scope as an operational device secret an
// operator can freely see and reveal. Placement scope (axis 1) still fences where,
// independently. A component-scoped operator and a component-scoped admin share the
// same scope, so the flag, not scope, is what separates their reach.
func TestSecretAdminSensitive(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Estate: a room in a building, a system, and a codec at both.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "bldg", "location_type": "building"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "room", "location_type": "room", "parent": "bldg"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "sys", "system_type": "meeting-room"}, http.StatusCreated)
	compRaw := c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "codec-1", "system": "sys", "location": "room"}, http.StatusCreated)
	var comp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(compRaw, &comp)

	// Owner seals three secrets:
	//   dev @ codec-1       operational device secret (snmp, type default not sensitive)
	//   zoom @ codec-1      platform credential (oauth2-client, type default admin-sensitive)
	//   global-dev @ global operational device secret placed above the operator's scope
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("dev", "component", "codec-1", "dev-community"), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/secrets", oauth2Req("zoom", "component", "codec-1"), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("global-dev", "global", "", "global-community"), http.StatusCreated)

	// The owner directory shows all three, with the admin_sensitive flag surfaced.
	owned := listSecrets(t, c, ownerTok)
	if len(owned) != 3 {
		t.Fatalf("owner list = %d, want 3", len(owned))
	}
	var zoomID, globalDevID string
	for _, s := range owned {
		switch s.Name {
		case "zoom":
			zoomID = s.ID
			if !s.AdminSensitive {
				t.Error("zoom should be admin_sensitive by its type default")
			}
		case "global-dev":
			globalDevID = s.ID
		case "dev":
			if s.AdminSensitive {
				t.Error("dev (snmp) should not be admin_sensitive")
			}
		}
	}
	if zoomID == "" || globalDevID == "" {
		t.Fatal("owner list missing zoom/global-dev")
	}

	// The owner reveals the platform credential (owner `>` reaches the admin tier).
	var ownerReveal struct {
		Fields map[string]string `json:"fields"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/secrets/"+zoomID+":reveal", nil, http.StatusOK), &ownerReveal)
	if ownerReveal.Fields["client_secret"] != "zoom-secret" {
		t.Errorf("owner revealed client_secret = %q, want zoom-secret", ownerReveal.Fields["client_secret"])
	}

	// A component-scoped OPERATOR (secret:read,reveal,create,update, no admin tier).
	opTok := setupScopedViewer(t, ctx, dsn, "operator-codec", "operator", "component", comp.ID)
	// Its directory shows only the operational, in-subtree secret: the admin-sensitive
	// zoom is filtered out (its existence and field names never leak), and the global
	// dev secret is out of scope.
	opList := listSecrets(t, c, opTok)
	if len(opList) != 1 || opList[0].Name != "dev" {
		t.Fatalf("operator list = %+v, want just the operational dev secret", opList)
	}
	// The admin-sensitive secret is a non-disclosing 404 on reveal (not a 403), so its
	// existence does not leak, even though it sits within the operator's scope.
	c.do(opTok, http.MethodPost, "/secrets/"+zoomID+":reveal", nil, http.StatusNotFound)
	// The out-of-scope global device secret is likewise a non-disclosing 404.
	c.do(opTok, http.MethodPost, "/secrets/"+globalDevID+":reveal", nil, http.StatusNotFound)
	// The operator reveals the operational secret in its scope.
	var opReveal struct {
		Fields map[string]string `json:"fields"`
	}
	json.Unmarshal(c.do(opTok, http.MethodPost, "/secrets/"+opList[0].ID+":reveal", nil, http.StatusOK), &opReveal)
	if opReveal.Fields["community"] != "dev-community" {
		t.Errorf("operator revealed community = %q, want dev-community", opReveal.Fields["community"])
	}
	// The operator cannot mint an admin-sensitive secret: by the type default...
	c.do(opTok, http.MethodPost, "/secrets", oauth2Req("op-zoom", "component", "codec-1"), http.StatusForbidden)
	// ...nor by asking for it explicitly on an operational type.
	c.do(opTok, http.MethodPost, "/secrets", secretReqAdmin("op-sensitive", "component", "codec-1", "x", true), http.StatusForbidden)
	// But it may create an operational one (explicit admin_sensitive=false).
	c.do(opTok, http.MethodPost, "/secrets", secretReqAdmin("op-ok", "component", "codec-1", "y", false), http.StatusCreated)

	// A component-scoped ADMIN (secret:> grants the admin tier at the SAME scope as
	// the operator): the sensitivity axis, not scope, is what separates them.
	admTok := setupScopedViewer(t, ctx, dsn, "admin-codec", "admin", "component", comp.ID)
	admList := listSecrets(t, c, admTok)
	// The admin sees every in-subtree secret, including the admin-sensitive zoom the
	// operator could not (dev, zoom, op-ok). The global one is out of scope.
	if len(admList) != 3 {
		t.Fatalf("admin scoped list = %d, want 3 (dev, zoom, op-ok)", len(admList))
	}
	sawZoom := false
	for _, s := range admList {
		if s.Name == "zoom" {
			sawZoom = true
		}
	}
	if !sawZoom {
		t.Error("admin should see the admin-sensitive zoom within its scope")
	}
	// The admin reveals the platform credential (admin tier plus in scope).
	var admReveal struct {
		Fields map[string]string `json:"fields"`
	}
	json.Unmarshal(c.do(admTok, http.MethodPost, "/secrets/"+zoomID+":reveal", nil, http.StatusOK), &admReveal)
	if admReveal.Fields["client_secret"] != "zoom-secret" {
		t.Errorf("admin revealed client_secret = %q, want zoom-secret", admReveal.Fields["client_secret"])
	}
}

func secretReq(name, ownerKind, owner, community string) map[string]any {
	body := map[string]any{
		"name":        name,
		"secret_type": "snmp-community",
		"owner_kind":  ownerKind,
		"fields":      map[string]string{"community": community},
	}
	if owner != "" {
		body["owner"] = owner
	}
	return body
}

// oauth2Req builds a create body for the OAuth2 client type, whose type default is
// admin-sensitive (a platform integration credential).
func oauth2Req(name, ownerKind, owner string) map[string]any {
	body := map[string]any{
		"name":        name,
		"secret_type": "oauth2-client",
		"owner_kind":  ownerKind,
		"fields":      map[string]string{"client_id": name + "-id", "client_secret": name + "-secret"},
	}
	if owner != "" {
		body["owner"] = owner
	}
	return body
}

// secretReqAdmin is secretReq with an explicit admin_sensitive override, for
// exercising the create-tier gate on an operational type.
func secretReqAdmin(name, ownerKind, owner, community string, adminSensitive bool) map[string]any {
	body := secretReq(name, ownerKind, owner, community)
	body["admin_sensitive"] = adminSensitive
	return body
}

type secretListItem struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	OwnerKind      string `json:"owner_kind"`
	AdminSensitive bool   `json:"admin_sensitive"`
}

func listSecrets(t *testing.T, c *apiClient, tok string) []secretListItem {
	t.Helper()
	raw := c.do(tok, http.MethodGet, "/secrets", nil, http.StatusOK)
	var out struct {
		Secrets []secretListItem `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode secrets list: %v", err)
	}
	return out.Secrets
}
