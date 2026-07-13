package secret

import (
	"bytes"
	"context"
	"testing"
)

// A fixed 32-byte key for deterministic tests.
func testKey(b byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = b
	}
	return k
}

func TestEnvelopeRoundTrip(t *testing.T) {
	p := NewStaticProvider(testKey(0x01))
	aad := []byte("component:dsp-1|snmp_community|community")

	env, err := Seal(context.Background(), p, []byte("public"), aad)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if len(env.Ciphertext) == 0 || len(env.Nonce) == 0 || len(env.WrappedDEK) == 0 {
		t.Fatalf("envelope missing parts: %+v", env)
	}
	if env.KeyID == "" {
		t.Fatalf("envelope missing key id")
	}
	if bytes.Contains(env.Ciphertext, []byte("public")) {
		t.Fatalf("ciphertext leaks plaintext")
	}

	got, err := Open(context.Background(), p, env, aad)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != "public" {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

// AAD binds the ciphertext to (owner, key, field): opening under a different AAD
// must fail, so a ciphertext copied into another secret row is useless.
func TestEnvelopeAADMismatchFails(t *testing.T) {
	p := NewStaticProvider(testKey(0x01))
	env, err := Seal(context.Background(), p, []byte("s3cr3t"), []byte("aad-A"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := Open(context.Background(), p, env, []byte("aad-B")); err == nil {
		t.Fatalf("expected AAD mismatch to fail, got nil")
	}
}

// A provider holding a different KEK cannot unwrap the DEK.
func TestEnvelopeWrongKeyFails(t *testing.T) {
	sealer := NewStaticProvider(testKey(0x01))
	env, err := Seal(context.Background(), sealer, []byte("s3cr3t"), []byte("aad"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	other := NewStaticProvider(testKey(0x02))
	if _, err := Open(context.Background(), other, env, []byte("aad")); err == nil {
		t.Fatalf("expected wrong-key open to fail, got nil")
	}
}

// KeyID is stable for a given KEK and differs across KEKs (so rotation is
// detectable and a stale key id routes to the right unwrap).
func TestKeyIDStableAndDistinct(t *testing.T) {
	a1 := NewStaticProvider(testKey(0x01))
	a2 := NewStaticProvider(testKey(0x01))
	b := NewStaticProvider(testKey(0x02))
	if a1.KeyID() != a2.KeyID() {
		t.Fatalf("same KEK gave different key ids: %s vs %s", a1.KeyID(), a2.KeyID())
	}
	if a1.KeyID() == b.KeyID() {
		t.Fatalf("different KEKs gave same key id: %s", a1.KeyID())
	}
}

// Unwrapping an envelope whose key id is unknown to the provider is a clean
// error, not a panic (the rotation-forward case where an old key id lingers).
func TestUnwrapUnknownKeyID(t *testing.T) {
	p := NewStaticProvider(testKey(0x01))
	env, err := Seal(context.Background(), p, []byte("x"), []byte("aad"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	env.KeyID = "static:deadbeef"
	if _, err := Open(context.Background(), p, env, []byte("aad")); err == nil {
		t.Fatalf("expected unknown key id to fail, got nil")
	}
}
