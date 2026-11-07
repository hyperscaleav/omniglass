package compressbench

import (
	"fmt"
	"strings"
	"time"
)

// Image is a Postgres 18 image a candidate runs on, and the server arguments
// it needs. Every image runs with fsync off: this measures storage and read
// shape, not the disk (the ingest ceiling is #1517).
type Image struct {
	Key       string
	Ref       string
	Extension string
	Args      []string
}

var (
	ImagePlain       = Image{Key: "plain", Ref: "postgres:18", Args: []string{"-c", "fsync=off"}}
	ImageDeltax      = Image{Key: "deltax", Ref: "omniglass-compressbench-deltax:dev", Extension: "pg_deltax", Args: []string{"-c", "fsync=off", "-c", "shared_preload_libraries=pg_deltax"}}
	ImageTimescaleDB = Image{Key: "timescaledb", Ref: "omniglass-compressbench-timescaledb:dev", Extension: "timescaledb", Args: []string{"-c", "fsync=off", "-c", "shared_preload_libraries=timescaledb", "-c", "timescaledb.telemetry_level=off"}}
)

// Candidate is one storage format: the DDL that creates it, the untimed
// statements that land the staging rows in it, the timed statements that
// compress them, and the reads every candidate answers.
//
// Every read binds $1 series, $2 from and $3 to. ReadRaw returns (ts, value)
// in timestamp then landing order. ReadBuckets also binds $4, the bucket width,
// and returns (start, count, min, max, avg). ReadAll returns every sample as
// (series_id, ts, value) in series, timestamp and landing order. Check, when
// set, counts rows that break the candidate's own invariants and must be zero.
type Candidate struct {
	Name        string
	Image       Image
	Setup       []string
	Load        []string
	Compress    []string
	ReadRaw     string
	ReadBuckets string
	ReadAll     string
	Check       string
	// HourlyAll, when set, returns every non-empty hour's stored summary as
	// (series_id, hour, count, min, max, sum, first, last) by series then hour.
	HourlyAll string
	// SummaryBytes, when set, is the stored bytes of the summary columns;
	// CompressedRows the share of rows whose sample array Postgres compressed.
	SummaryBytes   string
	CompressedRows string
	// DaySpans, when set, returns how each series-day is stored as
	// (series_id, day, span, count) by series then day; WantDaySpans is the
	// rule it is held to.
	DaySpans     string
	WantDaySpans func([]Sample) []DaySpan
	// Merge lands one late sample, binding $1 series, $2 ts and $3 value: for a
	// block, an in-place read-merge-rewrite of the one closed block it falls in.
	Merge string
	// Cagg, when set, is a continuous aggregate built after the candidate is
	// measured, reported as a row of its own.
	Cagg *Cagg
}

// Cagg is a continuous aggregate over a candidate: the DDL, the timed refresh,
// and the bucketed read it answers instead of the base table.
type Cagg struct {
	Name        string
	Setup       []string
	Refresh     []string
	ReadBuckets string
	// HourlyAll, when set, returns every stored hour as Candidate's does.
	HourlyAll string
}

// The staging table every candidate is built from: the samples in landing
// order. It lives in its own schema, outside every size.
const stagingDDL = `create schema staging;
create table staging.sample (seq bigint not null, series_id bigint not null, ts timestamptz not null, value float8 not null)`

// landRaw lands the staging rows in a narrow table, ids in landing order.
const landRaw = `insert into metric (series_id, ts, value) select series_id, ts, value from staging.sample order by seq`

const (
	lateInsert = `insert into metric (series_id, ts, value) values ($1, $2, $3)`
	rawRead    = `select ts, value from metric where series_id = $1 and ts >= $2 and ts < $3 order by ts, id`
	rawAll     = `select series_id, ts, value from metric order by series_id, ts, id`
)

// narrowBuckets is the bucketed read on a narrow table, binning with bin, an
// expression over $4 and ts.
func narrowBuckets(bin string) string {
	return `select ` + bin + ` as b, count(*), min(value), max(value), avg(value)
from metric where series_id = $1 and ts >= $2 and ts < $3 group by b order by b`
}

// productMetric is the product's metric table (migration
// 20261029132025_metric_series_narrow), its indexes, and a month partition
// for every month of the span.
func productMetric(spec Spec) []string {
	stmts := []string{`create table metric (
	id bigint generated always as identity,
	series_id bigint not null,
	ts timestamptz default now() not null,
	value float8 not null,
	source_rule_version bigint,
	source_rule text,
	source_rule_id uuid
) partition by range (ts)`}
	for m := monthOf(spec.start()); m.Before(spec.End()); m = m.AddDate(0, 1, 0) {
		stmts = append(stmts, fmt.Sprintf(`create table metric_p%s partition of metric for values from ('%s') to ('%s')`,
			m.Format("200601"), m.Format(time.RFC3339), m.AddDate(0, 1, 0).Format(time.RFC3339)))
	}
	return append(stmts, productIndexes...)
}

var productIndexes = []string{
	`create index metric_series_ts_idx on metric (series_id, ts desc, id desc)`,
	`create index metric_id_brin on metric using brin (id)`,
	`create index metric_ts_brin on metric using brin (ts)`,
}

func monthOf(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC) }

// Raw is the product's narrow row, uncompressed: the reference every other
// candidate is measured against.
func Raw(spec Spec) Candidate {
	return Candidate{
		Name: "raw", Image: ImagePlain,
		Setup:   productMetric(spec),
		Load:    []string{landRaw},
		ReadRaw: rawRead, ReadBuckets: narrowBuckets(`date_bin($4::interval, ts, ` + blockOrigin + `)`), ReadAll: rawAll,
		Merge: lateInsert,
	}
}

// Block is a homegrown format: one row a series a UTC span (an hour, day, week
// or month), values in a float8[] and their timestamps either as a
// timestamptz[] or as millisecond offsets from the block's start, with the
// block's summary beside them. A block longer than an hour also carries a
// summary of each hour it spans, as arrays of one element an hour (h_n, h_min,
// h_max, h_sum, h_first, h_last; an empty hour counts 0 and its other
// elements are null), so an hourly read never opens the sample arrays.
type Block struct {
	Span    string
	Offsets bool
}

// Name is F0 (timestamps) or F2 (offsets), with "/<span>" past an hour.
func (b Block) Name() string {
	name := "F0"
	if b.Offsets {
		name = "F2"
	}
	if b.Span != "hour" {
		name += "/" + b.Span
	}
	return name
}

func (b Block) hourly() bool { return b.Span != "hour" }

// tsType is the timestamps' column type. An int4 holds 24.8 days of
// milliseconds, so a month block's offsets are int8.
func (b Block) tsType() string {
	switch {
	case !b.Offsets:
		return "timestamptz[]"
	case b.Span == "month":
		return "int8[]"
	}
	return "int4[]"
}

// startOf is the start of the block an instant falls in. Every connection
// runs in UTC, so a day, week and month are UTC ones.
func (b Block) startOf(ts string) string { return "date_trunc('" + b.Span + "', " + ts + ")" }

func (b Block) end(start string) string { return start + " + interval '1 " + b.Span + "'" }

// blockOrigin is the origin every bucket is binned from.
const blockOrigin = `timestamptz '2026-09-01 00:00:00+00'`

var hourCols = []string{"h_n", "h_min", "h_max", "h_sum", "h_first", "h_last"}

func (b Block) columns() []string {
	cols := []string{"series_id", "start", "count", "min", "max", "sum", "first", "last", "vals", "ts"}
	if b.hourly() {
		cols = append(cols, hourCols...)
	}
	return cols
}

// build is the select that makes the block rows of src, a relation of
// (series_id, ts, value, seq) where seq is landing order. Compression builds
// from the staging rows; a late-row merge rebuilds one block from its own
// samples and the late one.
func (b Block) build(src string) string {
	tsAgg := "array_agg(ts order by ts, seq)"
	if b.Offsets {
		tsAgg = "array_agg((extract(epoch from ts - start) * 1000)::" + strings.TrimSuffix(b.tsType(), "[]") + " order by ts, seq)"
	}
	q := `with s as (select series_id, ts, value, seq, ` + b.startOf("ts") + ` as start from ` + src + `),
blk as (select series_id, start, count(*)::int4 as count, min(value) as min, max(value) as max, sum(value) as sum,
	(array_agg(value order by ts, seq))[1] as first, (array_agg(value order by ts desc, seq desc))[1] as last,
	array_agg(value order by ts, seq) as vals, ` + tsAgg + ` as ts
from s group by 1, 2)`
	if !b.hourly() {
		return q + `
select * from blk`
	}
	return q + `,
h as (select series_id, start, date_trunc('hour', ts) as hr, count(*)::int4 as n, min(value) as mn, max(value) as mx, sum(value) as sm,
	(array_agg(value order by ts, seq))[1] as fv, (array_agg(value order by ts desc, seq desc))[1] as lv
from s group by 1, 2, 3),
hs as (select k.series_id, k.start, array_agg(coalesce(h.n, 0) order by g.hr) as h_n, array_agg(h.mn order by g.hr) as h_min,
	array_agg(h.mx order by g.hr) as h_max, array_agg(h.sm order by g.hr) as h_sum,
	array_agg(h.fv order by g.hr) as h_first, array_agg(h.lv order by g.hr) as h_last
from blk k cross join lateral generate_series(k.start, ` + b.end("k.start") + ` - interval '1 hour', interval '1 hour') as g(hr)
	left join h on h.series_id = k.series_id and h.start = k.start and h.hr = g.hr
group by k.series_id, k.start)
select blk.series_id, blk.start, blk.count, blk.min, blk.max, blk.sum, blk.first, blk.last, blk.vals, blk.ts,
	hs.h_n, hs.h_min, hs.h_max, hs.h_sum, hs.h_first, hs.h_last
from blk join hs on hs.series_id = blk.series_id and hs.start = blk.start`
}

// Candidate is b as a benchmark candidate.
func (b Block) Candidate() Candidate {
	tsRead := "u.t"
	if b.Offsets {
		tsRead = "b.start + u.t * interval '1 millisecond'"
	}
	defs := []string{"series_id bigint not null", "start timestamptz not null", "count int4 not null",
		"min float8 not null", "max float8 not null", "sum float8 not null", "first float8 not null", "last float8 not null",
		"vals float8[] not null", "ts " + b.tsType() + " not null"}
	summary := []string{"count", "min", "max", "sum", "first", "last"}
	check := `select count(*) from block where count <> cardinality(vals) or count <> cardinality(ts)
	or first <> vals[1] or last <> vals[cardinality(vals)]`
	hourlyAll := `select series_id, start, count::int8, min, max, sum, first, last from block order by series_id, start`
	buckets := `select date_bin($4::interval, start, ` + blockOrigin + `) as bk, sum(count)::int8, min(min), max(max), sum(sum) / sum(count)
from block where series_id = $1 and start >= $2 and start < $3 group by bk order by bk`
	if b.hourly() {
		defs = append(defs, "h_n int4[] not null", "h_min float8[] not null", "h_max float8[] not null",
			"h_sum float8[] not null", "h_first float8[] not null", "h_last float8[] not null")
		summary = append(summary, hourCols...)
		check += `
	or cardinality(h_n) * 3600 <> extract(epoch from ` + b.end("start") + ` - start)
	or count <> (select sum(x) from unnest(h_n) x)`
		hours := `from block b, unnest(b.h_n, b.h_min, b.h_max, b.h_sum, b.h_first, b.h_last) with ordinality as h(n, mn, mx, sm, fv, lv, o)`
		hourlyAll = `select b.series_id, b.start + (h.o - 1) * interval '1 hour' as hr, h.n::int8, h.mn, h.mx, h.sm, h.fv, h.lv
` + hours + `
where h.n > 0 order by 1, 2`
		// A bucket is its hours' summaries: the arrays of samples stay shut.
		buckets = `select date_bin($4::interval, hr, ` + blockOrigin + `) as bk, sum(n)::int8, min(mn), max(mx), sum(sm) / sum(n)
from (select b.start + (h.o - 1) * interval '1 hour' as hr, h.n, h.mn, h.mx, h.sm
	` + hours + `
	where b.series_id = $1 and b.start >= ` + b.startOf("$2::timestamptz") + ` and b.start < $3) x
where n > 0 and hr >= $2 and hr < $3 group by bk order by bk`
	}
	sizes := make([]string, len(summary))
	for i, c := range summary {
		sizes[i] = "pg_column_size(" + c + ")"
	}
	cols := b.columns()
	sets := make([]string, 0, len(cols)-2)
	for _, c := range cols[2:] {
		sets = append(sets, c+" = excluded."+c)
	}
	unnest := `from block b, unnest(b.ts, b.vals) with ordinality as u(t, v, o)`
	one := `b.series_id = $1 and b.start = ` + b.startOf("$2::timestamptz")
	return Candidate{
		Name: b.Name(), Image: ImagePlain,
		Setup:    []string{"create table block (\n\t" + strings.Join(defs, ",\n\t") + ",\n\tprimary key (series_id, start)\n)"},
		Compress: []string{"insert into block (" + strings.Join(cols, ", ") + ")\n" + b.build("staging.sample")},
		ReadRaw: `select ts, v from (select ` + tsRead + ` as ts, u.v, b.start, u.o ` + unnest + `
	where b.series_id = $1 and b.start >= ` + b.startOf("$2::timestamptz") + ` and b.start < $3) r
where ts >= $2 and ts < $3 order by start, o`,
		ReadBuckets:    buckets,
		ReadAll:        `select b.series_id, ` + tsRead + `, u.v ` + unnest + ` order by b.series_id, b.start, u.o`,
		Check:          check,
		HourlyAll:      hourlyAll,
		SummaryBytes:   `select coalesce(sum(` + strings.Join(sizes, " + ") + `), 0)::int8 from block`,
		CompressedRows: `select coalesce(avg((pg_column_compression(vals) is not null)::int), 0)::float8 from block`,
		// The late sample sorts after every sample already in its block.
		Merge: "insert into block (" + strings.Join(cols, ", ") + ")\n" + b.build(`(select b.series_id, `+tsRead+` as ts, u.v as value, u.o as seq `+unnest+`
	where `+one+`
	union all select $1::int8, $2::timestamptz, $3::float8, 9223372036854775807) src`) + `
on conflict (series_id, start) do update set ` + strings.Join(sets, ", "),
	}
}

// Deltax is pg_deltax on the narrow row, partitioned by partition, segmented
// by series and ordered by ts then id (the id keeps duplicates of one instant
// in landing order, ADR-0061). The identity is BY DEFAULT: the default
// partition's drain re-inserts every column and a GENERATED ALWAYS identity
// refuses it (shown by the fit probes).
func Deltax(spec Spec, partition time.Duration, suffix string) Candidate {
	return Candidate{
		Name: "F4" + suffix, Image: ImageDeltax,
		Setup: append([]string{`create table metric (
	id bigint generated by default as identity,
	series_id bigint not null,
	ts timestamptz default now() not null,
	value float8 not null,
	source_rule_version bigint,
	source_rule text,
	source_rule_id uuid
)`, fmt.Sprintf(`select deltax.deltax_create_table('metric', 'ts', interval '%d seconds', 0)`, int(partition.Seconds()))},
			productIndexes[0]),
		Load: []string{landRaw, `select deltax.deltax_drain_default_partition('metric')`,
			`select deltax.deltax_enable_compression('metric', array['series_id'], array['ts', 'id'])`},
		Compress: []string{`select count(*) from deltax.deltax_compress_all_partitions('metric')`},
		ReadRaw:  rawRead, ReadBuckets: narrowBuckets(`deltax.time_bucket($4::interval, ts)`), ReadAll: rawAll,
	}
}

// TimescaleDB is a TSL hypertable with the columnstore on the narrow row,
// chunked by chunk, segmented by series and ordered by ts then id, with an
// hourly continuous aggregate of the same summary a block carries.
func TimescaleDB(spec Spec, chunk time.Duration, suffix string) Candidate {
	return Candidate{
		Name: "F5" + suffix, Image: ImageTimescaleDB,
		Setup: []string{`create table metric (
	id bigint generated always as identity,
	series_id bigint not null,
	ts timestamptz default now() not null,
	value float8 not null,
	source_rule_version bigint,
	source_rule text,
	source_rule_id uuid
)`, productIndexes[0],
			fmt.Sprintf(`select create_hypertable('metric', by_range('ts', interval '%d seconds'))`, int(chunk.Seconds())),
			`alter table metric set (timescaledb.enable_columnstore, timescaledb.segmentby = 'series_id', timescaledb.orderby = 'ts, id')`},
		Load:     []string{landRaw},
		Compress: []string{`select count(compress_chunk(c)) from show_chunks('metric') c`},
		Merge:    lateInsert,
		ReadRaw:  rawRead, ReadBuckets: narrowBuckets(`time_bucket($4::interval, ts)`), ReadAll: rawAll,
		Cagg: &Cagg{
			Name: "F5" + suffix + " cagg",
			Setup: []string{`create materialized view metric_hourly with (timescaledb.continuous) as
select series_id, time_bucket(interval '1 hour', ts) as bucket, count(*) as n, min(value) as mn, max(value) as mx,
	sum(value) as sm, first(value, ts) as fv, last(value, ts) as lv
from metric group by series_id, bucket with no data`},
			Refresh: []string{`call refresh_continuous_aggregate('metric_hourly', null, null)`},
			ReadBuckets: `select time_bucket($4::interval, bucket) as bk, sum(n)::int8, min(mn), max(mx), sum(sm) / sum(n)
from metric_hourly where series_id = $1 and bucket >= $2 and bucket < $3 group by bk order by bk`,
		},
	}
}

// Candidates is every candidate at spec's cadence (#1518): F0 and F2 hour and
// day blocks and F5 on day chunks, against raw. At 1 h, where a day block holds
// only 24 samples, week and month blocks and month chunks join them.
func Candidates(spec Spec) []Candidate {
	spans := []string{"hour", "day"}
	if spec.Cadence == time.Hour {
		spans = append(spans, "week", "month")
	}
	out := []Candidate{Raw(spec)}
	for _, span := range spans {
		for _, offsets := range []bool{false, true} {
			out = append(out, Block{Span: span, Offsets: offsets}.Candidate())
		}
	}
	out = append(out, TimescaleDB(spec, 24*time.Hour, ""))
	if spec.Cadence == time.Hour {
		out = append(out, TimescaleDB(spec, 31*24*time.Hour, "/month"))
	}
	return out
}

// BucketWidth is the bucketed read's width at a cadence: an hour, or a day
// for a sparse series, where an hour holds a single sample.
func BucketWidth(cadence time.Duration) time.Duration {
	if cadence >= 24*time.Hour {
		return 24 * time.Hour
	}
	return time.Hour
}

// sqlIdent is s made safe as a database name.
func sqlIdent(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return '_'
	}, s)
}
