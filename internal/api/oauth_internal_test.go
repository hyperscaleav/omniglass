package api

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/clientaddr"
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
