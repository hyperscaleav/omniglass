// Package redact keeps credentials out of the errors and logs the binary
// writes about the URLs it is configured with.
package redact

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseURL is url.Parse whose error never carries the input. A server URL or
// a database DSN can hold a user:password, and url.Parse's error quotes the
// whole input while its detail quotes pieces of it (the port it read, a bad
// escape): a password with a reserved character in it does not parse, so the
// parse error alone printed it (#979). what names the value for the operator
// ("the server URL", "the database DSN"), who can read it from where they set
// it; the parser's detail is withheld entirely, since no piece of it is safe.
func ParseURL(raw, what string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not a valid URL (the parser's detail is withheld, since it can quote a password); "+
			"percent-encode any reserved character in its user:password", what)
	}
	return u, nil
}

// ParseBaseURL is ParseURL for a server's base URL, the one a request path is
// appended to. It also refuses what a misread password leaves behind: a
// password whose first reserved character (/ ? #) comes first, or after digits
// alone, parses without an error but wrongly, the parser taking `user:` for
// the host and an empty or numeric port and the rest of the password for the
// path, the query or the fragment. No masking catches that (net/http masks
// only a parsed user:password), so every error naming the URL would print it,
// and the request would go to a host named after the username. Each misread
// leaves an @ past the host, or a query or fragment a base URL never carries,
// so both are refused, again without quoting the input.
func ParseBaseURL(raw, what string) (*url.URL, error) {
	u, err := ParseURL(raw, what)
	if err != nil {
		return nil, err
	}
	if strings.ContainsAny(raw, "?#") || strings.Contains(u.EscapedPath(), "@") {
		return nil, fmt.Errorf("%s does not read as a server's base URL: it carries a query, a fragment, or an @ past the host, "+
			"which is how a user:password with a reserved character in it is misread; percent-encode that character", what)
	}
	return u, nil
}
