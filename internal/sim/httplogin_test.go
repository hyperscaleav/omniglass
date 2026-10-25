package sim

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// The http login faces (#1114): a form login answering a session cookie, a
// JSON login answering a token with its lifetime, and RFC 7616 digest with
// a nonce that goes stale. Each session lives in the device's memory, and
// the console can expire them all, as a component's reboot would.

func serve(dev *Device, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, r)
	return w
}

func formPost(path, user, pass string) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(url.Values{"username": {user}, "password": {pass}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestHTTPFormLoginOpensASessionCookie(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{Mode: HTTPAuthForm, User: "admin", Password: "newtron", LoginPath: "/login"})

	w := serve(dev, httptest.NewRequest("GET", "/api/status", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Fatalf("no session = %d to %q, want a redirect to the login page", w.Code, w.Header().Get("Location"))
	}
	if w := serve(dev, httptest.NewRequest("GET", "/login", nil)); w.Code != 200 || !strings.Contains(w.Body.String(), "<form") {
		t.Fatalf("the login page = %d %q, want a form", w.Code, w.Body.String())
	}
	w = serve(dev, formPost("/login", "admin", "wrong"))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login?error=1" || len(w.Result().Cookies()) != 0 {
		t.Fatalf("a wrong password = %d to %q with %v, want back to the login page and no cookie", w.Code, w.Header().Get("Location"), w.Result().Cookies())
	}
	w = serve(dev, formPost("/login", "admin", "newtron"))
	cookies := w.Result().Cookies()
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" || len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatalf("the right password = %d to %q with %v, want a session cookie and a redirect home", w.Code, w.Header().Get("Location"), cookies)
	}
	r := httptest.NewRequest("GET", "/api/status", nil)
	r.AddCookie(cookies[0])
	if w := serve(dev, r); w.Code != 200 {
		t.Fatalf("a request in the session = %d, want 200", w.Code)
	}
	if dev.HTTPLogins() != 2 {
		t.Fatalf("logins = %d, want both attempts counted", dev.HTTPLogins())
	}

	if n := dev.ExpireHTTPSessions(); n != 1 {
		t.Fatalf("expired %d sessions, want 1", n)
	}
	r = httptest.NewRequest("GET", "/api/status", nil)
	r.AddCookie(cookies[0])
	if w := serve(dev, r); w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Fatalf("an expired session = %d, want back to the login page", w.Code)
	}
}

func TestHTTPTokenLoginAnswersATokenAndItsLifetime(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{Mode: HTTPAuthToken, User: "admin", Password: "newtron", LoginPath: "/api/login", SessionTTL: 600})
	now := time.Unix(1_700_000_000, 0)
	dev.SetClock(func() time.Time { return now })

	login := func(pass string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/login", strings.NewReader(fmt.Sprintf(`{"username":"admin","password":%q}`, pass)))
		r.Header.Set("Content-Type", "application/json")
		return serve(dev, r)
	}
	if w := login("wrong"); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "wrong") {
		t.Fatalf("a wrong password = %d %q, want a 401 that echoes nothing", w.Code, w.Body.String())
	}
	w := login("newtron")
	var got struct {
		Token     string  `json:"token"`
		ExpiresIn float64 `json:"expires_in"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Token == "" || got.ExpiresIn != 600 {
		t.Fatalf("the right password = %d %q, want a token and its lifetime", w.Code, w.Body.String())
	}
	bearer := func(tok string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/status", nil)
		if tok != "" {
			r.Header.Set("Authorization", "Bearer "+tok)
		}
		return serve(dev, r)
	}
	if w := bearer(got.Token); w.Code != 200 {
		t.Fatalf("a request with the token = %d", w.Code)
	}
	if w := bearer(""); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "unauthorized") {
		t.Fatalf("no token = %d %q", w.Code, w.Body.String())
	}
	now = now.Add(601 * time.Second)
	if w := bearer(got.Token); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "token expired") {
		t.Fatalf("a token past its lifetime = %d %q, want the expired answer", w.Code, w.Body.String())
	}
}

// digestAnswer is the test's own reading of RFC 7616: the Authorization a
// client sends for one challenge.
func digestAnswer(challenge, method, uri, user, pass, nc string) string {
	p := map[string]string{}
	for _, kv := range strings.Split(strings.TrimPrefix(challenge, "Digest "), ", ") {
		k, v, _ := strings.Cut(kv, "=")
		p[k] = strings.Trim(v, `"`)
	}
	var h func() hash.Hash = md5.New
	if p["algorithm"] == "SHA-256" {
		h = sha256.New
	}
	hx := func(s string) string { x := h(); x.Write([]byte(s)); return hex.EncodeToString(x.Sum(nil)) }
	ha1, ha2 := hx(user+":"+p["realm"]+":"+pass), hx(method+":"+uri)
	resp := hx(ha1 + ":" + p["nonce"] + ":" + nc + ":cn:auth:" + ha2)
	return fmt.Sprintf(`Digest username=%q, realm=%q, nonce=%q, uri=%q, algorithm=%s, response=%q, qop=auth, nc=%s, cnonce="cn", opaque=%q`,
		user, p["realm"], p["nonce"], uri, p["algorithm"], resp, nc, p["opaque"])
}

func digestChallengeOf(w *httptest.ResponseRecorder) string {
	for _, v := range w.Result().Header.Values("WWW-Authenticate") {
		if strings.HasPrefix(v, "Digest ") {
			return v
		}
	}
	return ""
}

func TestHTTPDigestChallengesAndGoesStale(t *testing.T) {
	for _, alg := range []string{"MD5", "SHA-256"} {
		t.Run(alg, func(t *testing.T) {
			dev := newAuthDevice(t, HTTPAuth{Mode: HTTPAuthDigest, User: "admin", Password: "boreal", Realm: "Boreal Edge Admin", Algorithm: alg})
			w := serve(dev, httptest.NewRequest("GET", "/api/status", nil))
			ch := digestChallengeOf(w)
			if w.Code != http.StatusUnauthorized || !strings.Contains(ch, `realm="Boreal Edge Admin"`) || !strings.Contains(ch, "algorithm="+alg) || !strings.Contains(ch, `qop="auth"`) {
				t.Fatalf("no answer = %d %q, want a digest challenge", w.Code, ch)
			}
			req := func(auth string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/api/status", nil)
				r.Header.Set("Authorization", auth)
				return serve(dev, r)
			}
			if w := req(digestAnswer(ch, "GET", "/api/status", "admin", "boreal", "00000001")); w.Code != 200 {
				t.Fatalf("the right answer = %d", w.Code)
			}
			// A count already answered is a replay: stale, answered afresh.
			if w := req(digestAnswer(ch, "GET", "/api/status", "admin", "boreal", "00000001")); w.Code != http.StatusUnauthorized || !strings.Contains(digestChallengeOf(w), "stale=true") {
				t.Fatalf("a replayed count = %d %q, want a stale challenge", w.Code, digestChallengeOf(w))
			}
			if w := req(digestAnswer(ch, "GET", "/api/status", "admin", "boreal", "00000002")); w.Code != 200 {
				t.Fatalf("the next count = %d", w.Code)
			}
			// The expired sessions rotate the nonce: the old one is stale.
			dev.ExpireHTTPSessions()
			w = req(digestAnswer(ch, "GET", "/api/status", "admin", "boreal", "00000003"))
			fresh := digestChallengeOf(w)
			if w.Code != http.StatusUnauthorized || !strings.Contains(fresh, "stale=true") || fresh == ch {
				t.Fatalf("the old nonce = %d %q, want a fresh stale challenge", w.Code, fresh)
			}
			if w := req(digestAnswer(fresh, "GET", "/api/status", "admin", "boreal", "00000001")); w.Code != 200 {
				t.Fatalf("an answer to the fresh nonce = %d", w.Code)
			}
			// A wrong password is a plain challenge, never stale.
			w = req(digestAnswer(fresh, "GET", "/api/status", "admin", "wrong", "00000002"))
			if w.Code != http.StatusUnauthorized || strings.Contains(digestChallengeOf(w), "stale") {
				t.Fatalf("a wrong password = %d %q, want a challenge that is not stale", w.Code, digestChallengeOf(w))
			}
		})
	}
}

func TestHTTPLoginFacesTrustTheConsoleAndLetItExpireSessions(t *testing.T) {
	for _, mode := range []HTTPAuthMode{HTTPAuthForm, HTTPAuthToken, HTTPAuthDigest} {
		t.Run(string(mode), func(t *testing.T) {
			dev := newAuthDevice(t, HTTPAuth{Mode: mode, User: "admin", Password: "pw", LoginPath: "/login", ExpirePath: "/api/admin/sessions/expire"})
			if res := dev.Operate(Operation{Kind: "http", Path: "/api/status"}); res.Status != 200 {
				t.Fatalf("the console's read = %+v, want it answered in process", res)
			}
			res := dev.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/admin/sessions/expire"})
			if res.Status != 200 || !strings.Contains(res.Body, `"expired"`) {
				t.Fatalf("the console's expiry = %+v", res)
			}
			// The expiry path is the console's alone.
			if w := serve(dev, httptest.NewRequest("POST", "/api/admin/sessions/expire", nil)); w.Code == 200 {
				t.Fatalf("the expiry path answered a network request: %d", w.Code)
			}
		})
	}
}

func TestHTTPAuthFromProfileReadsTheLoginFaces(t *testing.T) {
	form := httpAuthFromProfile(profile.Auth{Type: "form", DefaultUser: "admin", DefaultPassword: "pw", Enforce: true, ExpirePath: "/x"})
	if form.Mode != HTTPAuthForm || form.LoginPath != "/login" || form.ExpirePath != "/x" {
		t.Fatalf("form = %+v, want /login by default", form)
	}
	token := httpAuthFromProfile(profile.Auth{Type: "token", DefaultUser: "admin", DefaultPassword: "pw", Enforce: true, SessionTTL: 30})
	if token.Mode != HTTPAuthToken || token.LoginPath != "/api/login" || token.SessionTTL != 30 {
		t.Fatalf("token = %+v, want /api/login by default", token)
	}
	digest := httpAuthFromProfile(profile.Auth{Type: "digest", DefaultUser: "admin", DefaultPassword: "pw", Enforce: true})
	if digest.Mode != HTTPAuthDigest || digest.Algorithm != "MD5" {
		t.Fatalf("digest = %+v, want MD5 by default", digest)
	}
}
