package redact_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/redact"
)

// TestAnUnparseableURLNeverEchoesItsPassword: url.Parse's error quotes the
// whole input, and its detail quotes pieces of it (the port it read, a bad
// escape), so a password with a reserved character in it reaches the screen
// and the logs through the parse error alone (#979's review). Each case is a
// malformed URL carrying a password; the error must hold no piece of it.
func TestAnUnparseableURLNeverEchoesItsPassword(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		pieces []string
	}{
		// A bad port, the password beside it.
		{"https://operator:hunter2@omniglass.example:80a", []string{"hunter2", "operator:"}},
		// A slash in the password ends the authority early, so the parser reads
		// the password's first half as the port.
		{"postgres://og:s3c/ret@127.0.0.1:1/og", []string{"s3c", "ret@"}},
		// A bad escape inside the password.
		{"postgres://og:pa%zzss@db.example/og", []string{"%zz", "pa%", "zzss"}},
		// A control character, the parser quoting the input whole.
		{"https://operator:hunter2@omniglass.example/\x7f", []string{"hunter2"}},
	} {
		_, err := redact.ParseURL(tc.raw, "the server URL")
		if err == nil {
			t.Fatalf("%q parsed; the case needs a URL the parser refuses", tc.raw)
		}
		for _, p := range tc.pieces {
			if strings.Contains(err.Error(), p) {
				t.Errorf("parsing %q: the error %q carries %q", tc.raw, err, p)
			}
		}
		if !strings.Contains(err.Error(), "the server URL") {
			t.Errorf("the error %q does not name what failed to parse", err)
		}
	}
}

// TestAValidURLParsesUnchanged: the wrapper changes the error, not the parse.
func TestAValidURLParsesUnchanged(t *testing.T) {
	u, err := redact.ParseURL("postgres://og:s3c%2Fret@127.0.0.1:5432/og?sslmode=disable", "the DSN")
	if err != nil {
		t.Fatalf("a valid URL refused: %v", err)
	}
	if pw, _ := u.User.Password(); pw != "s3c/ret" || u.Host != "127.0.0.1:5432" || u.Path != "/og" {
		t.Errorf("parsed as %+v (password %q)", u, pw)
	}
}

// TestABaseURLRefusesAMisreadPassword: a password whose first reserved
// character comes first, or after digits alone, parses without an error but
// wrongly: the parser reads `user:` as the host and an empty or numeric port,
// and the rest of the password lands in the path, the query or the fragment.
// Nothing then masks it (net/http masks only a parsed user:password), so every
// error naming the URL printed it, and the request went to a host named after
// the username (#979's review). Each misread leaves an @ past the host, or a
// query or fragment a base URL never carries, so a base URL refuses both.
func TestABaseURLRefusesAMisreadPassword(t *testing.T) {
	for _, raw := range []string{
		"https://operator:/abcSECRET@omniglass.example",
		"https://operator:2024/abcSECRET@omniglass.example",
		"https://operator:123#abcSECRET@omniglass.example",
		"https://operator:?abcSECRET@omniglass.example",
		"https://operator:#abcSECRET@omniglass.example",
		// No // before the host (curl's user:pass@host habit): the parser takes
		// the user for the scheme and keeps the rest as one opaque string.
		"operator:abcSECRET@omniglass.example",
		"https:operator:abcSECRET@omniglass.example",
		"operator:abcSECRET@127.0.0.1:8080",
	} {
		_, err := redact.ParseBaseURL(raw, "the server URL")
		if err == nil {
			t.Errorf("%q: accepted as a base URL; its password was misread into the path, query or fragment", raw)
			continue
		}
		if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "operator") {
			t.Errorf("%q: the error %q carries a piece of the user:password", raw, err)
		}
		if !strings.Contains(err.Error(), "the server URL") {
			t.Errorf("the error %q does not name what failed to parse", err)
		}
	}
	// It withholds a parse error's detail as ParseURL does.
	if _, err := redact.ParseBaseURL("https://operator:hunter2@omniglass.example:80a", "the server URL"); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("an unparseable base URL: err = %v", err)
	}
}

// TestABaseURLAcceptsAServer: the refusals touch no server an operator types.
func TestABaseURLAcceptsAServer(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:8080",
		"https://omniglass.example/under/a/prefix",
		"https://operator:s3c%2Fret@omniglass.example",
		"https://operator:hunter2@omniglass.example:8443",
		"HTTP://LOCALHOST:8080",
		"https://[::1]:8080",
	} {
		if _, err := redact.ParseBaseURL(raw, "the server URL"); err != nil {
			t.Errorf("%q refused: %v", raw, err)
		}
	}
}
