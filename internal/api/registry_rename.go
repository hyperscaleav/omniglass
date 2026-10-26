package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// renameRegistryInput is the :rename body every registry #555 opened shares. The
// name rule lives here, in the contract, as it does on the fleet tiers' own
// :rename bodies; a uuid-shaped name passes the pattern and is refused by the
// gateway's name rule instead (422 either way).
type renameRegistryInput struct {
	ID   string `path:"id" doc:"The row, by its current name or its uuid"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The new name, unique in the registry (lowercase letters, digits, hyphens; never a uuid)"`
	}
}

// registryRename describes one registry's :rename route. resource is both the
// permission resource and the storage table (they are one word for every
// registry the rename covers); noun is how the prose names it.
type registryRename[O any] struct {
	resource   string
	collection string
	noun       string
	// refusals adds the registry's own refusals to the generated description,
	// after the ones every registry shares.
	refusals string
	// read answers the renamed row, by the uuid the rename returned, in the
	// registry's own response shape, so a rename answers exactly what a GET does.
	read func(ctx context.Context, gw storage.Gateway, id string) (*O, error)
	// mapErr is the registry's own error mapper, for the refusals the rename
	// does not word itself.
	mapErr func(error) error
}

// registerRegistryRename wires POST /<collection>/{id}:rename for one registry
// (#555, ADR-0555): the custom method (ADR-0076) the fleet tiers already use for
// a rename, here over storage.RenameRegistry, the one primitive every registry
// shares. Gated by <resource>:rename, its own permission rather than update,
// because a rename breaks what lives outside the platform (a script, a bookmark,
// an integration's config typed with the old name) even though nothing inside it
// moves: every reference holds the uuid.
func registerRegistryRename[O any](api huma.API, a *authenticator, gw storage.Gateway, r registryRename[O]) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "rename-" + strings.ReplaceAll(r.resource, "_", "-"),
		Method:      http.MethodPost,
		Path:        "/" + r.collection + "/{id}:rename",
		Summary:     "Rename a " + r.noun,
		Description: fmt.Sprintf("Moves a custom %[1]s's name, the handle an operator types, addressed by its current name or its uuid. "+
			"The uuid does not change, so every reference inside the platform still resolves and now reads the new name; "+
			"a script or a bookmark that typed the old one stops resolving, which is why the rename is its own act and its own permission rather than a patch field. "+
			"A label rendered from the name is not restamped (the recompute verb applies that). "+
			"An official %[1]s keeps its shipped name (422), since the boot seed owns it by name; a taken name is a 409; "+
			"an illegal or uuid-shaped name is a 422.%[3]s Audited as rename. Gated by %[2]s:rename.", r.noun, r.resource, r.refusals),
	}, r.resource, "rename"), func(ctx context.Context, in *renameRegistryInput) (*O, error) {
		id, err := gw.RenameRegistry(ctx, actorID(ctx), r.resource, in.ID, in.Body.Name)
		switch {
		case errors.Is(err, storage.ErrTypeExists):
			return nil, huma.Error409Conflict(fmt.Sprintf("a %s named %q already exists", r.noun, in.Body.Name))
		case errors.Is(err, storage.ErrTypeOfficial):
			return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("an official %s keeps its shipped name", r.noun))
		case errors.Is(err, storage.ErrRegistrySeededExample):
			return nil, huma.Error409Conflict(fmt.Sprintf("this %[1]s is a shipped example, which the boot seed plants by name whenever it is absent, "+
				"so renaming it would bring the shipped one back on the next boot beside yours; create a %[1]s of your own under the new name instead", r.noun))
		case err != nil:
			return nil, r.mapErr(err)
		}
		out, err := r.read(ctx, gw, id)
		if err != nil {
			return nil, r.mapErr(err)
		}
		return out, nil
	})
}

// registerRegistryRenames wires the five registries the rename covers. The
// catalogs keyed by telemetry name and the remaining type registries wait on
// #1188 (storage.renameableRegistries says why).
func registerRegistryRenames(api huma.API, a *authenticator, gw storage.Gateway) {
	registerRegistryRename(api, a, gw, registryRename[vendorOutput]{
		resource: "vendor", collection: "vendors", noun: "vendor",
		read: func(ctx context.Context, gw storage.Gateway, id string) (*vendorOutput, error) {
			m, err := gw.GetVendor(ctx, id)
			if err != nil {
				return nil, err
			}
			return &vendorOutput{Body: toVendorBody(m)}, nil
		},
		mapErr: func(err error) error { return mapTypeErr(err, "vendor") },
	})
	registerRegistryRename(api, a, gw, registryRename[driverOutput]{
		resource: "driver", collection: "drivers", noun: "driver",
		read: func(ctx context.Context, gw storage.Gateway, id string) (*driverOutput, error) {
			d, err := gw.GetDriver(ctx, id)
			if err != nil {
				return nil, err
			}
			return &driverOutput{Body: toDriverBody(d)}, nil
		},
		mapErr: mapDriverErr,
	})
	registerRegistryRename(api, a, gw, registryRename[productOutput]{
		resource: "product", collection: "products", noun: "product",
		read: func(ctx context.Context, gw storage.Gateway, id string) (*productOutput, error) {
			m, err := gw.GetProduct(ctx, id)
			if err != nil {
				return nil, err
			}
			return &productOutput{Body: toProductBody(m)}, nil
		},
		mapErr: mapProductErr,
	})
	registerRegistryRename(api, a, gw, registryRename[standardOutput]{
		resource: "standard", collection: "standards", noun: "standard",
		refusals: " A shipped example standard keeps its name too (409): the boot seed plants it by name whenever it is absent, so a renamed one would come back on the next boot; forking a template into a standard of your own is #827.",
		read: func(ctx context.Context, gw storage.Gateway, id string) (*standardOutput, error) {
			st, err := gw.GetStandard(ctx, id)
			if err != nil {
				return nil, err
			}
			return &standardOutput{Body: toStandardBody(st)}, nil
		},
		mapErr: mapStandardErr,
	})
	registerRegistryRename(api, a, gw, registryRename[locationTypeOutput]{
		resource: "location_type", collection: "location-types", noun: "location type",
		read: func(ctx context.Context, gw storage.Gateway, id string) (*locationTypeOutput, error) {
			lt, err := gw.GetLocationType(ctx, id)
			if err != nil {
				return nil, err
			}
			return &locationTypeOutput{Body: toLocationTypeBody(lt)}, nil
		},
		mapErr: func(err error) error { return mapTypeErr(err, "location_type") },
	})
}
