package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// The JWKS-backed JWT verifier (#2362, ADR-2361 point 8): one primitive for
// every signed token a provider hands Omniglass, an SSO id_token today and a
// Cloudflare Access assertion (#81) next. The key set is fetched through the
// caller's client (the egress contract's), cached by kid and refetched on an
// unknown one; a token is accepted only when its signature verifies against
// it, its iss is the configured issuer exactly, its aud holds the audience
// (and azp names it when aud holds several), and exp and iat are within
// JWTSkew of now. The signature and claim checks are go-oidc's, not ours.

// JWTSkew is the clock difference tolerated on exp and iat.
const JWTSkew = time.Minute

// ErrJWTInvalid is a token the verifier refuses; the wrapped reason says why
// and never quotes the token.
var ErrJWTInvalid = errors.New("auth: the token is not valid")

// JWTVerifier verifies the tokens of one issuer for one audience.
type JWTVerifier struct {
	v        *oidc.IDTokenVerifier
	audience string
	now      func() time.Time
}

// JWTClaims are a verified token's standard claims; Raw holds every claim,
// for the ones a caller reads itself (a groups claim, email).
type JWTClaims struct {
	Issuer   string
	Subject  string
	Audience []string
	Nonce    string
	Expiry   time.Time
	IssuedAt time.Time
	Raw      map[string]any
}

// NewJWTVerifier verifies tokens from issuer for audience against the key set
// at jwksURL, fetched with hc. now is the clock (nil is time.Now).
func NewJWTVerifier(ctx context.Context, hc *http.Client, issuer, jwksURL, audience string, now func() time.Time) *JWTVerifier {
	if now == nil {
		now = time.Now
	}
	// The key set keeps the context it is made with for its later fetches, so
	// it is detached from the caller's cancellation and carries the client.
	ks := oidc.NewRemoteKeySet(oidc.ClientContext(context.WithoutCancel(ctx), hc), jwksURL)
	v := oidc.NewVerifier(issuer, ks, &oidc.Config{
		ClientID: audience,
		// exp is judged JWTSkew late, so a token that expired a moment ago on
		// a clock slightly ahead of ours still verifies.
		Now: func() time.Time { return now().Add(-JWTSkew) },
		// RS256 and ES256 only: no none, no HMAC with a public key.
		SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256, oidc.PS256},
	})
	return &JWTVerifier{v: v, audience: audience, now: now}
}

// Verify checks raw and returns its claims, or ErrJWTInvalid with the reason.
func (j *JWTVerifier) Verify(ctx context.Context, raw string) (*JWTClaims, error) {
	t, err := j.v.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrJWTInvalid, err)
	}
	if t.IssuedAt.After(j.now().Add(JWTSkew)) {
		return nil, fmt.Errorf("%w: issued in the future", ErrJWTInvalid)
	}
	var all map[string]any
	if err := t.Claims(&all); err != nil {
		return nil, fmt.Errorf("%w: unreadable claims", ErrJWTInvalid)
	}
	// With several audiences the authorized party must be us (OIDC Core 3.1.3.7).
	if len(t.Audience) > 1 {
		azp, _ := all["azp"].(string)
		if azp != j.audience {
			return nil, fmt.Errorf("%w: several audiences and azp is not this client", ErrJWTInvalid)
		}
	}
	return &JWTClaims{Issuer: t.Issuer, Subject: t.Subject, Audience: t.Audience, Nonce: t.Nonce,
		Expiry: t.Expiry, IssuedAt: t.IssuedAt, Raw: all}, nil
}

// VerifyNonce is Verify plus the nonce check an id_token answering a sign-in
// takes: the token must carry exactly the nonce the sign-in sent.
func (j *JWTVerifier) VerifyNonce(ctx context.Context, raw, nonce string) (*JWTClaims, error) {
	c, err := j.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(c.Nonce), []byte(nonce)) != 1 {
		return nil, fmt.Errorf("%w: the nonce does not match the sign-in", ErrJWTInvalid)
	}
	return c, nil
}

// StringClaim reads a string claim, empty when absent or not a string.
func (c *JWTClaims) StringClaim(name string) string {
	s, _ := c.Raw[name].(string)
	return s
}

// BoolClaim reads a boolean claim, which some providers send as a string.
func (c *JWTClaims) BoolClaim(name string) bool {
	switch v := c.Raw[name].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

// The identifier an identity is linked by (ADR-2361 point 1): iss and sub
// exactly as the token carries them, never normalized.
func (c *JWTClaims) Identifier() string { return IdentityIdentifier(c.Issuer, c.Subject) }

// IdentityIdentifier joins an issuer and a subject as a link's identifier.
func IdentityIdentifier(issuer, subject string) string {
	if issuer == "" || subject == "" {
		return ""
	}
	return issuer + "|" + subject
}
