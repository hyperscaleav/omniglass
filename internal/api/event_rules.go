package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type criteriaBody struct {
	Op    string `json:"op" enum:"eq,ne,gt,ge,lt,le" doc:"The comparison: a property compares text with eq or ne; a metric compares numbers with any of them"`
	Value any    `json:"value" doc:"The threshold: text for a property, a number for a metric"`
}

type selectorBody struct {
	ComponentTypes []string `json:"component_types,omitempty" required:"false" doc:"Watch components whose type falls within one of these component types"`
	Components     []string `json:"components,omitempty" required:"false" doc:"Watch these components (by name or id on write; ids on read)"`
}

type eventRuleBody struct {
	ID             string        `json:"id" doc:"The rule's uuid, the stable form of name"`
	Name           string        `json:"name"`
	Label          string        `json:"label,omitempty"`
	Description    string        `json:"description,omitempty"`
	Selector       selectorBody  `json:"selector" doc:"The components the rule watches; both lists empty is every component"`
	SampleKey      string        `json:"sample_key" doc:"The property or metric the rule reads"`
	SampleLane     string        `json:"sample_lane" enum:"property,metric" doc:"Which lane the key reads, derived from the registries"`
	FireCriteria   criteriaBody  `json:"fire_criteria" doc:"When the sample meets this for fire_sustain, the rule opens an alarm"`
	ClearCriteria  *criteriaBody `json:"clear_criteria,omitempty" doc:"When the sample meets this for clear_sustain, the alarm clears; absent, the negation of fire_criteria"`
	FireSustain    string        `json:"fire_sustain" doc:"How long the fire criteria must hold, a Go duration (0s fires at once)"`
	ClearSustain   string        `json:"clear_sustain" doc:"How long the clear criteria must hold, a Go duration"`
	Severity       string        `json:"severity" doc:"The severity level the alarm is raised at, by name"`
	Message        string        `json:"message,omitempty" doc:"The alarm's message; empty uses the rule's label or name"`
	Enabled        bool          `json:"enabled"`
	Official       bool          `json:"official" doc:"Shipped with the platform and read-only"`
	Version        int           `json:"version" doc:"Bumps on every update"`
	OpenAlarms     int           `json:"open_alarms" doc:"How many alarms the rule raised are still open right now (its live state; GET /event-rules/{name}/state lists them)"`
	PendingStreaks int           `json:"pending_streaks" doc:"How many of the rule's streaks are waiting out a sustain right now"`
}

func toCriteriaBody(c rules.Criteria) criteriaBody {
	var v any
	_ = json.Unmarshal(c.Value, &v)
	return criteriaBody{Op: c.Op, Value: v}
}

func fromCriteriaBody(c criteriaBody) (rules.Criteria, error) {
	b, err := json.Marshal(c.Value)
	if err != nil {
		return rules.Criteria{}, huma.Error422UnprocessableEntity("criteria value is not JSON")
	}
	return rules.Criteria{Op: c.Op, Value: b}, nil
}

func toEventRuleBody(r *storage.EventRule) eventRuleBody {
	b := eventRuleBody{ID: r.ID, Name: r.Name, Label: r.Label, Description: r.Description,
		Selector:  selectorBody{ComponentTypes: r.Selector.ComponentTypes, Components: r.Selector.Components},
		SampleKey: r.SampleKey, SampleLane: string(r.SampleLane), FireCriteria: toCriteriaBody(r.FireCriteria),
		FireSustain: r.FireSustain.String(), ClearSustain: r.ClearSustain.String(), Severity: r.Severity,
		Message: r.Message, Enabled: r.Enabled, Official: r.Official, Version: r.Version,
		OpenAlarms: r.OpenAlarms, PendingStreaks: r.PendingStreaks}
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
		Name          string        `json:"name" minLength:"1" doc:"The rule's name (lowercase kebab)"`
		Label         string        `json:"label,omitempty" required:"false" doc:"A human label; also the alarm message when message is empty"`
		Description   string        `json:"description,omitempty" required:"false" doc:"What the rule watches for and why"`
		Selector      *selectorBody `json:"selector,omitempty" required:"false" doc:"The components the rule watches (component types and components); absent is every component"`
		SampleKey     string        `json:"sample_key" minLength:"1" doc:"The property or metric the rule reads"`
		FireCriteria  criteriaBody  `json:"fire_criteria" doc:"The criteria that opens an alarm: {op, value}"`
		ClearCriteria *criteriaBody `json:"clear_criteria,omitempty" required:"false" doc:"The criteria that clears it: {op, value}; absent clears on the negation of fire_criteria"`
		FireSustain   string        `json:"fire_sustain,omitempty" required:"false" doc:"A Go duration; empty is 0s"`
		ClearSustain  string        `json:"clear_sustain,omitempty" required:"false" doc:"A Go duration; empty is 0s"`
		Severity      string        `json:"severity" minLength:"1" doc:"The severity level the alarm is raised at, by name"`
		Message       string        `json:"message,omitempty" required:"false" doc:"The alarm message; empty uses the label or the name"`
		Enabled       *bool         `json:"enabled,omitempty" required:"false" doc:"Defaults to true"`
	}
}

type updateEventRuleInput struct {
	Name string `path:"name" doc:"The rule's name"`
	Body struct {
		Label         *string       `json:"label,omitempty" required:"false" doc:"A human label; also the alarm message when message is empty"`
		Description   *string       `json:"description,omitempty" required:"false" doc:"What the rule watches for and why"`
		Selector      *selectorBody `json:"selector,omitempty" required:"false" doc:"Replace the components the rule watches; absent leaves them unchanged, and an empty selector watches every component"`
		SampleKey     *string       `json:"sample_key,omitempty" required:"false" doc:"The property or metric the rule reads"`
		FireCriteria  *criteriaBody `json:"fire_criteria,omitempty" required:"false" doc:"The criteria that opens an alarm: {op, value}"`
		ClearCriteria *criteriaBody `json:"clear_criteria,omitempty" required:"false" doc:"The criteria that clears it: {op, value}; absent clears on the negation of fire_criteria"`
		ClearNegates  bool          `json:"clear_negates,omitempty" required:"false" doc:"Drop the clear criteria, so the rule clears on the negation of its fire criteria"`
		FireSustain   *string       `json:"fire_sustain,omitempty" required:"false" doc:"How long the fire criteria must hold, a Go duration (e.g. 30s); empty is 0s"`
		ClearSustain  *string       `json:"clear_sustain,omitempty" required:"false" doc:"How long the clear criteria must hold, a Go duration; empty is 0s"`
		Severity      *string       `json:"severity,omitempty" required:"false" doc:"The severity level the alarm is raised at, by name"`
		Message       *string       `json:"message,omitempty" required:"false" doc:"The alarm message; empty uses the label or the name"`
		Enabled       *bool         `json:"enabled,omitempty" required:"false" doc:"Whether the rule evaluates; defaults to true on create"`
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
		Description: "Lists the event rules, the authored config that raises alarms, each with its live state as counts (open_alarms, pending_streaks) read in the same query. Rules read like a registry: every rule is listed whatever the caller's scope, while the counts cover only components the caller may read. Gated by rule:read.",
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
		Description:   "Authors a rule that raises an alarm on each component it watches when its sample key meets the fire criteria for the fire sustain, and clears it when the clear criteria (or the negation of fire) holds for the clear sustain. The key must be a registered property or metric, the criteria evaluable on its lane, and the severity a level that exists (else 422); a name already taken is a 409. Audited. Gated by rule:create, with an all-scope grant.",
	}, "rule", "create"), func(ctx context.Context, in *createEventRuleInput) (*eventRuleOutput, error) {
		b := in.Body
		fire, err := fromCriteriaBody(b.FireCriteria)
		if err != nil {
			return nil, err
		}
		spec := storage.EventRuleSpec{Name: b.Name, Label: b.Label, Description: b.Description, SampleKey: b.SampleKey,
			Fire: fire, Severity: b.Severity, Message: b.Message, Enabled: b.Enabled}
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
		Description: "Patches a rule (an absent field is unchanged), bumping its version; disabling it stops new opens. Official rules are read-only (409). Audited. Gated by rule:update, with an all-scope grant.",
	}, "rule", "update"), func(ctx context.Context, in *updateEventRuleInput) (*eventRuleOutput, error) {
		b := in.Body
		patch := storage.EventRulePatch{Label: b.Label, Description: b.Description, SampleKey: b.SampleKey,
			ClearNegates: b.ClearNegates, Severity: b.Severity, Message: b.Message, Enabled: b.Enabled}
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

type eventRuleStateOutput struct {
	Body struct {
		Open    []ruleOpenAlarmBody `json:"open" doc:"The alarms the rule raised that are still open"`
		Pending []ruleStreakBody    `json:"pending" doc:"The rule's streaks waiting out a sustain"`
	}
}

func registerEventRuleStateRoute(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-event-rule-state",
		Method:      http.MethodGet,
		Path:        "/event-rules/{name}/state",
		Summary:     "Get an event rule's live state",
		Description: "What a rule holds right now: the alarms it raised that are still open, each on a component and instance, and its streaks waiting out a sustain. Only those on components the caller may read are listed. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, in *eventRuleNameInput) (*eventRuleStateOutput, error) {
		st, err := gw.EventRuleState(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapEventRuleErr(err)
		}
		out := &eventRuleStateOutput{}
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
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return huma.Error500InternalServerError("event rule operation failed")
}
