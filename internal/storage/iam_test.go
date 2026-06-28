package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestBootstrapOwnerIdempotent proves the first bootstrap creates a human owner
// with an owner@all grant and one bearer credential, and that a second run with
// the same username is a no-op that mints no second credential. Skipped under
// -short.
func TestBootstrapOwnerIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	// The owner@all grant references the owner role, so it must exist first.
	if err := gw.UpsertRole(ctx, storage.Role{ID: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner role: %v", err)
	}

	spec := storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "abcd1234"}
	created, err := gw.BootstrapOwner(ctx, spec)
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if !created {
		t.Fatal("first bootstrap should create the owner")
	}

	// Second run, same username, a different hash: a no-op that mints nothing.
	spec2 := spec
	spec2.SecretHash = make([]byte, 32)
	spec2.SecretHash[0] = 1
	created, err = gw.BootstrapOwner(ctx, spec2)
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if created {
		t.Fatal("second bootstrap with the same username should be a no-op")
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	for _, c := range []struct {
		name string
		sql  string
		want int
	}{
		{"human", `select count(*) from human where username = 'root'`, 1},
		{"credential", `select count(*) from credential c join human h on h.principal_id = c.principal_id where h.username = 'root'`, 1},
		{"owner@all grant", `select count(*) from principal_grant g join human h on h.principal_id = g.principal_id where h.username = 'root' and g.role_id = 'owner' and g.scope_kind = 'all'`, 1},
	} {
		var got int
		if err := conn.QueryRow(ctx, c.sql).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s rows = %d, want %d", c.name, got, c.want)
		}
	}
}
