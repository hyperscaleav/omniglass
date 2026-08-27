package storage_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// The wall clock is not a write sequence. On a VM whose clock steps (WSL2 under
// load corrects multi-second NTP drift mid-run), now() and uuidv7() from
// different transactions invert relative to true write order, and every list
// ordered by a clock-derived key flapped the zero-tolerance screenshot gate
// (#780: the audit page interleave, the users directory row swap). These tests
// simulate the step by rewriting the clock-derived columns of committed rows
// into an inverted order, then assert the lists still read in write order: the
// ordering key must be the DB-assigned write sequence, which no clock touches.

// TestAuditOrderSurvivesClockStep writes three audited rows, rewrites the middle
// row's uuidv7 id and ts to sort first under any clock-derived key, and asserts
// the trail still lists newest write first. Skipped under -short.
func TestAuditOrderSurvivesClockStep(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("raw conn: %v", err)
	}
	defer conn.Close(ctx)

	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner role: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	owner, err := gw.AuthenticateBearer(ctx, zeros)
	if err != nil {
		t.Fatalf("resolve owner: %v", err)
	}
	all := scope.Set{All: true}
	for _, u := range []string{"first", "second", "third"} {
		if _, err := gw.CreateHumanPrincipal(ctx, owner.ID, storage.HumanSpec{Username: u}, all); err != nil {
			t.Fatalf("create %s: %v", u, err)
		}
	}

	// The clock step: the middle write's clock-derived keys jump to sort
	// earliest. Its true position in the trail must not move.
	if _, err := conn.Exec(ctx, `
		update audit_log set id = '00000000-0000-7000-8000-000000000002',
		                     ts = ts - interval '1 hour'
		where resource_id = (select principal_id::text from human where username = 'second')`); err != nil {
		t.Fatalf("simulate step: %v", err)
	}

	entries, err := gw.ListAuditLog(ctx, storage.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var got []string
	for _, e := range entries {
		if e.Resource != "principal" {
			continue
		}
		var img struct {
			Username string `json:"username"`
		}
		if err := json.Unmarshal(e.New, &img); err != nil {
			t.Fatalf("decode row image: %v", err)
		}
		got = append(got, img.Username)
	}
	want := []string{"third", "second", "first"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("audit order after clock step: want %v, got %v", want, got)
	}
}

// TestPrincipalOrderSurvivesClockStep creates two humans, jumps the first one's
// created_at an hour forward (the step), and asserts the directory still lists
// them in creation order. Skipped under -short.
func TestPrincipalOrderSurvivesClockStep(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("raw conn: %v", err)
	}
	defer conn.Close(ctx)

	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner role: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	owner, err := gw.AuthenticateBearer(ctx, zeros)
	if err != nil {
		t.Fatalf("resolve owner: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateHumanPrincipal(ctx, owner.ID, storage.HumanSpec{Username: "later-human"}, all); err != nil {
		t.Fatalf("create human: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		update principal set created_at = created_at + interval '1 hour'
		where id = (select principal_id from human where username = 'root')`); err != nil {
		t.Fatalf("simulate step: %v", err)
	}

	list, err := gw.ListPrincipals(ctx, all, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Human == nil || list[0].Human.Username != "root" || list[1].Human == nil || list[1].Human.Username != "later-human" {
		t.Fatalf("directory order after clock step: want root then later-human, got %+v", list)
	}
}
