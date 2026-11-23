package sso_test

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sso"
)

// Derive is the PKCE verifier and nonce of a sign-in from its browser
// binding: stable for one binding, different across bindings and from each
// other, and a verifier RFC 7636 accepts (43 to 128 unreserved characters).
func TestDeriveIsStablePerBindingAndAValidVerifier(t *testing.T) {
	v1, n1 := sso.Derive("binding-one")
	v1b, n1b := sso.Derive("binding-one")
	v2, n2 := sso.Derive("binding-two")
	if v1 != v1b || n1 != n1b {
		t.Fatal("Derive is not stable for one binding")
	}
	if v1 == v2 || n1 == n2 || v1 == n1 {
		t.Fatal("Derive collides across bindings or between verifier and nonce")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`).MatchString(v1) {
		t.Fatalf("verifier %q is not an RFC 7636 verifier", v1)
	}
	sum := sha256.Sum256([]byte(v1))
	if base64.RawURLEncoding.EncodeToString(sum[:]) == v1 {
		t.Fatal("verifier equals its own challenge")
	}
}
