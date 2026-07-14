package storage

import (
	"errors"
	"regexp"
)

// ErrInvalidName is returned when a proposed entity name (the technical name /
// URL slug) does not match the slug rule. The API maps it to 422.
var ErrInvalidName = errors.New("storage: invalid entity name")

// entityNameRe is the slug rule for technical names: lowercase letters and
// digits and hyphens, starting with a letter or digit. Shared by create and
// rename so both surfaces agree; mirrored client-side for the inline check.
var entityNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateEntityName enforces the slug rule and a 100-char ceiling. It is the
// server-side source of truth for a component/system/location technical name.
func ValidateEntityName(name string) error {
	if len(name) > 100 || !entityNameRe.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}
