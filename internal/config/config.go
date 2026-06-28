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
)

// Config is the resolved runtime configuration for one process.
type Config struct {
	// DSN is the Postgres connection string the Storage Gateway dials.
	DSN string
	// Addr is the host:port the HTTP API listens on.
	Addr string
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
		DSN:  resolveDSN(),
		Addr: firstNonEmpty(os.Getenv("OMNIGLASS_ADDR"), DefaultAddr),
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
