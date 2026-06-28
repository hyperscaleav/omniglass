package auth_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
)

// TestNewBearerToken is a pure unit test (runs under -short): a minted token
// carries the scanner scheme, its stored hash matches re-hashing the cleartext,
// and two mints differ.
func TestNewBearerToken(t *testing.T) {
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !strings.HasPrefix(tok, auth.TokenScheme+"_") {
		t.Errorf("token %q lacks the %q scheme prefix", tok, auth.TokenScheme)
	}
	if !strings.Contains(tok, "_"+prefix+"_") {
		t.Errorf("token %q does not embed its locator %q", tok, prefix)
	}
	if !bytes.Equal(hash, auth.HashToken(tok)) {
		t.Error("returned hash does not match HashToken of the cleartext")
	}

	tok2, _, _, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint 2: %v", err)
	}
	if tok == tok2 {
		t.Error("two mints produced the same token")
	}
}
