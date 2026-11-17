package api

import (
	"context"
	"net/http"

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
// operation to a request under the agent ceiling, and the class it judged.
func agentPolicyRefuses(p settings.AgentSettings, op *huma.Operation) (tooldef.Class, bool) {
	if agentPolicyExemptOps[op.OperationID] {
		return "", false
	}
	class := operationClass(op)
	return class, p.Policy(string(class)) == settings.AgentNever
}

// enforceAgentPolicy refuses a ceilinged request that would rewrite the policy,
// or whose operation's class the install sets to never, writing an
// agent_refused audit row that names the operation (and, through ctx, the
// agent, channel and client). It reports whether the request may go on; on
// false the response is already written.
func (a *authenticator) enforceAgentPolicy(ctx huma.Context, c context.Context, actorID string) bool {
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
	class, refused := agentPolicyRefuses(p, op)
	if !refused {
		return true
	}
	_ = a.gw.WriteAuthEventFor(c, actorID, "agent_refused", "operation", op.OperationID)
	_ = huma.WriteErr(a.api, ctx, http.StatusForbidden, "this install does not let an agent call a "+string(class)+" operation; its person can")
	return false
}

type agentClassPolicy struct {
	Class  string `json:"class" enum:"read,write,destructive,credential_destination" doc:"The operation class"`
	Policy string `json:"policy" enum:"always,ask,never" doc:"always runs; ask runs, and the client confirms with its person first; never is refused by the server"`
}

type agentPolicyOutput struct {
	Body struct {
		Applies bool               `json:"applies" doc:"Whether this request is an agent's (an agent token, or any request over MCP), so the policy governs it"`
		Classes []agentClassPolicy `json:"classes" doc:"The install's resolved policy for each operation class"`
	}
}

// agentPolicyHandler answers the caller's resolved agent policy (#2063): one
// policy per class, and whether the request itself is held to it.
func (a *authenticator) agentPolicyHandler(ctx context.Context, _ *struct{}) (*agentPolicyOutput, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	p, err := a.gw.AgentSettings(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("reading the agent policy failed")
	}
	out := &agentPolicyOutput{}
	out.Body.Applies = agentCeiling(pr, storage.AuditChannelOf(ctx))
	for _, c := range tooldef.Classes {
		out.Body.Classes = append(out.Body.Classes, agentClassPolicy{Class: string(c), Policy: p.Policy(string(c))})
	}
	return out, nil
}
