package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// identityBindingBody is one identity binding on the wire: a conduit
// component's reported value under a match key, tied to the one system or
// component it names (#1385, ADR-1282).
type identityBindingBody struct {
	Conduit   string    `json:"conduit" doc:"Name of the conduit component: the component whose endpoint reports the identity, the path rather than the owner"`
	ConduitID string    `json:"conduit_id" doc:"The conduit component's uuid"`
	Key       string    `json:"key" doc:"The match key: a registered property type naming what kind of identity the value is (e.g. ucc-room-id)"`
	Value     string    `json:"value" doc:"The identity value the conduit reports (e.g. the id a UCC service gives one of the rooms it reports on)"`
	OwnerKind string    `json:"owner_kind" enum:"component,system" doc:"What kind of owner the identity names: a component or a system"`
	OwnerName string    `json:"owner_name" doc:"Name of the owner the identity's values land on"`
	OwnerID   string    `json:"owner_id" doc:"The owner's uuid, the address to navigate by"`
	CreatedAt time.Time `json:"created_at" doc:"When the binding was made"`
}

func toIdentityBindingBody(b storage.IdentityBinding) identityBindingBody {
	return identityBindingBody{
		Conduit: b.ConduitName, ConduitID: b.ConduitID, Key: b.Key, Value: b.Value,
		OwnerKind: b.OwnerKind, OwnerName: b.OwnerName, OwnerID: b.OwnerID, CreatedAt: b.CreatedAt,
	}
}

type identityBindingsInput struct {
	Name string `path:"name" doc:"Name of the conduit component, or a dotted address (e.g. boi.17c.415a.$comp.display-1)" example:"ucc-service-1"`
}

type identityBindingPathInput struct {
	Name  string `path:"name" doc:"Name of the conduit component, or a dotted address (e.g. boi.17c.415a.$comp.display-1)" example:"ucc-service-1"`
	Key   string `path:"key" doc:"The match key, a registered property type" example:"ucc-room-id"`
	Value string `path:"value" minLength:"1" maxLength:"256" doc:"The identity value the conduit reports, 1 to 256 characters" example:"room-17c-415a"`
}

type putIdentityBindingInput struct {
	Name  string `path:"name" doc:"Name of the conduit component, or a dotted address (e.g. boi.17c.415a.$comp.display-1)" example:"ucc-service-1"`
	Key   string `path:"key" doc:"The match key, a registered property type" example:"ucc-room-id"`
	Value string `path:"value" minLength:"1" maxLength:"256" doc:"The identity value the conduit reports, 1 to 256 characters" example:"room-17c-415a"`
	Body  struct {
		OwnerKind string `json:"owner_kind" enum:"component,system" doc:"What kind of owner the identity names. A component or a system; a place or a node owns no observed sample, so neither is an owner here"`
		Owner     string `json:"owner" minLength:"1" doc:"Name or uuid of the owner the identity's values land on, or a dotted address" example:"hq.west.level-2.boardroom-a.$sys.av"`
	}
}

type listIdentityBindingsOutput struct {
	Body struct {
		Conduit  string                `json:"conduit" doc:"The conduit component, as addressed"`
		Bindings []identityBindingBody `json:"bindings" doc:"The bindings whose owner the caller can read, ordered by key then value"`
		Hidden   int                   `json:"hidden" doc:"How many more bindings this conduit has whose owner is outside the caller's read: counted, never named"`
	}
}

type identityBindingOutput struct {
	Body identityBindingBody
}

// ErrIdentityOwnerNeedsSystemUpdate is the 403 a put naming a system owner
// returns when the caller holds no system:update at all. A binding writes onto
// its owner (the owner receives the identity's values), so it costs a write on
// both ends, the conduit's component:update and the owner's update (ADR-1282,
// the ADR-0510 rule).
const ErrIdentityOwnerNeedsSystemUpdate = "an identity binding lands the conduit's values for this identity on its owner, so it costs a write on both ends: component:update on the conduit, and system:update on a system owner. Ask for system:update over the system."

// ErrIdentityOwnerOutOfScope is the 403 for the scope half of the owner end:
// the caller reads the owner but holds no update over it. Reached only for an
// owner the caller can read, so naming it discloses nothing.
const ErrIdentityOwnerOutOfScope = "this owner is outside the %s:update scope your grants resolve to; an identity binding lands the conduit's values on its owner, so both the conduit and the owner must be in scope. Ask for a %s:update grant covering it."

// ErrIdentityMoveForbidden is the 403 for a put moving a bound identity to a
// different owner, by a caller without update on the owner it is bound to now.
// It names neither that owner nor its kind, since the caller may not read it.
const ErrIdentityMoveForbidden = "this identity is already bound to another owner, and moving it costs update on the owner it leaves as well as the one it joins (component:update or system:update over that owner). Ask whoever can update the current owner to move or unbind it."

// ErrIdentityConduitOutOfScope is the conduit end's 403: readable, but not
// inside the caller's component:update scope.
const ErrIdentityConduitOutOfScope = "this conduit component is outside the component:update scope your grants resolve to; an identity binding writes onto both its conduit and its owner. Ask for a component:update grant covering the conduit."

// registerIdentityBindingRoutes wires the identity bindings a conduit fans its
// identified values out through: the list, and the put and delete on one
// identity's address. They live under the conduit, beside the identities it
// reports, and cost a write on both ends.
func registerIdentityBindingRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-identity-bindings",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/identity-bindings",
		Summary:     "List the identities a conduit component fans out",
		Description: "The identity bindings of this conduit component: each value it reports under a match key (a registered property type, such as ucc-room-id) and the one system or component that value's samples land on. A component that speaks for many owners (a cloud service, a controller fronting many components) is the conduit, the path rather than the owner. Each binding is named only when its owner is inside the caller's read on its tier (system:read for a system owner, component:read for a component owner); the rest are counted in hidden, never named. Gated by component:read; a conduit outside it is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *identityBindingsInput) (*listIdentityBindingsOutput, error) {
		l, err := gw.ListIdentityBindings(ctx, in.Name, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapIdentityBindingErr(err)
		}
		out := &listIdentityBindingsOutput{}
		out.Body.Conduit = in.Name
		out.Body.Bindings = make([]identityBindingBody, 0, len(l.Bindings))
		for _, b := range l.Bindings {
			out.Body.Bindings = append(out.Body.Bindings, toIdentityBindingBody(b))
		}
		out.Body.Hidden = l.Hidden
		return out, nil
	})

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID: "put-identity-binding",
		Extensions:  mcpExposed(),
		Method:      http.MethodPut,
		Path:        "/components/{name}/identity-bindings/{key}/{value}",
		Summary:     "Bind an identity a conduit reports to its owner",
		Description: "Ties this value, reported by the conduit component under this match key, to one owner, a system or a component, so the conduit's samples naming it land on that owner. One identity on one conduit has exactly one owner: a put naming the owner it is already bound to changes nothing, and one naming a different owner moves it. The match key must be a registered property type (422 otherwise), and the owner a component or a system other than the conduit itself (a place or a node owns no observed sample, and a value with no identity already lands on the conduit: each a 422). The value rides the path as one percent-encoded segment, so a value carrying a slash, a space or another reserved character is escaped (room%2F1) and round-trips. A binding writes onto both of its ends, so it costs a write on both: gated by component:update on the conduit, and it also needs the owner's update (component:update, or system:update for a system, refused with a 403 naming it before anything resolves). Each end resolves in its own tier's read scope and must be inside its update scope: an end outside the read scope is a non-disclosing 404, one readable but not writable a 403. A move also needs update on the owner it leaves.",
	}, "component", "update"), "system", "update"), func(ctx context.Context, in *putIdentityBindingInput) (*identityBindingOutput, error) {
		if in.Body.OwnerKind == "system" && !a.allows(ctx, "system", "update") {
			return nil, huma.Error403Forbidden(ErrIdentityOwnerNeedsSystemUpdate)
		}
		b, err := gw.PutIdentityBinding(ctx, actorID(ctx), in.Name, in.Key, in.Value,
			storage.IdentityBindingOwner{Kind: in.Body.OwnerKind, Name: in.Body.Owner}, identityBindingScopes(ctx, a))
		if err != nil {
			return nil, mapIdentityBindingErr(err)
		}
		return &identityBindingOutput{Body: toIdentityBindingBody(*b)}, nil
	})

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "delete-identity-binding",
		Extensions:    mcpExposed(),
		Method:        http.MethodDelete,
		Path:          "/components/{name}/identity-bindings/{key}/{value}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Unbind an identity a conduit reports",
		Description:   "Removes the binding of this value, reported by the conduit component under this match key, so its samples no longer land on the owner it named. Unbinding writes onto both ends, so it costs a write on both: gated by component:update on the conduit, and it also needs update on the binding's owner (component:update, or system:update for a system), a 403 naming it when the caller reads the owner but cannot write it. A binding whose owner the caller cannot read is a 404, as one that does not exist is: the list does not name it either.",
	}, "component", "update"), "system", "update"), func(ctx context.Context, in *identityBindingPathInput) (*struct{}, error) {
		if err := gw.DeleteIdentityBinding(ctx, actorID(ctx), in.Name, in.Key, in.Value, identityBindingScopes(ctx, a)); err != nil {
			return nil, mapIdentityBindingErr(err)
		}
		return nil, nil
	})
}

func identityBindingScopes(ctx context.Context, a *authenticator) storage.IdentityBindingScopes {
	return storage.IdentityBindingScopes{
		ComponentRead: a.scopeFor(ctx, "component", "read"), ComponentUpdate: a.scopeFor(ctx, "component", "update"),
		SystemRead: a.scopeFor(ctx, "system", "read"), SystemUpdate: a.scopeFor(ctx, "system", "update"),
	}
}

func mapIdentityBindingErr(err error) error {
	// The owner end first: its sentinels are the same as the conduit's for a
	// component owner, and only this branch knows which end refused.
	var owner *storage.IdentityBindingOwnerError
	if errors.As(err, &owner) {
		switch {
		case errors.Is(owner.Err, storage.ErrComponentForbidden), errors.Is(owner.Err, storage.ErrSystemForbidden):
			return huma.Error403Forbidden(fmt.Sprintf(ErrIdentityOwnerOutOfScope, owner.Kind, owner.Kind))
		}
		if refErr, ok := mapRefErr(owner.Err); ok {
			return refErr
		}
		return huma.Error404NotFound("owner " + owner.Kind + " not found")
	}
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrIdentityBindingNotFound):
		return huma.Error404NotFound("no binding of this identity on this conduit")
	case errors.Is(err, storage.ErrIdentityBindingKeyUnregistered):
		return huma.Error422UnprocessableEntity("the match key is not a registered property type; register it, or use one that is (e.g. ucc-room-id)")
	case errors.Is(err, storage.ErrIdentityBindingOwnerKind):
		return huma.Error422UnprocessableEntity("an identity binding's owner is a component or a system; a place or a node owns no observed sample")
	case errors.Is(err, storage.ErrIdentityBindingOwnerIsConduit):
		return huma.Error422UnprocessableEntity("an identity binding's owner is not its own conduit: a value the conduit reports with no identity already lands on the conduit, so bind the identity to the system or component it names")
	case errors.Is(err, storage.ErrIdentityBindingValue):
		return huma.Error422UnprocessableEntity("an identity value is 1 to 256 characters")
	case errors.Is(err, storage.ErrIdentityBindingMoveForbidden):
		return huma.Error403Forbidden(ErrIdentityMoveForbidden)
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	case errors.Is(err, storage.ErrComponentForbidden):
		return huma.Error403Forbidden(ErrIdentityConduitOutOfScope)
	default:
		return err
	}
}
