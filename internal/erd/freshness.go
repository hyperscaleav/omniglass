package erd

import (
	"encoding/base64"
	"html"
	"regexp"
	"slices"
	"strings"
)

// svgText is a text node a rendered SVG draws, with its class (which says whether it
// is a table name, a column name, a type or a key label) and its content.
var svgText = regexp.MustCompile(`<text[^>]*class="text fill-([A-Z0-9]+)"[^>]*>([^<]*)</text>`)

// svgGroup is a group the renderer names with the base64 of the D2 path it drew:
// "telemetry.alarm" for a table, "(a.t -> b.u)[0]" for a connection.
var svgGroup = regexp.MustCompile(`<g class="([A-Za-z0-9+/]{4,}={0,2})">`)

var (
	d2Cluster = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*): \{$`)
	d2Table   = regexp.MustCompile(`^  ([A-Za-z_][A-Za-z0-9_]*): \{$`)
	d2Column  = regexp.MustCompile(`^    ([A-Za-z_][A-Za-z0-9_]*): ([A-Za-z0-9_\[\]() ,]+?)(?: \{constraint: ([^}]*)\})?$`)
	d2Edge    = regexp.MustCompile(`^([a-z_0-9]+)\.([a-z_0-9]+)\.[a-z_0-9]+ -> ([a-z_0-9]+)\.([a-z_0-9]+)\.[a-z_0-9]+$`)
	label     = map[string]string{"primary_key": "PK", "foreign_key": "FK", "unique": "UNQ"}
	letters   = regexp.MustCompile(`[A-Za-z_]+`)
)

// structure is a diagram reduced to comparable facts: one string per cluster, table,
// column (its table, name, type and key labels) and connection (its two tables),
// with a repeat counted, as the renderer numbers it.
type structure map[string]int

func (s structure) add(fact string) { s[fact]++ }

// labels normalizes a column's key labels, whether the source spells them as
// constraint words or the render as PK/FK/UNQ, to one sorted string.
func labels(words []string) string {
	var out []string
	for _, w := range words {
		if l, ok := label[w]; ok {
			w = l
		}
		out = append(out, w)
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

// sourceStructure reads the D2 source erdgen emits: clusters at column 0, tables at
// two spaces, columns at four, then one connection per line.
func sourceStructure(d2 string) structure {
	s := structure{}
	cluster, table := "", ""
	for _, line := range strings.Split(d2, "\n") {
		switch {
		case d2Cluster.MatchString(line):
			cluster = d2Cluster.FindStringSubmatch(line)[1]
			s.add("cluster " + cluster)
		case d2Table.MatchString(line):
			table = d2Table.FindStringSubmatch(line)[1]
			s.add("table " + cluster + "." + table)
		case strings.TrimSpace(line) == "shape: sql_table":
		case d2Column.MatchString(line):
			m := d2Column.FindStringSubmatch(line)
			s.add("column " + table + "." + m[1] + " " + m[2] + " " + labels(letters.FindAllString(m[3], -1)))
		case d2Edge.MatchString(line):
			m := d2Edge.FindStringSubmatch(line)
			s.add("edge " + m[1] + "." + m[2] + " -> " + m[3] + "." + m[4])
		}
	}
	return s
}

// renderStructure reads the same facts back out of the SVG: group classes name the
// clusters, tables and connections, and the text nodes carry each table's columns
// (a name, a type, a key label, in drawing order).
func renderStructure(svg string) structure {
	s := structure{}
	for _, m := range svgGroup.FindAllStringSubmatch(svg, -1) {
		raw, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			continue
		}
		path := html.UnescapeString(string(raw))
		switch {
		case strings.Contains(path, "("):
			s.add("edge " + edgePath(path))
		case strings.Contains(path, "."):
			s.add("table " + path)
		default:
			s.add("cluster " + path)
		}
	}
	table, col, typ := "", "", ""
	for _, m := range svgText.FindAllStringSubmatch(svg, -1) {
		text := html.UnescapeString(m[2])
		switch m[1] {
		case "N7":
			table = text
		case "B2":
			col = text
		case "N2":
			typ = text
		case "AA2":
			s.add("column " + table + "." + col + " " + typ + " " + labels(letters.FindAllString(text, -1)))
		}
	}
	return s
}

// edgePath turns a rendered connection id ("telemetry.(a -> b)[0]" inside one
// cluster, "(x.a -> y.b)[0]" across two) into "x.a -> y.b"; the index the renderer
// adds is dropped, the repeats being counted instead.
func edgePath(p string) string {
	prefix, rest, _ := strings.Cut(p, "(")
	rest, _, _ = strings.Cut(rest, ")")
	from, to, _ := strings.Cut(rest, " -> ")
	return prefix + from + " -> " + prefix + to
}

// RenderDrift compares a diagram's D2 source with the SVG rendered from it, by
// structure: every cluster, table, column (name, type and key labels) and
// connection (#2625). missing is what the source says and the SVG never drew;
// extra is what the SVG draws and the source no longer says. Both empty is a render
// that matches its source. A connection is compared by its two tables, since the
// render does not record which columns it joins; the FK labels on the columns cover
// the rest. It needs no d2 binary, so the docs build and make test can hold the
// committed render to its source.
func RenderDrift(d2, svg string) (missing, extra []string) {
	src, drawn := sourceStructure(d2), renderStructure(svg)
	for fact, n := range src {
		if drawn[fact] < n {
			missing = append(missing, fact)
		}
	}
	for fact, n := range drawn {
		if src[fact] < n {
			extra = append(extra, fact)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	return missing, extra
}
