package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("OMNIGLASS_DSN", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("OMNIGLASS_ADDR", "")

	got := Load()
	if got.DSN != DefaultDSN {
		t.Errorf("DSN = %q, want default %q", got.DSN, DefaultDSN)
	}
	if got.Addr != DefaultAddr {
		t.Errorf("Addr = %q, want default %q", got.Addr, DefaultAddr)
	}
}

func TestLoadOmniglassDSNWinsOverDatabaseURL(t *testing.T) {
	t.Setenv("OMNIGLASS_DSN", "postgres://a/og")
	t.Setenv("DATABASE_URL", "postgres://b/other")

	if got := Load().DSN; got != "postgres://a/og" {
		t.Errorf("DSN = %q, want OMNIGLASS_DSN to win", got)
	}
}

func TestLoadFallsBackToDatabaseURL(t *testing.T) {
	t.Setenv("OMNIGLASS_DSN", "")
	t.Setenv("DATABASE_URL", "postgres://b/other")

	if got := Load().DSN; got != "postgres://b/other" {
		t.Errorf("DSN = %q, want DATABASE_URL fallback", got)
	}
}

func TestLoadAddrOverride(t *testing.T) {
	t.Setenv("OMNIGLASS_ADDR", ":9999")

	if got := Load().Addr; got != ":9999" {
		t.Errorf("Addr = %q, want :9999", got)
	}
}

// An unset bus URL stays empty, so the server advertises the address its bus
// bound rather than a fixed port it may not hold.
func TestLoadLeavesAnUnsetBusURLToTheServer(t *testing.T) {
	t.Setenv("OMNIGLASS_NATS_URL", "")
	if got := Load().NatsURL; got != "" {
		t.Errorf("NatsURL = %q, want empty for the server to derive", got)
	}
	t.Setenv("OMNIGLASS_NATS_URL", "nats://bus.example:4222")
	if got := Load().NatsURL; got != "nats://bus.example:4222" {
		t.Errorf("NatsURL = %q, want the configured URL", got)
	}
}

func TestLoadReadyFile(t *testing.T) {
	t.Setenv("OMNIGLASS_READY_FILE", "/run/omniglass/ready.json")
	if got := Load().ReadyFile; got != "/run/omniglass/ready.json" {
		t.Errorf("ReadyFile = %q, want the configured path", got)
	}
}

// A server is named for its host unless OMNIGLASS_SERVER_NAME says otherwise:
// the name its own platform component carries, so two servers on one
// database are two components (#1627).
func TestLoadServerName(t *testing.T) {
	t.Setenv("OMNIGLASS_SERVER_NAME", "")
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = DefaultServerName
	}
	if got := Load().ServerName; got != host {
		t.Errorf("ServerName = %q, want the hostname %q", got, host)
	}
	t.Setenv("OMNIGLASS_SERVER_NAME", "api-east-1")
	if got := Load().ServerName; got != "api-east-1" {
		t.Errorf("ServerName = %q, want the configured name", got)
	}
}
