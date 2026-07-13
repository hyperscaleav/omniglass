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
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "root", SecretHash: hash2, Prefix: prefix2, Purpose: "session", ExpiresAt: &future}); err != nil || !ok {
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

// TestBearerIdentityFields proves a bearer credential carries and returns its
// identifying metadata: a token's description, the user-agent and client-ip that created
// it, and a last_used_at that AuthenticateBearer bumps (issues #193, #205). The bootstrap
// token gets a default description; a session leaves description empty.
func TestBearerIdentityFields(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: bh, Prefix: bp}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	pid, err := gw.ResolvePrincipalRef(ctx, "root")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// A token with a description and the device/address that created it.
	_, th, tp, _ := auth.NewBearerToken()
	future := time.Now().Add(90 * 24 * time.Hour)
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{
		Username: "root", SecretHash: th, Prefix: tp, Purpose: "token", ExpiresAt: &future,
		Description: "ci pipeline", UserAgent: "curl/8.4", ClientIP: "203.0.113.7",
	}); err != nil || !ok {
		t.Fatalf("issue token: ok=%v err=%v", ok, err)
	}

	byPrefix := func() map[string]storage.BearerCredential {
		creds, err := gw.ListBearerCredentials(ctx, pid, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		m := map[string]storage.BearerCredential{}
		for _, c := range creds {
			m[c.Prefix] = c
		}
		return m
	}

	m := byPrefix()
	// The minted token carries its description, user-agent, and client-ip.
	if got := m[tp]; got.Description != "ci pipeline" || got.UserAgent != "curl/8.4" || got.ClientIP != "203.0.113.7" {
		t.Errorf("token identity fields = %+v, want description/ua/ip set", got)
	}
	// The bootstrap token gets the default description; neither carries a user-agent.
	if got := m[bp]; got.Description != "bootstrap token" {
		t.Errorf("bootstrap description = %q, want 'bootstrap token'", got.Description)
	}
	// Neither has been used yet: last_used_at is nil.
	if m[tp].LastUsedAt != nil {
		t.Errorf("a fresh token should have no last_used_at, got %v", m[tp].LastUsedAt)
	}

	// Authenticating with the token bumps its last_used_at (the others stay nil).
	if _, err := gw.AuthenticateBearer(ctx, th); err != nil {
		t.Fatalf("authenticate token: %v", err)
	}
	m = byPrefix()
	if m[tp].LastUsedAt == nil {
		t.Errorf("last_used_at should be set after authentication")
	}
	if m[bp].LastUsedAt != nil {
		t.Errorf("an unused credential's last_used_at must stay nil, got %v", m[bp].LastUsedAt)
	}
}

// TestRevokeBearersByPurpose proves the purpose-filtered bulk revoke: it deletes
// every bearer of one purpose for a principal (all its sessions, or all its tokens),
// leaves the other purpose untouched, returns the count deleted, and is scoped to the
// owning principal so it never reaches another's credentials. This backs the admin
// "revoke all sessions" / "revoke all tokens" blade actions (issue #172).
func TestRevokeBearersByPurpose(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Bootstrap an owner (its bootstrap bearer is a token), then give the same
	// principal two sessions and one more token.
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: bh, Prefix: bp}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	pid, err := gw.ResolvePrincipalRef(ctx, "root")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	future := time.Now().Add(12 * time.Hour)
	for _, p := range []string{"session", "session"} {
		_, h, pf, _ := auth.NewBearerToken()
		if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "root", SecretHash: h, Prefix: pf, Purpose: p, ExpiresAt: &future}); err != nil || !ok {
			t.Fatalf("issue %s: ok=%v err=%v", p, ok, err)
		}
	}
	_, th, tp, _ := auth.NewBearerToken()
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "root", SecretHash: th, Prefix: tp, Purpose: "token", ExpiresAt: &future}); err != nil || !ok {
		t.Fatalf("issue token: ok=%v err=%v", ok, err)
	}
	_ = tp

	// A second principal with its own session: it must survive root's bulk revoke.
	mallory, err := gw.CreateHumanPrincipal(ctx, pid, storage.HumanSpec{Username: "mallory"}, scope.Set{All: true})
	if err != nil {
		t.Fatalf("create mallory: %v", err)
	}
	_, mh, mp, _ := auth.NewBearerToken()
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "mallory", SecretHash: mh, Prefix: mp, Purpose: "session", ExpiresAt: &future}); err != nil || !ok {
		t.Fatalf("issue mallory session: ok=%v err=%v", ok, err)
	}

	// Revoke all of root's SESSIONS: both sessions go, the two tokens (bootstrap +
	// issued) remain, and the count is 2.
	n, err := gw.RevokeBearersByPurpose(ctx, pid, "session")
	if err != nil {
		t.Fatalf("revoke sessions: %v", err)
	}
	if n != 2 {
		t.Fatalf("revoked %d sessions, want 2", n)
	}
	creds, err := gw.ListBearerCredentials(ctx, pid, bh)
	if err != nil {
		t.Fatalf("list after session revoke: %v", err)
	}
	if len(creds) != 2 {
		t.Fatalf("after revoking sessions root should keep 2 tokens, got %d: %+v", len(creds), creds)
	}
	for _, c := range creds {
		if c.Purpose != "token" {
			t.Errorf("a non-token survived the session revoke: %+v", c)
		}
	}

	// Mallory's session is untouched: the revoke is scoped to the owning principal.
	if mc, err := gw.ListBearerCredentials(ctx, mallory.ID, mh); err != nil || len(mc) != 1 {
		t.Fatalf("mallory should still have 1 session, got %d (err %v)", len(mc), err)
	}

	// Revoke all of root's TOKENS: both remaining tokens go, count 2, none left.
	n, err = gw.RevokeBearersByPurpose(ctx, pid, "token")
	if err != nil {
		t.Fatalf("revoke tokens: %v", err)
	}
	if n != 2 {
		t.Fatalf("revoked %d tokens, want 2", n)
	}
	if after, err := gw.ListBearerCredentials(ctx, pid, bh); err != nil || len(after) != 0 {
		t.Fatalf("after revoking tokens root should have 0 bearers, got %d (err %v)", len(after), err)
	}

	// Revoking again is a clean zero, not an error.
	if n, err := gw.RevokeBearersByPurpose(ctx, pid, "session"); err != nil || n != 0 {
		t.Fatalf("re-revoke = (%d, %v), want (0, nil)", n, err)
	}
}

// TestRevokeBearersByPurposeExcept proves the keep-current variant: it deletes every
// bearer of one purpose EXCEPT the credentials whose secret_hash is in keep, scoped to
// the principal, and returns the count. This backs "revoke my other sessions" on a
// password change and the self-service "revoke all sessions" (keep the one you are on),
// while leaving tokens untouched (issues #173, #195).
func TestRevokeBearersByPurposeExcept(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Bootstrap an owner (its bootstrap bearer is a token), then two sessions + one token.
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: bh, Prefix: bp}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	pid, err := gw.ResolvePrincipalRef(ctx, "root")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	future := time.Now().Add(12 * time.Hour)
	_, keepH, keepP, _ := auth.NewBearerToken() // the "current" session, to keep
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "root", SecretHash: keepH, Prefix: keepP, Purpose: "session", ExpiresAt: &future}); err != nil || !ok {
		t.Fatalf("issue keep session: ok=%v err=%v", ok, err)
	}
	_, otherH, _, _ := auth.NewBearerToken() // another session, to revoke
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "root", SecretHash: otherH, Prefix: "og", Purpose: "session", ExpiresAt: &future}); err != nil || !ok {
		t.Fatalf("issue other session: ok=%v err=%v", ok, err)
	}

	// Revoke sessions EXCEPT the current one: the other session goes (count 1), the
	// kept session and both tokens (bootstrap + none here) survive.
	n, err := gw.RevokeBearersByPurposeExcept(ctx, pid, "session", [][]byte{keepH})
	if err != nil {
		t.Fatalf("revoke except: %v", err)
	}
	if n != 1 {
		t.Fatalf("revoked %d, want 1 (the non-kept session)", n)
	}
	creds, err := gw.ListBearerCredentials(ctx, pid, keepH)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Two survive: the kept session and the bootstrap token.
	var keptSession, keptToken bool
	for _, c := range creds {
		if c.Prefix == keepP && c.Purpose == "session" {
			keptSession = true
		}
		if c.Purpose == "token" {
			keptToken = true
		}
	}
	if !keptSession {
		t.Errorf("the current session must be kept: %+v", creds)
	}
	if !keptToken {
		t.Errorf("tokens must survive a session-only revoke: %+v", creds)
	}
	if len(creds) != 2 {
		t.Fatalf("want 2 survivors (kept session + bootstrap token), got %d: %+v", len(creds), creds)
	}

	// Empty keep behaves like the plain bulk revoke (delete all of the purpose): the
	// kept session now goes too, and RevokeBearersByPurpose is the nil-keep alias.
	if n, err := gw.RevokeBearersByPurpose(ctx, pid, "session"); err != nil || n != 1 {
		t.Fatalf("plain purpose revoke = (%d, %v), want (1, nil)", n, err)
	}
	if after, err := gw.ListBearerCredentials(ctx, pid, bh); err != nil || len(after) != 1 {
		t.Fatalf("only the bootstrap token should remain, got %d (err %v)", len(after), err)
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
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "root", SecretHash: sh, Prefix: sp, Purpose: "session", ExpiresAt: &future}); err != nil || !ok {
		t.Fatalf("issue session: ok=%v err=%v", ok, err)
	}

	// An expired token (past expiry): still a stored row, but dead, so the list omits it.
	_, xh, xp, _ := auth.NewBearerToken()
	past := time.Now().Add(-time.Minute)
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "root", SecretHash: xh, Prefix: xp, Purpose: "token", ExpiresAt: &past}); err != nil || !ok {
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
