package storage

import (
	"strconv"
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
// ("dsp1") when the entity carries a platform-allocated ordinal.
//
// ordinal is the number the generator STORED on the row (#681), not one read
// back out of the name: nil means no ordinal the platform owns, which is
// exactly the state an operator-typed name and an operator-renamed row are
// both in. abbrev is the component_type's, resolved through the same
// inherited-from-parent chain generateNameForProduct walks.
//
// That the two facts now come from different places is what makes the #654
// guarantee structural rather than defensive. This render used to confirm the
// substitution was legitimate by re-deriving it: match a trailing "-<n>",
// then (after #654) confirm the segment was the type's own "<stem>-<n>". Both
// were inferences about who had named the row, drawn from the string that was
// about to be replaced. The row now says so directly. A component an operator
// renamed to "rack-3" has no stored ordinal, so there is no number to
// substitute and no rule to get wrong: "booth-2", "row-14", "rack-3" all keep
// their own words, and so does a hand-typed "display-1" that the old
// stem-match would have compacted.
//
// A caller with no abbrev or no ordinal gets the concatenated segments back
// with the last one untouched, the only sensible degradation when there is
// nothing to compact it to. System and location addresses have no type-level
// abbreviation and no generated ordinal today, so their callers pass nil and
// "".
func RenderBare(segments []string, ordinal *int, abbrev string) string {
	kept := nonAccessorSegments(segments)
	if len(kept) == 0 {
		return ""
	}
	if ordinal != nil && abbrev != "" {
		compacted := make([]string, len(kept))
		copy(compacted, kept)
		compacted[len(compacted)-1] = abbrev + strconv.Itoa(*ordinal)
		kept = compacted
	}
	return strings.Join(kept, "")
}
