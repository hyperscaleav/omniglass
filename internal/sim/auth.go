package sim

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// httpAuthFromProfile derives a runtime HTTPAuth from a profile's auth
// block. It only returns an enforcing config when Enforce is true: a
// profile may declare credentials for documentation (so the console
// can render "default user: admin" hints) without forcing every
// request to authenticate. Unrecognized auth types fall back to no
// gate, with a warning logged at startup elsewhere.
func httpAuthFromProfile(a profile.Auth) HTTPAuth {
	if !a.Enforce {
		return HTTPAuth{}
	}
	switch a.Type {
	case "basic":
		return HTTPAuth{
			Mode:        HTTPAuthBasic,
			User:        a.DefaultUser,
			Password:    a.DefaultPassword,
			Realm:       a.Realm,
			PublicPaths: append([]string(nil), a.PublicPaths...),
		}
	case "header":
		return HTTPAuth{
			Mode:        HTTPAuthHeader,
			APIKey:      a.APIKey,
			HeaderName:  a.Header,
			PublicPaths: append([]string(nil), a.PublicPaths...),
		}
	case "bearer":
		return HTTPAuth{
			Mode:        HTTPAuthBearer,
			APIKey:      a.APIKey,
			Realm:       a.Realm,
			PublicPaths: append([]string(nil), a.PublicPaths...),
		}
	case "query":
		return HTTPAuth{
			Mode:        HTTPAuthQuery,
			APIKey:      a.APIKey,
			QueryName:   a.Query,
			PublicPaths: append([]string(nil), a.PublicPaths...),
		}
	case "form", "token", "digest":
		h := HTTPAuth{
			Mode:        HTTPAuthMode(a.Type),
			User:        a.DefaultUser,
			Password:    a.DefaultPassword,
			Realm:       a.Realm,
			PublicPaths: append([]string(nil), a.PublicPaths...),
			LoginPath:   a.LoginPath,
			SessionTTL:  a.SessionTTL,
			Algorithm:   a.Algorithm,
			ExpirePath:  a.ExpirePath,
		}
		switch {
		case h.LoginPath == "" && h.Mode == HTTPAuthForm:
			h.LoginPath = "/login"
		case h.LoginPath == "" && h.Mode == HTTPAuthToken:
			h.LoginPath = "/api/login"
		}
		if h.Mode == HTTPAuthDigest && h.Algorithm == "" {
			h.Algorithm = "MD5"
		}
		return h
	default:
		return HTTPAuth{}
	}
}

// defaultAPIKeyHeader is the header consulted when an HTTPAuthHeader
// device leaves HeaderName empty. Matches the convention used by
// most AV gear that ships an API key (Crestron, QSC, Biamp, etc).
const defaultAPIKeyHeader = "X-API-Key"

// defaultAPIKeyQuery is the query parameter consulted when an
// HTTPAuthQuery device leaves QueryName empty.
const defaultAPIKeyQuery = "apikey"

// checkHTTPAuth enforces the device's HTTPAuth setting against r.
// Returns true when the request is allowed to proceed; returns false
// after writing a 401 response when it is not. Callers must stop
// handling the request when this returns false.
//
// Intentionally narrow: this is a teaching example of common AV-gear
// auth patterns, not a production credential gate. Comparisons use
// constant-time equality so the lab matches the discipline a real
// implementation would need, but TLS, replay protection, lockouts,
// and rotation are out of scope.
func (d *Device) checkHTTPAuth(w http.ResponseWriter, r *http.Request) bool {
	switch d.HTTPAuth.Mode {
	case HTTPAuthNone:
		return true
	}

	// PublicPaths bypass applies to every gated mode. Useful for a
	// landing page or token-acquisition flow that the user has to be
	// able to reach without credentials.
	for _, p := range d.HTTPAuth.PublicPaths {
		if r.URL.Path == p {
			return true
		}
	}

	// The console's own operations are trusted past a login face (#1114),
	// as an operator at the component's screen would be.
	switch d.HTTPAuth.Mode {
	case HTTPAuthForm, HTTPAuthToken, HTTPAuthDigest:
		if isConsole(r) {
			return true
		}
	}
	if allowed, handled := d.checkLoginFace(w, r); handled {
		return allowed
	}
	return d.checkStaticAuth(w, r, d.HTTPAuth)
}

// checkStaticAuth enforces one of the static modes (basic, a header key, a
// bearer token, a query key) of a, writing a 401 and returning false when
// the request does not carry the credential: the http face's gate, and a
// websocket face's own (#1034). An unknown mode fails closed.
func (d *Device) checkStaticAuth(w http.ResponseWriter, r *http.Request, a HTTPAuth) bool {
	switch a.Mode {
	case HTTPAuthNone:
		return true

	case HTTPAuthBasic:
		user, pass, ok := r.BasicAuth()
		if ok && constantTimeEqual(user, a.User) && constantTimeEqual(pass, a.Password) {
			return true
		}
		realm := a.Realm
		if realm == "" {
			realm = d.Hostname
		}
		// RFC 7617: realm value must be quoted; embedded quotes
		// would break the header. Strip them defensively rather
		// than ship a profile that produces an invalid challenge.
		realm = strings.ReplaceAll(realm, `"`, "")
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s", charset="UTF-8"`, realm))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","auth":"basic"}` + "\n"))
		return false

	case HTTPAuthHeader:
		header := a.HeaderName
		if header == "" {
			header = defaultAPIKeyHeader
		}
		got := r.Header.Get(header)
		if got != "" && constantTimeEqual(got, a.APIKey) {
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":"unauthorized","auth":"header","header":"%s"}`+"\n", header)
		return false

	case HTTPAuthBearer:
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if ok && got != "" && constantTimeEqual(got, a.APIKey) {
			return true
		}
		realm := strings.ReplaceAll(a.Realm, `"`, "")
		if realm == "" {
			realm = d.Hostname
		}
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s"`, realm))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","auth":"bearer"}` + "\n"))
		return false

	case HTTPAuthQuery:
		name := a.QueryName
		if name == "" {
			name = defaultAPIKeyQuery
		}
		got := r.URL.Query().Get(name)
		if got != "" && constantTimeEqual(got, a.APIKey) {
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":"unauthorized","auth":"query","param":"%s"}`+"\n", name)
		return false

	default:
		// Unknown mode: fail closed so a typo in a profile is loud
		// rather than silently wide-open.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":"unauthorized","auth":"unknown:%s"}`+"\n", a.Mode)
		return false
	}
}

// constantTimeEqual compares two strings in constant time. A length
// mismatch short-circuits with a single ConstantTimeCompare on the
// raw bytes so the comparison time leaks length but not content,
// which matches the standard library pattern.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
