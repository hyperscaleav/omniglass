package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/template"
)

// The template catalog over the API (ADR-0071, ADR-0827): the binary's
// shipped templates, read-only. A catalog read is a registry read with no
// owner, so there is no scope to inject; template:read sits on the viewer
// read floor. Nothing here writes: a template is never a row, and the
// createFromTemplate methods live on the collections they create in.

type templateRoleBody struct {
	Name           string   `json:"name" doc:"The role's name, unique within the standard"`
	Label          string   `json:"label" doc:"What an operator reads for the role"`
	Quorum         int      `json:"quorum" doc:"How many components must fill the role (at least one)"`
	Capacity       *int     `json:"capacity,omitempty" doc:"The most components the role takes; omitted for no cap"`
	Impact         string   `json:"impact,omitempty" doc:"What the system loses when the role is below quorum: outage, degraded (the default) or none"`
	AcceptedTypes  []string `json:"accepted_types,omitempty" doc:"The component types that may fill the role; empty accepts any"`
	PinnedProducts []string `json:"pinned_products,omitempty" doc:"The products the role is pinned to; empty pins none"`
	Choice         string   `json:"choice,omitempty" doc:"The choice the role belongs to, when it is one way of answering a choice"`
	Alternate      string   `json:"alternate,omitempty" doc:"The alternate of that choice the role belongs to"`
}

type templateAlternateBody struct {
	Name  string `json:"name" doc:"The alternate's name, unique within its choice"`
	Label string `json:"label" doc:"What an operator reads for the alternate"`
}

type templateChoiceBody struct {
	Name       string                  `json:"name" doc:"The choice's name, unique within the standard"`
	Label      string                  `json:"label" doc:"What an operator reads for the choice"`
	Alternates []templateAlternateBody `json:"alternates" doc:"The ways to answer the choice, the first the template's default"`
}

type templateContractBody struct {
	Name     string          `json:"name" doc:"The property or metric the standard declares"`
	Default  json.RawMessage `json:"default,omitempty" doc:"The contract default, a JSON value; omitted when the line sets none"`
	Required bool            `json:"required,omitempty" doc:"Whether a conforming system must carry a value"`
}

type templateMapPositionBody struct {
	Role     string  `json:"role" doc:"The role placed"`
	Position int     `json:"position" doc:"Which of the role's components, from 1"`
	X        float64 `json:"x" doc:"Horizontal position, 0 to 1 from the left"`
	Y        float64 `json:"y" doc:"Vertical position, 0 to 1 from the top"`
}

type templateMapBody struct {
	Aspect    float64                   `json:"aspect" doc:"The layout's width over its height"`
	Positions []templateMapPositionBody `json:"positions" doc:"Where each role's components sit"`
}

type templateStandardBody struct {
	Parent     string                 `json:"parent,omitempty" doc:"The standard template this variant hangs off; a variant is created from its template once the operator holds a standard of that name"`
	Choices    []templateChoiceBody   `json:"choices,omitempty" doc:"The exclusive-or groups of roles a conforming system answers one way"`
	Roles      []templateRoleBody     `json:"roles" doc:"The roles a conforming system needs filled"`
	Properties []templateContractBody `json:"properties,omitempty" doc:"The property contract lines a standard created from it declares"`
	Metrics    []templateContractBody `json:"metrics,omitempty" doc:"The metric contract lines a standard created from it declares"`
	Map        *templateMapBody       `json:"map,omitempty" doc:"The layout a conforming system's Map tab draws"`
}

type templateComponentBody struct {
	Product    string                     `json:"product" doc:"The product the component is"`
	Driver     string                     `json:"driver,omitempty" doc:"The driver the attach preset is for, when it is not the product's own"`
	Inputs     map[string]string          `json:"inputs,omitempty" doc:"The attach preset: every driver input but the address, a secret input as a secret's name"`
	Properties map[string]json.RawMessage `json:"properties,omitempty" doc:"Declared property values, by property name"`
}

type templateRoleFillBody struct {
	Component string `json:"component,omitempty" doc:"The component template that fills the role"`
	Product   string `json:"product,omitempty" doc:"The product that fills the role, when no component template does"`
}

type templateSystemBody struct {
	Standard   string                          `json:"standard" doc:"The standard the system conforms to, a standard template's name"`
	SystemType string                          `json:"system_type,omitempty" doc:"The system type the system is classified as"`
	Roles      map[string]templateRoleFillBody `json:"roles" doc:"What fills each role, by role name"`
	Properties map[string]json.RawMessage      `json:"properties,omitempty" doc:"Declared system property values, by property name"`
}

type templatePlaceSystemBody struct {
	Template string `json:"template" doc:"The system template that lands in the place"`
	Label    string `json:"label,omitempty" doc:"The system's label"`
}

type templatePlaceBody struct {
	Type    string                    `json:"type" doc:"The place's location type"`
	Name    string                    `json:"name,omitempty" doc:"The place's name; omitted where its type's name rule mints one, or the caller names it"`
	Label   string                    `json:"label,omitempty" doc:"The place's label"`
	Systems []templatePlaceSystemBody `json:"systems,omitempty" doc:"The system templates that land in the place"`
	Places  []templatePlaceBody       `json:"places,omitempty" doc:"The places nested under this one"`
}

type templateBody struct {
	Kind        string                 `json:"kind" enum:"standard_template,system_template,place_template,component_template" doc:"What the template creates"`
	Name        string                 `json:"name" doc:"The template's name, unique across the catalog"`
	Label       string                 `json:"label" doc:"What an operator reads for the template"`
	Description string                 `json:"description,omitempty" doc:"What the template is for"`
	Standard    *templateStandardBody  `json:"standard,omitempty" doc:"A standard template's body: what creating a standard from it writes"`
	Component   *templateComponentBody `json:"component,omitempty" doc:"A component template's body"`
	System      *templateSystemBody    `json:"system,omitempty" doc:"A system template's body"`
	Place       *templatePlaceBody     `json:"place,omitempty" doc:"A place template's body: the root of the subtree"`
}

func toTemplateBody(t *template.Template) templateBody {
	out := templateBody{Kind: string(t.Kind), Name: t.Name, Label: t.Label, Description: t.Description}
	switch {
	case t.Standard != nil:
		out.Standard = toTemplateStandardBody(t.Standard)
	case t.Component != nil:
		out.Component = &templateComponentBody{
			Product: t.Component.Product, Driver: t.Component.Attach.Driver,
			Inputs: t.Component.Attach.Inputs, Properties: t.Component.Properties,
		}
	case t.System != nil:
		sys := &templateSystemBody{
			Standard: t.System.Standard, SystemType: t.System.SystemType,
			Roles: map[string]templateRoleFillBody{}, Properties: t.System.Properties,
		}
		for role, f := range t.System.Roles {
			sys.Roles[role] = templateRoleFillBody{Component: f.Component, Product: f.Product}
		}
		out.System = sys
	case t.Place != nil:
		p := toTemplatePlaceBody(t.Place)
		out.Place = &p
	}
	return out
}

func toTemplateStandardBody(s *template.Standard) *templateStandardBody {
	out := &templateStandardBody{Parent: s.Parent, Roles: []templateRoleBody{}}
	for _, c := range s.Choices {
		cb := templateChoiceBody{Name: c.Name, Label: c.Label}
		for _, a := range c.Alternates {
			cb.Alternates = append(cb.Alternates, templateAlternateBody{Name: a.Name, Label: a.Label})
		}
		out.Choices = append(out.Choices, cb)
	}
	for _, r := range s.Roles {
		out.Roles = append(out.Roles, templateRoleBody{
			Name: r.Name, Label: r.Label, Quorum: r.Quorate(), Capacity: r.Capacity, Impact: r.Impact,
			AcceptedTypes: r.AcceptedTypes, PinnedProducts: r.PinnedProducts, Choice: r.Choice, Alternate: r.Alternate,
		})
	}
	for _, l := range s.Props {
		out.Properties = append(out.Properties, templateContractBody{Name: l.Name, Default: l.Default, Required: l.Required})
	}
	for _, l := range s.Metrics {
		out.Metrics = append(out.Metrics, templateContractBody{Name: l.Name, Default: l.Default, Required: l.Required})
	}
	if s.Map != nil {
		m := &templateMapBody{Aspect: s.Map.Aspect, Positions: []templateMapPositionBody{}}
		for _, p := range s.Map.Positions {
			m.Positions = append(m.Positions, templateMapPositionBody{Role: p.Role, Position: p.Position, X: p.X, Y: p.Y})
		}
		out.Map = m
	}
	return out
}

func toTemplatePlaceBody(p *template.Place) templatePlaceBody {
	out := templatePlaceBody{Type: p.Type, Name: p.Name, Label: p.Label}
	for _, s := range p.Systems {
		out.Systems = append(out.Systems, templatePlaceSystemBody{Template: s.Template, Label: s.Label})
	}
	for i := range p.Places {
		out.Places = append(out.Places, toTemplatePlaceBody(&p.Places[i]))
	}
	return out
}

type listTemplatesInput struct {
	Kind string `query:"kind" enum:"standard_template,system_template,place_template,component_template" doc:"Only templates of this kind; omitted lists every kind"`
}

type listTemplatesOutput struct {
	Body struct {
		Templates []templateBody `json:"templates" doc:"The catalog's templates, ordered by kind then name"`
	}
}

type templatePathInput struct {
	Template string `path:"template" doc:"The template, by name" example:"meeting-room"`
}

type templateOutput struct {
	Body templateBody
}

// registerTemplateRoutes wires the catalog reads over cat, the shipped
// catalog in production and a fixture in a test.
func registerTemplateRoutes(api huma.API, a *authenticator, cat *template.Catalog) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-templates",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/templates",
		Summary:     "List templates",
		Description: "Lists the template catalog this binary ships, ordered by kind then name, optionally one kind. A template is an example an operator clones: creating from one (POST /standards:createFromTemplate, and the same method on components, systems and locations) writes ordinary rows the operator owns, and nothing points back at the template. Gated by template:read.",
	}, "template", "read"), func(_ context.Context, in *listTemplatesInput) (*listTemplatesOutput, error) {
		out := &listTemplatesOutput{}
		list := cat.List(template.Kind(in.Kind))
		out.Body.Templates = make([]templateBody, 0, len(list))
		for i := range list {
			out.Body.Templates = append(out.Body.Templates, toTemplateBody(&list[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-template",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/templates/{template}",
		Summary:     "Get a template",
		Description: "Reads one template from the catalog by name, whole: its kind and the body that kind carries. An unknown name is a 404. Gated by template:read.",
	}, "template", "read"), func(_ context.Context, in *templatePathInput) (*templateOutput, error) {
		t, err := cat.Get(in.Template)
		if errors.Is(err, template.ErrNotFound) {
			return nil, huma.Error404NotFound("template " + in.Template + " is not in the catalog")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("get template")
		}
		return &templateOutput{Body: toTemplateBody(t)}, nil
	})
}
