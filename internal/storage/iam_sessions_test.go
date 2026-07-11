package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestListAndRevokeBearerCredentials proves a principal's own bearer credentials
// list with their metadata (id, prefix, created_at, expires_at) and never the
// secret, that one is revocable by id scoped to the owning principal, and that
// another principal's credential id is not revocable (a no-op, false). This backs
// the self-service session list and revoke (issue #172, slice 1).
func TestListAndRevokeBearerCredentials(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Bootstrap an owner; its bootstrap bearer is the first credential (a token, so
	// it has no expiry).
	_, hash1, prefix1, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash1, Prefix: prefix1}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	pid, err := gw.ResolvePrincipalRef(ctx, "root")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}

	// Issue a second bearer for the same principal, a web-login session (expires_at set).
	_, hash2, prefix2, _ := auth.NewBearerToken()
	future := time.Now().Add(12 * time.Hour)
	if ok, err := gw.IssueBearerCredential(ctx, "root", hash2, prefix2, "session", &future); err != nil || !ok {
		t.Fatalf("issue second: ok=%v err=%v", ok, err)
	}

	// List (as the bootstrap credential's session) returns both, with metadata and no
	// secret; the bootstrap credential is flagged current, the other is not.
	creds, err := gw.ListBearerCredentials(ctx, pid, hash1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(creds) != 2 {
		t.Fatalf("list = %d creds, want 2", len(creds))
	}
	byPrefix := map[string]storage.BearerCredential{}
	for _, c := range creds {
		if c.ID == "" || c.Prefix == "" {
			t.Errorf("credential missing id/prefix: %+v", c)
		}
		if c.CreatedAt.IsZero() {
			t.Errorf("credential missing created_at: %+v", c)
		}
		byPrefix[c.Prefix] = c
	}
	// The two are distinguished by purpose (both are bearers): the bootstrap credential
	// is a 'token', the issued one a 'session' with a bounded expiry.
	if got := byPrefix[prefix1]; got.Purpose != "token" {
		t.Errorf("bootstrap credential purpose = %q, want token", got.Purpose)
	}
	if got := byPrefix[prefix2]; got.Purpose != "session" {
		t.Errorf("issued credential purpose = %q, want session", got.Purpose)
	}
	if got := byPrefix[prefix2]; got.ExpiresAt == nil {
		t.Errorf("session credential expiry = nil, want a bounded time")
	}
	// Current marks the credential that authenticated this list, and only it.
	if !byPrefix[prefix1].Current {
		t.Errorf("the listing credential should be marked current")
	}
	if byPrefix[prefix2].Current {
		t.Errorf("a different credential must not be marked current")
	}

	// A second principal cannot revoke root's credential: the revoke is scoped to the
	// owning principal, so a mismatched principal id is a no-op that returns false and
	// deletes nothing.
	mallory, err := gw.CreateHumanPrincipal(ctx, pid, storage.HumanSpec{Username: "mallory"}, scope.Set{All: true})
	if err != nil {
		t.Fatalf("create mallory: %v", err)
	}
	sessionID := byPrefix[prefix2].ID
	if ok, err := gw.RevokeBearerByID(ctx, mallory.ID, sessionID); err != nil || ok {
		t.Fatalf("cross-principal revoke = (%v, %v), want (false, nil)", ok, err)
	}
	if creds, err := gw.ListBearerCredentials(ctx, pid, hash1); err != nil || len(creds) != 2 {
		t.Fatalf("after cross-principal revoke root should still have 2 creds, got %d (err %v)", len(creds), err)
	}

	// The owner revokes its own session by id: it drops from the list, and the
	// remaining credential still authenticates.
	if ok, err := gw.RevokeBearerByID(ctx, pid, sessionID); err != nil || !ok {
		t.Fatalf("revoke own = (%v, %v), want (true, nil)", ok, err)
	}
	after, err := gw.ListBearerCredentials(ctx, pid, hash1)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(after) != 1 || after[0].Prefix != prefix1 {
		t.Fatalf("after revoke = %+v, want only the bootstrap credential", after)
	}
	if pr, err := gw.AuthenticateBearer(ctx, hash1); err != nil || pr.ID != pid {
		t.Fatalf("bootstrap credential should still authenticate: pr=%v err=%v", pr, err)
	}

	// Revoking the same id again is a no-op (already gone), and a malformed id is a
	// clean false, not an error (the API maps it to 404, never a 500).
	if ok, err := gw.RevokeBearerByID(ctx, pid, sessionID); err != nil || ok {
		t.Fatalf("re-revoke = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := gw.RevokeBearerByID(ctx, pid, "not-a-uuid"); err != nil || ok {
		t.Fatalf("malformed id revoke = (%v, %v), want (false, nil)", ok, err)
	}
}

// TestListBearerCredentialsPurposeAndLiveFilter proves the list carries each
// credential's purpose (session vs token, the discriminator now that both kinds
// carry an expiry) and returns only LIVE rows: a credential whose expires_at has
// passed is excluded, mirroring AuthenticateBearer's own filter (issue #172).
func TestListBearerCredentialsPurposeAndLiveFilter(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Bootstrap an owner: its bootstrap bearer is a 'token' credential.
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: bh, Prefix: bp}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	pid, err := gw.ResolvePrincipalRef(ctx, "root")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}

	// A live web-login session (future expiry, purpose session).
	_, sh, sp, _ := auth.NewBearerToken()
	future := time.Now().Add(12 * time.Hour)
	if ok, err := gw.IssueBearerCredential(ctx, "root", sh, sp, "session", &future); err != nil || !ok {
		t.Fatalf("issue session: ok=%v err=%v", ok, err)
	}

	// An expired token (past expiry): still a stored row, but dead, so the list omits it.
	_, xh, xp, _ := auth.NewBearerToken()
	past := time.Now().Add(-time.Minute)
	if ok, err := gw.IssueBearerCredential(ctx, "root", xh, xp, "token", &past); err != nil || !ok {
		t.Fatalf("issue expired: ok=%v err=%v", ok, err)
	}

	creds, err := gw.ListBearerCredentials(ctx, pid, bh)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byPrefix := map[string]storage.BearerCredential{}
	for _, c := range creds {
		byPrefix[c.Prefix] = c
	}
	if _, listed := byPrefix[xp]; listed {
		t.Errorf("an expired credential must not be listed: %+v", creds)
	}
	if len(creds) != 2 {
		t.Fatalf("list = %d live creds, want 2 (bootstrap token + session)", len(creds))
	}
	if got := byPrefix[bp]; got.Purpose != "token" {
		t.Errorf("bootstrap credential purpose = %q, want token", got.Purpose)
	}
	if got := byPrefix[sp]; got.Purpose != "session" {
		t.Errorf("session credential purpose = %q, want session", got.Purpose)
	}
}
