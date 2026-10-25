package collection

import (
	"net/http"
	"strings"
	"testing"
)

// RFC 7616 section 3.9.1's worked example, for both algorithms: the same
// credential, nonce, cnonce and request answer exactly the RFC's responses.
func TestDigestAuthorizationMatchesRFC7616(t *testing.T) {
	base := digestChallenge{realm: "http-auth@example.org", nonce: "7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v",
		opaque: "FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS", qop: "auth, auth-int"}
	for alg, want := range map[string]string{
		"MD5":     "8ca523f5e9506fed4657c9700eebdbec",
		"SHA-256": "753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1",
	} {
		ch := base
		ch.algorithm = alg
		got, err := digestAuthorization(ch, "GET", "/dir/index.html", "Mufasa", "Circle of Life", 1, "f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, `response="`+want+`"`) || !strings.Contains(got, "nc=00000001") || !strings.Contains(got, "qop=auth,") {
			t.Errorf("%s authorization = %s, want response %s", alg, got, want)
		}
	}
}

func TestDigestChallengesPickTheStrongest(t *testing.T) {
	h := http.Header{}
	h.Add("WWW-Authenticate", `Digest realm="r", qop="auth", algorithm=MD5, nonce="n1", opaque="o"`)
	h.Add("WWW-Authenticate", `Digest realm="r", qop="auth", algorithm=SHA-256, nonce="n2", opaque="o", stale=true`)
	ch, ok := parseDigestChallenges(h)
	if !ok || ch.algorithm != "SHA-256" || ch.nonce != "n2" || !ch.stale {
		t.Fatalf("challenge = %+v, %v", ch, ok)
	}
	one := http.Header{"Www-Authenticate": {`Basic realm="a, b", Digest realm="Boreal Edge Admin", nonce="x,y", qop="auth"`}}
	ch, ok = parseDigestChallenges(one)
	if !ok || ch.realm != "Boreal Edge Admin" || ch.nonce != "x,y" || ch.algorithm != "MD5" {
		t.Fatalf("one header, two schemes = %+v, %v", ch, ok)
	}
	if _, ok := parseDigestChallenges(http.Header{"Www-Authenticate": {`Basic realm="x"`}}); ok {
		t.Fatal("a basic challenge read as digest")
	}
	// An unanswerable challenge sends nothing: no verdict on the credential,
	// so the request's own fault, never a gate.
	_, err := digestAuthorization(digestChallenge{nonce: "n", algorithm: "SHA-512-256"}, "GET", "/", "u", "p", 1, "c")
	if _, ok := AsRequestFault(err); !ok {
		t.Fatalf("an algorithm this node does not answer = %v, want a request fault", err)
	}
	_, err = digestAuthorization(digestChallenge{nonce: "n", algorithm: "MD5", qop: "auth-int"}, "GET", "/", "u", "p", 1, "c")
	if _, ok := AsRequestFault(err); !ok {
		t.Fatalf("auth-int alone = %v, want a request fault", err)
	}
	// RFC 7235 lets a parameter's "=" carry spaces, and a scheme be followed
	// by a tab: neither starts a new challenge.
	spaced := http.Header{"Www-Authenticate": {"Digest\trealm = \"r\", nonce = \"n1\", qop = \"auth\""}}
	if ch, ok := parseDigestChallenges(spaced); !ok || ch.nonce != "n1" || ch.realm != "r" || ch.qop != "auth" {
		t.Fatalf("a spaced challenge = %+v, %v", ch, ok)
	}
	huge := http.Header{"Www-Authenticate": {`Digest realm="` + strings.Repeat("r", 2000) + `", nonce="n"`}}
	if _, ok := parseDigestChallenges(huge); ok {
		t.Fatal("a challenge with a 2000-byte realm was answered")
	}
	legacy, err := digestAuthorization(digestChallenge{nonce: "n", algorithm: "MD5", realm: "r"}, "GET", "/", "u", "p", 1, "c")
	if err != nil || strings.Contains(legacy, "qop=") {
		t.Fatalf("a legacy challenge = %s, %v, want no qop", legacy, err)
	}
	if q := quote(`a"b\c`); q != `"a\"b\\c"` {
		t.Fatalf("quote = %s", q)
	}
}

// A count once sent under a nonce is never sent again: a stale challenge
// naming the held nonce keeps the count rising, and a new nonce restarts it.
func TestDigestCountNeverRepeatsUnderANonce(t *testing.T) {
	var d DigestState
	nc := func() string {
		r, _ := http.NewRequest("GET", "http://x/s", nil)
		if ok, _, err := d.authorize(r, "u", "p"); !ok || err != nil {
			t.Fatalf("authorize = %v, %v", ok, err)
		}
		_, rest, _ := strings.Cut(r.Header.Get("Authorization"), "nc=")
		return rest[:8]
	}
	d.learn(digestChallenge{realm: "r", nonce: "n1", algorithm: "MD5", qop: "auth"})
	if got := nc(); got != "00000001" {
		t.Fatalf("first count = %s", got)
	}
	d.learn(digestChallenge{realm: "r", nonce: "n1", algorithm: "MD5", qop: "auth", stale: true})
	if got := nc(); got != "00000002" {
		t.Fatalf("a stale challenge under the held nonce restarted the count: %s", got)
	}
	d.learn(digestChallenge{realm: "r", nonce: "n2", algorithm: "MD5", qop: "auth", stale: true})
	if got := nc(); got != "00000001" {
		t.Fatalf("a new nonce = %s, want the count from one", got)
	}
}

// A -sess session key is computed once per challenge, so every answer to it
// carries the one client nonce; a plain algorithm's answers each carry their
// own.
func TestDigestSessKeepsOneClientNoncePerChallenge(t *testing.T) {
	cnonce := func(d *DigestState) string {
		r, _ := http.NewRequest("GET", "http://x/s", nil)
		if ok, _, err := d.authorize(r, "u", "p"); !ok || err != nil {
			t.Fatalf("authorize = %v, %v", ok, err)
		}
		_, rest, _ := strings.Cut(r.Header.Get("Authorization"), `cnonce="`)
		v, _, _ := strings.Cut(rest, `"`)
		return v
	}
	var sess DigestState
	sess.learn(digestChallenge{realm: "r", nonce: "n1", algorithm: "MD5-sess", qop: "auth"})
	if a, b := cnonce(&sess), cnonce(&sess); a == "" || a != b {
		t.Fatalf("-sess client nonces %q, %q, want one per challenge", a, b)
	}
	var plain DigestState
	plain.learn(digestChallenge{realm: "r", nonce: "n1", algorithm: "MD5", qop: "auth"})
	if a, b := cnonce(&plain), cnonce(&plain); a == b {
		t.Fatalf("plain client nonces repeat: %q", a)
	}
}
