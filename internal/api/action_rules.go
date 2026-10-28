package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The action rule surface (#618, #1321): the subscription that wires an
// alarm transition to a notification. A rule, so it rides rule:* (decision 1
// of #618): reads registry-shaped on the viewer floor, writes all-scope like
// an event rule's, since a rule watches the whole fleet.

type actionRuleBody struct {
	ID                 string    `json:"id" doc:"The rule's uuid, the stable form of name"`
	Name               string    `json:"name"`
	Label              string    `json:"label,omitempty"`
	Description        string    `json:"description,omitempty"`
	On                 []string  `json:"on" doc:"The alarm transitions it fires on: raised, cleared, or both"`
	When               string    `json:"when,omitempty" doc:"The predicate over the alarm facts (the action expression site); absent always holds"`
	NotificationType   string    `json:"notification_type,omitempty" doc:"The notification type it sends, by name; absent sends the official one for the transition (alarm-raised, alarm-cleared)"`
	NotificationTypeID string    `json:"notification_type_id,omitempty" doc:"That type's uuid"`
	Targets            []string  `json:"targets" doc:"The notification targets it sends to, by name"`
	Enabled            bool      `json:"enabled"`
	Official           bool      `json:"official"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func toActionRuleBody(r *storage.ActionRule) actionRuleBody {
	targets := r.Targets
	if targets == nil {
		targets = []string{}
	}
	return actionRuleBody{ID: r.ID, Name: r.Name, Label: r.Label, Description: r.Description, On: r.On, When: r.When,
		NotificationType: r.NotificationType, NotificationTypeID: r.NotificationTypeID, Targets: targets,
		Enabled: r.Enabled, Official: r.Official, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

type listActionRulesOutput struct {
	Body struct {
		ActionRules []actionRuleBody `json:"action_rules"`
	}
}

type actionRuleOutput struct{ Body actionRuleBody }

type actionRuleRefInput struct {
	Name string `path:"name" doc:"The rule's name or uuid"`
}

type createActionRuleInput struct {
	Body struct {
		Name             string   `json:"name" minLength:"1" doc:"The rule's name (lowercase kebab)"`
		Label            string   `json:"label,omitempty" required:"false" doc:"A human label"`
		Description      string   `json:"description,omitempty" required:"false" doc:"What it is for"`
		On               []string `json:"on" minItems:"1" doc:"The alarm transitions it fires on: raised, cleared, or both. Acknowledging an alarm is never one"`
		When             string   `json:"when,omitempty" required:"false" doc:"A predicate over the alarm facts at the action expression site (component, place, severity, severity_rank, message, transition, rule, ...; see /expressions:validate with site action); absent always holds. A refusal is a 422 with its line and column"`
		NotificationType string   `json:"notification_type,omitempty" required:"false" doc:"The notification type to send, by name or uuid; absent sends the official one for the transition"`
		Targets          []string `json:"targets" minItems:"1" doc:"The notification targets to send to, by name or uuid"`
		Enabled          *bool    `json:"enabled,omitempty" required:"false" doc:"Whether it fires (default true): a disabled rule queues nothing on a transition, and what it queued before still delivers"`
	}
}

type updateActionRuleInput struct {
	Name string `path:"name" doc:"The rule's name or uuid"`
	Body struct {
		Label            *string   `json:"label,omitempty" required:"false" doc:"A human label (empty clears it)"`
		Description      *string   `json:"description,omitempty" required:"false" doc:"What it is for"`
		On               *[]string `json:"on,omitempty" required:"false" doc:"The transitions it fires on (replaces the set)"`
		When             *string   `json:"when,omitempty" required:"false" doc:"The predicate (empty always holds)"`
		NotificationType *string   `json:"notification_type,omitempty" required:"false" doc:"The type to send (empty returns to the official one per transition)"`
		Targets          *[]string `json:"targets,omitempty" required:"false" doc:"The targets (replaces the set)"`
		Enabled          *bool     `json:"enabled,omitempty" required:"false" doc:"Whether it fires: a disabled rule queues nothing on a transition, and what it queued before still delivers; re-enabled, it acts on the next transition"`
	}
}

func registerActionRuleRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-action-rule",
		Method:      http.MethodGet,
		Path:        "/action-rules",
		Summary:     "List action rules",
		Description: "Lists every action rule, by name: on which alarm transitions, when, and what it sends to which targets. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, _ *struct{}) (*listActionRulesOutput, error) {
		rs, err := gw.ListActionRules(ctx)
		if err != nil {
			return nil, mapActionRuleErr(err)
		}
		out := &listActionRulesOutput{}
		out.Body.ActionRules = make([]actionRuleBody, 0, len(rs))
		for i := range rs {
			out.Body.ActionRules = append(out.Body.ActionRules, toActionRuleBody(&rs[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-action-rule",
		Method:      http.MethodGet,
		Path:        "/action-rules/{name}",
		Summary:     "Get an action rule",
		Description: "Returns one action rule by name or uuid. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, in *actionRuleRefInput) (*actionRuleOutput, error) {
		r, err := gw.GetActionRule(ctx, in.Name)
		if err != nil {
			return nil, mapActionRuleErr(err)
		}
		return &actionRuleOutput{Body: toActionRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-action-rule",
		Method:        http.MethodPost,
		Path:          "/action-rules",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create an action rule",
		Description:   "Adds a rule that, on an alarm's raise or clear, when its predicate holds over the alarm facts, queues one notification per target in the transition's own transaction, which the server's tick then delivers. An unknown transition, type or target, or a predicate that does not compile, is a 422; a taken name a 409. Audited. Gated by rule:create, with an all-scope grant: a rule watches the whole fleet.",
	}, "rule", "create"), func(ctx context.Context, in *createActionRuleInput) (*actionRuleOutput, error) {
		r, err := gw.CreateActionRule(ctx, actorID(ctx), storage.ActionRuleSpec{
			Name: in.Body.Name, Label: in.Body.Label, Description: in.Body.Description, On: in.Body.On, When: in.Body.When,
			NotificationType: in.Body.NotificationType, Targets: in.Body.Targets, Enabled: in.Body.Enabled,
		}, a.scopeFor(ctx, "rule", "create"))
		if err != nil {
			return nil, mapActionRuleErr(err)
		}
		return &actionRuleOutput{Body: toActionRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-action-rule",
		Method:      http.MethodPatch,
		Path:        "/action-rules/{name}",
		Summary:     "Update an action rule",
		Description: "Patches a rule (an absent field is unchanged; on and targets replace their sets). It acts on transitions from the next one; a notification already queued is not re-rendered. Audited. Gated by rule:update, with an all-scope grant.",
	}, "rule", "update"), func(ctx context.Context, in *updateActionRuleInput) (*actionRuleOutput, error) {
		r, err := gw.UpdateActionRule(ctx, actorID(ctx), in.Name, storage.ActionRulePatch{
			Label: in.Body.Label, Description: in.Body.Description, On: in.Body.On, When: in.Body.When,
			NotificationType: in.Body.NotificationType, Targets: in.Body.Targets, Enabled: in.Body.Enabled,
		}, a.scopeFor(ctx, "rule", "update"))
		if err != nil {
			return nil, mapActionRuleErr(err)
		}
		return &actionRuleOutput{Body: toActionRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-action-rule",
		Method:        http.MethodDelete,
		Path:          "/action-rules/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete an action rule",
		Description:   "Removes a rule; the notifications it queued keep its name. Audited. Gated by rule:delete, with an all-scope grant.",
	}, "rule", "delete"), func(ctx context.Context, in *actionRuleRefInput) (*struct{}, error) {
		if err := gw.DeleteActionRule(ctx, actorID(ctx), in.Name, a.scopeFor(ctx, "rule", "delete")); err != nil {
			return nil, mapActionRuleErr(err)
		}
		return nil, nil
	})
}

func mapActionRuleErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrActionRuleNotFound):
		return huma.Error404NotFound("action rule not found")
	case errors.Is(err, storage.ErrActionRuleExists):
		return huma.Error409Conflict("an action rule with this name already exists")
	case errors.Is(err, storage.ErrActionRuleForbidden):
		return huma.Error403Forbidden("writing an action rule needs an all-scope grant: a rule watches the whole fleet")
	case errors.Is(err, storage.ErrActionRuleInvalid):
		msg := strings.TrimPrefix(err.Error(), "storage: action rule is invalid: ")
		var ee *expression.Error
		if errors.As(err, &ee) && ee.Line > 0 {
			return huma.Error422UnprocessableEntity(msg, &huma.ErrorDetail{Location: "body.when", Message: ee.Message,
				Value: map[string]any{"line": ee.Line, "column": ee.Column, "phase": string(ee.Phase)}})
		}
		return huma.Error422UnprocessableEntity(msg)
	}
	return mapTypeErr(err, "action rule")
}
