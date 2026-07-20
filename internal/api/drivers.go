package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// driverBody is the wire shape of a driver registry row. The registry lists
// alphabetically by display_name, like vendor.
type driverBody struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version,omitempty"`
	Official    bool   `json:"official"`
}

func toDriverBody(d *storage.Driver) driverBody {
	return driverBody{
		ID: d.ID, DisplayName: d.DisplayName, Version: d.Version, Official: d.Official,
	}
}

type listDriversOutput struct {
	Body struct {
		Drivers []driverBody `json:"drivers"`
	}
}

type driverPathInput struct {
	ID string `path:"id" doc:"The driver id"`
}

type createDriverInput struct {
	Body struct {
		ID          string `json:"id" minLength:"1" doc:"Globally unique driver id"`
		DisplayName string `json:"display_name" minLength:"1"`
		Version     string `json:"version,omitempty"`
	}
}

type updateDriverInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty"`
		Version     *string `json:"version,omitempty"`
	}
}

type driverOutput struct {
	Body driverBody
}

// registerDriverRoutes wires the driver registry CRUD surface, on the same
// pattern as the vendor and component/location/system type registries. Gated by
// driver:read|create|update|delete: driver:read sits in the viewer read-floor
// (*:read), the mutations at the admin tier, exactly like type:*.
func registerDriverRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-drivers",
		Method:      http.MethodGet,
		Path:        "/drivers",
		Summary:     "List drivers",
		Description: "Lists the driver registry, ordered alphabetically by display name. Populates the driver picker on the product form. Gated by driver:read.",
	}, "driver", "read"), func(ctx context.Context, _ *struct{}) (*listDriversOutput, error) {
		drivers, err := gw.ListDrivers(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list drivers")
		}
		out := &listDriversOutput{}
		out.Body.Drivers = make([]driverBody, 0, len(drivers))
		for i := range drivers {
			out.Body.Drivers = append(out.Body.Drivers, toDriverBody(&drivers[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-driver",
		Method:        http.MethodPost,
		Path:          "/drivers",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a driver",
		Description:   "Creates a custom (non-official) driver. Gated by driver:create.",
	}, "driver", "create"), func(ctx context.Context, in *createDriverInput) (*driverOutput, error) {
		d, err := gw.CreateDriver(ctx, actorID(ctx), storage.Driver{
			ID: in.Body.ID, DisplayName: in.Body.DisplayName, Version: in.Body.Version,
		})
		if err != nil {
			return nil, mapTypeErr(err, "driver")
		}
		return &driverOutput{Body: toDriverBody(d)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-driver",
		Method:      http.MethodGet,
		Path:        "/drivers/{id}",
		Summary:     "Get a driver",
		Description: "Fetches a driver by id. Gated by driver:read.",
	}, "driver", "read"), func(ctx context.Context, in *driverPathInput) (*driverOutput, error) {
		d, err := gw.GetDriver(ctx, in.ID)
		if err != nil {
			return nil, mapTypeErr(err, "driver")
		}
		return &driverOutput{Body: toDriverBody(d)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-driver",
		Method:      http.MethodPatch,
		Path:        "/drivers/{id}",
		Summary:     "Update a driver",
		Description: "Patches a custom driver's display_name or version. Official drivers are read-only (422). Gated by driver:update.",
	}, "driver", "update"), func(ctx context.Context, in *updateDriverInput) (*driverOutput, error) {
		d, err := gw.UpdateDriver(ctx, actorID(ctx), in.ID, storage.DriverPatch{
			DisplayName: in.Body.DisplayName, Version: in.Body.Version,
		})
		if err != nil {
			return nil, mapTypeErr(err, "driver")
		}
		return &driverOutput{Body: toDriverBody(d)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-driver",
		Method:        http.MethodDelete,
		Path:          "/drivers/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a driver",
		Description:   "Deletes a custom driver, refused if official (422). Gated by driver:delete.",
	}, "driver", "delete"), func(ctx context.Context, in *driverPathInput) (*struct{}, error) {
		if err := gw.DeleteDriver(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "driver")
		}
		return nil, nil
	})
}
