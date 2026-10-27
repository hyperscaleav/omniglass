package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The server's boot gate against a real database (#1195): a server outside
// lab mode refuses a database the lab marked, and a lab-mode server refuses a
// database that holds data with no mark. Both pass their own.
func TestServerBootGateRefusesTheOtherKindsDatabase(t *testing.T) {
	ctx := context.Background()
	open := func() *storage.PG {
		gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(gw.Close)
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatal(err)
		}
		return gw
	}
	withOwner := func(gw *storage.PG) {
		if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: []byte("h"), Prefix: "p"}); err != nil {
			t.Fatal(err)
		}
	}

	real := open()
	withOwner(real)
	if err := labBootGate(ctx, real, false); err != nil {
		t.Fatalf("a real server on its own database: %v", err)
	}
	if err := labBootGate(ctx, real, true); !errors.Is(err, storage.ErrNotLabDatabase) {
		t.Fatalf("a lab on a real database: %v, want ErrNotLabDatabase", err)
	}

	lab := open()
	if err := lab.MarkLab(ctx); err != nil {
		t.Fatal(err)
	}
	withOwner(lab)
	if err := labBootGate(ctx, lab, true); err != nil {
		t.Fatalf("a lab on its own database: %v", err)
	}
	if err := labBootGate(ctx, lab, false); !errors.Is(err, storage.ErrLabDatabase) {
		t.Fatalf("a real server on a lab's database: %v, want ErrLabDatabase", err)
	}
}
