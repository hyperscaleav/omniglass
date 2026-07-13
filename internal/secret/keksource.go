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
	envKey     = "OMNIGLASS_SECRET_KEY"
	envKeyFile = "OMNIGLASS_SECRET_KEY_FILE"
	kekLen     = 32
	fallback   = "secret.key"
)

// LoadKEK resolves the 32-byte key-encryption key from, in order: the
// OMNIGLASS_SECRET_KEY env var (base64), the OMNIGLASS_SECRET_KEY_FILE path, or
// a generated fallback persisted under dataDir. The fallback calls warn: an
// auto-generated key sitting on the same volume as the data is convenience, not
// security (disk theft gets both), and is not the intended deployment model.
func LoadKEK(getenv func(string) string, dataDir string, warn func(string)) ([]byte, string, error) {
	if v := getenv(envKey); v != "" {
		kek, err := decodeKey(v)
		if err != nil {
			return nil, "", fmt.Errorf("secret: %s: %w", envKey, err)
		}
		return kek, SourceEnv, nil
	}
	if path := getenv(envKeyFile); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("secret: read %s: %w", envKeyFile, err)
		}
		kek, err := decodeKey(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, "", fmt.Errorf("secret: %s %q: %w", envKeyFile, path, err)
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
	warn(fmt.Sprintf("no %s or %s set: generated a random secret key at %s. "+
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
