package storage

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

// ruleSelection is what a component offers an event rule's selector at one
// moment (#2475): the ids of its component type and every ancestor of it,
// none when its product names no type. A write that moves the component's
// type (its product, the product's type, a type's parent) reads it before
// and after, and hands both to reselectComponentRules.
type ruleSelection []string

// componentRuleSelection reads a component's rule selection inside tx: its
// type by its product, then the parent walk a selector's type match takes
// (ruleEvaluator.pick), cycle-safe.
func componentRuleSelection(ctx context.Context, tx pgx.Tx, componentID string) (ruleSelection, error) {
	var ids []string
	if err := tx.QueryRow(ctx, `with recursive up(id, parent_id) as (
			select ct.id, ct.parent_id from component c join product p on p.id = c.product_id
				join component_type ct on ct.id = p.component_type_id
			where c.id = $1::uuid
			union all
			select ct.id, ct.parent_id from component_type ct join up on ct.id = up.parent_id
		) cycle id set is_cycle using path
		select coalesce(array_agg(distinct id::text order by id::text), '{}') from up`, componentID).Scan(&ids); err != nil {
		return nil, fmt.Errorf("storage: component %s rule selection: %w", componentID, err)
	}
	return ruleSelection(ids), nil
}

// reselectComponentRules re-judges one component's event rules after a
// write changed its rule selection from before to after (#2475), in the
// writer's transaction, so a rule's conditions follow a component across a
// type change rather than waiting on a sample a transition-only input will
// never send:
//
//   - a rule that now selects the component and did not before primes there,
//     judging the newest sample of each of the component's series of its
//     input as primeRule does for a whole selector, so a condition that
//     already holds opens (and its alarm, after the rule's sustain);
//   - a rule that selected it and no longer does ends its conditions there
//     and drops its streaks there, as a narrowed selector does;
//   - a rule that selected it before and after is left alone, so no
//     condition or event is written twice.
//
// The rules considered are the enabled ones whose selector names a type in
// before or after (the component's old or new type, or an ancestor of
// either): a rule that selects by component, or every component, selects it
// the same on both sides of a type change, and no other rule is read. They
// are read FOR SHARE, the first lock this step takes, in the order
// newRuleEvaluator documents (the rule's row, then the series lock), so a
// rule write serializes with it the way it does with an ingest.
//
// actorID is the writer, named on the audit of any alarm a leaving rule
// resolves. No bus message is sent: the step is the whole of it.
func (p *PG) reselectComponentRules(ctx context.Context, tx pgx.Tx, actorID, componentID string, before, after ruleSelection) error {
	if slices.Equal(before, after) {
		return nil
	}
	touched := slices.Concat(before, after)
	if len(touched) == 0 {
		return nil
	}
	found, err := rulesNamingTypes(ctx, tx, touched)
	if err != nil || len(found) == 0 {
		return err
	}
	now, err := dbNow(ctx, tx)
	if err != nil {
		return err
	}
	m := newRuleMatcher(p, nil)
	o, err := m.ownerFacts(ctx, tx, componentID)
	if err != nil {
		return err
	}
	for _, r := range found {
		was := m.selectedBy(ctx, tx, r, componentID, before)
		is := m.selectedBy(ctx, tx, r, componentID, after)
		switch {
		case is && !was:
			if err := p.enterRule(ctx, tx, r, o, now); err != nil {
				return err
			}
		case was && !is:
			if err := p.leaveRule(ctx, tx, actorID, r, componentID); err != nil {
				return err
			}
		}
	}
	return nil
}

// rulesNamingTypes reads, FOR SHARE and in id order, the enabled event rules
// whose selector names a type (by name, as the catalog resolves it) among
// typeIDs.
func rulesNamingTypes(ctx context.Context, tx pgx.Tx, typeIDs []string) ([]EventRule, error) {
	rows, err := tx.Query(ctx, `select `+ruleCols("r.")+` from event_rule r
		where r.enabled and exists (
			select 1 from jsonb_array_elements_text(coalesce(r.selector->'component_types', '[]'::jsonb)) n(name)
			join component_type_in_effect() t on t.name = n.name
			where t.id = any($1::uuid[]))
		order by r.id for share of r`, typeIDs)
	if err != nil {
		return nil, fmt.Errorf("storage: find event rules by type: %w", err)
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

// selectedBy reports whether a rule's selector picks a component offering
// sel: the component by id, or a type it names (as the catalog resolves the
// name) among sel's. An empty selector picks every component.
func (e *ruleEvaluator) selectedBy(ctx context.Context, tx pgx.Tx, r EventRule, componentID string, sel ruleSelection) bool {
	if len(r.Selector.Components) == 0 && len(r.Selector.ComponentTypes) == 0 {
		return true
	}
	if slices.Contains(r.Selector.Components, componentID) {
		return true
	}
	for _, name := range r.Selector.ComponentTypes {
		id, ok := e.typeID(ctx, tx, name)
		if ok && slices.Contains(sel, id.String()) {
			return true
		}
	}
	return false
}

// enterRule primes a rule on a component it came to select: any streak a
// past selection left there is dropped, then each series of the rule's
// input on the component is judged at now by its newest sample.
func (p *PG) enterRule(ctx context.Context, tx pgx.Tx, r EventRule, o *ruleOwner, now time.Time) error {
	if err := dropComponentStreaks(ctx, tx, r, o.comp.ID); err != nil {
		return err
	}
	days := ""
	if r.SampleLane == rules.LaneMetric {
		var err error
		if days, err = p.metricLane(ctx, tx); err != nil {
			return err
		}
	}
	series, err := newestKeySamples(ctx, tx, r.SampleLane, r.SampleKey, o.comp.ID, days)
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

// leaveRule lets a rule go of a component it no longer selects: its
// conditions there end (each with its clear event and the alarm it
// resolves), any alarm it holds there with no condition resolves, and its
// streaks there are dropped.
func (p *PG) leaveRule(ctx context.Context, tx pgx.Tx, actorID string, r EventRule, componentID string) error {
	if err := p.endRuleFiringsOn(ctx, tx, actorID, r, componentID, nil); err != nil {
		return err
	}
	return dropComponentStreaks(ctx, tx, r, componentID)
}

// dropComponentStreaks deletes a rule's streaks on one component, each
// under its series lock, so it serializes with a sweep acting on it.
func dropComponentStreaks(ctx context.Context, tx pgx.Tx, r EventRule, componentID string) error {
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
		if _, err := tx.Exec(ctx, `delete from event_rule_state where rule_id = $1::uuid and component_id = $2::uuid
			and instance = $3`, r.ID, componentID, instance); err != nil {
			return fmt.Errorf("storage: rule %s drop streak: %w", r.Name, err)
		}
	}
	return nil
}
