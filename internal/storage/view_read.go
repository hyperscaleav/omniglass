package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/tail"
)

// The views read (#1830, ADR-1830): one scoped binding (a primitive, an
// entity selector, keys, a range, an optional step) answered as typed
// history from one snapshot, with the tail cursor that snapshot was taken
// at. Every binding of a request reads inside one REPEATABLE READ
// transaction opened under the tail's mark, so what a reader then follows
// on the tail starts exactly where its history ends.

// ViewFilter narrows a kind selector. Each field is optional; the ones a kind
// does not carry are refused by the caller's validation.
type ViewFilter struct {
	// System selects the components that are members of the named system.
	System string
	// Product selects the components of the named product.
	Product string
	// SystemType selects the systems of the named system type.
	SystemType string
}

// ViewBinding is one binding as the Gateway reads it: the caller has
// validated its shape and resolved Read, the binding kind's read scope.
type ViewBinding struct {
	Primitive tail.Lane
	// Kind is the entity kind the selector names: component or system.
	Kind string
	// Refs are literal entity references (name or id); empty selects by
	// Kind and Filter.
	Refs   []string
	Filter ViewFilter
	// Keys are the metric, property or event type names read; a log
	// binding carries none.
	Keys []string
	// The range: an absolute From (and To, zero for now), else the Window
	// before now.
	From, To time.Time
	Window   time.Duration
	// Step buckets a metric or property read; zero reads the raw points.
	Step time.Duration
	// Limit and Page page an event or log read, newest first.
	Limit int
	Page  string
	Logs  LogFilter
	Read  scope.Set
}

// ViewEntity is one entity a binding's selector resolved to.
type ViewEntity struct {
	ID, Name, Kind string
}

// ViewPoint is one raw metric sample.
type ViewPoint struct {
	TS    time.Time
	Value float64
}

// ViewMetricSeries is one metric series of a resolved entity: one key, one
// instance, its raw points or its bucket grid.
type ViewMetricSeries struct {
	EntityID, Key, Instance string
	Points                  []ViewPoint
	Buckets                 []MetricBucket
}

// ViewPropertyPoint is one property change.
type ViewPropertyPoint struct {
	TS    time.Time
	Value string
}

// ViewPropertySeries mirrors ViewMetricSeries over the property lane, with
// the value in force when the range opens.
type ViewPropertySeries struct {
	EntityID, Key, Instance string
	CarriedIn               *string
	Points                  []ViewPropertyPoint
	Buckets                 []PropertyBucket
}

// ViewResult is one binding's history.
type ViewResult struct {
	Entities []ViewEntity
	// From and To are the range read; CoveredFrom is the earliest instant
	// the read could hold data for (the retention floor, or the trend
	// floor when hourly trends answered the older part).
	From, To, CoveredFrom time.Time
	Metric                []ViewMetricSeries
	Property              []ViewPropertySeries
	Events                []Event
	Logs                  []LogLine
	// NextPage pages an event or log read further back; empty at the end.
	NextPage string
}

// ViewSnapshot is every binding's history and the tail cursor it was read
// at: the tail after Cursor is exactly what the history does not hold.
type ViewSnapshot struct {
	Cursor  string
	Now     time.Time
	Results []ViewResult
}

// The views read's bounds: a selector resolving to more entities, or a raw
// read holding more points, is refused rather than truncated, so a reader
// never mistakes a cut for the whole.
const (
	ViewMaxEntities = 200
	ViewMaxPoints   = 20000
	ViewMaxBuckets  = 5000
	ViewMaxPage     = 1000
	viewDefaultPage = 100
)

var (
	// ErrViewEntityNotFound is a literal ref that is absent or outside the
	// caller's read scope: the same non-disclosing answer for both.
	ErrViewEntityNotFound = errors.New("storage: view entity not found")
	// ErrViewTooLarge is a binding whose answer would pass a bound.
	ErrViewTooLarge = errors.New("storage: view too large")
	// ErrViewPage is a page cursor that does not parse.
	ErrViewPage = errors.New("storage: malformed view page")
)

// ViewTail is the tail the views watch follows.
func (p *PG) ViewTail() *tail.Hub { return p.tail }

// ReadViews reads every binding inside one snapshot taken under the tail's
// mark (ADR-1830).
func (p *PG) ReadViews(ctx context.Context, bindings []ViewBinding) (ViewSnapshot, error) {
	var out ViewSnapshot
	var tx pgx.Tx
	cursor, err := p.tail.Mark(func() error {
		var err error
		tx, err = p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return fmt.Errorf("storage: begin view read: %w", err)
		}
		// The first statement takes a REPEATABLE READ transaction's
		// snapshot: inside the mark, so no commit lands between them.
		return tx.QueryRow(ctx, `select now()`).Scan(&out.Now)
	})
	if tx != nil {
		defer func() { _ = tx.Rollback(ctx) }()
	}
	if err != nil {
		return out, err
	}
	out.Cursor = cursor
	s, err := p.platformSettings(ctx, tx)
	if err != nil {
		return out, err
	}
	out.Results = make([]ViewResult, 0, len(bindings))
	for _, b := range bindings {
		r, err := p.readView(ctx, tx, b, out.Now, s.Retention.MetricDays, s.Retention.PropertyDays, s.Retention.TrendDays)
		if err != nil {
			return out, err
		}
		out.Results = append(out.Results, r)
	}
	return out, nil
}

// ResolveViewEntities resolves a binding's selector alone, in scope: what a
// watch matches its tail rows against.
func (p *PG) ResolveViewEntities(ctx context.Context, b ViewBinding) ([]ViewEntity, error) {
	return p.resolveViewEntities(ctx, p.pool, b)
}

// ViewRange is a binding's range at now: the absolute bounds as given, else
// the window before now.
func ViewRange(b ViewBinding, now time.Time) (from, to time.Time) {
	to = now
	if !b.To.IsZero() {
		to = b.To
	}
	if !b.From.IsZero() {
		return b.From, to
	}
	return to.Add(-b.Window), to
}

func (p *PG) readView(ctx context.Context, q querier, b ViewBinding, now time.Time, metricDays, propertyDays, trendDays int) (ViewResult, error) {
	var r ViewResult
	ents, err := p.resolveViewEntities(ctx, q, b)
	if err != nil {
		return r, err
	}
	r.Entities = ents
	r.From, r.To = ViewRange(b, now)
	r.CoveredFrom = r.From
	ids := make([]string, len(ents))
	for i, e := range ents {
		ids[i] = e.ID
	}
	switch b.Primitive {
	case tail.LaneMetric:
		err = p.viewMetric(ctx, q, b, ids, now, metricDays, trendDays, &r)
	case tail.LaneProperty:
		err = p.viewProperty(ctx, q, b, ids, now, propertyDays, &r)
	case tail.LaneEvent:
		err = p.viewEvents(ctx, q, b, ids, &r)
	case tail.LaneLog:
		err = p.viewLogs(ctx, q, b, ids, &r)
	default:
		err = fmt.Errorf("storage: unknown view primitive %q", b.Primitive)
	}
	return r, err
}

// resolveViewEntities resolves a selector in b.Read: each literal ref through
// the scoped name resolution (absent and out of scope alike are
// ErrViewEntityNotFound), else every visible entity of the kind the filter
// keeps.
func (p *PG) resolveViewEntities(ctx context.Context, q querier, b ViewBinding) ([]ViewEntity, error) {
	if len(b.Refs) > ViewMaxEntities {
		return nil, fmt.Errorf("%w: %d entities named, at most %d", ErrViewTooLarge, len(b.Refs), ViewMaxEntities)
	}
	if len(b.Refs) > 0 {
		out := make([]ViewEntity, 0, len(b.Refs))
		seen := map[string]bool{}
		for _, ref := range b.Refs {
			e, err := p.resolveViewRef(ctx, q, b.Kind, ref, b.Read)
			if err != nil {
				return nil, err
			}
			if !seen[e.ID] {
				seen[e.ID] = true
				out = append(out, e)
			}
		}
		return out, nil
	}
	return p.listViewEntities(ctx, q, b)
}

func (p *PG) resolveViewRef(ctx context.Context, q querier, kind, ref string, read scope.Set) (ViewEntity, error) {
	switch kind {
	case "component":
		c, err := scopedByNameInScope(ctx, q, componentConfig, ref, "component", read)
		if err != nil {
			return ViewEntity{}, viewRefErr(ref, err, ErrComponentNotFound)
		}
		return ViewEntity{ID: c.ID, Name: c.Name, Kind: kind}, nil
	case "system":
		s, err := scopedByNameInScope(ctx, q, systemConfig, ref, "system", read)
		if err != nil {
			return ViewEntity{}, viewRefErr(ref, err, ErrSystemNotFound)
		}
		return ViewEntity{ID: s.ID, Name: s.Name, Kind: kind}, nil
	}
	return ViewEntity{}, fmt.Errorf("storage: unknown view entity kind %q", kind)
}

// viewRefErr folds every not-found shape a scoped resolution returns into
// the one non-disclosing sentinel; anything else (an ambiguous name, a
// database failure) passes through.
func viewRefErr(ref string, err, notFound error) error {
	var path *ErrPathNotFound
	if errors.Is(err, notFound) || errors.As(err, &path) {
		return fmt.Errorf("%w: %q", ErrViewEntityNotFound, ref)
	}
	return err
}

// listViewEntities is every entity of b.Kind in b.Read that the filter keeps,
// by name, refused past ViewMaxEntities.
func (p *PG) listViewEntities(ctx context.Context, q querier, b ViewBinding) ([]ViewEntity, error) {
	tbl := componentTable
	if b.Kind == "system" {
		tbl = systemTable
	}
	args, ok := scopedListArgs(tbl, b.Read)
	if !ok {
		return nil, nil
	}
	var conds []string
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if b.Filter.System != "" {
		add(`v.id in (select sm.component_id from system_member sm join system s on s.id = sm.system_id
			where s.name = $%[1]d or s.id::text = $%[1]d)`, b.Filter.System)
	}
	if b.Filter.Product != "" {
		add(`v.product_id in (select pd.id from product pd where pd.name = $%[1]d or pd.id::text = $%[1]d)`, b.Filter.Product)
	}
	if b.Filter.SystemType != "" {
		add(`v.system_type_id in (select st.id from system_type st where st.name = $%[1]d or st.id::text = $%[1]d)`, b.Filter.SystemType)
	}
	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}
	args = append(args, ViewMaxEntities+1)
	cols := "id, name, product_id"
	if b.Kind == "system" {
		cols = "id, name, system_type_id"
	}
	rows, err := q.Query(ctx, `select v.id::text, v.name from (`+scopedListSQL(tbl, cols, b.Read)+`) v`+where+
		fmt.Sprintf(` order by v.name, v.id limit $%d`, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve view %s selector: %w", b.Kind, err)
	}
	ents, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ViewEntity, error) {
		e := ViewEntity{Kind: b.Kind}
		return e, r.Scan(&e.ID, &e.Name)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: resolve view %s selector: %w", b.Kind, err)
	}
	if len(ents) > ViewMaxEntities {
		return nil, fmt.Errorf("%w: the selector matches more than %d entities; narrow it", ErrViewTooLarge, ViewMaxEntities)
	}
	return ents, nil
}

// viewType is one key's type in effect and its effective retention.
type viewType struct {
	id   string
	days int
}

// viewTypes resolves keys to their types in effect; a key no type carries
// reads nothing.
func viewTypes(ctx context.Context, q querier, table string, keys []string, laneDays int) (map[string]viewType, error) {
	rows, err := q.Query(ctx, fmt.Sprintf(`select t.name, t.id::text, coalesce(t.retention_days, $2::int) from %s_in_effect() t where t.name = any($1::text[])`, table), keys, laneDays)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve view %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]viewType{}
	for rows.Next() {
		var name string
		var t viewType
		if err := rows.Scan(&name, &t.id, &t.days); err != nil {
			return nil, fmt.Errorf("storage: scan view %s: %w", table, err)
		}
		out[name] = t
	}
	return out, rows.Err()
}

// viewGrid is a bucketed read's grid: buckets of step aligned to the Unix
// epoch, from the one holding from through the one holding to (to is
// exclusive), so a bucket's boundaries never move between two reads.
type viewGrid struct {
	lo, hi time.Time
	step   time.Duration
}

func newViewGrid(from, to time.Time, step time.Duration) (viewGrid, error) {
	if step < time.Second || step%time.Second != 0 {
		return viewGrid{}, fmt.Errorf("%w: a step is whole seconds, at least one", ErrViewTooLarge)
	}
	sec := int64(step / time.Second)
	bin := func(t time.Time) int64 {
		u := t.Unix()
		if r := u % sec; r < 0 {
			return u - r - sec
		}
		return u - u%sec
	}
	g := viewGrid{lo: time.Unix(bin(from), 0).UTC(), step: step}
	g.hi = time.Unix(bin(to.Add(-time.Microsecond))+sec, 0).UTC()
	if !g.hi.After(g.lo) {
		g.hi = g.lo.Add(step)
	}
	if n := g.n(); n > ViewMaxBuckets {
		return viewGrid{}, fmt.Errorf("%w: %d buckets of %s, at most %d; widen the step", ErrViewTooLarge, n, step, ViewMaxBuckets)
	}
	return g, nil
}

func (g viewGrid) n() int { return int(g.hi.Sub(g.lo) / g.step) }

// viewSplit is where a metric read takes its rows from: the raw rows from
// rawFrom, and, when hourly trends answer the older part, the trend hours in
// [trendFrom, rawFrom). covered is the earliest instant either reaches.
type viewSplit struct {
	rawFrom, trendFrom time.Time
	trends             bool
	covered            time.Time
}

// splitMetricRead decides where a metric read's rows come from (the range
// picks raw history or hourly trends): the raw rows inside the type's
// retention, and, for an hour-multiple step reaching past it, the hourly
// trends for the whole hours before the raw floor's next hour, back to the
// trend floor. Pure, so the rule tests without a database.
func splitMetricRead(lo, now time.Time, step time.Duration, rawDays, trendDays int) viewSplit {
	rawFloor := now.Add(-time.Duration(rawDays) * 24 * time.Hour)
	s := viewSplit{rawFrom: later(lo, rawFloor), covered: later(lo, rawFloor)}
	if step <= 0 || step%time.Hour != 0 || !lo.Before(rawFloor) {
		return s
	}
	cut := rawFloor.Truncate(time.Hour)
	if cut.Before(rawFloor) {
		cut = cut.Add(time.Hour)
	}
	trendFloor := later(lo, now.Add(-time.Duration(trendDays)*24*time.Hour).Truncate(time.Hour))
	if !trendFloor.Before(cut) {
		return s
	}
	return viewSplit{rawFrom: cut, trendFrom: trendFloor, trends: true, covered: trendFloor}
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// metricPart is one bucket's aggregates from one source (trend hours or raw
// rows) before they merge.
type metricPart struct {
	owner, instance string
	b               time.Time
	count           int64
	min, max, sum   float64
	last            float64
}

type viewSeriesKey struct{ owner, key, instance string }

// mergeMetricParts folds the parts into one bucket grid per series, in
// part order: a later part's last wins, so the caller passes the older
// source first. Empty buckets stay empty; a bucket's avg is its sum over its
// count. Pure, so the stitch tests without a database.
func mergeMetricParts(key string, g viewGrid, parts []metricPart) []ViewMetricSeries {
	idx := map[viewSeriesKey]int{}
	var out []ViewMetricSeries
	sums := [][]float64{}
	for _, pt := range parts {
		k := viewSeriesKey{pt.owner, key, pt.instance}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			s := ViewMetricSeries{EntityID: pt.owner, Key: key, Instance: pt.instance, Buckets: make([]MetricBucket, g.n())}
			for j := range s.Buckets {
				s.Buckets[j].TS = g.lo.Add(time.Duration(j) * g.step)
			}
			out = append(out, s)
			sums = append(sums, make([]float64, g.n()))
		}
		j := int(pt.b.Sub(g.lo) / g.step)
		if j < 0 || j >= g.n() {
			continue
		}
		bk := &out[i].Buckets[j]
		if bk.Count == 0 {
			mn, mx := pt.min, pt.max
			bk.Min, bk.Max = &mn, &mx
		} else {
			*bk.Min, *bk.Max = min(*bk.Min, pt.min), max(*bk.Max, pt.max)
		}
		last := pt.last
		bk.Last = &last
		bk.Count += pt.count
		sums[i][j] += pt.sum
		avg := sums[i][j] / float64(bk.Count)
		bk.Avg = &avg
	}
	sortViewSeries(out, func(s ViewMetricSeries) viewSeriesKey { return viewSeriesKey{s.EntityID, s.Key, s.Instance} })
	return out
}

func sortViewSeries[S any](s []S, key func(S) viewSeriesKey) {
	slices.SortStableFunc(s, func(a, b S) int {
		ka, kb := key(a), key(b)
		return cmpStrings(ka.owner, kb.owner, ka.key, kb.key, ka.instance, kb.instance)
	})
}

// cmpStrings compares pairs in order: a0 with b0, then a1 with b1.
func cmpStrings(pairs ...string) int {
	for i := 0; i+1 < len(pairs); i += 2 {
		if c := strings.Compare(pairs[i], pairs[i+1]); c != 0 {
			return c
		}
	}
	return 0
}

// viewMetric reads a metric binding: per key, the raw points of every
// measured series of the entities, or their bucket grids with the older
// whole hours from the hourly trends where the range passes the raw floor.
func (p *PG) viewMetric(ctx context.Context, q querier, b ViewBinding, ids []string, now time.Time, laneDays, trendDays int, r *ViewResult) error {
	if len(ids) == 0 {
		return nil
	}
	types, err := viewTypes(ctx, q, "metric_type", b.Keys, laneDays)
	if err != nil {
		return err
	}
	points := 0
	var g viewGrid
	if b.Step > 0 {
		if g, err = newViewGrid(r.From, r.To, b.Step); err != nil {
			return err
		}
	}
	covered := r.From
	for _, key := range b.Keys {
		t, ok := types[key]
		if !ok {
			continue
		}
		read := metricReadSQL("s.id", "$3::timestamptz", "$4::timestamptz", "$5")
		if b.Step == 0 {
			split := splitMetricRead(r.From, now, 0, t.days, trendDays)
			covered = later(covered, split.covered)
			rows, err := q.Query(ctx, `
				select s.owner_id::text, s.instance, m.ts, m.value
				from metric_series s cross join lateral (`+read+`) m
				where s.metric_type_id = $1::uuid and s.owner_kind = $6 and s.owner_id = any($2::uuid[])
				  and s.provenance in `+measuredProvenance+`
				order by s.owner_id, s.instance, `+metricSampleOrder("m", false)+`, s.id
				limit $7`, t.id, ids, split.rawFrom, r.To, laneDays, b.Kind, ViewMaxPoints-points+1)
			if err != nil {
				return fmt.Errorf("storage: read view metric %s: %w", key, err)
			}
			var cur *ViewMetricSeries
			for rows.Next() {
				var owner, inst string
				var pt ViewPoint
				if err := rows.Scan(&owner, &inst, &pt.TS, &pt.Value); err != nil {
					rows.Close()
					return fmt.Errorf("storage: scan view metric %s: %w", key, err)
				}
				if points++; points > ViewMaxPoints {
					rows.Close()
					return fmt.Errorf("%w: more than %d points; set a step", ErrViewTooLarge, ViewMaxPoints)
				}
				if cur == nil || cur.EntityID != owner || cur.Instance != inst {
					r.Metric = append(r.Metric, ViewMetricSeries{EntityID: owner, Key: key, Instance: inst})
					cur = &r.Metric[len(r.Metric)-1]
				}
				cur.Points = append(cur.Points, pt)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return fmt.Errorf("storage: read view metric %s: %w", key, err)
			}
			continue
		}
		split := splitMetricRead(g.lo, now, b.Step, t.days, trendDays)
		covered = later(covered, split.covered)
		var parts []metricPart
		if split.trends {
			rows, err := q.Query(ctx, `
				select s.owner_id::text, s.instance, date_bin(make_interval(secs => $3), tr.hour, timestamptz 'epoch'),
					sum(tr.count)::bigint, min(tr.min), max(tr.max), sum(tr.sum), (array_agg(tr.last order by tr.hour desc, s.id desc))[1]
				from metric_series s join metric_trend tr on tr.series_id = s.id
				where s.metric_type_id = $1::uuid and s.owner_kind = $6 and s.owner_id = any($2::uuid[])
				  and s.provenance = 'observed' and tr.hour >= $4 and tr.hour < $5
				group by 1, 2, 3`, t.id, ids, b.Step.Seconds(), split.trendFrom, split.rawFrom, b.Kind)
			if err != nil {
				return fmt.Errorf("storage: read view metric trends %s: %w", key, err)
			}
			if parts, err = scanMetricParts(rows, parts); err != nil {
				return err
			}
		}
		rows, err := q.Query(ctx, `
			select s.owner_id::text, s.instance, date_bin(make_interval(secs => $7), m.ts, timestamptz 'epoch'),
				count(*), min(m.value), max(m.value), sum(m.value),
				(array_agg(m.value order by m.ts desc, s.id desc, `+metricLineageOrder("m", true)+`))[1]
			from metric_series s cross join lateral (`+read+`) m
			where s.metric_type_id = $1::uuid and s.owner_kind = $6 and s.owner_id = any($2::uuid[])
			  and s.provenance in `+measuredProvenance+`
			group by 1, 2, 3`, t.id, ids, split.rawFrom, g.hi, laneDays, b.Kind, b.Step.Seconds())
		if err != nil {
			return fmt.Errorf("storage: read view metric buckets %s: %w", key, err)
		}
		if parts, err = scanMetricParts(rows, parts); err != nil {
			return err
		}
		r.Metric = append(r.Metric, mergeMetricParts(key, g, parts)...)
	}
	r.CoveredFrom = covered
	return nil
}

func scanMetricParts(rows pgx.Rows, parts []metricPart) ([]metricPart, error) {
	defer rows.Close()
	for rows.Next() {
		var pt metricPart
		if err := rows.Scan(&pt.owner, &pt.instance, &pt.b, &pt.count, &pt.min, &pt.max, &pt.sum, &pt.last); err != nil {
			return nil, fmt.Errorf("storage: scan view metric bucket: %w", err)
		}
		parts = append(parts, pt)
	}
	return parts, rows.Err()
}

// viewProperty reads a property binding: per key, every measured series of
// the entities with the value in force when the range opens, as its changes
// or its bucket grid.
func (p *PG) viewProperty(ctx context.Context, q querier, b ViewBinding, ids []string, now time.Time, laneDays int, r *ViewResult) error {
	if len(ids) == 0 {
		return nil
	}
	col, err := ownerColumn(b.Kind)
	if err != nil {
		return err
	}
	types, err := viewTypes(ctx, q, "property_type", b.Keys, laneDays)
	if err != nil {
		return err
	}
	var g viewGrid
	from := r.From
	if b.Step > 0 {
		if g, err = newViewGrid(r.From, r.To, b.Step); err != nil {
			return err
		}
		from = g.lo
	}
	points := 0
	covered := r.From
	for _, key := range b.Keys {
		t, ok := types[key]
		if !ok {
			continue
		}
		floor := now.Add(-time.Duration(t.days) * 24 * time.Hour)
		covered = later(covered, floor)
		lo := later(from, floor)
		series := map[viewSeriesKey]*ViewPropertySeries{}
		var order []viewSeriesKey
		get := func(owner, inst string) *ViewPropertySeries {
			k := viewSeriesKey{owner, key, inst}
			if s, ok := series[k]; ok {
				return s
			}
			s := &ViewPropertySeries{EntityID: owner, Key: key, Instance: inst}
			series[k] = s
			order = append(order, k)
			return s
		}
		rows, err := q.Query(ctx, fmt.Sprintf(`
			select distinct on (pr.%[1]s, pr.instance) pr.%[1]s::text, pr.instance, pr.value #>> '{}'
			from property pr
			where pr.%[1]s = any($1::uuid[]) and pr.property_type_id = $2::uuid and pr.provenance in `+measuredProvenance+`
			  and pr.ts >= $3 and pr.ts < $4
			order by pr.%[1]s, pr.instance, pr.ts desc, pr.id desc`, col), ids, t.id, floor, lo)
		if err != nil {
			return fmt.Errorf("storage: read view property carried-in %s: %w", key, err)
		}
		for rows.Next() {
			var owner, inst, v string
			if err := rows.Scan(&owner, &inst, &v); err != nil {
				rows.Close()
				return fmt.Errorf("storage: scan view property carried-in %s: %w", key, err)
			}
			get(owner, inst).CarriedIn = &v
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		to := r.To
		if b.Step > 0 {
			to = g.hi
		}
		rows, err = q.Query(ctx, fmt.Sprintf(`
			select pr.%[1]s::text, pr.instance, pr.ts, pr.value #>> '{}'
			from property pr
			where pr.%[1]s = any($1::uuid[]) and pr.property_type_id = $2::uuid and pr.provenance in `+measuredProvenance+`
			  and pr.ts >= $3 and pr.ts < $4
			order by pr.%[1]s, pr.instance, pr.ts, pr.id
			limit $5`, col), ids, t.id, lo, to, ViewMaxPoints-points+1)
		if err != nil {
			return fmt.Errorf("storage: read view property %s: %w", key, err)
		}
		for rows.Next() {
			var owner, inst string
			var pt ViewPropertyPoint
			if err := rows.Scan(&owner, &inst, &pt.TS, &pt.Value); err != nil {
				rows.Close()
				return fmt.Errorf("storage: scan view property %s: %w", key, err)
			}
			if points++; points > ViewMaxPoints {
				rows.Close()
				return fmt.Errorf("%w: more than %d changes; narrow the range", ErrViewTooLarge, ViewMaxPoints)
			}
			s := get(owner, inst)
			s.Points = append(s.Points, pt)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("storage: read view property %s: %w", key, err)
		}
		for _, k := range order {
			s := series[k]
			if b.Step > 0 {
				s.Buckets = propertyGrid(g, s.Points, s.CarriedIn)
				s.Points = nil
			}
			r.Property = append(r.Property, *s)
		}
	}
	sortViewSeries(r.Property, func(s ViewPropertySeries) viewSeriesKey { return viewSeriesKey{s.EntityID, s.Key, s.Instance} })
	r.CoveredFrom = covered
	return nil
}

// propertyGrid buckets one series' changes (oldest first) on g: each bucket's
// change count, the value its own changes end on, and the value held at its
// end. Pure.
func propertyGrid(g viewGrid, points []ViewPropertyPoint, carriedIn *string) []PropertyBucket {
	out := make([]PropertyBucket, g.n())
	for j := range out {
		out[j].TS = g.lo.Add(time.Duration(j) * g.step)
	}
	for _, pt := range points {
		j := int(pt.TS.Sub(g.lo) / g.step)
		if j < 0 || j >= len(out) {
			continue
		}
		v := pt.Value
		out[j].Count++
		out[j].Last = &v
	}
	holdAcross(out, carriedIn)
	return out
}

// ViewPage is an event or log page cursor: the (instant, id) of the last row
// a page returned, the next page reading strictly older.
func viewPage(at time.Time, id int64) string {
	return strconv.FormatInt(at.UnixMicro(), 10) + "." + strconv.FormatInt(id, 10)
}

func parseViewPage(page string) (time.Time, int64, error) {
	us, id, ok := strings.Cut(page, ".")
	if !ok {
		return time.Time{}, 0, fmt.Errorf("%w %q", ErrViewPage, page)
	}
	u, err1 := strconv.ParseInt(us, 10, 64)
	n, err2 := strconv.ParseInt(id, 10, 64)
	if err1 != nil || err2 != nil {
		return time.Time{}, 0, fmt.Errorf("%w %q", ErrViewPage, page)
	}
	return time.UnixMicro(u).UTC(), n, nil
}

// viewPageArgs renders a page's bound over the order columns (at, id) and the
// limit, appending their arguments.
func viewPageArgs(b ViewBinding, at string, args []any) (string, []any, int, error) {
	limit := b.Limit
	if limit <= 0 {
		limit = viewDefaultPage
	}
	limit = min(limit, ViewMaxPage)
	cond := ""
	if b.Page != "" {
		ts, id, err := parseViewPage(b.Page)
		if err != nil {
			return "", nil, 0, err
		}
		args = append(args, ts, id)
		cond = fmt.Sprintf(" and (%s, id) < ($%d, $%d)", at, len(args)-1, len(args))
	}
	return cond, args, limit, nil
}

// viewEvents reads an event binding: the entities' own events in range,
// newest first, a page at a time.
func (p *PG) viewEvents(ctx context.Context, q querier, b ViewBinding, ids []string, r *ViewResult) error {
	if len(ids) == 0 {
		return nil
	}
	col, err := ownerColumn(b.Kind)
	if err != nil {
		return err
	}
	args := []any{ids, r.From, r.To}
	keys := ""
	if len(b.Keys) > 0 {
		args = append(args, b.Keys)
		keys = fmt.Sprintf(" and event.event_type_id in (select et.id from event_type et where et.name = any($%d::text[]))", len(args))
	}
	page, args, limit, err := viewPageArgs(b, "ts", args)
	if err != nil {
		return err
	}
	args = append(args, limit+1)
	rows, err := q.Query(ctx, fmt.Sprintf(`
		select id, ts, owner_kind, %[1]s::text,
			(select et.name from event_type et where et.id = event.event_type_id), event.event_type_id, instance, origin, message, attributes, provenance, `+sourceRendered("event")+`,
			`+eventLineageCols+`
		from event
		where %[1]s = any($1::uuid[]) and ts >= $2 and ts < $3%[2]s%[3]s
		order by ts desc, id desc
		limit $%[4]d`, col, keys, page, len(args)), args...)
	if err != nil {
		return fmt.Errorf("storage: read view events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e Event
		var rule *string
		if err := rows.Scan(&e.ID, &e.TS, &e.OwnerKind, &e.OwnerID, &e.Key, &e.EventTypeID, &e.Instance, &e.Origin, &e.Message, &e.Attributes, &e.Provenance, &e.Source,
			&rule, &e.DerivedByRuleID, &e.AlarmID); err != nil {
			return fmt.Errorf("storage: scan view event: %w", err)
		}
		if rule != nil {
			e.DerivedByRule = *rule
		}
		r.Events = append(r.Events, e)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: read view events: %w", err)
	}
	if len(r.Events) > limit {
		r.Events = r.Events[:limit]
		last := r.Events[limit-1]
		r.NextPage = viewPage(last.TS, last.ID)
	}
	return nil
}

// viewLogs reads a log binding: the components' lines that arrived in range,
// newest arrival first, a page at a time, narrowed by the severity and
// source filter.
func (p *PG) viewLogs(ctx context.Context, q querier, b ViewBinding, ids []string, r *ViewResult) error {
	if len(ids) == 0 {
		return nil
	}
	if b.Kind != "component" {
		return fmt.Errorf("%w: log lines belong to components", ErrUnknownOwnerKind)
	}
	args := []any{ids, r.From, r.To}
	cond, args, err := b.Logs.where(args)
	if err != nil {
		return err
	}
	page, args, limit, err := viewPageArgs(b, logArrival, args)
	if err != nil {
		return err
	}
	args = append(args, limit+1)
	rows, err := q.Query(ctx, fmt.Sprintf(`
		select id, ts, component_id::text, instance, source,
			coalesce(severity, ''), coalesce(facility, ''), message, attributes, labels, coalesce(correlation_id, ''), received_at
		from log_line
		where component_id = any($1::uuid[]) and %[1]s >= $2 and %[1]s < $3%[2]s%[3]s
		order by %[1]s desc, id desc
		limit $%[4]d`, logArrival, cond, page, len(args)), args...)
	if err != nil {
		return fmt.Errorf("storage: read view logs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l LogLine
		if err := rows.Scan(&l.ID, &l.TS, &l.Component, &l.Instance, &l.Source, &l.Severity, &l.Facility, &l.Message, &l.Attributes, &l.Labels, &l.CorrelationID, &l.ReceivedAt); err != nil {
			return fmt.Errorf("storage: scan view log: %w", err)
		}
		r.Logs = append(r.Logs, l)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: read view logs: %w", err)
	}
	if len(r.Logs) > limit {
		r.Logs = r.Logs[:limit]
		last := r.Logs[limit-1]
		at := last.TS
		if last.ReceivedAt != nil {
			at = *last.ReceivedAt
		}
		r.NextPage = viewPage(at, last.ID)
	}
	return nil
}
