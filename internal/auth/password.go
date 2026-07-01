package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters for password hashing (OWASP-leaning defaults): 19 MiB of
// memory, 2 iterations, 1 lane, a 32-byte key, a 16-byte salt.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB == 19 MiB
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// ErrMalformedHash is returned by VerifyPassword when the stored encoding is not
// a well-formed argon2id PHC string.
var ErrMalformedHash = errors.New("auth: malformed password hash")

// HashPassword returns a PHC-encoded argon2id hash of the password. The encoding
// is self-describing (it carries the version, the parameters, and the salt), so
// VerifyPassword needs only the encoded string. This is what a password
// credential stores as its secret.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether the password matches the PHC-encoded argon2id
// hash, recomputing the key with the encoded parameters and salt and comparing
// in constant time. A malformed encoding is an error, never a silent false.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrMalformedHash
	}
	var mem, t uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &threads); err != nil {
		return false, ErrMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrMalformedHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrMalformedHash
	}
	got := argon2.IDKey([]byte(password), salt, t, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
