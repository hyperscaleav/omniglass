package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureEnv is an Env over an in-memory filesystem and environment: the
// docker CLI's config and context store as fixtures.
func fixtureEnv(env map[string]string, files map[string]string) Env {
	return Env{
		Getenv: func(k string) string { return env[k] },
		ReadFile: func(p string) ([]byte, error) {
			if s, ok := files[p]; ok {
				return []byte(s), nil
			}
			return nil, fs.ErrNotExist
		},
		Home: "/home/u",
		GOOS: "linux",
	}
}

// contextMeta is the context store's path for a context, keyed by the
// sha256 of its name as the docker CLI keys it.
func contextMeta(cfgDir, name string) string {
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(cfgDir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")
}

func TestResolveDaemonOrder(t *testing.T) {
	cfg := "/home/u/.docker"
	store := map[string]string{
		filepath.Join(cfg, "config.json"):   `{"currentContext":"desktop-linux"}`,
		contextMeta(cfg, "desktop-linux"):   `{"Name":"desktop-linux","Endpoints":{"docker":{"Host":"unix:///home/u/.docker/run/docker.sock"}}}`,
		contextMeta(cfg, "rootless"):        `{"Name":"rootless","Endpoints":{"docker":{"Host":"unix:///run/user/1000/docker.sock"}}}`,
		contextMeta("/etc/dc", "elsewhere"): `{"Name":"elsewhere","Endpoints":{"docker":{"Host":"ssh://ops@build"}}}`,
	}
	for _, tc := range []struct {
		name       string
		env        map[string]string
		files      map[string]string
		goos       string
		wantHost   string
		wantSource string
	}{
		{"DOCKER_HOST wins over everything",
			map[string]string{"DOCKER_HOST": "tcp://127.0.0.1:2375", "DOCKER_CONTEXT": "rootless"}, store, "linux",
			"tcp://127.0.0.1:2375", "DOCKER_HOST"},
		{"DOCKER_CONTEXT wins over the config's context",
			map[string]string{"DOCKER_CONTEXT": "rootless"}, store, "linux",
			"unix:///run/user/1000/docker.sock", `"rootless"`},
		{"the CLI config's current context",
			nil, store, "linux",
			"unix:///home/u/.docker/run/docker.sock", "current context"},
		{"DOCKER_CONFIG moves the config and the store",
			map[string]string{"DOCKER_CONFIG": "/etc/dc", "DOCKER_CONTEXT": "elsewhere"}, store, "linux",
			"ssh://ops@build", `"elsewhere"`},
		{"the default context is the default socket",
			map[string]string{"DOCKER_CONTEXT": "default"}, store, "linux",
			defaultUnixHost, "default socket"},
		{"no config at all is the default socket",
			nil, nil, "darwin",
			defaultUnixHost, "default socket"},
		{"on Windows, the named pipe",
			nil, nil, "windows",
			defaultPipeHost, "named pipe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := fixtureEnv(tc.env, tc.files)
			e.GOOS = tc.goos
			d, err := ResolveDaemon(e)
			if err != nil {
				t.Fatal(err)
			}
			if d.Host != tc.wantHost || !strings.Contains(d.Source, tc.wantSource) {
				t.Fatalf("got %+v, want host %s from a source naming %q", d, tc.wantHost, tc.wantSource)
			}
		})
	}
}

// A context the store does not hold is named, with where it came from,
// rather than silently falling back to a daemon the user did not choose.
func TestResolveDaemonUnknownContext(t *testing.T) {
	_, err := ResolveDaemon(fixtureEnv(map[string]string{"DOCKER_CONTEXT": "gone"}, nil))
	if err == nil || !strings.Contains(err.Error(), `"gone"`) || !strings.Contains(err.Error(), "DOCKER_CONTEXT") {
		t.Fatalf("got %v, want an error naming the context and DOCKER_CONTEXT", err)
	}
}

func TestIsLocal(t *testing.T) {
	for host, want := range map[string]bool{
		"unix:///var/run/docker.sock":            true,
		"unix:///home/u/.docker/run/docker.sock": true,
		"npipe:////./pipe/docker_engine":         true,
		"tcp://127.0.0.1:2375":                   true,
		"tcp://localhost:2375":                   true,
		"tcp://[::1]:2375":                       true,
		"tcp://10.0.0.5:2376":                    false,
		"tcp://build.example.com:2376":           false,
		"ssh://ops@build":                        false,
		"::not a url":                            false,
	} {
		if got := IsLocal(host); got != want {
			t.Errorf("IsLocal(%q) = %v, want %v", host, got, want)
		}
	}
}
