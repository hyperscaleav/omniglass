// Package secret is the secret primitive: encrypt-at-rest of operator-set
// sensitive values, resolved down the cascade and interpolated (masked) into
// requests. The crypto is envelope encryption behind a pluggable key provider,
// so the key-encryption key (KEK) can move from an env var to a KMS or Vault
// with no change to callers or storage.
package secret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Provider holds the key-encryption key (KEK) and wraps and unwraps per-value
// data-encryption keys (DEKs). The KEK never leaves the provider; envelope
// encryption means the value payload never reaches the provider either, only
// the small DEK, so a KMS or Vault implementation drops in behind this seam
// with no model change.
type Provider interface {
	// WrapDEK generates a fresh 32-byte DEK and returns it plaintext (for the
	// caller to encrypt with) alongside its wrapped form and the id of the KEK
	// that wrapped it (for rotation-aware unwrap).
	WrapDEK(ctx context.Context) (dek, wrapped []byte, keyID string, err error)
	// UnwrapDEK recovers a DEK from its wrapped form under the KEK identified by
	// keyID. An unknown or non-current keyID is an error, not a panic.
	UnwrapDEK(ctx context.Context, wrapped []byte, keyID string) (dek []byte, err error)
}

// StaticProvider is the default provider: a single local KEK (from an env var,
// a file, or a warned random fallback), wrapping DEKs with AES-256-GCM. KMS and
// Vault providers implement the same interface later.
type StaticProvider struct {
	kek   []byte
	keyID string
}

// NewStaticProvider builds a provider over a 32-byte KEK. The key id is a stable
// short digest of the KEK, so a rotation changes the id and a wrapped DEK
// records which KEK sealed it.
func NewStaticProvider(kek []byte) *StaticProvider {
	sum := sha256.Sum256(kek)
	return &StaticProvider{kek: kek, keyID: "static:" + hex.EncodeToString(sum[:4])}
}

// KeyID is the current KEK's id, stamped onto every envelope this provider
// seals.
func (p *StaticProvider) KeyID() string { return p.keyID }

func (p *StaticProvider) WrapDEK(ctx context.Context) (dek, wrapped []byte, keyID string, err error) {
	dek = make([]byte, 32)
	if _, err = rand.Read(dek); err != nil {
		return nil, nil, "", fmt.Errorf("secret: generate dek: %w", err)
	}
	wrapped, err = aesGCMSeal(p.kek, dek, []byte(p.keyID))
	if err != nil {
		return nil, nil, "", fmt.Errorf("secret: wrap dek: %w", err)
	}
	return dek, wrapped, p.keyID, nil
}

func (p *StaticProvider) UnwrapDEK(ctx context.Context, wrapped []byte, keyID string) ([]byte, error) {
	if keyID != p.keyID {
		return nil, fmt.Errorf("secret: unknown key id %q (current %q)", keyID, p.keyID)
	}
	dek, err := aesGCMOpen(p.kek, wrapped, []byte(p.keyID))
	if err != nil {
		return nil, fmt.Errorf("secret: unwrap dek: %w", err)
	}
	return dek, nil
}

// aesGCMSeal encrypts plaintext under key with a fresh random nonce, binding
// aad, and returns nonce||ciphertext. Used to wrap a DEK under the KEK.
func aesGCMSeal(key, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

// aesGCMOpen reverses aesGCMSeal: the leading nonce is split off, then the
// remainder is authenticated and decrypted with aad.
func aesGCMOpen(key, blob, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return nil, fmt.Errorf("secret: ciphertext too short")
	}
	return gcm.Open(nil, blob[:ns], blob[ns:], aad)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: aes cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
