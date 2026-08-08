package storage

import (
	"regexp"
	"strings"
)

// Renders is the two display-only compact forms of a tree entity's dotted
// address (#627 Task 15), computed and attached alongside Path and
// PathSegments (see PathOf in path.go) on every component, system, and
// location a GET or LIST returns. Never accepted back by the resolver: see
// RenderDash and RenderBare below for why.
type Renders struct {
	Dash string
	Bare string
}

// The two display-only compact forms of a dotted address (#627 Task 15):
// RenderDash and RenderBare, both pure functions over an already-resolved
// segment slice (PathOf's own accessor-inclusive output, the same shape
// ParseAddress produces from a typed ref). Neither round-trips through the
// resolver: stripping the accessor is lossy (RenderDash), and compacting the
// final segment to an abbreviation is lossier still (RenderBare). Both exist
// for reading and labelling only (a cable tag, a physical asset label, a
// compact row sub-line); `path` and `path_segments` beside them on the wire
// are the only forms ParseAddress ever accepts back.
//
// Identity itself never runs through either of these: a component's address
// derives from its own placement (its root ancestor's location, walked by
// PathOf below), never from a system it happens to belong to.

// trailingOrdinalRe matches a segment's own generated-name tail: the
// "-<digits>" suffix generateComponentName always mints (namegen.go's
// ordinalSuffixRe, applied here to a whole path segment rather than a
// stripped stem remainder, since RenderBare does not know the segment's stem
// on its own, only its abbreviation).
var trailingOrdinalRe = regexp.MustCompile(`-([0-9]+)$`)

// nonAccessorSegments drops every accessor ($comp/$sys/$role) from segments,
// keeping the rest in order: the one filter both renders below share.
func nonAccessorSegments(segments []string) []string {
	kept := make([]string, 0, len(segments))
	for _, s := range segments {
		if !isAccessor(s) {
			kept = append(kept, s)
		}
	}
	return kept
}

// RenderDash is the dash render: segments' non-accessor members joined with
// "-". Dash-joining the RAW segments of `boi.17c.216b.$comp.display-1` would
// carry the accessor straight through ("boi-17c-216b-$comp-display-1"); this
// is the corrected definition, stripping it first
// ("boi-17c-216b-display-1").
func RenderDash(segments []string) string {
	return strings.Join(nonAccessorSegments(segments), "-")
}

// RenderBare is the compact render: RenderDash's segments concatenated with
// NO separator (the point of a "bare" form: a physical label has no room for
// punctuation), with the final segment's own stem-ordinal tail
// ("display-1") replaced by "<abbrev><ordinal>" ("dsp1") when abbrev is
// non-empty and that final segment actually carries a trailing "-<n>" (every
// platform-generated name does; an operator-typed rename, which clears
// name_generated for good, may not). abbrev is the owning row's own type
// registry's abbreviation (component_type.abbrev, resolved through the same
// inherited-from-parent chain generateNameForProduct walks); system and
// location addresses have no type-level abbreviation today, so their
// callers pass "" and get the concatenated segments back with the final one
// untouched, which is the only sensible degradation when there is nothing to
// compact it to.
func RenderBare(segments []string, abbrev string) string {
	kept := nonAccessorSegments(segments)
	if len(kept) == 0 {
		return ""
	}
	if abbrev != "" {
		last := kept[len(kept)-1]
		if m := trailingOrdinalRe.FindStringSubmatch(last); m != nil {
			compacted := make([]string, len(kept))
			copy(compacted, kept)
			compacted[len(compacted)-1] = abbrev + m[1]
			kept = compacted
		}
	}
	return strings.Join(kept, "")
}
