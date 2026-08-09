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

// ordinalOnlyRe matches what is left of a segment once its own stem and the
// separator are cut away: the all-digit ordinal generateComponentName mints
// (namegen.go's ordinalSuffixRe, anchored here against the remainder rather
// than the whole segment, so the match is only ever consulted after the stem
// has already been confirmed).
var ordinalOnlyRe = regexp.MustCompile(`^[0-9]+$`)

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
// punctuation), with the final segment replaced by "<abbrev><ordinal>"
// ("dsp1") when that segment is exactly this type's own "<stem>-<ordinal>"
// ("display-1").
//
// stem and abbrev come from the SAME registry row (component_type's stem and
// abbrev, resolved together through the inherited-from-parent chain
// generateNameForProduct walks), and both are required for the substitution:
// the abbrev says what to write, the stem says the segment is one this type
// actually minted. Matching a bare trailing "-<n>" without confirming the stem
// (#654) rewrote names the platform never generated, so a component an
// operator renamed to "rack-3" rendered as "dsp3", putting a word on a cable
// label that appears nowhere in the entity's name. An operator rename clears
// name_generated for good, and the name they choose is theirs: "booth-2",
// "row-14", "rack-3" all keep their own words.
//
// A caller with no abbrev, no stem, or a final segment that is not this type's
// stem-ordinal pair gets the concatenated segments back with the last one
// untouched, which is the only sensible degradation when there is nothing to
// compact it to. System and location addresses have no type-level abbreviation
// today, so their callers pass "" for both.
func RenderBare(segments []string, stem, abbrev string) string {
	kept := nonAccessorSegments(segments)
	if len(kept) == 0 {
		return ""
	}
	if stem != "" && abbrev != "" {
		last := kept[len(kept)-1]
		if ordinal, ok := strings.CutPrefix(last, stem+"-"); ok && ordinalOnlyRe.MatchString(ordinal) {
			compacted := make([]string, len(kept))
			copy(compacted, kept)
			compacted[len(compacted)-1] = abbrev + ordinal
			kept = compacted
		}
	}
	return strings.Join(kept, "")
}
