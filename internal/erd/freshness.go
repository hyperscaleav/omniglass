package erd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// words are the identifiers a diagram names: tables, columns and types.
var words = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// svgText is the text a rendered SVG draws.
var svgText = regexp.MustCompile(`<text[^>]*>([^<]*)</text>`)

// d2Syntax is what D2 source says about the diagram and the render never draws.
var d2Syntax = map[string]bool{
	"shape": true, "sql_table": true, "direction": true, "right": true, "left": true, "up": true, "down": true,
	"constraint": true, "primary_key": true, "foreign_key": true, "unique": true,
}

// svgLabels is what the render adds to a column that the source spells as a
// constraint.
var svgLabels = map[string]bool{"PK": true, "FK": true, "UNQ": true}

// sourceHashComment is where docs/scripts/gen-diagrams.mjs records the hash of the
// D2 source it rendered, after the SVG.
var sourceHashComment = regexp.MustCompile(`<!-- d2-source-sha256: ([0-9a-f]{64}) -->`)

// D2SourceHash is the hash gen-diagrams.mjs embeds in a render: the SHA-256 of the
// fenced block's text, without the newline the fence's closing line follows.
func D2SourceHash(d2 string) string {
	sum := sha256.Sum256([]byte(strings.TrimSuffix(d2, "\n")))
	return hex.EncodeToString(sum[:])
}

// RenderStale is whether the SVG was rendered from a different source than d2: by
// the source hash the render carries, which sees every edit, including one that
// reuses names the SVG already draws (a column moved to another table, a type
// changed). A render with no hash was made before the hash and reads as stale.
// why says which, for the message that tells a person to re-render.
func RenderStale(d2, svg string) (stale bool, why string) {
	m := sourceHashComment.FindStringSubmatch(svg)
	if m == nil {
		return true, "the render carries no source hash (re-render it)"
	}
	if got := D2SourceHash(d2); m[1] != got {
		return true, fmt.Sprintf("the source changed since the render (render of %s, source is %s)", m[1][:12], got[:12])
	}
	return false, ""
}

// RenderDrift compares a diagram's D2 source with the SVG rendered from it, by the
// identifiers each carries (#2625). It names what differs when a render is stale,
// but it cannot see an edit that reuses names; RenderStale is the gate. missing is what the source names and the SVG
// never drew (a table or column added since the render); extra is what the SVG
// draws and the source no longer has (a table or column renamed or dropped). Both
// empty is a render that matches its source. It needs no d2 binary, so the docs
// build and make test can hold the committed render to its source.
func RenderDrift(d2, svg string) (missing, extra []string) {
	src := map[string]bool{}
	for _, w := range words.FindAllString(d2, -1) {
		if !d2Syntax[w] {
			src[w] = true
		}
	}
	drawn := map[string]bool{}
	for _, m := range svgText.FindAllStringSubmatch(svg, -1) {
		for _, w := range words.FindAllString(m[1], -1) {
			drawn[w] = true
		}
	}
	for w := range src {
		if !drawn[w] {
			missing = append(missing, w)
		}
	}
	for w := range drawn {
		if !src[w] && !svgLabels[w] {
			extra = append(extra, w)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	return missing, extra
}
