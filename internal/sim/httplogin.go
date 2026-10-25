package sim

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The http login faces (#1114): the shapes gear's web APIs log in with,
// for the collector's http logins and digest to meet on a real wire. A form
// login answers a session cookie, a JSON login a token and its lifetime,
// and a digest face a nonce challenge. Sessions live in the device's memory
// and the console can expire them all (and rotate the digest nonce), as a
// component's reboot or idle timeout would, so the collector's re-login is
// something an operator can watch. A teaching example, like the rest of the
// gate: constant-time comparisons, no lockout, no rotation policy.

// loginSessions is a device's open sessions and its digest nonce.
type loginSessions struct {
	mu       sync.Mutex
	open     map[string]time.Time // id to expiry, zero for none
	nonce    string
	answered map[string]bool // the nonce counts answered under nonce
}

// maxDigestCounts bounds the counts a nonce remembers: past it the nonce
// rotates, and the client's next answer reads stale.
const maxDigestCounts = 4096

// sessionCookie is the form login's cookie name.
const sessionCookie = "sid"

// consoleOperation marks a request the console fires in process (Operate),
// which no network client can set: the console is trusted past a login
// face, as an operator at the component's own screen would be.
type consoleOperation struct{}

func isConsole(r *http.Request) bool {
	v, _ := r.Context().Value(consoleOperation{}).(bool)
	return v
}

func asConsole(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), consoleOperation{}, true))
}

func randomID() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// HTTPLogins counts the logins the device's form or token face was sent.
func (d *Device) HTTPLogins() int64 { return d.httpLogins.Load() }

// HTTPDigestAnswers counts the requests that answered the device's digest
// challenge, right or wrong: a collector's attempts, beside the
// unanswered requests a challenge meets first.
func (d *Device) HTTPDigestAnswers() int64 { return d.httpDigestAnswers.Load() }

// openSession opens one session, living ttl (zero: until expired).
func (d *Device) openSession() string {
	id := randomID()
	var exp time.Time
	if d.HTTPAuth.SessionTTL > 0 {
		exp = d.clock().Add(time.Duration(d.HTTPAuth.SessionTTL) * time.Second)
	}
	d.logins.mu.Lock()
	defer d.logins.mu.Unlock()
	if d.logins.open == nil {
		d.logins.open = map[string]time.Time{}
	}
	d.logins.open[id] = exp
	return id
}

// liveSession reports whether id names a session that has not lapsed.
func (d *Device) liveSession(id string) bool {
	if id == "" {
		return false
	}
	d.logins.mu.Lock()
	defer d.logins.mu.Unlock()
	exp, ok := d.logins.open[id]
	if !ok {
		return false
	}
	if !exp.IsZero() && !d.clock().Before(exp) {
		delete(d.logins.open, id)
		return false
	}
	return true
}

// ExpireHTTPSessions forgets every session the device opened and rotates
// its digest nonce, as a reboot would, and returns how many it forgot.
func (d *Device) ExpireHTTPSessions() int {
	d.logins.mu.Lock()
	defer d.logins.mu.Unlock()
	n := len(d.logins.open)
	d.logins.open = map[string]time.Time{}
	d.logins.nonce, d.logins.answered = "", nil
	return n
}

// checkLoginFace runs the form, token and digest gates on a request that is
// not a login itself; handled is false for any other mode.
func (d *Device) checkLoginFace(w http.ResponseWriter, r *http.Request) (allowed, handled bool) {
	switch d.HTTPAuth.Mode {
	case HTTPAuthForm:
		if c, err := r.Cookie(sessionCookie); err == nil && d.liveSession(c.Value) {
			return true, true
		}
		http.Redirect(w, r, d.HTTPAuth.LoginPath, http.StatusFound)
		return false, true
	case HTTPAuthToken:
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if ok && d.liveSession(tok) {
			return true, true
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+strings.ReplaceAll(d.Hostname, `"`, "")+`"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if ok && tok != "" {
			_, _ = w.Write([]byte(`{"error":"token expired"}` + "\n"))
		} else {
			_, _ = w.Write([]byte(`{"error":"unauthorized","auth":"token"}` + "\n"))
		}
		return false, true
	case HTTPAuthDigest:
		return d.checkDigest(w, r), true
	}
	return false, false
}

func (d *Device) checkLoginCredentials(user, pass string) bool {
	d.httpLogins.Add(1)
	return constantTimeEqual(user, d.HTTPAuth.User) && constantTimeEqual(pass, d.HTTPAuth.Password)
}

const loginPage = `<!doctype html>
<html><head><title>%s sign in</title></head>
<body><h1>%s</h1>%s
<form method="post" action="%s">
<label>User <input name="username" autocomplete="username"></label>
<label>Password <input name="password" type="password" autocomplete="current-password"></label>
<button type="submit">Sign in</button>
</form></body></html>
`

// answerLogin answers a form or token face's login path itself, ahead of
// the gate and the routes: the page, and the login it posts. answered is
// false for any other request.
func (d *Device) answerLogin(w http.ResponseWriter, r *http.Request) (answered bool) {
	mode := d.HTTPAuth.Mode
	if (mode != HTTPAuthForm && mode != HTTPAuthToken) || r.URL.Path != d.HTTPAuth.LoginPath {
		return false
	}
	switch {
	case mode == HTTPAuthForm && r.Method == http.MethodGet:
		note := ""
		if r.URL.Query().Get("error") != "" {
			note = "<p>The user or password is wrong.</p>"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		host := htmlEscape(d.Hostname)
		_, _ = fmt.Fprintf(w, loginPage, host, host, note, htmlEscape(d.HTTPAuth.LoginPath))
	case mode == HTTPAuthForm && r.Method == http.MethodPost:
		_ = r.ParseForm()
		if !d.checkLoginCredentials(r.PostForm.Get("username"), r.PostForm.Get("password")) {
			http.Redirect(w, r, d.HTTPAuth.LoginPath+"?error=1", http.StatusFound)
			return true
		}
		cookie := &http.Cookie{Name: sessionCookie, Value: d.openSession(), Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode}
		if d.HTTPAuth.SessionTTL > 0 {
			cookie.MaxAge = d.HTTPAuth.SessionTTL
		}
		http.SetCookie(w, cookie)
		http.Redirect(w, r, "/", http.StatusFound)
	case mode == HTTPAuthToken && r.Method == http.MethodPost:
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in)
		w.Header().Set("Content-Type", "application/json")
		if !d.checkLoginCredentials(in.Username, in.Password) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid credentials"}` + "\n"))
			return true
		}
		ttl := d.HTTPAuth.SessionTTL
		if ttl <= 0 {
			ttl = defaultTokenTTL
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": d.openSession(), "token_type": "Bearer", "expires_in": ttl})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
	return true
}

// defaultTokenTTL is a token's lifetime when the profile declares none.
const defaultTokenTTL = 3600

// digestHasher is a digest algorithm's hash; nil for one the face does not
// answer.
func digestHasher(alg string) func() hash.Hash {
	switch strings.ToUpper(alg) {
	case "", "MD5":
		return md5.New
	case "SHA-256":
		return sha256.New
	}
	return nil
}

// digestNonce is the face's current nonce, minted when none is held.
func (d *Device) digestNonce() string {
	d.logins.mu.Lock()
	defer d.logins.mu.Unlock()
	if d.logins.nonce == "" {
		d.logins.nonce, d.logins.answered = randomID(), map[string]bool{}
	}
	return d.logins.nonce
}

func (d *Device) digestChallenge(w http.ResponseWriter, stale bool) {
	realm := strings.ReplaceAll(d.HTTPAuth.Realm, `"`, "")
	if realm == "" {
		realm = d.Hostname
	}
	alg := d.HTTPAuth.Algorithm
	if alg == "" {
		alg = "MD5"
	}
	v := fmt.Sprintf(`Digest realm=%q, qop="auth", algorithm=%s, nonce=%q, opaque=%q`, realm, alg, d.digestNonce(), d.DeviceID)
	if stale {
		v += ", stale=true"
	}
	w.Header().Set("WWW-Authenticate", v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized","auth":"digest"}` + "\n"))
}

func (d *Device) checkDigest(w http.ResponseWriter, r *http.Request) bool {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Digest ")
	if !ok {
		d.digestChallenge(w, false)
		return false
	}
	d.httpDigestAnswers.Add(1)
	p := parseDigestParams(raw)
	h := digestHasher(d.HTTPAuth.Algorithm)
	if h == nil || !strings.EqualFold(p["algorithm"], nonEmptyAlg(d.HTTPAuth.Algorithm)) || p["qop"] != "auth" || p["uri"] != r.URL.RequestURI() {
		d.digestChallenge(w, false)
		return false
	}
	hx := func(s string) string { x := h(); x.Write([]byte(s)); return hex.EncodeToString(x.Sum(nil)) }
	realm := strings.ReplaceAll(d.HTTPAuth.Realm, `"`, "")
	if realm == "" {
		realm = d.Hostname
	}
	ha1 := hx(d.HTTPAuth.User + ":" + realm + ":" + d.HTTPAuth.Password)
	want := hx(ha1 + ":" + p["nonce"] + ":" + p["nc"] + ":" + p["cnonce"] + ":auth:" + hx(r.Method+":"+p["uri"]))
	if !constantTimeEqual(p["username"], d.HTTPAuth.User) || !constantTimeEqual(p["response"], want) {
		d.digestChallenge(w, false)
		return false
	}
	// The answer is right: a nonce that is not the current one, or a count
	// already answered under it (a replay, or a second client sharing the
	// nonce), is stale, which a client answers afresh. A replay retires the
	// nonce, so the stale challenge carries a new one.
	current := d.digestNonce()
	d.logins.mu.Lock()
	fresh := p["nonce"] == current && !d.logins.answered[p["nc"]]
	switch {
	case fresh:
		d.logins.answered[p["nc"]] = true
		if len(d.logins.answered) > maxDigestCounts {
			d.logins.nonce, d.logins.answered = "", nil
		}
	case p["nonce"] == current:
		d.logins.nonce, d.logins.answered = "", nil
	}
	d.logins.mu.Unlock()
	if !fresh {
		d.digestChallenge(w, true)
		return false
	}
	return true
}

func nonEmptyAlg(a string) string {
	if a == "" {
		return "MD5"
	}
	return a
}

// parseDigestParams reads an Authorization: Digest header's parameters.
func parseDigestParams(raw string) map[string]string {
	out := map[string]string{}
	var cur strings.Builder
	inQuote := false
	flush := func() {
		k, v, ok := strings.Cut(strings.TrimSpace(cur.String()), "=")
		if ok {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
		}
		cur.Reset()
	}
	for _, c := range raw {
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == ',' && !inQuote:
			flush()
			continue
		}
		cur.WriteRune(c)
	}
	flush()
	return out
}

func htmlEscape(s string) string { return html.EscapeString(s) }

// operateSessionExpiry answers the console's expiry of every session.
func (d *Device) operateSessionExpiry() OperationResult {
	n := d.ExpireHTTPSessions()
	d.publishOperation("http", http.MethodPost, d.HTTPAuth.ExpirePath, http.StatusOK)
	return OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"expired":%d,"nonce_rotated":%t}`, n, d.HTTPAuth.Mode == HTTPAuthDigest) + "\n"}
}
