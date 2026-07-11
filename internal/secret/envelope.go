package secret

import (
	"context"
	"crypto/rand"
)

// Envelope is the stored form of one encrypted secret field: the value
// encrypted under a per-value DEK (ciphertext + nonce), the DEK itself wrapped
// under the provider's KEK, and the id of that KEK for rotation-aware decrypt.
// The plaintext value is never stored; only this envelope is.
type Envelope struct {
	Ciphertext []byte `json:"ct"`
	Nonce      []byte `json:"nonce"`
	WrappedDEK []byte `json:"wdek"`
	KeyID      string `json:"kid"`
}

// Seal encrypts plaintext into an Envelope. A fresh DEK is generated and wrapped
// by the provider; the value is then AES-256-GCM encrypted under that DEK with
// aad bound (the caller passes owner-arc|key|field, so a ciphertext cannot be
// lifted into another secret row).
func Seal(ctx context.Context, p Provider, plaintext, aad []byte) (Envelope, error) {
	dek, wrapped, keyID, err := p.WrapDEK(ctx)
	if err != nil {
		return Envelope{}, err
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, aad)
	return Envelope{Ciphertext: ct, Nonce: nonce, WrappedDEK: wrapped, KeyID: keyID}, nil
}

// Open reverses Seal: unwrap the DEK under the recorded KeyID, then authenticate
// and decrypt the ciphertext with the same aad. A wrong aad, wrong KEK, or
// tampered ciphertext fails the GCM auth check.
func Open(ctx context.Context, p Provider, env Envelope, aad []byte) ([]byte, error) {
	dek, err := p.UnwrapDEK(ctx, env.WrappedDEK, env.KeyID)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, env.Nonce, env.Ciphertext, aad)
}
