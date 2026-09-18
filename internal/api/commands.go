package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The command surface: the "do" primitive's write. Issuing a command records the
// invocation, writes a caused event, and (for a settleable command) opens an intended
// value in the property cache that the target's observed value settles against. It is
// an AIP custom method (:issue), gated by command:issue and scope-injected through the
// component. Settlement is computed and returned, never stored.
//
// The fence is the ISSUE scope, not the read scope (#749). This route actuates a
// physical device, so the set that decides which components it reaches has to come
// from the permission that authorizes the actuation: a principal holding a wide
// component read beside a room-scoped command grant may command that room and no
// more. The read scope is still passed, and still grants nothing: it decides only
// which refusal the caller is owed (ADR-0116).

type issueCommandInput struct {
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	Body struct {
		CommandType string          `json:"command_type" minLength:"1" doc:"The command_type to invoke"`
		Instance    string          `json:"instance,omitempty" doc:"The series discriminator (e.g. an endpoint), when the target is instanced"`
		Value       json.RawMessage `json:"value,omitempty" doc:"The intended value for the target property (a settleable command)"`
		Params      json.RawMessage `json:"params,omitempty" doc:"The invocation params, stored on the command and the caused event"`
	}
}

type commandOutput struct {
	Body struct {
		ID            int64  `json:"id"`
		CommandType   string `json:"command_type"`
		Instance      string `json:"instance,omitempty"`
		CausedEventID int64  `json:"caused_event_id" doc:"The caused event this command recorded"`
		Settlement    string `json:"settlement" doc:"The computed settlement verdict (none/pending/settled/failed)"`
		Status        string `json:"status" doc:"The recorded command status (issued/settled/failed/timed-out); issued until a settle-check records the terminal outcome"`
	}
}

// registerCommandRoutes wires the command issue custom method, the operator-facing
// write over a component's command_types.
func registerCommandRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "issue-component-command",
		Method:      http.MethodPost,
		Path:        "/components/{name}/commands:issue",
		Summary:     "Issue a command to a component",
		Description: "Records a command invocation, writes a caused event, and (for a settleable command) opens an intended value the observed value settles against. Returns the computed settlement verdict. Gated by command:issue, whose scope is resolved on the component tier from that permission (not from component:read); a component outside the caller's component:read is a non-disclosing 404, and one it can read but not command is a 403.",
	}, "command", "issue"), func(ctx context.Context, in *issueCommandInput) (*commandOutput, error) {
		// The ACTION scope comes from command:issue, so only the grants whose role
		// actually carries the actuation contribute their scope. The READ scope is
		// the caller's own component:read and decides only which refusal that
		// principal is owed (#749, ADR-0116): a component outside it is the
		// non-disclosing 404, and one inside it that the issue scope does not
		// reach is the truthful 403. It grants nothing.
		//
		// Resolved ONCE, here, and bound by id from this point on: the two gateway
		// calls below take that id rather than the caller's raw reference, so
		// there is no second name resolve to disagree with the first (or to raise
		// an ambiguity the first already settled).
		compID, err := gw.ResolveActionTarget(ctx, "component", in.Name,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "command", "issue"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		cmd, err := gw.IssueCommand(ctx, actorID(ctx), "component", compID, in.Body.CommandType, in.Body.Instance,
			in.Body.Value, in.Body.Params)
		if err != nil {
			return nil, mapCommandErr(err)
		}
		verdict, err := gw.CommandSettlement(ctx, "component", compID, in.Body.CommandType, in.Body.Instance)
		if err != nil {
			return nil, mapCommandErr(err)
		}
		out := &commandOutput{}
		out.Body.ID = cmd.ID
		out.Body.CommandType = cmd.CommandType
		out.Body.Instance = cmd.Instance
		out.Body.CausedEventID = cmd.CausedEventID
		out.Body.Settlement = string(verdict)
		out.Body.Status = cmd.Status
		return out, nil
	})
}

// mapCommandErr maps the command sentinels: a request that names a command_type that
// does not exist (or an invalid one) is a 422, as is a non-numeric intended value for
// a metric target or a params payload that violates the type's params_schema; an
// out-of-scope component falls through to the component mapping (a non-disclosing 404).
func mapCommandErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrCommandTypeNotFound), errors.Is(err, storage.ErrCommandTypeInvalid):
		return huma.Error422UnprocessableEntity("unknown or invalid command_type")
	case errors.Is(err, storage.ErrCommandValueNotNumeric):
		return huma.Error422UnprocessableEntity("intended value for a metric target must be numeric")
	// The params refusal keeps the storage detail: the 422 names the violated
	// constraint, not just the fact of violation.
	case errors.Is(err, storage.ErrCommandParamsInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return mapComponentErr(err)
	}
}
