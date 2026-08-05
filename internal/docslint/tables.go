package docslint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The storage-table lint (#450): a `table.column` reference in current-tense
// prose must exist in the introspected schema facts (docs/src/generated/
// schema.json, emitted by make gen). The hand-written per-page storage tables
// were the densest drift in the corpus; the generated SchemaTable component
// replaced most of them, and this lint is the backstop for the inline
// references the component does not cover. Fence-aware: a fenced table or
// column is target design.

// tableColumnSpan matches a dotted reference whose head could be a table. The
// scanner only fires when the head IS a known table, so subject names
// (og.v1...), frontmatter paths (sidebar.badge), and JSON field paths with
// non-table heads never reach the check.
var tableColumnSpan = regexp.MustCompile(`^([a-z_]+)\.([a-z_]+)`)

// loadSeededDottedNames reads the generated seed facts for registry keys that
// carry a dot (property, event, and command type names like command-issued or
// interface-reachable): a dotted NAME is not a column claim, and the seed facts
// are the authoritative list of them.
func loadSeededDottedNames() (map[string]bool, error) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "docs", "src", "generated", "seed.json"))
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, m := range seededName.FindAllStringSubmatch(string(b), -1) {
		names[m[1]] = true
	}
	return names, nil
}

var seededName = regexp.MustCompile(`"name": "([a-z_]+\.[a-z_.]+)"`)

// loadSchemaColumns reads the generated schema facts into table -> columns.
func loadSchemaColumns() (map[string]map[string]bool, error) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "docs", "src", "generated", "schema.json"))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Subsystems map[string]map[string]struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
		} `json:"subsystems"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	tables := map[string]map[string]bool{}
	for _, sub := range doc.Subsystems {
		for name, t := range sub {
			cols := map[string]bool{}
			for _, c := range t.Columns {
				cols[c.Name] = true
			}
			tables[name] = cols
		}
	}
	return tables, nil
}

// scanTableColumnsIn returns a finding per dotted span whose head is a live
// table and whose column is not on it.
func scanTableColumnsIn(line string, tables map[string]map[string]bool, seededNames map[string]bool) []string {
	if illustrativeProse.MatchString(line) {
		return nil
	}
	var out []string
	for _, m := range inlineSpan.FindAllStringSubmatch(line, -1) {
		span := strings.TrimSpace(m[1])
		tc := tableColumnSpan.FindStringSubmatch(span)
		if tc == nil {
			continue
		}
		if seededNames[span] {
			continue // a seeded registry key, a name that happens to dot
		}
		cols, ok := tables[tc[1]]
		if !ok {
			continue
		}
		if !cols[tc[2]] {
			out = append(out, span+": table "+tc[1]+" has no column "+tc[2]+" in the live schema (design goes in a fence; a drifted mention names the real column)")
		}
	}
	return out
}

// tableColumnAllowed lists pages whose dotted spans follow a different
// convention entirely: the Helm guide's spans are chart values paths
// (service.port, image.tag), and a database column reference does not belong
// on it in the first place.
var tableColumnAllowed = map[string]bool{
	filepath.Join("guides", "helm.mdx"): true,
}

// ScanTableColumns runs the storage-table lint over the docs tree.
func ScanTableColumns() ([]Finding, error) {
	tables, err := loadSchemaColumns()
	if err != nil {
		return nil, err
	}
	seededNames, err := loadSeededDottedNames()
	if err != nil {
		return nil, err
	}
	return scanUnfenced(func(rel, line string, inCode bool) []string {
		if inCode || tableColumnAllowed[rel] {
			return nil // examples quote partial shapes; Helm spans are values paths
		}
		return scanTableColumnsIn(line, tables, seededNames)
	})
}
