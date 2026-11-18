package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
)

// Local is the MCP catalog served in-process, for a run the server drives
// itself (#2121): the same tools the MCP endpoint lists, marked with the
// caller's agent policy, and the same API request per call, carrying only the
// Authorization it was made with. The credential never reaches the model; the
// API settles who the call is, its channel and its audit row, exactly as it
// does for a remote agent.
type Local struct {
	api           http.Handler
	authorization string
}

// NewLocal serves the catalog through api as the caller authorization names
// (an Authorization header value).
func NewLocal(api http.Handler, authorization string) *Local {
	return &Local{api: api, authorization: authorization}
}

// List is the catalog marked with the caller's agent policy. Unlike the MCP
// endpoint, which lists unmarked when the policy will not answer, it fails:
// the run it serves has no person at hand to catch a never tool.
func (l *Local) List(ctx context.Context) ([]*sdk.Tool, error) {
	policy, err := readAgentPolicy(ctx, l.api, l.authorization)
	if err != nil {
		return nil, fmt.Errorf("read the agent policy: %w", err)
	}
	list := &sdk.ListToolsResult{Tools: make([]*sdk.Tool, len(catalog))}
	for i, d := range catalog {
		list.Tools[i] = toolFor(d)
	}
	return markTools(list, policy).Tools, nil
}

// Call runs the named tool with args.
func (l *Local) Call(ctx context.Context, name string, args map[string]any) (*sdk.CallToolResult, error) {
	d, ok := catalogDef(name)
	if !ok {
		return nil, fmt.Errorf("no tool %q", name)
	}
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return call(ctx, l.api, d, raw, l.authorization, ""), nil
}

func catalogDef(name string) (tooldef.Def, bool) {
	for _, d := range catalog {
		if d.Name == name {
			return d, true
		}
	}
	return tooldef.Def{}, false
}
