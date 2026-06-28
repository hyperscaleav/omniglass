package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestAuthMeAndRoles is the slice's end-to-end proof: a bootstrapped owner
// authenticates and reads /auth/me and /roles; an unauthenticated call is 401;
// and a principal whose role lacks role:read is 403 on /roles but still 200 on
// /auth/me (authn does not require a capability). Skipped under -short.
func TestAuthMeAndRoles(t *testing.T) {
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

	ownerTok, ownerHash, ownerPrefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner token: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{
		Username: "root", SecretHash: ownerHash, Prefix: ownerPrefix,
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	limitedTok := setupLimitedPrincipal(t, ctx, dsn)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	get := func(path, token string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 0)
		dec := json.NewDecoder(resp.Body)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == nil {
			buf = raw
		}
		return resp.StatusCode, buf
	}

	// Unauthenticated.
	if code, _ := get("/api/v1/auth/me", ""); code != http.StatusUnauthorized {
		t.Errorf("/auth/me without token = %d, want 401", code)
	}
	if code, _ := get("/api/v1/auth/me", "ogp_bogus_token"); code != http.StatusUnauthorized {
		t.Errorf("/auth/me with bad token = %d, want 401", code)
	}

	// Owner /auth/me: 200 with the human identity, the owner@all grant, and a
	// non-empty permission set.
	code, body := get("/api/v1/auth/me", ownerTok)
	if code != http.StatusOK {
		t.Fatalf("/auth/me as owner = %d, want 200 (body %s)", code, body)
	}
	var me struct {
		Principal struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"principal"`
		Human *struct {
			Username string `json:"username"`
		} `json:"human"`
		Permissions []string `json:"permissions"`
		Grants      []struct {
			Role      string `json:"role"`
			ScopeKind string `json:"scope_kind"`
		} `json:"grants"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decode /auth/me: %v", err)
	}
	if me.Principal.Kind != "human" || me.Human == nil || me.Human.Username != "root" {
		t.Errorf("/auth/me principal = %+v, human = %+v, want human root", me.Principal, me.Human)
	}
	if len(me.Permissions) == 0 {
		t.Error("/auth/me owner permissions empty")
	}
	hasOwnerAll := false
	for _, g := range me.Grants {
		if g.Role == "owner" && g.ScopeKind == "all" {
			hasOwnerAll = true
		}
	}
	if !hasOwnerAll {
		t.Errorf("/auth/me grants = %+v, want an owner@all grant", me.Grants)
	}

	// Owner can list roles (owner *:* allows role:read).
	if code, _ := get("/api/v1/roles", ownerTok); code != http.StatusOK {
		t.Errorf("/roles as owner = %d, want 200", code)
	}

	// The limited principal can see itself but cannot list roles.
	if code, _ := get("/api/v1/auth/me", limitedTok); code != http.StatusOK {
		t.Errorf("/auth/me as limited = %d, want 200", code)
	}
	if code, _ := get("/api/v1/roles", limitedTok); code != http.StatusForbidden {
		t.Errorf("/roles as limited = %d, want 403", code)
	}
}

// setupLimitedPrincipal inserts a service principal whose only grant is a custom
// role with alarm:ack (and so no role:read, not even via the read floor, since
// it holds no permission on the role resource), and returns its bearer token.
func setupLimitedPrincipal(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx,
		`insert into role (id, official, permissions, inherits) values ('limited', false, $1, '{}')`,
		[]string{"alarm:ack"}); err != nil {
		t.Fatalf("insert limited role: %v", err)
	}
	var pid string
	if err := conn.QueryRow(ctx,
		`insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into service (principal_id, label) values ($1, 'limited-svc')`, pid); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint limited token: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		pid, hash, prefix); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into principal_grant (principal_id, role_id, scope_kind) values ($1, 'limited', 'all')`,
		pid); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	return tok
}
