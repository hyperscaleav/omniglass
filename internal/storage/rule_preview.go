package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// The preview's bounds (#959).
const (
	// RulePreviewWindow is how far back a preview judges samples.
	RulePreviewWindow = 24 * time.Hour
	// RulePreviewRowCap bounds each of a preview's two reads (the newest
	// sample before the window per series, and the window's samples), so a
	// request costs at most this many rows of each, whatever the fleet holds.
	RulePreviewRowCap = 5000
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

// RulePreviewSeries is a series the candidate holds in force now: since the
// fire edge in the window that brought it in, or nil when it was in force
// before the window began.
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
	InForce         []RulePreviewSeries
	Edges           []RulePreviewEdge
	Window          time.Duration
	Since, Until    time.Time
	RowCap          int
	Truncated       bool
	SustainsIgnored bool
}

// previewRow is one sample a preview judges.
type previewRow struct {
	component, name, typeName, instance string
	ts                                  time.Time
	value                               any
}

// PreviewEventRule judges a candidate rule without writing anything (#959),
// in a read-only transaction. Over each series the candidate selects, among
// the components the visible scope admits, it judges the newest sample
// before the window (the state the window opens on), then every sample in
// the window in order, as the ingest would with no sustains: a fire with the
// condition out of force is a fire edge, a clear with it in force a clear
// edge, anything else holds. The series in force at the end are in force
// now. The criteria compile once, and every sample runs under the criteria
// budget (a sample it cannot judge holds). Each of the two reads is bounded
// by the row cap; past it the preview is cut short and says so.
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
	if err := resolveRuleSelector(ctx, tx, &r, EventRuleSelector{}); err != nil {
		return nil, err
	}
	until, err := dbNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	since := until.Add(-RulePreviewWindow)
	out := &RulePreview{Window: RulePreviewWindow, Since: since, Until: until, RowCap: rulePreviewRowCap, SustainsIgnored: true,
		InForce: []RulePreviewSeries{}, Edges: []RulePreviewEdge{}}

	seeds, cut, err := previewRows(ctx, tx, r, visible, since, true)
	if err != nil {
		return nil, err
	}
	out.Truncated = out.Truncated || cut
	window, cut, err := previewRows(ctx, tx, r, visible, since, false)
	if err != nil {
		return nil, err
	}
	out.Truncated = out.Truncated || cut

	m := newRuleMatcher(p, nil)
	picked := map[string]bool{}
	selects := func(row previewRow) (bool, error) {
		if v, ok := picked[row.component]; ok {
			return v, nil
		}
		o, err := m.ownerFacts(ctx, tx, row.component)
		if err != nil {
			return false, err
		}
		v, err := m.selects(ctx, tx, r, o)
		picked[row.component] = v
		return v, err
	}
	type series struct{ component, instance string }
	type state struct {
		name    string
		inForce bool
		since   *time.Time
	}
	states := map[series]*state{}
	var order []series
	decide := func(row previewRow) rules.Action {
		a, err := judge.Decide(rules.Sample{Value: row.value, Key: r.SampleKey, Instance: row.instance,
			Component: rules.Component{Name: row.name, Type: row.typeName}})
		if err != nil {
			return rules.Hold
		}
		return a
	}
	step := func(row previewRow, inWindow bool) error {
		ok, err := selects(row)
		if err != nil || !ok {
			return err
		}
		k := series{row.component, row.instance}
		st, seen := states[k]
		if !seen {
			st = &state{name: row.name}
			states[k] = st
			order = append(order, k)
		}
		switch decide(row) {
		case rules.Fire:
			if !st.inForce {
				st.inForce = true
				st.since = nil
				if inWindow {
					at := row.ts
					st.since = &at
					out.Edges = append(out.Edges, RulePreviewEdge{ComponentID: row.component, ComponentName: row.name,
						Instance: row.instance, Kind: "fire", At: row.ts})
				}
			}
		case rules.Clear:
			if st.inForce {
				st.inForce, st.since = false, nil
				if inWindow {
					out.Edges = append(out.Edges, RulePreviewEdge{ComponentID: row.component, ComponentName: row.name,
						Instance: row.instance, Kind: "clear", At: row.ts})
				}
			}
		}
		return nil
	}
	for _, row := range seeds {
		if err := step(row, false); err != nil {
			return nil, err
		}
	}
	for _, row := range window {
		if err := step(row, true); err != nil {
			return nil, err
		}
	}
	for _, k := range order {
		if st := states[k]; st.inForce {
			out.InForce = append(out.InForce, RulePreviewSeries{ComponentID: k.component, ComponentName: st.name, Instance: k.instance, Since: st.since})
		}
	}
	return out, nil
}

// previewRows reads one of a preview's two bounded reads on the rule's lane,
// over the components the visible scope admits: with seeds, the newest
// sample before since of every series; without, every sample since, in
// series and time order. It reads at most the row cap, and reports whether
// there was more.
func previewRows(ctx context.Context, tx pgx.Tx, r EventRule, visible scope.Set, since time.Time, seeds bool) ([]previewRow, bool, error) {
	table, typeTable, value := laneTables(r.SampleLane)
	f := newReadFilter(componentTable, visible, 4)
	var q string
	if seeds {
		q = fmt.Sprintf(`select distinct on (s.component_id, s.instance) s.component_id::text, c.name, coalesce(ct.name, ''), s.instance, s.ts, %[3]s
			from %[1]s s join %[2]s t on t.id = s.%[2]s_id
			join component c on c.id = s.component_id
			left join product pr on pr.id = c.product_id left join component_type ct on ct.id = pr.component_type_id
			where t.name = $1 and s.component_id is not null and s.ts < $2 and %[4]s
			order by s.component_id, s.instance, s.ts desc, s.id desc limit $3`, table, typeTable, value, f.Pred("s.component_id"))
	} else {
		q = fmt.Sprintf(`select s.component_id::text, c.name, coalesce(ct.name, ''), s.instance, s.ts, %[3]s
			from %[1]s s join %[2]s t on t.id = s.%[2]s_id
			join component c on c.id = s.component_id
			left join product pr on pr.id = c.product_id left join component_type ct on ct.id = pr.component_type_id
			where t.name = $1 and s.component_id is not null and s.ts >= $2 and %[4]s
			order by s.component_id, s.instance, s.ts, s.id limit $3`, table, typeTable, value, f.Pred("s.component_id"))
	}
	args := append([]any{r.SampleKey, since, rulePreviewRowCap + 1}, f.Args...)
	rows, err := tx.Query(ctx, f.With+q, args...)
	if err != nil {
		return nil, false, fmt.Errorf("storage: rule preview read: %w", err)
	}
	defer rows.Close()
	var out []previewRow
	for rows.Next() {
		var row previewRow
		if err := rows.Scan(&row.component, &row.name, &row.typeName, &row.instance, &row.ts, &row.value); err != nil {
			return nil, false, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) > rulePreviewRowCap {
		return out[:rulePreviewRowCap], true, nil
	}
	return out, false, nil
}
