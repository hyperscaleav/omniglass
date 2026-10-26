package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// The declarative environment registry: one entry per variable the binary
// reads, with the operator doc and deployment scope attached to the read
// itself. Get refuses an unregistered key, so a new variable cannot be read
// without being declared, and the declaration is what the generated env-var
// reference (docs/src/generated/config.json, via cmd/configgen on make gen)
// renders into the deployment guides. Adding a variable and documenting it are
// the same act.

// Var is one declared environment variable.
type Var struct {
	// Key is the environment variable name.
	Key string `json:"key"`
	// Default is the human-readable default shown in the reference; empty means
	// none. The authoritative default lives with the consuming code.
	Default string `json:"default,omitempty"`
	// Scope is where the variable applies: "server" (the server process and the
	// container/Helm guides), "node" (the edge process), "cli" (the operator CLI,
	// surfaced through the generated CLI reference instead of the server tables),
	// or "test" (test harness only).
	Scope string `json:"scope"`
	// Doc is the one-line operator description.
	Doc string `json:"doc"`
}

// Registry declares every environment variable the binary reads, in the order
// the reference renders them.
var Registry = []Var{
	{Key: "OMNIGLASS_DSN", Default: "local dev DSN", Scope: "server",
		Doc: "Postgres connection string the Storage Gateway dials; wins over DATABASE_URL when both are set"},
	{Key: "DATABASE_URL", Scope: "server",
		Doc: "conventional DSN fallback, used when OMNIGLASS_DSN is unset"},
	{Key: "OMNIGLASS_ADDR", Default: ":8080", Scope: "server",
		Doc: "HTTP listen address; port 0 binds a free port, reported in the ready file"},
	{Key: "OMNIGLASS_SECURE_COOKIES", Default: "off", Scope: "server",
		Doc: "set true behind TLS to mark the session cookie Secure (https only)"},
	{Key: "OMNIGLASS_TRUSTED_PROXIES", Default: "loopback and private ranges", Scope: "server",
		Doc: "comma-separated CIDRs or addresses of the proxies whose X-Forwarded-For is believed, for the client address sessions, tokens and webhook limits record; a list replaces the default whole, none believes no proxy, and a bad entry fails the boot"},
	{Key: "OMNIGLASS_NATS_ADDR", Default: "127.0.0.1:4222", Scope: "server",
		Doc: "listen address (host:port) of the embedded NATS server; port 0 binds a free port, reported in the ready file"},
	{Key: "OMNIGLASS_NATS_STORE_DIR", Default: "temp dir", Scope: "server",
		Doc: "JetStream store directory"},
	{Key: "OMNIGLASS_NATS_URL", Default: "the bus's bound address", Scope: "server",
		Doc: "bus URL the node-claim exchange advertises to nodes; unset, the URL of the address the bus bound (loopback for a wildcard host such as 0.0.0.0)"},
	{Key: "OMNIGLASS_DATA_DIR", Default: ".omniglass", Scope: "server",
		Doc: "local non-Postgres server state (notably the fallback secret key)"},
	{Key: "OMNIGLASS_SETTINGS_FILE", Scope: "server",
		Doc: "optional operator settings file (JSON or YAML), the layer between code defaults and the DB override"},
	{Key: "OMNIGLASS_READY_FILE", Scope: "server",
		Doc: "path the server writes once it serves: a JSON object of the addresses it bound (addr, nats_addr) and the bus URL it advertises (nats_url), for a supervisor or test that asked for port 0; cleared at boot, removed at a graceful shutdown"},
	{Key: "OMNIGLASS_SECRET_KEY", Scope: "server",
		Doc: "base64 32-byte key-encryption key for secrets at rest; wins over the key file"},
	{Key: "OMNIGLASS_SECRET_KEY_FILE", Scope: "server",
		Doc: "path to the key-encryption key; the intended production source (the generated fallback under the data dir is convenience, not security)"},
	{Key: "OMNIGLASS_SERVER", Scope: "cli",
		Doc: "server base URL the CLI and the node use when --server is omitted, read when the command runs"},
	{Key: "OMNIGLASS_TOKEN", Scope: "cli",
		Doc: "bearer token the CLI uses when --token is omitted, read when the command runs and never printed by help"},
	{Key: "OMNIGLASS_NODE_NAME", Scope: "node",
		Doc: "the node's registered name, used by omniglass node run and sim run when --name is omitted"},
	{Key: "OMNIGLASS_NODE_TOKEN", Scope: "node",
		Doc: "the node's enrollment token, used by omniglass node run and sim run when --token is omitted, read when the command runs and never printed by help"},
	{Key: "OMNIGLASS_TEST_ADMIN_DSN", Scope: "test",
		Doc: "points the storage test harness at an existing admin database instead of a testcontainer"},
}

var registered = func() map[string]bool {
	m := make(map[string]bool, len(Registry))
	for _, v := range Registry {
		m[v.Key] = true
	}
	return m
}()

// Get reads a declared environment variable. An unregistered key panics: that
// is the point, a read that is not declared (and therefore not documented and
// not rendered into the deployment guides) is a build bug, not a runtime
// condition.
func Get(key string) string {
	if !registered[key] {
		panic(fmt.Sprintf("config: env var %s read without a Registry entry; declare it in internal/config/registry.go", key))
	}
	return os.Getenv(key)
}

// FactsJSON renders the registry as the generated env-var reference. The
// trailing newline makes the committed file POSIX-clean.
func FactsJSON() ([]byte, error) {
	doc := struct {
		Comment string `json:"//"`
		Vars    []Var  `json:"vars"`
	}{
		Comment: "generated by make gen (cmd/configgen); do not edit",
		Vars:    Registry,
	}
	out, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, fmt.Errorf("config facts: marshal: %w", err)
	}
	return append(out, '\n'), nil
}
