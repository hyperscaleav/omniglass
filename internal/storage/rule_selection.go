package storage

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// componentSelection is what an event rule selects a component by, read at
// one point of a write (#2475): its id, and its type's lineage, the in-effect
// names of its type and every ancestor (none when its product names no
// type). A write that moves a component between types (its product, its
// product's type, a type's parent) reads one before and one after, and
// reselectComponentRules acts on the difference.
type componentSelection struct {
	componentID string
	lineage     []string
}

// readComponentSelection reads a component's selection now, in the write's
// transaction. The lineage walk is the one the evaluator's matcher (pick)
// takes, up component_type.parent_id from the product's type, and keeps the
// names a selector's names resolve to (component_type_in_effect), so a name
// is in the lineage exactly when pick would accept it.
func readComponentSelection(ctx context.Context, tx pgx.Tx, componentID string) (componentSelection, error) {
	rows, err := tx.Query(ctx, `with recursive up(id, parent_id) as (
			select ct.id, ct.parent_id from component c join product p on p.id = c.product_id
				join component_type ct on ct.id = p.component_type_id where c.id = $1::uuid
			union all
			select ct.id, ct.parent_id from component_type ct join up on ct.id = up.parent_id
		) cycle id set is_cycle using path
		select distinct t.name from up join component_type_in_effect() t on t.id = up.id where not up.is_cycle
		order by t.name`, componentID)
	if err != nil {
		return componentSelection{}, fmt.Errorf("storage: rule selection of %s: %w", componentID, err)
	}
	lineage, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return componentSelection{}, fmt.Errorf("storage: rule selection of %s: %w", componentID, err)
	}
	return componentSelection{componentID: componentID, lineage: lineage}, nil
}

// selectedBy reports whether a rule's selector takes the component: every
// component when it is empty, else one it lists by id or one whose lineage
// holds a type it names. It is the matcher's rule (pick) on a captured
// selection, so a before read ahead of a write still answers after it.
func (s componentSelection) selectedBy(sel EventRuleSelector) bool {
	if len(sel.Components) == 0 && len(sel.ComponentTypes) == 0 {
		return true
	}
	if slices.Contains(sel.Components, s.componentID) {
		return true
	}
	for _, name := range sel.ComponentTypes {
		if slices.Contains(s.lineage, name) {
			return true
		}
	}
	return false
}

// reselectNames is the type names a reselect bounds its rules by: both
// lineages, sorted, each once.
func reselectNames(before, after componentSelection) []string {
	names := slices.Concat(before.lineage, after.lineage)
	slices.Sort(names)
	return slices.Compact(names)
}

// reselectCandidates reads the enabled event rules whose selector names one
// of names, FOR UPDATE in id order, as a rule write does (the lock order
// newRuleEvaluator documents). An ingest holds the rules it judges by FOR
// SHARE for its batch, so a share lock here would not wait for a batch that
// read the component's old type: its firing on a rule the component left
// would commit after the release saw nothing to end, and stay stranded,
// and a sample it judged under the old type would go unprimed by a rule the
// component came into. FOR UPDATE waits for that batch and then sees what it
// wrote; a batch starting after it waits in turn and reads the new type.
// Only such a rule can select a component differently before and
// after a type move: one selecting every component, or one listing it by id,
// selects it on both sides. So a move never reads, or re-primes, any other.
func reselectCandidates(ctx context.Context, tx pgx.Tx, names []string) ([]EventRule, error) {
	if len(names) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `select `+eventRuleCols+` from event_rule
		where enabled and coalesce(selector->'component_types', '[]'::jsonb) ?| $1::text[]
		order by id for update`, names)
	if err != nil {
		return nil, fmt.Errorf("storage: reselect event rules: %w", err)
	}
	defer rows.Close()
	var out []EventRule
	for rows.Next() {
		r, err := scanEventRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// reselectComponentRules re-judges one component's event rules after a write
// that changed its selection (#2475), inside the write's transaction, the
// event rules' analogue of markComponentCalcRules. before and after are the
// component's selection either side of the write (readComponentSelection).
// Of the rules naming a type in either lineage (reselectCandidates):
//
//   - one that now selects the component and did not before judges the
//     newest sample of each of the component's series of its input, as
//     primeRule does for a whole selector, so a condition that already holds
//     on a transition-only input opens (after the rule's sustain, through the
//     sweep) rather than waiting on a sample that will not come;
//   - one that no longer selects it ends its conditions there (each firing
//     with its clear event and the alarm it resolves, as a narrowed selector
//     does) and drops its streaks there, so a sustain under way never fires;
//   - one that selected it before and after is left alone: no second
//     condition, no second fire event.
//
// actorID is the writer the ends' alarm audit rows name ("" the system).
// Nothing is published: the step's writes are the evaluator's own, so a lab
// run with no bus sees them.
func (p *PG) reselectComponentRules(ctx context.Context, tx pgx.Tx, actorID string, before, after componentSelection) error {
	if before.componentID != after.componentID {
		return fmt.Errorf("storage: reselect rules across two components (%s, %s)", before.componentID, after.componentID)
	}
	candidates, err := reselectCandidates(ctx, tx, reselectNames(before, after))
	if err != nil || len(candidates) == 0 {
		return err
	}
	now, err := dbNow(ctx, tx)
	if err != nil {
		return err
	}
	m := newRuleMatcher(p, nil)
	o, err := m.ownerFacts(ctx, tx, after.componentID)
	if err != nil {
		return err
	}
	for _, r := range candidates {
		was, is := before.selectedBy(r.Selector), after.selectedBy(r.Selector)
		switch {
		case is && !was:
			if err := p.primeComponent(ctx, tx, r, o, now); err != nil {
				return err
			}
		case was && !is:
			if err := p.releaseComponent(ctx, tx, actorID, r, o.comp.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// primeComponent judges the newest sample of each of a component's series
// of a rule's input by the rule, at now: primeRule for one component.
func (p *PG) primeComponent(ctx context.Context, tx pgx.Tx, r EventRule, o *ruleOwner, now time.Time) error {
	series, err := p.newestSamplesOfComponent(ctx, tx, r.SampleLane, r.SampleKey, o.comp.ID)
	if err != nil {
		return err
	}
	for _, s := range series {
		if err := p.primeSeries(ctx, tx, r, o, s, now); err != nil {
			return err
		}
	}
	return nil
}

// releaseComponent ends a rule's conditions on one component it no longer
// selects (endRuleFirings for one component) and drops the rule's streaks
// there, each under its series lock.
func (p *PG) releaseComponent(ctx context.Context, tx pgx.Tx, actorID string, r EventRule, componentID string) error {
	if err := p.endFiringsOn(ctx, tx, actorID, r, componentID, nil); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `select instance from event_rule_state where rule_id = $1::uuid and component_id = $2::uuid
		order by instance`, r.ID, componentID)
	if err != nil {
		return fmt.Errorf("storage: rule %s streaks: %w", r.Name, err)
	}
	instances, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("storage: rule %s streaks: %w", r.Name, err)
	}
	for _, instance := range instances {
		if err := lockAdvisory(ctx, tx, ruleSeriesLockKey(r.ID, componentID, instance)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from event_rule_state where rule_id = $1::uuid and component_id = $2::uuid and instance = $3`,
			r.ID, componentID, instance); err != nil {
			return fmt.Errorf("storage: rule %s drop streak: %w", r.Name, err)
		}
	}
	return nil
}
