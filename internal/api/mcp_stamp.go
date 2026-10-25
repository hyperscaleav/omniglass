package api

import "github.com/hyperscaleav/omniglass/internal/mcp/tooldef"

// mcpExposed is the stamp that makes an operation a tool on the MCP server
// (#1162, ADR-1162), set as the operation's Extensions at registration so what
// is exposed is reviewed in the same diff as the route:
//
//	huma.Operation{OperationID: "list-components", Extensions: mcpExposed(), ...}
//
// The extension (tooldef.Extension) carries the operation's hints into
// api/openapi.json, where cmd/mcpgen reads it and generates the tool. A hint only
// adds caution; whether the tool is read-only comes from the method and the gate
// (tooldef.Annotate), never from the stamp. gated(...) and its siblings add their
// own keys to the same map.
func mcpExposed(hints ...tooldef.Hints) map[string]any {
	var h tooldef.Hints
	for _, x := range hints {
		h.Destructive = h.Destructive || x.Destructive
		h.OpenWorld = h.OpenWorld || x.OpenWorld
	}
	return map[string]any{tooldef.Extension: h}
}
