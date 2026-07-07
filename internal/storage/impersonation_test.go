package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestImpersonationSession proves the impersonation gateway against a real
// Postgres: begin mints a token resolving to the TARGET plus the real actor and
// mode, self-impersonation and a bad mode are refused, disabling either party or
// revoking the session or letting it expire all kill authentication, and ending an
// inactive session is a clean not-found. Skipped under -short.
func TestImpersonationSession(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := gw.UpsertRole(ctx, storage.Role{ID: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, _ := gw.AuthenticateBearer(ctx, zeros)
	all := scope.Set{All: true}
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice"}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// Begin an act-as session for root -> alice.
	token, sess, err := gw.BeginImpersonation(ctx, root.ID, alice.ID, "act_as", 30*time.Minute)
	if err != nil || token == "" {
		t.Fatalf("begin: token=%q err=%v", token, err)
	}
	if sess.TargetID != alice.ID || sess.RealActorID != root.ID || sess.Mode != "act_as" {
		t.Fatalf("session = %+v, want alice/root/act_as", sess)
	}

	// The token resolves to the TARGET (alice), carrying the real actor and mode.
	pr, ra, mode, sid, err := gw.AuthenticateImpersonation(ctx, auth.HashToken(token))
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if pr.ID != alice.ID || ra != root.ID || mode != "act_as" || sid != sess.ID {
		t.Fatalf("resolved pr=%s ra=%s mode=%s sid=%s, want alice/root/act_as/%s", pr.ID, ra, mode, sid, sess.ID)
	}

	// Self-impersonation and a bad mode are refused.
	if _, _, err := gw.BeginImpersonation(ctx, root.ID, root.ID, "act_as", time.Minute); !errors.Is(err, storage.ErrCannotImpersonateSelf) {
		t.Fatalf("self impersonation: want ErrCannotImpersonateSelf, got %v", err)
	}
	if _, _, err := gw.BeginImpersonation(ctx, root.ID, alice.ID, "sudo", time.Minute); err == nil {
		t.Fatal("bad mode should error")
	}

	// Disabling the target kills the session (both parties must be active).
	if err := gw.SetPrincipalActive(ctx, root.ID, alice.ID, false, all); err != nil {
		t.Fatalf("disable alice: %v", err)
	}
	if _, _, _, _, err := gw.AuthenticateImpersonation(ctx, auth.HashToken(token)); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("disabled target: want ErrCredentialNotFound, got %v", err)
	}
	if err := gw.SetPrincipalActive(ctx, root.ID, alice.ID, true, all); err != nil {
		t.Fatalf("re-enable alice: %v", err)
	}

	// Ending the session kills authentication; ending again is a clean not-found.
	if err := gw.EndImpersonation(ctx, sess.ID); err != nil {
		t.Fatalf("end: %v", err)
	}
	if _, _, _, _, err := gw.AuthenticateImpersonation(ctx, auth.HashToken(token)); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("revoked session: want ErrCredentialNotFound, got %v", err)
	}
	if err := gw.EndImpersonation(ctx, sess.ID); !errors.Is(err, storage.ErrImpersonationNotFound) {
		t.Fatalf("end revoked: want ErrImpersonationNotFound, got %v", err)
	}

	// An unknown token is a clean miss.
	if _, _, _, _, err := gw.AuthenticateImpersonation(ctx, auth.HashToken("ogp_unknown_token")); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("unknown token: want ErrCredentialNotFound, got %v", err)
	}

	// An already-expired session does not authenticate.
	expTok, expSess, err := gw.BeginImpersonation(ctx, root.ID, alice.ID, "view_as", -1*time.Minute)
	if err != nil {
		t.Fatalf("begin expired: %v", err)
	}
	if _, _, _, _, err := gw.AuthenticateImpersonation(ctx, auth.HashToken(expTok)); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("expired session: want ErrCredentialNotFound, got %v", err)
	}
	if err := gw.EndImpersonation(ctx, expSess.ID); !errors.Is(err, storage.ErrImpersonationNotFound) {
		t.Fatalf("end expired: want ErrImpersonationNotFound, got %v", err)
	}
}
