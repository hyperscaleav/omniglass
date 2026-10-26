package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

// Daemon is the Docker daemon the lab drives: its address, and where that
// address came from, so a preflight failure can name the setting to fix.
type Daemon struct {
	Host   string
	Source string
}

// Env is what the resolver reads from its surroundings: the environment, the
// filesystem (the docker CLI's config and context store), the home directory
// and the operating system. Injected, so the resolver is pure and tested
// against fixtures of the context store.
type Env struct {
	Getenv   func(string) string
	ReadFile func(string) ([]byte, error)
	Home     string
	GOOS     string
}

// The default daemon addresses, by platform: the named pipe on Windows, the
// socket everywhere else (Docker Desktop on macOS links it there too).
const (
	defaultUnixHost = "unix:///var/run/docker.sock"
	defaultPipeHost = "npipe:////./pipe/docker_engine"
)

// ResolveDaemon finds the daemon the way the docker CLI does: DOCKER_HOST,
// then DOCKER_CONTEXT, then the CLI config's current context, then the
// platform's default socket or named pipe. A context names its daemon in the
// CLI's context store, keyed by the sha256 of the context's name.
func ResolveDaemon(e Env) (Daemon, error) {
	if h := e.Getenv("DOCKER_HOST"); h != "" {
		return Daemon{Host: h, Source: "DOCKER_HOST"}, nil
	}
	cfgDir := e.Getenv("DOCKER_CONFIG")
	if cfgDir == "" {
		cfgDir = filepath.Join(e.Home, ".docker")
	}
	name, source := e.Getenv("DOCKER_CONTEXT"), "DOCKER_CONTEXT"
	if name == "" {
		b, err := e.ReadFile(filepath.Join(cfgDir, "config.json"))
		switch {
		case err == nil:
			var cfg struct {
				CurrentContext string `json:"currentContext"`
			}
			if err := json.Unmarshal(b, &cfg); err != nil {
				return Daemon{}, fmt.Errorf("read the docker CLI config %s: %w", filepath.Join(cfgDir, "config.json"), err)
			}
			name, source = cfg.CurrentContext, "the docker CLI's current context"
		case !errors.Is(err, fs.ErrNotExist):
			return Daemon{}, fmt.Errorf("read the docker CLI config: %w", err)
		}
	}
	if name != "" && name != "default" {
		sum := sha256.Sum256([]byte(name))
		meta := filepath.Join(cfgDir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")
		b, err := e.ReadFile(meta)
		if err != nil {
			return Daemon{}, fmt.Errorf("docker context %q (from %s) is not in the docker CLI's context store: %w", name, source, err)
		}
		var m struct {
			Endpoints map[string]struct {
				Host string `json:"Host"`
			} `json:"Endpoints"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return Daemon{}, fmt.Errorf("docker context %q: read %s: %w", name, meta, err)
		}
		h := m.Endpoints["docker"].Host
		if h == "" {
			return Daemon{}, fmt.Errorf("docker context %q (from %s) names no docker endpoint", name, source)
		}
		return Daemon{Host: h, Source: fmt.Sprintf("docker context %q (%s)", name, source)}, nil
	}
	if e.GOOS == "windows" {
		return Daemon{Host: defaultPipeHost, Source: "the default named pipe"}, nil
	}
	return Daemon{Host: defaultUnixHost, Source: "the default socket"}, nil
}

// IsLocal reports whether a daemon address is on this machine: a socket or a
// named pipe, or TCP to a loopback address. The lab publishes the console on
// this machine's loopback, which a remote daemon would bind on another
// machine instead, so a remote daemon is refused at preflight.
func IsLocal(host string) bool {
	u, err := url.Parse(host)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "unix", "npipe":
		return true
	case "tcp", "http", "https":
		h := u.Hostname()
		if strings.EqualFold(h, "localhost") {
			return true
		}
		ip := net.ParseIP(h)
		return ip != nil && ip.IsLoopback()
	}
	return false
}
