package api

import (
	"context"
	"maps"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// agentPolicyExemptOps answer whatever the policy says: reading who you are
// (the MCP server's bearer probe) and reading the policy itself, so an agent
// refused every read can still learn why.
var agentPolicyExemptOps = map[string]bool{
	"get-auth-me":              true,
	"get-auth-me-agent-policy": true,
}

// rewritesAgentPolicy reports whether an operation would change the policy
// that binds agents: patching or dropping the agent namespace, or the factory
// reset that drops it with every other. No agent may, whatever the classes
// say, or the policy binds only the agents that leave it alone.
func rewritesAgentPolicy(opID, namespace string) bool {
	switch opID {
	case "patch-settings-namespace", "delete-settings-namespace":
		return namespace == "agent"
	case "restore-settings-defaults":
		return true
	}
	return false
}

// operationClass is the agent-policy class of a registered operation (#2063):
// the MCP annotation rule over its method, gate and stamp (tooldef.Annotate),
// then the class rule over its gate and any second gate (tooldef.Classify).
// An operation the rule cannot annotate is held to the strictest non-bind
// class, destructive.
func operationClass(op *huma.Operation) tooldef.Class {
	perm, _ := op.Extensions["x-omniglass-permission"].(string)
	cond, _ := op.Extensions["x-omniglass-conditional-permission"].(string)
	hints, _ := op.Extensions[tooldef.Extension].(tooldef.Hints)
	a, err := tooldef.Annotate(op.Method, perm, hints)
	if err != nil {
		a = tooldef.Annotations{Destructive: true}
	}
	return tooldef.Classify(a, perm, cond)
}

// agentPolicyRefuses reports whether the install's agent policy refuses the
// operation to a request under the agent ceiling from a person granted roles,
// and the decision it judged: the class's policy narrowed by the person's
// roles, then by the operation's own override (#2088). The roles are every one
// the person is granted, so an agent token narrowed to fewer roles is still
// held to its person's overrides.
func agentPolicyRefuses(p settings.AgentSettings, op *huma.Operation, roles []string) (tooldef.Class, settings.AgentDecision, bool) {
	if agentPolicyExemptOps[op.OperationID] {
		return "", settings.AgentDecision{}, false
	}
	class := operationClass(op)
	d := p.ResolveTool(op.OperationID, string(class), roles)
	return class, d, d.Policy == settings.AgentNever
}

// refusalDetail is the 403 detail for a refused operation, naming the layer
// that refused it.
func refusalDetail(opID string, class tooldef.Class, d settings.AgentDecision) string {
	switch d.Source {
	case settings.AgentSourceTool:
		return "this install does not let an agent call " + opID + "; its person can"
	case settings.AgentSourceRole:
		return "this install does not let an agent of the " + d.Role + " role call a " + string(class) + " operation; its person can"
	}
	return "this install does not let an agent call a " + string(class) + " operation; its person can"
}

// enforceAgentPolicy refuses a ceilinged request that would rewrite the policy,
// or whose operation the install resolves to never for the person's roles,
// writing an agent_refused audit row that names the operation (and, through
// ctx, the agent, channel and client). It reports whether the request may go
// on; on false the response is already written.
func (a *authenticator) enforceAgentPolicy(ctx huma.Context, c context.Context, actorID string, roles []string) bool {
	op := ctx.Operation()
	if agentPolicyExemptOps[op.OperationID] {
		return true
	}
	if rewritesAgentPolicy(op.OperationID, ctx.Param("namespace")) {
		_ = a.gw.WriteAuthEventFor(c, actorID, "agent_refused", "operation", op.OperationID)
		_ = huma.WriteErr(a.api, ctx, http.StatusForbidden, "an agent cannot change the agent policy; its person can")
		return false
	}
	p, err := a.gw.AgentSettings(c)
	if err != nil {
		_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "authentication failed")
		return false
	}
	class, d, refused := agentPolicyRefuses(p, op, roles)
	if !refused {
		return true
	}
	_ = a.gw.WriteAuthEventFor(c, actorID, "agent_refused", "operation", op.OperationID)
	_ = huma.WriteErr(a.api, ctx, http.StatusForbidden, refusalDetail(op.OperationID, class, d))
	return false
}

// operations is every registered operation by ID.
func (a *authenticator) operations() map[string]*huma.Operation {
	a.opsOnce.Do(func() {
		a.ops = map[string]*huma.Operation{}
		for _, item := range a.api.OpenAPI().Paths {
			for _, op := range []*huma.Operation{item.Get, item.Put, item.Post, item.Delete, item.Options, item.Head, item.Patch, item.Trace} {
				if op != nil {
					a.ops[op.OperationID] = op
				}
			}
		}
	})
	return a.ops
}

// checkAgentOverrides refuses an agent-namespace patch whose override names a
// role the install does not have or a tool the API does not register, so a
// typo cannot leave the role or tool it meant unrestricted. A null (a delete)
// may name anything, so a stale override can always be dropped.
func (a *authenticator) checkAgentOverrides(ctx context.Context, patch map[string]any) error {
	if roles, ok := patch["roles"].(map[string]any); ok {
		idx, err := a.roleIndex(ctx)
		if err != nil {
			return err
		}
		for r, v := range roles {
			if _, known := idx[r]; v != nil && !known {
				return huma.Error422UnprocessableEntity("settings: agent.roles: unknown role " + r)
			}
		}
	}
	if tools, ok := patch["tools"].(map[string]any); ok {
		ops := a.operations()
		for t, v := range tools {
			if v == nil {
				continue
			}
			if _, known := ops[t]; !known {
				return huma.Error422UnprocessableEntity("settings: agent.tools: unknown tool " + t)
			}
			if agentPolicyExemptOps[t] {
				return huma.Error422UnprocessableEntity("settings: agent.tools: " + t + " answers every agent, so it takes no override")
			}
		}
	}
	return nil
}

type agentClassPolicy struct {
	Class  string `json:"class" enum:"read,write,destructive,credential_destination" doc:"The operation class"`
	Policy string `json:"policy" enum:"always,ask,never" doc:"always runs; ask runs, and the client confirms with its person first; never is refused by the server"`
	Source string `json:"source" enum:"platform,role,tool" doc:"The layer that set the policy: the platform's class value, a role's override, or the tool's own"`
	Role   string `json:"role,omitempty" doc:"The role whose override set the policy, when the source is role"`
}

type agentToolPolicy struct {
	Tool string `json:"tool" doc:"The tool (operation ID) an override names"`
	agentClassPolicy
}

type agentPolicyOutput struct {
	Body struct {
		Applies bool               `json:"applies" doc:"Whether this request is an agent's (an agent token, or any request over MCP), so the policy governs it"`
		Classes []agentClassPolicy `json:"classes" doc:"The resolved policy for each operation class, for the caller's roles"`
		Tools   []agentToolPolicy  `json:"tools" doc:"The resolved policy for each tool the install overrides; every other tool takes its class's"`
	}
}

// agentPolicyHandler answers the caller's resolved agent policy (#2063,
// #2088): one policy per class for the caller's roles, one per overridden
// tool, each naming the layer that set it, and whether the request itself is
// held to it.
func (a *authenticator) agentPolicyHandler(ctx context.Context, _ *struct{}) (*agentPolicyOutput, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	p, err := a.gw.AgentSettings(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("reading the agent policy failed")
	}
	roles := make([]string, 0, len(pr.Grants))
	for _, g := range pr.Grants {
		roles = append(roles, g.Role)
	}
	out := &agentPolicyOutput{}
	out.Body.Applies = agentCeiling(pr, storage.AuditChannelOf(ctx))
	out.Body.Tools = []agentToolPolicy{}
	for _, c := range tooldef.Classes {
		out.Body.Classes = append(out.Body.Classes, classPolicy(c, p.ResolveClass(string(c), roles)))
	}
	ops := a.operations()
	for _, t := range slices.Sorted(maps.Keys(p.Tools)) {
		op, ok := ops[t]
		if !ok || agentPolicyExemptOps[t] {
			continue
		}
		class := operationClass(op)
		out.Body.Tools = append(out.Body.Tools, agentToolPolicy{Tool: t, agentClassPolicy: classPolicy(class, p.ResolveTool(t, string(class), roles))})
	}
	return out, nil
}

func classPolicy(c tooldef.Class, d settings.AgentDecision) agentClassPolicy {
	return agentClassPolicy{Class: string(c), Policy: d.Policy, Source: d.Source, Role: d.Role}
}
