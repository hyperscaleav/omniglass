package secret

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadKEKFromEnv(t *testing.T) {
	key := testKey(0x07)
	e := env(map[string]string{"OMNIGLASS_SECRET_KEY": base64.StdEncoding.EncodeToString(key)})
	kek, src, err := LoadKEK(e, t.TempDir(), func(string) {})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if src != SourceEnv {
		t.Fatalf("source = %q, want env", src)
	}
	if string(kek) != string(key) {
		t.Fatalf("kek mismatch")
	}
}

func TestLoadKEKEnvWrongLength(t *testing.T) {
	e := env(map[string]string{"OMNIGLASS_SECRET_KEY": base64.StdEncoding.EncodeToString([]byte("too-short"))})
	if _, _, err := LoadKEK(e, t.TempDir(), func(string) {}); err == nil {
		t.Fatalf("expected error for short key")
	}
}

func TestLoadKEKFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k")
	key := testKey(0x09)
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		t.Fatal(err)
	}
	e := env(map[string]string{"OMNIGLASS_SECRET_KEY_FILE": path})
	kek, src, err := LoadKEK(e, t.TempDir(), func(string) {})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if src != SourceFile {
		t.Fatalf("source = %q, want file", src)
	}
	if string(kek) != string(key) {
		t.Fatalf("kek mismatch")
	}
}

// No key configured: generate a random KEK, persist it to the data dir (so it
// survives a reboot), and warn. A second load reads the same persisted key.
func TestLoadKEKFallbackPersistsAndWarns(t *testing.T) {
	dir := t.TempDir()
	warned := 0
	warn := func(string) { warned++ }

	kek1, src, err := LoadKEK(env(nil), dir, warn)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if src != SourceFallback {
		t.Fatalf("source = %q, want fallback", src)
	}
	if len(kek1) != 32 {
		t.Fatalf("kek len = %d, want 32", len(kek1))
	}
	if warned == 0 {
		t.Fatalf("expected a warning on fallback")
	}

	kek2, _, err := LoadKEK(env(nil), dir, func(string) {})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if string(kek1) != string(kek2) {
		t.Fatalf("fallback key not stable across loads")
	}
}
