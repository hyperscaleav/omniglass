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
}

// The staging table every candidate is built from: the samples in landing
// order. It lives in its own schema, outside every size.
const stagingDDL = `create schema staging;
create table staging.sample (seq bigint not null, series_id bigint not null, ts timestamptz not null, value float8 not null)`

// landRaw lands the staging rows in a narrow table, ids in landing order.
const landRaw = `insert into metric (series_id, ts, value) select series_id, ts, value from staging.sample order by seq`

const (
	rawRead = `select ts, value from metric where series_id = $1 and ts >= $2 and ts < $3 order by ts, id`
	rawAll  = `select series_id, ts, value from metric order by series_id, ts, id`
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
	}
}

// Block is a homegrown format: one row a series a block of width, values in
// a float8[] and their timestamps either as a timestamptz[] or as int4
// millisecond offsets from the block's start, with the block's summary beside
// them. Tuned sets toast_tuple_target=128 and lz4 on the arrays.
type Block struct {
	Width   time.Duration
	Offsets bool
	Tuned   bool
}

// Name is F0 to F3, with "/day" for day blocks.
func (b Block) Name() string {
	n := 0
	if b.Offsets {
		n += 2
	}
	if b.Tuned {
		n++
	}
	name := fmt.Sprintf("F%d", n)
	if b.Width == 24*time.Hour {
		name += "/day"
	}
	return name
}

// blockOrigin is the origin every block and bucket is binned from.
const blockOrigin = `timestamptz '2026-09-01 00:00:00+00'`

// Candidate is b as a benchmark candidate.
func (b Block) Candidate() Candidate {
	width := fmt.Sprintf("interval '%d seconds'", int(b.Width.Seconds()))
	tsType, tsAgg, tsRead := "timestamptz[]", "array_agg(ts order by ts, seq)", "u.t"
	if b.Offsets {
		tsType = "int4[]"
		tsAgg = "array_agg((extract(epoch from ts - date_bin(" + width + ", ts, " + blockOrigin + ")) * 1000)::int4 order by ts, seq)"
		tsRead = "b.start + u.t * interval '1 millisecond'"
	}
	with := ""
	if b.Tuned {
		with = " with (toast_tuple_target = 128)"
	}
	setup := []string{fmt.Sprintf(`create table block (
	series_id bigint not null,
	start timestamptz not null,
	count int4 not null,
	min float8 not null,
	max float8 not null,
	sum float8 not null,
	first float8 not null,
	last float8 not null,
	vals float8[] not null,
	ts %s not null,
	primary key (series_id, start)
)%s`, tsType, with)}
	if b.Tuned {
		setup = append(setup,
			`alter table block alter column vals set compression lz4`,
			`alter table block alter column ts set compression lz4`)
	}
	unnest := `from block b, unnest(b.ts, b.vals) with ordinality as u(t, v, o)`
	return Candidate{
		Name: b.Name(), Image: ImagePlain,
		Setup: setup,
		Compress: []string{`insert into block
select series_id, date_bin(` + width + `, ts, ` + blockOrigin + `), count(*), min(value), max(value), sum(value),
	(array_agg(value order by ts, seq))[1], (array_agg(value order by ts desc, seq desc))[1],
	array_agg(value order by ts, seq), ` + tsAgg + `
from staging.sample group by 1, 2`},
		ReadRaw: `select ts, v from (select ` + tsRead + ` as ts, u.v, b.start, u.o ` + unnest + `
	where b.series_id = $1 and b.start >= date_bin(` + width + `, $2, ` + blockOrigin + `) and b.start < $3) r
where ts >= $2 and ts < $3 order by start, o`,
		// The bucket width is a multiple of the block width, so a bucket is its
		// blocks' summaries: no array is opened.
		ReadBuckets: `select date_bin($4::interval, start, ` + blockOrigin + `) as bk, sum(count)::int8, min(min), max(max), sum(sum) / sum(count)
from block where series_id = $1 and start >= $2 and start < $3 group by bk order by bk`,
		ReadAll: `select b.series_id, ` + tsRead + `, u.v ` + unnest + ` order by b.series_id, b.start, u.o`,
		Check: `select count(*) from block where count <> cardinality(vals) or count <> cardinality(ts)
	or first <> vals[1] or last <> vals[cardinality(vals)]`,
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

// Candidates is every candidate at spec's cadence: F0 to F3 as hour blocks,
// F4 on day partitions and F5 on day chunks, against raw. A sparse cadence
// (a day or longer) adds day blocks and month partitions and chunks, so the
// per-row overheads the formats carry sit side by side.
func Candidates(spec Spec) []Candidate {
	day := 24 * time.Hour
	widths := []time.Duration{time.Hour}
	if spec.Cadence >= day {
		widths = append(widths, day)
	}
	out := []Candidate{Raw(spec)}
	for _, w := range widths {
		for _, offsets := range []bool{false, true} {
			for _, tuned := range []bool{false, true} {
				out = append(out, Block{Width: w, Offsets: offsets, Tuned: tuned}.Candidate())
			}
		}
	}
	out = append(out, Deltax(spec, day, ""), TimescaleDB(spec, day, ""))
	if spec.Cadence >= day {
		out = append(out, Deltax(spec, 31*day, "/month"), TimescaleDB(spec, 31*day, "/month"))
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
