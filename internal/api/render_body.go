package api

import "github.com/hyperscaleav/omniglass/internal/storage"

// renderBody is the wire shape of storage.Renders (#627 Task 15): the two
// display-only compact forms of a component, system, or location's dotted
// path. Shared by all three entity bodies rather than three copies, the same
// reason componentBody/systemBody/locationBody share checkNameOutput.
type renderBody struct {
	Dash string `json:"dash" doc:"The path's non-accessor segments joined with '-' (e.g. boi-17c-216b-display-1). Display only; not accepted by the resolver."`
	// Not abbrev-compacted on a LIST row (review finding 3,
	// task-15-review.md #2): resolving a component's abbrev
	// (componentTypeIDForProduct plus resolveTypeFacts' own ancestor walk)
	// costs two more queries per distinct product on the page, for a field
	// no console surface reads. A LIST row's bare still concatenates every
	// segment (still a real, resolvable-by-eye compact form), it just skips
	// the substitution a GET applies.
	Bare string `json:"bare" doc:"The dash render's segments concatenated with no separator, with the final stem-ordinal segment compacted to <abbrev><ordinal> when the owning type registers one (e.g. boi17c216bdsp1). On a LIST row the substitution is skipped (segments concatenated as-is) to avoid a per-row abbrev resolution; a GET always compacts it. Display only either way; not accepted by the resolver."`
}

// toRenderBody wraps r for the wire, or nil when path is empty (a
// create/update/move/rename/resetName response: the path attach only runs on
// a GET or LIST fetch, see scopedConfig.attachPaths' doc comment). A present
// but path-less Renders would otherwise serialize as two empty strings,
// which reads as "this entity has no path" rather than "not computed here".
func toRenderBody(path string, r storage.Renders) *renderBody {
	if path == "" {
		return nil
	}
	return &renderBody{Dash: r.Dash, Bare: r.Bare}
}
