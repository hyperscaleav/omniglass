package storage

import (
	"errors"
	"fmt"
	"strings"
)

// The name rule, selected by the table rather than by the caller.
//
// There are two rules and there were three. An entity name is one segment
// (hq-boardroom-dsp); a keyspace name is a dot-joined path of them
// (icmp.rtt-avg). They share a character set and differ only in whether a dot is
// legal and in how long a name may be, which is the whole of the distinction that
// survived one-character-set-everywhere. A third pattern validated principal_group
// at the API layer and admitted `.` and `_`; it is gone, folded into the entity rule.
//
// The selection is the point. Before this, a caller picked ValidateEntityKey or
// key.ValidateKey by hand at thirteen sites, and nothing checked that the choice
// matched what the table was. Both wrong choices are silent, and not choosing at all
// is silent too, which is how system_role reached production unvalidated. Here the
// table's declared shape (identity_shape.go) selects, so a table nobody classified
// cannot validate at all.

// MaxEntityNameLen bounds a single-segment entity name.
const MaxEntityNameLen = 100

// MaxKeyspaceNameLen bounds a dot-joined keyspace name. It is larger because the
// name is a path and the entity name is a path of one.
const MaxKeyspaceNameLen = 128

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
	case ShapeKeyBearing:
		return validateEntityName(name)
	case ShapeKeyspace:
		return validateKeyspaceName(name)
	default:
		return fmt.Errorf("%w: %q is declared %q", ErrTableNotNameBearing, table, id.Shape)
	}
}

// validateEntityName is the single-segment rule: no dots, so a name is exactly one
// position in a topic and can never split into two.
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

// validateKeyspaceName is the dotted rule: every segment obeys the entity character
// set, joined by dots. An empty segment (a leading, trailing, or doubled dot) fails
// on the segment rule rather than needing a case of its own.
func validateKeyspaceName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidEntityName)
	}
	if len(name) > MaxKeyspaceNameLen {
		return fmt.Errorf("%w: %q exceeds %d characters", ErrInvalidEntityName, name, MaxKeyspaceNameLen)
	}
	for _, seg := range strings.Split(name, ".") {
		if !entityNameRe.MatchString(seg) {
			return fmt.Errorf("%w: %q must be lowercase dot-separated segments of letters, digits, and hyphens",
				ErrInvalidEntityName, name)
		}
	}
	// A keyspace name may not be a uuid either. key.ValidateKey never checked this,
	// so a uuid-shaped property key was legal; the reason to forbid it is the same as
	// for an entity name, since both are resolved against a uuid first.
	if isUUID(name) {
		return fmt.Errorf("%w: %q", ErrEntityNameIsUUID, name)
	}
	return nil
}
