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
	// now is the transaction's clock: the receipt time a streak starts at.
	now time.Time
}

// ruleOwner is what a selector needs to know about a component.
type ruleOwner struct {
	comp   Component
	typeID uuid.UUID
	hasTyp bool
}

// newRuleEvaluator loads the enabled rules reading any of keys; nil when no
// rule reads them, so the caller skips evaluation entirely.
//
// It loads them FOR SHARE, holding each rule's row for the rest of the
// ingest transaction, so a rule write (which takes the row FOR UPDATE)
// serializes with every batch that judges by it: a disable, delete or
// narrowing either commits before the load (and the batch judges by what it
// left) or waits for the batch to commit (and its settle sees, and clears,
// whatever the batch raised). The rules a batch judges by are therefore the
// current version for the whole batch, with no re-check needed per sample.
//
// Lock order, the same on every path that acts on a rule's alarms: the
// rule's row first (FOR SHARE here and in the sweep, FOR UPDATE in a rule
// write), then the series lock (ruleSeriesLockKey), then the health locks a
// raise or clear takes. The row lock is the first lock an ingest takes, so
// it never waits on a rule while holding a series or health lock.
func (p *PG) newRuleEvaluator(ctx context.Context, tx pgx.Tx, keys []string) (*ruleEvaluator, error) {
	rows, err := tx.Query(ctx, `select `+eventRuleCols+` from event_rule where enabled and sample_key = any($1)
		order by id for share`, keys)
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
	now, err := dbNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	e := newRuleMatcher(p, byKey)
	e.now = now
	return e, nil
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
// breaks any streak it held, since nothing it says can continue one.
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
			action = rules.Hold
		}
		if err := e.p.stepRule(ctx, tx, r, &o.comp, instance, action, e.now); err != nil {
			return err
		}
	}
	return nil
}

// stepRule advances a rule's streak on one component and instance by a
// decision received at now (the server's clock) and applies whatever takes
// effect (#920). An action already in force (a fire with the alarm open, a
// clear with none) has nothing to wait for and breaks any contrary streak.
//
// It holds the series' rule lock for the rest of the transaction before it
// reads the open alarm or the streak, so it serializes with the sweep and
// any other writer of the same series: a reader never decides from an alarm
// state a concurrent raise or clear is about to change. The caller already
// holds the rule's row (newRuleEvaluator's FOR SHARE, or a rule write's FOR
// UPDATE), per the lock order newRuleEvaluator documents.
func (p *PG) stepRule(ctx context.Context, tx pgx.Tx, r EventRule, comp *Component, instance string, action rules.Action, now time.Time) error {
	if err := lockAdvisory(ctx, tx, ruleSeriesLockKey(r.ID, comp.ID, instance)); err != nil {
		return err
	}
	dedup := ruleDedupKey(r.Name, instance)
	isOpen, err := ruleAlarmOpen(ctx, tx, r, comp.ID, dedup)
	if err != nil {
		return err
	}
	action = unlessInForce(action, isOpen)
	prev, err := loadStreak(ctx, tx, r.ID, comp.ID, instance)
	if err != nil {
		return err
	}
	next, effect := rules.Step(prev, action, now, r.FireSustain, r.ClearSustain)
	if next.Pending != prev.Pending || !next.Since.Equal(prev.Since) {
		if err := saveStreak(ctx, tx, r.ID, comp.ID, instance, next); err != nil {
			return err
		}
	}
	return p.applyRule(ctx, tx, r, comp, dedup, effect)
}

// ruleSeriesLockKey is the advisory-lock key one rule's series on one
// component and instance serializes on: every path that reads its open
// alarm or streak and then acts (an ingest, the sweep, a rule write's prime)
// takes it first. Keyed by ids, the only keys guaranteed unique.
func ruleSeriesLockKey(ruleID, componentID, instance string) string {
	return "event_rule/" + ruleID + "/" + componentID + "/" + instance
}

// ruleAlarmOpen reports whether a rule holds its alarm open on a component.
func ruleAlarmOpen(ctx context.Context, tx pgx.Tx, r EventRule, componentID, dedup string) (bool, error) {
	var isOpen bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from alarm where component_id = $1::uuid and dedup_key = $2
		and cleared_at is null and raised_by_rule_id = $3::uuid)`, componentID, dedup, r.ID).Scan(&isOpen); err != nil {
		return false, fmt.Errorf("storage: rule %s open alarm: %w", r.Name, err)
	}
	return isOpen, nil
}

// unlessInForce turns an action already in force (a fire with the alarm
// open, a clear with none) into a hold.
func unlessInForce(action rules.Action, isOpen bool) rules.Action {
	if (action == rules.Fire && isOpen) || (action == rules.Clear && !isOpen) {
		return rules.Hold
	}
	return action
}

// seriesSample is the newest sample of one component's series.
type seriesSample struct {
	component, instance string
	value               any
}

// newestSamples reads the newest sample (by its timestamp, the series'
// order) of every component series of a key on a lane, or of one series
// when componentID is set.
func newestSamples(ctx context.Context, tx pgx.Tx, lane rules.Lane, key, componentID, instance string) ([]seriesSample, error) {
	table, typeTable, value := "property", "property_type", `s.value #>> '{}'`
	if lane == rules.LaneMetric {
		table, typeTable, value = "metric", "metric_type", "s.value"
	}
	q := fmt.Sprintf(`select distinct on (s.component_id, s.instance) s.component_id::text, s.instance, %s
		from %s s join %s t on t.id = s.%s_id
		where t.name = $1 and s.component_id is not null`, value, table, typeTable, typeTable)
	args := []any{key}
	if componentID != "" {
		q += ` and s.component_id = $2::uuid and s.instance = $3`
		args = append(args, componentID, instance)
	}
	rows, err := tx.Query(ctx, q+` order by s.component_id, s.instance, s.ts desc, s.id desc`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: newest samples of %s: %w", key, err)
	}
	defer rows.Close()
	var out []seriesSample
	for rows.Next() {
		var s seriesSample
		if err := rows.Scan(&s.component, &s.instance, &s.value); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// primeRule judges the newest sample of every series a rule selects, at the
// transaction's clock, as though each had just arrived: a rule created,
// enabled, or materially changed while its condition already holds starts
// its streak (or acts, with no sustain) rather than waiting on a sample a
// transition-only input will never send. It runs in the rule write's
// transaction, after the rule's streaks are reset.
func (p *PG) primeRule(ctx context.Context, tx pgx.Tx, r EventRule) error {
	now, err := dbNow(ctx, tx)
	if err != nil {
		return err
	}
	series, err := newestSamples(ctx, tx, r.SampleLane, r.SampleKey, "", "")
	if err != nil {
		return err
	}
	m := newRuleMatcher(p, nil)
	for _, s := range series {
		o, err := m.ownerFacts(ctx, tx, s.component)
		if err != nil {
			return err
		}
		picked, err := m.selects(ctx, tx, r, o)
		if err != nil {
			return err
		}
		if !picked {
			continue
		}
		action, err := rules.Decide(r.FireCriteria, r.ClearCriteria, s.value)
		if err != nil {
			slog.Warn("event rule cannot evaluate a sample", "facility", "rules", "rule", r.Name, "component", o.comp.Name, "key", r.SampleKey, "error", err.Error())
			continue
		}
		if err := p.stepRule(ctx, tx, r, &o.comp, s.instance, action, now); err != nil {
			return err
		}
	}
	return nil
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
// (a transition-only verdict that went down and stays down). Each due
// streak acts in a transaction of its own, in component order, so one
// outage never holds a lock set per streak at once and a failure (a
// deadlock included) costs that one streak, retried on the next tick.
//
// A disabled rule has no streaks to sweep: its disable deletes them, and an
// ingest judging by the rule holds its row, so the disable waits for that
// batch's streaks to land before deleting them (newRuleEvaluator).
func (p *PG) SweepEventRules(ctx context.Context, now time.Time) error {
	rows, err := p.pool.Query(ctx, `select s.component_id::text, s.instance, s.pending, s.since, `+ruleCols("r.")+`
		from event_rule_state s join event_rule r on r.id = s.rule_id
		where r.enabled order by s.component_id, s.rule_id, s.instance`)
	if err != nil {
		return fmt.Errorf("storage: rule sweep: %w", err)
	}
	var dues []dueStreak
	for rows.Next() {
		var d dueStreak
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
	for _, d := range dues {
		if err := p.sweepStreak(ctx, d); err != nil {
			slog.Warn("event rule sweep could not act on a streak", "facility", "rules", "rule", d.rule.Name, "component", d.component, "error", err.Error())
		}
	}
	return nil
}

// dueStreak is a streak the sweep found held past its sustain.
type dueStreak struct {
	component, instance string
	streak              rules.Streak
	rule                EventRule
}

// sweepStreak acts on one due streak, in its own transaction, holding the
// rule's row FOR SHARE and then the series' rule lock (the lock order
// newRuleEvaluator documents), so it serializes with a rule write and with
// an arriving sample. It claims the streak by deleting exactly the row it
// read (same pending, same since): a row a sample, an edit, or another
// sweep moved in the meantime comes back empty and the sweep does nothing.
// It then acts only when the act still holds: the rule is still enabled and
// still selects the component, and the series' newest sample still decides
// the streak's action; otherwise the claimed streak is simply dropped.
func (p *PG) sweepStreak(ctx context.Context, d dueStreak) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin rule sweep: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	r, err := scanEventRule(tx.QueryRow(ctx, `select `+eventRuleCols+` from event_rule where id = $1 for share`, d.rule.ID))
	live := err == nil && r.Enabled
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("storage: rule sweep reread %s: %w", d.rule.Name, err)
	}
	if err := lockAdvisory(ctx, tx, ruleSeriesLockKey(d.rule.ID, d.component, d.instance)); err != nil {
		return err
	}
	var claimed int
	err = tx.QueryRow(ctx, `delete from event_rule_state where rule_id = $1::uuid and component_id = $2::uuid and instance = $3
		and pending = $4 and since = $5 returning 1`,
		d.rule.ID, d.component, d.instance, pendingWord(d.streak.Pending), d.streak.Since).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: claim rule streak: %w", err)
	}
	if !live {
		return tx.Commit(ctx)
	}
	act, comp, err := p.streakStillHolds(ctx, tx, r, d)
	if err != nil {
		return err
	}
	if act != nil {
		if err := p.applyRule(ctx, tx, *act, comp, ruleDedupKey(act.Name, d.instance), d.streak.Pending); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// streakStillHolds re-reads what a claimed streak's act depends on, judged
// by r (the rule as the sweep locked it), and returns the rule to act as
// and the component to act on, or a nil rule when the act no longer holds.
func (p *PG) streakStillHolds(ctx context.Context, tx pgx.Tx, r EventRule, d dueStreak) (*EventRule, *Component, error) {
	m := newRuleMatcher(p, nil)
	o, err := m.ownerFacts(ctx, tx, d.component)
	if err != nil {
		return nil, nil, err
	}
	picked, err := m.selects(ctx, tx, r, o)
	if err != nil || !picked {
		return nil, nil, err
	}
	newest, err := newestSamples(ctx, tx, r.SampleLane, r.SampleKey, d.component, d.instance)
	if err != nil || len(newest) == 0 {
		return nil, nil, err
	}
	action, err := rules.Decide(r.FireCriteria, r.ClearCriteria, newest[0].value)
	if err != nil || action != d.streak.Pending {
		return nil, nil, nil
	}
	isOpen, err := ruleAlarmOpen(ctx, tx, r, d.component, ruleDedupKey(r.Name, d.instance))
	if err != nil {
		return nil, nil, err
	}
	if unlessInForce(action, isOpen) == rules.Hold {
		return nil, nil, nil
	}
	return &r, &o.comp, nil
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
