package storage

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

// ruleWindowSQL reads one component's series of a metric key over a window,
// observed and calculated (a command's intended row is not an observation),
// averaged into even buckets of the window's WindowBucket and newest first,
// so a limit of MaxWindowPoints keeps the newest buckets through the
// series' (series_id, ts) index.
const ruleWindowSQL = `select avg(extract(epoch from m.ts))::float8, avg(m.value)::float8 from metric_series s
	join metric_type_in_effect() t on t.id = s.metric_type_id
	join metric m on m.series_id = s.id
	where s.owner_id = $1::uuid and s.owner_kind = 'component' and t.name = $2 and s.instance = $3
		and s.provenance in ('observed', 'calculated')
		and m.ts > now() - make_interval(secs => $4::float8)
	group by floor(extract(epoch from m.ts) / $5::float8)
	order by 1 desc limit $6`

// ruleWindow is the window a rule's criteria read on one series (#1985):
// its points over the rule's sample_window, oldest first. Nil, with no
// query, for a rule whose criteria do not read window (every rule without a
// sample_window), or one whose criteria do not compile (decide reports it).
func (p *PG) ruleWindow(ctx context.Context, tx pgx.Tx, r EventRule, componentID, instance string) ([]rules.Point, error) {
	if r.SampleWindow <= 0 || r.SampleLane != rules.LaneMetric {
		return nil, nil
	}
	j, err := p.judges.judge(r)
	if err != nil || !j.ReadsWindow() {
		return nil, nil
	}
	bucket := max(rules.WindowBucket(r.SampleWindow).Seconds(), 1e-6)
	rows, err := tx.Query(ctx, ruleWindowSQL, componentID, r.SampleKey, instance, r.SampleWindow.Seconds(), bucket, rules.MaxWindowPoints)
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
