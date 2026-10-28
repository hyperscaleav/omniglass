package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The personal action rule surface (#1341, #1353): a user's own subscription
// to the alarms they can see, told to them alone on the channels it names.
// Every route is gated by notification:subscribe (granted to viewer, so every
// person who can see alarms can subscribe, and a custom role can withhold it)
// and is self-scoped: the ownership predicate rides every query, so another
// user's rule is a non-disclosing 404. A rule only ever matches an alarm on a
// component the owner's component read scope admits, judged before its when
// runs.

type personalRuleBody struct {
	ID                 string    `json:"id" doc:"The rule's uuid, the stable form of name"`
	Name               string    `json:"name" doc:"The rule's name, unique among your own rules"`
	Label              string    `json:"label,omitempty"`
	Description        string    `json:"description,omitempty"`
	On                 []string  `json:"on" doc:"The alarm transitions it fires on: raised, cleared, or both"`
	When               string    `json:"when,omitempty" doc:"The predicate over the alarm facts (the action expression site); absent always holds"`
	NotificationType   string    `json:"notification_type,omitempty" doc:"The notification type it sends, by name; absent sends the official one for the transition"`
	NotificationTypeID string    `json:"notification_type_id,omitempty" doc:"That type's uuid"`
	Channels           []string  `json:"channels" doc:"How it tells you: inbox (your console inbox), email (your directory email)"`
	Enabled            bool      `json:"enabled" doc:"Whether it fires; a paused rule queues nothing"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func toPersonalRuleBody(r *storage.ActionRule) personalRuleBody {
	chs := r.Channels
	if chs == nil {
		chs = []string{}
	}
	return personalRuleBody{ID: r.ID, Name: r.Name, Label: r.Label, Description: r.Description, On: r.On, When: r.When,
		NotificationType: r.NotificationType, NotificationTypeID: r.NotificationTypeID, Channels: chs,
		Enabled: r.Enabled, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

type listPersonalRulesOutput struct {
	Body struct {
		ActionRules []personalRuleBody `json:"action_rules"`
	}
}

type personalRuleOutput struct{ Body personalRuleBody }

type createPersonalRuleInput struct {
	Body struct {
		Name             string   `json:"name" minLength:"1" doc:"The rule's name (lowercase kebab), unique among your own rules"`
		Label            string   `json:"label,omitempty" required:"false" doc:"A human label"`
		Description      string   `json:"description,omitempty" required:"false" doc:"What it is for"`
		On               []string `json:"on" minItems:"1" doc:"The alarm transitions it fires on: raised, cleared, or both"`
		When             string   `json:"when,omitempty" required:"false" doc:"A predicate over the alarm facts (component, place, severity, severity_rank, message, rule, ...); absent always holds. Judged only for alarms on components you can read"`
		NotificationType string   `json:"notification_type,omitempty" required:"false" doc:"The notification type to send, by name or uuid; absent sends the official one for the transition"`
		Channels         []string `json:"channels" minItems:"1" doc:"How it tells you: inbox, email (your directory email)"`
		Targets          []string `json:"targets,omitempty" required:"false" doc:"Refused when set (a 422): a personal rule tells you alone and names no target"`
		Enabled          *bool    `json:"enabled,omitempty" required:"false" doc:"Whether it fires (default true)"`
	}
}

type personalRuleRefInput struct {
	Name string `path:"name" doc:"Your rule's name or uuid"`
}

type updatePersonalRuleInput struct {
	Name string `path:"name" doc:"Your rule's name or uuid"`
	Body struct {
		Label            *string   `json:"label,omitempty" required:"false" doc:"A human label (empty clears it)"`
		Description      *string   `json:"description,omitempty" required:"false" doc:"What it is for"`
		On               *[]string `json:"on,omitempty" required:"false" doc:"The transitions it fires on (replaces the set)"`
		When             *string   `json:"when,omitempty" required:"false" doc:"The predicate (empty always holds)"`
		NotificationType *string   `json:"notification_type,omitempty" required:"false" doc:"The type to send (empty returns to the official one per transition)"`
		Channels         *[]string `json:"channels,omitempty" required:"false" doc:"How it tells you (replaces the set)"`
		Enabled          *bool     `json:"enabled,omitempty" required:"false" doc:"Whether it fires: false pauses it, true resumes it from the next transition"`
	}
}

type testPersonalRuleInput struct {
	Name string `path:"name" doc:"Your rule's name or uuid"`
	Body *struct {
		On    *[]string `json:"on,omitempty" required:"false" doc:"A draft of the transitions to judge with, in place of the rule's own"`
		When  *string   `json:"when,omitempty" required:"false" doc:"A draft of the predicate to judge with, in place of the rule's own"`
		Limit int       `json:"limit,omitempty" required:"false" minimum:"0" maximum:"50" doc:"How many of your most recent alarms to judge (default 20)"`
	} `required:"false"`
}

type ruleMatchBody struct {
	Alarm      string    `json:"alarm" doc:"The alarm's uuid"`
	Component  string    `json:"component" doc:"The alarm's component, by name"`
	Severity   string    `json:"severity"`
	Message    string    `json:"message"`
	Transition string    `json:"transition" enum:"raised,cleared" doc:"The edge judged"`
	At         time.Time `json:"at" doc:"When that edge happened"`
	Matched    bool      `json:"matched" doc:"Whether the rule would have told you"`
	Error      string    `json:"error,omitempty" doc:"Why the when did not run, when it did not"`
}

type testPersonalRuleOutput struct {
	Body struct {
		Matches []ruleMatchBody `json:"matches" doc:"Your most recent alarms on components you can read, newest first, each transition the rule fires on that the alarm has had"`
	}
}

func registerPersonalRuleRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-auth-me-action-rules",
		Method:      http.MethodGet,
		Path:        "/auth/me/action-rules",
		Summary:     "List your alarm subscriptions",
		Description: "Your own personal action rules, by name. Self-scoped. Gated by notification:subscribe.",
	}, "notification", "subscribe"), func(ctx context.Context, _ *struct{}) (*listPersonalRulesOutput, error) {
		rs, err := gw.ListPersonalRules(ctx, callerID(ctx))
		if err != nil {
			return nil, mapPersonalRuleErr(err)
		}
		out := &listPersonalRulesOutput{}
		out.Body.ActionRules = make([]personalRuleBody, 0, len(rs))
		for i := range rs {
			out.Body.ActionRules = append(out.Body.ActionRules, toPersonalRuleBody(&rs[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-auth-me-action-rule",
		Method:      http.MethodGet,
		Path:        "/auth/me/action-rules/{name}",
		Summary:     "Get one of your alarm subscriptions",
		Description: "One of your own personal action rules, by name or uuid; another user's is a 404. Gated by notification:subscribe.",
	}, "notification", "subscribe"), func(ctx context.Context, in *personalRuleRefInput) (*personalRuleOutput, error) {
		r, err := gw.GetPersonalRule(ctx, callerID(ctx), in.Name)
		if err != nil {
			return nil, mapPersonalRuleErr(err)
		}
		return &personalRuleOutput{Body: toPersonalRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-auth-me-action-rule",
		Method:        http.MethodPost,
		Path:          "/auth/me/action-rules",
		DefaultStatus: http.StatusCreated,
		Summary:       "Subscribe yourself to alarms",
		Description:   "Creates a personal action rule: on which alarm transitions, when, what to send, and on which of your channels (inbox, email). It tells you alone and names no target (targets is a 422), and it only ever matches an alarm on a component you can read at that moment, judged before its when runs. A taken name among your rules, or a 51st rule, is a 409. Audited. Gated by notification:subscribe.",
	}, "notification", "subscribe"), func(ctx context.Context, in *createPersonalRuleInput) (*personalRuleOutput, error) {
		r, err := gw.CreatePersonalRule(ctx, actorID(ctx), callerID(ctx), storage.PersonalRuleSpec{
			Name: in.Body.Name, Label: in.Body.Label, Description: in.Body.Description, On: in.Body.On, When: in.Body.When,
			NotificationType: in.Body.NotificationType, Channels: in.Body.Channels, Targets: in.Body.Targets, Enabled: in.Body.Enabled,
		})
		if err != nil {
			return nil, mapPersonalRuleErr(err)
		}
		return &personalRuleOutput{Body: toPersonalRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-auth-me-action-rule",
		Method:      http.MethodPatch,
		Path:        "/auth/me/action-rules/{name}",
		Summary:     "Change or pause one of your alarm subscriptions",
		Description: "Patches one of your own personal action rules (an absent field is unchanged); enabled false pauses it. Another user's is a 404. Audited. Gated by notification:subscribe.",
	}, "notification", "subscribe"), func(ctx context.Context, in *updatePersonalRuleInput) (*personalRuleOutput, error) {
		r, err := gw.UpdatePersonalRule(ctx, actorID(ctx), callerID(ctx), in.Name, storage.PersonalRulePatch{
			Label: in.Body.Label, Description: in.Body.Description, On: in.Body.On, When: in.Body.When,
			NotificationType: in.Body.NotificationType, Channels: in.Body.Channels, Enabled: in.Body.Enabled,
		})
		if err != nil {
			return nil, mapPersonalRuleErr(err)
		}
		return &personalRuleOutput{Body: toPersonalRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-auth-me-action-rule",
		Method:        http.MethodDelete,
		Path:          "/auth/me/action-rules/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Unsubscribe: delete one of your alarm subscriptions",
		Description:   "Deletes one of your own personal action rules; what it already sent keeps its name. Another user's is a 404. Audited. Gated by notification:subscribe.",
	}, "notification", "subscribe"), func(ctx context.Context, in *personalRuleRefInput) (*struct{}, error) {
		if err := gw.DeletePersonalRule(ctx, actorID(ctx), callerID(ctx), in.Name); err != nil {
			return nil, mapPersonalRuleErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "test-auth-me-action-rule",
		Method:      http.MethodPost,
		Path:        "/auth/me/action-rules/{name}:test",
		Summary:     "Test one of your alarm subscriptions against your recent alarms",
		Description: "Judges one of your own personal action rules, or a draft of its on and when, against your most recent alarms on components you can read: for each transition it fires on, whether it would have told you. Writes and sends nothing. Another user's rule is a 404. Gated by notification:subscribe.",
	}, "notification", "subscribe"), func(ctx context.Context, in *testPersonalRuleInput) (*testPersonalRuleOutput, error) {
		var draft storage.RuleDraft
		limit := 0
		if in.Body != nil {
			draft = storage.RuleDraft{On: in.Body.On, When: in.Body.When}
			limit = in.Body.Limit
		}
		ms, err := gw.TestPersonalRule(ctx, callerID(ctx), in.Name, draft, a.scopeFor(ctx, "component", "read"), limit)
		if err != nil {
			return nil, mapPersonalRuleErr(err)
		}
		out := &testPersonalRuleOutput{}
		out.Body.Matches = make([]ruleMatchBody, 0, len(ms))
		for _, m := range ms {
			out.Body.Matches = append(out.Body.Matches, ruleMatchBody{Alarm: m.AlarmID, Component: m.ComponentName, Severity: m.Severity,
				Message: m.Message, Transition: m.Transition, At: m.At, Matched: m.Matched, Error: m.Error})
		}
		return out, nil
	})
}

func mapPersonalRuleErr(err error) error {
	if errors.Is(err, storage.ErrPersonalRuleCap) {
		return huma.Error409Conflict("you own the most personal rules a person may (50); delete one first")
	}
	if errors.Is(err, storage.ErrPersonalRuleOwner) {
		return huma.Error422UnprocessableEntity("a personal rule belongs to a person: a service account or a node is no one to tell")
	}
	return mapActionRuleErr(err)
}

type optOutOutput struct {
	Body struct {
		ActionRule string `json:"action_rule" doc:"The rule, by name"`
		OptedOut   bool   `json:"opted_out" doc:"Whether you are now opted out of it"`
	}
}

// registerOptOutRoutes wires group opt-out (#1354): a person opts out of a
// platform rule an admin marked opt_out_allowed, for themselves, and back
// in. Gated by notification:subscribe; self-scoped.
func registerOptOutRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	set := func(out bool) func(context.Context, *actionRuleRefInput) (*optOutOutput, error) {
		return func(ctx context.Context, in *actionRuleRefInput) (*optOutOutput, error) {
			r, err := gw.SetRuleOptOut(ctx, actorID(ctx), callerID(ctx), in.Name, out)
			if err != nil {
				if errors.Is(err, storage.ErrOptOutNotAllowed) {
					return nil, huma.Error409Conflict("this action rule does not allow opting out; an admin sets opt_out_allowed on it")
				}
				return nil, mapActionRuleErr(err)
			}
			o := &optOutOutput{}
			o.Body.ActionRule, o.Body.OptedOut = r.Name, out
			return o, nil
		}
	}
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "opt-out-action-rule",
		Method:      http.MethodPost,
		Path:        "/action-rules/{name}:optOut",
		Summary:     "Opt yourself out of a group notification",
		Description: "Stops a platform action rule telling you through a group it targets, for you alone. Only a rule an admin marked opt_out_allowed (a 409 otherwise); a rule that names you directly still tells you. Idempotent and audited. Gated by notification:subscribe.",
	}, "notification", "subscribe"), set(true))
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "opt-in-action-rule",
		Method:      http.MethodPost,
		Path:        "/action-rules/{name}:optIn",
		Summary:     "Opt yourself back in to a group notification",
		Description: "Undoes your opt-out of a platform action rule; always allowed. Idempotent and audited. Gated by notification:subscribe.",
	}, "notification", "subscribe"), set(false))
}
