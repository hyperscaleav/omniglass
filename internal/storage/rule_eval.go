package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

// ruleEvaluator judges one ingest batch's samples against the enabled event
// rules that read their keys (#919), raising and clearing alarms in the same
// transaction as the samples, through the same guarded writes a hand raise
// and clear use. It loads the rules once per batch and each owner's facts
// once, so a batch whose keys no rule reads costs one query and nothing more.
type ruleEvaluator struct {
	p     *PG
	byKey map[string][]EventRule
	owner map[string]*ruleOwner
	types map[string]uuid.UUID
	// picks caches whether a rule selects an owner, per batch.
	picks map[[2]string]bool
}

// ruleOwner is what a selector needs to know about a component.
type ruleOwner struct {
	comp   Component
	typeID uuid.UUID
	hasTyp bool
}

// newRuleEvaluator loads the enabled rules reading any of keys; nil when no
// rule reads them, so the caller skips evaluation entirely.
func (p *PG) newRuleEvaluator(ctx context.Context, tx pgx.Tx, keys []string) (*ruleEvaluator, error) {
	rows, err := tx.Query(ctx, `select `+eventRuleCols+` from event_rule where enabled and sample_key = any($1)`, keys)
	if err != nil {
		return nil, fmt.Errorf("storage: load event rules: %w", err)
	}
	byKey := map[string][]EventRule{}
	for rows.Next() {
		r, err := scanEventRule(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		byKey[r.SampleKey] = append(byKey[r.SampleKey], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(byKey) == 0 {
		return nil, nil
	}
	return newRuleMatcher(p, byKey), nil
}

// newRuleMatcher is an evaluator's selector machinery, usable on its own to
// ask which components a rule watches.
func newRuleMatcher(p *PG, byKey map[string][]EventRule) *ruleEvaluator {
	return &ruleEvaluator{p: p, byKey: byKey, owner: map[string]*ruleOwner{}, types: map[string]uuid.UUID{}, picks: map[[2]string]bool{}}
}

func (e *ruleEvaluator) ownerFacts(ctx context.Context, tx pgx.Tx, componentID string) (*ruleOwner, error) {
	if o, ok := e.owner[componentID]; ok {
		return o, nil
	}
	o := &ruleOwner{comp: Component{ID: componentID}}
	var typeID *string
	if err := tx.QueryRow(ctx, `select c.name, p.component_type_id::text from component c
		left join product p on p.id = c.product_id where c.id = $1::uuid`, componentID).Scan(&o.comp.Name, &typeID); err != nil {
		return nil, fmt.Errorf("storage: rule owner %s: %w", componentID, err)
	}
	if typeID != nil {
		if id, err := uuid.Parse(*typeID); err == nil {
			o.typeID, o.hasTyp = id, true
		}
	}
	e.owner[componentID] = o
	return o, nil
}

func (e *ruleEvaluator) typeID(ctx context.Context, tx pgx.Tx, name string) (uuid.UUID, bool) {
	if id, ok := e.types[name]; ok {
		return id, true
	}
	var s string
	if err := tx.QueryRow(ctx, `select id::text from component_type where name = $1`, name).Scan(&s); err != nil {
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.UUID{}, false
	}
	e.types[name] = id
	return id, true
}

// selects reports whether a rule watches a component: every component when
// its selector is empty, else one it names or one whose type falls within a
// type it names. Cached per batch; the type walk runs in the transaction and
// its error is returned, so a fault never silently skips a fire or a clear.
func (e *ruleEvaluator) selects(ctx context.Context, tx pgx.Tx, r EventRule, o *ruleOwner) (bool, error) {
	key := [2]string{r.ID, o.comp.ID}
	if v, ok := e.picks[key]; ok {
		return v, nil
	}
	v, err := e.pick(ctx, tx, r, o)
	if err != nil {
		return false, err
	}
	e.picks[key] = v
	return v, nil
}

func (e *ruleEvaluator) pick(ctx context.Context, tx pgx.Tx, r EventRule, o *ruleOwner) (bool, error) {
	sel := r.Selector
	if len(sel.Components) == 0 && len(sel.ComponentTypes) == 0 {
		return true, nil
	}
	for _, id := range sel.Components {
		if id == o.comp.ID {
			return true, nil
		}
	}
	if !o.hasTyp {
		return false, nil
	}
	for _, name := range sel.ComponentTypes {
		accepted, ok := e.typeID(ctx, tx, name)
		if !ok {
			continue // the type was deleted: the rule matches nothing by it
		}
		var within bool
		if err := tx.QueryRow(ctx, `with recursive up(id, parent_id) as (
				select id, parent_id from component_type where id = $1
				union all
				select ct.id, ct.parent_id from component_type ct join up on ct.id = up.parent_id
			) cycle id set is_cycle using path
			select exists (select 1 from up where id = $2)`, o.typeID, accepted).Scan(&within); err != nil {
			return false, fmt.Errorf("storage: rule type match: %w", err)
		}
		if within {
			return true, nil
		}
	}
	return false, nil
}

// ruleDedupKey is the condition a rule's alarm is about on its owner: the
// rule, and the sample's instance when it has one, so a per-endpoint input
// holds one alarm per endpoint (ruling 5 on #819).
//
// The "rule:" prefix keeps a rule's conditions out of the namespace a hand
// raise uses (whose key defaults to its message), so an operator's alarm
// never collides with, or is cleared by, a rule's.
func ruleDedupKey(rule, instance string) string {
	if instance == "" {
		return "rule:" + rule
	}
	return "rule:" + rule + ":" + instance
}

// ruleMessage is the alarm's message: the rule's message, else its label,
// else its name.
func ruleMessage(r EventRule) string {
	switch {
	case r.Message != "":
		return r.Message
	case r.Label != "":
		return r.Label
	}
	return r.Name
}

// observe judges one landed sample, under a savepoint: a rule that fails
// (a deadlock, a recompute fault) is logged and rolled back to the savepoint,
// and the sample itself still lands. An ingest is never refused for a rule's
// sake.
func (e *ruleEvaluator) observe(ctx context.Context, tx pgx.Tx, lane rules.Lane, ownerKind, ownerID, key, instance string, ts time.Time, value any) error {
	if e == nil || ownerKind != "component" || len(e.byKey[key]) == 0 {
		return nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: rule savepoint: %w", err)
	}
	if err := e.judge(ctx, sp, lane, ownerID, key, instance, ts, value); err != nil {
		_ = sp.Rollback(ctx)
		slog.Warn("event rule evaluation failed; the sample landed, the rule did not act", "facility", "rules",
			"component", ownerID, "key", key, "instance", instance, "error", err.Error())
		return nil
	}
	return sp.Commit(ctx)
}

// judge is observe's work. Only a component owns an alarm today. A sample
// older than its series' newest is history and decides nothing: a late or
// redelivered down after an up must not reopen an incident nothing will
// clear. A sample a rule cannot evaluate (the wrong type) is logged and
// skipped.
func (e *ruleEvaluator) judge(ctx context.Context, tx pgx.Tx, lane rules.Lane, ownerID, key, instance string, ts time.Time, value any) error {
	table, typeTable := "property", "property_type"
	if lane == rules.LaneMetric {
		table, typeTable = "metric", "metric_type"
	}
	var stale bool
	if err := tx.QueryRow(ctx, fmt.Sprintf(`select exists (select 1 from %s s join %s t on t.id = s.%s_id
		where s.component_id = $1::uuid and t.name = $2 and s.instance = $3 and s.ts > $4)`, table, typeTable, typeTable),
		ownerID, key, instance, ts).Scan(&stale); err != nil {
		return fmt.Errorf("storage: rule newest check: %w", err)
	}
	if stale {
		return nil
	}
	o, err := e.ownerFacts(ctx, tx, ownerID)
	if err != nil {
		return err
	}
	for _, r := range e.byKey[key] {
		picked, err := e.selects(ctx, tx, r, o)
		if err != nil {
			return err
		}
		if !picked {
			continue
		}
		action, err := rules.Decide(r.FireCriteria, r.ClearCriteria, value)
		if err != nil {
			slog.Warn("event rule cannot evaluate a sample", "facility", "rules", "rule", r.Name, "component", o.comp.Name, "key", key, "error", err.Error())
			continue
		}
		dedup := ruleDedupKey(r.Name, instance)
		id := r.ID
		switch action {
		case rules.Fire:
			if _, err := e.p.raiseAlarmTx(ctx, tx, "", &o.comp, AlarmSpec{Severity: r.Severity, Message: ruleMessage(r), DedupKey: dedup}, &id); err != nil {
				return fmt.Errorf("storage: rule %s raise on %s: %w", r.Name, o.comp.Name, err)
			}
		case rules.Clear:
			if err := e.p.clearAlarmByDedupTx(ctx, tx, "", &o.comp, dedup, &id); err != nil {
				return fmt.Errorf("storage: rule %s clear on %s: %w", r.Name, o.comp.Name, err)
			}
		}
	}
	return nil
}

// clearRuleAlarms clears a rule's open alarms on every component keep
// refuses (nil keeps none): a rule disabled or deleted lets go of what it
// holds, and one narrowed lets go of what it no longer watches.
func (p *PG) clearRuleAlarms(ctx context.Context, tx pgx.Tx, actorID string, r EventRule, keep func(*ruleOwner) (bool, error)) error {
	rows, err := tx.Query(ctx, `select component_id::text, dedup_key from alarm where raised_by_rule_id = $1 and cleared_at is null`, r.ID)
	if err != nil {
		return fmt.Errorf("storage: rule %s open alarms: %w", r.Name, err)
	}
	type open struct{ component, dedup string }
	var opens []open
	for rows.Next() {
		var o open
		if err := rows.Scan(&o.component, &o.dedup); err != nil {
			rows.Close()
			return err
		}
		opens = append(opens, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	m := newRuleMatcher(p, nil)
	id := r.ID
	for _, a := range opens {
		o, err := m.ownerFacts(ctx, tx, a.component)
		if err != nil {
			return err
		}
		if keep != nil {
			kept, err := keep(o)
			if err != nil {
				return err
			}
			if kept {
				continue
			}
		}
		if err := p.clearAlarmByDedupTx(ctx, tx, actorID, &o.comp, a.dedup, &id); err != nil {
			return err
		}
	}
	return nil
}

// sampleKeys lists a batch's distinct keys.
func sampleKeys[T any](evs []T, key func(T) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ev := range evs {
		k := key(ev)
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
