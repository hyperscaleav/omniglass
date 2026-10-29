package storagetest

import (
	"fmt"
	"slices"
	"strings"
)

// The metric lane is narrow (#1275, ADR-1275): a sample row is
// (id, series_id, ts, value) in metric, what it is a sample of is a row of
// metric_series, and a command's intended rows are in metric_declared. A test
// that plants rows or counts them in SQL reads and writes the lane in the wide
// shape a metric row had before, through these two helpers, so what it states
// stays the fact it is about rather than the join that stores it.

// MetricRows is the metric lane read in the wide shape, as a parenthesized
// subquery to alias: each hot-tier sample and each declared row beside its
// series' columns (id, ts, owner_kind, component_id, system_id, location_id,
// node_id, metric_type_id, instance, value, provenance, source, source_rule,
// source_rule_version, source_rule_id, event_id, command_id).
const MetricRows = `(select m.id, m.ts, s.owner_kind, s.component_id, s.system_id, s.location_id, s.node_id,
		s.metric_type_id, s.instance, m.value, s.provenance, s.source, m.source_rule, m.source_rule_version,
		m.source_rule_id, null::bigint as event_id, null::bigint as command_id
	from metric m join metric_series s on s.id = m.series_id
	union all
	select d.id, d.ts, s.owner_kind, s.component_id, s.system_id, s.location_id, s.node_id,
		s.metric_type_id, s.instance, d.value, s.provenance, s.source, null, null, null, d.event_id, d.command_id
	from metric_declared d join metric_series s on s.id = d.series_id)`

// wideMetricCols is every column of the wide shape a planted row may name,
// with its type and the value a row that does not name it takes.
var wideMetricCols = []struct{ name, typ, def string }{
	{"ts", "timestamptz", "now()"},
	{"owner_kind", "text", "null"},
	{"component_id", "uuid", "null"},
	{"system_id", "uuid", "null"},
	{"location_id", "uuid", "null"},
	{"node_id", "uuid", "null"},
	{"metric_type_id", "uuid", "null"},
	{"instance", "text", "''"},
	{"value", "float8", "null"},
	{"provenance", "text", "'observed'"},
	{"source", "text", "''"},
	{"source_rule", "text", "null"},
	{"source_rule_version", "bigint", "null"},
	{"source_rule_id", "uuid", "null"},
	{"event_id", "bigint", "null"},
	{"command_id", "bigint", "null"},
}

// WideMetricInsert is the statement that plants metric rows given in the wide
// shape: cols names some of the wide columns (a comma-separated list, as an
// insert's column list would), and rows is a VALUES list or a SELECT yielding
// them in that order, its parameters typed by the columns they fill. Each
// row's series is found or started, then the row lands in the hot tier, or in
// the declared tier when it is intended. Owner kind, type and value are
// required, as they were.
func WideMetricInsert(cols, rows string) string {
	names := strings.Split(cols, ",")
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
	}
	typed := make([]string, len(names))
	for i, n := range names {
		j := slices.IndexFunc(wideMetricCols, func(c struct{ name, typ, def string }) bool { return c.name == n })
		if j < 0 {
			panic(fmt.Sprintf("storagetest: %q is no column of the wide metric shape", n))
		}
		typed[i] = "null::" + wideMetricCols[j].typ
	}
	// A typed row of nulls ahead of the planted rows gives every parameter its
	// column's type; it is dropped by its null owner kind.
	body := strings.TrimSpace(rows)
	if strings.HasPrefix(strings.ToLower(body), "values") {
		body = "values (" + strings.Join(typed, ", ") + "), " + strings.TrimSpace(body[len("values"):])
	} else {
		body = "select " + strings.Join(typed, ", ") + " where false union all " + body
	}
	full := make([]string, len(wideMetricCols))
	for i, c := range wideMetricCols {
		v := c.def
		if slices.Contains(names, c.name) {
			v = "w0." + c.name
			if c.def != "null" {
				v = "coalesce(w0." + c.name + ", " + c.def + ")"
			}
		}
		full[i] = v + "::" + c.typ + " as " + c.name
	}
	return `with w0(` + strings.Join(names, ", ") + `) as (` + body + `),
	w as (select ` + strings.Join(full, ", ") + ` from w0 where w0.owner_kind is not null),
	k as (select distinct metric_type_id, owner_kind, component_id, system_id, location_id, node_id, instance, provenance, source,
		coalesce(component_id, system_id, location_id, node_id) as owner_id from w),
	known as (select s.id, s.owner_id, s.metric_type_id, s.instance, s.provenance, s.source
		from k join metric_series s on s.owner_id = k.owner_id and s.metric_type_id = k.metric_type_id
			and s.instance = k.instance and s.provenance = k.provenance and s.source = k.source and s.collector_node_id is null),
	fresh as (insert into metric_series (metric_type_id, owner_kind, component_id, system_id, location_id, node_id, instance, provenance, source)
		select metric_type_id, owner_kind, component_id, system_id, location_id, node_id, instance, provenance, source from k
		where not exists (select 1 from known n where n.owner_id = k.owner_id and n.metric_type_id = k.metric_type_id
			and n.instance = k.instance and n.provenance = k.provenance and n.source = k.source)
		on conflict do nothing
		returning id, owner_id, metric_type_id, instance, provenance, source),
	series as (select * from known union all select * from fresh),
	hot as (insert into metric (series_id, ts, value, source_rule, source_rule_version, source_rule_id)
		select s.id, w.ts, w.value, w.source_rule, w.source_rule_version, w.source_rule_id
		from w left join series s on s.owner_id = coalesce(w.component_id, w.system_id, w.location_id, w.node_id)
			and s.metric_type_id = w.metric_type_id and s.instance = w.instance and s.provenance = w.provenance and s.source = w.source
		where w.provenance <> 'intended'
		returning id)
	insert into metric_declared (series_id, ts, value, event_id, command_id)
	select s.id, w.ts, w.value, w.event_id, w.command_id
	from w left join series s on s.owner_id = coalesce(w.component_id, w.system_id, w.location_id, w.node_id)
		and s.metric_type_id = w.metric_type_id and s.instance = w.instance and s.provenance = w.provenance and s.source = w.source
	where w.provenance = 'intended'`
}
