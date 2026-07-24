package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestSelfProfileAuditsRealActor proves the self-service mutations (which skip the
// capability middleware) still audit, and that an impersonated edit records the
// real actor: a plain profile edit audits with a null real_actor, while the same
// edit under WithRealActor records the impersonating admin. Skipped under -short.
func TestSelfProfileAuditsRealActor(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, _ := gw.AuthenticateBearer(ctx, zeros)
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice"}, scope.Set{All: true})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	name := func(s string) *string { return &s }
	// A normal self-edit: audited with a null real actor.
	if err := gw.UpdateHumanProfile(ctx, alice.ID, storage.HumanProfilePatch{DisplayName: name("Alice One")}); err != nil {
		t.Fatalf("self edit: %v", err)
	}
	// The same edit while impersonated: the real actor rides the context.
	if err := gw.UpdateHumanProfile(storage.WithRealActor(ctx, root.ID), alice.ID, storage.HumanProfilePatch{DisplayName: name("Alice Two")}); err != nil {
		t.Fatalf("impersonated edit: %v", err)
	}

	ac, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer ac.Close(ctx)
	rows, err := ac.Query(ctx, `
		select actor_principal_id, real_actor_principal_id from audit_log
		where verb = 'update' and resource = 'principal' and resource_id = $1
		order by ts asc`, alice.ID)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()
	type row struct{ actor, real *string }
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.actor, &r.real); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 profile-update audit rows, got %d", len(got))
	}
	// First edit: actor alice, no real actor.
	if got[0].actor == nil || *got[0].actor != alice.ID || got[0].real != nil {
		t.Fatalf("first audit = %+v, want actor=alice real=nil", got[0])
	}
	// Second (impersonated): actor alice, real actor root.
	if got[1].actor == nil || *got[1].actor != alice.ID || got[1].real == nil || *got[1].real != root.ID {
		t.Fatalf("second audit = %+v, want actor=alice real=root", got[1])
	}
}
