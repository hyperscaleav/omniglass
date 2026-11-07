package compressbench

import (
	"fmt"
	"strings"
	"time"
)

// This file is the summary-free block (#1522): F2's offsets and values with no
// summary columns, one span a series-day chosen by the day's own sample count,
// and the metric_trend rollup (ADR-1276) measured beside it.

const (
	// PlainHourMin is the average samples an hour at which a series-day is cut
	// into hour blocks: 170 doubles of values and int4 offsets cross Postgres's
	// 2 KB compression threshold.
	PlainHourMin = 170
	// PlainDayMin is the samples a day at which a series-day is one day block.
	// A sparser day waits for its ISO week's block.
	PlainDayMin = 170
)

// DaySpan is the span one series-day's samples are stored at: hour, day or
// week blocks, or raw rows when its week is under the week minimum.
type DaySpan struct {
	SeriesID int64
	Day      time.Time
	Span     string
	Count    int
}

// isoWeek is the Monday that starts t's ISO week.
func isoWeek(day time.Time) time.Time { return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7)) }

// PlainSpans is the span rule over samples, which are in series then
// timestamp order: each series-day by its count, and the days too sparse for a
// day block of their own as one week block a series an ISO week, or raw when
// those days hold fewer than weekMin samples together.
func PlainSpans(samples []Sample, weekMin int) []DaySpan {
	var out []DaySpan
	for _, s := range samples {
		day := s.TS.UTC().Truncate(24 * time.Hour)
		if n := len(out); n > 0 && out[n-1].SeriesID == s.SeriesID && out[n-1].Day.Equal(day) {
			out[n-1].Count++
			continue
		}
		out = append(out, DaySpan{SeriesID: s.SeriesID, Day: day, Count: 1})
	}
	type week struct {
		series int64
		start  time.Time
	}
	waiting := map[week]int{}
	for i, d := range out {
		switch {
		case d.Count >= 24*PlainHourMin:
			out[i].Span = "hour"
		case d.Count >= PlainDayMin:
			out[i].Span = "day"
		default:
			waiting[week{d.SeriesID, isoWeek(d.Day)}] += d.Count
		}
	}
	for i, d := range out {
		if d.Span != "" {
			continue
		}
		out[i].Span = "week"
		if waiting[week{d.SeriesID, isoWeek(d.Day)}] < weekMin {
			out[i].Span = "raw"
		}
	}
	return out
}

// Plain is the summary-free block table, metric_block. WeekMin is the samples
// a series' waiting days must hold in an ISO week to make a week block; under
// it they stay in the product's narrow table, which exists only when WeekMin
// is over 1. Trend adds metric_trend as a rollup of its own.
type Plain struct {
	WeekMin int
	Trend   bool
}

const plainDDL = `create table metric_block (
	series_id bigint not null,
	span_start timestamptz not null,
	span int4 not null,
	n int4 not null,
	offset_ms int4[] not null,
	value float8[] not null,
	primary key (series_id, span_start, span)
)`

// plainStage is every staging sample with the span its series-day takes, its
// block's start, and its block's count. The counts are the rule PlainSpans
// states: 24 * PlainHourMin a day for hours, PlainDayMin for a day.
var plainStage = fmt.Sprintf(`create table staging.plain as
with d as (select series_id, date_trunc('day', ts) as day, count(*) as n from staging.sample group by 1, 2),
s as (select s.seq, s.series_id, s.ts, s.value,
	case when d.n >= %d then 3600 when d.n >= %d then 86400 else 604800 end as span
from staging.sample s join d on d.series_id = s.series_id and d.day = date_trunc('day', s.ts)),
t as (select *, case span when 3600 then date_trunc('hour', ts) when 86400 then date_trunc('day', ts) else date_trunc('week', ts) end as span_start from s)
select *, count(*) over (partition by series_id, span_start, span) as blk_n from t`, 24*PlainHourMin, PlainDayMin)

// plainTS is a block sample's instant.
const plainTS = `b.span_start + u.o * interval '1 millisecond'`

// plainSamples is every block sample as (series_id, ts, v, o), o its landing
// order within its block, filtered by where; with the narrow table, its rows
// too, o being their id.
func (p Plain) plainSamples(blockWhere, rawWhere string) string {
	q := `select b.series_id, ` + plainTS + ` as ts, u.v, u.ord as o
	from metric_block b, unnest(b.offset_ms, b.value) with ordinality as u(o, v, ord)` + blockWhere
	if p.raw() {
		q += `
	union all select series_id, ts, value, id from metric` + rawWhere
	}
	return q
}

func (p Plain) raw() bool { return p.WeekMin > 1 }

// Candidate is p as a benchmark candidate, F2p.
func (p Plain) Candidate(spec Spec) Candidate {
	setup := []string{plainDDL}
	compress := []string{plainStage, fmt.Sprintf(`insert into metric_block (series_id, span_start, span, n, offset_ms, value)
select series_id, span_start, span, count(*)::int4,
	array_agg((extract(epoch from ts - span_start) * 1000)::int4 order by ts, seq),
	array_agg(value order by ts, seq)
from staging.plain where span < 604800 or blk_n >= %d group by 1, 2, 3`, p.WeekMin)}
	if p.raw() {
		setup = append(setup, productMetric(spec)...)
		compress = append(compress, fmt.Sprintf(`insert into metric (series_id, ts, value)
select series_id, ts, value from staging.plain where span = 604800 and blk_n < %d order by seq`, p.WeekMin))
	}
	compress = append(compress, `drop table staging.plain`)
	// A block that starts up to a week before the window can hold its samples.
	window := `
	where b.series_id = $1 and b.span_start > $2::timestamptz - interval '7 days' and b.span_start < $3`
	rawWindow := `
	where series_id = $1 and ts >= $2 and ts < $3`
	c := Candidate{
		Name: "F2p", Image: ImagePlain,
		Setup:    setup,
		Compress: compress,
		ReadRaw: `select ts, v from (` + p.plainSamples(window, rawWindow) + `) x
where ts >= $2 and ts < $3 order by ts, o`,
		ReadBuckets: `select date_bin($4::interval, ts, ` + blockOrigin + `) as bk, count(*), min(v), max(v), avg(v) from (` +
			p.plainSamples(window, rawWindow) + `) x
where ts >= $2 and ts < $3 group by bk order by bk`,
		ReadAll: `select series_id, ts, v from (` + p.plainSamples("", "") + `) x order by series_id, ts, o`,
		Check: `select count(*) from metric_block where n <> cardinality(offset_ms) or n <> cardinality(value)
	or span not in (3600, 86400, 604800)
	or span_start <> case span when 3600 then date_trunc('hour', span_start) when 86400 then date_trunc('day', span_start) else date_trunc('week', span_start) end
	or exists (select 1 from unnest(offset_ms) o where o < 0 or o >= span * 1000)`,
		CompressedRows: `select coalesce(avg((pg_column_compression(value) is not null)::int), 0)::float8 from metric_block`,
		DaySpans: `select series_id, date_trunc('day', ts) as day, span, count(*)::int8 from (
	select b.series_id, ` + plainTS + ` as ts, case b.span when 3600 then 'hour' when 86400 then 'day' else 'week' end as span
	from metric_block b, unnest(b.offset_ms) as u(o)` + p.rawSpans() + `) x
group by 1, 2, 3 order by 1, 2, 3`,
		WantDaySpans: func(s []Sample) []DaySpan { return PlainSpans(s, p.WeekMin) },
	}
	if p.Trend {
		c.Cagg = trend("F2p trend")
	}
	return c
}

func (p Plain) rawSpans() string {
	if !p.raw() {
		return ""
	}
	return `
	union all select series_id, ts, 'raw' from metric`
}

// trend is metric_trend (migration 20261029171258_metric_trend) as a rollup
// beside a candidate: one row a series-hour. Its foreign key to metric_series
// is left out, since the benchmark has no series table and the key adds no
// bytes.
func trend(name string) *Cagg {
	return &Cagg{
		Name: name,
		Setup: []string{`create table metric_trend (
	series_id bigint not null,
	hour timestamptz not null,
	count integer not null,
	min double precision not null,
	max double precision not null,
	sum double precision not null,
	first double precision not null,
	last double precision not null,
	constraint metric_trend_pkey primary key (series_id, hour),
	constraint metric_trend_count_check check (count > 0)
)`, `create index metric_trend_hour_brin on metric_trend using brin (hour)`},
		Refresh: []string{`insert into metric_trend (series_id, hour, count, min, max, sum, first, last)
select series_id, date_trunc('hour', ts), count(*), min(value), max(value), sum(value),
	(array_agg(value order by ts, seq))[1], (array_agg(value order by ts desc, seq desc))[1]
from staging.sample group by 1, 2`},
		ReadBuckets: `select date_bin($4::interval, hour, ` + blockOrigin + `) as bk, sum(count)::int8, min(min), max(max), sum(sum) / sum(count)
from metric_trend where series_id = $1 and hour >= $2 and hour < $3 group by bk order by bk`,
		HourlyAll: `select series_id, hour, count::int8, min, max, sum, first, last from metric_trend order by series_id, hour`,
	}
}

// plainSpan is the span F2p's rule gives a series polled at cadence all day.
func plainSpan(cadence time.Duration) string {
	switch perDay := int(24 * time.Hour / cadence); {
	case perDay >= 24*PlainHourMin:
		return "hour"
	case perDay >= PlainDayMin:
		return "day"
	}
	return "week"
}

// PlainCandidates is #1522's comparison at one cadence: raw, #1518's F2 at the
// span its rule picked (with hourly summaries; none at 24 h, which it left
// raw), and F2p with metric_trend beside it.
func PlainCandidates(spec Spec) []Candidate {
	out := []Candidate{Raw(spec)}
	if spec.Cadence < 24*time.Hour {
		out = append(out, Block{Span: plainSpan(spec.Cadence), Offsets: true}.Candidate())
	}
	return append(out, Plain{WeekMin: 1, Trend: true}.Candidate(spec))
}

// SweepWeeks is the week sweep's span, in ISO weeks.
const SweepWeeks = 4

// SweepSpec is the week sweep's dataset at perWeek samples a series a week:
// SweepWeeks whole ISO weeks, so every block is a full week.
func SweepSpec(perWeek, series int, seed uint64) Spec {
	return Spec{Cadence: 7 * 24 * time.Hour / time.Duration(perWeek), Series: series, Span: SweepWeeks * 7 * 24 * time.Hour,
		Seed: seed, Start: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), Name: fmt.Sprintf("%d/wk", perWeek)}
}

// SweepCandidates is the week sweep: raw against F2p, every block a week.
func SweepCandidates(spec Spec) []Candidate {
	return []Candidate{Raw(spec), Plain{WeekMin: 1}.Candidate(spec)}
}

// SweepRow is one count of the week sweep: bytes a sample raw and as a week
// block.
type SweepRow struct {
	PerWeek int
	Raw     float64
	Plain   float64
}

// Sweep is r's week sweep, one row a spec measured both ways.
func Sweep(r Report) []SweepRow {
	var out []SweepRow
	for _, spec := range r.Specs {
		raw, ok1 := r.cell("raw", spec.Label())
		pl, ok2 := r.cell("F2p", spec.Label())
		if !ok1 || !ok2 || raw.BytesPerSample.N == 0 || pl.BytesPerSample.N == 0 {
			continue
		}
		out = append(out, SweepRow{int(7 * 24 * time.Hour / spec.Cadence), raw.BytesPerSample.Median, pl.BytesPerSample.Median})
	}
	return out
}

// WeekMinimum is the smallest count a week from which every count measured
// stores a week block in fewer bytes than raw rows.
func WeekMinimum(rows []SweepRow) (int, bool) {
	k, ok := 0, false
	for i := len(rows) - 1; i >= 0 && rows[i].Plain < rows[i].Raw; i-- {
		k, ok = rows[i].PerWeek, true
	}
	return k, ok
}

// SweepTable is the week sweep as markdown, with its minimum.
func SweepTable(rows []SweepRow) string {
	var b strings.Builder
	b.WriteString("| samples a week | cadence | raw B/sample | F2p week block B/sample | block / raw |\n|--:|---|--:|--:|--:|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %d | %s | %.2f | %.2f | %.2f |\n", r.PerWeek, (7 * 24 * time.Hour / time.Duration(r.PerWeek)).String(), r.Raw, r.Plain, r.Plain/r.Raw)
	}
	if k, ok := WeekMinimum(rows); ok {
		fmt.Fprintf(&b, "\nFrom %d samples a week, a week block costs less than raw rows.\n", k)
	} else {
		b.WriteString("\nNo count measured makes a week block cheaper than raw rows.\n")
	}
	return b.String()
}

// MixedSpec is #1522's cadence change over one ISO week from monday: series 1
// polls 60 s and turns to 30 s at Wednesday noon, series 2 polls 1 h and turns
// to 30 s at Thursday noon, and series 3 reports three times a week. Cadence
// is the fastest phase's, for the reads; the phases make the samples.
func MixedSpec(seed uint64) Spec {
	h := time.Hour
	week := 7 * 24 * h
	return Spec{Name: "mixed", Cadence: 30 * time.Second, Series: 3, Span: week, Seed: seed,
		Start: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), Phases: [][]Phase{
			{{From: 0, To: 60 * h, Cadence: time.Minute}, {From: 60 * h, To: week, Cadence: 30 * time.Second}},
			{{From: 0, To: 84 * h, Cadence: h}, {From: 84 * h, To: week, Cadence: 30 * time.Second}},
			{{From: 0, To: week, Cadence: 56 * h}},
		}}
}

// MixedTable is each series-day of a mixed run's F2p cell as markdown.
func MixedTable(c Cell) string {
	var b strings.Builder
	b.WriteString("| series | day | span | samples |\n|--:|---|---|--:|\n")
	for _, d := range c.DaySpans {
		fmt.Fprintf(&b, "| %d | %s | %s | %d |\n", d.SeriesID, d.Day.Format("Mon 2006-01-02"), d.Span, d.Count)
	}
	return b.String()
}

// plainRef is the bytes a sample #1522 compares F2p with at each cadence: F2
// at its span as #1518 measured it, and the draft ADR's estimate on #1265 of
// that block with its summaries taken out.
var plainRef = map[string][2]float64{
	"10s": {7.78, 7.7}, "30s": {7.59, 7.2}, "60s": {8.16, 7.4}, "5m": {10.73, 8.2}, "1h": {26.04, 10.4},
}

// PlainTable is #1522's comparison as markdown: per cadence, the span F2p's
// rule gives, bytes a sample for F2 at that span and for F2p beside #1518's
// figure and the draft's estimate, what metric_trend adds, the 30 d bucketed
// read from the blocks and from the trend, and F2p's rows compressed.
func PlainTable(r Report) string {
	var b strings.Builder
	b.WriteString("| cadence | span | F2 B/sample | F2p B/sample | #1518 F2 | draft | trend B/sample | F2p 30 d read ms | trend 30 d read ms | F2p rows compressed |\n")
	b.WriteString("|---|---|--:|--:|--:|--:|--:|--:|--:|--:|\n")
	bps := func(name, cad string) string {
		if c, ok := r.cell(name, cad); ok && c.BytesPerSample.N > 0 {
			return fmt.Sprintf("%.2f", c.BytesPerSample.Median)
		}
		return "n/a"
	}
	read := func(name, cad string) string {
		c, _ := r.cell(name, cad)
		return stat(c.ReadBuckets, 2)
	}
	for _, spec := range r.Specs {
		cad, span := spec.Label(), plainSpan(spec.Cadence)
		f2 := "n/a"
		if spec.Cadence < 24*time.Hour {
			name := "F2"
			if span != "hour" {
				name += "/" + span
			}
			f2 = bps(name, cad)
		}
		ref, draft := "n/a", "n/a"
		if v, ok := plainRef[cad]; ok {
			ref, draft = fmt.Sprintf("%.2f", v[0]), fmt.Sprintf("%.1f", v[1])
		}
		pl, _ := r.cell("F2p", cad)
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", cad, span, f2, bps("F2p", cad), ref, draft,
			bps("F2p trend", cad), read("F2p", cad), read("F2p trend", cad), pct(pl.CompressedRows))
	}
	return b.String()
}
