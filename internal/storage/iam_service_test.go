package storage_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// A service principal the trusted lane keeps (#1370: the lab's simulated
// pusher): ensuring it twice finds the same principal, and setting its token
// replaces the one before, so each lab run's freshly minted token is the only
// one that authenticates it. An unknown service is reported, not created.
func TestAServiceKeepsOneTokenTheTrustedLaneSets(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()

	id, err := gw.EnsureServicePrincipal(ctx, "", "sim-pusher")
	if err != nil {
		t.Fatal(err)
	}
	again, err := gw.EnsureServicePrincipal(ctx, "", "sim-pusher")
	if err != nil || again != id {
		t.Fatalf("ensuring it again = %q, %v; want the same principal %q", again, err, id)
	}

	first, second := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	for _, h := range [][]byte{first, second} {
		ok, err := gw.SetServiceToken(ctx, "", "sim-pusher", h, "0a0b0c0d")
		if err != nil || !ok {
			t.Fatalf("set token: %v, %v", ok, err)
		}
	}
	if _, err := gw.AuthenticateBearer(ctx, first); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("the replaced token authenticates: %v", err)
	}
	pr, err := gw.AuthenticateBearer(ctx, second)
	if err != nil || pr.ID != id || pr.Kind != "service" || pr.Service == nil || pr.Service.Name != "sim-pusher" {
		t.Fatalf("the current token = %+v, %v; want the service sim-pusher", pr, err)
	}
	if ok, err := gw.SetServiceToken(ctx, "", "nobody", first, "0a0b0c0d"); err != nil || ok {
		t.Fatalf("a token for an unknown service: %v, %v; want false, nil", ok, err)
	}
}

// The trusted lane ensures an operator role it needs (#1370's pusher role):
// created once, audited, and never rewritten. A role of that name already
// holding other permissions, or an official one, is refused rather than
// overwritten.
func TestEnsureRoleCreatesOnceAndNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	pusher := storage.Role{Name: "pusher", Permissions: []string{"telemetry:push"}, Label: "Pusher"}
	for i := 0; i < 2; i++ {
		if err := gw.EnsureRole(ctx, "", pusher); err != nil {
			t.Fatalf("ensure %d: %v", i, err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var audits int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'role' and verb = 'create'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("%d role create audit rows, want 1", audits)
	}
	wider := pusher
	wider.Permissions = []string{"telemetry:push", "component:read"}
	if err := gw.EnsureRole(ctx, "", wider); !errors.Is(err, storage.ErrRoleConflict) {
		t.Fatalf("ensuring a role whose permissions differ: %v, want ErrRoleConflict", err)
	}
	if err := gw.UpsertRole(ctx, storage.Role{Name: "viewer", Official: true, Permissions: []string{"*:read"}}); err != nil {
		t.Fatal(err)
	}
	if err := gw.EnsureRole(ctx, "", storage.Role{Name: "viewer", Permissions: []string{"*:read"}}); !errors.Is(err, storage.ErrRoleConflict) {
		t.Fatalf("ensuring over an official role: %v, want ErrRoleConflict", err)
	}
}
