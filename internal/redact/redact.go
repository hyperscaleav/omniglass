// Package redact masks credentials in text a component sent back: a reply
// to a command, an error, a reason. It is the one masking primitive the node
// (before a reply leaves it) and the server (before a reply is stored and
// served) both run, so the forms it knows are known everywhere at once.
//
// Two passes. Secrets masks the values the caller knows are credentials (the
// secret fields an endpoint authenticates with) in every form a component
// could echo them back in. Keyed masks what nobody told it about but the text
// itself labels as a credential: a key-value pair whose key reads as one
// (`"password":"..."`, `token=...`, an authorization header). Text runs both.
//
// Pure, no I/O: a leaf every other package can import.
package redact

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
)

// Mask is what a masked credential reads as.
const Mask = "[redacted]"

// Secrets masks each secret in the forms a component commonly echoes it in:
// raw, url-escaped (query and path, either hex case), JSON-escaped with or
// without an escaped slash, with html characters escaped or not, and by an
// ASCII-only encoder (\uXXXX), html-escaped (each spelling of a quote), and
// its own base64 (standard and url, padded or not). Matching ignores case, so
// a component that upper-cases what it echoes is caught too. It runs on the
// whole text before anything collapses or cuts it, so no transform can hide a
// form from it. An empty secret masks nothing. What it cannot catch is a form
// it does not know (a hash, a partial echo); Keyed is the second line.
func Secrets(text string, secrets ...string) string {
	seen := map[string]bool{}
	var forms []string
	add := func(f string) {
		if f != "" && !seen[strings.ToLower(f)] {
			seen[strings.ToLower(f)] = true
			forms = append(forms, f)
		}
	}
	for _, v := range secrets {
		if v == "" {
			continue
		}
		add(v)
		add(url.QueryEscape(v))
		add(url.PathEscape(v))
		h := html.EscapeString(v)
		add(h)
		add(strings.NewReplacer("&#39;", "&apos;", "&#34;", "&quot;").Replace(h))
		add(strings.NewReplacer("&#39;", "&#x27;", "&#34;", "&#x22;").Replace(h))
		// JSON-escaped every way an encoder writes it: with html characters
		// escaped (Go's default) and without (most others'), and ASCII-only.
		for _, j := range []string{jsonEscaped(v, true), jsonEscaped(v, false), asciiJSON(v)} {
			add(j)
			add(strings.ReplaceAll(j, "/", `\/`))
		}
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			add(enc.EncodeToString([]byte(v)))
		}
	}
	if len(forms) == 0 {
		return text
	}
	// The longest form first, so a shorter one never leaves a longer one's
	// remainder behind (leftmost-first alternation tries them in order).
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	quoted := make([]string, len(forms))
	for i, f := range forms {
		quoted[i] = regexp.QuoteMeta(f)
	}
	return regexp.MustCompile(`(?i)(?:`+strings.Join(quoted, "|")+`)`).ReplaceAllLiteralString(text, Mask)
}

// jsonEscaped is v as the inside of a JSON string, by Go's encoder.
func jsonEscaped(v string, escapeHTML bool) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(escapeHTML)
	if err := enc.Encode(v); err != nil {
		return ""
	}
	j := strings.TrimSuffix(b.String(), "\n")
	return j[1 : len(j)-1]
}

// asciiJSON is v as an ASCII-only encoder writes the inside of a JSON
// string: every rune past ASCII as \uXXXX, a surrogate pair past the BMP.
func asciiJSON(v string) string {
	j := jsonEscaped(v, false)
	var b strings.Builder
	for _, r := range j {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r > 0xFFFF:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, hi, lo)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String()
}

// BasicAuth is the value a basic login's authorization header carries for
// username and password, a secret in its own right; empty when both are.
func BasicAuth(username, password string) string {
	if username == "" && password == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

// credentialKey is a key that names a credential, with any prefix
// (client_secret, access_token, x-api-key) and an optional value suffix
// (secret_value), matched whole: tokens_used and secret_type are not
// credentials.
const credentialKey = `[\w-]*(?:password|passwd|passphrase|pwd|pass|secret|token|api[_-]?key|apikey|authorization|community|private[_-]?key|cookie|sid)(?:[_-]?value)?`

// headerLine is a header whose whole value is the credential: an
// authorization (a scheme and its token) or a cookie.
var headerLine = regexp.MustCompile(`(?im)^([ \t]*(?:proxy-authorization|authorization|set-cookie|cookie)[ \t]*:[ \t]*)([^\r\n]*[^\s\r\n])`)

// keyedPair is a credential-labeled pair: the key, its separator, and a
// value, quoted (to its closing quote, or the end of the text when it never
// closes), an object or a list (masked whole), or bare. A bare value never
// begins with a bracket or a brace, so a value already masked is left as it
// is and a nested one is never cut in half.
var keyedPair = regexp.MustCompile(`(?i)(^|[^\w-])(` + credentialKey + `)(["']?[ \t]*[:=][ \t]*)("(?:[^"\\]|\\.)*(?:"|$)|'(?:[^'\\]|\\.)*(?:'|$)|\{[^{}]*\}|\[[^\[\]]*\]|[^\s,;&}\]\[{"'][^\s,;&}\]"']*)`)

// keywordValue is a config line's credential: a keyword, a space, the value
// (`set password hunter2`, `snmp-server community s3cret RO`).
var keywordValue = regexp.MustCompile(`(?i)(^|[^\w-])(password|passwd|secret|community)([ \t]+)([^\s"'<>\[{][^\s"'<>]*)`)

// keyedElement is a markup element that names a credential.
var keyedElement = regexp.MustCompile(`(?i)(<` + credentialKey + `\b[^>]*>)([^<]+)(</)`)

// Keyed masks the value of every pair, element and config keyword the text
// labels as a credential, and the value of an authorization or cookie
// header. An empty quoted value is left as it is: there is nothing in it to
// mask. A bare value runs to the next space, so a multi-word one past its
// first word is not caught; Secrets is the line that catches a known one.
func Keyed(text string) string {
	text = headerLine.ReplaceAllString(text, "${1}"+Mask)
	text = keyedElement.ReplaceAllString(text, "${1}"+Mask+"${3}")
	text = keyedPair.ReplaceAllStringFunc(text, func(m string) string {
		sub := keyedPair.FindStringSubmatch(m)
		lead, key, sep, value := sub[1], sub[2], sub[3], sub[4]
		switch {
		case value == `""` || value == `''` || value == Mask:
			return m
		case strings.HasPrefix(value, `'`):
			value = `'` + Mask + `'`
		case strings.HasPrefix(value, `"`), strings.HasPrefix(value, "{"), strings.HasPrefix(value, "["):
			value = `"` + Mask + `"`
		default:
			value = Mask
		}
		return lead + key + sep + value
	})
	return keywordValue.ReplaceAllString(text, "${1}${2}${3}"+Mask)
}

// Text masks the known secrets, then the credential-labeled pairs.
func Text(text string, secrets ...string) string {
	return Keyed(Secrets(text, secrets...))
}
