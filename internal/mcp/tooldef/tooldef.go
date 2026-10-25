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
	// InputSchema is the tool's JSON Schema, every argument in one object.
	InputSchema string
}
