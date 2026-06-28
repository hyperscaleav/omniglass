package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestPGPing proves the pgx-backed Gateway pings a real migrated Postgres
// successfully. Skipped under -short.
func TestPGPing(t *testing.T) {
	gw := storagetest.NewDB(t)
	if err := gw.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

// TestNewPGBadDSN proves NewPG fails fast on an unreachable backend rather than
// returning a gateway that errors later. This needs no container (the dial
// itself fails), so it runs under -short too.
func TestNewPGBadDSN(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // a cancelled context makes the connect attempt fail immediately
	if _, err := storage.NewPG(ctx, "postgres://nobody@127.0.0.1:1/none?sslmode=disable"); err == nil {
		t.Fatal("NewPG with unreachable DSN: want error, got nil")
	}
}
