package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The alarm surface: what is currently wrong with a component. Raising and
// clearing are the two writes that move health from the component end, and
// each recomputes the rollup in its own transaction, so the alarm and the
// verdict it caused are never separately visible. An alarm impairs its
// component wholesale (#626): it no longer names what it takes away, only how
// bad it is.
//
// Alarms hang off the component and ride its gating: component:read to see them,
// component:update to raise or clear one. Every route resolves the component
// first, and the write routes resolve it with BOTH sets (#736): outside the
// caller's component:read the component is a non-disclosing 404, and inside it
// but outside the write's own scope the refusal is a 403 that says so. The list
// route passes its read set for both halves, which is the same set twice and so
// the plain non-disclosing 404 it always was.
//
// Acknowledging is the exception, and deliberately so: it carries its OWN
// permission (alarm:acknowledge) and resolves its ACTION scope from that
// permission rather than from component:update, because recording that a human
// has seen an alarm is not editing the component. A role may hold one without
// the other, and binding the two would let a wide component read or a narrow
// component write decide who may acknowledge. The read half is still plain
// component:read and still grants nothing; it only decides which refusal a
// caller is owed, which is the distinction #728 could not make and filed.

type alarmBody struct {
	ID        string     `json:"id"`
	Component string     `json:"component"`
	Severity  string     `json:"severity" doc:"info, warning, or critical"`
	Message   string     `json:"message"`
	DedupKey  string     `json:"dedup_key" doc:"The condition identity: one open alarm per (component, dedup_key)"`
	RaisedAt  time.Time  `json:"raised_at"`
	ClearedAt *time.Time `json:"cleared_at,omitempty" doc:"Null while the alarm is active"`
	Active    bool       `json:"active"`
	// The acknowledgement is orthogonal to cleared: an alarm can be acknowledged
	// and still raised, raised and unacknowledged, or cleared having never been
	// acknowledged.
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty" doc:"When a human first recorded seeing this alarm; null while nobody has. Independent of cleared_at"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty" doc:"Who acknowledged it, by name; empty while unacknowledged, or once that principal has been purged (the audit log keeps the name)"`
	Acknowledged   bool       `json:"acknowledged" doc:"Whether anybody has recorded seeing this alarm; says nothing about whether it is still raised"`
}

func toAlarmBody(a *storage.Alarm) alarmBody {
	return alarmBody{
		ID:             a.ID,
		Component:      a.ComponentID,
		Severity:       a.Severity,
		Message:        a.Message,
		DedupKey:       a.DedupKey,
		RaisedAt:       a.RaisedAt,
		ClearedAt:      a.ClearedAt,
		Active:         a.Active(),
		AcknowledgedAt: a.AcknowledgedAt,
		AcknowledgedBy: a.AcknowledgedBy,
		Acknowledged:   a.Acknowledged(),
	}
}

type listAlarmsInput struct {
	Name           string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	IncludeCleared bool   `query:"include_cleared" doc:"Include cleared alarms, so the list is the history rather than what is wrong now"`
	Unacknowledged bool   `query:"unacknowledged" doc:"Only the alarms nobody has looked at. On its own this is the queue an operator works (raised and unacknowledged); with include_cleared it also returns the incidents that came and went unattended"`
}

type listAlarmsOutput struct {
	Body struct {
		Component string      `json:"component"`
		Alarms    []alarmBody `json:"alarms"`
	}
}

type raiseAlarmInput struct {
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	Body struct {
		Severity string `json:"severity" enum:"info,warning,critical" doc:"How bad it is; critical puts the component itself in outage"`
		Message  string `json:"message,omitempty" doc:"What is wrong, for the operator reading it later"`
		DedupKey string `json:"dedup_key,omitempty" doc:"The condition identity; defaults to the message. Raising an already-open (component, dedup_key) returns the existing open alarm instead of a duplicate"`
	}
}

type alarmOutput struct {
	Body alarmBody
}

type clearAlarmInput struct {
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	ID   string `path:"id" doc:"The alarm id"`
}

// registerAlarmRoutes wires the component alarm surface.
func registerAlarmRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-alarms",
		Method:      http.MethodGet,
		Path:        "/components/{name}/alarms",
		Summary:     "List a component's alarms",
		Description: "What is currently wrong with this component, newest first. Pass include_cleared for the history rather than the active set. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *listAlarmsInput) (*listAlarmsOutput, error) {
		compID, err := requireComponentInScope(ctx, a, gw, in.Name, "read")
		if err != nil {
			return nil, err
		}
		alarms, err := gw.ListAlarms(ctx, compID, storage.AlarmFilter{
			IncludeCleared: in.IncludeCleared,
			Unacknowledged: in.Unacknowledged,
		})
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
		Description:   "Records a condition on this component, then recomputes health in the same transaction: the component's own verdict moves, and if it is now outage (a critical alarm), any role it occupies loses it as an occupant while the alarm is active, which can move its system and location verdicts with it; a lesser (info or warning) alarm degrades the component but leaves it occupying its roles. Gated by component:update; read and update scopes drive the 404 versus 403 split.",
	}, "component", "update"), func(ctx context.Context, in *raiseAlarmInput) (*alarmOutput, error) {
		compID, err := requireComponentInScope(ctx, a, gw, in.Name, "update")
		if err != nil {
			return nil, err
		}
		alarm, err := gw.RaiseAlarm(ctx, actorID(ctx), compID, storage.AlarmSpec{
			Severity: in.Body.Severity,
			Message:  in.Body.Message,
			DedupKey: in.Body.DedupKey,
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
		Description:   "Marks the alarm cleared and recomputes health in the same transaction, so the recovery is recorded as a transition at the moment it happened. The row is kept: what was wrong and when outlives the fix. Clearing an alarm that is already cleared or does not exist is a 404. Gated by component:update; read and update scopes drive the 404 versus 403 split.",
	}, "component", "update"), func(ctx context.Context, in *clearAlarmInput) (*struct{}, error) {
		compID, err := requireComponentInScope(ctx, a, gw, in.Name, "update")
		if err != nil {
			return nil, err
		}
		if err := gw.ClearAlarm(ctx, actorID(ctx), compID, in.ID); err != nil {
			return nil, mapAlarmErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "acknowledge-component-alarm",
		Method:      http.MethodPost,
		Path:        "/components/{name}/alarms/{id}:acknowledge",
		Summary:     "Acknowledge an alarm",
		Description: "Records that a human has seen this alarm, and changes nothing else. The alarm stays exactly as raised as it was: acknowledging is not fixing, so health is NOT recomputed and cleared_at is untouched. Acknowledging is orthogonal to clearing in both directions, so a cleared alarm can still be acknowledged by whoever reviews the history, and clearing never acknowledges on an operator's behalf. Acknowledging twice is idempotent: the first person and the first time stay, and the no-op writes no second audit row. Gated by alarm:acknowledge, whose scope is resolved on the component tier from that permission (not from component:update); a component outside the caller's component:read is a non-disclosing 404, and one it can read but not acknowledge on is a 403.",
	}, "alarm", "acknowledge"), func(ctx context.Context, in *clearAlarmInput) (*alarmOutput, error) {
		// The ACTION scope comes from alarm:acknowledge, so only the grants whose
		// role actually carries the acknowledgement contribute their scope: a wide
		// component read does not widen what may be acknowledged, and a principal
		// that may acknowledge in one building cannot acknowledge in another one it
		// can merely see.
		//
		// The READ scope is the caller's own component:read, and it decides only
		// which refusal that principal is owed (#736, #728): a component outside it
		// is still the non-disclosing 404, and a component inside it that the
		// acknowledgement does not reach is the truthful 403. It grants nothing.
		compID, err := gw.ResolveActionTarget(ctx, "component", in.Name,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "alarm", "acknowledge"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		alarm, err := gw.AcknowledgeAlarm(ctx, actorID(ctx), compID, in.ID)
		if err != nil {
			return nil, mapAlarmErr(err)
		}
		return &alarmOutput{Body: toAlarmBody(alarm)}, nil
	})
}

// mapAlarmErr translates the alarm sentinels into HTTP status. A bad severity is
// something the caller sent, so it is a 422.
func mapAlarmErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrAlarmNotFound):
		return huma.Error404NotFound("alarm not found")
	case errors.Is(err, storage.ErrAlarmSeverity):
		return huma.Error422UnprocessableEntity("severity must be info, warning, or critical")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	default:
		return huma.Error500InternalServerError("alarm operation failed")
	}
}
