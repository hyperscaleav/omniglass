// Package auth holds the credential primitives shared by the bootstrap path and
// the request authn middleware: minting a bearer token and hashing one for
// lookup. The cleartext token is shown once; only its sha256 is ever stored.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// TokenScheme is the non-secret prefix marking an Omniglass bearer token, so a
// leaked token is detectable by secret scanners.
const TokenScheme = "ogp"

// DefaultTokenLifetime and MaxTokenLifetime bound a CLI/API token's expiry (issue
// #172): no bearer is issued without an expiry, so a leaked token is not valid
// forever. A CLI-minted token (and the bootstrap token) defaults to 90 days; the
// operator may shorten or lengthen it with --ttl up to the hard 365-day cap, above
// which the CLI errors. A web-login session has its own, shorter fixed lifetime.
const (
	DefaultTokenLifetime = 90 * 24 * time.Hour
	MaxTokenLifetime     = 365 * 24 * time.Hour
)

// NewBearerToken mints a bearer token of the form ogp_<locator>_<secret>. It
// returns the cleartext (shown once), its sha256 hash (stored), and the
// non-secret locator (stored, for audit and scanners).
func NewBearerToken() (token string, hash []byte, prefix string, err error) {
	pb := make([]byte, 4)
	sb := make([]byte, 24)
	if _, err = rand.Read(pb); err != nil {
		return "", nil, "", fmt.Errorf("auth: random locator: %w", err)
	}
	if _, err = rand.Read(sb); err != nil {
		return "", nil, "", fmt.Errorf("auth: random secret: %w", err)
	}
	prefix = hex.EncodeToString(pb) // 8 hex chars, non-secret
	token = TokenScheme + "_" + prefix + "_" + base64.RawURLEncoding.EncodeToString(sb)
	return token, HashToken(token), prefix, nil
}

// HashToken returns the sha256 of a token, the stored form. The authn middleware
// hashes the presented token and looks the hash up.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
