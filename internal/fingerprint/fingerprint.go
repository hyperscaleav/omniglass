// Package fingerprint is the connection fingerprint (#1056 ruling, ADR-0158):
// one deterministic digest over every input a connection to an endpoint is
// made with, held or stateless. A refusal retrying cannot fix gates the
// endpoint at its ladder rung for the fingerprint it happened under; the
// gate lifts only when the fingerprint changes (a rotated secret, a new
// address, port or transport, a changed login declaration or pin) or an
// operator's :retry moves the counter that is part of it. A leaf, pure: the
// storage gateway computes it for the worklist, and the node only compares.
package fingerprint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// Inputs is every input a connection to an endpoint is made with. It is the
// one declaration of them: a field added here is hashed (every field is, by
// reflection test), and the worklist's table test holds every field a node
// reads to connect to one of these.
type Inputs struct {
	// Transport is the wire (tcp, telnet, ssh, ...).
	Transport string `json:"transport"`
	// Params is the endpoint's declared address settings: the target as
	// declared (host and port, never a DNS result) and the timeout.
	Params json.RawMessage `json:"params"`
	// DriverInputs is an attachment's inputs: host, port, and secret
	// reference names (never values).
	DriverInputs json.RawMessage `json:"driver_inputs"`
	// SecretVersions maps each secret input to a digest of its sealed
	// envelope: a rotation re-seals, so it moves the fingerprint, and no
	// plaintext-derived material is ever hashed.
	SecretVersions map[string]string `json:"secret_versions"`
	// Session is the driver's session block: the login and terminal
	// declarations, the credential input, the channel, the re-probe.
	Session *driver.SessionSpec `json:"session"`
	// Ladder is the resolved reachability ladder: what the transport rung
	// is (a connect, a handshake, a named request) and whether a session
	// rung exists.
	Ladder *driver.EffectiveLadder `json:"ladder"`
	// HostKeyPin is the ssh host key the endpoint pins.
	HostKeyPin string `json:"host_key_pin"`
	// Retry is the operator's :retry counter.
	Retry int64 `json:"retry"`
}

// Of digests the inputs: SHA-256 over their canonical JSON (map keys sorted,
// raw JSON compacted and key-sorted), hex. Never logged.
func Of(in Inputs) string {
	in.Params = canonical(in.Params)
	in.DriverInputs = canonical(in.DriverInputs)
	b, _ := json.Marshal(in)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// canonical re-encodes a JSON document with sorted keys, so two spellings of
// one document are one input; an unparsable one is kept as is.
func canonical(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	b, _ := json.Marshal(v)
	return b
}
