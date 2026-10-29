package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
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
	ID          string `json:"id"`
	Component   string `json:"component" doc:"The component the alarm is on, by name"`
	ComponentID string `json:"component_id" doc:"The component's uuid, the stable form of component"`
	// The rule that raised the alarm (#919), absent on a hand-raised one.
	RaisedByRule   string     `json:"raised_by_rule,omitempty" doc:"The event rule that raised the alarm, by name; absent when an operator raised it"`
	RaisedByRuleID string     `json:"raised_by_rule_id,omitempty" doc:"The raising rule's uuid"`
	Severity       string     `json:"severity" doc:"The severity level the alarm was raised at, by name (see /severity-levels)"`
	Message        string     `json:"message"`
	DedupKey       string     `json:"dedup_key" doc:"The condition identity: one open alarm per (component, dedup_key)"`
	RaisedAt       time.Time  `json:"raised_at"`
	ClearedAt      *time.Time `json:"cleared_at,omitempty" doc:"Null while the alarm is active"`
	Active         bool       `json:"active"`
	// The acknowledgement is orthogonal to cleared: an alarm can be acknowledged
	// and still raised, raised and unacknowledged, or cleared having never been
	// acknowledged.
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty" doc:"When a human first recorded seeing this alarm; null while nobody has. Independent of cleared_at"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty" doc:"Who acknowledged it, by name; empty while unacknowledged, or once that principal has been purged (the audit log keeps the name)"`
	Acknowledged   bool       `json:"acknowledged" doc:"Whether anybody has recorded seeing this alarm; says nothing about whether it is still raised"`
	// The snooze belongs to the alarm's condition (#1318), so every alarm of
	// the same (component, dedup_key) reads the same pair.
	SnoozedUntil *time.Time `json:"snoozed_until,omitempty" doc:"Until when the alarm's condition (component and dedup_key) is snoozed: its notifications are written suppressed until then. Null while it is not snoozed. Independent of acknowledged and cleared_at"`
	SnoozedBy    string     `json:"snoozed_by,omitempty" doc:"Who snoozed the condition, by name; empty while it is not snoozed, or once that principal has been purged (the audit log keeps the name)"`
}

func toAlarmBody(a *storage.Alarm) alarmBody {
	return alarmBody{
		ID:             a.ID,
		Component:      a.ComponentName,
		ComponentID:    a.ComponentID,
		RaisedByRule:   derefStr(a.RaisedByRule),
		RaisedByRuleID: derefStr(a.RaisedByRuleID),
		Severity:       a.Severity,
		Message:        a.Message,
		DedupKey:       a.DedupKey,
		RaisedAt:       a.RaisedAt,
		ClearedAt:      a.ClearedAt,
		Active:         a.Active(),
		AcknowledgedAt: a.AcknowledgedAt,
		AcknowledgedBy: a.AcknowledgedBy,
		Acknowledged:   a.Acknowledged(),
		SnoozedUntil:   a.SnoozedUntil,
		SnoozedBy:      a.SnoozedBy,
	}
}

type snoozeAlarmInput struct {
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	ID   string `path:"id" doc:"The alarm id: any alarm of the condition to snooze"`
	Body struct {
		Duration string `json:"duration" doc:"How long from now the condition stays snoozed, from 1m to 7d: a duration such as 15m, 1h, 4h or 24h, or whole days such as 7d. Snoozing a snoozed condition replaces its deadline, shorter or longer"`
	}
}

// parseSnoozeDuration reads a snooze's duration: a Go duration (15m, 1h,
// 24h) or whole days (7d), the unit an operator thinks in at the top of the
// bound. Anything else, or anything outside the bound, is the 422 naming it.
func parseSnoozeDuration(s string) (time.Duration, error) {
	var d time.Duration
	var err error
	if n, ok := strings.CutSuffix(s, "d"); ok && n != "" && strings.Trim(n, "0123456789") == "" && len(n) <= 3 {
		days := 0
		for _, c := range n {
			days = days*10 + int(c-'0')
		}
		d = time.Duration(days) * 24 * time.Hour
	} else {
		d, err = time.ParseDuration(s)
	}
	if err != nil || d < storage.SnoozeMin || d > storage.SnoozeMax {
		return 0, storage.ErrSnoozeDuration
	}
	return d, nil
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
		Verdict   string      `json:"verdict" doc:"The component's own verdict, the one its active alarms make (healthy, degraded, or outage), whatever the filter: a component's view reads it here rather than off the whole fleet view"`
	}
}

type raiseAlarmInput struct {
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	Body struct {
		Severity string `json:"severity" minLength:"1" doc:"How bad it is: a severity level by name (see /severity-levels); a level at or above high puts the component itself in outage"`
		Message  string `json:"message,omitempty" doc:"What is wrong, for the operator reading it later"`
		DedupKey string `json:"dedup_key,omitempty" doc:"The condition identity; defaults to the message. Raising an already-open (component, dedup_key) returns the existing open alarm instead of a duplicate. A key (or, without one, a message) beginning rule: is refused (422): that namespace names a rule's condition"`
	}
}

type alarmOutput struct {
	Body alarmBody
}

type clearAlarmInput struct {
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	ID   string `path:"id" doc:"The alarm id"`
}

// systemAlarmWindow and systemAlarmLimit bound the system's alarms read: the
// window is the system's Activity history (the health report's 30 days), and
// the cap keeps a flapping member from making the read fleet-sized.
const (
	systemAlarmWindow = 30 * 24 * time.Hour
	systemAlarmLimit  = 500
)

type systemAlarmsOutput struct {
	Body struct {
		System string      `json:"system"`
		Alarms []alarmBody `json:"alarms"`
	}
}

// registerAlarmRoutes wires the component alarm surface.
func registerAlarmRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-system-alarms",
		Method:      http.MethodGet,
		Path:        "/systems/{name}/alarms",
		Summary:     "List a system's members' alarms",
		Description: "Every member's alarms in one read, newest first, each naming its component: the active ones whatever their age, and those cleared in the last 30 days, capped at 500. It is what a system's Activity reads to explain its incidents, one request however many members the system has. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemPathInput) (*systemAlarmsOutput, error) {
		sys, err := gw.GetSystem(ctx, in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		alarms, err := gw.ListSystemAlarms(ctx, sys.ID, systemAlarmWindow, systemAlarmLimit)
		if err != nil {
			return nil, huma.Error500InternalServerError("read system alarms")
		}
		out := &systemAlarmsOutput{}
		out.Body.System = sys.Name
		out.Body.Alarms = make([]alarmBody, 0, len(alarms))
		for i := range alarms {
			out.Body.Alarms = append(out.Body.Alarms, toAlarmBody(&alarms[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-alarms",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/alarms",
		Summary:     "List a component's alarms",
		Description: "What is currently wrong with this component, newest first, with the component's own verdict those alarms make. Pass include_cleared for the history rather than the active set. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
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
		verdict, err := gw.ComponentVerdict(ctx, compID)
		if err != nil {
			return nil, huma.Error500InternalServerError("read component verdict")
		}
		out := &listAlarmsOutput{}
		out.Body.Component = in.Name
		out.Body.Verdict = verdict
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
		Description:   "Records a condition on this component, then recomputes health in the same transaction: the component's own verdict moves, and if it is now outage (an alarm at or above the high level), any role it occupies loses it as an occupant while the alarm is active, which can move its system and location verdicts with it; a lesser alarm degrades the component but leaves it occupying its roles. Gated by component:update; read and update scopes drive the 404 versus 403 split.",
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

	// Snooze (#1318, ADR-1318) is acknowledgement's shape exactly: its own
	// permission, alarm:snooze, whose scope resolves on the component tier
	// from that permission, with the caller's component:read deciding only
	// between the non-disclosing 404 and the truthful 403.
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "snooze-component-alarm",
		Method:      http.MethodPost,
		Path:        "/components/{name}/alarms/{id}:snooze",
		Summary:     "Snooze an alarm's condition",
		Description: "Snoozes the alarm's condition, its component and dedup_key, for a duration from now (1m to 7d). Snoozing a snoozed condition replaces the deadline. It changes nothing else: it does not acknowledge, does not recompute health, a snoozed alarm stays in the unacknowledged queue, and a clear does not end it. It notifies nothing itself. Audited on this alarm. Gated by alarm:snooze, whose scope is resolved on the component tier from that permission; a component outside the caller's component:read is a non-disclosing 404, and one it can read but not snooze on is a 403.",
	}, "alarm", "snooze"), func(ctx context.Context, in *snoozeAlarmInput) (*alarmOutput, error) {
		d, err := parseSnoozeDuration(in.Body.Duration)
		if err != nil {
			return nil, mapAlarmErr(err)
		}
		compID, err := gw.ResolveActionTarget(ctx, "component", in.Name,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "alarm", "snooze"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		alarm, err := gw.SnoozeAlarm(ctx, actorID(ctx), compID, in.ID, d)
		if err != nil {
			return nil, mapAlarmErr(err)
		}
		return &alarmOutput{Body: toAlarmBody(alarm)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "unsnooze-component-alarm",
		Method:      http.MethodPost,
		Path:        "/components/{name}/alarms/{id}:unsnooze",
		Summary:     "End an alarm's condition's snooze",
		Description: "Ends the snooze on the alarm's condition early. Audited on this alarm, naming the caller. Unsnoozing a condition that is not snoozed is a 200 that writes nothing. Gated by alarm:snooze, resolved as :snooze is.",
	}, "alarm", "snooze"), func(ctx context.Context, in *clearAlarmInput) (*alarmOutput, error) {
		compID, err := gw.ResolveActionTarget(ctx, "component", in.Name,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "alarm", "snooze"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		alarm, err := gw.UnsnoozeAlarm(ctx, actorID(ctx), compID, in.ID)
		if err != nil {
			return nil, mapAlarmErr(err)
		}
		return &alarmOutput{Body: toAlarmBody(alarm)}, nil
	})
}

type alarmTimelineEntryBody struct {
	At       time.Time `json:"at" doc:"When it happened (a notification: when it was queued, the transition's own moment)"`
	Kind     string    `json:"kind" enum:"event,audit,notification" doc:"An event carrying the alarm (a rule's fire or clear event), an audit row about it, or a notification about it (#618)"`
	What     string    `json:"what" doc:"The event's type (e.g. condition-started), the audit action (raise, acknowledge or clear), or a notification's status as it stands (queued, sent or failed)"`
	Actor    string    `json:"actor,omitempty" doc:"Who acted, by name; absent when the platform acted for a rule"`
	Rule     string    `json:"rule,omitempty" doc:"The event rule the entry belongs to, by name: the rule that wrote the event, or the rule the platform acted for"`
	Instance string    `json:"instance,omitempty" doc:"The series instance (an endpoint) an event is about"`
	Message  string    `json:"message,omitempty" doc:"The event's message, the message the alarm was raised with, or a notification's rendered subject"`
	// A notification's own fields.
	Notification string `json:"notification,omitempty" doc:"A notification's uuid, what :retry names"`
	ActionRule   string `json:"action_rule,omitempty" doc:"The action rule that queued a notification, by name"`
	Transition   string `json:"transition,omitempty" doc:"The alarm edge a notification is about: raised or cleared"`
	Target       string `json:"target,omitempty" doc:"The target a notification goes to, by name"`
	Attempts     int    `json:"attempts,omitempty" doc:"A notification's attempts so far"`
	Error        string `json:"error,omitempty" doc:"Why a notification's last attempt failed"`
	Channel      string `json:"channel,omitempty" enum:"webhook,inbox,email," doc:"How a notification travels: webhook, a person's inbox, or email"`
	Recipient    string `json:"recipient,omitempty" doc:"The person a notification is addressed to, by username: shown to that person and to holders of principal:read:admin"`
	Withheld     bool   `json:"recipient_withheld,omitempty" doc:"True when a notification is addressed to a person whose name the caller may not see"`
}

type alarmTimelineOutput struct {
	Body struct {
		Component string                   `json:"component"`
		AlarmID   string                   `json:"alarm_id"`
		Entries   []alarmTimelineEntryBody `json:"entries" doc:"Oldest first; at one instant an event comes before the audit row it caused"`
	}
}

// registerAlarmTimelineRoute wires an alarm's timeline (#950): its events
// and its audit rows merged by time. Gated alarm:read; the component is
// resolved in the caller's component read scope first, so a component it
// cannot read is the same non-disclosing 404 an unknown one is.
func registerAlarmTimelineRoute(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-component-alarm-timeline",
		Method:      http.MethodGet,
		Path:        "/components/{name}/alarms/{id}:timeline",
		Summary:     "Read an alarm's timeline",
		Description: "The alarm's history in one list, oldest first: the events that carry it (an event rule's fire event, which opened it, and its clear event, which resolved it) merged by time with its audit rows (the raise, each acknowledgement, the clear, and a rule edit that opened or resolved it, naming the editor) and the notifications its transitions queued, each at the moment it was queued with its status as it stands (#618). At one instant an event comes before the audit row it caused, and that before a notification. Gated by alarm:read; a component outside the caller's component read scope, and an alarm not on the component, are a non-disclosing 404.",
	}, "alarm", "read"), func(ctx context.Context, in *clearAlarmInput) (*alarmTimelineOutput, error) {
		compID, err := requireComponentInScope(ctx, a, gw, in.Name, "read")
		if err != nil {
			return nil, err
		}
		entries, err := gw.AlarmTimeline(ctx, compID, in.ID, recipientView(ctx, a))
		if err != nil {
			return nil, mapAlarmErr(err)
		}
		out := &alarmTimelineOutput{}
		out.Body.Component = in.Name
		out.Body.AlarmID = in.ID
		out.Body.Entries = make([]alarmTimelineEntryBody, 0, len(entries))
		for _, e := range entries {
			b := alarmTimelineEntryBody{At: e.At, Kind: e.Kind, What: e.What,
				Actor: e.Actor, Rule: e.Rule, Instance: e.Instance, Message: e.Message}
			if e.Kind == "notification" {
				b.Rule = ""
				b.Notification, b.ActionRule, b.Transition, b.Target, b.Attempts, b.Error = e.NotificationID, e.Rule, e.Transition, e.Target, e.Attempts, e.Error
				b.Channel, b.Recipient, b.Withheld = e.Channel, e.Recipient, e.Personal && e.Recipient == ""
			}
			out.Body.Entries = append(out.Body.Entries, b)
		}
		return out, nil
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
	case errors.Is(err, storage.ErrAlarmInvalid):
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "storage: alarm is invalid: "))
	case errors.Is(err, storage.ErrSnoozeDuration):
		return huma.Error422UnprocessableEntity("duration must be from 1m to 7d (168h), for example 15m, 1h, 4h, 24h or 7d")
	case errors.Is(err, storage.ErrAlarmSeverity):
		return huma.Error422UnprocessableEntity("severity must name a severity level (see /severity-levels)")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	default:
		return huma.Error500InternalServerError("alarm operation failed")
	}
}
