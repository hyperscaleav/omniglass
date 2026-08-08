package api

import (
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// mapRefErr translates the sentinels shared by every bare-name resolution path
// (scopedByName/scopedByNameInScope/resolveScopedRef) into HTTP status. A
// caller runs it FIRST and falls through to its own entity-specific mapping
// when ok is false; every map*Err that resolves a component, system, or
// location reference needs this, not only the three tree-entity mappers,
// because a name Task 10's read paths resolve once at the top of a handler
// can now be ambiguous too (#627 scopes name uniqueness to placement, so a
// bare-name reference that used to be unique by construction can match more
// than one row).
//
// Candidates is empty for a path that resolved with no caller scope to filter
// by at all (the three *NameTaken advisories, storage.withoutCandidates): the
// reference is still refused as ambiguous, the message just names no uuid,
// since nothing on that path could tell which ones the caller may read.
//
// Today this only recognizes storage.ErrAmbiguousName. A later slice's path
// resolver adds storage.ErrPathNotFound's non-disclosing 404 here too, once
// that sentinel exists; this function is the one place both belong; nothing
// downstream refuses ambiguity on its own.
func mapRefErr(err error) (error, bool) {
	var ambig *storage.ErrAmbiguousName
	if errors.As(err, &ambig) {
		if len(ambig.Candidates) == 0 {
			return huma.Error409Conflict(fmt.Sprintf(
				"%q is ambiguous for %s: matches more than one row. Address it by uuid instead of by name.",
				ambig.Ref, ambig.Kind)), true
		}
		return huma.Error409Conflict(fmt.Sprintf(
			"%q is ambiguous for %s: matches %s. Address it by uuid instead of by name.",
			ambig.Ref, ambig.Kind, strings.Join(ambig.Candidates, ", "))), true
	}
	return nil, false
}

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
	case errors.Is(err, storage.ErrEntityNameIsUUID):
		return huma.Error422UnprocessableEntity(kind + " name may not be a uuid")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity(kind + " name must be lowercase letters, digits, and hyphens, starting with a letter or digit")
	default:
		return huma.Error500InternalServerError("type operation failed")
	}
}
