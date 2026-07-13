// Package tag is the pure core of the tag primitive: a tag is an operator
// key: value label whose key is a tenant-wide governed vocabulary and whose
// values bind per entity and resolve union-on-key, override-on-value down the
// cascade. This file owns the vocabulary rules with no I/O, so they are
// unit-testable in isolation: what makes a well-formed tag key, which entity
// kinds a key may apply to, and what makes a well-formed bound value. The
// storage layer owns the cascade and the owner arc; this package owns "is this
// key normalized and is this value well-formed".
package tag

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// keyPattern is the normalized-key rule: a lowercase identifier (a leading
// letter, then lowercase letters, digits, or underscores). Enforcing one
// canonical spelling is the whole point of a governed vocabulary: it stops
// `env` beside `environment` beside `Environment` from all being minted as
// distinct keys.
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// MaxKeyLen and MaxValueLen bound a key and a bound value. A key is a short
// identifier; a value is a free-text label, generous but not unbounded so a
// binding cannot smuggle a blob.
const (
	MaxKeyLen   = 64
	MaxValueLen = 256
)

// ValidateKey reports whether name is a well-formed, normalized tag key: a
// lowercase identifier within the length bound. A name with uppercase,
// whitespace, or punctuation is rejected so the vocabulary stays canonical.
func ValidateKey(name string) error {
	if name == "" {
		return fmt.Errorf("tag: key is empty")
	}
	if len(name) > MaxKeyLen {
		return fmt.Errorf("tag: key exceeds %d characters", MaxKeyLen)
	}
	if !keyPattern.MatchString(name) {
		return fmt.Errorf("tag: key %q must be a lowercase identifier (a leading letter, then lowercase letters, digits, or underscores)", name)
	}
	return nil
}

// ValidateValue reports whether v is a well-formed bound value: non-empty after
// trimming and within the length bound. Free text otherwise; whether a key may
// constrain or normalize its values is a deferred governance question, so the
// value side stays permissive here.
func ValidateValue(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("tag: value is empty")
	}
	if utf8.RuneCountInString(v) > MaxValueLen {
		return fmt.Errorf("tag: value exceeds %d characters", MaxValueLen)
	}
	return nil
}

// ValidateAllowedValues reports whether a key's declared value enum is
// well-formed: every entry a valid bound value (non-empty, within the length
// bound) and no duplicates. An empty set is legal and means the key is
// free-text; a non-empty set is the enum a bound value must belong to.
func ValidateAllowedValues(values []string) error {
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if err := ValidateValue(v); err != nil {
			return fmt.Errorf("tag: allowed value %q invalid: %w", v, err)
		}
		if seen[v] {
			return fmt.Errorf("tag: duplicate allowed value %q", v)
		}
		seen[v] = true
	}
	return nil
}

// ValueAllowed reports whether value satisfies a key's value domain: an empty
// allowed set (a free-text key) admits any value, otherwise the value must be a
// member of the declared enum.
func ValueAllowed(allowed []string, value string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if a == value {
			return true
		}
	}
	return false
}

// EntityKind is a kind of entity a tag can be bound to. These are the estate
// tiers on the exclusive arc that carry bindings; a key's applies_to narrows a
// key to a subset of them.
type EntityKind string

const (
	KindComponent EntityKind = "component"
	KindSystem    EntityKind = "system"
	KindLocation  EntityKind = "location"
)

// EntityKinds is the ordered set of bindable entity kinds, for validation and
// the create form's applies_to picker.
var EntityKinds = []EntityKind{KindComponent, KindSystem, KindLocation}

// ValidKind reports whether k is a known bindable entity kind.
func ValidKind(k EntityKind) bool {
	for _, e := range EntityKinds {
		if k == e {
			return true
		}
	}
	return false
}

// ValidateAppliesTo reports whether kinds is a legal applies_to set: every
// entry a known entity kind, with no duplicates. An empty set is legal and
// means the key is universal (it applies to every entity kind); a non-empty set
// narrows the key to exactly those kinds.
func ValidateAppliesTo(kinds []string) error {
	seen := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		if !ValidKind(EntityKind(k)) {
			return fmt.Errorf("tag: unknown entity kind %q in applies_to", k)
		}
		if seen[k] {
			return fmt.Errorf("tag: duplicate entity kind %q in applies_to", k)
		}
		seen[k] = true
	}
	return nil
}

// AppliesToKind reports whether a key with the given applies_to set may be bound
// to kind. An empty applies_to is universal (any kind); otherwise the kind must
// be listed. A kind that is not a known bindable kind is never applicable.
func AppliesToKind(appliesTo []string, kind EntityKind) bool {
	if !ValidKind(kind) {
		return false
	}
	if len(appliesTo) == 0 {
		return true
	}
	for _, k := range appliesTo {
		if EntityKind(k) == kind {
			return true
		}
	}
	return false
}
