package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestPrincipalCreateValidation proves the server rejects a malformed username or
// email at the edge (422), so the inline form rules have a real backstop: a
// username must be a lowercase handle (no capitals, no spaces), and an email must
// look like one. A well-formed create is accepted. Skipped under -short.
func TestPrincipalCreateValidation(t *testing.T) {
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

	ownerTok := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// A username with capitals or spaces is refused.
	c.do(ownerTok, "POST", "/principals", map[string]string{"username": "Jordan"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, "POST", "/principals", map[string]string{"username": "jordan smith"}, http.StatusUnprocessableEntity)
	// A malformed email is refused.
	c.do(ownerTok, "POST", "/principals", map[string]string{"username": "jordan", "email": "not-an-email"}, http.StatusUnprocessableEntity)
	// A well-formed handle and email are accepted.
	c.do(ownerTok, "POST", "/principals", map[string]string{"username": "jordan-r", "email": "jordan@example.com"}, http.StatusCreated)

	// The same handle rule guards a group name.
	c.do(ownerTok, "POST", "/principal-groups", map[string]string{"name": "Field Crew"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, "POST", "/principal-groups", map[string]string{"name": "field-crew"}, http.StatusCreated)

	// The password policy (issue #151) guards an initial password: too short, a
	// common/denylisted password, and a password that contains the username are all
	// refused (422); a strong one is accepted.
	c.do(ownerTok, "POST", "/principals", map[string]string{"username": "shorty", "password": "abc123"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, "POST", "/principals", map[string]string{"username": "commonpw", "password": "administrator"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, "POST", "/principals", map[string]string{"username": "jordan2", "password": "my-jordan2-pass"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, "POST", "/principals", map[string]string{"username": "strongone", "password": "orange-boat-42x"}, http.StatusCreated)
}
