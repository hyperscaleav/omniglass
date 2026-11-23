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
		to, cookie, err := a.beginOIDC(req.Context(), chi.URLParam(req, "provider"), requestOrigin(req, o.trustedProxies), "", nil)
		if err != nil {
			o.log.Warn("sso: sign-in could not start", "err", err.Error())
			msg := "Single sign-on could not start: the identity provider is unreachable or misconfigured. Sign in with your password, or ask an administrator."
			if errors.Is(err, errSSOOff) {
				msg = "Single sign-on is not configured on this server."
			}
			http.Redirect(w, req, withQuery(loginPath, "sso_error", msg), http.StatusFound)
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

// oidcCallback completes a sign-in or a self-link.
func (a *authenticator) oidcCallback(w http.ResponseWriter, r *http.Request, o options) {
	ctx := r.Context()
	fail := func(msg string) {
		http.Redirect(w, r, withQuery(loginPath, "sso_error", msg), http.StatusFound)
	}
	clear := a.bindingCookie("", -1)
	http.SetCookie(w, &clear)
	q := r.URL.Query()
	st, err := a.gw.ConsumeOIDCState(ctx, oidcHash(q.Get("state")))
	if err != nil {
		fail("This sign-in has expired or was already used. Start again.")
		return
	}
	failTo := fail
	if st.LinkUserID != "" {
		failTo = func(msg string) {
			http.Redirect(w, r, withQuery(profilePath, "sso_error", msg), http.StatusFound)
		}
	}
	bc, err := r.Cookie(oidcBindingCookie)
	if err != nil || subtle.ConstantTimeCompare(oidcHash(bc.Value), st.BindingHash) != 1 {
		failTo("This sign-in was not started in this browser. Start again from the sign-in page.")
		return
	}
	if e := q.Get("error"); e != "" {
		failTo("The identity provider did not sign you in (" + e + ").")
		return
	}
	_, c, err := a.sso.dial(ctx, st.Provider, requestOrigin(r, o.trustedProxies))
	if err != nil {
		o.log.Warn("sso: callback could not reach the provider", "err", err.Error())
		failTo("The identity provider is unreachable or misconfigured. Sign in with your password, or ask an administrator.")
		return
	}
	verifier, nonce := sso.Derive(bc.Value)
	in, err := c.Exchange(ctx, q.Get("code"), verifier, nonce)
	if err != nil {
		o.log.Warn("sso: the provider's answer was refused", "err", err.Error())
		failTo("The identity provider's answer did not verify, so you are not signed in.")
		return
	}
	identifier := in.Claims.Identifier()
	if identifier == "" {
		failTo("The identity provider's answer names no subject.")
		return
	}
	if st.LinkUserID != "" {
		a.completeSelfLink(w, r, st, identifier, failTo)
		return
	}
	p, err := a.gw.SSOProvider(ctx, st.Provider)
	if err != nil || p == nil {
		fail("Single sign-on is not configured on this server.")
		return
	}
	token, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		fail("Sign-in failed. Try again.")
		return
	}
	// A provider that issues no refresh token gets no long-lived access
	// (ADR-2361 point 9): the session lasts the grace, which is also the
	// link's, since nothing will re-check it.
	lifetime := sessionCookieLifetime
	if in.RefreshToken == "" {
		lifetime = p.Grace
	}
	ua, ip := r.UserAgent(), clientIP(r, o.trustedProxies)
	u, err := a.gw.SignInByIdentity(ctx, storage.SSOSignIn{Identifier: identifier, RefreshToken: in.RefreshToken, Grace: p.Grace,
		SessionHash: hash, Prefix: prefix, ExpiresAt: time.Now().Add(lifetime), UserAgent: ua, ClientIP: ip})
	switch {
	case errors.Is(err, storage.ErrIdentityNotLinked):
		fail("Your identity at the provider (" + identifier + ") is not linked to an Omniglass person. " +
			"An administrator links it from your person page, or sign in with your password and link it from your profile.")
		return
	case errors.Is(err, storage.ErrAccountDisabled):
		if u != nil {
			_ = a.gw.WriteAuthEvent(ctx, u.ID, "login_denied")
		}
		fail("account disabled")
		return
	case err != nil:
		o.log.Error("sso: sign-in", "err", err.Error())
		fail("Sign-in failed. Try again.")
		return
	}
	cookie := a.sessionCookie(token)
	cookie.MaxAge = int(lifetime.Seconds())
	http.SetCookie(w, &cookie)
	http.Redirect(w, r, "/web/", http.StatusFound)
}

// completeSelfLink links the identity to the person who started the flow,
// only while the session that started it is still the one presented.
func (a *authenticator) completeSelfLink(w http.ResponseWriter, r *http.Request, st *storage.OIDCState, identifier string, fail func(string)) {
	ctx := r.Context()
	tok, ok := sessionCookieToken(r.Header.Get("Cookie"))
	if !ok || subtle.ConstantTimeCompare(auth.HashToken(tok), st.LinkSessionHash) != 1 {
		fail("Linking needs the session that started it. Sign in and start again from your profile.")
		return
	}
	u, err := a.gw.AuthenticateBearer(ctx, auth.HashToken(tok))
	if err != nil || u.ID != st.LinkUserID {
		fail("Linking needs the session that started it. Sign in and start again from your profile.")
		return
	}
	switch _, err := a.gw.LinkIdentity(ctx, u.ID, u.ID, identifier); {
	case errors.Is(err, storage.ErrIdentityLinked):
		fail("That identity is already linked to an Omniglass person.")
		return
	case err != nil:
		fail("Linking failed. Try again.")
		return
	}
	http.Redirect(w, r, withQuery(profilePath, "sso_linked", "1"), http.StatusFound)
}
