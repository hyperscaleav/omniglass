package erd

import (
	"regexp"
	"slices"
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

// RenderDrift compares a diagram's D2 source with the SVG rendered from it, by the
// identifiers each carries (#2625). missing is what the source names and the SVG
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
