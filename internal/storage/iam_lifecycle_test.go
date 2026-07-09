package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestPrincipalLifecycle proves the archive -> restore -> purge lifecycle
// against a real Postgres: archive soft-deletes (hidden from the directory,
// cannot authenticate, reversible), purge is gated on archival and hard-deletes
// the row, and a purge preserves the audit trail (the actor's name survives via the
// denormalized snapshot once the principal id is nulled). Skipped under -short.
func TestPrincipalLifecycle(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := gw.UpsertRole(ctx, storage.Role{ID: "owner", Official: true, Permissions: []string{"*:*", ">"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, _ := gw.AuthenticateBearer(ctx, zeros)
	all := scope.Set{All: true}

	pwHash, _ := auth.HashPassword("alice-s3cret")
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice", PasswordHash: pwHash}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// Archive: soft-deleted. archived_at set, inactive, hidden from the list,
	// and cannot authenticate.
	if err := gw.ArchivePrincipal(ctx, root.ID, alice.ID, all); err != nil {
		t.Fatalf("archive: %v", err)
	}
	got, err := gw.GetPrincipal(ctx, alice.ID, all)
	if err != nil {
		t.Fatalf("get archived: %v", err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("archived principal should carry archived_at")
	}
	if got.Active {
		t.Fatal("archived principal should be inactive")
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "alice-s3cret"); !errors.Is(err, storage.ErrAccountDisabled) {
		t.Fatalf("archived auth: want ErrAccountDisabled, got %v", err)
	}
	list, err := gw.ListPrincipals(ctx, all, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range list {
		if p.ID == alice.ID {
			t.Fatal("archived principal should be hidden from the default directory")
		}
	}
	// The "show archived" directory (includeArchived) surfaces her, carrying
	// archived_at, so an admin can re-find her to restore or purge.
	shown, err := gw.ListPrincipals(ctx, all, true)
	if err != nil {
		t.Fatalf("list include-archived: %v", err)
	}
	var seen bool
	for _, p := range shown {
		if p.ID == alice.ID {
			seen = true
			if p.ArchivedAt == nil {
				t.Fatal("shown archived principal should carry archived_at")
			}
		}
	}
	if !seen {
		t.Fatal("include-archived list should surface the archived principal")
	}

	// Purge is gated: a live (not archived) principal cannot be purged.
	bob, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "bob"}, all)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if err := gw.PurgePrincipal(ctx, root.ID, bob.ID, all); !errors.Is(err, storage.ErrNotArchived) {
		t.Fatalf("purge live: want ErrNotArchived, got %v", err)
	}

	// Restore: restored to active and visible, and can authenticate again.
	if err := gw.RestorePrincipal(ctx, root.ID, alice.ID, all); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, _ = gw.GetPrincipal(ctx, alice.ID, all)
	if got.ArchivedAt != nil || !got.Active {
		t.Fatalf("restored should be active and clear: active=%v archived=%v", got.Active, got.ArchivedAt)
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "alice-s3cret"); err != nil {
		t.Fatalf("restored password should work: %v", err)
	}

	// The last active owner cannot be archived.
	if err := gw.ArchivePrincipal(ctx, root.ID, root.ID, all); !errors.Is(err, storage.ErrLastOwner) {
		t.Fatalf("archive last owner: want ErrLastOwner, got %v", err)
	}

	// Make alice an owner, then let alice perform an audited action (so an audit row
	// records alice as the actor), then archive + purge alice.
	if _, err := gw.CreateGrant(ctx, root.ID, alice.ID, storage.GrantSpec{Role: "owner", ScopeKind: "all"}, all); err != nil {
		t.Fatalf("grant alice owner: %v", err)
	}
	if _, err := gw.CreateHumanPrincipal(ctx, alice.ID, storage.HumanSpec{Username: "carol"}, all); err != nil {
		t.Fatalf("alice creates carol: %v", err)
	}
	if err := gw.ArchivePrincipal(ctx, root.ID, alice.ID, all); err != nil {
		t.Fatalf("archive alice: %v", err)
	}
	if err := gw.PurgePrincipal(ctx, root.ID, alice.ID, all); err != nil {
		t.Fatalf("purge alice: %v", err)
	}
	if _, err := gw.GetPrincipal(ctx, alice.ID, all); !errors.Is(err, storage.ErrPrincipalNotFound) {
		t.Fatalf("purged principal: want ErrPrincipalNotFound, got %v", err)
	}

	// Audit preserved: alice's create-carol action still names "alice" as the actor,
	// even though her principal row (and its id link) is gone.
	entries, err := gw.ListAuditLog(ctx, storage.AuditFilter{Limit: 500})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.ActorName == "alice" {
			found = true
			if e.ActorID != "" {
				t.Fatalf("purged actor should have a null id but a preserved name, got id=%q", e.ActorID)
			}
		}
	}
	if !found {
		t.Fatal("audit trail lost alice as an actor after purge (denormalized snapshot missing)")
	}
}
