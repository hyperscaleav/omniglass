package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestAPushOnlyIntegrationPushesWhereItIsGranted holds the push fence to
// ADR-0752 as amended: telemetry:push is the stronger grant, so its own scope is
// enough to see the component a push names. An integration holding push on one
// component and no component:read pushes to that component; every other
// component, real or not, is the same non-disclosing 404.
func TestAPushOnlyIntegrationPushesWhereItIsGranted(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(make([]byte, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The role must exist before the first request builds the role index.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into role (name, official, permissions, inherits) values ('push-only', false, '{telemetry:push}', '{}')`); err != nil {
		t.Fatalf("insert push-only role: %v", err)
	}
	conn.Close(ctx)

	all := scope.Set{All: true}
	ids := map[string]string{}
	for _, name := range []string{"bar-1", "bar-2"} {
		c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		ids[name] = c.ID
	}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(discardPublisher{})))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	bootstrapOwnerTok(t, ctx, gw)

	pusher := principalWithGrants(t, ctx, dsn, "pusher", []grant{{role: "push-only", scopeKind: "component", scopeID: ids["bar-1"]}})
	push := func(owner string, want int) {
		t.Helper()
		c.do(pusher, http.MethodPost, "/telemetry:push", map[string]any{
			"owner":   map[string]any{"kind": "component", "ref": owner},
			"metrics": []map[string]any{{"name": "room-temperature", "value": 21.5}},
		}, want)
	}

	push("bar-1", http.StatusAccepted)
	push("bar-2", http.StatusNotFound)
	push("no-such-bar", http.StatusNotFound)
	// Push confers no read: reading the component it reports for is refused at
	// the permission gate, before any row is looked up.
	c.do(pusher, http.MethodGet, "/components/bar-1", nil, http.StatusForbidden)
}
