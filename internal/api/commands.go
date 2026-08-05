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

type issueCommandInput struct {
	Name string `path:"name" doc:"The component name"`
	Body struct {
		CommandType string          `json:"command_type" minLength:"1" doc:"The command_type to invoke"`
		Instance    string          `json:"instance,omitempty" doc:"The series discriminator (e.g. an interface), when the target is instanced"`
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
		Description: "Records a command invocation, writes a caused event, and (for a settleable command) opens an intended value the observed value settles against. Returns the computed settlement verdict. Gated by command:issue; an out-of-scope component is a non-disclosing 404.",
	}, "command", "issue"), func(ctx context.Context, in *issueCommandInput) (*commandOutput, error) {
		comp, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		cmd, err := gw.IssueCommand(ctx, actorID(ctx), "component", comp.Name, in.Body.CommandType, in.Body.Instance,
			in.Body.Value, in.Body.Params, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapCommandErr(err)
		}
		verdict, err := gw.CommandSettlement(ctx, "component", comp.Name, in.Body.CommandType, in.Body.Instance, a.scopeFor(ctx, "component", "read"))
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
// a metric target; an out-of-scope component falls through to the component mapping
// (a non-disclosing 404).
func mapCommandErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrCommandTypeNotFound), errors.Is(err, storage.ErrCommandTypeInvalid):
		return huma.Error422UnprocessableEntity("unknown or invalid command_type")
	case errors.Is(err, storage.ErrCommandValueNotNumeric):
		return huma.Error422UnprocessableEntity("intended value for a metric target must be numeric")
	default:
		return mapComponentErr(err)
	}
}
