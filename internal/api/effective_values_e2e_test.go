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

// The variable and secret cascades are the same engine as the tag cascade, and
// until now only the tag one had a route. The gateway resolvers were written,
// tested, and unreachable: no HTTP surface, so no CLI command and no console
// read. The CLI guide taught `omniglass effective-secret list` as though it
// worked (#359).
//
// The two are gated differently on purpose. A variable read rides `variable:read`
// like the variable directory; a secret read rides `secret:read`, which the viewer
// floor deliberately does not carry, and returns MASKED fields. Plaintext stays
// behind the audited `secret reveal`, and this surface is not a way around it.

type resolvedValueResp struct {
	Name      string          `json:"name"`
	OwnerKind string          `json:"owner_kind"`
	OwnerName string          `json:"owner_name"`
	Band      int             `json:"band"`
	Winner    bool            `json:"winner"`
	Value     json.RawMessage `json:"value,omitempty"`
	Fields    []struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Secret bool   `json:"secret"`
	} `json:"fields,omitempty"`
}

func TestEffectiveVariablesAPI(t *testing.T) {
	c, tok, _, _ := newCascadeFixture(t)

	got := effectiveValues(t, c, tok, "codec-1", "effective-variables")
	if len(got) == 0 {
		t.Fatal("no resolved variables: the cascade returned nothing for a component with bound values")
	}

	// The nearest owner wins; the shadowed candidate is still reported, which is
	// what makes the surface teach the cascade rather than just answer it.
	var winner *resolvedValueResp
	shadowed := 0
	for i := range got {
		if got[i].Name != "poll_interval" {
			continue
		}
		if got[i].Winner {
			winner = &got[i]
		} else {
			shadowed++
		}
	}
	if winner == nil {
		t.Fatal("poll_interval has no winner in the resolved set")
	}
	if winner.OwnerKind != "component" {
		t.Errorf("poll_interval winner owned by %q, want the component (the most specific binding)", winner.OwnerKind)
	}
	if shadowed == 0 {
		t.Error("no shadowed candidate reported: the cascade should show what it overrode, not only the answer")
	}
}

func TestEffectiveSecretsAPIMasksFields(t *testing.T) {
	c, tok, _, _ := newCascadeFixture(t)

	got := effectiveValues(t, c, tok, "codec-1", "effective-secrets")
	if len(got) == 0 {
		t.Fatal("no resolved secrets: the cascade returned nothing for a component with a bound secret")
	}
	saw := false
	for _, r := range got {
		for _, f := range r.Fields {
			if !f.Secret {
				continue
			}
			saw = true
			// The plaintext seeded by the fixture must never appear here. This
			// surface exists to explain which secret applies, not to hand out
			// its contents; reveal is the audited path.
			if f.Value == cascadeSecretPlaintext {
				t.Errorf("secret %q field %q returned PLAINTEXT on the effective read", r.Name, f.Name)
			}
		}
	}
	if !saw {
		t.Fatal("no encrypted field in the resolved secrets, so the masking assertion proves nothing")
	}
}

func TestEffectiveSecretsAPIRefusesTheViewerFloor(t *testing.T) {
	c, _, compID, dsn := newCascadeFixture(t)
	// The viewer floor carries variable:read and deliberately does not carry
	// secret:read, so the two effective reads must answer differently for it.
	viewerTok := setupScopedViewer(t, context.Background(), dsn, "viewer-codec", "viewer", "component", compID)

	// A viewer may read the variable cascade and may not read the secret one:
	// secret:read is not part of the viewer floor, and the effective read is not
	// a side door around that.
	c.do(viewerTok, http.MethodGet, "/components/codec-1/effective-variables", nil, http.StatusOK)
	c.do(viewerTok, http.MethodGet, "/components/codec-1/effective-secrets", nil, http.StatusForbidden)
}

func effectiveValues(t *testing.T, c *apiClient, tok, comp, route string) []resolvedValueResp {
	t.Helper()
	raw := c.do(tok, http.MethodGet, "/components/"+comp+"/"+route, nil, http.StatusOK)
	var out struct {
		Variables []resolvedValueResp `json:"variables"`
		Secrets   []resolvedValueResp `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", route, err)
	}
	if len(out.Variables) > 0 {
		return out.Variables
	}
	return out.Secrets
}

// cascadeSecretPlaintext is the value the fixture seals. The masking assertion
// compares against it, so it must never be what the effective read returns.
const cascadeSecretPlaintext = "s3cret-pw"

// newCascadeFixture stands up an estate with a value bound at more than one tier,
// which is the only shape that can show a winner AND a shadowed candidate. The
// component sits in a room so the location band has something in it.
func newCascadeFixture(t *testing.T) (*apiClient, string, string, string) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(
		secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "bldg", "location_type": "building"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "room", "location_type": "room", "parent": "bldg"}, http.StatusCreated)
	compRaw := c.do(tok, http.MethodPost, "/components", map[string]any{"name": "codec-1", "location": "room"}, http.StatusCreated)
	var comp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(compRaw, &comp); err != nil {
		t.Fatalf("decode component: %v", err)
	}

	// poll_interval at two tiers: the room's value is shadowed by the component's.
	c.do(tok, http.MethodPost, "/variables", map[string]any{
		"name": "poll_interval", "value_type": "int", "owner_kind": "location",
		"owner": "room", "value": 60}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/variables", map[string]any{
		"name": "poll_interval", "value_type": "int", "owner_kind": "component",
		"owner": "codec-1", "value": 30}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/secrets", map[string]any{
		"name": "device-login", "secret_type": "basic-auth", "owner_kind": "location", "owner": "room",
		"fields": map[string]any{"username": "admin", "password": cascadeSecretPlaintext}}, http.StatusCreated)
	return c, tok, comp.ID, dsn
}
