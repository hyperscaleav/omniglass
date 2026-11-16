package api

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
)

// TestPlaintextOperationsAreStamped keeps the credential-blind rule (#2025)
// from depending on someone remembering a stamp: the MCP generator refuses a
// tool on an operation stamped x-omniglass-plaintext, and this test makes sure
// every operation that carries a credential in plaintext has that stamp. An
// operation carries one when its gate is a reveal, or when its request or 2xx
// response body has a credential-shaped property: a token or a password, a
// secret on its way in, or a secret's fields as an object of values (the
// masked form, a list of field names, is not one).
func TestPlaintextOperationsAreStamped(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read openapi.json (run make gen): %v", err)
	}
	var spec struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]schemaShape `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}
	resolve := func(s schemaShape) schemaShape {
		if s.Ref != "" {
			return spec.Components.Schemas[strings.TrimPrefix(s.Ref, "#/components/schemas/")]
		}
		return s
	}
	var stamped, carrying []string
	for path, ops := range spec.Paths {
		for method, rawOp := range ops {
			if method == "parameters" {
				continue
			}
			var op struct {
				OperationID string          `json:"operationId"`
				Permission  string          `json:"x-omniglass-permission"`
				Plaintext   bool            `json:"x-omniglass-plaintext"`
				Stamp       json.RawMessage `json:"x-omniglass-mcp"`
				RequestBody struct {
					Content map[string]struct {
						Schema schemaShape `json:"schema"`
					} `json:"content"`
				} `json:"requestBody"`
				Responses map[string]struct {
					Content map[string]struct {
						Schema schemaShape `json:"schema"`
					} `json:"content"`
				} `json:"responses"`
			}
			if err := json.Unmarshal(rawOp, &op); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			if op.Plaintext {
				stamped = append(stamped, op.OperationID)
				if op.Stamp != nil {
					t.Errorf("%s is stamped both plaintext and as an MCP tool", op.OperationID)
				}
			}
			why := ""
			if strings.Split(op.Permission+":", ":")[1] == "reveal" {
				why = "its gate " + op.Permission + " reveals"
			}
			for _, c := range op.RequestBody.Content {
				if p := credentialProp(resolve(c.Schema), resolve, true); p != "" && why == "" {
					why = "its request carries " + p
				}
			}
			for code, r := range op.Responses {
				if !strings.HasPrefix(code, "2") {
					continue
				}
				for _, c := range r.Content {
					if p := credentialProp(resolve(c.Schema), resolve, false); p != "" && why == "" {
						why = "its response carries " + p
					}
				}
			}
			if why != "" {
				carrying = append(carrying, op.OperationID)
				if !op.Plaintext {
					t.Errorf("%s: %s in plaintext, but it is not stamped %s (Extensions: plaintext())", op.OperationID, why, tooldef.PlaintextExtension)
				}
			}
		}
	}
	sort.Strings(stamped)
	sort.Strings(carrying)
	if strings.Join(stamped, ",") != strings.Join(carrying, ",") {
		t.Errorf("stamped plaintext %v, but only %v carry a credential the rule sees; a stamp the rule cannot justify hides a tool for no reason, or the rule missed a shape", stamped, carrying)
	}
}

// schemaShape is the part of a schema the rule reads.
type schemaShape struct {
	Ref        string                 `json:"$ref"`
	Type       any                    `json:"type"`
	Properties map[string]schemaShape `json:"properties"`
}

// credentialProp names the credential-shaped property a body has, or "". A
// request's secret is plaintext on its way in; a response's secret is the
// masked form, so only a request's counts.
func credentialProp(s schemaShape, resolve func(schemaShape) schemaShape, request bool) string {
	for name, p := range s.Properties {
		p = resolve(p)
		switch {
		case name == "token" || name == "password":
			return name
		case name == "secret" && request:
			return name
		case name == "fields" && p.Type == "object":
			return "fields as values"
		}
	}
	return ""
}
