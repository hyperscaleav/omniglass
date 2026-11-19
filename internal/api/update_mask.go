package api

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/updatemask"
)

// resolveUpdateMask turns a PATCH body's update_mask into the write set the
// storage layer stores (ADR-0091). A mask naming a field the route does not
// patch is a 422 naming it, never a silent no-op: the caller stated an intent
// the resource has no field for. The implied mask is read off the patch the
// write is about to store, so "which fields did the caller send" is defined once
// for both layers.
func resolveUpdateMask(mask, patchable []string, populated updatemask.Fields) (updatemask.Fields, error) {
	write, err := updatemask.Resolve(mask, patchable, populated)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return write, nil
}
