package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestIssueBearerCredential proves a second bearer credential can be minted for
// an existing principal and authenticates to the same identity, and that an
// unknown username is reported (not an error). This backs `omniglass token` and
// the `make dev` login when the owner already exists.
func TestIssueBearerCredential(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tok1, hash1, prefix1, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint first: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash1, Prefix: prefix1}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// Issue a second credential for the same owner.
	tok2, hash2, prefix2, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint second: %v", err)
	}
	ok, err := gw.IssueBearerCredential(ctx, "root", hash2, prefix2, "token", nil)
	if err != nil || !ok {
		t.Fatalf("issue for root = (%v, %v), want (true, nil)", ok, err)
	}

	// Both tokens authenticate to the same principal.
	for name, tok := range map[string]string{"first": tok1, "second": tok2} {
		pr, err := gw.AuthenticateBearer(ctx, auth.HashToken(tok))
		if err != nil {
			t.Fatalf("authenticate %s: %v", name, err)
		}
		if pr.Human == nil || pr.Human.Username != "root" {
			t.Errorf("%s token resolved to %+v, want human root", name, pr.Human)
		}
	}

	// An unknown username is reported as not-found, not an error.
	ok, err = gw.IssueBearerCredential(ctx, "nobody", hash2, prefix2, "token", nil)
	if err != nil || ok {
		t.Errorf("issue for unknown = (%v, %v), want (false, nil)", ok, err)
	}
}

// TestBearerExpiry proves an expired bearer credential authenticates nothing (issue
// #157): a session past its expires_at is treated as absent, while a future expiry
// (and a nil expiry, tested above) still resolves.
func TestBearerExpiry(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// An expired session credential authenticates nothing.
	expTok, expHash, expPrefix, _ := auth.NewBearerToken()
	past := time.Now().Add(-time.Minute)
	if ok, err := gw.IssueBearerCredential(ctx, "root", expHash, expPrefix, "token", &past); err != nil || !ok {
		t.Fatalf("issue expired: ok=%v err=%v", ok, err)
	}
	if _, err := gw.AuthenticateBearer(ctx, auth.HashToken(expTok)); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("expired bearer: want ErrCredentialNotFound, got %v", err)
	}

	// A credential with a future expiry still authenticates.
	okTok, okHash, okPrefix, _ := auth.NewBearerToken()
	future := time.Now().Add(time.Hour)
	if ok, err := gw.IssueBearerCredential(ctx, "root", okHash, okPrefix, "token", &future); err != nil || !ok {
		t.Fatalf("issue future: ok=%v err=%v", ok, err)
	}
	if pr, err := gw.AuthenticateBearer(ctx, auth.HashToken(okTok)); err != nil || pr.Human == nil || pr.Human.Username != "root" {
		t.Fatalf("future bearer should authenticate: pr=%v err=%v", pr, err)
	}
}
