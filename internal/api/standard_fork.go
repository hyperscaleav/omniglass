package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/template"
)

type forkStandardInput struct {
	Body struct {
		Template string `json:"template" minLength:"1" doc:"The standard template to fork, by name" example:"meeting-room"`
		Name     string `json:"name,omitempty" doc:"The name the standard takes; omitted takes the template's"`
		Label    string `json:"label,omitempty" doc:"The standard's label; omitted takes the template's"`
		Parent   string `json:"parent,omitempty" doc:"For a variant, the standard it hangs off, by name or uuid; omitted takes your standard named for the template's parent"`
	}
}

// registerStandardFork wires POST /standards:fork over cat: one standard of
// the caller's own, written whole from a standard template in one
// transaction (#827, ADR-0827). It writes nothing the standard's own routes
// could not, so it is gated by standard:create, and the template's name is
// recorded in the audit detail and nowhere else.
func registerStandardFork(api huma.API, a *authenticator, gw storage.Gateway, cat *template.Catalog) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "fork-standard",
		Extensions:    mcpExposed(),
		Method:        http.MethodPost,
		Path:          "/standards:fork",
		DefaultStatus: http.StatusCreated,
		Summary:       "Fork a standard from a template",
		Description: "Writes one standard of your own (official false) from a standard template in the catalog: its roles, choices, property and metric contract lines and map, in one transaction, audited once as a fork naming the template. " +
			"Nothing points back at the template, so a later release can change the template without touching your standard, and your standard renames, edits and deletes like any you created. " +
			"The name is the template's unless you give one; a taken name is a 409, so forking the same template again needs a second name. " +
			"A variant forks only once you hold the standard it hangs off (the parent you name, or yours named for the template's parent), else a 422 naming the template to fork first. " +
			"An unknown template is a 404. Gated by standard:create.",
	}, "standard", "create"), func(ctx context.Context, in *forkStandardInput) (*standardOutput, error) {
		t, err := cat.GetKind(in.Body.Template, template.KindStandard)
		if err != nil {
			return nil, huma.Error404NotFound(fmt.Sprintf("standard template %s is not in the catalog", in.Body.Template))
		}
		st, err := template.ForkStandard(ctx, gw, cat, actorID(ctx), t.Name, template.ForkOptions{
			Name: in.Body.Name, Label: in.Body.Label, Parent: in.Body.Parent,
		})
		if err != nil {
			return nil, mapForkErr(err, t, in.Body.Parent)
		}
		return &standardOutput{Body: toStandardBody(st)}, nil
	})
}

func mapForkErr(err error, t *template.Template, parent string) error {
	switch {
	case errors.Is(err, storage.ErrForkParentMissing):
		if parent != "" {
			return huma.Error422UnprocessableEntity(fmt.Sprintf("%s is a variant, and the standard %s it would hang off does not exist", t.Name, parent))
		}
		return huma.Error422UnprocessableEntity(fmt.Sprintf("%s is a variant of %s: fork %s first, or name the standard it hangs off as parent", t.Name, t.Standard.Parent, t.Standard.Parent))
	case errors.Is(err, storage.ErrTypeExists):
		return huma.Error409Conflict("a standard of that name already exists; fork under another name")
	case errors.Is(err, storage.ErrRoleRefNotFound), errors.Is(err, storage.ErrPropertyTypeNotFound), errors.Is(err, storage.ErrMetricTypeNotFound):
		return huma.Error422UnprocessableEntity(fmt.Sprintf("template %s names a component type, product, property or metric this fleet's catalogs do not hold", t.Name))
	}
	return mapStandardErr(err)
}
