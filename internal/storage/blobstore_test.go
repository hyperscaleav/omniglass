package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/blob"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The pgblobs backend runs the same blob.Store contract as the in-memory double,
// but against a real Postgres blob table. This is the capability-wrapping merge
// gate: the fake-seam unit test in internal/blob is a checkpoint, this is the
// close.
func newPGBlobStore(t *testing.T) blob.Store {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	t.Cleanup(pool.Close)
	return storage.NewPGBlobStore(pool)
}

func TestPGBlobStoreRoundTripAndDedup(t *testing.T) {
	ctx := context.Background()
	s := newPGBlobStore(t)
	payload := []byte("a full SNMP walk captured at the edge")

	key, err := s.Put(ctx, payload)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if key != blob.Hash(payload) {
		t.Fatalf("put returned %q, want the content hash", key)
	}

	got, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("get returned %q, want %q", got, payload)
	}

	// Identical bytes dedup: a second put returns the same key and does not
	// error on the primary-key collision.
	key2, err := s.Put(ctx, payload)
	if err != nil {
		t.Fatalf("second put (dedup): %v", err)
	}
	if key2 != key {
		t.Fatalf("dedup broken: %q vs %q", key2, key)
	}
}

func TestPGBlobStoreGetUnknownIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newPGBlobStore(t)

	_, err := s.Get(ctx, blob.Hash([]byte("never stored")))
	if !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("get unknown: got %v, want ErrNotFound", err)
	}
}

func TestPGBlobStoreExists(t *testing.T) {
	ctx := context.Background()
	s := newPGBlobStore(t)
	key, _ := s.Put(ctx, []byte("runbook.pdf bytes"))

	ok, err := s.Exists(ctx, key)
	if err != nil || !ok {
		t.Fatalf("exists(stored) = %v, %v; want true, nil", ok, err)
	}
	ok, err = s.Exists(ctx, blob.Hash([]byte("absent")))
	if err != nil || ok {
		t.Fatalf("exists(absent) = %v, %v; want false, nil", ok, err)
	}
}
