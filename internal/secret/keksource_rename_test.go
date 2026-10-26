package secret

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The encryption key's variables were named OMNIGLASS_SECRET_KEY and
// OMNIGLASS_SECRET_KEY_FILE, which an operator could not tell apart from a
// secret's own key (#558). They are OMNIGLASS_ENCRYPTION_KEY and
// OMNIGLASS_ENCRYPTION_KEY_FILE now. The old names are read for one deprecation
// window, because the variable is how every deployment supplies the key: a
// rename that ignored them would lock an operator out of their own encrypted
// data on upgrade.

func keyFile(t *testing.T, key []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "k")
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func collect() (*[]string, func(string)) {
	var got []string
	return &got, func(m string) { got = append(got, m) }
}

func TestLoadKEKFromEncryptionKeyLogsNothing(t *testing.T) {
	key := testKey(0x11)
	warnings, warn := collect()
	kek, src, err := LoadKEK(env(map[string]string{
		"OMNIGLASS_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(key),
	}), t.TempDir(), warn)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if src != SourceEnv || string(kek) != string(key) {
		t.Fatalf("source %q, key match %v: want env and the key", src, string(kek) == string(key))
	}
	if len(*warnings) != 0 {
		t.Fatalf("booting with only the new name warned: %q", *warnings)
	}
}

func TestLoadKEKFromEncryptionKeyFileLogsNothing(t *testing.T) {
	key := testKey(0x12)
	warnings, warn := collect()
	kek, src, err := LoadKEK(env(map[string]string{
		"OMNIGLASS_ENCRYPTION_KEY_FILE": keyFile(t, key),
	}), t.TempDir(), warn)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if src != SourceFile || string(kek) != string(key) {
		t.Fatalf("source %q: want file and the key", src)
	}
	if len(*warnings) != 0 {
		t.Fatalf("booting with only the new name warned: %q", *warnings)
	}
}

func TestLoadKEKReadsTheOldNameAndWarnsEveryBoot(t *testing.T) {
	// Both old names, one at a time. Each still loads the key, and each boot
	// says the name is deprecated and names its replacement: the warning is the
	// whole point of the window, so it is not a once-only notice.
	key := testKey(0x13)
	cases := map[string]struct {
		vars map[string]string
		src  string
		want string
	}{
		"env": {
			vars: map[string]string{"OMNIGLASS_SECRET_KEY": base64.StdEncoding.EncodeToString(key)},
			src:  SourceEnv, want: "OMNIGLASS_ENCRYPTION_KEY",
		},
		"file": {
			vars: map[string]string{"OMNIGLASS_SECRET_KEY_FILE": keyFile(t, key)},
			src:  SourceFile, want: "OMNIGLASS_ENCRYPTION_KEY_FILE",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			for boot := 1; boot <= 2; boot++ {
				warnings, warn := collect()
				kek, src, err := LoadKEK(env(c.vars), t.TempDir(), warn)
				if err != nil {
					t.Fatalf("boot %d: load: %v", boot, err)
				}
				if src != c.src || string(kek) != string(key) {
					t.Fatalf("boot %d: source %q, want %q and the key", boot, src, c.src)
				}
				if len(*warnings) != 1 {
					t.Fatalf("boot %d: warnings %q, want exactly one", boot, *warnings)
				}
				w := (*warnings)[0]
				for old := range c.vars {
					if !strings.Contains(w, old) || !strings.Contains(w, c.want) || !strings.Contains(w, "deprecated") {
						t.Errorf("boot %d: warning %q should call %s deprecated and name %s", boot, w, old, c.want)
					}
				}
			}
		})
	}
}

func TestLoadKEKNewNameWinsAndTheOldOneIsCalledOut(t *testing.T) {
	// Both generations set, with different keys. The new one wins, whichever
	// source each uses (a new key file outranks an old key variable, since the
	// operator has started the move), and the leftover old name is called out
	// as ignored so it does not linger.
	newKey, oldKey := testKey(0x21), testKey(0x22)
	cases := map[string]map[string]string{
		"env over env": {
			"OMNIGLASS_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(newKey),
			"OMNIGLASS_SECRET_KEY":     base64.StdEncoding.EncodeToString(oldKey),
		},
		"new file over old env": {
			"OMNIGLASS_ENCRYPTION_KEY_FILE": keyFile(t, newKey),
			"OMNIGLASS_SECRET_KEY":          base64.StdEncoding.EncodeToString(oldKey),
		},
		"new env over old file": {
			"OMNIGLASS_ENCRYPTION_KEY":  base64.StdEncoding.EncodeToString(newKey),
			"OMNIGLASS_SECRET_KEY_FILE": keyFile(t, oldKey),
		},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			warnings, warn := collect()
			kek, _, err := LoadKEK(env(vars), t.TempDir(), warn)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if string(kek) != string(newKey) {
				t.Fatalf("loaded the old generation's key; the new name must win")
			}
			if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "ignored") {
				t.Fatalf("warnings %q, want one saying the old name is ignored", *warnings)
			}
		})
	}
}

func TestNoKEKMessageCallsItASecretKey(t *testing.T) {
	// "Secret key" is the identifier of a secret an operator named; the key that
	// encrypts every secret is the encryption key, in every string an operator
	// reads (#558). The fallback warning is the one LoadKEK writes on its own.
	warnings, warn := collect()
	if _, _, err := LoadKEK(env(nil), t.TempDir(), warn); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(*warnings) != 1 {
		t.Fatalf("fallback warnings %q, want one", *warnings)
	}
	w := (*warnings)[0]
	if strings.Contains(strings.ToLower(w), "secret key") || strings.Contains(w, "OMNIGLASS_SECRET_KEY") {
		t.Errorf("fallback warning %q still says secret key", w)
	}
	if !strings.Contains(w, "OMNIGLASS_ENCRYPTION_KEY_FILE") || !strings.Contains(w, "encryption key") {
		t.Errorf("fallback warning %q should name the encryption key and its file variable", w)
	}
}
