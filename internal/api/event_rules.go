package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// criteriaBody is a criterion as the API returns it: always an expression
// (#957).
type criteriaBody struct {
	Expr string `json:"expr" doc:"The criterion, an expression over the sample (value, key, instance, component.name, component.type) that yields true or false, e.g. value > 80 && instance != \"aux\""`
}

// criteriaInput is a criterion as a body may give it: an expression, or the
// short form {op, value} (one comparison of value against a threshold),
// which is lowered to its expression (eq "down" is value == "down").
type criteriaInput struct {
	Expr  string `json:"expr,omitempty" required:"false" doc:"The criterion as an expression over the sample (value, key, instance, component.name, component.type), yielding true or false; give this or op and value"`
	Op    string `json:"op,omitempty" required:"false" enum:"eq,ne,gt,ge,lt,le" doc:"The short form's comparison: a property compares text with eq or ne; a metric compares numbers with any of them"`
	Value any    `json:"value,omitempty" required:"false" doc:"The short form's threshold: text for a property, a number for a metric"`
}

type selectorBody struct {
	ComponentTypes []string `json:"component_types,omitempty" required:"false" doc:"Watch components whose type falls within one of these component types"`
	Components     []string `json:"components,omitempty" required:"false" doc:"Watch these components (by name or id on write; ids on read)"`
}

type eventRuleBody struct {
	ID                string             `json:"id" doc:"The rule's uuid, the stable form of name"`
	Name              string             `json:"name"`
	Label             string             `json:"label,omitempty"`
	Description       string             `json:"description,omitempty"`
	Selector          selectorBody       `json:"selector" doc:"The components the rule watches; both lists empty is every component"`
	SampleKey         string             `json:"sample_key" doc:"The property or metric the rule reads"`
	SampleLane        string             `json:"sample_lane" enum:"property,metric" doc:"Which lane the key reads, derived from the registries"`
	FireCriteria      criteriaBody       `json:"fire_criteria" doc:"When the sample meets this for fire_sustain, the rule's condition starts (and it opens an alarm when it names a severity)"`
	ClearCriteria     *criteriaBody      `json:"clear_criteria,omitempty" doc:"When the sample meets this for clear_sustain, the condition ends (and the alarm clears); absent, the negation of fire_criteria"`
	FireSustain       string             `json:"fire_sustain" doc:"How long the fire criteria must hold, a Go duration (0s fires at once)"`
	ClearSustain      string             `json:"clear_sustain" doc:"How long the clear criteria must hold, a Go duration"`
	Severity          string             `json:"severity,omitempty" doc:"The severity level the alarm is raised at, by name; absent, the rule is informational and raises no alarm"`
	FireEventType     string             `json:"fire_event_type" doc:"The event type the rule writes when its condition starts, by name"`
	ClearEventType    string             `json:"clear_event_type,omitempty" doc:"The event type the rule writes when its condition ends, by name; absent, it writes none"`
	Message           string             `json:"message,omitempty" doc:"The alarm's message; empty uses the rule's label or name"`
	Enabled           bool               `json:"enabled"`
	Official          bool               `json:"official" doc:"Shipped with the platform and read-only"`
	Version           int                `json:"version" doc:"Bumps on every update"`
	OpenAlarms        int                `json:"open_alarms" doc:"How many alarms the rule raised are still open right now (its live state; GET /event-rules/{name}/state lists them)"`
	PendingStreaks    int                `json:"pending_streaks" doc:"How many of the rule's streaks are waiting out a sustain right now"`
	ConditionsInForce int                `json:"conditions_in_force" doc:"How many conditions the rule holds in force right now, one per component and instance, whether or not it raises an alarm (GET /event-rules/{name}/state lists them)"`
	LastJudgedAt      *time.Time         `json:"last_judged_at,omitempty" required:"false" doc:"When an ingested sample was last judged by the rule, to within 5s; absent when it never has been. A fleet-wide fact about the rule, naming no component"`
	CriteriaError     *criteriaErrorBody `json:"criteria_error,omitempty" required:"false" doc:"Why the rule's stored criteria do not compile (its key's data type changed under them, or a row the backfill left in short form); absent when they compile. While present the rule holds: it judges nothing"`
}

// criteriaErrorBody is a stored rule's criteria compile error.
type criteriaErrorBody struct {
	Field   string `json:"field" enum:"fire_criteria,clear_criteria" doc:"Which criteria"`
	Line    int    `json:"line,omitempty" required:"false" doc:"The 1-based line of the fault, when it has one"`
	Column  int    `json:"column,omitempty" required:"false" doc:"The 1-based column of the fault, when it has one"`
	Message string `json:"message"`
}

func toCriteriaErrorBody(f *storage.CriteriaFault) *criteriaErrorBody {
	if f == nil {
		return nil
	}
	return &criteriaErrorBody{Field: f.Field, Line: f.Line, Column: f.Column, Message: f.Message}
}

func toCriteriaBody(c rules.Criteria) criteriaBody {
	return criteriaBody{Expr: c.Expr}
}

func fromCriteriaBody(c criteriaInput) (rules.Criteria, error) {
	out := rules.Criteria{Expr: c.Expr, Op: c.Op}
	if c.Value != nil {
		b, err := json.Marshal(c.Value)
		if err != nil {
			return rules.Criteria{}, huma.Error422UnprocessableEntity("criteria value is not JSON")
		}
		out.Value = b
	}
	return out, nil
}

func toEventRuleBody(r *storage.EventRule) eventRuleBody {
	b := eventRuleBody{ID: r.ID, Name: r.Name, Label: r.Label, Description: r.Description,
		Selector:  selectorBody{ComponentTypes: r.Selector.ComponentTypes, Components: r.Selector.Components},
		SampleKey: r.SampleKey, SampleLane: string(r.SampleLane), FireCriteria: toCriteriaBody(r.FireCriteria),
		FireSustain: r.FireSustain.String(), ClearSustain: r.ClearSustain.String(), Severity: r.Severity,
		FireEventType: r.FireEventType, ClearEventType: r.ClearEventType, Message: r.Message, Enabled: r.Enabled, Official: r.Official, Version: r.Version,
		OpenAlarms: r.OpenAlarms, PendingStreaks: r.PendingStreaks, ConditionsInForce: r.ConditionsInForce, LastJudgedAt: r.LastJudgedAt,
		CriteriaError: toCriteriaErrorBody(r.CriteriaError)}
	if r.ClearCriteria != nil {
		c := toCriteriaBody(*r.ClearCriteria)
		b.ClearCriteria = &c
	}
	return b
}

func parseSustain(field, s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, huma.Error422UnprocessableEntity(fmt.Sprintf("%s %q is not a duration (e.g. 30s, 5m)", field, s))
	}
	return d, nil
}

type listEventRulesOutput struct {
	Body struct {
		EventRules []eventRuleBody `json:"event_rules"`
	}
}

type eventRuleOutput struct{ Body eventRuleBody }

type eventRuleNameInput struct {
	Name string `path:"name" doc:"The rule's name"`
}

type createEventRuleInput struct {
	Body struct {
		Name           string         `json:"name" minLength:"1" doc:"The rule's name (lowercase kebab)"`
		Label          string         `json:"label,omitempty" required:"false" doc:"A human label; also the alarm message when message is empty"`
		Description    string         `json:"description,omitempty" required:"false" doc:"What the rule watches for and why"`
		Selector       *selectorBody  `json:"selector,omitempty" required:"false" doc:"The components the rule watches (component types and components); absent is every component"`
		SampleKey      string         `json:"sample_key" minLength:"1" doc:"The property or metric the rule reads"`
		FireCriteria   criteriaInput  `json:"fire_criteria" doc:"The criteria that starts the condition: {expr}, or the short form {op, value}"`
		ClearCriteria  *criteriaInput `json:"clear_criteria,omitempty" required:"false" doc:"The criteria that ends it: {expr} or {op, value}; absent clears on the negation of fire_criteria, !(fire)"`
		FireSustain    string         `json:"fire_sustain,omitempty" required:"false" doc:"A Go duration; empty is 0s"`
		ClearSustain   string         `json:"clear_sustain,omitempty" required:"false" doc:"A Go duration; empty is 0s"`
		Severity       string         `json:"severity,omitempty" required:"false" doc:"The severity level the alarm is raised at, by name; absent or empty, the rule is informational and raises no alarm"`
		FireEventType  string         `json:"fire_event_type" minLength:"1" doc:"The event type the rule writes when its condition starts, by name (condition-started is the generic one)"`
		ClearEventType string         `json:"clear_event_type,omitempty" required:"false" doc:"The event type the rule writes when its condition ends, by name (condition-ended is the generic one); absent, it writes none"`
		Message        string         `json:"message,omitempty" required:"false" doc:"The alarm message; empty uses the label or the name"`
		Enabled        *bool          `json:"enabled,omitempty" required:"false" doc:"Defaults to true"`
	}
}

type updateEventRuleInput struct {
	Name string `path:"name" doc:"The rule's name"`
	Body struct {
		Label          *string        `json:"label,omitempty" required:"false" doc:"A human label; also the alarm message when message is empty"`
		Description    *string        `json:"description,omitempty" required:"false" doc:"What the rule watches for and why"`
		Selector       *selectorBody  `json:"selector,omitempty" required:"false" doc:"Replace the components the rule watches; absent leaves them unchanged, and an empty selector watches every component"`
		SampleKey      *string        `json:"sample_key,omitempty" required:"false" doc:"The property or metric the rule reads"`
		FireCriteria   *criteriaInput `json:"fire_criteria,omitempty" required:"false" doc:"The criteria that starts the condition: {expr}, or the short form {op, value}"`
		ClearCriteria  *criteriaInput `json:"clear_criteria,omitempty" required:"false" doc:"The criteria that ends it: {expr} or {op, value}; absent clears on the negation of fire_criteria, !(fire)"`
		ClearNegates   bool           `json:"clear_negates,omitempty" required:"false" doc:"Drop the clear criteria, so the rule clears on the negation of its fire criteria"`
		FireSustain    *string        `json:"fire_sustain,omitempty" required:"false" doc:"How long the fire criteria must hold, a Go duration (e.g. 30s); empty is 0s"`
		ClearSustain   *string        `json:"clear_sustain,omitempty" required:"false" doc:"How long the clear criteria must hold, a Go duration; empty is 0s"`
		Severity       *string        `json:"severity,omitempty" required:"false" doc:"The severity level the alarm is raised at, by name; empty makes the rule informational (no alarm)"`
		FireEventType  *string        `json:"fire_event_type,omitempty" required:"false" doc:"The event type the rule writes when its condition starts, by name; it cannot be emptied"`
		ClearEventType *string        `json:"clear_event_type,omitempty" required:"false" doc:"The event type the rule writes when its condition ends, by name; empty writes none"`
		Message        *string        `json:"message,omitempty" required:"false" doc:"The alarm message; empty uses the label or the name"`
		Enabled        *bool          `json:"enabled,omitempty" required:"false" doc:"Whether the rule evaluates; defaults to true on create"`
	}
}

// registerEventRuleRoutes wires event rule authoring (#918), and the rule's
// live state (#921): the rules that raise alarms, gated rule:*. A rule reads
// like a registry (rule:read from any grant), its live state fenced to the
// caller's component read scope; writing one takes an all-scope grant, since
// its selector reaches the whole fleet (ruling 2 on #819, amended #921).
// Official rules are read-only.
func registerEventRuleRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-event-rule",
		Method:      http.MethodGet,
		Path:        "/event-rules",
		Summary:     "List event rules",
		Description: "Lists the event rules, the authored config that raises alarms, each with its live state as counts (conditions_in_force, open_alarms, pending_streaks) read in the same query. Rules read like a registry: every rule is listed whatever the caller's scope, while the counts cover only components the caller may read. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, _ *struct{}) (*listEventRulesOutput, error) {
		rs, err := gw.ListEventRules(ctx, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapEventRuleErr(err)
		}
		out := &listEventRulesOutput{}
		out.Body.EventRules = make([]eventRuleBody, 0, len(rs))
		for i := range rs {
			out.Body.EventRules = append(out.Body.EventRules, toEventRuleBody(&rs[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-event-rule",
		Method:      http.MethodGet,
		Path:        "/event-rules/{name}",
		Summary:     "Get an event rule",
		Description: "Returns one event rule by name, with its live state as counts over the components the caller may read. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, in *eventRuleNameInput) (*eventRuleOutput, error) {
		r, err := gw.GetEventRule(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapEventRuleErr(err)
		}
		return &eventRuleOutput{Body: toEventRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-event-rule",
		Method:        http.MethodPost,
		Path:          "/event-rules",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create an event rule",
		Description:   "Authors a rule whose condition starts on each component it watches when its sample key meets the fire criteria for the fire sustain, and ends when the clear criteria (or the negation of fire) holds for the clear sustain. It names the event type it writes when the condition starts (required) and, optionally, when it ends; with a severity it also raises an alarm while the condition holds, and without one it is informational. Criteria are expressions over the sample (value, key, instance, component.name, component.type) yielding true or false; a body may give {expr} or the short form {op, value}, and the answer is always {expr}. The key must be a registered property or metric, the criteria must compile against its lane (an unknown name or a result that is not a bool is a 422 naming the criteria, line and column), the event types registered and the severity, when given, a level that exists (else 422); a name already taken is a 409. Audited. Gated by rule:create, with an all-scope grant.",
	}, "rule", "create"), func(ctx context.Context, in *createEventRuleInput) (*eventRuleOutput, error) {
		b := in.Body
		fire, err := fromCriteriaBody(b.FireCriteria)
		if err != nil {
			return nil, err
		}
		spec := storage.EventRuleSpec{Name: b.Name, Label: b.Label, Description: b.Description, SampleKey: b.SampleKey,
			Fire: fire, Severity: b.Severity, Message: b.Message, Enabled: b.Enabled,
			FireEventType: b.FireEventType, ClearEventType: b.ClearEventType}
		if b.Selector != nil {
			spec.Selector = storage.EventRuleSelector{ComponentTypes: b.Selector.ComponentTypes, Components: b.Selector.Components}
		}
		if b.ClearCriteria != nil {
			c, err := fromCriteriaBody(*b.ClearCriteria)
			if err != nil {
				return nil, err
			}
			spec.Clear = &c
		}
		if spec.FireSustain, err = parseSustain("fire_sustain", b.FireSustain); err != nil {
			return nil, err
		}
		if spec.ClearSustain, err = parseSustain("clear_sustain", b.ClearSustain); err != nil {
			return nil, err
		}
		r, err := gw.CreateEventRule(ctx, actorID(ctx), spec, a.scopeFor(ctx, "rule", "create"))
		if err != nil {
			return nil, mapEventRuleErr(err)
		}
		return rereadEventRule(ctx, a, gw, r.Name)
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-event-rule",
		Method:      http.MethodPatch,
		Path:        "/event-rules/{name}",
		Summary:     "Update an event rule",
		Description: "Patches a rule (an absent field is unchanged), bumping its version; disabling it stops new opens. Criteria take {expr} or the short form {op, value}, compiled as on create (a 422 names the criteria, line and column). An empty severity makes the rule informational and an empty clear_event_type drops its clear event; the fire event type cannot be emptied (422). Official rules are read-only (409). Audited. Gated by rule:update, with an all-scope grant.",
	}, "rule", "update"), func(ctx context.Context, in *updateEventRuleInput) (*eventRuleOutput, error) {
		b := in.Body
		patch := storage.EventRulePatch{Label: b.Label, Description: b.Description, SampleKey: b.SampleKey,
			ClearNegates: b.ClearNegates, Severity: b.Severity, Message: b.Message, Enabled: b.Enabled,
			FireEventType: b.FireEventType, ClearEventType: b.ClearEventType}
		if b.Selector != nil {
			patch.Selector = &storage.EventRuleSelector{ComponentTypes: b.Selector.ComponentTypes, Components: b.Selector.Components}
		}
		if b.FireCriteria != nil {
			c, err := fromCriteriaBody(*b.FireCriteria)
			if err != nil {
				return nil, err
			}
			patch.Fire = &c
		}
		if b.ClearCriteria != nil {
			c, err := fromCriteriaBody(*b.ClearCriteria)
			if err != nil {
				return nil, err
			}
			patch.Clear = &c
		}
		if b.FireSustain != nil {
			d, err := parseSustain("fire_sustain", *b.FireSustain)
			if err != nil {
				return nil, err
			}
			patch.FireSustain = &d
		}
		if b.ClearSustain != nil {
			d, err := parseSustain("clear_sustain", *b.ClearSustain)
			if err != nil {
				return nil, err
			}
			patch.ClearSustain = &d
		}
		r, err := gw.UpdateEventRule(ctx, actorID(ctx), in.Name, patch, a.scopeFor(ctx, "rule", "update"))
		if err != nil {
			return nil, mapEventRuleErr(err)
		}
		return rereadEventRule(ctx, a, gw, r.Name)
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-event-rule",
		Method:        http.MethodDelete,
		Path:          "/event-rules/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete an event rule",
		Description:   "Removes an operator rule; an official rule is read-only (409). Audited. Gated by rule:delete, with an all-scope grant.",
	}, "rule", "delete"), func(ctx context.Context, in *eventRuleNameInput) (*struct{}, error) {
		if err := gw.DeleteEventRule(ctx, actorID(ctx), in.Name, a.scopeFor(ctx, "rule", "delete")); err != nil {
			return nil, mapEventRuleErr(err)
		}
		return nil, nil
	})
}

type ruleOpenAlarmBody struct {
	AlarmID     string    `json:"alarm_id"`
	Component   string    `json:"component" doc:"The component, by name"`
	ComponentID string    `json:"component_id" doc:"The component's uuid"`
	Instance    string    `json:"instance,omitempty" doc:"The sample instance (an endpoint) the alarm is about"`
	Severity    string    `json:"severity"`
	RaisedAt    time.Time `json:"raised_at"`
}

type ruleStreakBody struct {
	Component   string    `json:"component" doc:"The component, by name"`
	ComponentID string    `json:"component_id" doc:"The component's uuid"`
	Instance    string    `json:"instance,omitempty"`
	Pending     string    `json:"pending" enum:"fire,clear" doc:"The action the rule's samples point at, waiting out its sustain"`
	Since       time.Time `json:"since" doc:"When the streak began"`
}

type ruleConditionBody struct {
	Component   string    `json:"component" doc:"The component, by name"`
	ComponentID string    `json:"component_id" doc:"The component's uuid"`
	Instance    string    `json:"instance,omitempty" doc:"The sample instance (an endpoint) the condition is on"`
	Since       time.Time `json:"since" doc:"When the condition came into force"`
}

type eventRuleStateOutput struct {
	Body struct {
		Conditions []ruleConditionBody `json:"conditions" doc:"The conditions the rule holds in force, informational or not"`
		Open       []ruleOpenAlarmBody `json:"open" doc:"The alarms the rule raised that are still open"`
		Pending    []ruleStreakBody    `json:"pending" doc:"The rule's streaks waiting out a sustain"`
		// LastJudgedAt names no component, so it is not fenced by the read scope.
		LastJudgedAt *time.Time `json:"last_judged_at,omitempty" required:"false" doc:"When an ingested sample was last judged by the rule, to within 5s; absent when it never has been"`
		// CriteriaError names no component either.
		CriteriaError *criteriaErrorBody `json:"criteria_error,omitempty" required:"false" doc:"Why the rule's stored criteria do not compile; absent when they compile. While present the rule holds"`
	}
}

func registerEventRuleStateRoute(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-event-rule-state",
		Method:      http.MethodGet,
		Path:        "/event-rules/{name}/state",
		Summary:     "Get an event rule's live state",
		Description: "What a rule holds right now: the conditions it holds in force (one per component and instance, whether or not it raises an alarm), the alarms it raised that are still open, each on a component and instance, its streaks waiting out a sustain, and when it last judged an ingested sample. Only conditions, alarms and streaks on components the caller may read are listed. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, in *eventRuleNameInput) (*eventRuleStateOutput, error) {
		st, err := gw.EventRuleState(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapEventRuleErr(err)
		}
		out := &eventRuleStateOutput{}
		out.Body.LastJudgedAt = st.LastJudgedAt
		out.Body.CriteriaError = toCriteriaErrorBody(st.CriteriaError)
		out.Body.Conditions = make([]ruleConditionBody, 0, len(st.Conditions))
		for _, k := range st.Conditions {
			out.Body.Conditions = append(out.Body.Conditions, ruleConditionBody{Component: k.ComponentName, ComponentID: k.ComponentID,
				Instance: k.Instance, Since: k.Since})
		}
		out.Body.Open = make([]ruleOpenAlarmBody, 0, len(st.Open))
		for _, o := range st.Open {
			out.Body.Open = append(out.Body.Open, ruleOpenAlarmBody{AlarmID: o.AlarmID, Component: o.ComponentName,
				ComponentID: o.ComponentID, Instance: o.Instance, Severity: o.Severity, RaisedAt: o.RaisedAt})
		}
		out.Body.Pending = make([]ruleStreakBody, 0, len(st.Pending))
		for _, s := range st.Pending {
			out.Body.Pending = append(out.Body.Pending, ruleStreakBody{Component: s.ComponentName, ComponentID: s.ComponentID,
				Instance: s.Instance, Pending: s.Pending, Since: s.Since})
		}
		return out, nil
	})
}

// rereadEventRule answers a write with the rule as the caller reads it: the
// write's own counts are fleet-wide, and the caller's component read scope may
// be narrower than its all-scope rule grant.
func rereadEventRule(ctx context.Context, a *authenticator, gw storage.Gateway, name string) (*eventRuleOutput, error) {
	r, err := gw.GetEventRule(ctx, name, a.scopeFor(ctx, "component", "read"))
	if err != nil {
		return nil, mapEventRuleErr(err)
	}
	return &eventRuleOutput{Body: toEventRuleBody(r)}, nil
}

// criteriaProblem is a 422 for a criterion that did not compile, located at
// the criteria's expression (or at the criteria, when the body gave the short
// form), carrying the line and column of the fault when it has one.
func criteriaProblem(ce *rules.CriteriaError) error {
	loc := "body." + ce.Field
	if !ce.Short {
		loc += ".expr"
	}
	d := &huma.ErrorDetail{Location: loc, Message: ce.Err.Error()}
	var ee *expression.Error
	if errors.As(ce, &ee) {
		d.Message = ee.Message
		if ee.Line > 0 {
			d.Value = map[string]any{"line": ee.Line, "column": ee.Column, "phase": string(ee.Phase)}
		}
	}
	return huma.Error422UnprocessableEntity(ce.Field+": "+d.Message, d)
}

func mapEventRuleErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrEventRuleNotFound):
		return huma.Error404NotFound("event rule not found")
	case errors.Is(err, storage.ErrEventRuleOfficial):
		return huma.Error409Conflict("an official event rule is read-only")
	case errors.Is(err, storage.ErrEventRuleExists):
		return huma.Error409Conflict("an event rule already has that name")
	case errors.Is(err, storage.ErrEventRuleForbidden):
		return huma.Error403Forbidden("writing an event rule needs an all-scope grant: its selector reaches the whole fleet")
	case errors.Is(err, storage.ErrEventRuleInvalid):
		var ce *rules.CriteriaError
		if errors.As(err, &ce) {
			return criteriaProblem(ce)
		}
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return huma.Error500InternalServerError("event rule operation failed")
}
