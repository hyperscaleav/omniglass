package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
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
		if err := e.p.stepRule(ctx, tx, r, &o.comp, instance, action, ts); err != nil {
			return err
		}
	}
	return nil
}

// stepRule advances a rule's streak on one component and instance by a
// sample's decision and applies whatever takes effect (#920). An action
// already in force (a fire with the alarm open, a clear with none) has
// nothing to wait for and breaks any contrary streak.
func (p *PG) stepRule(ctx context.Context, tx pgx.Tx, r EventRule, comp *Component, instance string, action rules.Action, ts time.Time) error {
	dedup := ruleDedupKey(r.Name, instance)
	var isOpen bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from alarm where component_id = $1::uuid and dedup_key = $2
		and cleared_at is null and raised_by_rule_id = $3::uuid)`, comp.ID, dedup, r.ID).Scan(&isOpen); err != nil {
		return fmt.Errorf("storage: rule %s open alarm: %w", r.Name, err)
	}
	if (action == rules.Fire && isOpen) || (action == rules.Clear && !isOpen) {
		action = rules.Hold
	}
	prev, err := loadStreak(ctx, tx, r.ID, comp.ID, instance)
	if err != nil {
		return err
	}
	next, effect := rules.Step(prev, action, ts, r.FireSustain, r.ClearSustain)
	if err := saveStreak(ctx, tx, r.ID, comp.ID, instance, next); err != nil {
		return err
	}
	return p.applyRule(ctx, tx, r, comp, dedup, effect)
}

// applyRule raises or clears a rule's alarm on a component.
func (p *PG) applyRule(ctx context.Context, tx pgx.Tx, r EventRule, comp *Component, dedup string, effect rules.Action) error {
	id := r.ID
	switch effect {
	case rules.Fire:
		if _, err := p.raiseAlarmTx(ctx, tx, "", comp, AlarmSpec{Severity: r.Severity, Message: ruleMessage(r), DedupKey: dedup}, &id); err != nil {
			return fmt.Errorf("storage: rule %s raise on %s: %w", r.Name, comp.Name, err)
		}
	case rules.Clear:
		if err := p.clearAlarmByDedupTx(ctx, tx, "", comp, dedup, &id); err != nil {
			return fmt.Errorf("storage: rule %s clear on %s: %w", r.Name, comp.Name, err)
		}
	}
	return nil
}

func pendingWord(a rules.Action) string {
	if a == rules.Fire {
		return "fire"
	}
	return "clear"
}

func loadStreak(ctx context.Context, tx pgx.Tx, ruleID, componentID, instance string) (rules.Streak, error) {
	var pending string
	var since time.Time
	err := tx.QueryRow(ctx, `select pending, since from event_rule_state
		where rule_id = $1::uuid and component_id = $2::uuid and instance = $3`, ruleID, componentID, instance).Scan(&pending, &since)
	if errors.Is(err, pgx.ErrNoRows) {
		return rules.Streak{}, nil
	}
	if err != nil {
		return rules.Streak{}, fmt.Errorf("storage: rule streak: %w", err)
	}
	s := rules.Streak{Pending: rules.Clear, Since: since}
	if pending == "fire" {
		s.Pending = rules.Fire
	}
	return s, nil
}

// saveStreak writes a pending streak, or removes the row when none is.
func saveStreak(ctx context.Context, tx pgx.Tx, ruleID, componentID, instance string, s rules.Streak) error {
	if s.Pending == rules.Hold {
		_, err := tx.Exec(ctx, `delete from event_rule_state where rule_id = $1::uuid and component_id = $2::uuid and instance = $3`,
			ruleID, componentID, instance)
		return err
	}
	_, err := tx.Exec(ctx, `insert into event_rule_state (rule_id, component_id, instance, pending, since)
		values ($1::uuid, $2::uuid, $3, $4, $5)
		on conflict (rule_id, component_id, instance) do update set pending = excluded.pending, since = excluded.since`,
		ruleID, componentID, instance, pendingWord(s.Pending), s.Since)
	return err
}

// SweepEventRules acts on every pending streak that has held for its rule's
// sustain at now (#920): the clock for an input that sends no further sample
// (a transition-only verdict that went down and stays down). A streak no
// contrary sample broke is, by construction, still holding. Each streak acts
// under a savepoint, so one failure does not stop the rest.
func (p *PG) SweepEventRules(ctx context.Context, now time.Time) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin rule sweep: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `delete from event_rule_state s using event_rule r where r.id = s.rule_id and not r.enabled`); err != nil {
		return fmt.Errorf("storage: rule sweep prune: %w", err)
	}
	rows, err := tx.Query(ctx, `select s.component_id::text, s.instance, s.pending, s.since, `+ruleCols("r.")+`
		from event_rule_state s join event_rule r on r.id = s.rule_id`)
	if err != nil {
		return fmt.Errorf("storage: rule sweep: %w", err)
	}
	type due struct {
		component, instance string
		streak              rules.Streak
		rule                EventRule
	}
	var dues []due
	for rows.Next() {
		var d due
		var pending string
		var since time.Time
		r, err := scanEventRuleAfter(rows, &d.component, &d.instance, &pending, &since)
		if err != nil {
			rows.Close()
			return err
		}
		d.rule = r
		d.streak = rules.Streak{Pending: rules.Clear, Since: since}
		if pending == "fire" {
			d.streak.Pending = rules.Fire
		}
		if d.streak.Due(now, r.FireSustain, r.ClearSustain) {
			dues = append(dues, d)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	m := newRuleMatcher(p, nil)
	for _, d := range dues {
		sp, err := tx.Begin(ctx)
		if err != nil {
			return fmt.Errorf("storage: rule sweep savepoint: %w", err)
		}
		err = func() error {
			o, err := m.ownerFacts(ctx, sp, d.component)
			if err != nil {
				return err
			}
			if err := saveStreak(ctx, sp, d.rule.ID, d.component, d.instance, rules.Streak{}); err != nil {
				return err
			}
			return p.applyRule(ctx, sp, d.rule, &o.comp, ruleDedupKey(d.rule.Name, d.instance), d.streak.Pending)
		}()
		if err != nil {
			_ = sp.Rollback(ctx)
			slog.Warn("event rule sweep could not act on a streak", "facility", "rules", "rule", d.rule.Name, "component", d.component, "error", err.Error())
			continue
		}
		if err := sp.Commit(ctx); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
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

// RuleOpenAlarm is an open alarm a rule raised, on one component and
// instance.
type RuleOpenAlarm struct {
	AlarmID       string
	ComponentID   string
	ComponentName string
	Instance      string
	Severity      string
	RaisedAt      time.Time
}

// RuleStreak is a rule's pending streak on one component and instance.
type RuleStreak struct {
	ComponentID   string
	ComponentName string
	Instance      string
	Pending       string // fire, clear
	Since         time.Time
}

// EventRuleLive is a rule's live state: what it holds open and what it is
// waiting on.
type EventRuleLive struct {
	Open    []RuleOpenAlarm
	Pending []RuleStreak
}

// EventRuleState reads a rule's live state by name (fleet-wide config: an
// all-scope reader only).
func (p *PG) EventRuleState(ctx context.Context, name string, read scope.Set) (*EventRuleLive, error) {
	r, err := p.GetEventRule(ctx, name, read)
	if err != nil {
		return nil, err
	}
	prefix := ruleDedupKey(r.Name, "")
	out := &EventRuleLive{Open: []RuleOpenAlarm{}, Pending: []RuleStreak{}}
	rows, err := p.pool.Query(ctx, `select a.id::text, a.component_id::text, c.name, a.dedup_key, a.severity, a.raised_at
		from alarm a join component c on c.id = a.component_id
		where a.raised_by_rule_id = $1 and a.cleared_at is null order by a.raised_at`, r.ID)
	if err != nil {
		return nil, fmt.Errorf("storage: rule %s open alarms: %w", name, err)
	}
	for rows.Next() {
		var o RuleOpenAlarm
		var dedup string
		if err := rows.Scan(&o.AlarmID, &o.ComponentID, &o.ComponentName, &dedup, &o.Severity, &o.RaisedAt); err != nil {
			rows.Close()
			return nil, err
		}
		o.Instance = strings.TrimPrefix(strings.TrimPrefix(dedup, prefix), ":")
		out.Open = append(out.Open, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = p.pool.Query(ctx, `select s.component_id::text, c.name, s.instance, s.pending, s.since
		from event_rule_state s join component c on c.id = s.component_id
		where s.rule_id = $1 order by s.since`, r.ID)
	if err != nil {
		return nil, fmt.Errorf("storage: rule %s streaks: %w", name, err)
	}
	defer rows.Close()
	for rows.Next() {
		var s RuleStreak
		if err := rows.Scan(&s.ComponentID, &s.ComponentName, &s.Instance, &s.Pending, &s.Since); err != nil {
			return nil, err
		}
		out.Pending = append(out.Pending, s)
	}
	return out, rows.Err()
}
