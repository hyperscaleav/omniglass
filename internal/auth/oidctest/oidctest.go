// Package oidctest is a fake OpenID Connect provider for tests (#2362): a TLS
// test server serving discovery, a JWKS, an authorization endpoint that signs
// in whichever subject the test chose, and a token endpoint that exchanges a
// code (PKCE S256 checked) and redeems a refresh token, issuing RS256
// id_tokens. Knobs let a test tamper with the next id_token, sign it with a
// key the JWKS does not hold, withhold refresh tokens, refuse a subject's
// refresh (invalid_grant) or answer every refresh 503.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// ClientID and ClientSecret are the one client the provider knows.
const (
	ClientID     = "omniglass-test"
	ClientSecret = "test-client-secret"
)

// Provider is the fake identity provider.
type Provider struct {
	srv     *httptest.Server
	key     *rsa.PrivateKey
	foreign *rsa.PrivateKey

	mu       sync.Mutex
	subject  string
	claims   map[string]any
	tamper   func(map[string]any)
	badSig   bool
	noRefres bool
	down     bool
	codes    map[string]grant
	refresh  map[string]grant
	revoked  map[string]bool
	// Refreshes counts refresh_token grants answered, refused or not.
	refreshes int
}

// grant is what a code or a refresh token was issued for.
type grant struct {
	subject   string
	nonce     string
	challenge string
	redirect  string
	claims    map[string]any
}

// New starts a provider, closed when the test ends.
func New(t testing.TB) *Provider {
	t.Helper()
	p := &Provider{key: mustKey(t), foreign: mustKey(t), subject: "alice-sub",
		codes: map[string]grant{}, refresh: map[string]grant{}, revoked: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("/jwks", p.jwks)
	mux.HandleFunc("/authorize", p.authorize)
	mux.HandleFunc("/token", p.token)
	p.srv = httptest.NewTLSServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func mustKey(t testing.TB) *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Issuer is the provider's issuer URL.
func (p *Provider) Issuer() string { return p.srv.URL }

// Roots trust the provider's certificate.
func (p *Provider) Roots() *x509.CertPool {
	r := x509.NewCertPool()
	r.AddCert(p.srv.Certificate())
	return r
}

// SignIn makes the next authorization sign in subject, with extra claims in
// its id_tokens.
func (p *Provider) SignIn(subject string, claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subject, p.claims = subject, claims
}

// Tamper edits the claims of the next id_token the code exchange issues.
func (p *Provider) Tamper(f func(claims map[string]any)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tamper = f
}

// SignWithForeignKey makes the next id_token signed by a key the JWKS lacks.
func (p *Provider) SignWithForeignKey() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.badSig = true
}

// WithholdRefresh stops issuing refresh tokens.
func (p *Provider) WithholdRefresh() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.noRefres = true
}

// Revoke refuses every later refresh for subject (invalid_grant), as a
// provider does once the person is removed.
func (p *Provider) Revoke(subject string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revoked[subject] = true
}

// Down answers every refresh 503 while down is true.
func (p *Provider) Down(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = down
}

// Refreshes is how many refresh grants reached the token endpoint.
func (p *Provider) Refreshes() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refreshes
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                p.Issuer(),
		"authorization_endpoint":                p.Issuer() + "/authorize",
		"token_endpoint":                        p.Issuer() + "/token",
		"jwks_uri":                              p.Issuer() + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &p.key.PublicKey, KeyID: "k1", Algorithm: string(jose.RS256), Use: "sig"}}})
}

// authorize signs in the chosen subject at once and sends the browser back.
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	code := random()
	p.mu.Lock()
	p.codes[code] = grant{subject: p.subject, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"),
		redirect: q.Get("redirect_uri"), claims: p.claims}
	p.mu.Unlock()
	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	bq := back.Query()
	bq.Set("code", code)
	bq.Set("state", q.Get("state"))
	back.RawQuery = bq.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_request")
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != ClientID || secret != ClientSecret {
		oauthErr(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var g grant
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		c, ok := p.codes[r.PostForm.Get("code")]
		delete(p.codes, r.PostForm.Get("code"))
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || c.redirect != r.PostForm.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge {
			oauthErr(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		g = c
	case "refresh_token":
		p.refreshes++
		if p.down {
			oauthErr(w, http.StatusServiceUnavailable, "temporarily_unavailable")
			return
		}
		c, ok := p.refresh[r.PostForm.Get("refresh_token")]
		if !ok || p.revoked[c.subject] {
			oauthErr(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		delete(p.refresh, r.PostForm.Get("refresh_token"))
		c.nonce = ""
		g = c
	default:
		oauthErr(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	idt, err := p.idToken(g)
	if err != nil {
		oauthErr(w, http.StatusInternalServerError, "server_error")
		return
	}
	out := map[string]any{"access_token": random(), "token_type": "Bearer", "expires_in": 300, "id_token": idt}
	if !p.noRefres {
		rt := random()
		p.refresh[rt] = g
		out["refresh_token"] = rt
	}
	writeJSON(w, http.StatusOK, out)
}

// idToken signs g's id_token, applying and clearing the one-shot knobs.
func (p *Provider) idToken(g grant) (string, error) {
	now := time.Now()
	claims := map[string]any{"iss": p.Issuer(), "sub": g.subject, "aud": ClientID,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()}
	if g.nonce != "" {
		claims["nonce"] = g.nonce
	}
	for k, v := range g.claims {
		claims[k] = v
	}
	if p.tamper != nil {
		p.tamper(claims)
		p.tamper = nil
	}
	key := p.key
	if p.badSig {
		key, p.badSig = p.foreign, false
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return jws.CompactSerialize()
}

func random() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func oauthErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

// Mint signs an id_token for subject carrying nonce, as the code exchange
// would, the one-shot knobs applied: for a test of the verifier alone.
func (p *Provider) Mint(t testing.TB, subject, nonce string) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	tok, err := p.idToken(grant{subject: subject, nonce: nonce, claims: p.claims})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}
