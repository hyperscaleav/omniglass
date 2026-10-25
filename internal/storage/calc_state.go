package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// A calc rule's live state and its preview (#998, epic #525). Both name
// owners and members, so both are fenced: an owner is listed only when the
// caller may read it (a component by the component read scope, a system by
// the system read scope), and a member a skipped or stale entry names is
// named only when the caller may read that component; otherwise it is a
// count.

// CalcMemberRef is a member a computation skipped (it held no value of an
// input), named by id and, when the reader may read it, by name.
type CalcMemberRef struct {
	Key, ComponentID, ComponentName string
}

// CalcStaleRef is an instance an input's list left out as stale.
type CalcStaleRef struct {
	Key, ComponentID, ComponentName, Instance string
}

// CalcOwnerState is a rule's last outcome on one owner, and whether the
// owner is pending now. An owner pending before its first computation has
// no outcome yet (ComputedAt nil).
type CalcOwnerState struct {
	OwnerKind, OwnerID, OwnerName string
	Value                         json.RawMessage
	ComputedAt                    *time.Time
	RuleVersion                   int
	Missing                       []string
	Skipped                       []CalcMemberRef
	SkippedHidden                 int
	Stale                         []CalcStaleRef
	StaleHidden                   int
	Error                         string
	NoMembersSince                *time.Time
	Pending                       bool
	// DueAt is when a deferred mark (a failed computation backing off) is
	// due; nil when the mark is due now or there is none.
	DueAt *time.Time
}

// CalcRuleLive is a rule's state on the owners the caller may read, at most
// the limit asked for, of Total; and how many of all those owners are
// pending.
type CalcRuleLive struct {
	Owners       []CalcOwnerState
	Total        int
	PendingDepth int
}

// CalcStateLimit is how many owners a state read lists when the caller asks
// for no other limit; CalcStateMaxLimit is the most it may ask for.
const (
	CalcStateLimit    = 100
	CalcStateMaxLimit = 1000
)

// calcStateRank orders a rule's owners for a reader (#999's review): those
// it failed on, then those pending, then those it wrote nothing for (an
// input missing, a system with no members), then those holding a value.
const calcStateRank = `case when coalesce(s.error, '') <> '' then 0
		when pd.marked_at is not null then 1
		when s.no_members_since is not null or coalesce(s.missing, '[]') <> '[]' then 2
		when s.value is not null then 3 else 4 end`

// CalcRuleState reads a rule's live state (by name or uuid), fenced to the
// owners the component and system read scopes admit, ordered by
// calcStateRank and then name, at most limit owners (CalcStateLimit when it
// is not positive, never more than CalcStateMaxLimit).
func (p *PG) CalcRuleState(ctx context.Context, ref string, components, systems scope.Set, limit int) (*CalcRuleLive, error) {
	if limit <= 0 {
		limit = CalcStateLimit
	}
	limit = min(limit, CalcStateMaxLimit)
	// One read-only transaction: the owners, their names and the members
	// named read from one snapshot.
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("storage: begin calc rule state: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	r, err := readCalcRule(ctx, tx, ref, false)
	if err != nil {
		return nil, err
	}
	live := &CalcRuleLive{Owners: []CalcOwnerState{}}
	type stored struct {
		state   CalcOwnerState
		skipped []skippedMember
		stale   []staleInstance
	}
	var rows []stored
	// A rule's owners are all of its one kind (fixed at create).
	for _, kind := range []string{r.OwnerKind} {
		kindTotal, kindPending := 0, 0
		owner := calcOwner{kind: kind}
		table, vis := componentTable, components
		if kind == rules.OwnerSystem {
			table, vis = systemTable, systems
		}
		f := newReadFilter(table, vis, 3)
		col := owner.col()
		q := f.With + ` select o.id::text, o.name, s.value, s.computed_at, coalesce(s.rule_version, 0),
				coalesce(s.missing, '[]'), coalesce(s.skipped, '[]'), coalesce(s.stale, '[]'), coalesce(s.error, ''), s.no_members_since,
				pd.marked_at is not null, case when pd.marked_at > now() then pd.marked_at end,
				count(*) over (), count(*) filter (where pd.marked_at is not null) over ()
			from (select ` + col + ` as id from calc_rule_state where rule_id = $1::uuid and ` + col + ` is not null
				union select ` + col + ` from calc_pending where rule_id = $1::uuid and ` + col + ` is not null) k
			join ` + string(table) + ` o on o.id = k.id
			left join calc_rule_state s on s.rule_id = $1::uuid and s.` + col + ` = o.id
			left join calc_pending pd on pd.rule_id = $1::uuid and pd.` + col + ` = o.id
			where ` + f.Pred("o.id") + ` order by ` + calcStateRank + `, o.name, o.id limit $2`
		res, err := tx.Query(ctx, q, append([]any{r.ID, limit}, f.Args...)...)
		if err != nil {
			return nil, fmt.Errorf("storage: calc rule %s state: %w", r.Name, err)
		}
		for res.Next() {
			var x stored
			var value, missing, skipped, stale []byte
			x.state.OwnerKind = kind
			var total, pending int
			if err := res.Scan(&x.state.OwnerID, &x.state.OwnerName, &value, &x.state.ComputedAt, &x.state.RuleVersion,
				&missing, &skipped, &stale, &x.state.Error, &x.state.NoMembersSince, &x.state.Pending, &x.state.DueAt, &total, &pending); err != nil {
				res.Close()
				return nil, fmt.Errorf("storage: calc rule %s state: %w", r.Name, err)
			}
			if len(value) > 0 {
				x.state.Value = value
			}
			if err := errors.Join(json.Unmarshal(missing, &x.state.Missing), json.Unmarshal(skipped, &x.skipped),
				json.Unmarshal(stale, &x.stale)); err != nil {
				res.Close()
				return nil, fmt.Errorf("storage: calc rule %s state: %w", r.Name, err)
			}
			rows = append(rows, x)
			// The window counts are the same on every row.
			kindTotal, kindPending = total, pending
		}
		res.Close()
		live.Total += kindTotal
		live.PendingDepth += kindPending
		if err := res.Err(); err != nil {
			return nil, err
		}
	}
	var ids []string
	for _, x := range rows {
		for _, m := range x.skipped {
			ids = append(ids, m.Component)
		}
		for _, s := range x.stale {
			ids = append(ids, s.Component)
		}
	}
	names, err := visibleComponentNames(ctx, tx, ids, components)
	if err != nil {
		return nil, err
	}
	for _, x := range rows {
		st := x.state
		st.Skipped, st.SkippedHidden = nameSkipped(x.skipped, names)
		st.Stale, st.StaleHidden = nameStale(x.stale, names)
		// The pending depth (counted above, over every owner listed or
		// not) is over the owners the caller may read: a mark on an owner
		// it may not is no fact it learns.
		live.Owners = append(live.Owners, st)
	}
	return live, nil
}

// hiddenCalcMembers counts the members of a system a preview would read
// that the component read scope does not admit.
func hiddenCalcMembers(ctx context.Context, q querier, ms []calcMember, read scope.Set) (int, error) {
	if len(ms) == 0 {
		return 0, nil
	}
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.id
	}
	names, err := visibleComponentNames(ctx, q, ids, read)
	if err != nil {
		return 0, err
	}
	return len(ms) - len(names), nil
}

// visibleComponentNames names the components among ids the read scope
// admits, in one statement; an id it leaves out is not visible.
func visibleComponentNames(ctx context.Context, q querier, ids []string, read scope.Set) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	f := newReadFilter(componentTable, read, 2)
	rows, err := q.Query(ctx, f.With+` select c.id::text, c.name from component c where c.id = any($1::uuid[]) and `+f.Pred("c.id"),
		append([]any{ids}, f.Args...)...)
	if err != nil {
		return nil, fmt.Errorf("storage: name calc members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func nameSkipped(in []skippedMember, names map[string]string) ([]CalcMemberRef, int) {
	out, hidden := []CalcMemberRef{}, 0
	for _, m := range in {
		name, ok := names[m.Component]
		if !ok {
			hidden++
			continue
		}
		out = append(out, CalcMemberRef{Key: m.Key, ComponentID: m.Component, ComponentName: name})
	}
	return out, hidden
}

func nameStale(in []staleInstance, names map[string]string) ([]CalcStaleRef, int) {
	out, hidden := []CalcStaleRef{}, 0
	for _, s := range in {
		name, ok := names[s.Component]
		if !ok {
			hidden++
			continue
		}
		out = append(out, CalcStaleRef{Key: s.Key, ComponentID: s.Component, ComponentName: name, Instance: s.Instance})
	}
	return out, hidden
}

// CalcPreviewOwnerCap bounds the owners a preview computes: past it, the
// preview says how many it computed of how many.
const CalcPreviewOwnerCap = 200

// CalcPreviewWorkCap bounds a preview's input reads (each member's each
// input, a component its own one member): a system preview over large
// systems stops at it, a whole owner at a time, and says so as truncation.
const CalcPreviewWorkCap = 5000

// calcWindowWork is what one windowed input's read weighs on the work cap:
// a range read of up to rules.MaxCalcWindowSamples rows, counted as a tenth
// of its bound in current-value reads (each of which walks a key's instances
// one index probe at a time).
const calcWindowWork = rules.MaxCalcWindowSamples / 10

// calcInputWork is one member's reads of a rule's inputs, in work units: one
// a current-value input, calcWindowWork a windowed one.
func calcInputWork(inputs []rules.CalcInput) int {
	n := 0
	for _, in := range inputs {
		if in.Window > 0 {
			n += calcWindowWork
			continue
		}
		n++
	}
	return n
}

// calcPreviewOwnerCap and calcPreviewWorkCap are the caps; variables only so
// a test can lower them.
var (
	calcPreviewOwnerCap = CalcPreviewOwnerCap
	calcPreviewWorkCap  = CalcPreviewWorkCap
)

// CalcPreviewOwner is what a candidate rule would do on one owner now.
type CalcPreviewOwner struct {
	OwnerKind, OwnerID, OwnerName string
	// Value is what the rule would write; nil when it would write nothing
	// (an input missing, an error).
	Value json.RawMessage
	// Current is the output's current value on the owner; nil when none.
	Current json.RawMessage
	// Differs is whether writing Value would add a sample (it differs from
	// Current, or there is none).
	Differs       bool
	Missing       []string
	Skipped       []CalcMemberRef
	SkippedHidden int
	Stale         []CalcStaleRef
	StaleHidden   int
	NoMembers     bool
	Error         string
}

// CalcPreview is a candidate rule computed on the owners it selects in
// scope, at most OwnerCap of OwnersTotal (Truncated past it, or past the
// work cap).
type CalcPreview struct {
	Owners                    []CalcPreviewOwner
	OwnersJudged, OwnersTotal int
	OwnerCap                  int
	Truncated                 bool
}

// PreviewCalcRule computes a candidate rule on every owner it selects
// within the read scopes, with the reduce the tick runs, in a read-only
// transaction that writes nothing and takes no lock a save waits on (the
// graph is read from the transaction's snapshot). The selector's names
// resolve in the caller's scope first, so a name that does not exist and
// one the caller may not read are refused alike. The candidate is then
// checked as a save would check it (prepareCalcRule: it compiles, closes no
// cycle, takes no key another enabled rule or a collection writes), so a
// rule that could not be saved is refused here with the same error; given a
// saved rule's name it is judged as an edit of that rule. The candidate is
// compiled and the key registry read once; a component preview selects the
// components holding one of its inputs, as the tick would mark them.
func (p *PG) PreviewCalcRule(ctx context.Context, spec CalcRuleSpec, components, systems scope.Set) (*CalcPreview, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("storage: begin calc preview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	r := CalcRule{Name: spec.Name, OwnerKind: spec.OwnerKind, Selector: spec.Selector, Inputs: spec.Inputs, Reduce: spec.Reduce,
		OutputKey: spec.OutputKey, Enabled: true, candidate: true}
	if r.Name == "" {
		r.Name = "preview"
	} else if saved, err := readCalcRule(ctx, tx, r.Name, false); err == nil {
		r.ID = saved.ID
	} else if !errors.Is(err, ErrCalcRuleNotFound) {
		return nil, err
	}
	if err := resolveSelectorInScope(ctx, tx, &r, components, systems); err != nil {
		return nil, err
	}
	var outIDs [2]any
	if err := prepareCalcRule(ctx, tx, &r, calcCheckAll, &outIDs); err != nil {
		return nil, err
	}
	plan, fault, err := p.calcPlan(ctx, tx, r)
	if err != nil {
		return nil, err
	}
	if fault != "" {
		return nil, invalidCalc("reduce", "%s", fault)
	}
	owners, err := previewCalcOwners(ctx, tx, r, components, systems)
	if err != nil {
		return nil, err
	}
	// The owners holding a current output come first, so the cap never hides
	// one whose value the rule would change; then by name (#999's review).
	if err := orderPreviewOwners(ctx, tx, r, owners); err != nil {
		return nil, err
	}
	pv := &CalcPreview{Owners: []CalcPreviewOwner{}, OwnersTotal: len(owners), OwnerCap: calcPreviewOwnerCap}
	type computed struct {
		row     CalcPreviewOwner
		skipped []skippedMember
		stale   []staleInstance
	}
	var all []computed
	var ids []string
	work := 0
	for i, o := range owners {
		if i >= calcPreviewOwnerCap {
			pv.Truncated = true
			break
		}
		if p.calcHook != nil {
			if err := p.calcHook("preview"); err != nil {
				return nil, err
			}
		}
		members, hidden := 1, 0
		if o.owner.kind == rules.OwnerSystem {
			ms, err := systemMembers(ctx, tx, o.owner.id)
			if err != nil {
				return nil, err
			}
			members = len(ms)
			if !plan.prog.Aggregate() {
				if hidden, err = hiddenCalcMembers(ctx, tx, ms, components); err != nil {
					return nil, err
				}
			}
		}
		if hidden > 0 {
			// An expression that reads a member by position (members[1].value.t,
			// .name, .id, or an input's list t[1]) could show, over a system
			// with a member the caller may not read, that member's own value:
			// withheld, the members counted. One that folds each list whole
			// (avg(t)) shows an aggregate, as the rule's state does.
			all = append(all, computed{row: CalcPreviewOwner{OwnerKind: o.owner.kind, OwnerID: o.owner.id, OwnerName: o.name, Missing: []string{},
				Error: fmt.Sprintf("withheld: %d of the system's members are outside your read scope, and an expression can read a member by position", hidden)}})
			continue
		}
		cost := members * calcInputWork(r.Inputs)
		if work+cost > calcPreviewWorkCap && i > 0 {
			pv.Truncated = true
			break
		}
		work += cost
		res, err := p.reduceWith(ctx, tx, r, o.owner, plan)
		if err != nil {
			return nil, err
		}
		row := CalcPreviewOwner{OwnerKind: o.owner.kind, OwnerID: o.owner.id, OwnerName: o.name, Missing: res.missing,
			NoMembers: res.noMembers, Error: res.err}
		if row.Missing == nil {
			row.Missing = []string{}
		}
		current, err := currentOutput(ctx, tx, r, o.owner)
		if err != nil {
			return nil, err
		}
		if current != nil {
			row.Current, _ = json.Marshal(current)
		}
		if res.value != nil && res.err == "" && len(res.missing) == 0 {
			row.Value, _ = json.Marshal(res.value)
			row.Differs = current == nil || current != res.value
		}
		all = append(all, computed{row: row, skipped: res.skipped, stale: res.stale})
		for _, m := range res.skipped {
			ids = append(ids, m.Component)
		}
		for _, s := range res.stale {
			ids = append(ids, s.Component)
		}
	}
	pv.OwnersJudged = len(all)
	// The rows lead with the owners holding a value or that the rule would
	// write one on, then by name.
	valued := func(o CalcPreviewOwner) int {
		if o.Current != nil || (o.Value != nil && o.Differs) {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(all, func(a, b computed) int {
		if d := valued(a.row) - valued(b.row); d != 0 {
			return d
		}
		return strings.Compare(a.row.OwnerName, b.row.OwnerName)
	})
	names, err := visibleComponentNames(ctx, tx, ids, components)
	if err != nil {
		return nil, err
	}
	for _, c := range all {
		row := c.row
		row.Skipped, row.SkippedHidden = nameSkipped(c.skipped, names)
		row.Stale, row.StaleHidden = nameStale(c.stale, names)
		pv.Owners = append(pv.Owners, row)
	}
	return pv, nil
}

// resolveSelectorInScope resolves a preview's selector names to ids within
// the caller's read scope, the way the event rule preview does: a name that
// is absent and one that is out of scope give the same refusal, and a name
// shared in and out of scope resolves among the rows in scope only.
func resolveSelectorInScope(ctx context.Context, q querier, r *CalcRule, components, systems scope.Set) error {
	ids := make([]string, 0, len(r.Selector.Components))
	for _, ref := range r.Selector.Components {
		c, err := scopedByNameInScope(ctx, q, componentConfig, ref, "component", components)
		if err != nil {
			return invalidCalc("selector", "component %q does not exist", ref)
		}
		ids = append(ids, c.ID)
	}
	r.Selector.Components = ids
	sysIDs := make([]string, 0, len(r.Selector.Systems))
	for _, ref := range r.Selector.Systems {
		s, err := scopedByNameInScope(ctx, q, systemConfig, ref, "system", systems)
		if err != nil {
			return invalidCalc("selector", "system %q does not exist", ref)
		}
		sysIDs = append(sysIDs, s.ID)
	}
	r.Selector.Systems = sysIDs
	return nil
}

type previewOwner struct {
	owner calcOwner
	name  string
}

// orderPreviewOwners sorts a preview's owners in place: those holding a
// current value of the output first, then by name, then id, so the cap
// never hides an owner whose existing value would change. One statement.
func orderPreviewOwners(ctx context.Context, tx pgx.Tx, r CalcRule, owners []previewOwner) error {
	if len(owners) == 0 {
		return nil
	}
	ids := make([]string, len(owners))
	for i, o := range owners {
		ids[i] = o.owner.id
	}
	table, typeCol := "property", "property_type_id"
	if r.OutputLane == rules.LaneMetric {
		table, typeCol = "metric", "metric_type_id"
	}
	col := calcOwner{kind: r.OwnerKind}.col()
	rows, err := tx.Query(ctx, `select o.id::text from unnest($1::uuid[]) as o(id)
		where exists (select 1 from `+table+` x where x.`+col+` = o.id and x.`+typeCol+` = $2::uuid and x.instance = ''
			and x.provenance in ('observed', 'calculated'))`, ids, r.outputID)
	if err != nil {
		return fmt.Errorf("storage: calc preview holders of the output: %w", err)
	}
	holds := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		holds[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rank := func(o previewOwner) int {
		if holds[o.owner.id] {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(owners, func(a, b previewOwner) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		if d := strings.Compare(a.name, b.name); d != 0 {
			return d
		}
		return strings.Compare(a.owner.id, b.owner.id)
	})
	return nil
}

// previewCalcOwners is the owners a candidate selects within the read
// scope, by id: a component rule's selected components holding a value of
// one of its inputs (the ones the tick would mark), a system rule's selected
// systems.
func previewCalcOwners(ctx context.Context, tx pgx.Tx, r CalcRule, components, systems scope.Set) ([]previewOwner, error) {
	if r.OwnerKind == rules.OwnerComponent {
		cands, err := previewCandidates(ctx, tx, r.Selector.components(), r.Selector.Components, components)
		if err != nil {
			return nil, err
		}
		ids := make([]string, len(cands))
		for i, c := range cands {
			ids[i] = c.id
		}
		metricIDs, propertyIDs := inputTypeIDs(r.Inputs)
		rows, err := tx.Query(ctx, `select c.id::text from unnest($1::uuid[]) as c(id)
			where exists (select 1 from metric m where m.component_id = c.id and m.metric_type_id = any($2::uuid[]))
				or exists (select 1 from property x where x.component_id = c.id and x.property_type_id = any($3::uuid[]))`, ids, metricIDs, propertyIDs)
		if err != nil {
			return nil, fmt.Errorf("storage: calc preview holders: %w", err)
		}
		holds := map[string]bool{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			holds[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		var out []previewOwner
		for _, c := range cands {
			if holds[c.id] {
				out = append(out, previewOwner{owner: calcOwner{kind: rules.OwnerComponent, id: c.id}, name: c.name})
			}
		}
		return out, nil
	}
	sel, err := json.Marshal(r.Selector)
	if err != nil {
		return nil, err
	}
	f := newReadFilter(systemTable, systems, 2)
	rows, err := tx.Query(ctx, f.With+` select s.id::text, s.name from system s, (select $1::jsonb as selector) r
		where `+systemSelectedSQL("r", "s")+` and `+f.Pred("s.id")+` order by s.id`, append([]any{sel}, f.Args...)...)
	if err != nil {
		return nil, fmt.Errorf("storage: calc preview selection: %w", err)
	}
	defer rows.Close()
	var out []previewOwner
	for rows.Next() {
		var o previewOwner
		o.owner.kind = rules.OwnerSystem
		if err := rows.Scan(&o.owner.id, &o.name); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CalcCounts is a rule's live state at a glance, over the owners the caller
// may read: owners holding a computed value, owners pending, owners whose
// last computation wrote nothing (an input missing, a system with no
// members), and owners whose last computation failed.
type CalcCounts struct {
	Computed, Pending, Unsupported, Errors int
}

// CalcRuleCounts counts every rule's live state, by rule id, over the
// owners the component and system read scopes admit: one statement per owner
// kind, so a list reads every rule's counts at once.
func (p *PG) CalcRuleCounts(ctx context.Context, components, systems scope.Set) (map[string]CalcCounts, error) {
	out := map[string]CalcCounts{}
	for _, kind := range []string{rules.OwnerComponent, rules.OwnerSystem} {
		table, vis := componentTable, components
		if kind == rules.OwnerSystem {
			table, vis = systemTable, systems
		}
		col := calcOwner{kind: kind}.col()
		f := newReadFilter(table, vis, 1)
		q := f.With + ` select r.id::text,
				(select count(*) from calc_rule_state s where s.rule_id = r.id and s.` + col + ` is not null and ` + f.Pred("s."+col) + `
					and s.value is not null and s.error = '' and s.missing = '[]'),
				(select count(*) from calc_pending d where d.rule_id = r.id and d.` + col + ` is not null and ` + f.Pred("d."+col) + `),
				(select count(*) from calc_rule_state s where s.rule_id = r.id and s.` + col + ` is not null and ` + f.Pred("s."+col) + `
					and s.error = '' and (s.missing <> '[]' or s.no_members_since is not null)),
				(select count(*) from calc_rule_state s where s.rule_id = r.id and s.` + col + ` is not null and ` + f.Pred("s."+col) + `
					and s.error <> '')
			from calc_rule r where r.owner_kind = '` + kind + `'`
		rows, err := p.pool.Query(ctx, q, f.Args...)
		if err != nil {
			return nil, fmt.Errorf("storage: calc rule counts: %w", err)
		}
		for rows.Next() {
			var id string
			var c CalcCounts
			if err := rows.Scan(&id, &c.Computed, &c.Pending, &c.Unsupported, &c.Errors); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = c
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
