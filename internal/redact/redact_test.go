package redact_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/redact"
)

// Every form a component could echo a secret back in is masked: raw,
// url-escaped, JSON-escaped (with and without an escaped slash) and
// html-escaped, the longest first so no remainder survives.
func TestSecretsMasksEveryForm(t *testing.T) {
	secret := `p@ss/w"rd<1>`
	for _, form := range []string{
		secret,
		"p%40ss%2Fw%22rd%3C1%3E",
		"p@ss%2Fw%22rd%3C1%3E",
		`p@ss/w\"rd<1>`,
		`p@ss\/w\"rd<1>`,
		"p@ss/w&#34;rd&lt;1&gt;",
		// Go's encoder escapes the html characters as unicode escapes.
		strings.NewReplacer("<", `\`+"u003c", ">", `\`+"u003e").Replace(`p@ss/w\"rd<1>`),
	} {
		got := redact.Secrets("before "+form+" after", secret)
		if strings.Contains(got, "rd") || !strings.Contains(got, "[redacted]") {
			t.Errorf("form %q: got %q, want the secret masked", form, got)
		}
	}
}

func TestSecretsSkipsEmptyAndLeavesOtherText(t *testing.T) {
	if got := redact.Secrets("volume 42", "", "kestrel"); got != "volume 42" {
		t.Fatalf("got %q, want the text untouched", got)
	}
	if got := redact.Secrets("volume 42"); got != "volume 42" {
		t.Fatalf("got %q with no secrets, want the text untouched", got)
	}
}

// A basic login's authorization header value is masked as well as its
// password.
func TestBasicAuthFormIsAMaskableSecret(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("admin:kestrel"))
	if got := redact.BasicAuth("admin", "kestrel"); got != b64 {
		t.Fatalf("BasicAuth = %q, want %q", got, b64)
	}
	if got := redact.BasicAuth("", ""); got != "" {
		t.Fatalf("BasicAuth with nothing to send = %q, want empty", got)
	}
	got := redact.Secrets("Authorization: Basic "+b64, redact.BasicAuth("admin", "kestrel"), "kestrel")
	if strings.Contains(got, b64) {
		t.Fatalf("got %q, want the header value masked", got)
	}
}

// A credential the caller did not know of is still masked where the text
// names it as one: a key-value pair whose key reads as a credential.
func TestKeyedMasksCredentialShapedPairs(t *testing.T) {
	for in, want := range map[string]string{
		`{"password":"hunter2","volume":42}`:    `{"password":"[redacted]","volume":42}`,
		`{"Token": "abc.def", "ok": true}`:      `{"Token": "[redacted]", "ok": true}`,
		`login user=admin passwd=hunter2 ok`:    `login user=admin passwd=[redacted] ok`,
		`Authorization: Bearer abc.def`:         `Authorization: [redacted]`,
		`api_key=k1&x=1`:                        `api_key=[redacted]&x=1`,
		`{"api-key":"k1"}`:                      `{"api-key":"[redacted]"}`,
		`secret: s3`:                            `secret: [redacted]`,
		`{"type":"set","value":42,"id":"a1b2"}`: `{"type":"set","value":42,"id":"a1b2"}`,
		`INPUT hdmi3`:                           `INPUT hdmi3`,
		`{"ok":true,"tokens_used":3}`:           `{"ok":true,"tokens_used":3}`,
		`{"password":""}`:                       `{"password":""}`,
	} {
		if got := redact.Keyed(in); got != want {
			t.Errorf("Keyed(%q) = %q, want %q", in, got, want)
		}
	}
}

// Text runs both: the known secrets first, then the credential-shaped
// pairs.
func TestTextRunsBoth(t *testing.T) {
	got := redact.Text(`{"echo":"kestrel","password":"other"}`, "kestrel")
	if want := `{"echo":"[redacted]","password":"[redacted]"}`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The forms a review found a component could still echo (#1155): a case
// change, the secret's own base64, lowercase percent-hex, an ASCII-only JSON
// encoder's unicode escapes, and the other html spellings of a quote.
func TestSecretsMasksTheLessCommonForms(t *testing.T) {
	for _, tc := range []struct{ secret, text string }{
		{"hunter2", "PASSWORD IS HUNTER2"},
		{"hunter2", "echo aHVudGVyMg== back"},
		{"hunter2", "echo aHVudGVyMg back"},
		{"a/b&c", "q=a%2fb%26c"},
		{"Pässw0rd", `{"p":"P` + `\` + `u00e4ssw0rd"}`},
		{"it's", "it&apos;s"},
		{"it's", "it&#x27;s"},
		{`say "hi"`, "say &quot;hi&quot;"},
	} {
		got := redact.Secrets(tc.text, tc.secret)
		if !strings.Contains(got, "[redacted]") {
			t.Errorf("secret %q in %q: got %q, want it masked", tc.secret, tc.text, got)
		}
	}
}

// A credential a config line or a markup document labels: a keyword and a
// space, an element, a cookie, and a value whose quote never closes. A value
// that is an object or a list is masked whole, never mangled.
func TestKeyedMasksTheLessCommonLabels(t *testing.T) {
	for in, want := range map[string]string{
		`set password hunter2`:                     `set password [redacted]`,
		`snmp-server community s3cret RO`:          `snmp-server community [redacted] RO`,
		`<password>hunter2</password>`:             `<password>[redacted]</password>`,
		`<Token type="x">abc</Token>`:              `<Token type="x">[redacted]</Token>`,
		`{"pass":"hunter2"}`:                       `{"pass":"[redacted]"}`,
		`{"secret_value":"s3"}`:                    `{"secret_value":"[redacted]"}`,
		"Cookie: sid=abc; theme=dark":              "Cookie: [redacted]",
		"Set-Cookie: sid=abc; HttpOnly":            "Set-Cookie: [redacted]",
		`{"password":"hunter2`:                     `{"password":"[redacted]"`,
		`{"token": {"value":"abc123"}, "ok":true}`: `{"token": "[redacted]", "ok":true}`,
		`{"keys": ["a1", "b2"]}`:                   `{"keys": ["a1", "b2"]}`,
		`{"api_key": ["a1", "b2"]}`:                `{"api_key": "[redacted]"}`,
		`INPUT hdmi3`:                              `INPUT hdmi3`,
	} {
		if got := redact.Keyed(in); got != want {
			t.Errorf("Keyed(%q) = %q, want %q", in, got, want)
		}
	}
}
