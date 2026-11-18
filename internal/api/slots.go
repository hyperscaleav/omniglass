package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/updatemask"
)

// The slot surface, in three parts. Declaration: what a standard says every
// conforming system needs filled, and what one system declares ad-hoc.
// Resolution: the per-system read that merges both arcs with who fills each slot
// today. Staffing: assign and unassign a component, refused when the component
// is not a typed match, its product's component_type outside every type the slot
// accepts, or (if the slot pins products) its product not one of them (#626).
//
// The refusal is the point of the model, so it is never bare: a shortfall is a
// 422 that NAMES both parties, what the component actually is (or which product
// it is) and what the slot wants, because "no" that does not say why leaves the
// operator nothing to do. Capability retired with the family (#626): once
// assigned, an occupant satisfies its slot as long as its own verdict is not
// outage (internal/health); a merely degraded occupant still counts, nothing
// more.
//
// Gating follows the owner: the standard arc rides standard:read/update/delete,
// the system arc rides system:read/update. Every system arc route resolves its
// owner within the caller's scope first, so an out-of-scope target is a
// non-disclosing 404 rather than a forbidden or a silent write.

type systemSlotBody struct {
	Name           string   `json:"name" doc:"The slot's name within its owner (the address)"`
	Label          string   `json:"label" doc:"The slot's human label"`
	Quorum         int      `json:"quorum" doc:"How many components must fill the slot"`
	Capacity       *int     `json:"capacity,omitempty" doc:"The most components the slot will accept; null means no upper bound beyond quorum"`
	PositionLabels []string `json:"position_labels" doc:"Human labels for each position within the slot, by index; empty when unlabeled"`
	AcceptedTypes  []string `json:"accepted_types" doc:"The component_types a filling component's product must be classified within (self or a descendant); empty accepts any type"`
	PinnedProducts []string `json:"pinned_products" doc:"If set, a filling component's product must be one of these; empty accepts any product of an accepted type"`
	Impact         string   `json:"impact" doc:"What an impaired slot means for its system: outage, degraded, or none"`
	Alternate      string   `json:"alternate,omitempty" doc:"The choice/alternate this slot joins, addressed as \"choice-name/alternate-name\" (#626); absent when the slot is unconditional. The same form the write body takes, so a read round-trips into a write"`
}

func toSystemSlotBody(r *storage.SystemSlot) systemSlotBody {
	types, products, labels := r.AcceptedTypes, r.PinnedProducts, r.PositionLabels
	if types == nil {
		types = []string{}
	}
	if products == nil {
		products = []string{}
	}
	if labels == nil {
		labels = []string{}
	}
	return systemSlotBody{
		Name:           r.Name,
		Label:          r.Label,
		Quorum:         r.Quorum,
		Capacity:       r.Capacity,
		PositionLabels: labels,
		AcceptedTypes:  types,
		PinnedProducts: products,
		Impact:         r.Impact,
		Alternate:      r.Alternate,
	}
}

// effectiveSlotBody is one resolved slot for a system: the declaration, where it
// came from, and its staffing today. assigned and understaffed are served rather
// than left to the client so every surface reads staffing the same way.
type effectiveSlotBody struct {
	Name           string   `json:"name"`
	Label          string   `json:"label"`
	Quorum         int      `json:"quorum"`
	Capacity       *int     `json:"capacity,omitempty" doc:"The most components the slot will accept; null means no upper bound beyond quorum"`
	PositionLabels []string `json:"position_labels" doc:"Human labels for each position within the slot, by index; empty when unlabeled"`
	AcceptedTypes  []string `json:"accepted_types" doc:"The component_types a filling component's product must be classified within (self or a descendant); empty accepts any type"`
	PinnedProducts []string `json:"pinned_products" doc:"If set, a filling component's product must be one of these; empty accepts any product of an accepted type"`
	Impact         string   `json:"impact" doc:"What an impaired slot means for its system: outage, degraded, or none"`
	Alternate      string   `json:"alternate,omitempty" doc:"The choice/alternate this slot joins, addressed as \"choice-name/alternate-name\" (#626); absent when the slot is unconditional. The same form the write body takes, so a read round-trips into a write"`
	FromStandard   bool     `json:"from_standard" doc:"True when the slot is inherited from the system's standard; false when declared on the system"`
	AssignedTo     []string `json:"assigned_to" doc:"The component names filling this slot in this system"`
	// Positions is index-for-index with AssignedTo: Positions[i] is
	// AssignedTo[i]'s own 1-based position. A gap left by an unassign is
	// never compacted (#626), so the array is not assumed dense; a caller
	// reordering occupants (the console's drag-to-reorder) must read the
	// real position here rather than assume AssignedTo[i] sits at i+1.
	Positions []int `json:"positions" doc:"AssignedTo's own 1-based position, index for index; not necessarily i+1, since an unassign leaves a gap rather than compacting"`
	Assigned  int   `json:"assigned" doc:"How many components fill the slot"`
	// Understaffed counts assignment rows against quorum regardless of
	// health, so it can disagree with the health read's short for the same
	// slot at the same instant when an assigned component's own verdict is
	// outage: this answers "did enough components sign up", short answers
	// "is enough of it actually occupying the slot". Both are correct under
	// their own definition.
	Understaffed int `json:"understaffed" doc:"How many more assignment rows the slot wants before quorum, health-blind; zero when enough are assigned regardless of their own condition. See the health read's short for the occupancy-aware figure"`
}

func toEffectiveSlotBody(e *storage.EffectiveSlot) effectiveSlotBody {
	types, products, to, labels := e.AcceptedTypes, e.PinnedProducts, e.AssignedTo, e.PositionLabels
	if types == nil {
		types = []string{}
	}
	if products == nil {
		products = []string{}
	}
	if to == nil {
		to = []string{}
	}
	if labels == nil {
		labels = []string{}
	}
	positions := e.Positions
	if positions == nil {
		positions = []int{}
	}
	return effectiveSlotBody{
		Name:           e.Name,
		Label:          e.Label,
		Quorum:         e.Quorum,
		Capacity:       e.Capacity,
		PositionLabels: labels,
		AcceptedTypes:  types,
		PinnedProducts: products,
		Impact:         e.Impact,
		Alternate:      e.Alternate,
		FromStandard:   e.FromStandard,
		Positions:      positions,
		AssignedTo:     to,
		Assigned:       e.Assigned(),
		Understaffed:   e.Understaffed(),
	}
}

type listStandardSlotsOutput struct {
	Body struct {
		Slots []systemSlotBody `json:"slots"`
	}
}

type systemSlotOutput struct {
	Body systemSlotBody
}

type listSystemSlotsOutput struct {
	Body struct {
		System string              `json:"system"`
		Slots  []effectiveSlotBody `json:"slots"`
	}
}

// slotSpecBody is the declaration payload, shared by the standard and system
// arcs: the slot is addressed by the path, so the body carries only how it
// presents and what it requires.
//
// Every field is patched under the mask (#666, AIP-134): with no update_mask,
// the fields this body populated are the ones that change, so an omitted field
// keeps whatever the slot already declares. Naming a field in update_mask is
// what writes it regardless, which is the only way to CLEAR one, since a
// cleared field and an omitted field carry the same empty value on the wire.
type slotSpecBody struct {
	UpdateMask     []string `json:"update_mask,omitempty" doc:"Which fields this write changes (AIP-134). Omit it and the fields present in the body change and nothing else; name a field here and it is written even when the body leaves it empty, which is how a field is CLEARED; send [\"*\"] for full replacement, where every field the body omits goes back to its default. A field this resource does not patch is a 422 naming it"`
	Label          string   `json:"label,omitempty" doc:"The slot's human label; defaults to the slot name on first declare"`
	Quorum         int      `json:"quorum,omitempty" minimum:"0" doc:"How many components must fill the slot; one on first declare"`
	Capacity       *int     `json:"capacity,omitempty" minimum:"1" doc:"The most components the slot will accept; must be at least quorum, and unbounded on first declare. Name capacity in update_mask with no value here to clear it back to unbounded"`
	PositionLabels []string `json:"position_labels,omitempty" doc:"Human labels for each position within the slot, by index; replaces the label set wholesale when written. An empty list is not a populated field, so clearing the labels means naming position_labels in update_mask"`
	AcceptedTypes  []string `json:"accepted_types,omitempty" doc:"The component_types a filling component's product must be classified within (self or a descendant); replaces the accepted set wholesale when written, and an empty set accepts any type. Clearing it means naming accepted_types in update_mask"`
	PinnedProducts []string `json:"pinned_products,omitempty" doc:"If set, a filling component's product must be one of these; replaces the pinned set wholesale when written, and an empty set accepts any product of an accepted type. Clearing it means naming pinned_products in update_mask"`
	Impact         string   `json:"impact,omitempty" enum:"outage,degraded,none" doc:"What an impaired slot means for its system; degraded on first declare. The same broken component matters differently depending on the slot it was filling: a dead confidence monitor is not a dead main display"`
	// Alternate is *string, not string, because the empty string is the
	// explicit DETACH here rather than an absent value: a slot's alternate
	// silently detaching on an unrelated edit (#626) took a shipped
	// standard's systems from healthy to degraded with no audit trail and no
	// way back except raw SQL, so a set pointer always means the caller said
	// something about this field.
	Alternate *string `json:"alternate,omitempty" doc:"The choice/alternate this slot joins, addressed as \"choice-name/alternate-name\" (#626). An empty string detaches the slot, making it unconditional; an unknown choice or alternate is a 422"`
}

// resolveSlotWrite turns the body's update_mask into the write set the write
// obeys, refusing a mask this resource cannot honor with the field named.
//
// The implied mask (an absent update_mask) is read off the SPEC the write is
// about to store, not off the body field by field, so "which fields did the
// caller send" is defined once for both layers instead of once per layer,
// where the two definitions would drift.
func resolveSlotWrite(body slotSpecBody, spec storage.SystemSlotSpec) (storage.SystemSlotSpec, error) {
	write, err := updatemask.Resolve(body.UpdateMask, storage.SlotPatchFields, spec.Populated())
	if err != nil {
		return spec, err
	}
	spec.Write = write
	return spec, nil
}

type standardSlotPathInput struct {
	ID   string `path:"id" doc:"The standard, by name or uuid" example:"meeting-room"`
	Slot string `path:"slot" doc:"The slot name"`
}

type setStandardSlotInput struct {
	ID   string `path:"id" doc:"The standard, by name or uuid" example:"meeting-room"`
	Slot string `path:"slot" doc:"The slot name"`
	Body slotSpecBody
}

type systemSlotPathInput struct {
	Name string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Slot string `path:"slot" doc:"The slot name"`
}

type setSystemSlotInput struct {
	Name string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Slot string `path:"slot" doc:"The slot name"`
	Body slotSpecBody
}

type slotAssignmentPathInput struct {
	Name      string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Slot      string `path:"slot" doc:"The slot name"`
	Component string `path:"component" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
}

// swapSlotPositionsInput is the :swapPositions verb's input: the slot,
// addressed the same way every other slot route addresses it, and the two
// 1-based positions to exchange. Position stays an ordering attribute of an
// assignment rather than a second address for one (identity_shape.go's
// declaration that system_slot_assignment is id-only stays true): the verb
// is slot-level, not a per-position resource.
type swapSlotPositionsInput struct {
	Name string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Slot string `path:"slot" doc:"The slot name"`
	Body struct {
		// Named position/with, not a/b: a and b generate CLI flags --a and
		// --b, which say nothing about what they take (cligen derives a
		// flag per body field, so the field name IS the flag name).
		Position int `json:"position" minimum:"1" doc:"One of the two positions to exchange"`
		With     int `json:"with" minimum:"1" doc:"The other position to exchange with"`
	}
}

// registerSlotRoutes wires the two arcs of the slot surface.
func registerSlotRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	registerStandardSlotRoutes(api, a, gw)
	registerSystemSlotRoutes(api, a, gw)
}

// registerStandardSlotRoutes wires the standard's slot declarations, the
// system-side counterpart of its property contract and gated with it: read at
// the standard:read viewer floor, writes at standard:update / standard:delete.
func registerStandardSlotRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-standard-slots",
		Method:      http.MethodGet,
		Path:        "/standards/{id}/slots",
		Summary:     "List a standard's declared slots",
		Description: "Lists the slots this standard declares (every conforming system inherits them live), ordered by name, each with its quorum and the component_types (accepted_types) and, if pinned, the products a filling component must match. An unknown standard is a 404, never an empty list. Gated by standard:read.",
	}, "standard", "read"), func(ctx context.Context, in *standardPathInput) (*listStandardSlotsOutput, error) {
		slots, err := gw.ListSystemSlots(ctx, "standard", in.ID)
		if errors.Is(err, storage.ErrUnknownStandard) {
			return nil, huma.Error404NotFound("standard not found")
		}
		if err != nil {
			return nil, mapSlotErr(err)
		}
		out := &listStandardSlotsOutput{}
		out.Body.Slots = make([]systemSlotBody, 0, len(slots))
		for i := range slots {
			out.Body.Slots = append(out.Body.Slots, toSystemSlotBody(&slots[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-standard-slot",
		Method:      http.MethodPatch,
		Path:        "/standards/{id}/slots/{slot}",
		Summary:     "Declare a slot on a standard",
		Description: "Declares a slot every conforming system needs filled, or revises it in place (the slot is addressed by name, so the write is idempotent and declaring is this same route). Partial by default: the fields present in the body change and the rest of the declaration is left alone. update_mask overrides that, writing exactly the fields it names, which is how a field is cleared, and [\"*\"] replaces the whole declaration. An unknown standard, type, or product is a 422, as is a mask naming a field this resource does not patch. A name a conforming system already declares itself is a 409, since a slot name must mean one slot in a system, and so is a capacity below what a conforming system already holds; either names the conflicting system only if the caller may read it. Gated by standard:update.",
	}, "standard", "update"), func(ctx context.Context, in *setStandardSlotInput) (*systemSlotOutput, error) {
		altID, err := resolveAlternateBody(ctx, gw, "standard", in.ID, in.Body)
		if err != nil {
			return nil, mapSlotErr(err)
		}
		spec, err := resolveSlotWrite(in.Body, slotSpec(in.Slot, in.Body, altID))
		if err != nil {
			return nil, mapSlotErr(err)
		}
		r, err := gw.SetSystemSlot(ctx, actorID(ctx), "standard", in.ID, spec)
		if err != nil {
			return nil, mapSlotErr(redactRefusalSystem(ctx, a, gw, err))
		}
		return &systemSlotOutput{Body: toSystemSlotBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-standard-slot",
		Method:        http.MethodDelete,
		Path:          "/standards/{id}/slots/{slot}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a slot from a standard",
		Description:   "Removes the slot from the standard, and with it every assignment conforming systems made to it. A slot the standard does not declare is a 404. Gated by standard:delete.",
	}, "standard", "delete"), func(ctx context.Context, in *standardSlotPathInput) (*struct{}, error) {
		if err := gw.DeleteSystemSlot(ctx, actorID(ctx), "standard", in.ID, in.Slot); err != nil {
			return nil, mapSlotErr(err)
		}
		return nil, nil
	})
}

// registerSystemSlotRoutes wires the per-system resolved read, the ad-hoc
// declarations, and staffing. Every one resolves the system within the caller's
// read scope and then requires its update scope, so a system outside the read
// scope is a non-disclosing 404 and one the caller can read but not write is a 403.
func registerSystemSlotRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-system-slots",
		Method:      http.MethodGet,
		Path:        "/systems/{name}/slots",
		Summary:     "List a system's effective slots",
		Description: "Every slot this system needs filled: those its standard declares (from_standard true) plus those declared directly on it, each with the types it accepts (and products it pins, if any), the components filling it, and how many more it wants before quorum (understaffed). A one-off system shows only its own. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemPathInput) (*listSystemSlotsOutput, error) {
		slots, err := gw.EffectiveSlots(ctx, in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSlotErr(err)
		}
		out := &listSystemSlotsOutput{}
		out.Body.System = in.Name
		out.Body.Slots = make([]effectiveSlotBody, 0, len(slots))
		for i := range slots {
			out.Body.Slots = append(out.Body.Slots, toEffectiveSlotBody(&slots[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-system-slot",
		Method:      http.MethodPatch,
		Path:        "/systems/{name}/slots/{slot}",
		Summary:     "Declare a slot on a system",
		Description: "Declares a slot directly on this system (how a one-off system gets slots at all, and how a conforming one adds what its standard does not cover), or revises it in place. Partial by default: the fields present in the body change and the rest of the declaration is left alone. update_mask overrides that, writing exactly the fields it names, which is how a field is cleared, and [\"*\"] replaces the whole declaration. A name the system's standard already declares is a 409: a system slot never shadows an inherited one, since a slot name must mean one slot in a system. Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *setSystemSlotInput) (*systemSlotOutput, error) {
		sysID, err := requireSystemInScope(ctx, a, gw, in.Name)
		if err != nil {
			return nil, err
		}
		altID, err := resolveAlternateBody(ctx, gw, "system", sysID, in.Body)
		if err != nil {
			return nil, mapSlotErr(err)
		}
		spec, err := resolveSlotWrite(in.Body, slotSpec(in.Slot, in.Body, altID))
		if err != nil {
			return nil, mapSlotErr(err)
		}
		r, err := gw.SetSystemSlot(ctx, actorID(ctx), "system", sysID, spec)
		if err != nil {
			return nil, mapSlotErr(err)
		}
		return &systemSlotOutput{Body: toSystemSlotBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-system-slot",
		Method:        http.MethodDelete,
		Path:          "/systems/{name}/slots/{slot}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a slot from a system",
		Description:   "Removes a slot declared on this system, and with it every assignment to it. A slot the system does not declare itself is a 404 (a slot inherited from its standard is withdrawn on the standard, not here). Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *systemSlotPathInput) (*struct{}, error) {
		sysID, err := requireSystemInScope(ctx, a, gw, in.Name)
		if err != nil {
			return nil, err
		}
		if err := gw.DeleteSystemSlot(ctx, actorID(ctx), "system", sysID, in.Slot); err != nil {
			return nil, mapSlotErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "assign-system-slot",
		Method:        http.MethodPut,
		Path:          "/systems/{name}/slots/{slot}/assignments/{component}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Assign a component to a slot",
		Description:   "Puts this component in the slot for this system. Refused with a 422 naming both parties when the component is not a typed match: its product's component_type outside every type the slot accepts, or (if the slot pins products) its product not one of them. A slot with no accepted types takes any type. Idempotent. Gated by system:update, and also needs component:update, because staffing a slot makes the component a member and writes onto it: each end resolves in its own tier's read scope and must be inside its update scope, so an end outside the read scope is a non-disclosing 404 and one readable but not writable a 403.",
	}, "system", "update"), "component", "update"), func(ctx context.Context, in *slotAssignmentPathInput) (*struct{}, error) {
		if err := requireComponentEndPermission(ctx, a); err != nil {
			return nil, err
		}
		if err := gw.AssignSlot(ctx, actorID(ctx), in.Name, in.Slot, in.Component,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"),
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update")); err != nil {
			return nil, mapSlotErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "unassign-system-slot",
		Method:        http.MethodDelete,
		Path:          "/systems/{name}/slots/{slot}/assignments/{component}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Unassign a component from a slot",
		Description:   "Takes this component out of the slot, leaving the slot understaffed until another fills it. A component that was not filling the slot is a 404. Gated by system:update, and also needs component:update, as assigning does; read and update scopes drive the system's 404 versus 403 split, and an occupant outside the component:update scope is a 403 (the slot's own read already names it).",
	}, "system", "update"), "component", "update"), func(ctx context.Context, in *slotAssignmentPathInput) (*struct{}, error) {
		if err := requireComponentEndPermission(ctx, a); err != nil {
			return nil, err
		}
		if err := gw.UnassignSlot(ctx, actorID(ctx), in.Name, in.Slot, in.Component,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"),
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update")); err != nil {
			return nil, mapSlotErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "swap-slot-positions",
		Method:        http.MethodPost,
		Path:          "/systems/{name}/slots/{slot}:swapPositions",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Exchange two occupants' positions within a slot",
		Description:   "Exchanges the positions of whichever components currently hold position and with within this slot: an ordering change only, it does not affect who is assigned or the system's health. Either position missing an occupant is a 404. Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *swapSlotPositionsInput) (*struct{}, error) {
		// No requireSystemInScope here: unlike set/delete-system-slot,
		// SwapPositions takes a scope argument and resolves it itself
		// (ownerInScope, the same as AssignSlot and UnassignSlot), so a
		// second check ahead of it would only duplicate the gate.
		if err := gw.SwapPositions(ctx, actorID(ctx), in.Name, in.Slot, in.Body.Position, in.Body.With,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update")); err != nil {
			return nil, mapSlotErr(err)
		}
		return nil, nil
	})
}

// slotSpec fills the declaration spec from the path and body, field for field:
// nothing is defaulted here, because a default applied on the way in is
// indistinguishable from a value the caller sent, and the implied mask is
// exactly that distinction (the slot-name default for an absent label
// lives in SetSystemSlot, on the value, where it applies only to a write that
// actually writes the field). altID is the already-resolved AlternateID
// (resolveAlternateBody), not read from body directly: resolution needs the
// gateway and the owner, neither of which this function has, and keeping it a
// pure function here keeps slotSpec testable without a database.
func slotSpec(name string, body slotSpecBody, altID *string) storage.SystemSlotSpec {
	return storage.SystemSlotSpec{
		Name:           name,
		Label:          body.Label,
		Quorum:         body.Quorum,
		Capacity:       body.Capacity,
		PositionLabels: body.PositionLabels,
		AcceptedTypes:  body.AcceptedTypes,
		PinnedProducts: body.PinnedProducts,
		Impact:         body.Impact,
		AlternateID:    altID,
	}
}

// resolveAlternateBody turns a request body's optional "choice/alternate"
// wire reference into the id storage.SystemSlotSpec.AlternateID expects,
// nil when the caller omitted the field (SetSystemSlot then leaves whatever
// is already declared alone, the fix for #626's silent-detach-on-edit
// defect: every route that declares a slot used to construct AlternateID
// from nothing, which always wrote NULL over it). An unknown or malformed
// reference is storage.ErrSlotRefNotFound, mapped to 422 by mapSlotErr the
// same way a bad accepted type or pinned product is.
func resolveAlternateBody(ctx context.Context, gw storage.Gateway, ownerKind, ownerID string, body slotSpecBody) (*string, error) {
	if body.Alternate == nil {
		return nil, nil
	}
	id, err := gw.ResolveAlternate(ctx, ownerKind, ownerID, *body.Alternate)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// requireSystemInScope resolves the system a declaration is about to land on
// and returns its id. The declaration methods address their owner by kind and
// id and take no scope (a standard is not scope-scoped at all), so the system
// arc gets its ABAC check here, at the only route that can reach it. The id is
// returned rather than discarded so the caller can pass it straight on to the
// gateway calls that follow, instead of handing back the caller's raw reference
// for a second, redundant resolve: once names are no longer unique (#627), a
// second name-based resolve is not just wasted work, it is a second surface
// that can raise storage.ErrAmbiguousName with no mapping, on a reference the
// first resolve already proved unambiguous.
//
// It resolves through ResolveActionTarget with BOTH sets rather than through
// GetSystem with the update set alone (#736). Passing an action scope into a
// parameter that means read is what made a system the caller reads every day
// but may not write come back as "system not found": a lie about existence that
// an operator looking straight at the row reads as a broken platform rather than
// as the missing grant it is. The read set is the caller's own system:read, not
// a convenient wider one, which is the condition that keeps the 403 from
// disclosing a row the caller could not otherwise see.
func requireSystemInScope(ctx context.Context, a *authenticator, gw storage.Gateway, name string) (string, error) {
	id, err := gw.ResolveActionTarget(ctx, "system", name,
		a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"))
	if err != nil {
		return "", mapSystemErr(err)
	}
	return id, nil
}

// requireComponentInScope is its component-arc twin, used by the alarm routes,
// and returns the component's id for the same reason. action names the
// permission whose scope decides the write (component:update for raising and
// clearing); the read half is always the caller's own component:read.
//
// The read-only caller (the alarm LIST) passes "read" for both, which is the
// same set twice and therefore the plain non-disclosing read it always was: a
// route with nothing to act on has no third branch to pick.
//
// Both helpers also got cheaper on the way through, which is worth stating so it
// is not mistaken for an accident: GetSystem / GetComponent resolve through
// scopedGet, which runs attachPaths to fill the row's path and renders, and a
// component's full render costs a componentTypeIDForProduct lookup plus
// resolveTypeFacts' ancestor walk. Every caller here wanted an id. The resolve
// they use now does not build a render nobody reads.
func requireComponentInScope(ctx context.Context, a *authenticator, gw storage.Gateway, name, action string) (string, error) {
	id, err := gw.ResolveActionTarget(ctx, "component", name,
		a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", action))
	if err != nil {
		return "", mapComponentErr(err)
	}
	return id, nil
}

// slotCollisionMessage renders a slot-name collision (#503, ADR-0503) as the
// 409 every path that meets one answers, whichever write it was: it names the
// slot and both owners, and the recovery, so the same sentence serves a
// declaration, a standard change and a staffing write alike. A collision whose
// system the caller may not read (redactRefusalSystem) says so instead of
// naming it.
func slotCollisionMessage(err error) (string, bool) {
	var c *storage.SlotNameCollision
	if !errors.As(err, &c) {
		return "", false
	}
	if c.System == "" {
		return fmt.Sprintf("slot %q would mean two slots in a conforming system you cannot read: its standard %q declares it, and so does that system itself. A slot is addressed by its name within a system, so the name must mean one slot there; ask someone who can read the system to withdraw or rename its slot, or rename this one",
			c.Slot, c.Standard), true
	}
	return fmt.Sprintf("slot %q would mean two slots in system %q: its standard %q declares it, and so does the system itself. A slot is addressed by its name within a system, so the name must mean one slot there; withdraw or rename one of the two first",
		c.Slot, c.System, c.Standard), true
}

// redactRefusalSystem drops from a standard-arc refusal every system the
// caller may not read. A standard-arc declaration checks conforming systems
// across the fleet, and standard:update can be held beside a grant that reads
// no system at all, so the refusal must not tell that caller which system
// exists, what it declares, or how it is staffed (#503 review, #1216). A
// slot-name collision keeps its one system only if readable; a capacity
// shortfall names the worst-exceeding system the caller can read, or none
// and no count, so repeated probes cannot tally another scope's staffing.
// Any other error passes through.
func redactRefusalSystem(ctx context.Context, a *authenticator, gw storage.Gateway, err error) error {
	var c *storage.SlotNameCollision
	if errors.As(err, &c) && c.SystemID != "" {
		if _, gerr := gw.GetSystem(ctx, c.SystemID, a.scopeFor(ctx, "system", "read")); gerr != nil {
			redacted := *c
			redacted.System = ""
			return &redacted
		}
		return err
	}
	var short *storage.CapacityShortfall
	if errors.As(err, &short) && len(short.Exceeding) > 0 {
		ids := make([]string, len(short.Exceeding))
		for i, e := range short.Exceeding {
			ids[i] = e.SystemID
		}
		// A failed scope check fails closed: a 500 names nothing, where a
		// guessed redaction would tell a caller who reads every system that
		// it reads none of them.
		readable, serr := gw.InScopeIDs(ctx, "system", ids, a.scopeFor(ctx, "system", "read"))
		if serr != nil {
			return serr
		}
		redacted := *short
		redacted.System, redacted.Have = "", 0
		for _, e := range short.Exceeding {
			if readable[e.SystemID] {
				redacted.System, redacted.Have = e.System, e.Have
				break
			}
		}
		return &redacted
	}
	return err
}

// mapSlotErr translates the slot sentinels into HTTP status. The one that
// matters is the shortfall: a component that cannot fill a slot is a 422 that
// names both parties in operator vocabulary, what the component actually is
// (or which product it is) and what the slot wants, so the operator can act
// on the refusal rather than guess. A bare refusal would say nothing.
func mapSlotErr(err error) error {
	if platErr, ok := mapPlatformErr(err); ok {
		return platErr
	}
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	// A mask this resource cannot honor is a request fault that names the
	// field, the same shape as a shortfall: the caller stated an intent the
	// resource has no field for, and a silent no-op would leave them
	// believing a write landed.
	var maskErr *updatemask.Error
	if errors.As(err, &maskErr) {
		return huma.Error422UnprocessableEntity(maskErr.Error())
	}
	if msg, ok := slotCollisionMessage(err); ok {
		return huma.Error409Conflict(msg)
	}
	var typeShort *storage.TypeShortfall
	if errors.As(err, &typeShort) {
		want := append([]string(nil), typeShort.WantTypes...)
		sort.Strings(want)
		return huma.Error422UnprocessableEntity(fmt.Sprintf(
			"component %q is a %s; slot %q wants a %s",
			typeShort.Component, typeShort.ComponentType, typeShort.Slot, strings.Join(want, " or a ")))
	}
	var pinShort *storage.ProductPinShortfall
	if errors.As(err, &pinShort) {
		want := append([]string(nil), pinShort.WantProducts...)
		sort.Strings(want)
		return huma.Error422UnprocessableEntity(fmt.Sprintf(
			"component %q is product %q; slot %q wants product %s",
			pinShort.Component, pinShort.ComponentProduct, pinShort.Slot, strings.Join(want, " or ")))
	}
	// A double-staffing or capacity-shortfall refusal depends on OTHER rows
	// (what else is currently assigned), so it is a 409, not a 422: the
	// declaration or assignment request is not invalid on its own, it
	// conflicts with the fleet's current state.
	var staffed *storage.ComponentStaffedShortfall
	if errors.As(err, &staffed) {
		return huma.Error409Conflict(fmt.Sprintf(
			"component %q already fills %q in %q; a component fills at most one slot per system",
			staffed.Component, staffed.HeldSlot, staffed.System))
	}
	var capShort *storage.CapacityShortfall
	if errors.As(err, &capShort) {
		if capShort.System == "" {
			return huma.Error409Conflict(fmt.Sprintf(
				"slot %q is filled beyond %d components in a conforming system you cannot read; capacity cannot be lowered to %d until someone who can read it unassigns the excess",
				capShort.Slot, capShort.Want, capShort.Want))
		}
		return huma.Error409Conflict(fmt.Sprintf(
			"slot %q in system %q is filled by %d components; capacity cannot be lowered to %d",
			capShort.Slot, capShort.System, capShort.Have, capShort.Want))
	}
	var capFull *storage.CapacityFullShortfall
	if errors.As(err, &capFull) {
		return huma.Error409Conflict(fmt.Sprintf(
			"slot %q in system %q already holds %d of its %d-component capacity",
			capFull.Slot, capFull.System, capFull.Have, capFull.Capacity))
	}
	switch {
	case errors.Is(err, storage.ErrEntityNameIsUUID):
		return huma.Error422UnprocessableEntity("slot name may not be a uuid")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity("slot name must be lowercase letters, digits, and hyphens, starting with a letter or digit")
	case errors.Is(err, storage.ErrSlotNotFound):
		return huma.Error404NotFound("slot not found")
	case errors.Is(err, storage.ErrAssignmentMissing):
		return huma.Error404NotFound("component is not filling this slot")
	case errors.Is(err, storage.ErrSlotExists):
		return huma.Error409Conflict("a slot with this name is already declared here")
	case errors.Is(err, storage.ErrSlotRefNotFound):
		return huma.Error422UnprocessableEntity("unknown owner, type, or product")
	case errors.Is(err, storage.ErrSlotImpact):
		return huma.Error422UnprocessableEntity("impact must be outage, degraded, or none")
	case errors.Is(err, storage.ErrComponentAlreadyStaffed):
		return huma.Error409Conflict("component already fills a different slot in this system")
	case errors.Is(err, storage.ErrSlotPositionTaken):
		return huma.Error409Conflict("that position is already taken")
	case errors.Is(err, storage.ErrCapacityBelowQuorum):
		return huma.Error422UnprocessableEntity("capacity must be at least the slot's quorum")
	// Before the not-found cases, and a 403 rather than a 404: the staffing
	// routes resolve their system with the read-then-action split (#736), so
	// this sentinel means the caller can READ that system and simply may not
	// staff it. Reporting it absent would be false to a caller looking straight
	// at the row.
	case errors.Is(err, storage.ErrSystemForbidden):
		return huma.Error403Forbidden("forbidden")
	// The component end of a staffing write (#510): readable, or already an
	// occupant, but outside the caller's component:update scope.
	case errors.Is(err, storage.ErrComponentForbidden):
		return huma.Error403Forbidden(ErrComponentEndOutOfScope)
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error404NotFound("system not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	default:
		return huma.Error500InternalServerError("slot operation failed")
	}
}
