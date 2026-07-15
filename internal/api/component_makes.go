package api

import (
	"context"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// componentMakeBody is the wire shape of a component_make registry row. The
// registry lists alphabetically by display_name, like component_type.
type componentMakeBody struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name"`
	Icon         string `json:"icon,omitempty"`
	SupportPhone string `json:"support_phone,omitempty"`
	Website      string `json:"website,omitempty"`
	Official     bool   `json:"official"`
}

func toComponentMakeBody(m *storage.ComponentMake) componentMakeBody {
	return componentMakeBody{
		ID: m.ID, DisplayName: m.DisplayName, Icon: m.Icon,
		SupportPhone: m.SupportPhone, Website: m.Website, Official: m.Official,
	}
}

type listComponentMakesOutput struct {
	Body struct {
		Makes []componentMakeBody `json:"makes"`
	}
}

type componentMakePathInput struct {
	ID string `path:"id" doc:"The component_make id"`
}

type createComponentMakeInput struct {
	Body struct {
		ID           string `json:"id" minLength:"1" doc:"Globally unique make id"`
		DisplayName  string `json:"display_name" minLength:"1"`
		Icon         string `json:"icon,omitempty"`
		SupportPhone string `json:"support_phone,omitempty"`
		Website      string `json:"website,omitempty"`
	}
}

type updateComponentMakeInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName  *string `json:"display_name,omitempty"`
		Icon         *string `json:"icon,omitempty"`
		SupportPhone *string `json:"support_phone,omitempty"`
		Website      *string `json:"website,omitempty"`
	}
}

type componentMakeOutput struct {
	Body componentMakeBody
}

// validWebsiteScheme is defense-in-depth against a stored javascript:/data:
// href: an empty website is fine (the field is optional), but a non-empty
// one must parse as an absolute http(s) URL. The client applies the same
// scheme check before it renders a link; this closes the gap for a
// non-browser caller (CLI/curl) that bypasses the client entirely.
func validWebsiteScheme(website string) bool {
	if website == "" {
		return true
	}
	u, err := url.Parse(website)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// registerComponentMakeRoutes wires the component_make registry CRUD surface,
// on the same pattern as the component/location/system type registries. Gated
// by make:read|create|update|delete: make:read sits in the viewer read-floor
// (*:read), the mutations at the admin tier, exactly like type:*.
func registerComponentMakeRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-component-makes",
		Method:      http.MethodGet,
		Path:        "/component-makes",
		Summary:     "List component makes",
		Description: "Lists the component_make registry, ordered alphabetically by display name. Populates the make picker on the component_model form. Gated by make:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("make", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listComponentMakesOutput, error) {
		makes, err := gw.ListComponentMakes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list component makes")
		}
		out := &listComponentMakesOutput{}
		out.Body.Makes = make([]componentMakeBody, 0, len(makes))
		for i := range makes {
			out.Body.Makes = append(out.Body.Makes, toComponentMakeBody(&makes[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-component-make",
		Method:        http.MethodPost,
		Path:          "/component-makes",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a component make",
		Description:   "Creates a custom (non-official) component_make. Gated by make:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("make", "create")},
	}, func(ctx context.Context, in *createComponentMakeInput) (*componentMakeOutput, error) {
		if !validWebsiteScheme(in.Body.Website) {
			return nil, huma.Error422UnprocessableEntity("website must be an http or https URL")
		}
		m, err := gw.CreateComponentMake(ctx, actorID(ctx), storage.ComponentMake{
			ID: in.Body.ID, DisplayName: in.Body.DisplayName, Icon: in.Body.Icon,
			SupportPhone: in.Body.SupportPhone, Website: in.Body.Website,
		})
		if err != nil {
			return nil, mapTypeErr(err, "component_make")
		}
		return &componentMakeOutput{Body: toComponentMakeBody(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-component-make",
		Method:      http.MethodGet,
		Path:        "/component-makes/{id}",
		Summary:     "Get a component make",
		Description: "Fetches a component_make by id. Gated by make:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("make", "read")},
	}, func(ctx context.Context, in *componentMakePathInput) (*componentMakeOutput, error) {
		m, err := gw.GetComponentMake(ctx, in.ID)
		if err != nil {
			return nil, mapTypeErr(err, "component_make")
		}
		return &componentMakeOutput{Body: toComponentMakeBody(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-component-make",
		Method:      http.MethodPatch,
		Path:        "/component-makes/{id}",
		Summary:     "Update a component make",
		Description: "Patches a custom component_make's display_name, icon, support_phone, or website. Official makes are read-only (422). Gated by make:update.",
		Middlewares: huma.Middlewares{a.authn, a.require("make", "update")},
	}, func(ctx context.Context, in *updateComponentMakeInput) (*componentMakeOutput, error) {
		if in.Body.Website != nil && !validWebsiteScheme(*in.Body.Website) {
			return nil, huma.Error422UnprocessableEntity("website must be an http or https URL")
		}
		m, err := gw.UpdateComponentMake(ctx, actorID(ctx), in.ID, storage.ComponentMakePatch{
			DisplayName: in.Body.DisplayName, Icon: in.Body.Icon,
			SupportPhone: in.Body.SupportPhone, Website: in.Body.Website,
		})
		if err != nil {
			return nil, mapTypeErr(err, "component_make")
		}
		return &componentMakeOutput{Body: toComponentMakeBody(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-component-make",
		Method:        http.MethodDelete,
		Path:          "/component-makes/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a component make",
		Description:   "Deletes a custom component_make, refused if official (422). Gated by make:delete.",
		Middlewares:   huma.Middlewares{a.authn, a.require("make", "delete")},
	}, func(ctx context.Context, in *componentMakePathInput) (*struct{}, error) {
		if err := gw.DeleteComponentMake(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "component_make")
		}
		return nil, nil
	})
}
