// Package tooldef holds the pure half of the MCP tool catalog (#1162, ADR-1162):
// the shape of one generated tool and the one rule that annotates it. The
// generator (cmd/mcpgen) and the server runtime (internal/mcp) both use it, so
// what the catalog promises a client and what the server serves cannot differ.
package tooldef

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Extension is the OpenAPI extension an operation carries when it is exposed as
// an MCP tool, stamped in Go at registration (internal/api) and read by the
// generator. Its value is the operation's Hints.
const Extension = "x-omniglass-mcp"

// PlaintextExtension is the OpenAPI extension an operation carries when its
// request or response holds a credential in plaintext (a minted token, a
// revealed secret's fields, a secret's value on its way in), stamped in Go at
// registration (internal/api). Such an operation is never a tool: a credential
// a human must see goes to the human through the console, not through an
// agent's context (CredentialBlind, #2025).
const PlaintextExtension = "x-omniglass-plaintext"

// CredentialBlind is the rule that keeps a plaintext credential out of an
// agent's context (#2025): an operation gated on a reveal action, or stamped
// plaintext, cannot be a tool. The generator refuses such a stamp, so the
// catalog cannot carry one however the stamps drift.
func CredentialBlind(permission string, plaintext bool) error {
	if gateAction(permission) == "reveal" {
		return fmt.Errorf("tooldef: an operation gated on %s reveals a credential, so it cannot be a tool", permission)
	}
	if plaintext {
		return fmt.Errorf("tooldef: an operation stamped %s carries a credential in plaintext, so it cannot be a tool", PlaintextExtension)
	}
	return nil
}

// Hints is what an operation's stamp may declare beyond what its method and gate
// already say. Each hint only adds caution: there is no read-only hint, so a stamp
// can never present a write as a read.
type Hints struct {
	// Destructive marks a create-shaped write (a POST) whose effect cannot simply
	// be undone, such as issuing a command to a component.
	Destructive bool `json:"destructive,omitempty"`
	// OpenWorld marks a tool that reaches outside omniglass: a node touching the
	// network, or the server fetching from the internet.
	OpenWorld bool `json:"open_world,omitempty"`
}

// ParseHints reads a stamp strictly: an unknown key, a false hint (a stamp only
// ever declares caution, so false is noise) or a value that is not an object is
// an error.
func ParseHints(raw []byte) (Hints, error) {
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil || probe == nil {
		return Hints{}, fmt.Errorf("tooldef: a %s stamp must be an object", Extension)
	}
	for k, v := range probe {
		if b, ok := v.(bool); !ok || !b {
			return Hints{}, fmt.Errorf("tooldef: stamp hint %q must be true when present", k)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var h Hints
	if err := dec.Decode(&h); err != nil {
		return Hints{}, fmt.Errorf("tooldef: %s stamp: %w", Extension, err)
	}
	return h, nil
}

// Annotations is what a tool tells an MCP client it may assume, the MCP tool
// annotations: a client gates a tool that is not read-only behind its user's
// confirmation, and warns harder on a destructive one.
type Annotations struct {
	ReadOnly    bool
	Destructive bool
	Idempotent  bool
	OpenWorld   bool
}

// Annotate is the rule (#1162). A tool is read-only when its operation is a GET
// (or HEAD), or a POST whose gate is a read (a preview or a validate, which writes
// nothing). Anything else writes: a PATCH, a PUT or a DELETE is destructive (it
// overwrites or removes), a POST is additive unless its stamp says otherwise. A
// read, a PUT and a DELETE are idempotent. OpenWorld comes only from the stamp.
// A destructive hint on a read is refused as a contradiction.
func Annotate(method, permission string, h Hints) (Annotations, error) {
	method = strings.ToUpper(method)
	var a Annotations
	switch method {
	case "GET", "HEAD":
		a.ReadOnly = true
	case "POST":
		a.ReadOnly = gateAction(permission) == "read"
	case "PATCH", "PUT", "DELETE":
		a.Destructive = true
	default:
		return Annotations{}, fmt.Errorf("tooldef: no annotation rule for method %q", method)
	}
	if h.Destructive {
		if a.ReadOnly {
			return Annotations{}, errors.New("tooldef: a destructive hint on a read-only operation is a contradiction")
		}
		a.Destructive = true
	}
	a.Idempotent = a.ReadOnly || method == "PUT" || method == "DELETE"
	a.OpenWorld = h.OpenWorld
	return a, nil
}

// Class is a tool class of the agent policy (#2063): what an operator sets an
// agent's always, ask or never for, one class per operation.
type Class string

const (
	ClassRead                  Class = "read"
	ClassWrite                 Class = "write"
	ClassDestructive           Class = "destructive"
	ClassCredentialDestination Class = "credential_destination"
)

// Classes is every class, in the order the policy lists them.
var Classes = []Class{ClassRead, ClassWrite, ClassDestructive, ClassCredentialDestination}

// Classify is the class rule (#2063). An operation any of whose permissions is
// a bind (a write that may hand a secret to a target) is a credential
// destination, whatever else it does: it is the class an operator most wants to
// hold apart. Otherwise a read-only operation is a read, a destructive one
// destructive, and the rest a write. permissions are the route's gate and any
// second gate it enforces on some requests, so the class is the most sensitive
// thing the operation can do.
func Classify(a Annotations, permissions ...string) Class {
	for _, p := range permissions {
		if gateAction(p) == "bind" {
			return ClassCredentialDestination
		}
	}
	switch {
	case a.ReadOnly:
		return ClassRead
	case a.Destructive:
		return ClassDestructive
	}
	return ClassWrite
}

// gateAction is a permission's action token, the second of `resource:action` or
// `resource:action:admin`; empty for an authn-only route.
func gateAction(permission string) string {
	parts := strings.Split(permission, ":")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// Def is one generated tool: an API operation as the MCP server serves it. The
// generator writes the catalog of these from api/openapi.json; the runtime builds
// each request from it.
type Def struct {
	// Name is the operation's id, which is also the tool's name.
	Name string
	// Title is the operation's summary; Description its description, which names
	// its gate.
	Title       string
	Description string
	// Method and Path are the operation's route, Path relative to /api/v1 with
	// its {placeholders}.
	Method string
	Path   string
	// Permission is the gate the route checks (x-omniglass-permission), empty for
	// an authn-only route. The route enforces it; the catalog only reports it.
	Permission string
	// PathParams, QueryParams and BodyParams sort a call's arguments onto the
	// request: each argument belongs to exactly one, which the generator checks.
	PathParams  []string
	QueryParams []string
	BodyParams  []string
	// BodyWhole is set when the body is not an object of named properties (a
	// free-form merge patch): its one argument, body, is sent as the whole body.
	BodyWhole   bool
	Annotations Annotations
	// Class is the operation's agent-policy class (Classify over its
	// annotations, its gate and any second gate), which the install's agent
	// policy is resolved against for the tool's _meta (#2063).
	Class Class
	// InputSchema is the tool's JSON Schema, every argument in one object.
	InputSchema string
}
