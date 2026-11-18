package storage

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// ruleHistory is what a rule's criteria read of one series beyond its newest
// sample (#1985): the series' window, oldest first, and its age in seconds.
// Each is read only for a rule whose criteria read it.
type ruleHistory struct {
	window []rules.Point
	age    float64
}

// windowBucketSamples is the most samples one bucket of a window averages:
// its newest. A window reads at most MaxWindowPoints of them per series,
// so its cost is bounded whatever the key's cadence.
const windowBucketSamples = 4

// ruleWindowSQL reads one component's series of a metric key over a window,
// observed and calculated (a command's intended row is not an observation),
// as MaxWindowPoints even buckets of the window's WindowBucket, newest
// first. Each bucket probes each series through its (series_id, ts) index
// for its newest windowBucketSamples rows and averages them, so a read
// touches at most buckets x samples rows a series: the bound is in the
// probe, not a limit applied after the rows are grouped. An empty bucket
// is no point.
const ruleWindowSQL = `with s as materialized (
		select s.id from metric_series s
		join metric_type_in_effect() t on t.id = s.metric_type_id
		where s.owner_id = $1::uuid and s.owner_kind = 'component' and t.name = $2 and s.instance = $3
			and s.provenance in ('observed', 'calculated'))
	select avg(extract(epoch from p.ts))::float8, avg(p.value)::float8
	from generate_series(1, $5::int) b(i)
	cross join lateral (
		select f.ts, f.value from s cross join lateral (
			select m.ts, m.value from metric m
			where m.series_id = s.id
				and m.ts > now() - make_interval(secs => b.i * $4::float8)
				and m.ts <= now() - make_interval(secs => (b.i - 1) * $4::float8)
			order by m.ts desc limit $6) f
		order by f.ts desc limit $6) p
	group by b.i
	order by b.i`

// ruleAgeSQL is how long one component's series of a key has run: since its
// oldest sample still kept, observed or calculated, read through each
// series' index; 0 for a series with none.
const ruleAgeSQL = `select coalesce(greatest(extract(epoch from now() - min(f.ts)), 0), 0)::float8 from metric_series s
	join metric_type_in_effect() t on t.id = s.metric_type_id
	cross join lateral (select m.ts from metric m where m.series_id = s.id order by m.ts limit 1) f
	where s.owner_id = $1::uuid and s.owner_kind = 'component' and t.name = $2 and s.instance = $3
		and s.provenance in ('observed', 'calculated')`

// processAgeSQL is how long the process reporting on one component has run:
// its newest process-uptime, plus the time since that sample; 0 for a
// component with none.
const processAgeSQL = `select coalesce((select greatest(extract(epoch from now() - f.ts) + f.value, 0) from metric_series s
	join metric_type_in_effect() t on t.id = s.metric_type_id
	cross join lateral (select m.ts, m.value from metric m where m.series_id = s.id order by m.ts desc limit 1) f
	where s.owner_id = $1::uuid and s.owner_kind = 'component' and t.name = $2 and s.instance = ''
		and s.provenance = 'observed'
	order by f.ts desc limit 1), 0)::float8`

// ruleHistory reads what a rule's criteria read of one series beyond its
// newest sample: its window over the rule's sample_window, oldest first, and
// its age. Nothing, with no query, for a rule whose criteria read neither,
// or one whose criteria do not compile (decide reports it).
func (p *PG) ruleHistory(ctx context.Context, tx pgx.Tx, r EventRule, componentID, instance string) (ruleHistory, error) {
	var h ruleHistory
	if r.SampleLane != rules.LaneMetric {
		return h, nil
	}
	j, err := p.judges.judge(r)
	if err != nil {
		return h, nil
	}
	if r.SampleWindow > 0 && j.ReadsWindow() {
		if h.window, err = ruleWindow(ctx, tx, r, componentID, instance); err != nil {
			return h, err
		}
	}
	if j.ReadsAge() {
		if h.age, err = ruleAge(ctx, tx, r.SampleKey, componentID, instance); err != nil {
			return h, fmt.Errorf("storage: rule %s age on %s: %w", r.Name, componentID, err)
		}
	}
	return h, nil
}

// ruleWindow is a rule's window on one series, oldest first.
func ruleWindow(ctx context.Context, tx pgx.Tx, r EventRule, componentID, instance string) ([]rules.Point, error) {
	bucket := max(rules.WindowBucket(r.SampleWindow).Seconds(), 1e-6)
	rows, err := tx.Query(ctx, ruleWindowSQL, componentID, r.SampleKey, instance, bucket, rules.MaxWindowPoints, windowBucketSamples)
	if err != nil {
		return nil, fmt.Errorf("storage: rule %s window on %s: %w", r.Name, componentID, err)
	}
	pts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (rules.Point, error) {
		var pt rules.Point
		err := row.Scan(&pt.At, &pt.Value)
		return pt, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: rule %s window on %s: %w", r.Name, componentID, err)
	}
	slices.Reverse(pts)
	return pts, nil
}

// ruleAge is how long a series has run, in seconds. A process's own key
// restarts with the process, so its age runs from the process's start (its
// process-uptime on the same component), not from a series that spans
// restarts; any other key's runs from its oldest kept sample.
func ruleAge(ctx context.Context, tx pgx.Tx, key, componentID, instance string) (float64, error) {
	var age float64
	var err error
	if writerclass.ProcessSelf(key) {
		err = tx.QueryRow(ctx, processAgeSQL, componentID, writerclass.ProcessUptime).Scan(&age)
	} else {
		err = tx.QueryRow(ctx, ruleAgeSQL, componentID, key, instance).Scan(&age)
	}
	return age, err
}
