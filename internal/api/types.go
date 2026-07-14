package api

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// mapTypeErr translates the shared type-registry storage sentinels into HTTP
// status. kind is the wire label used in the message (e.g. "location_type").
// Shared by the location/system/component type routes.
func mapTypeErr(err error, kind string) error {
	switch {
	case errors.Is(err, storage.ErrTypeNotFound):
		return huma.Error404NotFound(kind + " not found")
	case errors.Is(err, storage.ErrTypeExists):
		return huma.Error409Conflict(kind + " id already exists")
	case errors.Is(err, storage.ErrTypeOfficial):
		return huma.Error422UnprocessableEntity("official " + kind + " is read-only")
	case errors.Is(err, storage.ErrTypeInUse):
		return huma.Error409Conflict(kind + " is referenced by existing rows")
	case errors.Is(err, storage.ErrReservedTypeID):
		return huma.Error422UnprocessableEntity("\"root\" is a reserved " + kind + " id")
	default:
		return huma.Error500InternalServerError("type operation failed")
	}
}
