package storage

import (
	"errors"
	"regexp"
)

// A segment is the one token an entity contributes to every address that passes
// through it: the `rm215a` in `boi.17c.rm215a`. It is not the whole address, which
// is assembled from the segments of an entity and its ancestors, and it is not the
// operator-facing label, which is free text.

// ErrInvalidSegment is returned when a proposed segment does not match the segment
// rule. The API maps it to 422.
var ErrInvalidSegment = errors.New("storage: invalid entity segment")

// ErrSegmentIsUUID is the narrower refusal for a segment shaped exactly like a
// uuid. It is separate from ErrInvalidSegment because the two need different words:
// a uuid satisfies the segment rule completely, so telling an operator to use
// lowercase letters, digits, and hyphens describes what they already did.
var ErrSegmentIsUUID = errors.New("storage: entity segment may not be a uuid")

// segmentRe is the segment rule: lowercase letters and digits and hyphens,
// starting with a letter or digit. Shared by create and rename so both surfaces
// agree; mirrored client-side for the inline check.
//
// The exclusions carry weight beyond tidiness. It rejects `$`, which is what lets
// the address grammar use `$comp` / `$sys` / `$role` as accessors without reserving
// any word: a location may still legitimately take the segment `sys`, because
// `boi.17c.sys.$sys.av` cannot be misread. It rejects `.`, which would split one
// segment into two path tokens. And it rejects `*` and `>`, which are NATS
// wildcards, so a segment can never be mistaken for a subject pattern.
var segmentRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// uuidRe is the exact canonical uuid shape, which the segment rule above does NOT
// exclude: a uuid is lowercase hex and hyphens, so it satisfies the segment rule
// perfectly. The check is deliberately narrow, matching the full 8-4-4-4-12 form
// and nothing else, so ordinary hyphenated segments that merely look hex-ish
// ("019f8754", "ab-cd-ef") keep working.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// isUUID reports whether a reference is the canonical uuid form. It is the whole
// of the dual-accept disambiguation: a segment cannot take this shape, so a match
// here means the caller gave an id.
func isUUID(ref string) bool { return uuidRe.MatchString(ref) }

// ValidateSegment enforces the segment rule and a 100-char ceiling. It is the
// server-side source of truth for every segment-bearing table.
//
// A segment may not BE a uuid. A reference in a path or a join field accepts either
// form and resolves the uuid first, so a segment that is also a uuid would make
// `/components/019f8754-...` mean two different things depending on which entity
// happened to exist. Forbidding the shape is what keeps that resolution a
// property of the request rather than of the data.
func ValidateSegment(segment string) error {
	if len(segment) > 100 || !segmentRe.MatchString(segment) {
		return ErrInvalidSegment
	}
	if uuidRe.MatchString(segment) {
		return ErrSegmentIsUUID
	}
	return nil
}
