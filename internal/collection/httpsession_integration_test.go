package collection_test

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// http logins and digest over real sockets (#1114): a component behind a
// form login, one behind a JSON token login, one behind digest, each its own
// listener, so the jar, the captured token and the challenge all cross a
// real wire.

// loginComponent is a component behind a login: a form login answering a
// session cookie, or a JSON login answering a token, and a status document
// only a live session reads.
type loginComponent struct {
	mu       sync.Mutex
	password string
	sessions map[string]bool
	n        int
	// broken refuses every session, even a fresh one.
	broken      bool
	lifetime    string
	loginHits   atomic.Int32
	statusHits  atomic.Int32
	lastBearer  string
	lastCookies string
}

func newLoginComponent(t *testing.T) (*loginComponent, string) {
	t.Helper()
	c := &loginComponent{password: "s3cret-pass", sessions: map[string]bool{}, lifetime: "300"}
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	return c, srv.Listener.Addr().String()
}

func (c *loginComponent) open() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	v := fmt.Sprintf("sess-%d-%x", c.n, time.Now().UnixNano())
	c.sessions[v] = true
	return v
}

func (c *loginComponent) live(v string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.broken && c.sessions[v]
}

func (c *loginComponent) expire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions = map[string]bool{}
}

func (c *loginComponent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/login":
		c.loginHits.Add(1)
		_ = r.ParseForm()
		if r.PostForm.Get("username") != "admin" || r.PostForm.Get("password") != c.pass() {
			http.Redirect(w, r, "/login?error=1", http.StatusFound)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: c.open(), Path: "/", HttpOnly: true})
		http.Redirect(w, r, "/", http.StatusFound)
	case "/api/login":
		c.loginHits.Add(1)
		var in struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Username != "admin" || in.Password != c.pass() {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprintf(w, `{"error":"bad credentials for %s"}`, in.Password)
			return
		}
		c.mu.Lock()
		life := c.lifetime
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":%q,"expires_in":%s}`, c.open(), life)
	case "/status", "/api/status", "/echo":
		c.statusHits.Add(1)
		cookie, _ := r.Cookie("sid")
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		c.mu.Lock()
		c.lastBearer, c.lastCookies = bearer, r.Header.Get("Cookie")
		c.mu.Unlock()
		ok := (cookie != nil && c.live(cookie.Value)) || (bearer != "" && c.live(bearer))
		switch {
		case !ok && r.URL.Path == "/api/status":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"token expired"}`))
		case !ok:
			http.Redirect(w, r, "/login", http.StatusFound)
		case r.URL.Path == "/echo":
			// A component that echoes what it was sent in its refusal.
			w.WriteHeader(http.StatusBadRequest)
			v := bearer
			if cookie != nil {
				v = cookie.Value
			}
			_, _ = fmt.Fprintf(w, "bad request from session %s with password %s", v, c.pass())
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"power":"on"}`))
		}
	default:
		http.NotFound(w, r)
	}
}

func (c *loginComponent) pass() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.password
}

func formLoginSession() *driver.SessionSpec {
	return &driver.SessionSpec{Login: &driver.Login{
		Path: "/login",
		Steps: []driver.LoginStep{{Request: &driver.Request{Method: "POST", Path: "/login", BodyFormat: driver.BodyFormatForm,
			Body: json.RawMessage(`{"username":"${secret.login.username}","password":"${secret.login.password}"}`)}}},
	}}
}

func tokenLoginSession() *driver.SessionSpec {
	return &driver.SessionSpec{Login: &driver.Login{
		Steps: []driver.LoginStep{{Request: &driver.Request{Method: "POST", Path: "/api/login",
			Body: json.RawMessage(`{"username":"${secret.login.username}","password":"${secret.login.password}"}`)}}},
		Capture: []driver.Capture{{Name: "token", Extract: driver.Extract{JSONPath: "token"}}},
		Expiry:  &driver.Extract{JSONPath: "expires_in"},
		Headers: map[string]string{"Authorization": "Bearer ${session.token}"},
		Expired: &driver.Match{Regex: "token expired"},
	}}
}

func loginSecrets(pass string) map[string]map[string]string {
	return map[string]map[string]string{"login": {"username": "admin", "password": pass}}
}

func doHTTP(t *testing.T, r *collection.Runner, x collection.HTTPExchange, pass string) (collection.HTTPAnswer, error) {
	t.Helper()
	ans, sx, err := r.DoHTTP(context.Background(), x, loginSecrets(pass))
	if err != nil {
		return ans, err
	}
	return ans, collection.JudgeHTTPAnswer(ans, sx)
}

func TestHTTPFormLoginKeepsItsSessionAndLogsInOnceOnLapse(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, target := newLoginComponent(t)
	r := &collection.Runner{HTTPFetch: collection.NewHTTPFetcher()}
	state := collection.NewHTTPAuthState()
	x := collection.HTTPExchange{Target: target, Request: driver.Request{Path: "/status"}, Session: formLoginSession(), Timeout: 2 * time.Second, State: state}

	for i := range 3 {
		ans, err := doHTTP(t, r, x, "s3cret-pass")
		if err != nil || ans.Status != 200 || !ans.Credentialed {
			t.Fatalf("poll %d = %+v, %v", i, ans, err)
		}
	}
	if comp.loginHits.Load() != 1 || state.Logins() != 1 {
		t.Fatalf("three polls logged in %d times (state %d), want one session kept", comp.loginHits.Load(), state.Logins())
	}

	// The component forgets the session: the next request is sent back to
	// the login page, which logs in once more and replays.
	comp.expire()
	ans, err := doHTTP(t, r, x, "s3cret-pass")
	if err != nil || ans.Status != 200 {
		t.Fatalf("the poll after the lapse = %+v, %v", ans, err)
	}
	if comp.loginHits.Load() != 2 || comp.statusHits.Load() != 5 {
		t.Fatalf("logins %d, status reads %d, want one re-login and one replay", comp.loginHits.Load(), comp.statusHits.Load())
	}

	// A component that refuses even a fresh session: one re-login, then the
	// terminal refusal, never a second.
	comp.mu.Lock()
	comp.broken = true
	comp.mu.Unlock()
	_, err = doHTTP(t, r, x, "s3cret-pass")
	terr, ok := collection.AsTerminal(err)
	if !ok || terr.Class != driver.ClassAuth || !strings.Contains(terr.Reason, "fresh login") {
		t.Fatalf("a session refused after a fresh login = %v, want a terminal auth refusal", err)
	}
	if comp.loginHits.Load() != 3 {
		t.Fatalf("logins %d, want exactly one more", comp.loginHits.Load())
	}
}

func TestHTTPWrongPasswordIsRefusedOnceAndNeverRetried(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	for name, session := range map[string]*driver.SessionSpec{"form": formLoginSession(), "token": tokenLoginSession()} {
		t.Run(name, func(t *testing.T) {
			comp, target := newLoginComponent(t)
			r := &collection.Runner{HTTPFetch: collection.NewHTTPFetcher()}
			state := collection.NewHTTPAuthState()
			x := collection.HTTPExchange{Target: target, Request: driver.Request{Path: "/api/status"}, Session: session, Timeout: 2 * time.Second, State: state}
			for range 3 {
				_, err := doHTTP(t, r, x, "wrong-pass")
				terr, ok := collection.AsTerminal(err)
				if !ok || terr.Class != driver.ClassAuth {
					t.Fatalf("a wrong password = %v, want a terminal auth refusal", err)
				}
				if strings.Contains(err.Error(), "wrong-pass") {
					t.Fatalf("the refusal carries the password: %v", err)
				}
			}
			if comp.loginHits.Load() != 1 || comp.statusHits.Load() != 0 {
				t.Fatalf("logins %d, status reads %d, want exactly one login attempt and no request", comp.loginHits.Load(), comp.statusHits.Load())
			}
		})
	}
}

func TestHTTPTokenLoginRidesItsHeaderAndRenewsBeforeExpiry(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, target := newLoginComponent(t)
	comp.lifetime = "0.3"
	r := &collection.Runner{HTTPFetch: collection.NewHTTPFetcher()}
	state := collection.NewHTTPAuthState()
	x := collection.HTTPExchange{Target: target, Request: driver.Request{Path: "/api/status"}, Session: tokenLoginSession(), Timeout: 2 * time.Second, State: state}
	if ans, err := doHTTP(t, r, x, "s3cret-pass"); err != nil || ans.Status != 200 {
		t.Fatalf("first poll = %+v, %v", ans, err)
	}
	comp.mu.Lock()
	bearer := comp.lastBearer
	comp.mu.Unlock()
	if !strings.HasPrefix(bearer, "sess-1-") {
		t.Fatalf("the request carried bearer %q, want the captured token", bearer)
	}
	time.Sleep(350 * time.Millisecond)
	if ans, err := doHTTP(t, r, x, "s3cret-pass"); err != nil || ans.Status != 200 {
		t.Fatalf("poll past the lifetime = %+v, %v", ans, err)
	}
	if comp.loginHits.Load() != 2 || comp.statusHits.Load() != 2 {
		t.Fatalf("logins %d, status reads %d, want a renewal before the request, not a refused one", comp.loginHits.Load(), comp.statusHits.Load())
	}

	// The declared expired text lapses the session too.
	comp.lifetime = "300"
	if _, err := doHTTP(t, r, x, "s3cret-pass"); err != nil {
		t.Fatal(err)
	}
	comp.expire()
	if ans, err := doHTTP(t, r, x, "s3cret-pass"); err != nil || ans.Status != 200 {
		t.Fatalf("poll after the token expired = %+v, %v", ans, err)
	}
}

func TestHTTPConcurrentRequestsShareOneLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, target := newLoginComponent(t)
	r := &collection.Runner{HTTPFetch: collection.NewHTTPFetcher()}
	state := collection.NewHTTPAuthState()
	x := collection.HTTPExchange{Target: target, Request: driver.Request{Path: "/status"}, Session: formLoginSession(), Timeout: 2 * time.Second, State: state}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := doHTTP(t, r, x, "s3cret-pass"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if comp.loginHits.Load() != 1 {
		t.Fatalf("eight concurrent requests logged in %d times, want one", comp.loginHits.Load())
	}
}

func TestHTTPSessionScrubsWhatItHolds(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	for name, session := range map[string]*driver.SessionSpec{"form": formLoginSession(), "token": tokenLoginSession()} {
		t.Run(name, func(t *testing.T) {
			comp, target := newLoginComponent(t)
			r := &collection.Runner{HTTPFetch: collection.NewHTTPFetcher()}
			x := collection.HTTPExchange{Target: target, Request: driver.Request{Path: "/echo"}, Session: session, Timeout: 2 * time.Second, State: collection.NewHTTPAuthState()}
			_, err := doHTTP(t, r, x, "s3cret-pass")
			if _, ok := collection.AsRequestFault(err); !ok {
				t.Fatalf("an echoing 400 = %v, want a request fault", err)
			}
			if strings.Contains(err.Error(), "s3cret-pass") || strings.Contains(err.Error(), "sess-") {
				t.Fatalf("the reason carries a secret or the session: %v", err)
			}
			if comp.statusHits.Load() != 1 {
				t.Fatalf("status reads %d, want one", comp.statusHits.Load())
			}
		})
	}
}

// digestComponent answers RFC 7616 digest with its own reading of the
// spec: a nonce it rotates (calling the old one stale), and a count it
// holds the client to.
type digestComponent struct {
	mu        sync.Mutex
	algorithm string
	password  string
	nonce     string
	n         int
	lastNC    string
	hits      atomic.Int32
	answered  atomic.Int32
}

func (d *digestComponent) rotate() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n++
	d.nonce, d.lastNC = fmt.Sprintf("nonce-%d-%x", d.n, time.Now().UnixNano()), ""
}

func (d *digestComponent) challenge(w http.ResponseWriter, stale bool) {
	d.mu.Lock()
	v := fmt.Sprintf(`Digest realm="panel", qop="auth", algorithm=%s, nonce=%q, opaque="op-1"`, d.algorithm, d.nonce)
	d.mu.Unlock()
	if stale {
		v += ", stale=true"
	}
	w.Header().Add("WWW-Authenticate", `Basic realm="panel"`)
	w.Header().Add("WWW-Authenticate", v)
	w.WriteHeader(http.StatusUnauthorized)
}

func (d *digestComponent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.hits.Add(1)
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Digest ") {
		d.challenge(w, false)
		return
	}
	d.answered.Add(1)
	p := map[string]string{}
	for _, kv := range strings.Split(strings.TrimPrefix(auth, "Digest "), ", ") {
		k, v, _ := strings.Cut(kv, "=")
		p[k] = strings.Trim(v, `"`)
	}
	d.mu.Lock()
	nonce, pass, alg := d.nonce, d.password, d.algorithm
	d.mu.Unlock()
	var h func() hash.Hash = md5.New
	if alg == "SHA-256" {
		h = sha256.New
	}
	hx := func(s string) string { s2 := h(); s2.Write([]byte(s)); return hex.EncodeToString(s2.Sum(nil)) }
	ha1 := hx(p["username"] + ":panel:" + pass)
	ha2 := hx(r.Method + ":" + p["uri"])
	want := hx(ha1 + ":" + p["nonce"] + ":" + p["nc"] + ":" + p["cnonce"] + ":auth:" + ha2)
	if p["response"] != want || p["algorithm"] != alg || p["opaque"] != "op-1" {
		d.challenge(w, false)
		return
	}
	if p["nonce"] != nonce {
		d.challenge(w, true)
		return
	}
	d.mu.Lock()
	if p["nc"] <= d.lastNC {
		d.mu.Unlock()
		d.challenge(w, true)
		return
	}
	d.lastNC = p["nc"]
	d.mu.Unlock()
	_, _ = w.Write([]byte(`{"power":"on"}`))
}

func TestHTTPDigestAnswersItsChallengeAndAStaleNonce(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	for _, alg := range []string{"MD5", "SHA-256"} {
		t.Run(alg, func(t *testing.T) {
			comp := &digestComponent{algorithm: alg, password: "d1gest-pass"}
			comp.rotate()
			srv := httptest.NewServer(comp)
			defer srv.Close()
			r := &collection.Runner{HTTPFetch: collection.NewHTTPFetcher()}
			state := collection.NewHTTPAuthState()
			session := &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeDigest}
			cred := &driver.HTTPCredential{Scheme: driver.SchemeDigest, Username: "admin", Password: "d1gest-pass"}
			x := collection.HTTPExchange{Target: srv.Listener.Addr().String(), Request: driver.Request{Path: "/status"}, Credential: cred, Session: session, Timeout: 2 * time.Second, State: state}

			if ans, err := doHTTP(t, r, x, ""); err != nil || ans.Status != 200 {
				t.Fatalf("first request = %+v, %v", ans, err)
			}
			if comp.hits.Load() != 2 {
				t.Fatalf("first request took %d round trips, want the challenge and the answer", comp.hits.Load())
			}
			if ans, err := doHTTP(t, r, x, ""); err != nil || ans.Status != 200 || comp.hits.Load() != 3 {
				t.Fatalf("second request = %+v, %v after %d round trips, want the held challenge answered at once", ans, err, comp.hits.Load())
			}
			// A rotated nonce is stale, never a refusal: answered again once.
			comp.rotate()
			if ans, err := doHTTP(t, r, x, ""); err != nil || ans.Status != 200 || comp.hits.Load() != 5 {
				t.Fatalf("after the nonce rotated = %+v, %v after %d round trips", ans, err, comp.hits.Load())
			}
			// A wrong password is the credential's refusal after one answer.
			comp.mu.Lock()
			comp.password = "rotated"
			comp.mu.Unlock()
			before := comp.hits.Load()
			_, err := doHTTP(t, r, x, "")
			if terr, ok := collection.AsTerminal(err); !ok || terr.Class != driver.ClassAuth {
				t.Fatalf("a wrong digest password = %v, want a terminal auth refusal", err)
			}
			if got := comp.hits.Load() - before; got > 2 {
				t.Fatalf("a refused answer took %d round trips, want no loop", got)
			}
		})
	}
}

// A login step may carry a secret field in its query: a request that draws
// no answer names its URL with every one masked, however short.
func TestHTTPLoginNeverNamesItsSecretsInAnError(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	target := srv.Listener.Addr().String()
	srv.Close() // nothing answers there now
	session := &driver.SessionSpec{Login: &driver.Login{Steps: []driver.LoginStep{{Request: &driver.Request{Path: "/login",
		Query: map[string]string{"user": "${secret.login.username}", "pass": "${secret.login.password}"}}}}}}
	r := &collection.Runner{HTTPFetch: collection.NewHTTPFetcher()}
	x := collection.HTTPExchange{Target: target, Request: driver.Request{Path: "/status"}, Session: session, Timeout: time.Second, State: collection.NewHTTPAuthState()}
	_, _, err := r.DoHTTP(context.Background(), x, map[string]map[string]string{"login": {"username": "operator-7", "password": "p&w"}})
	if err == nil {
		t.Fatal("a login to a closed port succeeded")
	}
	if _, terminal := collection.AsTerminal(err); terminal {
		t.Fatalf("a refused connect = %v, want a retryable fault, not a refusal", err)
	}
	for _, leak := range []string{"p&w", "p%26w", "operator-7"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("the error names %q: %v", leak, err)
		}
	}
}
