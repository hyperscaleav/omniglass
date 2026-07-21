package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The alarm surface: what is currently wrong with a component, and the
// capabilities that condition takes away. Raising and clearing are the two writes
// that move health from the component end, and each recomputes the rollup in its
// own transaction, so the alarm and the verdict it caused are never separately
// visible.
//
// Alarms hang off the component and ride its gating: component:read to see them,
// component:update to raise or clear one. Every route resolves the component
// within the caller's scope first, so an out-of-scope component is a
// non-disclosing 404.

type alarmBody struct {
	ID           string     `json:"id"`
	Component    string     `json:"component"`
	Severity     string     `json:"severity" doc:"info, warning, or critical"`
	Message      string     `json:"message"`
	Capabilities []string   `json:"capabilities" doc:"The capabilities this alarm degrades; empty means it reaches no role"`
	RaisedAt     time.Time  `json:"raised_at"`
	ClearedAt    *time.Time `json:"cleared_at,omitempty" doc:"Null while the alarm is active"`
	Active       bool       `json:"active"`
}

func toAlarmBody(a *storage.Alarm) alarmBody {
	caps := a.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return alarmBody{
		ID:           a.ID,
		Component:    a.ComponentID,
		Severity:     a.Severity,
		Message:      a.Message,
		Capabilities: caps,
		RaisedAt:     a.RaisedAt,
		ClearedAt:    a.ClearedAt,
		Active:       a.Active(),
	}
}

type listAlarmsInput struct {
	Name           string `path:"name" doc:"The component's unique name"`
	IncludeCleared bool   `query:"include_cleared" doc:"Include cleared alarms, so the list is the history rather than what is wrong now"`
}

type listAlarmsOutput struct {
	Body struct {
		Component string      `json:"component"`
		Alarms    []alarmBody `json:"alarms"`
	}
}

type raiseAlarmInput struct {
	Name string `path:"name" doc:"The component's unique name"`
	Body struct {
		Severity     string   `json:"severity" enum:"info,warning,critical" doc:"How bad it is; critical puts the component itself in outage"`
		Message      string   `json:"message,omitempty" doc:"What is wrong, for the operator reading it later"`
		Capabilities []string `json:"capabilities,omitempty" doc:"The capabilities this condition degrades; a role requiring one of them can no longer be filled by this component"`
	}
}

type alarmOutput struct {
	Body alarmBody
}

type clearAlarmInput struct {
	Name string `path:"name" doc:"The component's unique name"`
	ID   string `path:"id" doc:"The alarm id"`
}

// registerAlarmRoutes wires the component alarm surface.
func registerAlarmRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-alarms",
		Method:      http.MethodGet,
		Path:        "/components/{name}/alarms",
		Summary:     "List a component's alarms",
		Description: "What is currently wrong with this component, newest first, each with the capabilities it degrades. Pass include_cleared for the history rather than the active set. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *listAlarmsInput) (*listAlarmsOutput, error) {
		if err := requireComponentInScope(ctx, a, gw, in.Name, "read"); err != nil {
			return nil, err
		}
		alarms, err := gw.ListAlarms(ctx, in.Name, in.IncludeCleared)
		if err != nil {
			return nil, mapAlarmErr(err)
		}
		out := &listAlarmsOutput{}
		out.Body.Component = in.Name
		out.Body.Alarms = make([]alarmBody, 0, len(alarms))
		for i := range alarms {
			out.Body.Alarms = append(out.Body.Alarms, toAlarmBody(&alarms[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "raise-component-alarm",
		Method:        http.MethodPost,
		Path:          "/components/{name}/alarms",
		DefaultStatus: http.StatusCreated,
		Summary:       "Raise an alarm on a component",
		Description:   "Records a condition on this component and the capabilities it degrades, then recomputes health in the same transaction: any role requiring a degraded capability can no longer be filled by this component, and its system and location verdicts move with it. An unknown capability is a 422. Gated by component:update; an out-of-scope component is a non-disclosing 404.",
	}, "component", "update"), func(ctx context.Context, in *raiseAlarmInput) (*alarmOutput, error) {
		if err := requireComponentInScope(ctx, a, gw, in.Name, "update"); err != nil {
			return nil, err
		}
		alarm, err := gw.RaiseAlarm(ctx, actorID(ctx), in.Name, storage.AlarmSpec{
			Severity:     in.Body.Severity,
			Message:      in.Body.Message,
			Capabilities: in.Body.Capabilities,
		})
		if err != nil {
			return nil, mapAlarmErr(err)
		}
		return &alarmOutput{Body: toAlarmBody(alarm)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "clear-component-alarm",
		Method:        http.MethodDelete,
		Path:          "/components/{name}/alarms/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Clear an alarm",
		Description:   "Marks the alarm cleared and recomputes health in the same transaction, so the recovery is recorded as a transition at the moment it happened. The row is kept: what was wrong and when outlives the fix. Clearing an alarm that is already cleared or does not exist is a 404. Gated by component:update; an out-of-scope component is a non-disclosing 404.",
	}, "component", "update"), func(ctx context.Context, in *clearAlarmInput) (*struct{}, error) {
		if err := requireComponentInScope(ctx, a, gw, in.Name, "update"); err != nil {
			return nil, err
		}
		if err := gw.ClearAlarm(ctx, actorID(ctx), in.Name, in.ID); err != nil {
			return nil, mapAlarmErr(err)
		}
		return nil, nil
	})
}

// mapAlarmErr translates the alarm sentinels into HTTP status. A bad severity and
// an unknown capability are both things the caller sent, so both are 422.
func mapAlarmErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrAlarmNotFound):
		return huma.Error404NotFound("alarm not found")
	case errors.Is(err, storage.ErrAlarmSeverity):
		return huma.Error422UnprocessableEntity("severity must be info, warning, or critical")
	case errors.Is(err, storage.ErrAlarmRefNotFound):
		return huma.Error422UnprocessableEntity("unknown capability")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	default:
		return huma.Error500InternalServerError("alarm operation failed")
	}
}
