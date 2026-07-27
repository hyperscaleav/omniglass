// Package erd renders an entity-relationship diagram of the platform schema as
// D2. It has two halves: Introspect reads the live Postgres catalog into a
// Schema, and Render turns a Schema into a subsystem-clustered D2 document. The
// D2 carries structure and semantic shapes only, never colors: the docs site's
// custom.css themes it from the brand tokens (the docs-diagram contract).
package erd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
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

// querier is the subset of pgx used for introspection; both *pgx.Conn and
// *pgxpool.Pool satisfy it.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Introspect reads the public schema of a live Postgres into a Schema: base
// tables (excluding dbmate's schema_migrations), their columns, and their
// primary- and foreign-key constraints. Tables and columns come back in catalog
// order; Render sorts for a stable document.
func Introspect(ctx context.Context, db querier) (Schema, error) {
	cols, err := queryColumns(ctx, db)
	if err != nil {
		return Schema{}, err
	}
	if err := markPrimaryKeys(ctx, db, cols); err != nil {
		return Schema{}, err
	}
	fks, err := queryForeignKeys(ctx, db, cols)
	if err != nil {
		return Schema{}, err
	}

	names, err := queryTables(ctx, db)
	if err != nil {
		return Schema{}, err
	}
	s := Schema{Tables: make([]Table, 0, len(names))}
	for _, n := range names {
		s.Tables = append(s.Tables, Table{Name: n, Columns: cols[n], FKs: fks[n]})
	}
	return s, nil
}

func queryTables(ctx context.Context, db querier) ([]string, error) {
	rows, err := db.Query(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_type = 'BASE TABLE'
		  AND table_name <> 'schema_migrations'
		ORDER BY table_name`)
	if err != nil {
		return nil, fmt.Errorf("erd: query tables: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// queryColumns returns a per-table, ordinal-ordered column list keyed by table
// name.
func queryColumns(ctx context.Context, db querier) (map[string][]Column, error) {
	rows, err := db.Query(ctx, `
		SELECT table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("erd: query columns: %w", err)
	}
	defer rows.Close()
	out := map[string][]Column{}
	for rows.Next() {
		var tbl, name, typ string
		if err := rows.Scan(&tbl, &name, &typ); err != nil {
			return nil, err
		}
		out[tbl] = append(out[tbl], Column{Name: name, Type: typ})
	}
	return out, rows.Err()
}

func markPrimaryKeys(ctx context.Context, db querier, cols map[string][]Column) error {
	rows, err := db.Query(ctx, `
		SELECT kcu.table_name, kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		 AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY'
		  AND tc.table_schema = 'public'`)
	if err != nil {
		return fmt.Errorf("erd: query primary keys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tbl, col string
		if err := rows.Scan(&tbl, &col); err != nil {
			return err
		}
		setFlag(cols, tbl, col, func(c *Column) { c.PK = true })
	}
	return rows.Err()
}

func queryForeignKeys(ctx context.Context, db querier, cols map[string][]Column) (map[string][]ForeignKey, error) {
	rows, err := db.Query(ctx, `
		SELECT kcu.table_name, kcu.column_name,
		       ccu.table_name AS ref_table, ccu.column_name AS ref_column
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		 AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage ccu
		  ON tc.constraint_name = ccu.constraint_name
		 AND tc.table_schema = ccu.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema = 'public'
		ORDER BY kcu.table_name, kcu.column_name`)
	if err != nil {
		return nil, fmt.Errorf("erd: query foreign keys: %w", err)
	}
	defer rows.Close()
	out := map[string][]ForeignKey{}
	for rows.Next() {
		var tbl, col, refTbl, refCol string
		if err := rows.Scan(&tbl, &col, &refTbl, &refCol); err != nil {
			return nil, err
		}
		out[tbl] = append(out[tbl], ForeignKey{Column: col, RefTable: refTbl, RefColumn: refCol})
		setFlag(cols, tbl, col, func(c *Column) { c.FK = true })
	}
	return out, rows.Err()
}

// setFlag applies fn to the named column in place.
func setFlag(cols map[string][]Column, tbl, col string, fn func(*Column)) {
	for i := range cols[tbl] {
		if cols[tbl][i].Name == col {
			fn(&cols[tbl][i])
			return
		}
	}
}
