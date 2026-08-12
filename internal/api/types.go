package api

import (
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/updatemask"
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
// since nothing on that path could tell which ones the caller may read. A path
// that resolved WITHIN the caller's own read scope lists them (#697), because
// every candidate is then a row that caller may read, and each is a spelling of
// the same reference that resolves: this message is the only place an operator
// is told which rows collided.
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

// The two refusals a create FORM has to act on, and the reason both carry a
// machine-readable location rather than only a sentence (#702).
//
// A create form is the one caller that cannot treat a refusal as "show the
// message and stop". Two of them have a specific recovery it must run, and both
// are otherwise indistinguishable from refusals that mean something else:
//
//   - the platform will not NAME this row (no stem on the component_type chain,
//     an unclassified system, a system_type chain with no stem, a location_type
//     with no name rule). The form unlocks the name field and asks the operator
//     to type one. Every other 422 on the same route means the body is wrong,
//     and unlocking on those would blank a field for no reason.
//   - the drafted NAME moved between the preview and the submit, because the
//     ordinal was taken or the type that mints it was edited. The form re-reads
//     the draft and shows the new name. Every other 409 on a create is a name
//     collision the operator has to resolve themselves.
//
// Matching on the sentence would tie the console to server copy, so each is
// stamped with a huma.ErrorDetail whose Location names the request field the
// recovery acts on: body.name for the first, body.expected_name for the second,
// which also carries the name the create would have produced as its Value. That
// is the RFC 9457 errors array the ErrorModel already publishes, so it costs no
// new wire shape and shows up in the generated client for free.

// errNoGeneratedName is the 422 for "the platform will not name this row",
// located on body.name because supplying one is the fix and the field is where
// the form acts.
func errNoGeneratedName(msg string) error {
	return huma.Error422UnprocessableEntity(msg, &huma.ErrorDetail{Message: msg, Location: "body.name"})
}

// mapDraftedNameErr translates the create form's name-precondition sentinels
// (#702). Called first by every create mapper that can generate a name, on the
// same fall-through shape as mapRefErr, because the precondition is one wire
// contract shared by three tiers rather than three similar ones.
//
// The conflict is a 409 and not a 422: the request is well formed and was legal
// when it was composed, and the estate simply moved underneath it, which is the
// definition of a conflict. It names both the name the form was shown and the
// one this create would produce, because "try again" is not the recovery, "here
// is what you would get instead" is. The message deliberately does not assert
// WHY the two differ: another create taking the number and an edit to the type's
// stem produce the same fact and the same recovery, and a message that named
// only the first would be wrong half the time.
func mapDraftedNameErr(err error) (error, bool) {
	var moved *storage.DraftedNameMovedError
	if errors.As(err, &moved) {
		msg := fmt.Sprintf(
			"this create would name the row %q, not %q as the form was shown, because the number was taken or the type that names it changed while the form was open. Nothing was created.",
			moved.Name, moved.Expected)
		return huma.Error409Conflict(msg, &huma.ErrorDetail{
			Message:  msg,
			Location: "body.expected_name",
			Value:    moved.Name,
		}), true
	}
	if errors.Is(err, storage.ErrNameExpectedOnTypedName) {
		return huma.Error422UnprocessableEntity(
			"expected_name applies only to a name the platform generates: omit the name to have one generated, or drop expected_name",
			&huma.ErrorDetail{Location: "body.expected_name", Message: "sent beside a name"}), true
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
	// A mask this resource cannot honor is a request fault that NAMES the
	// field, never a silent no-op: the caller stated an intent the resource has
	// no field for, and swallowing it would leave them believing a write landed
	// (ADR-0091). Here rather than in one registry's own mapper because the
	// mask is a wire primitive every registry adopting it reaches the same way.
	var maskErr *updatemask.Error
	if errors.As(err, &maskErr) {
		return huma.Error422UnprocessableEntity(maskErr.Error())
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
