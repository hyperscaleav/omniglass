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

type resolvedSecretResp struct {
	Name      string `json:"name"`
	OwnerKind string `json:"owner_kind"`
	OwnerName string `json:"owner_name"`
	Band      int    `json:"band"`
	Winner    bool   `json:"winner"`
	Fields    []struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Secret bool   `json:"secret"`
	} `json:"fields"`
}

// TestSecretAPI drives the secret surface over HTTP: an owner seals secrets at
// several scopes and reads the effective-secrets cascade for a component
// (masked, winner resolved), while a component-scoped viewer may read the
// cascade but is forbidden create and the all-scope directory.
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
	compRaw := c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "codec-1", "component_type": "codec", "system": "sys", "location": "room"}, http.StatusCreated)
	var comp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(compRaw, &comp)

	// The create form's shape list.
	types := c.do(ownerTok, http.MethodGet, "/secret-types", nil, http.StatusOK)
	if !bytes.Contains(types, []byte("snmp-community")) {
		t.Fatalf("secret-types missing snmp-community: %s", types)
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

	// The effective-secrets cascade for the codec: component wins over room over global.
	resolved := effectiveSecrets(t, c, ownerTok, "codec-1")
	if len(resolved) != 3 {
		t.Fatalf("resolved = %d, want 3 candidates", len(resolved))
	}
	var winner *resolvedSecretResp
	for i := range resolved {
		if resolved[i].Winner {
			if winner != nil {
				t.Fatalf("more than one winner")
			}
			winner = &resolved[i]
		}
	}
	if winner == nil || winner.OwnerKind != "component" || winner.Band != 3 {
		t.Fatalf("winner = %+v, want component band 3", winner)
	}
	// Masked over the wire: the plaintext never appears.
	for _, f := range winner.Fields {
		if f.Name == "community" {
			if !f.Secret || f.Value == "codec-community" {
				t.Errorf("community field leaked: %+v", f)
			}
			if f.Value != secret.Masked {
				t.Errorf("community value = %q, want mask", f.Value)
			}
		}
	}

	// Owner directory lists all three.
	var listed struct {
		Secrets []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			OwnerKind string `json:"owner_kind"`
		} `json:"secrets"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/secrets", nil, http.StatusOK), &listed)
	if len(listed.Secrets) != 3 {
		t.Fatalf("owner list = %d, want 3", len(listed.Secrets))
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

	// A component-scoped viewer: may read the cascade, forbidden to create, to
	// list the all-scope directory, or to reveal (secret:reveal is not on the
	// *:read floor).
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-codec", "viewer", "component", comp.ID)
	if got := effectiveSecrets(t, c, viewerTok, "codec-1"); len(got) != 3 {
		t.Errorf("viewer cascade = %d, want 3", len(got))
	}
	c.do(viewerTok, http.MethodPost, "/secrets", secretReq("nope", "component", "codec-1", "x"), http.StatusForbidden)
	c.do(viewerTok, http.MethodGet, "/secrets", nil, http.StatusForbidden)
	c.do(viewerTok, http.MethodPost, "/secrets/"+compPollID+":reveal", nil, http.StatusForbidden)
	c.do(viewerTok, http.MethodPost, "/secrets/"+compPollID+":copy", nil, http.StatusForbidden)
	c.do(viewerTok, http.MethodPatch, "/secrets/"+compPollID, map[string]any{"fields": map[string]string{"community": "x"}}, http.StatusForbidden)

	// A component-scoped operator: may seal and edit secrets in its subtree
	// (secret:create,update), but decrypt (reveal/copy) and delete stay off its
	// role, so those are 403.
	opTok := setupScopedViewer(t, ctx, dsn, "operator-codec", "operator", "component", comp.ID)
	var opCreated struct {
		ID string `json:"id"`
	}
	json.Unmarshal(c.do(opTok, http.MethodPost, "/secrets", secretReq("op-poll", "component", "codec-1", "op-community"), http.StatusCreated), &opCreated)
	if opCreated.ID == "" {
		t.Fatal("operator create returned no id")
	}
	c.do(opTok, http.MethodPatch, "/secrets/"+opCreated.ID, map[string]any{"fields": map[string]string{"community": "op-rotated"}}, http.StatusOK)
	c.do(opTok, http.MethodPost, "/secrets/"+opCreated.ID+":reveal", nil, http.StatusForbidden)
	c.do(opTok, http.MethodPost, "/secrets/"+opCreated.ID+":copy", nil, http.StatusForbidden)
	c.do(opTok, http.MethodDelete, "/secrets/"+opCreated.ID, nil, http.StatusForbidden)
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

func effectiveSecrets(t *testing.T, c *apiClient, tok, comp string) []resolvedSecretResp {
	t.Helper()
	raw := c.do(tok, http.MethodGet, "/components/"+comp+"/effective-secrets", nil, http.StatusOK)
	var out struct {
		Secrets []resolvedSecretResp `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode effective-secrets: %v", err)
	}
	return out.Secrets
}
