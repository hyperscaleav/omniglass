package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDotEnvReadsTheCommonForms(t *testing.T) {
	got, err := ParseDotEnv(".env", `
# a comment
PLAIN=value
export EXPORTED=yes
SPACED = padded   # trailing comment
EMPTY=
JSON='{"type":"api-key","fields":{"key":"sk #1"}}'
DOUBLE="line\nnext \"q\""
HASH=a#b
`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"PLAIN": "value", "EXPORTED": "yes", "SPACED": "padded", "EMPTY": "",
		"JSON":   `{"type":"api-key","fields":{"key":"sk #1"}}`,
		"DOUBLE": "line\nnext \"q\"", "HASH": "a#b",
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, e := range got {
		if want[e.Key] != e.Value {
			t.Errorf("%s = %q, want %q", e.Key, e.Value, want[e.Key])
		}
	}
}

func TestParseDotEnvRefusesNamingTheLineNeverTheValue(t *testing.T) {
	for name, doc := range map[string]string{
		"no equals":     "sk-hidden",
		"bad key":       "1KEY=sk-hidden",
		"open single":   "KEY='sk-hidden",
		"open double":   `KEY="sk-hidden`,
		"after a quote": "KEY='a' sk-hidden",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDotEnv(".env.local", "OK=1\n"+doc)
			if !errors.Is(err, ErrDotEnv) || !strings.Contains(err.Error(), ".env.local line 2") || strings.Contains(err.Error(), "sk-hidden") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// .env.local wins over .env, and a variable the shell set wins over both.
func TestLoadDotEnvLayersTheFilesUnderTheShell(t *testing.T) {
	dir := t.TempDir()
	write := func(name, doc string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".env", "A=env\nB=env\nC=env\n")
	write(".env.local", "B=local\nC=local\nD=local\n")
	shell := map[string]string{"C": "shell"}
	set := map[string]string{}
	read, err := LoadDotEnv(dir,
		func(k string) (string, bool) { v, ok := shell[k]; return v, ok },
		func(k, v string) error { set[k] = v; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(read, ",") != ".env,.env.local" {
		t.Fatalf("read %v", read)
	}
	want := map[string]string{"A": "env", "B": "local", "D": "local"}
	if len(set) != len(want) {
		t.Fatalf("set %v, want %v", set, want)
	}
	for k, v := range want {
		if set[k] != v {
			t.Errorf("%s = %q, want %q", k, set[k], v)
		}
	}
}

func TestLoadDotEnvWithNoFilesSetsNothing(t *testing.T) {
	read, err := LoadDotEnv(t.TempDir(), func(string) (string, bool) { return "", false },
		func(k, v string) error { t.Fatalf("set %s", k); return nil })
	if err != nil || len(read) != 0 {
		t.Fatalf("read %v, err %v", read, err)
	}
}
