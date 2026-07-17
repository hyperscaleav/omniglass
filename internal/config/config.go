// Package config reads the single binary's runtime configuration from the
// environment. It is the only place env vars are read, so the rest of the
// code takes a typed Config and stays testable without poking at os.Getenv.
//
// Configuration is intentionally minimal for the walking skeleton: the
// database DSN and the HTTP listen address. Later slices add their own keys
// through this same package so there remains exactly one config seam.
package config

import "os"

// Defaults applied when the corresponding env var is unset or empty.
const (
	// DefaultDSN points at a conventional local Postgres. Override with
	// OMNIGLASS_DSN (or the standard DATABASE_URL) in every real deployment;
	// production is BYO Postgres.
	DefaultDSN = "postgres://omniglass:omniglass@localhost:5432/omniglass?sslmode=disable"
	// DefaultAddr is the HTTP API listen address. Override with OMNIGLASS_ADDR.
	DefaultAddr = ":8080"
	// DefaultDataDir holds local server state that is not in Postgres, notably
	// the fallback secret key when no KEK is provided. Override with
	// OMNIGLASS_DATA_DIR; a real deployment sets OMNIGLASS_SECRET_KEY_FILE and
	// never touches this.
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
	// DataDir is the directory for local, non-Postgres server state (the fallback
	// secret key). Override with OMNIGLASS_DATA_DIR.
	DataDir string
	// SettingsFile is the path to an optional operator settings file, the middle
	// layer of the settings engine (between embedded code defaults and the DB
	// override). Empty means no file layer. Set OMNIGLASS_SETTINGS_FILE.
	SettingsFile string
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
		DSN:           resolveDSN(),
		Addr:          firstNonEmpty(os.Getenv("OMNIGLASS_ADDR"), DefaultAddr),
		SecureCookies: os.Getenv("OMNIGLASS_SECURE_COOKIES") == "true",
		DataDir:       firstNonEmpty(os.Getenv("OMNIGLASS_DATA_DIR"), DefaultDataDir),
		SettingsFile:  firstNonEmpty(os.Getenv("OMNIGLASS_SETTINGS_FILE"), DefaultSettingsFile),
	}
}

func resolveDSN() string {
	return firstNonEmpty(os.Getenv("OMNIGLASS_DSN"), os.Getenv("DATABASE_URL"), DefaultDSN)
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
