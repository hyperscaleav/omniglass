package storage

import (
	"errors"
	"regexp"
)

// An entity's key is the human-readable machine identifier an operator types and an
// address carries: the `rm215a` in `boi.17c.rm215a`. It is not the uuid, which is
// identity and survives a rename, and it is not the name, which is the free-text
// label a human reads.
//
// This is the ENTITY key rule, and it is not the only key rule. `internal/key` owns
// the canonical keyspace that `property_type`, `event_type`, and `command_type`
// share (`icmp.rtt_avg`, `interface.reachable`): lowercase snake_case, optionally
// dot-hierarchied. The two must not be merged by accident, because a keyspace key
// legitimately carries a dot and an underscore while an entity key legitimately
// carries a hyphen.
//
// `internal/key` also fixes the word **segment**: one dot-separated component of a
// key. That is its only meaning here too. A segment is a position in a path; a key
// is the value at one.

// ErrInvalidEntityKey is returned when a proposed key does not match the entity-key
// rule. The API maps it to 422.
var ErrInvalidEntityKey = errors.New("storage: invalid entity key")

// ErrEntityKeyIsUUID is the narrower refusal for a key shaped exactly like a uuid.
// It is separate from ErrInvalidEntityKey because the two need different words: a
// uuid satisfies the entity-key rule completely, so telling an operator to use
// lowercase letters, digits, and hyphens describes what they already did.
var ErrEntityKeyIsUUID = errors.New("storage: entity key may not be a uuid")

// entityKeyRe is the entity-key rule: lowercase letters and digits and hyphens,
// starting with a letter or digit. Shared by create and rename so both surfaces
// agree; mirrored client-side for the inline check.
//
// The exclusions carry weight beyond tidiness. It rejects `$`, which is what lets
// the address grammar use `$comp` / `$sys` / `$role` as accessors without reserving
// any word: a location may still legitimately take the key `sys`, because
// `boi.17c.sys.$sys.av` cannot be misread. It rejects `.`, which would split one key
// into two segments. And it rejects `*` and `>`, which are NATS wildcards, so a key
// can never be mistaken for a subject pattern.
var entityKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// uuidRe is the exact canonical uuid shape, which the entity-key rule above does NOT
// exclude: a uuid is lowercase hex and hyphens, so it satisfies that rule perfectly.
// The check is deliberately narrow, matching the full 8-4-4-4-12 form and nothing
// else, so ordinary hyphenated keys that merely look hex-ish ("019f8754", "ab-cd-ef")
// keep working.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// isUUID reports whether a reference is the canonical uuid form. It is the whole of
// the dual-accept disambiguation: a key cannot take this shape, so a match here
// means the caller gave an id.
func isUUID(ref string) bool { return uuidRe.MatchString(ref) }

// ValidateEntityKey enforces the entity-key rule and a 100-char ceiling. It is the
// server-side source of truth for every key-bearing entity table.
//
// A key may not BE a uuid. A reference in a path or a join field accepts either form
// and resolves the uuid first, so a key that is also a uuid would make
// `/components/019f8754-...` mean two different things depending on which entity
// happened to exist. Forbidding the shape is what keeps that resolution a property of
// the request rather than of the data.
func ValidateEntityKey(key string) error {
	if len(key) > 100 || !entityKeyRe.MatchString(key) {
		return ErrInvalidEntityKey
	}
	if uuidRe.MatchString(key) {
		return ErrEntityKeyIsUUID
	}
	return nil
}
