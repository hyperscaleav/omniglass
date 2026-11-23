package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// DeleteBlocker is one row standing in the way of a refused delete (#2463).
type DeleteBlocker struct {
	Kind  string `json:"kind" enum:"component,endpoint,system,location" doc:"The blocking row's own resource, so a client can link it and offer that kind's own remove."`
	ID    string `json:"id" format:"uuid" doc:"The blocking row's uuid."`
	Name  string `json:"name" doc:"The blocking row's name, its operator-facing address (an endpoint's is unique within its component)."`
	Label string `json:"label" doc:"What an operator reads for the blocking row: its label, or its name when it has none."`
}

// DeleteRefusal is the 409 a refused tree delete answers with: the problem
// detail every refusal carries, plus what is in the way. It is the ErrorModel
// with two optional fields added, so a client that reads only detail reads it
// exactly as before. Blockers are named only within the caller's read scope on
// each blocker's own kind; the rest are counted in blockers_hidden, never
// named (the non-disclosure every scoped read keeps).
type DeleteRefusal struct {
	huma.ErrorModel
	Blockers       []DeleteBlocker `json:"blockers,omitempty" doc:"The rows in the way that the caller may read, each to be removed or moved before the delete can succeed."`
	BlockersHidden int             `json:"blockers_hidden,omitempty" doc:"How many more rows are in the way that are not listed: outside the caller's read scope, or past the listing cap."`
}

// withDeleteRefusal declares a tree delete's 409 as a DeleteRefusal in the
// spec (so the generated client types the blockers) and keeps the default
// error response every other refusal (404, 403) answers with.
func withDeleteRefusal(api huma.API, op huma.Operation) huma.Operation {
	reg := api.OpenAPI().Components.Schemas
	errSchema := reg.Schema(reflect.TypeOf(huma.ErrorModel{}), true, "ErrorModel")
	refusal := reg.Schema(reflect.TypeOf(DeleteRefusal{}), true, "DeleteRefusal")
	if op.Responses == nil {
		op.Responses = map[string]*huma.Response{}
	}
	op.Responses["409"] = &huma.Response{
		Description: "Refused: something still depends on the row. blockers names what the caller may see; blockers_hidden counts the rest.",
		Content:     map[string]*huma.MediaType{"application/problem+json": {Schema: refusal}},
	}
	op.Responses["default"] = &huma.Response{
		Description: "Error",
		Content:     map[string]*huma.MediaType{"application/problem+json": {Schema: errSchema}},
	}
	return op
}

// deleteRefusal turns a mapped delete error into a DeleteRefusal when it is a
// 409 the blockers explain (occupancy, owned endpoints, a restrict reference),
// reading them within the caller's own read scopes. Any other error, or a
// blocker read that fails or finds nothing, returns mapped unchanged: the
// list is an aid to the refusal, never a reason to replace it with a 500.
func (a *authenticator) deleteRefusal(ctx context.Context, gw storage.Gateway, kind, ref string, cause, mapped error) error {
	if !isBlockedDelete(cause) {
		return mapped
	}
	var em *huma.ErrorModel
	if !errors.As(mapped, &em) || em.Status != http.StatusConflict {
		return mapped
	}
	bl, err := gw.DeleteBlockersOf(ctx, kind, ref, func(k string) scope.Set { return a.scopeFor(ctx, k, "read") })
	if err != nil || bl == nil || (len(bl.Items) == 0 && bl.Hidden == 0) {
		return mapped
	}
	out := &DeleteRefusal{ErrorModel: *em, BlockersHidden: bl.Hidden}
	for _, b := range bl.Items {
		out.Blockers = append(out.Blockers, DeleteBlocker{Kind: b.Kind, ID: b.ID, Name: b.Name, Label: b.Label})
	}
	return out
}

func isBlockedDelete(err error) bool {
	return errors.Is(err, storage.ErrReferenced) ||
		errors.Is(err, storage.ErrComponentOccupied) ||
		errors.Is(err, storage.ErrComponentHasEndpoints) ||
		errors.Is(err, storage.ErrSystemOccupied) ||
		errors.Is(err, storage.ErrLocationOccupied)
}
