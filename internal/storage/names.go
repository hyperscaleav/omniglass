package storage

import (
	"errors"
	"regexp"
)

// ErrInvalidName is returned when a proposed entity name (the technical name /
// URL slug) does not match the slug rule. The API maps it to 422.
var ErrInvalidName = errors.New("storage: invalid entity name")

// ErrNameIsUUID is the narrower refusal for a name that is shaped exactly like a
// uuid. It is separate from ErrInvalidName because the two need different words:
// a uuid satisfies the slug rule completely, so telling an operator to use
// lowercase letters, digits, and hyphens describes what they already did.
var ErrNameIsUUID = errors.New("storage: entity name may not be a uuid")

// entityNameRe is the slug rule for technical names: lowercase letters and
// digits and hyphens, starting with a letter or digit. Shared by create and
// rename so both surfaces agree; mirrored client-side for the inline check.
var entityNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// uuidRe is the exact canonical uuid shape, which the slug rule above does NOT
// exclude: a uuid is lowercase hex and hyphens, so it satisfies the slug rule
// perfectly. The check is deliberately narrow, matching the full 8-4-4-4-12 form
// and nothing else, so ordinary hyphenated names that merely look hex-ish
// ("019f8754", "ab-cd-ef") keep working.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// isUUID reports whether a reference is the canonical uuid form. It is the whole
// of the dual-accept disambiguation: a name cannot take this shape, so a match
// here means the caller gave an id.
func isUUID(ref string) bool { return uuidRe.MatchString(ref) }

// ValidateEntityName enforces the slug rule and a 100-char ceiling. It is the
// server-side source of truth for a component/system/location technical name.
//
// A name may not BE a uuid. A reference in a path or a join field accepts either
// form and resolves the uuid first, so a name that is also a uuid would make
// `/components/019f8754-...` mean two different things depending on which entity
// happened to exist. Forbidding the shape is what keeps that resolution a
// property of the request rather than of the data.
func ValidateEntityName(name string) error {
	if len(name) > 100 || !entityNameRe.MatchString(name) {
		return ErrInvalidName
	}
	if uuidRe.MatchString(name) {
		return ErrNameIsUUID
	}
	return nil
}
