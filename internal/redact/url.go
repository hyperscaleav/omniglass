// Package redact keeps credentials out of the errors and logs the binary
// writes about the URLs it is configured with.
package redact

import (
	"fmt"
	"net/url"
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
