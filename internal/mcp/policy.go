package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// agentPolicyMeta is the _meta key a listed tool carries its agent policy
// under (#2063): its class and what the install resolves for that class.
const agentPolicyMeta = "omniglass/agent_policy"

// toolClass is each catalog tool's agent-policy class, by name.
var toolClass = func() map[string]tooldef.Class {
	m := make(map[string]tooldef.Class, len(catalog))
	for _, d := range catalog {
		m[d.Name] = d.Class
	}
	return m
}()

// withAgentPolicy marks every tool in a tools/list answer with the install's
// agent policy for it, read from the API with the caller's own token over the
// MCP channel, so a client can hide a never tool and confirm an ask one before
// calling it. The server refuses a never call either way; a policy the API
// will not answer leaves the list unmarked rather than failing it.
func withAgentPolicy(api http.Handler) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil || method != "tools/list" {
				return res, err
			}
			list, ok := res.(*sdk.ListToolsResult)
			if !ok {
				return res, nil
			}
			var authorization string
			if extra := req.GetExtra(); extra != nil && extra.Header != nil {
				authorization = extra.Header.Get("Authorization")
			}
			policy, err := readAgentPolicy(ctx, api, authorization)
			if err != nil {
				slog.Warn("mcp: tools listed without their agent policy", "err", err)
				return res, nil
			}
			return markTools(list, policy), nil
		}
	}
}

// agentPolicy is the caller's resolved agent policy: one per class, and one
// per tool the install overrides (#2088).
type agentPolicy struct {
	classes map[tooldef.Class]string
	tools   map[string]string
}

// of is a tool's policy: its own override's resolution, else its class's.
func (p agentPolicy) of(tool string, class tooldef.Class) string {
	if v, ok := p.tools[tool]; ok {
		return v
	}
	return p.classes[class]
}

// markTools is list with each catalog tool's _meta carrying its class and its
// resolved policy. The tools are copied: the listed ones are the server's own
// and shared by every session.
func markTools(list *sdk.ListToolsResult, policy agentPolicy) *sdk.ListToolsResult {
	out := *list
	out.Tools = make([]*sdk.Tool, len(list.Tools))
	for i, t := range list.Tools {
		class, ok := toolClass[t.Name]
		if !ok {
			out.Tools[i] = t
			continue
		}
		cp := *t
		cp.Meta = sdk.Meta{}
		for k, v := range t.Meta {
			cp.Meta[k] = v
		}
		cp.Meta[agentPolicyMeta] = map[string]string{"class": string(class), "policy": policy.of(t.Name, class)}
		out.Tools[i] = &cp
	}
	return &out
}

// readAgentPolicy is the caller's resolved agent policy, as the API answers
// GET /auth/me/agent-policy over the MCP channel.
func readAgentPolicy(ctx context.Context, api http.Handler, authorization string) (agentPolicy, error) {
	dctx, release := detached(ctx)
	defer release()
	httpReq, err := http.NewRequestWithContext(storage.WithAuditChannel(dctx, storage.AuditChannelMCP, ""), http.MethodGet, apiBase+"/auth/me/agent-policy", nil)
	if err != nil {
		return agentPolicy{}, err
	}
	httpReq.Header.Set("Authorization", authorization)
	httpReq.Header.Set("Accept", "application/json")
	rec := newRecorder()
	api.ServeHTTP(rec, httpReq)
	if rec.status != http.StatusOK {
		return agentPolicy{}, &policyStatusError{rec.status}
	}
	var body struct {
		Classes []struct {
			Class  tooldef.Class `json:"class"`
			Policy string        `json:"policy"`
		} `json:"classes"`
		Tools []struct {
			Tool   string `json:"tool"`
			Policy string `json:"policy"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rec.body.Bytes(), &body); err != nil {
		return agentPolicy{}, err
	}
	policy := agentPolicy{classes: make(map[tooldef.Class]string, len(body.Classes)), tools: make(map[string]string, len(body.Tools))}
	for _, c := range body.Classes {
		policy.classes[c.Class] = c.Policy
	}
	for _, t := range body.Tools {
		policy.tools[t.Tool] = t.Policy
	}
	return policy, nil
}

type policyStatusError struct{ status int }

func (e *policyStatusError) Error() string {
	return "the agent policy answered " + http.StatusText(e.status)
}
