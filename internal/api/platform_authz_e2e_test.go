package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// estateWriterPerms is the estate authority both principals in the platform-tier
// test hold: everything needed to write a variable, a secret, a tag value, and a
// settings namespace. The only difference between the two roles built from it is
// platform:*, which is exactly what the tier gate turns on.
var estateWriterPerms = []string{
	"variable:*",
	"secret:>",
	"tag:*",
	"location:update",
	"settings:read,update",
}

// TestPlatformTierNeedsItsOwnPermission pins the separation the platform tier
// exists for: an ALL scope is full-estate reach, not install-wide authority. A
// principal holding every estate permission at the all scope must be refused at
// the least-specific tier (variables, secrets, tag bindings, settings) and
// allowed everywhere below it; the same principal plus platform:* is allowed at
// the tier too. Without the second gate, the first half of every pair passes,
// which is the pre-slice behaviour this test rejects.
func TestPlatformTierNeedsItsOwnPermission(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Both custom roles must exist before the first authenticated request, which
	// is what loads the role index (lazy, once per handler).
	insertRole(t, ctx, dsn, "estate-writer", estateWriterPerms)
	insertRole(t, ctx, dsn, "install-writer", append(append([]string{}, estateWriterPerms...), "platform:*"))

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	estateTok := principalWithGrants(t, ctx, dsn, "estate-writer-svc", []grant{{role: "estate-writer", scopeKind: "all"}})
	installTok := principalWithGrants(t, ctx, dsn, "install-writer-svc", []grant{{role: "install-writer", scopeKind: "all"}})

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// The estate the below-tier writes land on, and a tag key to bind.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "ceres", "location_type": "campus"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "environment"}, http.StatusCreated)
	// A tier variable and a tier secret the update and delete legs act on, placed
	// by the owner (whose > tail carries the platform permission).
	tierVarID := createdID(t, c.do(ownerTok, http.MethodPost, "/variables",
		varReq("tier_poll", "int", "platform", "", 10), http.StatusCreated))
	tierSecretID := createdID(t, c.do(ownerTok, http.MethodPost, "/secrets",
		secretReq("tier_auth", "platform", "", "public"), http.StatusCreated))

	// Refused at the tier: create, update, and delete of a variable.
	c.do(estateTok, http.MethodPost, "/variables", varReq("denied_at_tier", "string", "platform", "", "x"), http.StatusForbidden)
	c.do(estateTok, http.MethodPatch, "/variables/"+tierVarID, map[string]any{"value": 99}, http.StatusForbidden)
	c.do(estateTok, http.MethodDelete, "/variables/"+tierVarID, nil, http.StatusForbidden)

	// Allowed below the tier: the same principal owns the estate outright.
	belowID := createdID(t, c.do(estateTok, http.MethodPost, "/variables",
		varReq("allowed_below", "string", "location", "ceres", "x"), http.StatusCreated))
	c.do(estateTok, http.MethodPatch, "/variables/"+belowID, map[string]any{"value": "y"}, http.StatusOK)
	c.do(estateTok, http.MethodDelete, "/variables/"+belowID, nil, http.StatusNoContent)

	// The same split on secrets, across all three verbs: the tier gate on update and
	// delete lives in the Gateway (only the stored row knows its tier), so it needs
	// its own coverage rather than riding on the create leg.
	c.do(estateTok, http.MethodPost, "/secrets", secretReq("tier_snmp", "platform", "", "public"), http.StatusForbidden)
	c.do(estateTok, http.MethodPatch, "/secrets/"+tierSecretID, secretFieldsReq("rotated"), http.StatusForbidden)
	c.do(estateTok, http.MethodDelete, "/secrets/"+tierSecretID, nil, http.StatusForbidden)

	// Allowed below the tier, on the same three verbs: the gate is about the tier,
	// not about the principal.
	belowSecretID := createdID(t, c.do(estateTok, http.MethodPost, "/secrets",
		secretReq("below_snmp", "location", "ceres", "public"), http.StatusCreated))
	c.do(estateTok, http.MethodPatch, "/secrets/"+belowSecretID, secretFieldsReq("rotated"), http.StatusOK)
	c.do(estateTok, http.MethodDelete, "/secrets/"+belowSecretID, nil, http.StatusNoContent)

	c.do(estateTok, http.MethodPost, "/tags/environment:setPlatform", map[string]any{"value": "prod"}, http.StatusForbidden)
	c.do(estateTok, http.MethodPost, "/tags/environment:clearPlatform", nil, http.StatusForbidden)
	c.do(estateTok, http.MethodPost, "/locations/ceres:setTag", map[string]any{"key": "environment", "value": "prod"}, http.StatusOK)
	c.do(estateTok, http.MethodPatch, "/settings/ui", map[string]any{"theme": "omniglass-light"}, http.StatusForbidden)
	c.do(estateTok, http.MethodDelete, "/settings/ui", nil, http.StatusForbidden)
	c.do(estateTok, http.MethodPost, "/settings:restoreDefaults", nil, http.StatusForbidden)

	// platform:* opens every one of them, with no other change to the grant.
	c.do(installTok, http.MethodPost, "/variables", varReq("allowed_at_tier", "string", "platform", "", "x"), http.StatusCreated)
	c.do(installTok, http.MethodPatch, "/variables/"+tierVarID, map[string]any{"value": 99}, http.StatusOK)
	c.do(installTok, http.MethodDelete, "/variables/"+tierVarID, nil, http.StatusNoContent)
	c.do(installTok, http.MethodPost, "/secrets", secretReq("tier_snmp", "platform", "", "public"), http.StatusCreated)
	c.do(installTok, http.MethodPatch, "/secrets/"+tierSecretID, secretFieldsReq("rotated"), http.StatusOK)
	c.do(installTok, http.MethodDelete, "/secrets/"+tierSecretID, nil, http.StatusNoContent)
	c.do(installTok, http.MethodPost, "/tags/environment:setPlatform", map[string]any{"value": "prod"}, http.StatusOK)
	c.do(installTok, http.MethodPost, "/tags/environment:clearPlatform", nil, http.StatusNoContent)
	c.do(installTok, http.MethodPatch, "/settings/ui", map[string]any{"theme": "omniglass-light"}, http.StatusOK)
}

// TestSeededRolesCarryThePlatformPermission pins where install-wide authority
// sits in the official roles: admin holds it explicitly and owner through its >
// tail, while the day-to-day roles do not, however wide their scope.
func TestSeededRolesCarryThePlatformPermission(t *testing.T) {
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
	bootstrapOwnerTok(t, ctx, gw)
	adminTok := principalWithGrants(t, ctx, dsn, "platform-admin", []grant{{role: "admin", scopeKind: "all"}})
	opTok := principalWithGrants(t, ctx, dsn, "platform-operator", []grant{{role: "operator", scopeKind: "all"}})

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// The all-scoped operator carries variable:create but not the tier.
	c.do(opTok, http.MethodPost, "/variables", varReq("op_tier", "int", "platform", "", 1), http.StatusForbidden)
	c.do(adminTok, http.MethodPost, "/variables", varReq("admin_tier", "int", "platform", "", 1), http.StatusCreated)
}

// insertRole adds a custom (non-official) role with the given raw permission
// strings, so a test can vary one permission at a time.
func insertRole(t *testing.T, ctx context.Context, dsn, id string, perms []string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx,
		`insert into role (id, official, permissions, inherits) values ($1, false, $2, '{}')`,
		id, perms); err != nil {
		t.Fatalf("insert role %s: %v", id, err)
	}
}

// secretFieldsReq is the update body for an snmp-community secret: the one field
// the shape carries, re-sealed on every write.
func secretFieldsReq(community string) map[string]any {
	return map[string]any{"fields": map[string]string{"community": community}}
}

// createdID pulls the id out of a create response body.
func createdID(t *testing.T, raw []byte) string {
	t.Helper()
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode created body: %v (raw %s)", err, raw)
	}
	if body.ID == "" {
		t.Fatalf("created body carries no id (raw %s)", raw)
	}
	return body.ID
}
