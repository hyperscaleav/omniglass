package blob

import (
	"context"
	"sync"
)

// Mem is an in-memory Store. It is the backend for the pure contract tests and a
// convenient dev/test double for consumers of the blob primitive; the shipped
// product default is the pgblobs backend in the storage package.
type Mem struct {
	mu    sync.RWMutex
	blobs map[string][]byte
}

// NewMem returns an empty in-memory blob store.
func NewMem() *Mem {
	return &Mem{blobs: make(map[string][]byte)}
}

// Put stores data under its content hash, deduplicating on the key.
func (m *Mem) Put(_ context.Context, data []byte) (string, error) {
	key := Hash(data)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.blobs[key]; !ok {
		cp := make([]byte, len(data))
		copy(cp, data)
		m.blobs[key] = cp
	}
	return key, nil
}

// Get returns the bytes under key, verifying the content hash.
func (m *Mem) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	data, ok := m.blobs[key]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	if Hash(data) != key {
		return nil, ErrCorrupt
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp, nil
}

// Exists reports whether a blob is stored under key.
func (m *Mem) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	_, ok := m.blobs[key]
	m.mu.RUnlock()
	return ok, nil
}

// Len reports how many distinct blobs are stored. It backs the dedup assertion
// in the contract tests.
func (m *Mem) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.blobs)
}
