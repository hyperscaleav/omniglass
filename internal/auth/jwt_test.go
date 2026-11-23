package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/auth/oidctest"
	"github.com/hyperscaleav/omniglass/internal/egress"
)

// The JWT verifier (#2362) against the fake provider's real key set over
// TLS: each refusal is a token the provider really signed (or signed with a
// key it does not publish), not a hand-built string.
func TestJWTVerifierAcceptsTheProvidersTokenAndRefusesEveryTamper(t *testing.T) {
	ctx := context.Background()
	idp := oidctest.New(t)
	pol, err := egress.ParsePolicy([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	hc := egress.New(pol, egress.Options{RootCAs: idp.Roots()}).HTTPClient()
	v := auth.NewJWTVerifier(ctx, hc, idp.Issuer(), idp.Issuer()+"/jwks", oidctest.ClientID, nil)

	c, err := v.VerifyNonce(ctx, idp.Mint(t, "alice-sub", "n1"), "n1")
	if err != nil {
		t.Fatalf("a good token: %v", err)
	}
	if c.Identifier() != idp.Issuer()+"|alice-sub" {
		t.Fatalf("identifier = %q", c.Identifier())
	}

	cases := []struct {
		name  string
		setup func()
		nonce string
	}{
		{"bad signature", idp.SignWithForeignKey, "n1"},
		{"wrong iss", func() { idp.Tamper(func(c map[string]any) { c["iss"] = "https://evil.example" }) }, "n1"},
		{"wrong aud", func() { idp.Tamper(func(c map[string]any) { c["aud"] = "someone-else" }) }, "n1"},
		{"expired", func() {
			idp.Tamper(func(c map[string]any) { c["exp"] = time.Now().Add(-5 * time.Minute).Unix() })
		}, "n1"},
		{"issued in the future", func() {
			idp.Tamper(func(c map[string]any) { c["iat"] = time.Now().Add(10 * time.Minute).Unix() })
		}, "n1"},
		{"several audiences, azp not us", func() {
			idp.Tamper(func(c map[string]any) { c["aud"] = []string{oidctest.ClientID, "other"}; c["azp"] = "other" })
		}, "n1"},
		{"nonce mismatch", func() {}, "n2"},
	}
	for _, tc := range cases {
		tc.setup()
		tok := idp.Mint(t, "alice-sub", "n1")
		if _, err := v.VerifyNonce(ctx, tok, tc.nonce); !errors.Is(err, auth.ErrJWTInvalid) {
			t.Errorf("%s: err = %v, want ErrJWTInvalid", tc.name, err)
		}
	}
}
