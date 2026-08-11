package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The draft label render (#699): what a create form asks so it can show the
// label the platform is about to write, in a locked field, before the row
// exists. One custom method per estate collection, three operations.
//
// # The gate, and why it is :create rather than :read or :update
//
// The question this route answers is "what will the row I am about to create be
// called": it exists to be acted on by the create beside it, and an operator who
// cannot create has no use for the answer. So it is gated by the entity's own
// <kind>:create, the same permission the POST it precedes needs, which also
// makes "a viewer cannot probe the estate through the create form" one fact
// rather than two. It is deliberately NOT :update: nothing here reads or touches
// an existing row.
//
// # The scopes, which are the placement's and not the entity's
//
// The gate says who may ASK. What the answer may CONTAIN is a separate
// question, and a sharper one, because the rendered string is assembled partly
// from other rows: a component's label can read the label of its location and
// the label of its primary system's TYPE, and a system's can read its
// location's. So the placement refs resolve within the caller's location:read
// and system:read scopes, and a placement outside them is refused rather than
// rendered. Without that, this route is a disclosure channel: name a location
// you cannot read, read its label back out of the preview.
//
// The two are injected separately and by their own resource, not by the drafted
// entity's, because scope trees do not cross tiers: a component:read grant says
// nothing about which locations a caller may see, and threading it at the
// location table would deny every non-all caller instead of narrowing anything.
//
// A LOCATION draft takes no scope at all. Its data map is the location's own
// name and its type's display name (labels.go keeps placement off that tier
// deliberately), so there is no other estate row in the answer to guard.

// draftLabelOutput is the rendered answer plus the rule that produced it.
//
// The rule is not decoration. It is the only way a surface can tell "no rule
// applies at any tier" (both fields empty, which an operator reaches by clearing
// the rule at every tier) from "a rule applies and rendered nothing for this
// row", and the two want different words in front of an operator. It also lets the form say WHERE the value came
// from, which is the difference between a console that operates the label engine
// and one that merely reports its output.
type draftLabelOutput struct {
	Body struct {
		Label string `json:"label" doc:"The label the create would store, with the token \"n\" standing where the ordinal will go. Empty means no label is stored and the surface falls back to the name."`
		Rule  string `json:"rule" doc:"The label rule that produced it, resolved through the same tiers the create uses. Empty means no tier carries a rule for this classification."`
	}
}

func toDraftLabelOutput(d storage.DraftLabel) *draftLabelOutput {
	out := &draftLabelOutput{}
	out.Body.Label, out.Body.Rule = d.Label, d.Rule
	return out
}

// The name field carries the same three-state meaning on all three bodies, and
// the SAME meaning it carries on the create beside it: omitted hands the
// platform the pen. That is not a convenience, it is what keeps a locked field
// and what gets posted from being able to disagree, since the form asks both
// routes the identical question in the identical shape.

type renderComponentLabelInput struct {
	Body struct {
		Product  string `json:"product" required:"true" doc:"The product this component is an instance of, by name or uuid; the classification both a label rule and a generated name are resolved from"`
		Name     string `json:"name,omitempty" doc:"The name the row will carry. Omit it to render the label the platform's own generated name would produce, with the ordinal written as the token \"n\"; supply it to render the label an operator-named row would carry, which has no ordinal at all."`
		Location string `json:"location,omitempty" doc:"The location this component will sit at, by name or uuid. Resolved within the caller's location:read scope: a location out of scope is refused, never rendered."`
		System   string `json:"system,omitempty" doc:"The system this component will belong to, by name or uuid. Resolved within the caller's system:read scope."`
	}
}

type renderSystemLabelInput struct {
	Body struct {
		SystemTypeID string `json:"system_type_id,omitempty" doc:"The system_type this system is classified by, by name or uuid. Required to render a generated name's label: the stem lives on that registry row."`
		StandardID   string `json:"standard_id,omitempty" doc:"The standard this system conforms to, by name or uuid; omit for a one-off system"`
		Name         string `json:"name,omitempty" doc:"The name the row will carry. Omit it to render the label the platform's own generated name would produce, with the ordinal written as the token \"n\"; supply it to render the label an operator-named row would carry, which has no ordinal at all."`
		Location     string `json:"location,omitempty" doc:"The location this system will sit at, by name or uuid. Resolved within the caller's location:read scope."`
	}
}

type renderLocationLabelInput struct {
	Body struct {
		LocationType string `json:"location_type" required:"true" doc:"The location_type this location is classified by, by name or uuid"`
		Name         string `json:"name,omitempty" doc:"The name the row will carry. Omit it to render the label the platform's own generated name would produce, which a location_type with no name rule refuses; supply it to render the label an operator-named location would carry."`
	}
}

// registerComponentLabelDraft wires POST /components:renderLabel.
func registerComponentLabelDraft(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "render-component-label",
		Method:      http.MethodPost,
		Path:        "/components:renderLabel",
		Summary:     "Render the label a component create would store",
		Description: "Renders the label a component create would stamp, for the classification and placement a create form already holds, without creating anything. It allocates no ordinal, opens no write transaction and takes no advisory lock, which is what separates it from a preview that mints: the ordinal is written as the token \"n\", because it is allocated against live siblings inside the create's own transaction and does not exist until the row does. Omitting name renders the label the platform's own generated name would produce, and refuses (422) exactly where a nameless create would. Gated by component:create, the permission the create it precedes needs; the location and system refs resolve within the caller's location:read and system:read scopes, because the rendered string can carry their labels.",
	}, "component", "create"), func(ctx context.Context, in *renderComponentLabelInput) (*draftLabelOutput, error) {
		d, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
			ProductName:  in.Body.Product,
			Name:         in.Body.Name,
			LocationName: in.Body.Location,
			SystemName:   in.Body.System,
		}, a.scopeFor(ctx, "location", "read"), a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return toDraftLabelOutput(d), nil
	})
}

// registerSystemLabelDraft wires POST /systems:renderLabel.
func registerSystemLabelDraft(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "render-system-label",
		Method:      http.MethodPost,
		Path:        "/systems:renderLabel",
		Summary:     "Render the label a system create would store",
		Description: "The system tier of :renderLabel on components. Renders the label a system create would stamp, allocating nothing. Omitting name renders the label the platform's generated name would produce and refuses (422) an unclassified system, the same refusal a nameless create gives, since the stem lives on the system_type. Gated by system:create; the location ref resolves within the caller's location:read scope, because a system's label can carry its location's.",
	}, "system", "create"), func(ctx context.Context, in *renderSystemLabelInput) (*draftLabelOutput, error) {
		d, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
			SystemTypeRef: in.Body.SystemTypeID,
			StandardRef:   in.Body.StandardID,
			Name:          in.Body.Name,
			LocationName:  in.Body.Location,
		}, a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return toDraftLabelOutput(d), nil
	})
}

// registerLocationLabelDraft wires POST /locations:renderLabel.
//
// It injects no scope, and that is a property of the tier rather than an
// omission: a location's label data map carries its own name and its type's
// display name and nothing about where it sits, so the answer contains no fact
// from another estate row to leak.
func registerLocationLabelDraft(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "render-location-label",
		Method:      http.MethodPost,
		Path:        "/locations:renderLabel",
		Summary:     "Render the label a location create would store",
		Description: "The location tier of :renderLabel on components. Renders the label a location create would stamp, allocating nothing. A shipped estate answers from the global location rule, which reads the location's own name as words and titles it, so a location named north-wing drafts as North Wing; an empty label means no rule resolves at any tier, and the surface falls back to the name. Omitting name refuses (422) a location_type with no name rule, the same refusal a nameless create gives. Gated by location:create. No placement scope is injected because a location's label rule reads no other estate row.",
	}, "location", "create"), func(ctx context.Context, in *renderLocationLabelInput) (*draftLabelOutput, error) {
		d, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{
			LocationTypeRef: in.Body.LocationType,
			Name:            in.Body.Name,
		})
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return toDraftLabelOutput(d), nil
	})
}
