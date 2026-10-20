package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

type previewEventRuleInput struct {
	Body struct {
		SampleKey     string         `json:"sample_key" minLength:"1" doc:"The property or metric the candidate rule reads"`
		Selector      *selectorBody  `json:"selector,omitempty" required:"false" doc:"The components it watches; absent or empty, every component"`
		FireCriteria  criteriaInput  `json:"fire_criteria" doc:"The criteria its condition starts on: {expr}, or the short form {op, value}"`
		ClearCriteria *criteriaInput `json:"clear_criteria,omitempty" required:"false" doc:"The criteria it ends on; absent, the negation of fire_criteria"`
		FireSustain   string         `json:"fire_sustain,omitempty" required:"false" doc:"Accepted so a rule body previews as it is, and ignored: a preview judges each sample alone"`
		ClearSustain  string         `json:"clear_sustain,omitempty" required:"false" doc:"Accepted and ignored, like fire_sustain"`
	}
}

type previewSeriesBody struct {
	Component   string     `json:"component" doc:"The component, by name"`
	ComponentID string     `json:"component_id" doc:"The component's uuid"`
	Instance    string     `json:"instance,omitempty" doc:"The sample instance (an endpoint)"`
	Since       *time.Time `json:"since,omitempty" required:"false" doc:"When the window's fire edge brought it into force; absent when it was in force before the window"`
}

type previewEdgeBody struct {
	Component   string    `json:"component" doc:"The component, by name"`
	ComponentID string    `json:"component_id" doc:"The component's uuid"`
	Instance    string    `json:"instance,omitempty" doc:"The sample instance (an endpoint)"`
	At          time.Time `json:"at" doc:"The sample that moved it"`
	Kind        string    `json:"kind" enum:"fire,clear" doc:"Which edge"`
}

type previewEventRuleOutput struct {
	Body struct {
		InForceNow      []previewSeriesBody `json:"in_force_now" doc:"The series the candidate would hold in force now"`
		Edges           []previewEdgeBody   `json:"edges" doc:"The edges it would have written over the window, in series and time order"`
		Window          string              `json:"window" doc:"How far back it judged, a Go duration"`
		WindowSince     time.Time           `json:"window_since"`
		WindowUntil     time.Time           `json:"window_until"`
		RowCap          int                 `json:"row_cap" doc:"The most rows each of its two reads may take"`
		Truncated       bool                `json:"truncated" doc:"A read hit the row cap: the preview covers only part of the window"`
		SustainsIgnored bool                `json:"sustains_ignored" doc:"Always true: each sample is judged alone, so a sustain that would have held an edge back is not applied"`
	}
}

func registerEventRulePreviewRoute(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "preview-event-rule",
		Method:      http.MethodPost,
		Path:        "/event-rules:preview",
		Summary:     "Preview an event rule",
		Description: "Judges a candidate rule (sample key, selector, criteria) without writing anything: over each series it selects, the newest sample before the last 24 hours and then every sample in them, in order, each alone (sustains are ignored, and the answer says so). Answers the edges it would have written and the series it would hold in force now, only on components the caller may read. Each of its two reads is bounded by a row cap, and a preview cut short says truncated. Criteria take {expr} or {op, value}; one that does not compile is a 422 naming the criteria, line and column. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, in *previewEventRuleInput) (*previewEventRuleOutput, error) {
		b := in.Body
		fire, err := fromCriteriaBody(b.FireCriteria)
		if err != nil {
			return nil, err
		}
		spec := storage.RulePreviewSpec{SampleKey: b.SampleKey, Fire: fire}
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
		p, err := gw.PreviewEventRule(ctx, spec, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapEventRuleErr(err)
		}
		out := &previewEventRuleOutput{}
		o := &out.Body
		o.Window, o.WindowSince, o.WindowUntil = p.Window.String(), p.Since, p.Until
		o.RowCap, o.Truncated, o.SustainsIgnored = p.RowCap, p.Truncated, p.SustainsIgnored
		o.InForceNow = make([]previewSeriesBody, 0, len(p.InForce))
		for _, s := range p.InForce {
			o.InForceNow = append(o.InForceNow, previewSeriesBody{Component: s.ComponentName, ComponentID: s.ComponentID, Instance: s.Instance, Since: s.Since})
		}
		o.Edges = make([]previewEdgeBody, 0, len(p.Edges))
		for _, e := range p.Edges {
			o.Edges = append(o.Edges, previewEdgeBody{Component: e.ComponentName, ComponentID: e.ComponentID, Instance: e.Instance, At: e.At, Kind: e.Kind})
		}
		return out, nil
	})
}
