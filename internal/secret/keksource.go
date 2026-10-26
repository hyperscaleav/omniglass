package secret

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KEK sources, most-explicit-first. The env and file sources are operator-set;
// the fallback is a generated key persisted to the data volume, a convenience
// that trades security for not losing every secret on reboot.
const (
	SourceEnv      = "env"
	SourceFile     = "file"
	SourceFallback = "fallback"
)

const (
	envKey     = "OMNIGLASS_ENCRYPTION_KEY"
	envKeyFile = "OMNIGLASS_ENCRYPTION_KEY_FILE"
	kekLen     = 32
	fallback   = "secret.key"
)

// The names the two variables carried before #558. "Secret key" already meant a
// secret's own identifier, so an operator could not tell the master key that
// encrypts every secret from the name of one; the variables are the encryption
// key's now. These are still read, for one deprecation window, because the
// variable is how every deployment supplies the key and a rename that ignored
// them would lock an operator out of their own encrypted data on upgrade.
// Removing them is a breaking change for a later release.
const (
	legacyEnvKey     = "OMNIGLASS_SECRET_KEY"
	legacyEnvKeyFile = "OMNIGLASS_SECRET_KEY_FILE"
)

// kekVars is one generation of the pair: the base64 key itself, and the path to
// a file holding it.
type kekVars struct{ env, file string }

var (
	currentVars = kekVars{env: envKey, file: envKeyFile}
	legacyVars  = kekVars{env: legacyEnvKey, file: legacyEnvKeyFile}
)

// LoadKEK resolves the 32-byte key-encryption key from, in order: the
// OMNIGLASS_ENCRYPTION_KEY env var (base64), the OMNIGLASS_ENCRYPTION_KEY_FILE
// path, or a generated fallback persisted under dataDir. The fallback calls
// warn: an auto-generated key sitting on the same volume as the data is
// convenience, not security (disk theft gets both), and is not the intended
// deployment model.
//
// The pre-#558 names are read when neither new name is set, and warn on every
// call, so every boot, naming the replacement: a deprecation an operator can
// miss once is one they will miss. When a new name is set the old generation
// is ignored as a whole, whichever source each uses (an operator who has
// started the move meant the new value), and a leftover old name warns that it
// is ignored. Booting with only the new names warns about nothing.
func LoadKEK(getenv func(string) string, dataDir string, warn func(string)) ([]byte, string, error) {
	vars := currentVars
	if getenv(envKey) == "" && getenv(envKeyFile) == "" {
		vars = legacyVars
	}
	for _, rename := range [][2]string{{legacyEnvKey, envKey}, {legacyEnvKeyFile, envKeyFile}} {
		old, replacement := rename[0], rename[1]
		if getenv(old) == "" {
			continue
		}
		if vars == legacyVars {
			warn(fmt.Sprintf("%s is deprecated and will be removed in a later release: "+
				"rename it to %s (the value is unchanged). It is read for now.", old, replacement))
		} else {
			warn(fmt.Sprintf("%s is set but ignored, because %s or %s is set: remove %s.",
				old, envKey, envKeyFile, old))
		}
	}
	if v := getenv(vars.env); v != "" {
		kek, err := decodeKey(v)
		if err != nil {
			return nil, "", fmt.Errorf("secret: %s: %w", vars.env, err)
		}
		return kek, SourceEnv, nil
	}
	if path := getenv(vars.file); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("secret: read %s: %w", vars.file, err)
		}
		kek, err := decodeKey(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, "", fmt.Errorf("secret: %s %q: %w", vars.file, path, err)
		}
		return kek, SourceFile, nil
	}
	return loadOrCreateFallback(dataDir, warn)
}

func loadOrCreateFallback(dataDir string, warn func(string)) ([]byte, string, error) {
	path := filepath.Join(dataDir, fallback)
	if raw, err := os.ReadFile(path); err == nil {
		kek, err := decodeKey(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, "", fmt.Errorf("secret: fallback key %q: %w", path, err)
		}
		return kek, SourceFallback, nil
	}
	kek := make([]byte, kekLen)
	if _, err := rand.Read(kek); err != nil {
		return nil, "", fmt.Errorf("secret: generate fallback key: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("secret: create data dir: %w", err)
	}
	enc := base64.StdEncoding.EncodeToString(kek)
	if err := os.WriteFile(path, []byte(enc), 0o600); err != nil {
		return nil, "", fmt.Errorf("secret: persist fallback key: %w", err)
	}
	warn(fmt.Sprintf("no %s or %s set: generated a random encryption key at %s. "+
		"This is convenience, not security (a key on the data volume falls with the volume). "+
		"Set %s (a mounted file) for any real deployment.", envKey, envKeyFile, path, envKeyFile))
	return kek, SourceFallback, nil
}

// decodeKey accepts a base64 (std or raw) KEK and enforces the 32-byte length.
func decodeKey(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			if len(b) != kekLen {
				return nil, fmt.Errorf("key must be %d bytes, got %d", kekLen, len(b))
			}
			return b, nil
		}
	}
	return nil, fmt.Errorf("key is not valid base64")
}
