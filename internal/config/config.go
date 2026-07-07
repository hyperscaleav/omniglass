// Package config reads the single binary's runtime configuration from the
// environment. It is the only place env vars are read, so the rest of the
// code takes a typed Config and stays testable without poking at os.Getenv.
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
	// DefaultAddr is the HTTP API listen address. Override with OMNIGLASS_ADDR.
	DefaultAddr = ":8080"
	// DefaultNatsAddr is the embedded NATS listen address (host:port). Override
	// with OMNIGLASS_NATS_ADDR. Bound to loopback by default: a node reaches it
	// through the same host the API is on.
	DefaultNatsAddr = "127.0.0.1:4222"
	// DefaultNatsURL is the URL the node-claim exchange advertises. Override with
	// OMNIGLASS_NATS_URL when the node reaches the bus at a different address.
	DefaultNatsURL = "nats://127.0.0.1:4222"
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
	// NatsAddr is the host:port the embedded NATS server binds.
	NatsAddr string
	// NatsStoreDir is the JetStream store directory.
	NatsStoreDir string
	// NatsURL is the address the node-claim reply advertises to nodes.
	NatsURL string
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
		NatsAddr:      firstNonEmpty(os.Getenv("OMNIGLASS_NATS_ADDR"), DefaultNatsAddr),
		NatsStoreDir:  firstNonEmpty(os.Getenv("OMNIGLASS_NATS_STORE_DIR"), filepath.Join(os.TempDir(), "omniglass-nats")),
		NatsURL:       firstNonEmpty(os.Getenv("OMNIGLASS_NATS_URL"), DefaultNatsURL),
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
