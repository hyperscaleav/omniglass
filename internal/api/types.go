package api

import (
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// mapRefErr translates the sentinels shared by every bare-name and dotted-
// address resolution path (scopedByName/scopedByNameInScope/resolveScopedRef,
// and #627 Task 12's resolvePath underneath all three) into HTTP status. A
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
// storage.ErrPathNotFound is a dotted address that failed to resolve
// structurally (a missing segment, or a plane mismatch): mapped to the SAME
// wording as an ambiguous-but-out-of-scope candidate list would use, a plain
// non-disclosing "not found", because which of "absent" and "wrong kind" it
// was is not a distinction worth leaking, mirroring why an absent row and an
// out-of-scope row already share cfg.notFound. storage.ErrInvalidAddress is a
// syntax failure caught before any query ran (an empty segment, an
// unrecognized accessor, a segment failing the entity name rule): a 422, the
// caller's Msg verbatim. storage.ErrAddressNotAccepted is a dotted or
// accessor-bearing ref sent to a registry whose names stay a single global
// token (node, tag, the four lane types): also a 422, naming the kind.
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
	var notFound *storage.ErrPathNotFound
	if errors.As(err, &notFound) {
		return huma.Error404NotFound(notFound.Kind + " not found"), true
	}
	var invalid *storage.ErrInvalidAddress
	if errors.As(err, &invalid) {
		return huma.Error422UnprocessableEntity(invalid.Msg), true
	}
	var notAccepted *storage.ErrAddressNotAccepted
	if errors.As(err, &notAccepted) {
		return huma.Error422UnprocessableEntity(fmt.Sprintf(
			"a %s name is a single token, not an address (got %q)", notAccepted.Kind, notAccepted.Ref)), true
	}
	return nil, false
}

// mapTypeErr translates the shared type-registry storage sentinels into HTTP
// status. kind is the wire label used in the message (e.g. "location_type").
// Shared by the location/system/component type routes.
func mapTypeErr(err error, kind string) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrTypeNotFound):
		return huma.Error404NotFound(kind + " not found")
	case errors.Is(err, storage.ErrTypeExists):
		return huma.Error409Conflict(kind + " id already exists")
	case errors.Is(err, storage.ErrTypeOfficial):
		return huma.Error422UnprocessableEntity("official " + kind + " is read-only")
	case errors.Is(err, storage.ErrTypeInUse):
		return huma.Error409Conflict(kind + " is referenced by existing rows")
	case errors.Is(err, storage.ErrTypeNotForked):
		return huma.Error409Conflict(kind + " has no changes of yours to discard")
	case errors.Is(err, storage.ErrReservedTypeID):
		return huma.Error422UnprocessableEntity("\"root\" is a reserved " + kind + " id")
	// Before the two name cases below, deliberately: a name rule that cannot
	// mint a legal name wraps the very error the mint failed with, so a later
	// arm would match it first and report the TYPE's own name as the problem
	// when the type's name is fine (#687).
	case errors.Is(err, storage.ErrInvalidNameRule):
		return huma.Error422UnprocessableEntity("name rule cannot generate a legal name: " + err.Error())
	case errors.Is(err, storage.ErrEntityNameIsUUID):
		return huma.Error422UnprocessableEntity(kind + " name may not be a uuid")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity(kind + " name must be lowercase letters, digits, and hyphens, starting with a letter or digit")
	// A label rule that does not compile is refused HERE, at the edit, rather
	// than stored and discovered later by a write path rendering one entity
	// (#682). The template engine's own message names the offending construct
	// and its position, which is the only thing that makes a template error
	// actionable, so it is passed through rather than replaced.
	case errors.Is(err, storage.ErrInvalidLabelRule):
		return huma.Error422UnprocessableEntity("label rule does not parse: " + err.Error())
	default:
		return huma.Error500InternalServerError("type operation failed")
	}
}
