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
