// Package erd renders an entity-relationship diagram of the platform schema as
// D2. It has two halves: Introspect reads the live Postgres catalog into a
// Schema, and Render turns a Schema into a subsystem-clustered D2 document. The
// D2 carries structure and semantic shapes only, never colors: the docs site's
// custom.css themes it from the brand tokens (the docs-diagram contract).
package erd

import (
	"fmt"
	"sort"
	"strings"
)

// Column is one column of a table. Only primary-key and foreign-key columns are
// rendered (the relational skeleton); the rest are introspected but omitted to
// keep a 40-plus-table diagram legible.
type Column struct {
	Name string
	Type string
	PK   bool
	FK   bool
}

// ForeignKey is one foreign-key edge: Column on the owning table references
// RefTable.RefColumn.
type ForeignKey struct {
	Column    string
	RefTable  string
	RefColumn string
}

// Table is one relation with its columns and outbound foreign keys.
type Table struct {
	Name    string
	Columns []Column
	FKs     []ForeignKey
}

// Schema is the whole introspected public schema.
type Schema struct {
	Tables []Table
}

// Cluster is a named subsystem and the tables that belong to it.
type Cluster struct {
	Name   string
	Tables []string
}

// unclustered is the container that catches any table not named in the cluster
// map, so a newly added, unmapped table shows up in the diagram (and trips the
// drift gate) instead of vanishing.
const unclustered = "unclustered"

// Render turns a Schema into a D2 ERD. Each cluster becomes a container (in the
// given order), each table a sql_table node listing its primary-key and
// foreign-key columns, and each foreign key an edge between the two rows. A
// table absent from every cluster lands in a trailing "unclustered" container.
// Pure and deterministic: tables, columns, and edges are sorted so the output is
// stable and diffable.
func Render(s Schema, clusters []Cluster) (string, error) {
	tables := make(map[string]Table, len(s.Tables))
	names := make([]string, 0, len(s.Tables))
	for _, t := range s.Tables {
		if _, dup := tables[t.Name]; dup {
			return "", fmt.Errorf("erd: duplicate table %q", t.Name)
		}
		tables[t.Name] = t
		names = append(names, t.Name)
	}

	// loc maps every table to its container; unmapped tables fall to unclustered.
	loc := make(map[string]string, len(names))
	for _, c := range clusters {
		for _, t := range c.Tables {
			loc[t] = c.Name
		}
	}
	var orphans []string
	for _, n := range names {
		if _, ok := loc[n]; !ok {
			loc[n] = unclustered
			orphans = append(orphans, n)
		}
	}

	order := make([]Cluster, 0, len(clusters)+1)
	order = append(order, clusters...)
	if len(orphans) > 0 {
		order = append(order, Cluster{Name: unclustered, Tables: orphans})
	}

	var b strings.Builder
	b.WriteString("direction: right\n")
	for _, c := range order {
		present := make([]string, 0, len(c.Tables))
		for _, t := range c.Tables {
			if _, ok := tables[t]; ok {
				present = append(present, t)
			}
		}
		sort.Strings(present)
		if len(present) == 0 {
			continue
		}
		b.WriteString("\n" + c.Name + ": {\n")
		for _, tn := range present {
			writeTable(&b, tables[tn])
		}
		b.WriteString("}\n")
	}

	if edges := edgeLines(s, loc); len(edges) > 0 {
		b.WriteString("\n")
		for _, e := range edges {
			b.WriteString(e + "\n")
		}
	}
	return b.String(), nil
}

// writeTable emits one sql_table node with its primary-key then foreign-key
// columns.
func writeTable(b *strings.Builder, t Table) {
	b.WriteString("  " + t.Name + ": {\n")
	b.WriteString("    shape: sql_table\n")
	for _, col := range keyColumns(t) {
		constraint := ""
		switch {
		case col.PK:
			constraint = " {constraint: primary_key}"
		case col.FK:
			constraint = " {constraint: foreign_key}"
		}
		b.WriteString(fmt.Sprintf("    %s: %s%s\n", col.Name, col.Type, constraint))
	}
	b.WriteString("  }\n")
}

// keyColumns returns a table's primary-key columns then its foreign-key columns,
// each group sorted by name. A column that is both is rendered as a primary key.
func keyColumns(t Table) []Column {
	var pks, fks []Column
	for _, c := range t.Columns {
		switch {
		case c.PK:
			pks = append(pks, c)
		case c.FK:
			fks = append(fks, c)
		}
	}
	sort.Slice(pks, func(i, j int) bool { return pks[i].Name < pks[j].Name })
	sort.Slice(fks, func(i, j int) bool { return fks[i].Name < fks[j].Name })
	return append(pks, fks...)
}

// edgeLines returns the sorted foreign-key edges, each as a container-qualified
// row-to-row connection.
func edgeLines(s Schema, loc map[string]string) []string {
	lines := make([]string, 0)
	for _, t := range s.Tables {
		for _, fk := range t.FKs {
			from := loc[t.Name] + "." + t.Name + "." + fk.Column
			to := loc[fk.RefTable] + "." + fk.RefTable + "." + fk.RefColumn
			lines = append(lines, from+" -> "+to)
		}
	}
	sort.Strings(lines)
	return lines
}
