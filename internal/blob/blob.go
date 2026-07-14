// Package blob is the content-addressed blob store: the reusable Storage Gateway
// primitive that holds opaque bytes keyed by the sha256 of those bytes. The key
// is the content, so identical bytes dedup to one blob, the hash verifies the
// bytes on read (tamper-evident), and a stored blob is immutable (changing the
// bytes changes the key). A file handle, a large log body, or a raw collection
// payload references a blob by hash; rows never inline bytes.
//
// Store is the seam the backend swaps behind (pgblobs by default, an
// S3-compatible object store, or local disk) with no change to a consumer. Only
// the byte read/write resolution differs; the key is identical across backends.
package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ErrNotFound is returned by Get when no blob is stored under the key.
var ErrNotFound = errors.New("blob: not found")

// ErrCorrupt is returned by Get when the stored bytes do not hash to the key
// they are filed under: the content-addressing integrity check failed.
var ErrCorrupt = errors.New("blob: content hash mismatch")

// Hash is the content address of data: the lowercase hex sha256 of the bytes.
// It is the blob key, so two callers storing the same bytes compute the same
// key and dedup to one blob.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Store is the content-addressed byte store behind the Storage Gateway. The
// backend (pgblobs, S3, disk) is swappable behind this interface.
type Store interface {
	// Put stores data and returns its content key (Hash(data)). It is
	// idempotent and deduplicating: storing bytes already present is a no-op
	// that returns the same key.
	Put(ctx context.Context, data []byte) (key string, err error)
	// Get returns the bytes stored under key, verifying they still hash to key
	// (ErrCorrupt otherwise). ErrNotFound if no blob is stored under key.
	Get(ctx context.Context, key string) ([]byte, error)
	// Exists reports whether a blob is stored under key, without transferring
	// its bytes. It backs the dedup decision on the write path.
	Exists(ctx context.Context, key string) (bool, error)
}
