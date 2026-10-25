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
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Mask is what a masked credential reads as.
const Mask = "[redacted]"

// Secrets masks every form of each secret a component could echo back in its
// text: raw, url-escaped (query and path), JSON-escaped with or without an
// escaped slash, and html-escaped. It runs on the whole text before anything
// collapses or cuts it, so no transform can hide a form from it. An empty
// secret masks nothing.
func Secrets(text string, secrets ...string) string {
	var forms []string
	for _, v := range secrets {
		if v == "" {
			continue
		}
		forms = append(forms, v, url.QueryEscape(v), url.PathEscape(v), html.EscapeString(v))
		// JSON-escaped both ways an encoder writes it: with html characters
		// escaped (Go's default) and without (most others').
		for _, escapeHTML := range []bool{true, false} {
			var b strings.Builder
			enc := json.NewEncoder(&b)
			enc.SetEscapeHTML(escapeHTML)
			if err := enc.Encode(v); err == nil {
				j := strings.TrimSuffix(b.String(), "\n")
				j = j[1 : len(j)-1]
				forms = append(forms, j, strings.ReplaceAll(j, "/", `\/`))
			}
		}
	}
	if len(forms) == 0 {
		return text
	}
	// The longest form first, so a shorter one never leaves a longer one's
	// remainder behind.
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	for _, f := range forms {
		if f != "" {
			text = strings.ReplaceAll(text, f, Mask)
		}
	}
	return text
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
// (client_secret, access_token, x-api-key), matched whole: tokens_used and
// secret_type are not credentials.
const credentialKey = `[\w-]*(?:password|passwd|pwd|passphrase|secret|token|api[_-]?key|apikey|authorization|community|private[_-]?key)`

// headerLine is an authorization header, whose whole value is the
// credential (a scheme and its token).
var headerLine = regexp.MustCompile(`(?im)^([ \t]*(?:proxy-)?authorization[ \t]*:[ \t]*)([^\r\n]*[^\s\r\n])`)

// keyedPair is a credential-labeled pair: the key, its separator, and a
// quoted or bare value. A bare value never begins with the mask's bracket,
// so a value already masked is left as it is.
var keyedPair = regexp.MustCompile(`(?i)(^|[^\w-])(` + credentialKey + `)(["']?[ \t]*[:=][ \t]*)("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|[^\s,;&}\]\["'][^\s,;&}\]"']*)`)

// Keyed masks the value of every pair the text labels as a credential, and
// the value of an authorization header. An empty quoted value is left as it
// is: there is nothing in it to mask.
func Keyed(text string) string {
	text = headerLine.ReplaceAllString(text, "${1}"+Mask)
	return keyedPair.ReplaceAllStringFunc(text, func(m string) string {
		sub := keyedPair.FindStringSubmatch(m)
		lead, key, sep, value := sub[1], sub[2], sub[3], sub[4]
		switch {
		case value == `""` || value == `''`:
			return m
		case strings.HasPrefix(value, `"`):
			value = `"` + Mask + `"`
		case strings.HasPrefix(value, `'`):
			value = `'` + Mask + `'`
		default:
			value = Mask
		}
		return lead + key + sep + value
	})
}

// Text masks the known secrets, then the credential-labeled pairs.
func Text(text string, secrets ...string) string {
	return Keyed(Secrets(text, secrets...))
}
