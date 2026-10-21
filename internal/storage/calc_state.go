package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// CalcStaleRef is an instance a reducer skipped as stale.
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

// CalcRuleLive is a rule's state on the owners the caller may read, and how
// many of those owners are pending.
type CalcRuleLive struct {
	Owners       []CalcOwnerState
	PendingDepth int
}

// CalcRuleState reads a rule's live state (by name or uuid), fenced to the
// owners the component and system read scopes admit.
func (p *PG) CalcRuleState(ctx context.Context, ref string, components, systems scope.Set) (*CalcRuleLive, error) {
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
	for _, kind := range []string{rules.OwnerComponent, rules.OwnerSystem} {
		owner := calcOwner{kind: kind}
		table, vis := componentTable, components
		if kind == rules.OwnerSystem {
			table, vis = systemTable, systems
		}
		f := newReadFilter(table, vis, 2)
		col := owner.col()
		q := f.With + ` select o.id::text, o.name, s.value, s.computed_at, coalesce(s.rule_version, 0),
				coalesce(s.missing, '[]'), coalesce(s.skipped, '[]'), coalesce(s.stale, '[]'), coalesce(s.error, ''), s.no_members_since,
				pd.marked_at is not null, case when pd.marked_at > now() then pd.marked_at end
			from (select ` + col + ` as id from calc_rule_state where rule_id = $1::uuid and ` + col + ` is not null
				union select ` + col + ` from calc_pending where rule_id = $1::uuid and ` + col + ` is not null) k
			join ` + string(table) + ` o on o.id = k.id
			left join calc_rule_state s on s.rule_id = $1::uuid and s.` + col + ` = o.id
			left join calc_pending pd on pd.rule_id = $1::uuid and pd.` + col + ` = o.id
			where ` + f.Pred("o.id") + ` order by o.name, o.id`
		res, err := tx.Query(ctx, q, append([]any{r.ID}, f.Args...)...)
		if err != nil {
			return nil, fmt.Errorf("storage: calc rule %s state: %w", r.Name, err)
		}
		for res.Next() {
			var x stored
			var value, missing, skipped, stale []byte
			x.state.OwnerKind = kind
			if err := res.Scan(&x.state.OwnerID, &x.state.OwnerName, &value, &x.state.ComputedAt, &x.state.RuleVersion,
				&missing, &skipped, &stale, &x.state.Error, &x.state.NoMembersSince, &x.state.Pending, &x.state.DueAt); err != nil {
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
		}
		res.Close()
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
		live.Owners = append(live.Owners, st)
		// The pending depth is over the owners the caller may read: a mark
		// on an owner it may not is no fact it learns.
		if st.Pending {
			live.PendingDepth++
		}
	}
	return live, nil
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
	out, _, err := resolveSampleKey(ctx, tx, r.OutputKey)
	if err != nil {
		return nil, err
	}
	r.outputType = out.valueType
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
		members := 1
		if o.owner.kind == rules.OwnerSystem {
			ms, err := systemMembers(ctx, tx, o.owner.id)
			if err != nil {
				return nil, err
			}
			members = len(ms)
		}
		if work+members*len(r.Inputs) > calcPreviewWorkCap && i > 0 {
			pv.Truncated = true
			break
		}
		work += members * len(r.Inputs)
		res, err := p.reduceWith(ctx, tx, r, o.owner, plan)
		if err != nil {
			return nil, err
		}
		row := CalcPreviewOwner{OwnerKind: o.owner.kind, OwnerID: o.owner.id, OwnerName: o.name, Missing: res.missing,
			NoMembers: res.noMembers, Error: res.err}
		if row.Missing == nil {
			row.Missing = []string{}
		}
		current, _, err := readCalcInput(ctx, tx, o.owner, rules.TypedInput{Key: r.OutputKey, ValueType: r.outputType}, false)
		var bad *calcValueError
		if err != nil && !errors.As(err, &bad) {
			return nil, err
		}
		if len(current) == 1 {
			row.Current, _ = json.Marshal(current[0])
		}
		if res.value != nil && res.err == "" && len(res.missing) == 0 {
			row.Value, _ = json.Marshal(res.value)
			row.Differs = len(current) != 1 || current[0] != res.value
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
		keys := inputKeys(r.Inputs)
		rows, err := tx.Query(ctx, `with mt as (select id from metric_type where name = any($2::text[])),
				pt as (select id from property_type where name = any($2::text[]))
			select c.id::text from unnest($1::uuid[]) as c(id)
			where exists (select 1 from metric m, mt where m.component_id = c.id and m.metric_type_id = mt.id)
				or exists (select 1 from property x, pt where x.component_id = c.id and x.property_type_id = pt.id)`, ids, keys)
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
