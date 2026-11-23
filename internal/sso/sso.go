// Package sso is single sign-on through an OIDC identity provider (#2362,
// ADR-2361): the client that discovers the provider, sends a person to sign
// in (authorization code, PKCE S256, a state and a nonce), exchanges the code
// and verifies the id_token, and the re-check sweep that redeems each link's
// refresh token and revokes what the provider refuses. Every provider call
// goes through the egress contract under the install's allow list; the
// id_token is verified by the one JWT verifier (auth.JWTVerifier).
package sso

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// CallbackPath is where every provider sends the browser back, under the
// origin the request reached Omniglass at; registered at the provider.
const CallbackPath = "/auth/oidc/callback"

// ErrRefused is a refresh the provider refused (invalid_grant): the person
// may no longer sign in there. Any other failure is transient.
var ErrRefused = errors.New("sso: the provider refused the refresh")

// Client talks to one provider.
type Client struct {
	provider *storage.SSOProvider
	hc       *http.Client
	conf     oauth2.Config
	verifier *auth.JWTVerifier
}

// discovery is what the provider's discovery document names.
type discovery struct {
	JWKSURI string `json:"jwks_uri"`
}

// Dial discovers p through hc (the egress contract's client). redirectURL is
// the callback the code is sent to; empty for the sweep, which never sends a
// person anywhere.
func Dial(ctx context.Context, p *storage.SSOProvider, hc *http.Client, redirectURL string) (*Client, error) {
	op, err := oidc.NewProvider(oidc.ClientContext(ctx, hc), p.Issuer)
	if err != nil {
		return nil, fmt.Errorf("sso: discover %s: %w", p.Issuer, err)
	}
	var d discovery
	if err := op.Claims(&d); err != nil || d.JWKSURI == "" {
		return nil, fmt.Errorf("sso: the discovery document at %s names no jwks_uri", p.Issuer)
	}
	return &Client{
		provider: p, hc: hc,
		conf: oauth2.Config{ClientID: p.ClientID, ClientSecret: p.ClientSecret, Endpoint: op.Endpoint(),
			RedirectURL: redirectURL, Scopes: p.Scopes},
		verifier: auth.NewJWTVerifier(ctx, hc, p.Issuer, d.JWKSURI, p.ClientID, nil),
	}, nil
}

// Derive is the PKCE verifier and the nonce of a sign-in, from the binding
// value its browser holds, so neither is stored: only a request carrying the
// cookie can recompute them. Pure.
func Derive(binding string) (verifier, nonce string) {
	mac := func(label string) string {
		m := hmac.New(sha256.New, []byte(binding))
		m.Write([]byte(label))
		return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	}
	return mac("pkce-verifier"), mac("nonce")
}

// AuthCodeURL is where a person is sent to sign in.
func (c *Client) AuthCodeURL(state, verifier, nonce string) string {
	return c.conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))
}

// SignIn is what a completed exchange vouches for.
type SignIn struct {
	Claims       *auth.JWTClaims
	RefreshToken string
}

func (c *Client) ctx(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, c.hc)
}

// Exchange trades a code for tokens and verifies the id_token: signature,
// issuer, audience, expiry and the sign-in's nonce.
func (c *Client) Exchange(ctx context.Context, code, verifier, nonce string) (*SignIn, error) {
	tok, err := c.conf.Exchange(c.ctx(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("sso: the code exchange failed: %s", exchangeReason(err))
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return nil, errors.New("sso: the provider answered without an id_token")
	}
	claims, err := c.verifier.VerifyNonce(ctx, raw, nonce)
	if err != nil {
		return nil, err
	}
	return &SignIn{Claims: claims, RefreshToken: tok.RefreshToken}, nil
}

// Refresh redeems a refresh token, returning the rotated one (empty when the
// provider kept the old). A refusal is ErrRefused.
func (c *Client) Refresh(ctx context.Context, refresh string) (string, error) {
	if refresh == "" {
		return "", ErrRefused
	}
	tok, err := c.conf.TokenSource(c.ctx(ctx), &oauth2.Token{RefreshToken: refresh}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && (re.ErrorCode == "invalid_grant" || re.ErrorCode == "unauthorized_client") {
			return "", ErrRefused
		}
		return "", fmt.Errorf("sso: the refresh failed: %s", exchangeReason(err))
	}
	if tok.RefreshToken == refresh {
		return "", nil
	}
	return tok.RefreshToken, nil
}

// exchangeReason words a token endpoint failure without its body, which may
// echo what was sent.
func exchangeReason(err error) string {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		if re.ErrorCode != "" {
			return "the provider answered " + re.ErrorCode
		}
		if re.Response != nil {
			return fmt.Sprintf("the provider answered %d", re.Response.StatusCode)
		}
	}
	msg := err.Error()
	if i := strings.Index(msg, ": "); i >= 0 && strings.HasPrefix(msg, "Post ") {
		msg = msg[i+2:]
	}
	return msg
}

// Cache keeps a dialed client per provider configuration and policy for a
// while, so a sign-in does not rediscover the provider and refetch its keys
// on every request; a changed configuration is a new key.
type Cache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]cached
}

type cached struct {
	c  *Client
	at time.Time
}

// NewCache keeps clients for ttl.
func NewCache(ttl time.Duration) *Cache { return &Cache{ttl: ttl, m: map[string]cached{}} }

// Get returns the client for key, dialing it when absent or stale.
func (k *Cache) Get(ctx context.Context, key string, dial func(context.Context) (*Client, error)) (*Client, error) {
	k.mu.Lock()
	e, ok := k.m[key]
	k.mu.Unlock()
	if ok && time.Since(e.at) < k.ttl {
		return e.c, nil
	}
	c, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	for key, e := range k.m {
		if time.Since(e.at) >= k.ttl {
			delete(k.m, key)
		}
	}
	k.m[key] = cached{c: c, at: time.Now()}
	k.mu.Unlock()
	return c, nil
}
