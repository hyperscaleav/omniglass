// Package config reads the single binary's runtime configuration from the
// environment. Every environment variable the binary reads is declared in the
// Registry here and read through Get (an unregistered read panics), so the
// rest of the code takes a typed Config and stays testable, and the generated
// env-var reference can never omit a variable.
//
// Configuration is intentionally minimal for the walking skeleton: the
// database DSN and the HTTP listen address. Later slices add their own keys
// through this same package so there remains exactly one config seam.
package config

import (
	"os"
	"path/filepath"
)

// Defaults applied when the corresponding env var is unset or empty.
const (
	// DefaultDSN points at a conventional local Postgres. Override with
	// OMNIGLASS_DSN (or the standard DATABASE_URL) in every real deployment;
	// production is BYO Postgres.
	DefaultDSN = "postgres://omniglass:omniglass@localhost:5432/omniglass?sslmode=disable"
	// DefaultAddr is the HTTP API listen address. Override with OMNIGLASS_ADDR;
	// port 0 asks the kernel for a free port, which the ready file reports.
	DefaultAddr = ":8080"
	// DefaultNatsAddr is the embedded NATS listen address (host:port). Override
	// with OMNIGLASS_NATS_ADDR; port 0 asks for a free port, as for the API.
	// Bound to loopback by default: a node reaches it through the same host
	// the API is on.
	DefaultNatsAddr = "127.0.0.1:4222"
	// DefaultDataDir holds local server state that is not in Postgres, notably
	// the fallback encryption key when no KEK is provided. Override with
	// OMNIGLASS_DATA_DIR; a real deployment sets OMNIGLASS_ENCRYPTION_KEY_FILE
	// and never touches this.
	DefaultDataDir = ".omniglass"
	// DefaultSettingsFile is empty: most deployments have no operator settings
	// file, so the settings engine's base layer is just the embedded code
	// defaults. Point OMNIGLASS_SETTINGS_FILE at a JSON or YAML file to add the
	// file layer between code defaults and the DB override.
	DefaultSettingsFile = ""
)

// Config is the resolved runtime configuration for one process.
type Config struct {
	// DSN is the Postgres connection string the Storage Gateway dials.
	DSN string
	// Addr is the host:port the HTTP API listens on.
	Addr string
	// SecureCookies marks the session cookie Secure (https only). Set
	// OMNIGLASS_SECURE_COOKIES=true behind TLS; off by default for local http dev.
	SecureCookies bool
	// TrustedProxies is the raw OMNIGLASS_TRUSTED_PROXIES list: the peers a
	// forwarded header is believed from (#1185). The server parses it at boot
	// (ingress.ParseTrustedProxies), so a bad entry fails the boot, not this Load.
	TrustedProxies string
	// NatsAddr is the host:port the embedded NATS server binds.
	NatsAddr string
	// NatsStoreDir is the JetStream store directory.
	NatsStoreDir string
	// NatsURL is the address the node-claim reply advertises to nodes. Empty
	// means the URL of the address the bus bound (loopback when it bound a
	// wildcard host), so a bus asked for port 0 advertises the port it got.
	// Set OMNIGLASS_NATS_URL when the node reaches the bus at another address.
	NatsURL string
	// DataDir is the directory for local, non-Postgres server state (the fallback
	// encryption key). Override with OMNIGLASS_DATA_DIR.
	DataDir string
	// SettingsFile is the path to an optional operator settings file, the middle
	// layer of the settings engine (between embedded code defaults and the DB
	// override). Empty means no file layer. Set OMNIGLASS_SETTINGS_FILE.
	SettingsFile string
	// ReadyFile is where the server writes the addresses it bound once it
	// serves, for a supervisor or a test that asked for port 0. Empty means no
	// file. Set OMNIGLASS_READY_FILE.
	ReadyFile string
}

// Load resolves the configuration from the environment, applying defaults for
// any unset key. It never fails: an unset DSN falls back to the local default
// so `omniglass server` runs out of the box against a dev Postgres, and a
// genuinely bad DSN surfaces later as a connection error, not a config error.
//
// DSN precedence is OMNIGLASS_DSN, then the conventional DATABASE_URL, then
// the local default. OMNIGLASS_DSN wins so an operator can pin the binary's
// database independently of any DATABASE_URL the surrounding platform sets.
func Load() Config {
	return Config{
		DSN:            resolveDSN(),
		Addr:           firstNonEmpty(Get("OMNIGLASS_ADDR"), DefaultAddr),
		SecureCookies:  Get("OMNIGLASS_SECURE_COOKIES") == "true",
		TrustedProxies: Get("OMNIGLASS_TRUSTED_PROXIES"),
		NatsAddr:       firstNonEmpty(Get("OMNIGLASS_NATS_ADDR"), DefaultNatsAddr),
		NatsStoreDir:   firstNonEmpty(Get("OMNIGLASS_NATS_STORE_DIR"), filepath.Join(os.TempDir(), "omniglass-nats")),
		NatsURL:        Get("OMNIGLASS_NATS_URL"),
		DataDir:        firstNonEmpty(Get("OMNIGLASS_DATA_DIR"), DefaultDataDir),
		SettingsFile:   firstNonEmpty(Get("OMNIGLASS_SETTINGS_FILE"), DefaultSettingsFile),
		ReadyFile:      Get("OMNIGLASS_READY_FILE"),
	}
}

func resolveDSN() string {
	return firstNonEmpty(Get("OMNIGLASS_DSN"), Get("DATABASE_URL"), DefaultDSN)
}

// firstNonEmpty returns the first non-empty string, or "" if all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
