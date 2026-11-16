package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/clientaddr"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// OAuth 2.1 for MCP clients (#2048, ADR-2048). The MCP server is the protected
// resource and this server is its authorization server: a client finds both
// from the 401 the MCP endpoint answers, registers itself (RFC 7591), sends its
// person to the console to consent, and exchanges the code (PKCE, S256 only)
// for an agent token accepted on the MCP channel only and bound to the roles
// the person approved. These are protocol endpoints at the root, beside /mcp,
// not API operations; the consent itself is an API operation the console calls
// with the person's session.

const (
	// oauthCodeLifetime bounds a code between consent and exchange.
	oauthCodeLifetime = 5 * time.Minute
	// oauthTokenLifetime bounds an issued access token; the client renews it
	// with its refresh token (#2073).
	oauthTokenLifetime = time.Hour
	// oauthRefreshLifetime bounds a refresh token. Each refresh issues the next,
	// so a client in use stays signed in and one idle this long signs in again.
	oauthRefreshLifetime = 30 * 24 * time.Hour
	// oauthConsentPath is the console page a person consents on.
	oauthConsentPath = "/web/oauth/authorize"
	// oauthMaxRedirects bounds a registration's redirect URIs.
	oauthMaxRedirects = 10
	// oauthDefaultClientName names a client that registered without a name.
	oauthDefaultClientName = "MCP client"
)

// requestOrigin is the scheme and host the caller reached this server at, the
// base every advertised URL is built from. A forwarded scheme is believed only
// from a trusted proxy.
func requestOrigin(r *http.Request, trust clientaddr.Trust) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if trust.Proxied(r.RemoteAddr) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// mcpResource is the MCP server's resource identifier under an origin.
func mcpResource(origin string) string { return origin + "/mcp" }

// MCPResourceMetadata is the protected-resource metadata URL (RFC 9728) the
// MCP endpoint's 401 names, for the request it answers.
func MCPResourceMetadata(trust clientaddr.Trust) func(*http.Request) string {
	return func(r *http.Request) string {
		return requestOrigin(r, trust) + "/.well-known/oauth-protected-resource/mcp"
	}
}

// validRedirectURI reports whether a client may register a redirect URI:
// absolute, no fragment, and https, or plain http on a loopback host (a
// native client listening on this machine, RFC 8252).
func validRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Host == "" || u.User != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return isLoopbackHost(u.Hostname())
	}
	return false
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// clientAgentName is a client's declared name as an agent token's name: the
// characters an agent name allows, collapsed and bounded, or a default.
func clientAgentName(declared string) string {
	var b strings.Builder
	space := false
	for _, r := range declared {
		ok := r == '.' || r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		if b.Len() == 0 && !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 100 {
			break
		}
	}
	if b.Len() == 0 {
		return oauthDefaultClientName
	}
	return b.String()
}

// pkceMatches reports whether a code verifier answers an S256 challenge.
func pkceMatches(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

// newOAuthCode is a fresh authorization code and the hash it is stored by.
func newOAuthCode() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	code := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(code))
	return code, sum[:], nil
}

// hashOAuthCode is the stored form of a presented code.
func hashOAuthCode(code string) []byte {
	sum := sha256.Sum256([]byte(code))
	return sum[:]
}

// redirectWith is a client's redirect URI carrying the response parameters,
// its own query kept.
func redirectWith(redirectURI string, params url.Values) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// oauthEndpoints serves the protocol side of sign-in.
type oauthEndpoints struct {
	gw    storage.Gateway
	trust clientaddr.Trust
}

// mountOAuth puts the metadata, registration, authorization and token
// endpoints on the root router.
func mountOAuth(r chi.Router, gw storage.Gateway, trust clientaddr.Trust) {
	e := &oauthEndpoints{gw: gw, trust: trust}
	r.Get("/.well-known/oauth-protected-resource", cors(e.resourceMetadata))
	r.Get("/.well-known/oauth-protected-resource/mcp", cors(e.resourceMetadata))
	r.Get("/.well-known/oauth-authorization-server", cors(e.serverMetadata))
	r.Post("/oauth/register", cors(e.register))
	r.Get("/oauth/authorize", e.authorize)
	r.Post("/oauth/token", cors(e.token))
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-authorization-server", "/oauth/register", "/oauth/token"} {
		r.Options(p, cors(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	}
}

// cors opens an endpoint to a browser-based client. None of them reads a
// cookie: the metadata is public and registration and exchange authenticate
// by what the request carries.
func cors(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// oauthError is an OAuth error response (RFC 6749 section 5.2).
func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func (e *oauthEndpoints) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	origin := requestOrigin(r, e.trust)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 mcpResource(origin),
		"authorization_servers":    []string{origin},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Omniglass",
	})
}

func (e *oauthEndpoints) serverMetadata(w http.ResponseWriter, r *http.Request) {
	origin := requestOrigin(r, e.trust)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         origin,
		"authorization_endpoint":                         origin + "/oauth/authorize",
		"token_endpoint":                                 origin + "/oauth/token",
		"registration_endpoint":                          origin + "/oauth/register",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"authorization_response_iss_parameter_supported": true,
	})
}

// register is dynamic client registration (RFC 7591). Every client is public
// (no secret): it proves itself at the exchange with its PKCE verifier.
func (e *oauthEndpoints) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "the body is not a JSON client registration")
		return
	}
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > oauthMaxRedirects {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "register between 1 and 10 redirect URIs")
		return
	}
	for _, u := range in.RedirectURIs {
		if !validRedirectURI(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "a redirect URI must be https, or http on a loopback host: "+u)
			return
		}
	}
	for _, g := range in.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "only the authorization_code and refresh_token grants are supported")
			return
		}
	}
	for _, t := range in.ResponseTypes {
		if t != "code" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "only the code response type is supported")
			return
		}
	}
	if m := in.TokenEndpointAuthMethod; m != "" && m != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "clients are public: token_endpoint_auth_method is none")
		return
	}
	c, err := e.gw.RegisterOAuthClient(r.Context(), clientAgentName(in.ClientName), in.RedirectURIs)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "registration failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// authorize checks a request and sends the person to the console to consent.
// A request with an unknown client or an unregistered redirect URI is never
// redirected (RFC 6749 section 4.1.2.1): the URI cannot be trusted. Any other
// fault goes back to the client as an error on its redirect.
func (e *oauthEndpoints) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, found, err := e.gw.GetOAuthClient(r.Context(), q.Get("client_id"))
	if err != nil {
		http.Error(w, "authorization failed", http.StatusInternalServerError)
		return
	}
	redirect := q.Get("redirect_uri")
	if !found || !registered(c, redirect) {
		http.Error(w, "unknown client or unregistered redirect_uri", http.StatusBadRequest)
		return
	}
	origin := requestOrigin(r, e.trust)
	fail := func(code, desc string) {
		v := url.Values{"error": {code}, "error_description": {desc}, "iss": {origin}}
		if s := q.Get("state"); s != "" {
			v.Set("state", s)
		}
		http.Redirect(w, r, redirectWith(redirect, v), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only the code response type is supported")
		return
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "PKCE with the S256 method is required")
		return
	}
	resource := q.Get("resource")
	if resource == "" {
		resource = mcpResource(origin)
	} else if resource != mcpResource(origin) {
		fail("invalid_target", "the resource is not this server's MCP endpoint")
		return
	}
	next := url.Values{
		"client_id":             {c.ID},
		"redirect_uri":          {redirect},
		"code_challenge":        {q.Get("code_challenge")},
		"code_challenge_method": {"S256"},
		"resource":              {resource},
	}
	if s := q.Get("state"); s != "" {
		next.Set("state", s)
	}
	http.Redirect(w, r, oauthConsentPath+"?"+next.Encode(), http.StatusFound)
}

// registered reports whether a redirect URI is one the client registered,
// compared exactly.
func registered(c storage.OAuthClient, redirect string) bool {
	for _, u := range c.RedirectURIs {
		if u == redirect {
			return true
		}
	}
	return false
}

// token exchanges a code for an agent token. The code is spent by the first
// attempt to use it, right or wrong, so a stolen code that loses the race is
// worthless and one that wins it fails the client's own exchange.
func (e *oauthEndpoints) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "the body is not a form")
		return
	}
	f := r.PostForm
	switch f.Get("grant_type") {
	case "authorization_code":
	case "refresh_token":
		e.refresh(w, r, f)
		return
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "only the authorization_code and refresh_token grants are supported")
		return
	}
	code, found, err := e.gw.ConsumeOAuthCode(r.Context(), hashOAuthCode(f.Get("code")))
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "exchange failed")
		return
	}
	if !found || code.ClientID != f.Get("client_id") || code.RedirectURI != f.Get("redirect_uri") ||
		!pkceMatches(f.Get("code_verifier"), code.CodeChallenge) ||
		(f.Get("resource") != "" && f.Get("resource") != code.Resource) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the code is unknown, spent, expired, or does not match this request")
		return
	}
	c, found, err := e.gw.GetOAuthClient(r.Context(), code.ClientID)
	if err != nil || !found {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the client is gone")
		return
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "exchange failed")
		return
	}
	refresh, refreshHash, err := newOAuthCode()
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "exchange failed")
		return
	}
	now := time.Now()
	if err := e.gw.IssueOAuthToken(r.Context(), storage.OAuthTokenIssue{
		PrincipalID: code.PrincipalID, ClientID: c.ID, SecretHash: hash, Prefix: prefix,
		ExpiresAt: now.Add(oauthTokenLifetime), RefreshHash: refreshHash, RefreshExpiresAt: now.Add(oauthRefreshLifetime),
		Agent: c.Name, AgentRoles: code.Roles, UserAgent: r.UserAgent(), ClientIP: clientIP(r, e.trust),
	}); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "exchange failed")
		return
	}
	writeTokens(w, tok, refresh)
}

// refresh renews a client's access token with its refresh token, rotating
// both. A refresh token is spent by its first use; presenting it again ends
// the grant, so a leaked one stops working for the thief and the client alike
// and the person sees the client gone from their sessions.
func (e *oauthEndpoints) refresh(w http.ResponseWriter, r *http.Request, f url.Values) {
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "refresh failed")
		return
	}
	refresh, refreshHash, err := newOAuthCode()
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "refresh failed")
		return
	}
	now := time.Now()
	outcome, err := e.gw.RefreshOAuthToken(r.Context(), storage.OAuthRefresh{
		TokenHash: hashOAuthCode(f.Get("refresh_token")), ClientID: f.Get("client_id"),
		SecretHash: hash, Prefix: prefix, ExpiresAt: now.Add(oauthTokenLifetime),
		RefreshHash: refreshHash, RefreshExpiresAt: now.Add(oauthRefreshLifetime),
	})
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "refresh failed")
		return
	}
	if outcome != storage.OAuthRefreshed {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is unknown, spent, expired, or not this client's")
		return
	}
	writeTokens(w, tok, refresh)
}

// writeTokens is the token endpoint's success answer.
func writeTokens(w http.ResponseWriter, access, refresh string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(oauthTokenLifetime.Seconds()),
		"refresh_token": refresh,
	})
}
