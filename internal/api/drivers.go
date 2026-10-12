package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// driverBody is the wire shape of a driver registry row. The registry lists
// alphabetically by label, like vendor.
type driverBody struct {
	ID       string          `json:"id" doc:"The driver's uuid, the stable handle that survives a rename"`
	Name     string          `json:"name" doc:"The name an operator reads and types; renameable"`
	Label    string          `json:"label"`
	Version  string          `json:"version,omitempty"`
	Official bool            `json:"official"`
	Spec     json.RawMessage `json:"spec,omitempty" doc:"The declarative spec body (#813): transports, inputs, poll functions, listeners, command bindings. Absent on a stub that cannot be attached yet"`
}

func toDriverBody(d *storage.Driver) driverBody {
	return driverBody{
		ID: d.ID, Name: d.Name, Label: d.Label, Version: d.Version, Official: d.Official,
		Spec: json.RawMessage(d.Spec),
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
		Name    string          `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The globally unique name; renameable"`
		Label   string          `json:"label" minLength:"1" doc:"What an operator reads in pickers and lists"`
		Version string          `json:"version,omitempty" doc:"A free-form version string, e.g. 1.0.0"`
		Spec    json.RawMessage `json:"spec,omitempty" doc:"The declarative spec body; validated against the catalogs, and a spec that fails validation refuses the write (422)"`
	}
}

type updateDriverInput struct {
	ID   string `path:"id"`
	Body struct {
		Label   *string         `json:"label,omitempty" doc:"A new operator-facing label"`
		Version *string         `json:"version,omitempty" doc:"A new version string, e.g. 1.0.1"`
		Spec    json.RawMessage `json:"spec,omitempty" doc:"A replacement spec body; validated like the create's, refused with a 422 when it cannot be interpreted"`
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
		Description: "Lists the driver registry, ordered alphabetically by label. Populates the driver picker on the product form. Gated by driver:read.",
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
			Name: in.Body.Name, Label: in.Body.Label, Version: in.Body.Version,
			Spec: []byte(in.Body.Spec),
		})
		if err != nil {
			return nil, mapDriverErr(err)
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
		Description: "Patches a custom driver's label or version. Official drivers are read-only (422). Gated by driver:update.",
	}, "driver", "update"), func(ctx context.Context, in *updateDriverInput) (*driverOutput, error) {
		d, err := gw.UpdateDriver(ctx, actorID(ctx), in.ID, storage.DriverPatch{
			Label: in.Body.Label, Version: in.Body.Version, Spec: []byte(in.Body.Spec),
		})
		if err != nil {
			return nil, mapDriverErr(err)
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

// mapDriverErr layers the spec write gate over the registry mapper: a spec
// that fails validation is a 422 carrying the fault by name, so an author
// fixes the spec rather than reading a generic refusal.
func mapDriverErr(err error) error {
	if errors.Is(err, storage.ErrSpecInvalid) {
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "storage: "))
	}
	return mapTypeErr(err, "driver")
}
