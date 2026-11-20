package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DotEnvFiles are the files the omniglass binary loads from its working
// directory before it runs (#2223), in order: .env, then .env.local on top of
// it. A variable the shell already set wins over both, so a file is a
// default a one-off export overrides.
var DotEnvFiles = []string{".env", ".env.local"}

// ErrDotEnv is a line of a .env file that does not parse. The message names
// the file and the line number and never quotes the line, which may carry a
// secret.
var ErrDotEnv = errors.New("dotenv")

var dotEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// DotEnvEntry is one KEY=VALUE a .env file declares.
type DotEnvEntry struct{ Key, Value string }

// ParseDotEnv reads a .env document: one KEY=VALUE a line, blank lines and
// lines starting with # skipped, an optional leading `export `. A value in
// single quotes is taken literally (the form a JSON value takes), one in
// double quotes takes \n, \", \\ escapes, and a bare value is trimmed and
// ends at a ` #` comment. name labels the errors.
func ParseDotEnv(name, doc string) ([]DotEnvEntry, error) {
	var out []DotEnvEntry
	sc := bufio.NewScanner(strings.NewReader(doc))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, raw, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !dotEnvKey.MatchString(key) {
			return nil, fmt.Errorf("%w: %s line %d is not KEY=VALUE", ErrDotEnv, name, n)
		}
		val, err := dotEnvValue(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("%w: %s line %d (%s): %v", ErrDotEnv, name, n, key, err)
		}
		out = append(out, DotEnvEntry{Key: key, Value: val})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrDotEnv, name, err)
	}
	return out, nil
}

func dotEnvValue(raw string) (string, error) {
	switch {
	case strings.HasPrefix(raw, "'"):
		end := strings.Index(raw[1:], "'")
		if end < 0 {
			return "", errors.New("the single quote is not closed")
		}
		if rest := strings.TrimSpace(raw[end+2:]); rest != "" && !strings.HasPrefix(rest, "#") {
			return "", errors.New("text follows the closing quote")
		}
		return raw[1 : end+1], nil
	case strings.HasPrefix(raw, `"`):
		var b strings.Builder
		for i := 1; i < len(raw); i++ {
			c := raw[i]
			if c == '\\' && i+1 < len(raw) {
				i++
				switch raw[i] {
				case 'n':
					b.WriteByte('\n')
				default:
					b.WriteByte(raw[i])
				}
				continue
			}
			if c == '"' {
				if rest := strings.TrimSpace(raw[i+1:]); rest != "" && !strings.HasPrefix(rest, "#") {
					return "", errors.New("text follows the closing quote")
				}
				return b.String(), nil
			}
			b.WriteByte(c)
		}
		return "", errors.New("the double quote is not closed")
	default:
		if i := strings.Index(raw, " #"); i >= 0 {
			raw = raw[:i]
		}
		return strings.TrimSpace(raw), nil
	}
}

// LoadDotEnv loads DotEnvFiles from dir into the process environment through
// setenv, a later file over an earlier one, and never over a variable that was
// already set when it began (lookup reports those). A file that is absent is
// skipped; one that does not parse is an error naming it. It returns the files
// it read.
func LoadDotEnv(dir string, lookup func(string) (string, bool), setenv func(string, string) error) ([]string, error) {
	shell := map[string]bool{}
	merged := map[string]string{}
	var order []string
	var read []string
	for _, f := range DotEnvFiles {
		path := filepath.Join(dir, f)
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrDotEnv, f, err)
		}
		entries, err := ParseDotEnv(f, string(b))
		if err != nil {
			return nil, err
		}
		read = append(read, f)
		for _, e := range entries {
			if _, seen := merged[e.Key]; !seen {
				if _, set := lookup(e.Key); set {
					shell[e.Key] = true
				}
				order = append(order, e.Key)
			}
			merged[e.Key] = e.Value
		}
	}
	for _, k := range order {
		if shell[k] {
			continue
		}
		if err := setenv(k, merged[k]); err != nil {
			return nil, fmt.Errorf("%w: set %s: %v", ErrDotEnv, k, err)
		}
	}
	return read, nil
}

// LoadDotEnvFromWorkingDir is LoadDotEnv over the process: the working
// directory's files into the process environment, under what the shell set.
// The binary calls it once, before any command reads its configuration.
func LoadDotEnvFromWorkingDir() ([]string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, nil
	}
	return LoadDotEnv(wd, os.LookupEnv, os.Setenv)
}
