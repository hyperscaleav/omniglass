package storage

import (
	"errors"
	"regexp"
)

// The identity triad, in the order an operator meets it:
//
//	id            a uuid. immutable, the primary key and every foreign key target.
//	name          the human-readable machine identifier an operator types and an
//	              address carries: the `rm215a` in `boi.17c.rm215a`. renameable.
//	display_name  an optional friendly string a human reads ("HQ Boardroom DSP").
//
// A rename moves the name and never the id, which is why every reference stores the
// id and why an audit row keys on it.
//
// The name rule itself, what it applies to, and why there is exactly one of it now
// (not two) live in name_rule.go, not here: this file owns the rule's regular
// expression and the uuid disambiguation, not its rationale restated a second time.
//
// The word **segment** means one dot-separated component of a #627 dotted ADDRESS
// (`boi.17c.$comp.display-1`), never of a name: #586 backfilled the last dotted name
// (a keyspace catalog key) dot-free before this branch existed, and no name has held
// one since (name_rule.go). An address is a reference that concatenates several
// individually-valid single-segment names; it is resolved, never stored. Why
// the rule below is an allowlist rather than a denylist, and why that is what lets
// one grammar render as a CLI argument, a REST path, or a NATS subject with no
// escaping (a DNS label and an email localpart too, for a segment that also fits
// the 63-octet ceiling and does not end in the hyphen this rule alone permits), is
// recorded in
// [ADR-0089](https://docs.omniglass.hyperscaleav.com/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup),
// not restated here.

// ErrInvalidEntityName is returned when a proposed name does not match the entity
// name rule. The API maps it to 422.
var ErrInvalidEntityName = errors.New("storage: invalid entity name")

// ErrEntityNameIsUUID is the narrower refusal for a name shaped exactly like a uuid.
// It is separate from ErrInvalidEntityName because the two need different words: a
// uuid satisfies the entity name rule completely, so telling an operator to use
// lowercase letters, digits, and hyphens describes what they already did.
var ErrEntityNameIsUUID = errors.New("storage: entity name may not be a uuid")

// entityNameRe is the entity name rule: lowercase letters and digits and hyphens,
// starting with a letter or digit. Shared by create and rename so both surfaces
// agree; mirrored client-side for the inline check. It excludes `$`, `.`, `*`, `>`,
// `/`, `#`, `%`, and `:`, none of it incidental (ADR-0089): `$` is what lets the
// address grammar use `$comp` / `$sys` / `$role` as accessors with no word reserved
// (a location may still legitimately take the name `sys`, since `boi.17c.sys.$sys.av`
// cannot be misread), `.` is what keeps a name to exactly one address segment, and
// the rest are wildcards or separators in NATS, MQTT, URL escaping, and the router's
// own `:verb` convention.
var entityNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// uuidRe is the exact canonical uuid shape, which the entity name rule above does NOT
// exclude: a uuid is lowercase hex and hyphens, so it satisfies that rule perfectly.
// The check is deliberately narrow, matching the full 8-4-4-4-12 form and nothing
// else, so ordinary hyphenated names that merely look hex-ish ("019f8754", "ab-cd-ef")
// keep working.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// isUUID reports whether a reference is the canonical uuid form. It is the whole of
// the dual-accept disambiguation: a name cannot take this shape, so a match here
// means the caller gave an id.
func isUUID(ref string) bool { return uuidRe.MatchString(ref) }
