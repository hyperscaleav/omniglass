package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// capabilityBody is the wire shape of a capability registry row. The registry
// lists alphabetically by display_name, like component_type.
type capabilityBody struct {
	ID          string `json:"id" doc:"The capability's uuid, the stable handle that survives a rename"`
	Name        string `json:"name" doc:"The kebab handle an operator reads and types; renameable"`
	DisplayName string `json:"display_name"`
	Official    bool   `json:"official"`
}

func toCapabilityBody(c *storage.Capability) capabilityBody {
	return capabilityBody{ID: c.ID, Name: c.Name, DisplayName: c.DisplayName, Official: c.Official}
}

type listCapabilitiesOutput struct {
	Body struct {
		Capabilities []capabilityBody `json:"capabilities"`
	}
}

type capabilityPathInput struct {
	ID string `path:"id" doc:"The capability id"`
}

type createCapabilityInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" doc:"The globally unique kebab handle; renameable"`
		DisplayName string `json:"display_name" minLength:"1"`
	}
}

type updateCapabilityInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty"`
	}
}

type capabilityOutput struct {
	Body capabilityBody
}

// registerCapabilityRoutes wires the capability registry CRUD surface, on the
// same pattern as the component/location/system type registries. Gated by
// capability:read|create|update|delete: capability:read sits in the viewer
// read-floor (*:read), the mutations at the admin tier, exactly like type:*.
func registerCapabilityRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-capabilities",
		Method:      http.MethodGet,
		Path:        "/capabilities",
		Summary:     "List capabilities",
		Description: "Lists the capability registry, ordered alphabetically by display name. Populates the capability picker on the product form. Gated by capability:read.",
	}, "capability", "read"), func(ctx context.Context, _ *struct{}) (*listCapabilitiesOutput, error) {
		caps, err := gw.ListCapabilities(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list capabilities")
		}
		out := &listCapabilitiesOutput{}
		out.Body.Capabilities = make([]capabilityBody, 0, len(caps))
		for i := range caps {
			out.Body.Capabilities = append(out.Body.Capabilities, toCapabilityBody(&caps[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-capability",
		Method:        http.MethodPost,
		Path:          "/capabilities",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a capability",
		Description:   "Creates a custom (non-official) capability. Gated by capability:create.",
	}, "capability", "create"), func(ctx context.Context, in *createCapabilityInput) (*capabilityOutput, error) {
		c, err := gw.CreateCapability(ctx, actorID(ctx), storage.Capability{
			Name: in.Body.Name, DisplayName: in.Body.DisplayName,
		})
		if err != nil {
			return nil, mapTypeErr(err, "capability")
		}
		return &capabilityOutput{Body: toCapabilityBody(c)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-capability",
		Method:      http.MethodGet,
		Path:        "/capabilities/{id}",
		Summary:     "Get a capability",
		Description: "Fetches a capability by id. Gated by capability:read.",
	}, "capability", "read"), func(ctx context.Context, in *capabilityPathInput) (*capabilityOutput, error) {
		c, err := gw.GetCapability(ctx, in.ID)
		if err != nil {
			return nil, mapTypeErr(err, "capability")
		}
		return &capabilityOutput{Body: toCapabilityBody(c)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-capability",
		Method:      http.MethodPatch,
		Path:        "/capabilities/{id}",
		Summary:     "Update a capability",
		Description: "Patches a custom capability's display_name. Official capabilities are read-only (422). Gated by capability:update.",
	}, "capability", "update"), func(ctx context.Context, in *updateCapabilityInput) (*capabilityOutput, error) {
		c, err := gw.UpdateCapability(ctx, actorID(ctx), in.ID, storage.CapabilityPatch{
			DisplayName: in.Body.DisplayName,
		})
		if err != nil {
			return nil, mapTypeErr(err, "capability")
		}
		return &capabilityOutput{Body: toCapabilityBody(c)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-capability",
		Method:        http.MethodDelete,
		Path:          "/capabilities/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a capability",
		Description:   "Deletes a custom capability, refused if official (422). Gated by capability:delete.",
	}, "capability", "delete"), func(ctx context.Context, in *capabilityPathInput) (*struct{}, error) {
		if err := gw.DeleteCapability(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "capability")
		}
		return nil, nil
	})
}
