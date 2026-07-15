package blob_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/blob"
)

func TestHashIsDeterministicAndContentAddressed(t *testing.T) {
	a := blob.Hash([]byte("firmware-v1"))
	again := blob.Hash([]byte("firmware-v1"))
	other := blob.Hash([]byte("firmware-v2"))

	if a != again {
		t.Fatalf("hash not deterministic: %q vs %q", a, again)
	}
	if a == other {
		t.Fatal("distinct bytes hashed to the same key")
	}
	if len(a) != 64 {
		t.Fatalf("sha256 hex key should be 64 chars, got %d (%q)", len(a), a)
	}
}

// The Store contract, exercised against the in-memory backend. The same
// contract is re-run against the real pgblobs backend in the storage package
// (the capability-wrapping merge gate).
func TestMemStorePutIsContentAddressedAndDedups(t *testing.T) {
	ctx := context.Background()
	s := blob.NewMem()

	key, err := s.Put(ctx, []byte("a capture"))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if key != blob.Hash([]byte("a capture")) {
		t.Fatalf("put returned %q, want the content hash", key)
	}

	// Identical bytes dedup to one blob: same key, count unchanged.
	key2, err := s.Put(ctx, []byte("a capture"))
	if err != nil {
		t.Fatalf("second put: %v", err)
	}
	if key2 != key {
		t.Fatalf("dedup broken: second put returned %q, want %q", key2, key)
	}
	if n := s.Len(); n != 1 {
		t.Fatalf("identical bytes stored as %d blobs, want 1 (dedup)", n)
	}
}

func TestMemStoreGetReturnsStoredBytes(t *testing.T) {
	ctx := context.Background()
	s := blob.NewMem()
	want := []byte("runbook contents")

	key, err := s.Put(ctx, want)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("get returned %q, want %q", got, want)
	}
}

func TestMemStoreGetUnknownKeyIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := blob.NewMem()

	_, err := s.Get(ctx, blob.Hash([]byte("never stored")))
	if !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("get of unknown key: got %v, want ErrNotFound", err)
	}
}

func TestMemStoreExists(t *testing.T) {
	ctx := context.Background()
	s := blob.NewMem()
	key, _ := s.Put(ctx, []byte("x"))

	ok, err := s.Exists(ctx, key)
	if err != nil || !ok {
		t.Fatalf("exists(stored) = %v, %v; want true, nil", ok, err)
	}
	ok, err = s.Exists(ctx, blob.Hash([]byte("y")))
	if err != nil || ok {
		t.Fatalf("exists(absent) = %v, %v; want false, nil", ok, err)
	}
}
