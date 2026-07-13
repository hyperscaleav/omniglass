package cli

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSetPasswordBreakGlassLockout proves the break-glass set-password revokes the
// target's live SESSIONS (so a stolen login stops at once) while KEEPING its API tokens
// by default, and revokes the tokens too only with --revoke-tokens. The new password
// authenticates and the old one does not. Real Postgres; skipped under -short.
func TestSetPasswordBreakGlassLockout(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	t.Setenv("OMNIGLASS_DSN", dsn)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("gw: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Bootstrap an owner (its bootstrap bearer is a token) with a password.
	pwHash, err := auth.HashPassword("orange-boat-42x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_, bh, bp, _ := auth.NewBearerToken()
	future := time.Now().Add(90 * 24 * time.Hour)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: bh, Prefix: bp, PasswordHash: pwHash, ExpiresAt: &future}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	pid, err := gw.ResolvePrincipalRef(ctx, "root")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	counts := func() (sessions, tokens int) {
		creds, err := gw.ListBearerCredentials(ctx, pid, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, c := range creds {
			if c.Purpose == "session" {
				sessions++
			} else {
				tokens++
			}
		}
		return
	}
	addSession := func() {
		_, h, p, _ := auth.NewBearerToken()
		if ok, err := gw.IssueBearerCredential(ctx, "root", h, p, "session", &future); err != nil || !ok {
			t.Fatalf("add session: ok=%v err=%v", ok, err)
		}
	}

	// Start with a live session plus the bootstrap token.
	addSession()
	if s, tk := counts(); s != 1 || tk != 1 {
		t.Fatalf("precondition: want 1 session + 1 token, got %d + %d", s, tk)
	}

	// Break-glass without the flag: the session is revoked, the token survives.
	if err := runSetPassword(ctx, "root", "purple-canyon-7", false); err != nil {
		t.Fatalf("set-password: %v", err)
	}
	if s, tk := counts(); s != 0 || tk != 1 {
		t.Fatalf("after break-glass (no flag): want 0 sessions + 1 token, got %d + %d", s, tk)
	}
	// The new password authenticates; the old one does not.
	if _, err := gw.AuthenticatePassword(ctx, "root", "purple-canyon-7"); err != nil {
		t.Fatalf("new password should authenticate: %v", err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "root", "orange-boat-42x"); err == nil {
		t.Fatalf("old password should be rejected after the reset")
	}

	// Break-glass WITH --revoke-tokens: a fresh session and the surviving token both go.
	addSession()
	if err := runSetPassword(ctx, "root", "green-river-88x", true); err != nil {
		t.Fatalf("set-password --revoke-tokens: %v", err)
	}
	if s, tk := counts(); s != 0 || tk != 0 {
		t.Fatalf("after break-glass (--revoke-tokens): want 0 + 0, got %d + %d", s, tk)
	}

	// An unknown user is a clean error, not a panic.
	if err := runSetPassword(ctx, "ghost", "whatever-strong-12", false); err == nil {
		t.Fatalf("set-password on an unknown user should error")
	}
}
