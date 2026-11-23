package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/sso"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Single sign-on through an OIDC identity provider (#2362, ADR-2361). Sign-in
// starts at /auth/oidc/start/{provider}: a fresh state and a browser binding
// are minted, only their hashes stored, the binding set in a short-lived
// httpOnly SameSite=Lax cookie, and the browser sent to the provider with the
// PKCE challenge and nonce derived from the binding. The provider sends it
// back to /auth/oidc/callback, which spends the state once, refuses a state
// the browser's cookie does not carry (login CSRF), exchanges the code,
// verifies the id_token, and either signs the linked person in through the
// path a password sign-in takes or, for a self-link flow, links the identity
// to the signed-in person who started it. These are browser endpoints at the
// root, beside /oauth, not API operations.

const (
	// oidcStateLifetime bounds a sign-in between start and callback.
	oidcStateLifetime = 10 * time.Minute
	// oidcBindingCookie is the cookie binding a sign-in to its browser.
	oidcBindingCookie = "og-oidc"
	// oidcCookiePath scopes the binding cookie to the sign-in endpoints.
	oidcCookiePath = "/auth/oidc"
	// loginPath and profilePath are the console pages a sign-in lands on.
	loginPath   = "/web/login"
	profilePath = "/web/profile"
)

// ssoClientTTL is how long a dialed provider (its discovery and key set) is
// kept before it is discovered again.
const ssoClientTTL = 10 * time.Minute

// oidcRandom is a fresh 256-bit value, URL-safe.
func oidcRandom() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// oidcHash is the stored form of a state or a binding.
func oidcHash(v string) []byte {
	sum := sha256.Sum256([]byte(v))
	return sum[:]
}

// ssoDialer dials the configured provider through the install's egress
// policy, keeping each dialed client a while.
type ssoDialer struct {
	gw    storage.Gateway
	eo    egress.Options
	cache *sso.Cache
}

// errSSOOff is a provider name that is not configured.
var errSSOOff = errors.New("api: single sign-on is not configured")

// dial reads the provider and its policy and returns its client.
func (d *ssoDialer) dial(ctx context.Context, name, origin string) (*storage.SSOProvider, *sso.Client, error) {
	p, err := d.gw.SSOProvider(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	if p == nil {
		return nil, nil, errSSOOff
	}
	pol, err := d.gw.EgressPolicy(ctx)
	if err != nil {
		return nil, nil, err
	}
	redirect := origin + sso.CallbackPath
	key := strings.Join([]string{p.Issuer, p.ClientID, string(oidcHash(p.ClientSecret)), strings.Join(p.Scopes, " "), redirect,
		fmt.Sprint(pol.Prefixes), strings.Join(pol.Hosts, ",")}, "\x00")
	c, err := d.cache.Get(ctx, key, func(ctx context.Context) (*sso.Client, error) {
		return sso.Dial(ctx, p, egress.New(pol, d.eo).HTTPClient(), redirect)
	})
	return p, c, err
}

// beginOIDC mints a sign-in's state and binding, stores their hashes, and
// returns the provider URL to send the browser to and the binding cookie.
func (a *authenticator) beginOIDC(ctx context.Context, provider, origin, linkUserID string, linkSession []byte) (string, http.Cookie, error) {
	_, c, err := a.sso.dial(ctx, provider, origin)
	if err != nil {
		return "", http.Cookie{}, err
	}
	state, err := oidcRandom()
	if err != nil {
		return "", http.Cookie{}, err
	}
	binding, err := oidcRandom()
	if err != nil {
		return "", http.Cookie{}, err
	}
	if err := a.gw.CreateOIDCState(ctx, storage.OIDCState{StateHash: oidcHash(state), BindingHash: oidcHash(binding),
		Provider: provider, LinkUserID: linkUserID, LinkSessionHash: linkSession,
		ExpiresAt: time.Now().Add(oidcStateLifetime)}); err != nil {
		return "", http.Cookie{}, err
	}
	verifier, nonce := sso.Derive(binding)
	return c.AuthCodeURL(state, verifier, nonce), a.bindingCookie(binding, int(oidcStateLifetime.Seconds())), nil
}

func (a *authenticator) bindingCookie(value string, maxAge int) http.Cookie {
	return http.Cookie{Name: oidcBindingCookie, Value: value, Path: oidcCookiePath, HttpOnly: true,
		Secure: a.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: maxAge}
}

// mountOIDC puts the sign-in endpoints on the root router.
func mountOIDC(r chi.Router, a *authenticator, o options) {
	r.Get("/auth/oidc/start/{provider}", func(w http.ResponseWriter, req *http.Request) {
		provider := chi.URLParam(req, "provider")
		to, cookie, err := a.beginOIDC(req.Context(), provider, requestOrigin(req, o.trustedProxies), "", nil)
		if err != nil {
			code := sso.ErrUnreachable
			if errors.Is(err, errSSOOff) {
				code = sso.ErrNotConfigured
			}
			refuseOIDC(w, req, o, loginPath, code, err.Error(), "provider", provider)
			return
		}
		http.SetCookie(w, &cookie)
		http.Redirect(w, req, to, http.StatusFound)
	})
	r.Get(sso.CallbackPath, func(w http.ResponseWriter, req *http.Request) {
		a.oidcCallback(w, req, o)
	})
}

func withQuery(path, k, v string) string { return path + "?" + url.Values{k: {v}}.Encode() }

// refuseOIDC sends a refused sign-in or self-link back to page with its code
// (?sso_error=<code>, one of the closed set the console has a fixed message
// for) and logs why: the detail (the provider's own error, a verification
// failure, the identity it vouched for) stays in this log line, never in the
// URL, so a crafted link cannot put words on the page (#2362).
func refuseOIDC(w http.ResponseWriter, r *http.Request, o options, page string, code sso.ErrorCode, reason string, attrs ...any) {
	o.log.Warn("sso: sign-in refused", append([]any{"code", string(code), "reason", reason}, attrs...)...)
	http.Redirect(w, r, withQuery(page, "sso_error", string(code)), http.StatusFound)
}

// oidcCallback completes a sign-in or a self-link.
func (a *authenticator) oidcCallback(w http.ResponseWriter, r *http.Request, o options) {
	ctx := r.Context()
	fail := func(code sso.ErrorCode, reason string, attrs ...any) {
		refuseOIDC(w, r, o, loginPath, code, reason, attrs...)
	}
	clear := a.bindingCookie("", -1)
	http.SetCookie(w, &clear)
	q := r.URL.Query()
	st, err := a.gw.ConsumeOIDCState(ctx, oidcHash(q.Get("state")))
	if err != nil {
		fail(sso.ErrState, "the state is unknown, expired or already spent: "+err.Error())
		return
	}
	failTo := fail
	if st.LinkUserID != "" {
		failTo = func(code sso.ErrorCode, reason string, attrs ...any) {
			refuseOIDC(w, r, o, profilePath, code, reason, append([]any{"self_link_user", st.LinkUserID}, attrs...)...)
		}
	}
	bc, err := r.Cookie(oidcBindingCookie)
	if err != nil || subtle.ConstantTimeCompare(oidcHash(bc.Value), st.BindingHash) != 1 {
		failTo(sso.ErrBrowser, "the browser holds no binding for this state, or another sign-in's (login CSRF)")
		return
	}
	if e := q.Get("error"); e != "" {
		failTo(sso.ErrProvider, "the provider answered with an error", "provider_error", e, "provider_error_description", q.Get("error_description"))
		return
	}
	_, c, err := a.sso.dial(ctx, st.Provider, requestOrigin(r, o.trustedProxies))
	if err != nil {
		failTo(sso.ErrUnreachable, "the callback could not reach the provider: "+err.Error())
		return
	}
	verifier, nonce := sso.Derive(bc.Value)
	in, err := c.Exchange(ctx, q.Get("code"), verifier, nonce)
	if err != nil {
		failTo(sso.ErrToken, "the provider's answer was refused: "+err.Error())
		return
	}
	identifier := in.Claims.Identifier()
	if identifier == "" {
		failTo(sso.ErrNoSubject, "the verified id_token names no subject")
		return
	}
	if st.LinkUserID != "" {
		a.completeSelfLink(w, r, st, identifier, failTo)
		return
	}
	p, err := a.gw.SSOProvider(ctx, st.Provider)
	if err != nil || p == nil {
		fail(sso.ErrNotConfigured, fmt.Sprintf("the provider %q is no longer configured (%v)", st.Provider, err))
		return
	}
	token, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		fail(sso.ErrFailed, "mint the session: "+err.Error())
		return
	}
	// A link with no refresh token gets no long-lived access (ADR-2361
	// point 9): the session lasts the grace, which is also the link's, since
	// nothing will re-check it. A sign-in that brings no new token keeps the
	// link's stored one, so storage picks the lifetime.
	now := time.Now()
	ua, ip := r.UserAgent(), clientIP(r, o.trustedProxies)
	u, expires, err := a.gw.SignInByIdentity(ctx, storage.SSOSignIn{Identifier: identifier, RefreshToken: in.RefreshToken, Grace: p.Grace,
		SessionHash: hash, Prefix: prefix, ExpiresAt: now.Add(sessionCookieLifetime), GraceExpiresAt: now.Add(p.Grace),
		UserAgent: ua, ClientIP: ip})
	switch {
	case errors.Is(err, storage.ErrIdentityNotLinked):
		// The identity is named here, in the log, for an administrator to
		// link; the person's page says only that it is not linked.
		fail(sso.ErrUnlinked, "the identity is linked to no person", "identity", identifier)
		return
	case errors.Is(err, storage.ErrAccountDisabled):
		if u != nil {
			_ = a.gw.WriteAuthEvent(ctx, u.ID, "login_denied")
		}
		fail(sso.ErrDisabled, "the linked person's account is disabled", "identity", identifier)
		return
	case err != nil:
		fail(sso.ErrFailed, "sign in by identity: "+err.Error(), "identity", identifier)
		return
	}
	cookie := a.sessionCookie(token)
	cookie.MaxAge = int(time.Until(expires).Seconds())
	http.SetCookie(w, &cookie)
	http.Redirect(w, r, "/web/", http.StatusFound)
}

// completeSelfLink links the identity to the person who started the flow,
// only while the session that started it is still the one presented.
func (a *authenticator) completeSelfLink(w http.ResponseWriter, r *http.Request, st *storage.OIDCState, identifier string, fail func(sso.ErrorCode, string, ...any)) {
	ctx := r.Context()
	tok, ok := sessionCookieToken(r.Header.Get("Cookie"))
	if !ok || subtle.ConstantTimeCompare(auth.HashToken(tok), st.LinkSessionHash) != 1 {
		fail(sso.ErrSession, "the callback carries no session, or not the one that started the link")
		return
	}
	u, err := a.gw.AuthenticateBearer(ctx, auth.HashToken(tok))
	if err != nil || u.ID != st.LinkUserID {
		fail(sso.ErrSession, "the session that started the link no longer authenticates as its person")
		return
	}
	switch _, err := a.gw.LinkIdentity(ctx, u.ID, u.ID, identifier); {
	case errors.Is(err, storage.ErrIdentityLinked):
		fail(sso.ErrAlreadyLinked, "the identity is linked to another person", "identity", identifier)
		return
	case err != nil:
		fail(sso.ErrFailed, "link the identity: "+err.Error(), "identity", identifier)
		return
	}
	http.Redirect(w, r, withQuery(profilePath, "sso_linked", "1"), http.StatusFound)
}
