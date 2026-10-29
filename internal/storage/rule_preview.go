package storage

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// The preview's bounds (#959).
const (
	// RulePreviewWindow is how far back a preview judges samples.
	RulePreviewWindow = 24 * time.Hour
	// RulePreviewRowCap bounds the window samples a preview reads, taken a
	// whole series at a time: a series that would pass it is not read, and
	// the preview says how many series it judged of how many.
	RulePreviewRowCap = 5000
	// RulePreviewSeedLookback is how far before the window a series' starting
	// state is looked for: its newest decisive sample (one that fires or
	// clears) within it. A series with none starts unknown.
	RulePreviewSeedLookback = 7 * 24 * time.Hour
	// RulePreviewSeedProbe bounds the samples one series' seed probe reads,
	// newest first, looking for a decisive one.
	RulePreviewSeedProbe = 256
)

// rulePreviewRowCap is RulePreviewRowCap; a variable only so a test can
// lower it.
var rulePreviewRowCap = RulePreviewRowCap

// RulePreviewSpec is a candidate rule: what it reads, what it watches, and
// its criteria. Sustains are not part of it: a preview ignores them.
type RulePreviewSpec struct {
	SampleKey string
	Selector  EventRuleSelector
	Fire      rules.Criteria
	Clear     *rules.Criteria
}

// RulePreviewSeries is a series the candidate holds in force now (since the
// fire edge in the window that brought it in, or nil when it was in force
// before the window began), or one whose starting state is unknown.
type RulePreviewSeries struct {
	ComponentID, ComponentName, Instance string
	Since                                *time.Time
}

// RulePreviewEdge is an edge the candidate would have written: fire or
// clear, on a series, at the sample that moved it.
type RulePreviewEdge struct {
	ComponentID, ComponentName, Instance, Kind string
	At                                         time.Time
}

// RulePreview is what a candidate rule would have done.
type RulePreview struct {
	InForce []RulePreviewSeries
	Edges   []RulePreviewEdge
	// StateUnknown are the series with no decisive sample within the seed
	// lookback: their first decisive sample in the window set their state
	// without an edge, since whether it moved anything is not known.
	StateUnknown []RulePreviewSeries
	Window       time.Duration
	Since, Until time.Time
	RowCap       int
	// SeriesJudged of SeriesTotal series were judged whole; Truncated when
	// the cap stopped the preview short of the rest.
	SeriesJudged, SeriesTotal int
	Truncated                 bool
	SustainsIgnored           bool
}

// previewCandidate is a component the candidate selects, in scope.
type previewCandidate struct{ id, name, typeName string }

// previewSample is one sample a preview judges.
type previewSample struct {
	id    int64
	ts    time.Time
	value any
}

// PreviewEventRule judges a candidate rule without writing anything (#959),
// in a read-only transaction, the way the ingest would have judged its
// samples with no sustains. Its reads are driven from the components the
// candidate selects within the visible scope (the selection is SQL, so an
// unselected row costs nothing): each one's instances of the key by a
// loose index skip, then per series an index probe for its starting state
// (the newest decisive sample within RulePreviewSeedLookback before the
// window; none, and the series is reported unknown) and a bounded probe of
// its window. A window sample older than one that arrived before it is
// history, as the ingest's stale check makes it, and decides nothing. A fire
// out of force is a fire edge, a clear in force a clear edge; the series in
// force at the end are in force now. The criteria compile once and every
// sample runs under the criteria budget (a sample it cannot judge holds).
// The window reads stop, a whole series at a time, at the row cap.
func (p *PG) PreviewEventRule(ctx context.Context, spec RulePreviewSpec, visible scope.Set) (*RulePreview, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("storage: begin rule preview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	r := EventRule{SampleKey: spec.SampleKey, Selector: spec.Selector, FireCriteria: spec.Fire, ClearCriteria: spec.Clear}
	if err := resolveRuleLane(ctx, tx, &r); err != nil {
		return nil, err
	}
	judge, err := rules.Compile(r.SampleLane, r.dataType, spec.Fire, spec.Clear)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEventRuleInvalid, err)
	}
	ids, err := previewSelectorIDs(ctx, tx, spec.Selector, visible)
	if err != nil {
		return nil, err
	}
	_, typeTable, _ := laneTables(r.SampleLane)
	var typeID string
	if err := tx.QueryRow(ctx, `select id::text from `+typeTable+` where name = $1`, r.SampleKey).Scan(&typeID); err != nil {
		return nil, fmt.Errorf("storage: rule preview key: %w", err)
	}
	until, err := dbNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	since := until.Add(-RulePreviewWindow)
	out := &RulePreview{Window: RulePreviewWindow, Since: since, Until: until, RowCap: rulePreviewRowCap, SustainsIgnored: true,
		InForce: []RulePreviewSeries{}, Edges: []RulePreviewEdge{}, StateUnknown: []RulePreviewSeries{}}

	cands, err := previewCandidates(ctx, tx, spec.Selector, ids, visible)
	if err != nil {
		return nil, err
	}
	byID := map[string]previewCandidate{}
	candIDs := make([]string, 0, len(cands))
	for _, c := range cands {
		byID[c.id] = c
		candIDs = append(candIDs, c.id)
	}
	type series struct{ component, instance string }
	var all []series
	if len(candIDs) > 0 {
		rows, err := tx.Query(ctx, previewInstancesSQL(r.SampleLane), typeID, candIDs)
		if err != nil {
			return nil, fmt.Errorf("storage: rule preview series: %w", err)
		}
		for rows.Next() {
			var k series
			if err := rows.Scan(&k.component, &k.instance); err != nil {
				rows.Close()
				return nil, err
			}
			all = append(all, k)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out.SeriesTotal = len(all)

	read := 0
	for _, k := range all {
		c := byID[k.component]
		sample := func(v any) rules.Sample {
			return rules.Sample{Value: v, Key: r.SampleKey, Instance: k.instance, Component: rules.Component{Name: c.name, Type: c.typeName}}
		}
		decide := func(v any) rules.Action {
			a, err := judge.Decide(sample(v))
			if err != nil {
				return rules.Hold
			}
			return a
		}
		window, err := previewProbe(ctx, tx, previewWindowSQL(r.SampleLane), k.component, typeID, k.instance, since, rulePreviewRowCap-read+1)
		if err != nil {
			return nil, err
		}
		if read+len(window) > rulePreviewRowCap {
			out.Truncated = true
			break
		}
		read += len(window)
		seeds, err := previewProbe(ctx, tx, previewSeedSQL(r.SampleLane), k.component, typeID, k.instance, since, since.Add(-RulePreviewSeedLookback), RulePreviewSeedProbe)
		if err != nil {
			return nil, err
		}
		known, inForce := false, false
		for _, s := range seeds { // newest first
			switch decide(s.value) {
			case rules.Fire:
				known, inForce = true, true
			case rules.Clear:
				known = true
			default:
				continue
			}
			break
		}
		if !known {
			out.StateUnknown = append(out.StateUnknown, RulePreviewSeries{ComponentID: k.component, ComponentName: c.name, Instance: k.instance})
		}
		var firedAt *time.Time
		for _, s := range judgedInArrival(window) {
			a := decide(s.value)
			switch {
			case a == rules.Hold:
				continue
			case !known:
				known, inForce = true, a == rules.Fire
				continue
			case a == rules.Fire && !inForce:
				inForce = true
				at := s.ts
				firedAt = &at
				out.Edges = append(out.Edges, RulePreviewEdge{ComponentID: k.component, ComponentName: c.name, Instance: k.instance, Kind: "fire", At: s.ts})
			case a == rules.Clear && inForce:
				inForce, firedAt = false, nil
				out.Edges = append(out.Edges, RulePreviewEdge{ComponentID: k.component, ComponentName: c.name, Instance: k.instance, Kind: "clear", At: s.ts})
			}
		}
		if inForce {
			out.InForce = append(out.InForce, RulePreviewSeries{ComponentID: k.component, ComponentName: c.name, Instance: k.instance, Since: firedAt})
		}
		out.SeriesJudged++
	}
	return out, nil
}

// judgedInArrival is the window samples the ingest would have judged, in the
// order it judged them: a sample is history (decides nothing) when one that
// arrived before it (a lower id) is newer (a later ts), which is the ingest's
// stale check. The rest arrive in time order, so they are returned by time.
func judgedInArrival(window []previewSample) []previewSample {
	sorted := append([]previewSample(nil), window...)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].ts.Equal(sorted[j].ts) {
			return sorted[i].ts.Before(sorted[j].ts)
		}
		return sorted[i].id < sorted[j].id
	})
	keep := make([]bool, len(sorted))
	minAbove := int64(math.MaxInt64) // the lowest id among strictly newer samples
	for i := len(sorted) - 1; i >= 0; {
		j := i
		groupMin := int64(math.MaxInt64)
		for j >= 0 && sorted[j].ts.Equal(sorted[i].ts) {
			keep[j] = sorted[j].id < minAbove
			groupMin = min(groupMin, sorted[j].id)
			j--
		}
		minAbove = min(minAbove, groupMin)
		i = j
	}
	out := make([]previewSample, 0, len(sorted))
	for i, s := range sorted {
		if keep[i] {
			out = append(out, s)
		}
	}
	return out
}

// previewSelectorIDs resolves the selector's components within the caller's
// read scope (#627: scope decides before ambiguity does), so a component the
// caller cannot read is refused exactly as one that does not exist.
func previewSelectorIDs(ctx context.Context, q querier, sel EventRuleSelector, visible scope.Set) ([]string, error) {
	for _, t := range sel.ComponentTypes {
		var ok bool
		if err := q.QueryRow(ctx, `select exists (select 1 from component_type where name = $1)`, t).Scan(&ok); err != nil {
			return nil, fmt.Errorf("storage: check rule component type: %w", err)
		}
		if !ok {
			return nil, invalidRule("component type %q does not exist", t)
		}
	}
	ids := make([]string, 0, len(sel.Components))
	for _, ref := range sel.Components {
		c, err := scopedByNameInScope(ctx, q, componentConfig, ref, "component", visible)
		if err != nil {
			return nil, invalidRule("component %q: %v", ref, ErrComponentNotFound)
		}
		ids = append(ids, c.ID)
	}
	return ids, nil
}

// previewCandidates is the components a candidate selects within the visible
// scope, in one statement: every component when its selector is empty, else
// those it names and those whose type falls within a type it names.
func previewCandidates(ctx context.Context, q querier, sel EventRuleSelector, ids []string, visible scope.Set) ([]previewCandidate, error) {
	f := newReadFilter(componentTable, visible, 4)
	sub := `sub(id) as (select id from component_type where name = any($2)
			union select ct.id from component_type ct join sub on ct.parent_id = sub.id)`
	with := "with recursive " + sub + " "
	if f.With != "" {
		with = f.With + ", " + sub + " "
	}
	everyone := len(sel.Components) == 0 && len(sel.ComponentTypes) == 0
	rows, err := q.Query(ctx, with+`select c.id::text, c.name, coalesce(ct.name, '')
		from component c left join product pr on pr.id = c.product_id left join component_type ct on ct.id = pr.component_type_id
		where `+f.Pred("c.id")+` and ($1 or c.id = any($3::uuid[]) or pr.component_type_id in (select id from sub))
		order by c.id`, append([]any{everyone, sel.ComponentTypes, ids}, f.Args...)...)
	if err != nil {
		return nil, fmt.Errorf("storage: rule preview selection: %w", err)
	}
	defer rows.Close()
	var out []previewCandidate
	for rows.Next() {
		var c previewCandidate
		if err := rows.Scan(&c.id, &c.name, &c.typeName); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// previewInstancesSQL lists each candidate component's instances of a key
// ($1 the type id, $2 the component ids) by a loose index skip on the
// series index: one probe per instance, never a scan of the key's samples.
func previewInstancesSQL(lane rules.Lane) string {
	if lane == rules.LaneMetric {
		// The candidates' component series of the key that hold a sample.
		return `select distinct s.owner_id::text, s.instance from metric_series s
			where s.owner_id = any($2::uuid[]) and s.owner_kind = 'component' and s.metric_type_id = $1::uuid
			  and s.provenance in ('observed', 'calculated') and exists (select 1 from metric m where m.series_id = s.id)
			order by 1, 2`
	}
	table, typeTable, _ := laneTables(lane)
	return fmt.Sprintf(`with recursive inst(component_id, instance) as (
			select c.id, (select s.instance from %[1]s s where s.component_id = c.id and s.%[2]s_id = $1::uuid
				order by s.instance limit 1)
			from unnest($2::uuid[]) as c(id)
			union all
			select i.component_id, (select s.instance from %[1]s s where s.component_id = i.component_id and s.%[2]s_id = $1::uuid
				and s.instance > i.instance order by s.instance limit 1)
			from inst i where i.instance is not null
		)
		select component_id::text, instance from inst where instance is not null order by component_id, instance`, table, typeTable)
}

// previewSeedSQL reads a series' samples before the window, newest first,
// within the lookback ($1 component, $2 type id, $3 instance, $4 since, $5
// the lookback's start, $6 the probe's bound): an index probe.
func previewSeedSQL(lane rules.Lane) string {
	if lane == rules.LaneMetric {
		return `select m.id, m.ts, m.value from metric_series s join metric m on m.series_id = s.id
		where s.owner_id = $1::uuid and s.owner_kind = 'component' and s.metric_type_id = $2::uuid and s.instance = $3
		  and s.provenance in ('observed', 'calculated') and m.ts < $4 and m.ts >= $5
		order by m.ts desc limit $6`
	}
	table, typeTable, value := laneTables(lane)
	return fmt.Sprintf(`select s.id, s.ts, %[3]s from %[1]s s
		where s.component_id = $1::uuid and s.%[2]s_id = $2::uuid and s.instance = $3 and s.ts < $4 and s.ts >= $5
		order by s.ts desc limit $6`, table, typeTable, value)
}

// previewWindowSQL reads a series' samples in the window ($1 component, $2
// type id, $3 instance, $4 since, $5 the bound): an index probe.
func previewWindowSQL(lane rules.Lane) string {
	if lane == rules.LaneMetric {
		return `select m.id, m.ts, m.value from metric_series s join metric m on m.series_id = s.id
		where s.owner_id = $1::uuid and s.owner_kind = 'component' and s.metric_type_id = $2::uuid and s.instance = $3
		  and s.provenance in ('observed', 'calculated') and m.ts >= $4
		order by m.ts desc limit $5`
	}
	table, typeTable, value := laneTables(lane)
	return fmt.Sprintf(`select s.id, s.ts, %[3]s from %[1]s s
		where s.component_id = $1::uuid and s.%[2]s_id = $2::uuid and s.instance = $3 and s.ts >= $4
		order by s.ts desc limit $5`, table, typeTable, value)
}

// previewProbe runs one per-series probe.
func previewProbe(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]previewSample, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: rule preview probe: %w", err)
	}
	defer rows.Close()
	var out []previewSample
	for rows.Next() {
		var s previewSample
		if err := rows.Scan(&s.id, &s.ts, &s.value); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
