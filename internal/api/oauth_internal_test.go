package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/clientaddr"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func TestValidRedirectURIAllowsHTTPSAndLoopbackOnly(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"https://app.example/cb":           true,
		"http://127.0.0.1:53682/callback":  true,
		"http://[::1]:8080/cb":             true,
		"http://localhost:3000/cb":         true,
		"http://evil.example/cb":           false,
		"http://127.0.0.1.evil.example/cb": false,
		"https://app.example/cb#frag":      false,
		"https://user@app.example/cb":      false,
		"javascript:alert(1)":              false,
		"/relative":                        false,
		"":                                 false,
	}
	for in, want := range cases {
		if got := validRedirectURI(in); got != want {
			t.Errorf("validRedirectURI(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestClientAgentNameKeepsWhatAnAgentNameAllows(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Claude Desktop":        "Claude Desktop",
		"  <script>x</script> ": "script x script",
		"--":                    oauthDefaultClientName,
		"":                      oauthDefaultClientName,
		"v1.2_beta-x":           "v1.2_beta-x",
	}
	for in, want := range cases {
		if got := clientAgentName(in); got != want {
			t.Errorf("clientAgentName(%q) = %q, want %q", in, got, want)
		}
	}
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	if got := clientAgentName(string(long)); len(got) != 100 {
		t.Errorf("a 300-character name kept %d characters, want 100", len(got))
	}
}

func TestPKCEMatchesOnlyTheS256OfTheVerifier(t *testing.T) {
	t.Parallel()
	// RFC 7636 appendix B.
	verifier, challenge := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if !pkceMatches(verifier, challenge) {
		t.Error("the RFC 7636 example does not match")
	}
	if pkceMatches(verifier+"x", challenge) || pkceMatches("", challenge) || pkceMatches(verifier, "") {
		t.Error("a wrong or empty verifier matched")
	}
}

func TestRequestOriginBelievesAForwardedSchemeOnlyFromATrustedProxy(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "http://og.example/mcp", nil)
	r.RemoteAddr = "203.0.113.9:4000"
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := requestOrigin(r, clientaddr.Default); got != "http://og.example" {
		t.Errorf("an untrusted peer's forwarded scheme moved the origin to %s", got)
	}
	r.RemoteAddr = "127.0.0.1:4000"
	if got := requestOrigin(r, clientaddr.Default); got != "https://og.example" {
		t.Errorf("a trusted proxy's forwarded https = %s, want https://og.example", got)
	}
	r.Header.Del("X-Forwarded-Proto")
	r.TLS = &tls.ConnectionState{}
	if got := requestOrigin(r, clientaddr.Default); got != "https://og.example" {
		t.Errorf("a TLS request = %s, want https", got)
	}
}

func TestIsMetadataClientIDTakesOnlyAnHTTPSURLWithAPath(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"https://app.example/client.json":        true,
		"https://app.example:8443/oauth/client":  true,
		"https://app.example":                    false,
		"https://app.example/":                   false,
		"http://app.example/client.json":         false,
		"https://user@app.example/client.json":   false,
		"https://app.example/client.json#frag":   false,
		"https://app.example/a/../client.json":   false,
		"https://app.example/./client.json":      false,
		"9b2e8f8e-1c4d-4c55-9f3e-6a1f2b3c4d5e":   false,
		"":                                       false,
		"https://app.example/client.json?v=1":    true,
		"https://[::1]/client.json":              true,
		"https://app.example/%2e%2e/client.json": false,
	}
	for in, want := range cases {
		if got := isMetadataClientID(in); got != want {
			t.Errorf("isMetadataClientID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseClientMetadataHoldsADocumentToItsURL(t *testing.T) {
	t.Parallel()
	const id = "https://app.example/client.json"
	good := `{"client_id":"` + id + `","client_name":"App","redirect_uris":["http://127.0.0.1:5000/cb"],` +
		`"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
	m, err := parseClientMetadata(id, []byte(good))
	if err != nil || m.ClientName != "App" || len(m.RedirectURIs) != 1 {
		t.Fatalf("a good document = %+v, %v", m, err)
	}
	bad := map[string]string{
		"another client id":     `{"client_id":"https://elsewhere.example/client.json","redirect_uris":["https://app.example/cb"]}`,
		"no client id":          `{"redirect_uris":["https://app.example/cb"]}`,
		"no redirect":           `{"client_id":"` + id + `"}`,
		"a bad redirect":        `{"client_id":"` + id + `","redirect_uris":["http://evil.example/cb"]}`,
		"a client secret":       `{"client_id":"` + id + `","redirect_uris":["https://app.example/cb"],"token_endpoint_auth_method":"client_secret_basic"}`,
		"an implicit grant":     `{"client_id":"` + id + `","redirect_uris":["https://app.example/cb"],"grant_types":["implicit"]}`,
		"a token response type": `{"client_id":"` + id + `","redirect_uris":["https://app.example/cb"],"response_types":["token"]}`,
		"not json":              `<html>`,
	}
	for name, body := range bad {
		if _, err := parseClientMetadata(id, []byte(body)); err == nil {
			t.Errorf("%s: parsed, want refused", name)
		}
	}
}

func TestTokenClientIDReadsTheFormOrTheBasicUsername(t *testing.T) {
	t.Parallel()
	const doc = "https://client.example/mcp.json"
	req := func(form string, basic *[2]string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if basic != nil {
			r.SetBasicAuth(basic[0], basic[1])
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		return r
	}
	escaped := url.QueryEscape(doc)
	for _, tc := range []struct {
		name  string
		r     *http.Request
		want  string
		agree bool
	}{
		{"form only", req("client_id="+escaped, nil), doc, true},
		{"basic only, form-encoded username", req("", &[2]string{escaped, ""}), doc, true},
		{"both, the same", req("client_id="+escaped, &[2]string{escaped, "ignored"}), doc, true},
		{"both, differing", req("client_id=other", &[2]string{escaped, ""}), "", false},
		{"neither", req("", nil), "", true},
	} {
		got, ok := tokenClientID(tc.r)
		if got != tc.want || ok != tc.agree {
			t.Errorf("%s: tokenClientID = %q %v, want %q %v", tc.name, got, ok, tc.want, tc.agree)
		}
	}
}

// A loopback redirect matches on any port, since a native client listens on
// whichever one is free (OAuth 2.1, RFC 8252 section 7.3); everything else,
// and every non-loopback URI, matches exactly.
func TestRegisteredMatchesALoopbackRedirectOnAnyPort(t *testing.T) {
	t.Parallel()
	c := storage.OAuthClient{RedirectURIs: []string{
		"http://127.0.0.1/callback", "http://localhost:8080/cb", "https://app.example/cb",
	}}
	for redirect, want := range map[string]bool{
		"http://127.0.0.1/callback":         true,
		"http://127.0.0.1:53682/callback":   true,
		"http://localhost:1234/cb":          true,
		"https://app.example/cb":            true,
		"http://127.0.0.1:53682/other":      false,
		"http://127.0.0.1:53682/callback?x": false,
		"http://localhost:53682/callback":   false,
		"http://[::1]:53682/callback":       false,
		"https://127.0.0.1:53682/callback":  false,
		"https://app.example:8443/cb":       false,
		"http://evil.example/callback":      false,
	} {
		if got := registered(c, redirect); got != want {
			t.Errorf("registered(%q) = %v, want %v", redirect, got, want)
		}
	}
}
