package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The preview-then-apply verb for generated labels (#685).
//
// One pair of custom methods per labelled entity kind rather than one pair
// taking the kind in a body, because the permission is the entity's own
// :update and a route's gate is a constant: a body field deciding which
// permission applies would be a gate the spec cannot publish and the roles view
// cannot report. Three collections, six operations, one handler.
//
// Both halves are gated by :update rather than the preview by :read. A preview
// is half of an edit, not a report: it exists to be acted on, and an operator
// who cannot apply has no use for the blast radius of a change they cannot
// make. Gating them alike also makes "a viewer cannot trigger a recompute" one
// fact rather than two.

// labelChangeBody is one row a recompute would alter, or did.
//
// Kind travels on every row because a location recompute answers with rows of
// three kinds: moving a location's label stales the components and systems
// placed at it, and a preview that hid them would understate the blast radius
// of the button it is describing.
type labelChangeBody struct {
	Kind string `json:"kind" doc:"The entity kind this row belongs to: component, system, or location"`
	ID   string `json:"id" doc:"The entity's uuid"`
	Name string `json:"name" doc:"The entity's name, for display"`
	From string `json:"from" doc:"The label as stored now; empty when the entity has none"`
	To   string `json:"to" doc:"The label its rules produce; empty when the rule renders nothing"`
}

type labelRecomputeOutput struct {
	Body struct {
		Count   int               `json:"count" doc:"How many rows changed, or would change"`
		Changed []labelChangeBody `json:"changed" doc:"Every row, one entry each. Bounded by the estate, not paginated: this is the whole blast radius by design"`
	}
}

func toLabelRecomputeOutput(changes []storage.LabelChange) *labelRecomputeOutput {
	out := &labelRecomputeOutput{}
	out.Body.Count = len(changes)
	out.Body.Changed = make([]labelChangeBody, 0, len(changes))
	for _, ch := range changes {
		out.Body.Changed = append(out.Body.Changed, labelChangeBody{
			Kind: ch.Kind, ID: ch.ID, Name: ch.Name, From: ch.From, To: ch.To,
		})
	}
	return out
}

func mapLabelRecomputeErr(err error) error {
	if errors.Is(err, storage.ErrLabelKindUnknown) {
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return huma.Error500InternalServerError("recompute labels", err)
}

// registerLabelRecomputeRoutes wires the pair for one entity kind. Called once
// per kind from that kind's own route registration, so the operations sit
// beside the resource they act on in the generated reference and the CLI.
func registerLabelRecomputeRoutes(api huma.API, a *authenticator, gw storage.Gateway, kind, collection string) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "preview-" + kind + "-labels",
		Method:      http.MethodPost,
		Path:        collection + ":previewLabels",
		Summary:     "Preview a " + kind + " label recompute",
		Description: "Lists exactly the rows a recompute would change, and leaves the estate as it found it. Use it before :recomputeLabels to see the blast radius of a rule edit. Every generated label in the caller's read scope is re-rendered from its current rules and compared with what is stored; a label an operator typed by hand is never a candidate. A location preview also lists the components and systems placed at every location whose label would move, because those go stale the moment it does. Gated by " + kind + ":update, the same permission the apply needs: a preview is half of an edit rather than a report, and an operator who cannot apply has no use for it.",
	}, kind, "update"), func(ctx context.Context, _ *struct{}) (*labelRecomputeOutput, error) {
		changes, err := gw.PreviewLabelRecompute(ctx, kind, a.scopeFor(ctx, kind, "read"))
		if err != nil {
			return nil, mapLabelRecomputeErr(err)
		}
		return toLabelRecomputeOutput(changes), nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "recompute-" + kind + "-labels",
		Method:      http.MethodPost,
		Path:        collection + ":recomputeLabels",
		Summary:     "Recompute " + kind + " labels",
		Description: "Applies what :previewLabels describes, over the rows in the caller's read and update scope, and returns exactly what it changed. Idempotent: a second call changes nothing. A label an operator typed by hand is left alone, and clearing that label by hand is how it is handed back to the platform. Recorded as ONE audit row for the operation, naming the rule tier and the affected count, rather than one row per changed entity. Gated by " + kind + ":update.",
	}, kind, "update"), func(ctx context.Context, _ *struct{}) (*labelRecomputeOutput, error) {
		changes, err := gw.RecomputeLabels(ctx, actorID(ctx), kind,
			a.scopeFor(ctx, kind, "read"), a.scopeFor(ctx, kind, "update"))
		if err != nil {
			return nil, mapLabelRecomputeErr(err)
		}
		return toLabelRecomputeOutput(changes), nil
	})
}
