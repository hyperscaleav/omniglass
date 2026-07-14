package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/blob"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgBlobStore is the default (pgblobs) blob.Store backend: content-addressed
// bytes held inline in the Postgres blob table. It is the single-binary,
// no-external-dependency story; an S3-compatible or disk backend implements the
// same blob.Store and is injected with WithBlobStore.
type pgBlobStore struct {
	pool *pgxpool.Pool
}

// NewPGBlobStore builds the default Postgres-backed blob store over a pool. It
// is exported so a deployment (or a test) can wire the backend explicitly, the
// same way an S3 or disk backend would be constructed and passed to
// WithBlobStore.
func NewPGBlobStore(pool *pgxpool.Pool) blob.Store {
	return &pgBlobStore{pool: pool}
}

// Put stores data under its content hash, deduplicating on the primary key: a
// second write of identical bytes is an ON CONFLICT DO NOTHING no-op that still
// returns the key.
func (s *pgBlobStore) Put(ctx context.Context, data []byte) (string, error) {
	key := blob.Hash(data)
	if _, err := s.pool.Exec(ctx,
		`insert into blob (sha256, bytes, size) values ($1, $2, $3)
		 on conflict (sha256) do nothing`,
		key, data, len(data)); err != nil {
		return "", fmt.Errorf("storage: put blob: %w", err)
	}
	return key, nil
}

// Get returns the bytes under key, verifying they still hash to key. A missing
// row is blob.ErrNotFound; a hash mismatch is blob.ErrCorrupt.
func (s *pgBlobStore) Get(ctx context.Context, key string) ([]byte, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `select bytes from blob where sha256 = $1`, key).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, blob.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get blob: %w", err)
	}
	if blob.Hash(data) != key {
		return nil, blob.ErrCorrupt
	}
	return data, nil
}

// Exists reports whether a blob is stored under key, without reading its bytes.
func (s *pgBlobStore) Exists(ctx context.Context, key string) (bool, error) {
	var one int
	err := s.pool.QueryRow(ctx, `select 1 from blob where sha256 = $1`, key).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: blob exists: %w", err)
	}
	return true, nil
}
