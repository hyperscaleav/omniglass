package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The draft identity render (#699, #702): what a create form asks so it can
// show the name and the label the platform is about to write, in locked fields,
// before the row exists. One custom method per estate collection, three
// operations.
//
// # Why the answer carries the name, and what the form does with it
//
// The server has already resolved the classification's stem, the mint and the
// bucket's lowest free ordinal in order to render the label at all, so
// returning the NAME costs nothing and is the only way the two locked fields
// can be one answer rather than two that can disagree (#702 removes the
// console's own walk of the type chain for this).
//
// The name it carries is REAL rather than a token, and it is provisional:
// nothing is allocated or reserved by asking. A form does not post it back as
// the name field, which would claim the pen; it posts it as expected_name, the
// create's precondition, and a create that would stamp a different name is
// refused with a 409 naming the one it would stamp. The NAME rather than the
// ordinal beside it, because the name is the whole claim: it carries the stem
// and the suppression rule as well as the number, so an edit to the type that
// mints it invalidates the claim where a number would have passed (#702 review).
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
// scope, and a placement outside it is refused rather than rendered. Without
// that, this route is a disclosure channel: name a location you cannot read,
// read its label back out of the preview.
//
// A component's SYSTEM ref is the exception, and it resolves in a WRITE set
// (#713). Naming a system on the create binds that system's membership, which
// costs system:update (ADR-0107), so resolving the preview's copy of the same
// reference in system:read alone would serve a preview for a create the
// platform refuses. The rule is the create's rule: what the bind requires
// decides whether it resolves, and system:read decides only what the refusal is
// allowed to say.
//
// The two are injected separately and by their own resource, not by the drafted
// entity's, because scope trees do not cross tiers: a component:read grant says
// nothing about which locations a caller may see, and threading it at the
// location table would deny every non-all caller instead of narrowing anything.
//
// A LOCATION draft takes no READ scope. Its data map is the location's own name
// and its type's label (labels.go keeps placement off that tier
// deliberately), so there is no other estate row's label in the answer to guard.
// It takes the create scope like the other two, because the ordinal is read from
// a placement bucket either way.
//
// # The parentless bucket, which is a scope question and not a permission one
//
// Omitting the parent names the bucket every create gates on an ALL create scope
// (a root location, a parentless component, a parentless system), so the draft
// refuses it in the same set rather than answering (#702 review). The answer is
// not inert: it carries the lowest free ordinal, read from the bucket's sibling
// names, so it says which of that bucket's names are taken, and the stem being
// asked about is the caller's to choose since a forked type's name rule is
// ordinary (#703). The gate is a scope, so it sits beside the route's permission
// gate rather than inside it.

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
		Name    string `json:"name" doc:"The name the create would stamp: the one you supplied, or the one the platform would mint. Generated names carry the ordinal that is free in the placement bucket right now."`
		Ordinal int    `json:"ordinal,omitempty" doc:"The ordinal that name was minted from, absent when you supplied the name (an operator-named row carries no ordinal). Informational: what a form posts back as the create's precondition is the NAME above, since that carries the stem and the suppression rule as well as this number."`
		Label   string `json:"label" doc:"The label the create would store. Empty means no label is stored and the surface falls back to the name."`
		Rule    string `json:"rule" doc:"The label rule that produced it, resolved through the same tiers the create uses. Empty means no tier carries a rule for this classification."`
	}
}

func toDraftLabelOutput(d storage.DraftLabel) *draftLabelOutput {
	out := &draftLabelOutput{}
	out.Body.Name, out.Body.Ordinal = d.Name, d.Ordinal
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
		Name     string `json:"name,omitempty" doc:"The name the row will carry. Omit it to draft the name and label the platform would produce; supply it to draft the label an operator-named row would carry, which has no ordinal at all."`
		Parent   string `json:"parent,omitempty" doc:"The parent component, by name or uuid. Part of the placement bucket a generated name's ordinal is read from, so a draft that omits it previews the wrong bucket. Resolved within the caller's component:create scope, the same set the create resolves it in."`
		Location string `json:"location,omitempty" doc:"The location this component will sit at, by name or uuid. Resolved within the caller's location:read scope: a location out of scope is refused, never rendered."`
		System   string `json:"system,omitempty" doc:"The system this component will belong to, by name or uuid. Naming it requires system:update, exactly as the create does, because the create inserts that system's membership; it resolves within that scope, and system:read decides only whether the refusal may name the system."`
	}
}

type renderSystemLabelInput struct {
	Body struct {
		SystemTypeID string `json:"system_type_id,omitempty" doc:"The system_type this system is classified by, by name or uuid. Required to render a generated name's label: the stem lives on that registry row."`
		StandardID   string `json:"standard_id,omitempty" doc:"The standard this system conforms to, by name or uuid; omit for a one-off system"`
		Name         string `json:"name,omitempty" doc:"The name the row will carry. Omit it to draft the name and label the platform would produce; supply it to draft the label an operator-named row would carry, which has no ordinal at all."`
		Parent       string `json:"parent,omitempty" doc:"The parent system, by name or uuid. Part of the placement bucket a generated name's ordinal is read from. Resolved within the caller's system:create scope."`
		Location     string `json:"location,omitempty" doc:"The location this system will sit at, by name or uuid. Resolved within the caller's location:read scope."`
	}
}

type renderLocationLabelInput struct {
	Body struct {
		LocationType string `json:"location_type" required:"true" doc:"The location_type this location is classified by, by name or uuid"`
		Name         string `json:"name,omitempty" doc:"The name the row will carry. Omit it to draft the name and label the platform would produce, which a location_type with no name rule refuses; supply it to draft the label an operator-named location would carry."`
		Parent       string `json:"parent,omitempty" doc:"The parent location, by name or uuid. A location has two placement buckets, under a parent or at the root, and this is which one a generated name's ordinal is read from. Resolved within the caller's location:create scope."`
	}
}

// registerComponentLabelDraft wires POST /components:renderLabel.
//
// It carries the create's conditional permission as well as its scopes (#713).
// The system reference is the one field on this body that the create WRITES
// THROUGH rather than only reads: naming it makes the create insert that
// system's membership, which costs system:update (#707, ADR-0107). A draft that
// asked for less than the create asks is a preview of a refusal, and the two
// halves of the create's gate are both reachable that way: the permission (held
// nowhere) and the scope (held, but not over this system). Both are rehearsed
// here, in the create's own order, so the agreement between a preview and the
// create it previews is the platform's property rather than the console's good
// manners.
func registerComponentLabelDraft(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID: "render-component-label",
		Method:      http.MethodPost,
		Path:        "/components:renderLabel",
		Summary:     "Draft the name and label a component create would store",
		Description: "Drafts the name and the label a component create would stamp, for the classification and placement a create form already holds, without creating anything. It allocates no ordinal, opens no write transaction and takes no advisory lock, which is what separates it from a preview that mints: the ordinal is READ (the lowest free number among the live siblings in the placement bucket) rather than allocated. That answer is provisional, so a form posts the NAME back as expected_name on the create and is refused (409) rather than silently renamed if another create takes the number or the type's stem moves first. Omitting name drafts the name the platform would mint, and refuses (422) exactly where a nameless create would. Gated by component:create, the permission the create it precedes needs; the parent resolves within the caller's component:create scope and the location ref within location:read, because the rendered string can carry that label. Naming a system additionally requires system:update and resolves within that scope, the same as the create, because the create binds that system's membership: a preview is never served for a bind the create would refuse. Omitting parent is the parentless bucket, which a create refuses without an all-scoped grant, so the draft refuses it too (403): a form must not preview a bucket its create declines, and the previewed ordinal reports which names that bucket already holds.",
	}, "component", "create"), "system", "update"), func(ctx context.Context, in *renderComponentLabelInput) (*draftLabelOutput, error) {
		// The same body-conditional gate the create carries, checked here for the
		// same reason: middleware cannot see the body, and a draft that names no
		// system previews no membership and costs component:create alone.
		if in.Body.System != "" && !a.allows(ctx, "system", "update") {
			return nil, huma.Error403Forbidden(ErrSystemBindNeedsUpdate)
		}
		d, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
			ProductName:  in.Body.Product,
			Name:         in.Body.Name,
			ParentName:   in.Body.Parent,
			LocationName: in.Body.Location,
			SystemName:   in.Body.System,
		}, a.scopeFor(ctx, "component", "create"), a.scopeFor(ctx, "location", "read"),
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"))
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
		Summary:     "Draft the name and label a system create would store",
		Description: "The system tier of :renderLabel on components. Drafts the name and the label a system create would stamp, allocating nothing: the ordinal is read from the placement bucket and the drafted name is posted back as expected_name on the create. A system suppresses the first ordinal in a bucket, so the first boardroom in a room drafts as boardroom and the second as boardroom-2. Omitting name drafts the name the platform would mint and refuses (422) an unclassified system, the same refusal a nameless create gives, since the stem lives on the system_type. Gated by system:create; the parent resolves within the caller's system:create scope and the location ref within location:read, because a system's label can carry its location's. Omitting parent is the parentless bucket, refused (403) without an all-scoped create grant, exactly as the create refuses it.",
	}, "system", "create"), func(ctx context.Context, in *renderSystemLabelInput) (*draftLabelOutput, error) {
		d, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
			SystemTypeRef: in.Body.SystemTypeID,
			StandardRef:   in.Body.StandardID,
			Name:          in.Body.Name,
			ParentName:    in.Body.Parent,
			LocationName:  in.Body.Location,
		}, a.scopeFor(ctx, "system", "create"), a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return toDraftLabelOutput(d), nil
	})
}

// registerLocationLabelDraft wires POST /locations:renderLabel.
//
// It injects one scope where it used to inject none, and the one it injects is
// the create's rather than a read scope. A location's label data map carries
// its own name and its type's label and nothing about where it sits, so
// the RENDER still contains no fact from another estate row to leak; what the
// scope guards is the parent whose bucket the ordinal is read from, which is
// the same reference the create resolves in the same set.
func registerLocationLabelDraft(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "render-location-label",
		Method:      http.MethodPost,
		Path:        "/locations:renderLabel",
		Summary:     "Draft the name and label a location create would store",
		Description: "The location tier of :renderLabel on components. Drafts the name and the label a location create would stamp, allocating nothing. A shipped estate answers from the global location rule, which reads the location's own name as words and titles it, so a location named north-wing drafts as North Wing; an empty label means no rule resolves at any tier, and the surface falls back to the name. Omitting name refuses (422) a location_type with no name rule, the same refusal a nameless create gives. Gated by location:create; the parent resolves within the caller's location:create scope, because a location's two placement buckets are under a parent or at the root and that is where the ordinal is read from. Omitting parent is the ROOT bucket, which a create refuses without an all-scoped grant, so the draft refuses it too (403) rather than reporting which names the estate root already holds.",
	}, "location", "create"), func(ctx context.Context, in *renderLocationLabelInput) (*draftLabelOutput, error) {
		d, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{
			LocationTypeRef: in.Body.LocationType,
			Name:            in.Body.Name,
			ParentName:      in.Body.Parent,
		}, a.scopeFor(ctx, "location", "create"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return toDraftLabelOutput(d), nil
	})
}
