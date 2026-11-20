package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	// container/Helm guides), "outpost" (the edge process), "cli" (the operator CLI,
	// surfaced through the generated CLI reference instead of the server tables),
	// or "test" (test harness only).
	Scope string `json:"scope"`
	// Doc is the one-line operator description.
	Doc string `json:"doc"`
	// Deprecated names the variable that replaces this one, for an old name the
	// binary still reads through a deprecation window and warns about on every
	// read. Empty for a current variable. The reference renders it, so the
	// deployment guides teach the rename from the same declaration.
	Deprecated string `json:"deprecated,omitempty"`
	// Family, when set, names the prefix family a variable belongs to (#2223):
	// a row generated for a key under OMNIGLASS_SETTING__ carries the family the operator reads about. Hand-declared Vars leave
	// this empty.
	Family string `json:"family,omitempty"`
}

// Family declares a prefix a variable may share, when the variable's full
// name is not known at build time: the OMNIGLASS_SETTING__ layer, whose keys
// come from the reflected Settings struct. config.Get accepts an exact Registry key
// or a name under a declared family prefix, so a read a generator would make
// does not panic as "undeclared" even though no single Registry row names it.
type Family struct {
	Prefix string `json:"prefix"`
	Scope  string `json:"scope"`
	Doc    string `json:"doc"`
}

// Families declares the prefix variables omniglass reads (#2223). The env
// settings layer (OMNIGLASS_SETTING__*) is expanded per-key in the generated
// reference; the env-backed platform secrets (OMNIGLASS_SECRET__*) render as
// one family row, since their names are the operator's.
var Families = []Family{
	{Prefix: "OMNIGLASS_SETTING__", Scope: "server",
		Doc: "sets one platform setting from the environment (OMNIGLASS_SETTING__<NAMESPACE>__<KEY>=value); the env layer sits between the settings file and the platform binding, so a console edit still wins. See the generated per-key rows"},
	{Prefix: "OMNIGLASS_SECRET__", Scope: "server",
		Doc: `declares a platform secret from the environment, named <NAME> lowercased with each underscore a hyphen (OMNIGLASS_SECRET__BEDROCK_KEY is bedrock-key); the value is a JSON object {"type": "<secret type>", "fields": {...}} checked against the type at boot. Held in memory, never sealed or written; listed with source env and changed only by its variable and a restart, and a stored platform secret of the same name fails the boot`},
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
		Doc: "bus URL the outpost-claim exchange advertises to outposts; unset, the URL of the address the bus bound (loopback for a wildcard host such as 0.0.0.0)"},
	{Key: "OMNIGLASS_DATA_DIR", Default: ".omniglass", Scope: "server",
		Doc: "local non-Postgres server state (notably the fallback encryption key)"},
	{Key: "OMNIGLASS_SETTINGS_FILE", Scope: "server",
		Doc: "optional operator settings file (JSON or YAML), the layer between code defaults and the DB override"},
	{Key: "OMNIGLASS_READY_FILE", Scope: "server",
		Doc: "path the server writes once it serves: a JSON object of the addresses it bound (addr, nats_addr) and the bus URL it advertises (nats_url), for a supervisor or test that asked for port 0; cleared at boot, removed at a graceful shutdown"},
	{Key: "OMNIGLASS_OMNIBOT_REPLAY", Scope: "server",
		Doc: "path to a recorded model the server's omnibot plays, turn by turn as each conversation stands, for a demo or a test; unset, the server has no model and a run is refused"},
	{Key: "OMNIGLASS_ENCRYPTION_KEY", Scope: "server",
		Doc: "base64 32-byte encryption key, the key-encryption key every secret is sealed under at rest; wins over the key file"},
	{Key: "OMNIGLASS_ENCRYPTION_KEY_FILE", Scope: "server",
		Doc: "path to a file holding the encryption key; the intended production source (the generated fallback under the data dir is convenience, not security)"},
	// The encryption key's names before #558, read only while neither new name
	// is set, with a warning on every boot (internal/secret.LoadKEK).
	{Key: "OMNIGLASS_SECRET_KEY", Scope: "server", Deprecated: "OMNIGLASS_ENCRYPTION_KEY",
		Doc: "the encryption key's old name, read only when neither OMNIGLASS_ENCRYPTION_KEY nor OMNIGLASS_ENCRYPTION_KEY_FILE is set; warns on every boot, removed in a later release"},
	{Key: "OMNIGLASS_SECRET_KEY_FILE", Scope: "server", Deprecated: "OMNIGLASS_ENCRYPTION_KEY_FILE",
		Doc: "the encryption key file's old name, read only when neither OMNIGLASS_ENCRYPTION_KEY nor OMNIGLASS_ENCRYPTION_KEY_FILE is set; warns on every boot, removed in a later release"},
	{Key: "OMNIGLASS_SERVER_NAME", Default: "the hostname", Scope: "server",
		Doc: "the server's identity: the name its own platform component, which carries its request and query timing, is ensured under at boot; set it when servers share a hostname or a server moves hosts"},
	// The closed loop on Platform health (#1647, ADR-1647): how often the
	// server brackets each outpost with a simulator, and the cadence it assumes
	// for those outposts, which it is not told. omniglass lab run sets all three
	// from its cadence profile.
	{Key: "OMNIGLASS_CLOSED_LOOP_BRACKET", Default: "1m", Scope: "server",
		Doc: "how often the server brackets each outpost with a connected simulator for Platform health's sliding window, ten brackets long; 1s to 1m, and a value outside that fails the boot"},
	{Key: "OMNIGLASS_CLOSED_LOOP_HEARTBEAT", Default: "30s", Scope: "server",
		Doc: "the check-in interval the closed loop assumes for each outpost with a simulator, to count which poll targets were due over its window; set it to the outposts' --heartbeat"},
	{Key: "OMNIGLASS_CLOSED_LOOP_PROBE_EVERY", Default: "every check-in", Scope: "server",
		Doc: "the reachability-probe interval the closed loop assumes for each outpost with a simulator; set it to the outposts' --probe-every"},
	// The ingest lanes (#1896, ADR-1896): the telemetry stream files each
	// outpost's batches under a partition of its name, one lane a partition.
	{Key: "OMNIGLASS_INGEST_PARTITIONS", Default: "4", Scope: "server",
		Doc: "how many ingest lanes land outpost telemetry side by side, each outpost's batches always on one lane and in the order it sent them; 1 to 64, held under the database pool less one, and a value outside that range fails the boot; change it with the telemetry backlog drained, or an outpost's batches held across the change may land out of order"},
	{Key: "OMNIGLASS_SERVER", Scope: "cli",
		Doc: "server base URL the CLI and the outpost use when --server is omitted, read when the command runs"},
	{Key: "OMNIGLASS_TOKEN", Scope: "cli",
		Doc: "bearer token the CLI uses when --token is omitted, read when the command runs and never printed by help"},
	{Key: "OMNIGLASS_OUTPOST_NAME", Scope: "outpost",
		Doc: "the outpost's registered name, used by omniglass outpost run and sim run when --name is omitted"},
	{Key: "OMNIGLASS_OUTPOST_TOKEN", Scope: "outpost",
		Doc: "the outpost's enrollment token, used by omniglass outpost run and sim run when --token is omitted, read when the command runs and never printed by help"},
	{Key: "OMNIGLASS_OUTPOST_SESSION_DIALS", Default: "16", Scope: "outpost",
		Doc: "how many held sessions omniglass outpost run lets make their first dial and handshake at once when --session-dials is omitted (a redial takes no slot); a session waiting for a slot reads establishing"},
	{Key: "OMNIGLASS_LANE_WORKERS", Default: "auto", Scope: "outpost",
		Doc: "how many endpoint lanes omniglass outpost run runs at once, each sending one request to its endpoint at a time, when --lane-workers is omitted; auto sizes them from the CPU and memory the outpost may use"},
	// omniglass lab run finds its Docker daemon the way the docker CLI does
	// (#1194): these three, then the platform's default socket or pipe.
	{Key: "DOCKER_HOST", Scope: "cli",
		Doc: "the Docker daemon omniglass lab run drives, ahead of any docker context; a daemon on another machine is refused"},
	{Key: "DOCKER_CONTEXT", Scope: "cli",
		Doc: "the docker context omniglass lab run uses when DOCKER_HOST is unset, ahead of the docker CLI's current context"},
	{Key: "DOCKER_CONFIG", Default: "~/.docker", Scope: "cli",
		Doc: "the docker CLI's config directory, where omniglass lab run reads the current context and the context store"},
	{Key: "OMNIGLASS_LAB_IMAGE", Scope: "cli",
		Doc: "the omniglass image omniglass lab run runs when --image is omitted; one that is not the binary's own build is warned about"},
	{Key: "OMNIGLASS_LAB_OUTPOST_TOKEN_HASH", Scope: "cli",
		Doc: "set by omniglass lab run for its seed one-off: the hash of the outpost token it minted for this run, never set by hand"},
	{Key: "OMNIGLASS_LAB_SITE_OUTPOST_TOKEN_HASHES", Scope: "cli",
		Doc: "set by omniglass lab run for a scale lab's seed one-off: each site outpost's name and the hash of the token it minted for it this run (outpost=hash, comma-separated), never set by hand"},
	{Key: "OMNIGLASS_LAB_PUSHER_TOKENS", Scope: "cli",
		Doc: "set by omniglass lab run for its seed one-off: each declared pusher's name and the hash and non-secret prefix of the token it minted for it this run (name=hash:prefix, comma-separated), never set by hand"},
	{Key: "OMNIGLASS_LAB_OMNIBOT_KEY", Scope: "cli",
		Doc: "set by omniglass lab run --omnibot-key-env for its seed one-off: the model key the seed seals as the lab's omnibot key secret, never set by hand and never printed"},
	{Key: "OMNIGLASS_SIM_PUSHERS", Scope: "cli",
		Doc: "the outside actors omniglass outpost run --simulate runs as emulator devices with a push face: each pusher's name, profile and token (name=profile:token, comma-separated); set by omniglass lab run, read when the command runs and never printed by help"},
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
// condition. A name under a declared family prefix (OMNIGLASS_SETTING__)
// counts as declared, since the family covers it even
// though no Registry row names it individually.
func Get(key string) string {
	if !declared(key) {
		panic(fmt.Sprintf("config: env var %s read without a Registry entry; declare it in internal/config/registry.go", key))
	}
	return os.Getenv(key)
}

// declared reports whether key is covered by the Registry: an exact entry,
// or a name that falls under a declared family prefix.
func declared(key string) bool {
	if registered[key] {
		return true
	}
	for _, f := range Families {
		if strings.HasPrefix(key, f.Prefix) && len(key) > len(f.Prefix) {
			return true
		}
	}
	return false
}

// Named reads a variable the operator names on the command line, as
// omniglass lab run --omnibot-key-env does (#2109): its name is the operator's
// choice, so it has no Registry entry, and the flag that names it is the
// documentation. Every other read goes through Get.
func Named(key string) string {
	return os.Getenv(key)
}

// Environ is the process environment's KEY=VALUE entries under one declared
// family prefix (#2223), for a reader that takes a whole family at once: the
// server's env settings layer, and the lab forwarding it to its server. A
// prefix no family declares panics, as Get does for an undeclared key.
func Environ(prefix string) []string {
	ok := false
	for _, f := range Families {
		ok = ok || f.Prefix == prefix
	}
	if !ok {
		panic(fmt.Sprintf("config: env family %s read without a Families entry; declare it in internal/config/registry.go", prefix))
	}
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// FactsJSON renders the registry as the generated env-var reference. Each
// hand-declared Var is emitted as itself; each declared Family is either
// expanded per-key (OMNIGLASS_SETTING__, driven by the Settings struct, one
// row per platform-domain leaf) or emitted as a single family entry. The trailing
// newline makes the committed file POSIX-clean.
func FactsJSON() ([]byte, error) {
	vars := append([]Var{}, Registry...)
	vars = append(vars, familyFacts()...)
	doc := struct {
		Comment string `json:"//"`
		Vars    []Var  `json:"vars"`
	}{
		Comment: "generated by make gen (cmd/configgen); do not edit",
		Vars:    vars,
	}
	out, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, fmt.Errorf("config facts: marshal: %w", err)
	}
	return append(out, '\n'), nil
}

// SettingsEnvFacts is the generator seam the settings package fills on
// init: reflecting the Settings struct lives there (it is the only place
// that owns the struct), and configgen calls FactsJSON here, so this
// package reaches the facts through a function variable rather than
// importing settings. A nil seam renders no per-key rows (a tool with the
// settings package compiled out still gets the family entry).
var SettingsEnvFacts func() []SettingsEnvFact

// SettingsEnvFact is one settable env key the reference carries: the full
// variable name, the key's default string, and the key's doc.
type SettingsEnvFact struct {
	Key     string
	Default string
	Doc     string
}

// familyFacts expands the declared families (#2223): OMNIGLASS_SETTING__ one
// row per settable key off the Settings struct (through the SettingsEnvFacts
// seam); any other family a single row, its name the operator's.
func familyFacts() []Var {
	var out []Var
	for _, f := range Families {
		switch f.Prefix {
		case "OMNIGLASS_SETTING__":
			if SettingsEnvFacts == nil {
				continue
			}
			for _, k := range SettingsEnvFacts() {
				out = append(out, Var{
					Key:     k.Key,
					Default: k.Default,
					Scope:   f.Scope,
					Doc:     k.Doc,
					Family:  f.Prefix,
				})
			}
		default:
			out = append(out, Var{
				Key:    f.Prefix + "<NAME>",
				Scope:  f.Scope,
				Doc:    f.Doc,
				Family: f.Prefix,
			})
		}
	}
	return out
}
