package collection

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"strings"
	"sync"
)

// HTTP digest (RFC 7616, #1114): the component challenges an unauthorized
// request with a nonce, and the client answers with a hash of the
// credential, the nonce and the request, so the password itself never
// crosses the wire. MD5 and SHA-256 (and their -sess forms), qop=auth (or
// the legacy no-qop form); a nonce is reused with a rising count until the
// component calls it stale, which is a fresh challenge, never a refusal.

// DigestState is one endpoint's digest challenge, reused across requests
// with its nonce count, so a poll answers preemptively rather than drawing a
// 401 every time. Safe for concurrent use.
type DigestState struct {
	mu sync.Mutex
	ch *digestChallenge
	nc uint32
	// cnonce is the client nonce of the held challenge: a -sess algorithm's
	// session key is computed once per challenge (RFC 7616 3.4.2), so every
	// answer to it uses the same one.
	cnonce string
}

// digestChallenge is one WWW-Authenticate: Digest challenge.
type digestChallenge struct {
	realm, nonce, opaque, algorithm, qop string
	stale                                bool
}

// digestAlgorithms are the algorithms this client answers, strongest first.
var digestAlgorithms = []string{"SHA-256-SESS", "SHA-256", "MD5-SESS", "MD5"}

// parseDigestChallenges reads every Digest challenge in a response's
// WWW-Authenticate headers and picks the strongest algorithm this client
// answers; ok is false when none is a Digest challenge it can answer.
func parseDigestChallenges(h http.Header) (digestChallenge, bool) {
	var best digestChallenge
	rank := len(digestAlgorithms)
	for _, v := range h.Values("WWW-Authenticate") {
		for _, raw := range splitChallenges(v) {
			ch, ok := parseDigestChallenge(raw)
			if !ok {
				continue
			}
			for i, a := range digestAlgorithms {
				if strings.EqualFold(ch.algorithm, a) && i < rank {
					best, rank = ch, i
				}
			}
		}
	}
	return best, rank < len(digestAlgorithms)
}

// splitChallenges splits a WWW-Authenticate value into its challenges (a
// server may offer several schemes in one header): a parameter list item that
// begins with a scheme name starts a new challenge.
func splitChallenges(v string) []string {
	var out []string
	for _, item := range splitParams(v) {
		t := strings.TrimSpace(item)
		if t == "" {
			continue
		}
		if len(out) == 0 || startsScheme(t) {
			out = append(out, t)
			continue
		}
		out[len(out)-1] += ", " + t
	}
	return out
}

// startsScheme reports a challenge's start: a scheme token, then
// whitespace, then not an "=" (a parameter may have space around its "=",
// RFC 7235's BWS).
func startsScheme(s string) bool {
	sp := strings.IndexAny(s, " \t")
	if sp <= 0 || strings.Contains(s[:sp], "=") {
		return false
	}
	return !strings.HasPrefix(strings.TrimLeft(s[sp:], " \t"), "=")
}

// maxChallengeParam bounds a challenge parameter: a realm, a nonce or an
// opaque past it is not a challenge this node answers.
const maxChallengeParam = 1024

// parseDigestChallenge reads one challenge; ok is false for another scheme,
// or for a challenge with no nonce or a parameter past maxChallengeParam.
func parseDigestChallenge(raw string) (digestChallenge, bool) {
	raw = strings.TrimSpace(raw)
	sp := strings.IndexAny(raw, " \t")
	if sp < 0 || !strings.EqualFold(raw[:sp], "Digest") {
		return digestChallenge{}, false
	}
	params := raw[sp+1:]
	ch := digestChallenge{algorithm: "MD5"}
	for _, kv := range splitParams(params) {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), unquote(strings.TrimSpace(v))
		if len(v) > maxChallengeParam {
			return digestChallenge{}, false
		}
		switch k {
		case "realm":
			ch.realm = v
		case "nonce":
			ch.nonce = v
		case "opaque":
			ch.opaque = v
		case "algorithm":
			ch.algorithm = v
		case "qop":
			ch.qop = v
		case "stale":
			ch.stale = strings.EqualFold(v, "true")
		}
	}
	return ch, ch.nonce != ""
}

// splitParams splits auth-params on commas outside quotes.
func splitParams(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, escaped := false, false
	for _, r := range s {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inQuote:
			escaped = true
		case r == '"':
			inQuote = !inQuote
		case r == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// unquote removes a quoted-string's quotes and escapes.
func unquote(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(v)
	}
	return v
}

// quote renders a quoted-string.
func quote(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// hasher is the challenge's hash function; nil for one this client does
// not answer.
func (c digestChallenge) hasher() func() hash.Hash {
	switch strings.ToUpper(strings.TrimSuffix(strings.ToUpper(c.algorithm), "-SESS")) {
	case "MD5":
		return md5.New
	case "SHA-256":
		return sha256.New
	}
	return nil
}

// qopAuth reports whether the challenge offers qop=auth; legacy is a
// challenge with no qop at all (RFC 2069).
func (c digestChallenge) qopAuth() (auth, legacy bool) {
	if c.qop == "" {
		return false, true
	}
	for _, q := range strings.Split(c.qop, ",") {
		if strings.TrimSpace(q) == "auth" {
			return true, false
		}
	}
	return false, false
}

// digestAuthorization computes the Authorization header for one request.
func digestAuthorization(c digestChallenge, method, uri, user, pass string, nc uint32, cnonce string) (string, error) {
	newHash := c.hasher()
	if newHash == nil {
		return "", &RequestFault{Status: 401, Reason: fmt.Sprintf("the component's digest algorithm %s is not one this node answers (MD5, SHA-256)", snippet([]byte(c.algorithm), nil))}
	}
	h := func(s string) string {
		hh := newHash()
		hh.Write([]byte(s))
		return hex.EncodeToString(hh.Sum(nil))
	}
	auth, legacy := c.qopAuth()
	if !auth && !legacy {
		// Nothing is sent: an unanswerable challenge is no verdict on the
		// credential, and never gates.
		return "", &RequestFault{Status: 401, Reason: fmt.Sprintf("the component's digest qop %s offers no auth, which is the one this node answers", snippet([]byte(c.qop), nil))}
	}
	ha1 := h(user + ":" + c.realm + ":" + pass)
	if strings.HasSuffix(strings.ToUpper(c.algorithm), "-SESS") {
		ha1 = h(ha1 + ":" + c.nonce + ":" + cnonce)
	}
	ha2 := h(method + ":" + uri)
	ncs := fmt.Sprintf("%08x", nc)
	var resp string
	if auth {
		resp = h(ha1 + ":" + c.nonce + ":" + ncs + ":" + cnonce + ":auth:" + ha2)
	} else {
		resp = h(ha1 + ":" + c.nonce + ":" + ha2)
	}
	parts := []string{
		"username=" + quote(user), "realm=" + quote(c.realm), "nonce=" + quote(c.nonce), "uri=" + quote(uri),
		"algorithm=" + c.algorithm, "response=" + quote(resp),
	}
	if auth {
		parts = append(parts, "qop=auth", "nc="+ncs, "cnonce="+quote(cnonce))
	}
	if c.opaque != "" {
		parts = append(parts, "opaque="+quote(c.opaque))
	}
	return "Digest " + strings.Join(parts, ", "), nil
}

// newCnonce is a fresh client nonce.
func newCnonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// authorize answers the held challenge for a request, its count risen, and
// names the nonce it answered; false when no challenge is held yet.
func (d *DigestState) authorize(r *http.Request, user, pass string) (bool, string, error) {
	if d == nil {
		return false, "", nil
	}
	d.mu.Lock()
	ch := d.ch
	if ch == nil {
		d.mu.Unlock()
		return false, "", nil
	}
	d.nc++
	nc, cnonce := d.nc, d.cnonce
	d.mu.Unlock()
	if !strings.HasSuffix(strings.ToUpper(ch.algorithm), "-SESS") {
		cnonce = newCnonce()
	}
	v, err := digestAuthorization(*ch, r.Method, r.URL.RequestURI(), user, pass, nc, cnonce)
	if err != nil {
		return false, "", err
	}
	r.Header.Set("Authorization", v)
	return true, ch.nonce, nil
}

// learn holds a new challenge, its count from zero and a client nonce of
// its own; a challenge naming the nonce already held keeps its count rising
// and its client nonce, since a count once sent under a nonce is never sent
// again.
func (d *DigestState) learn(ch digestChallenge) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ch == nil || d.ch.nonce != ch.nonce {
		d.nc, d.cnonce = 0, newCnonce()
	}
	d.ch = &ch
}
