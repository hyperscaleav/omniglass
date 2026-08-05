package storage

import (
	"errors"
	"fmt"
)

// The name rule. There is one, and it applies to every name-bearing table.
//
// There were three, then two, now one. A keyspace name used to be a dot-joined path
// of segments (icmp.rtt-avg) while an entity name was a single segment, selected by
// the table's declared shape. That distinction is retired (#586): a name is one kebab
// token everywhere, so `icmp.rtt-avg` is `icmp-rtt-avg` and there is no path to
// validate. One rule, one character set, one ceiling.
//
// What survives from the two-rule design is the thing worth keeping: the table must be
// DECLARED to bear a name at all. Before that, a caller picked between validators by
// hand at thirteen sites and nothing checked the choice matched the table, which is how
// system_role reached production unvalidated. A table nobody classified still cannot
// validate, and that is deliberate.

// MaxEntityNameLen bounds every name. The keyspace ceiling of 128 existed because
// that name was a path; with no paths there is one number.
const MaxEntityNameLen = 100

// ErrTableNotNameBearing is returned when a table has no declared name rule: either
// it is not in the declaration at all, or it is declared as bearing no name (id-only,
// or a human identifier on its own rule).
//
// It is deliberately an error rather than a nil, because "this table has no name" is
// not the same answer as "this name is fine", and a caller that cannot tell them
// apart is the bug this package exists to prevent.
var ErrTableNotNameBearing = errors.New("storage: table bears no operator-typed name")

// ValidateName reports whether name is legal for table, choosing the rule from the
// table's declared identity shape.
func ValidateName(table, name string) error {
	id, declared := IdentityShapes[table]
	if !declared {
		return fmt.Errorf("%w: %q is not in the identity declaration", ErrTableNotNameBearing, table)
	}
	switch id.Shape {
	case ShapeKeyBearing, ShapeKeyspace:
		return validateEntityName(name)
	default:
		return fmt.Errorf("%w: %q is declared %q", ErrTableNotNameBearing, table, id.Shape)
	}
}

// validateEntityName is the rule: one kebab token, no dots, so a name is exactly one
// position wherever it is carried and can never split into two.
func validateEntityName(name string) error {
	if len(name) > MaxEntityNameLen {
		return fmt.Errorf("%w: %q exceeds %d characters", ErrInvalidEntityName, name, MaxEntityNameLen)
	}
	if !entityNameRe.MatchString(name) {
		return fmt.Errorf("%w: %q must be lowercase letters, digits, and hyphens", ErrInvalidEntityName, name)
	}
	if isUUID(name) {
		return fmt.Errorf("%w: %q", ErrEntityNameIsUUID, name)
	}
	return nil
}
