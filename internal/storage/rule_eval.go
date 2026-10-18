package storage

import (
	"context"
	"fmt"
	"log/slog"

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
	return &ruleEvaluator{p: p, byKey: byKey, owner: map[string]*ruleOwner{}, types: map[string]uuid.UUID{}}, nil
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
// type it names.
func (e *ruleEvaluator) selects(ctx context.Context, tx pgx.Tx, r EventRule, o *ruleOwner) bool {
	sel := r.Selector
	if len(sel.Components) == 0 && len(sel.ComponentTypes) == 0 {
		return true
	}
	for _, id := range sel.Components {
		if id == o.comp.ID {
			return true
		}
	}
	if !o.hasTyp {
		return false
	}
	for _, name := range sel.ComponentTypes {
		accepted, ok := e.typeID(ctx, tx, name)
		if !ok {
			continue
		}
		if within, err := e.p.TypeIsWithin(ctx, o.typeID, accepted); err == nil && within {
			return true
		}
	}
	return false
}

// ruleDedupKey is the condition a rule's alarm is about on its owner: the
// rule, and the sample's instance when it has one, so a per-endpoint input
// holds one alarm per endpoint (ruling 5 on #819).
func ruleDedupKey(rule, instance string) string {
	if instance == "" {
		return rule
	}
	return rule + ":" + instance
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

// observe judges one landed sample. Only a component owns an alarm today, so
// any other owner is skipped. A sample a rule cannot evaluate (a value of
// the wrong type) is logged and skipped rather than failing the batch: an
// ingest is never refused for a rule's sake.
func (e *ruleEvaluator) observe(ctx context.Context, tx pgx.Tx, ownerKind, ownerID, key, instance string, value any) error {
	if e == nil || ownerKind != "component" {
		return nil
	}
	rs := e.byKey[key]
	if len(rs) == 0 {
		return nil
	}
	o, err := e.ownerFacts(ctx, tx, ownerID)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if !e.selects(ctx, tx, r, o) {
			continue
		}
		action, err := rules.Decide(r.FireCriteria, r.ClearCriteria, value)
		if err != nil {
			slog.Warn("event rule cannot evaluate a sample", "facility", "rules", "rule", r.Name, "component", o.comp.Name, "key", key, "error", err.Error())
			continue
		}
		dedup := ruleDedupKey(r.Name, instance)
		switch action {
		case rules.Fire:
			id := r.ID
			if _, err := e.p.raiseAlarmTx(ctx, tx, "", &o.comp, AlarmSpec{Severity: r.Severity, Message: ruleMessage(r), DedupKey: dedup}, &id); err != nil {
				return fmt.Errorf("storage: rule %s raise on %s: %w", r.Name, o.comp.Name, err)
			}
		case rules.Clear:
			if err := e.p.clearAlarmByDedupTx(ctx, tx, "", &o.comp, dedup); err != nil {
				return fmt.Errorf("storage: rule %s clear on %s: %w", r.Name, o.comp.Name, err)
			}
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
