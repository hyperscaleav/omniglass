package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestEnvSecretAPI drives an env-backed platform secret (#2223) over HTTP: it
// is listed with source env, its type and no fields, and an update, a delete
// or a reveal of it is a 409 naming the variable to change instead.
func TestEnvSecretAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	decl, err := secret.ParseEnv([]string{`OMNIGLASS_SECRET__BEDROCK_KEY={"type":"api-key","fields":{"key":"sk-bedrock"}}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.InstallEnvSecrets(ctx, decl); err != nil {
		t.Fatal(err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	raw := c.do(ownerTok, http.MethodGet, "/secrets", nil, http.StatusOK)
	if bytes.Contains(raw, []byte("sk-bedrock")) {
		t.Fatalf("the list carries the env value: %s", raw)
	}
	var listed struct {
		Secrets []struct {
			Name       string            `json:"name"`
			SecretType string            `json:"secret_type"`
			Source     string            `json:"source"`
			SourceVar  string            `json:"source_var"`
			Fields     []json.RawMessage `json:"fields"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Secrets) != 1 {
		t.Fatalf("listed = %s", raw)
	}
	s := listed.Secrets[0]
	if s.Name != "bedrock-key" || s.SecretType != "api-key" || s.Source != "env" || s.SourceVar != "OMNIGLASS_SECRET__BEDROCK_KEY" || len(s.Fields) != 0 {
		t.Fatalf("env secret over the wire = %+v", s)
	}

	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/secrets/bedrock-key", map[string]any{"fields": map[string]string{"key": "x"}}},
		{http.MethodDelete, "/secrets/bedrock-key", nil},
		{http.MethodPost, "/secrets/bedrock-key:reveal", nil},
		{http.MethodPost, "/secrets", map[string]any{"name": "bedrock-key", "secret_type": "api-key", "owner_kind": "platform", "fields": map[string]string{"key": "x"}}},
	} {
		got := c.do(ownerTok, call.method, call.path, call.body, http.StatusConflict)
		if !strings.Contains(string(got), "OMNIGLASS_SECRET__BEDROCK_KEY") || strings.Contains(string(got), "sk-bedrock") {
			t.Fatalf("%s %s = %s, want a 409 naming the variable", call.method, call.path, got)
		}
	}
}
