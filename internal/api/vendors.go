package api

import (
	"context"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// vendorBody is the wire shape of a vendor registry row. The registry lists
// alphabetically by display_name, like component_type.
type vendorBody struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name"`
	Kind         string `json:"kind" enum:"manufacturer,integrator,developer"`
	Icon         string `json:"icon,omitempty"`
	SupportPhone string `json:"support_phone,omitempty"`
	Website      string `json:"website,omitempty"`
	Official     bool   `json:"official"`
}

func toVendorBody(m *storage.Vendor) vendorBody {
	return vendorBody{
		ID: m.ID, DisplayName: m.DisplayName, Kind: m.Kind, Icon: m.Icon,
		SupportPhone: m.SupportPhone, Website: m.Website, Official: m.Official,
	}
}

type listVendorsOutput struct {
	Body struct {
		Vendors []vendorBody `json:"vendors"`
	}
}

type vendorPathInput struct {
	ID string `path:"id" doc:"The vendor id"`
}

type createVendorInput struct {
	Body struct {
		ID           string `json:"id" minLength:"1" doc:"Globally unique vendor id"`
		DisplayName  string `json:"display_name" minLength:"1"`
		Kind         string `json:"kind,omitempty" enum:"manufacturer,integrator,developer" default:"manufacturer"`
		Icon         string `json:"icon,omitempty"`
		SupportPhone string `json:"support_phone,omitempty"`
		Website      string `json:"website,omitempty"`
	}
}

type updateVendorInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName  *string `json:"display_name,omitempty"`
		Kind         *string `json:"kind,omitempty" enum:"manufacturer,integrator,developer"`
		Icon         *string `json:"icon,omitempty"`
		SupportPhone *string `json:"support_phone,omitempty"`
		Website      *string `json:"website,omitempty"`
	}
}

type vendorOutput struct {
	Body vendorBody
}

// validVendorKind reports whether kind is one of the closed vendor-kind set
// (manufacturer/integrator/developer). The DB CHECK constraint enforces it too;
// this rejects a bad value at the edge with a clean 422 instead of a 500.
func validVendorKind(kind string) bool {
	switch kind {
	case "manufacturer", "integrator", "developer":
		return true
	}
	return false
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

// registerVendorRoutes wires the vendor registry CRUD surface, on the same
// pattern as the component/location/system type registries. Gated by
// vendor:read|create|update|delete: vendor:read sits in the viewer read-floor
// (*:read), the mutations at the admin tier, exactly like type:*.
func registerVendorRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-vendors",
		Method:      http.MethodGet,
		Path:        "/vendors",
		Summary:     "List vendors",
		Description: "Lists the vendor registry, ordered alphabetically by display name. Populates the vendor picker on the product form. Gated by vendor:read.",
	}, "vendor", "read"), func(ctx context.Context, _ *struct{}) (*listVendorsOutput, error) {
		makes, err := gw.ListVendors(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list vendors")
		}
		out := &listVendorsOutput{}
		out.Body.Vendors = make([]vendorBody, 0, len(makes))
		for i := range makes {
			out.Body.Vendors = append(out.Body.Vendors, toVendorBody(&makes[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-vendor",
		Method:        http.MethodPost,
		Path:          "/vendors",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a vendor",
		Description:   "Creates a custom (non-official) vendor. Gated by vendor:create.",
	}, "vendor", "create"), func(ctx context.Context, in *createVendorInput) (*vendorOutput, error) {
		if in.Body.Kind == "" {
			in.Body.Kind = string(storage.VendorManufacturer) // default, matches the column default
		}
		if !validVendorKind(in.Body.Kind) {
			return nil, huma.Error422UnprocessableEntity("kind must be one of manufacturer, integrator, developer")
		}
		if !validWebsiteScheme(in.Body.Website) {
			return nil, huma.Error422UnprocessableEntity("website must be an http or https URL")
		}
		m, err := gw.CreateVendor(ctx, actorID(ctx), storage.Vendor{
			ID: in.Body.ID, DisplayName: in.Body.DisplayName, Kind: in.Body.Kind, Icon: in.Body.Icon,
			SupportPhone: in.Body.SupportPhone, Website: in.Body.Website,
		})
		if err != nil {
			return nil, mapTypeErr(err, "vendor")
		}
		return &vendorOutput{Body: toVendorBody(m)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-vendor",
		Method:      http.MethodGet,
		Path:        "/vendors/{id}",
		Summary:     "Get a vendor",
		Description: "Fetches a vendor by id. Gated by vendor:read.",
	}, "vendor", "read"), func(ctx context.Context, in *vendorPathInput) (*vendorOutput, error) {
		m, err := gw.GetVendor(ctx, in.ID)
		if err != nil {
			return nil, mapTypeErr(err, "vendor")
		}
		return &vendorOutput{Body: toVendorBody(m)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-vendor",
		Method:      http.MethodPatch,
		Path:        "/vendors/{id}",
		Summary:     "Update a vendor",
		Description: "Patches a custom vendor's display_name, kind, icon, support_phone, or website. Official vendors are read-only (422). Gated by vendor:update.",
	}, "vendor", "update"), func(ctx context.Context, in *updateVendorInput) (*vendorOutput, error) {
		if in.Body.Kind != nil && !validVendorKind(*in.Body.Kind) {
			return nil, huma.Error422UnprocessableEntity("kind must be one of manufacturer, integrator, developer")
		}
		if in.Body.Website != nil && !validWebsiteScheme(*in.Body.Website) {
			return nil, huma.Error422UnprocessableEntity("website must be an http or https URL")
		}
		m, err := gw.UpdateVendor(ctx, actorID(ctx), in.ID, storage.VendorPatch{
			DisplayName: in.Body.DisplayName, Kind: in.Body.Kind, Icon: in.Body.Icon,
			SupportPhone: in.Body.SupportPhone, Website: in.Body.Website,
		})
		if err != nil {
			return nil, mapTypeErr(err, "vendor")
		}
		return &vendorOutput{Body: toVendorBody(m)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-vendor",
		Method:        http.MethodDelete,
		Path:          "/vendors/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a vendor",
		Description:   "Deletes a custom vendor, refused if official (422). Gated by vendor:delete.",
	}, "vendor", "delete"), func(ctx context.Context, in *vendorPathInput) (*struct{}, error) {
		if err := gw.DeleteVendor(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "vendor")
		}
		return nil, nil
	})
}
