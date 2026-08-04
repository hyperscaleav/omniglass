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
// This is the ENTITY name rule: one segment, no dot. The other rule, the keyspace
// one, is the same character set joined by dots (`icmp.rtt-avg`). They are two rules
// and ONE validator: storage.ValidateName picks between them from the table's
// declared identity shape (identity_shape.go), so a call site cannot pick the wrong
// rule or forget to pick at all.
//
// An earlier version of this comment said the two rules carried different character
// sets and that a keyspace key legitimately carried an underscore. Neither was true:
// the character sets were unified, and no rule here has ever admitted an underscore.
//
// The word **segment** means one dot-separated component of a name. A segment is a
// position in a path; a name is the value at one.

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
