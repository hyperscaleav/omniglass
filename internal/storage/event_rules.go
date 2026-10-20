package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// EventRuleSelector picks the components a rule watches: those whose
// component type falls within one of ComponentTypes (by name), and those
// listed in Components (by id once stored); both empty is every component.
type EventRuleSelector struct {
	ComponentTypes []string `json:"component_types,omitempty"`
	Components     []string `json:"components,omitempty"`
}

// EventRule is the authored config that raises alarms (#918, #819): it
// reads one sample key on its lane, fires when FireCriteria holds for
// FireSustain, clears when ClearCriteria (or, when nil, the negation of
// FireCriteria) holds for ClearSustain, at a severity level by name. Version
// bumps on every update. Official rules ship with the boot seed and are
// read-only.
type EventRule struct {
	ID            string
	Name          string
	Label         string
	Description   string
	Selector      EventRuleSelector
	SampleKey     string
	SampleLane    rules.Lane
	FireCriteria  rules.Criteria
	ClearCriteria *rules.Criteria
	FireSustain   time.Duration
	ClearSustain  time.Duration
	Severity      string
	Message       string
	Enabled       bool
	Official      bool
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// The rule's live state as counts, read beside it (never authored, never
	// audited): the alarms it raised that are still open, and its streaks
	// waiting out a sustain. EventRuleState lists them.
	OpenAlarms     int `json:"-"`
	PendingStreaks int `json:"-"`
	// LastJudgedAt is when an ingest batch last judged a sample by the rule,
	// to within ruleJudgedEvery; nil when it never has. A fact about the
	// rule, naming no component.
	LastJudgedAt *time.Time `json:"-"`
}

// EventRuleSpec is a rule to create; its selector's components may be
// given by name or id.
type EventRuleSpec struct {
	Name         string
	Label        string
	Description  string
	Selector     EventRuleSelector
	SampleKey    string
	Fire         rules.Criteria
	Clear        *rules.Criteria
	FireSustain  time.Duration
	ClearSustain time.Duration
	Severity     string
	Message      string
	Enabled      *bool
}

// EventRulePatch carries an update; nil is unchanged. ClearNegates, when
// true, drops the clear criterion so the rule clears on the negation of its
// fire criterion.
type EventRulePatch struct {
	Label        *string
	Description  *string
	Selector     *EventRuleSelector
	SampleKey    *string
	Fire         *rules.Criteria
	Clear        *rules.Criteria
	ClearNegates bool
	FireSustain  *time.Duration
	ClearSustain *time.Duration
	Severity     *string
	Message      *string
	Enabled      *bool
}

// Event rule sentinels.
var (
	ErrEventRuleNotFound  = errors.New("storage: event rule not found")
	ErrEventRuleExists    = errors.New("storage: an event rule already has that name")
	ErrEventRuleOfficial  = errors.New("storage: an official event rule is read-only")
	ErrEventRuleInvalid   = errors.New("storage: event rule is invalid")
	ErrEventRuleForbidden = errors.New("storage: writing an event rule needs an all-scope grant")
)

// ruleCols is the rule read shape, its columns qualified by alias ("" for
// none, "r." in a join).
func ruleCols(a string) string {
	return fmt.Sprintf(`%[1]sid, %[1]sname, coalesce(%[1]slabel, ''), %[1]sdescription, %[1]sselector, %[1]ssample_key, %[1]ssample_lane,
	%[1]sfire_criteria, %[1]sclear_criteria,
	(extract(epoch from %[1]sfire_sustain) * 1000000)::bigint, (extract(epoch from %[1]sclear_sustain) * 1000000)::bigint,
	%[1]sseverity, %[1]smessage, %[1]senabled, %[1]sofficial, %[1]sversion, %[1]screated_at, %[1]supdated_at`, a)
}

var eventRuleCols = ruleCols("")

// scanEventRuleAfter scans leading columns into lead, then a rule.
func scanEventRuleAfter(row pgx.Row, lead ...any) (EventRule, error) {
	return scanEventRuleWith(row, lead...)
}

func scanEventRule(row pgx.Row) (EventRule, error) {
	return scanEventRuleWith(row)
}

// scanEventRuleTrailing scans a rule, then trailing columns into trail.
func scanEventRuleTrailing(row pgx.Row, trail ...any) (EventRule, error) {
	return scanEventRuleCols(row, nil, trail)
}

func scanEventRuleWith(row pgx.Row, lead ...any) (EventRule, error) {
	return scanEventRuleCols(row, lead, nil)
}

func scanEventRuleCols(row pgx.Row, lead, trail []any) (EventRule, error) {
	var r EventRule
	var sel, fire, clear []byte
	var lane string
	var fireUs, clearUs int64
	dest := append(lead, &r.ID, &r.Name, &r.Label, &r.Description, &sel, &r.SampleKey, &lane, &fire, &clear,
		&fireUs, &clearUs, &r.Severity, &r.Message, &r.Enabled, &r.Official, &r.Version, &r.CreatedAt, &r.UpdatedAt)
	dest = append(dest, trail...)
	err := row.Scan(dest...)
	if err != nil {
		return r, err
	}
	r.SampleLane = rules.Lane(lane)
	r.FireSustain, r.ClearSustain = time.Duration(fireUs)*time.Microsecond, time.Duration(clearUs)*time.Microsecond
	if err := json.Unmarshal(sel, &r.Selector); err != nil {
		return r, fmt.Errorf("storage: event rule %q selector: %w", r.Name, err)
	}
	if err := json.Unmarshal(fire, &r.FireCriteria); err != nil {
		return r, fmt.Errorf("storage: event rule %q fire criteria: %w", r.Name, err)
	}
	if len(clear) > 0 {
		var c rules.Criteria
		if err := json.Unmarshal(clear, &c); err != nil {
			return r, fmt.Errorf("storage: event rule %q clear criteria: %w", r.Name, err)
		}
		r.ClearCriteria = &c
	}
	return r, nil
}

func invalidRule(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrEventRuleInvalid, fmt.Sprintf(format, a...))
}

// maxSustain bounds a sustain: a year is past any condition worth holding an
// alarm open for, and well inside what a duration and an interval both hold.
const maxSustain = 365 * 24 * time.Hour

// ruleChecks says which of a rule's references to validate: all of them on
// create; on update only what the patch changed from the stored rule, so a
// rule whose component, type or key was deleted can still be edited (relabelled,
// disabled) by a client that restates every field, and simply matches nothing
// by the stale reference until it is pointed somewhere real. kept is the
// stored selector: an entry it already holds is carried over unresolved, and
// only an entry new to the rule must exist.
type ruleChecks struct {
	key, criteria, selector bool
	kept                    EventRuleSelector
}

var checkAll = ruleChecks{key: true, criteria: true, selector: true}

// prepareEventRule validates a rule against the registries and resolves its
// selector's components to ids, setting the lane its key reads.
func prepareEventRule(ctx context.Context, q querier, r *EventRule, check ruleChecks) error {
	if err := ValidateName("event_rule", r.Name); err != nil {
		return fmt.Errorf("%w: %w", ErrEventRuleInvalid, err)
	}
	for _, d := range []time.Duration{r.FireSustain, r.ClearSustain} {
		if d < 0 || d > maxSustain {
			return invalidRule("a sustain must be between 0s and %s", maxSustain)
		}
		if d%time.Microsecond != 0 {
			return invalidRule("a sustain is kept to the microsecond; %s is finer", d)
		}
	}
	if check.key {
		if err := resolveRuleLane(ctx, q, r); err != nil {
			return err
		}
	}
	if check.key || check.criteria {
		if err := r.FireCriteria.Validate(r.SampleLane); err != nil {
			return invalidRule("fire criteria: %v", err)
		}
		if r.ClearCriteria != nil {
			if err := r.ClearCriteria.Validate(r.SampleLane); err != nil {
				return invalidRule("clear criteria: %v", err)
			}
		}
	}
	if check.selector {
		return resolveRuleSelector(ctx, q, r, check.kept)
	}
	return nil
}

func resolveRuleLane(ctx context.Context, q querier, r *EventRule) error {
	var isProperty, isMetric bool
	if err := q.QueryRow(ctx, `select exists (select 1 from property_type where name = $1),
		exists (select 1 from metric_type where name = $1)`, r.SampleKey).Scan(&isProperty, &isMetric); err != nil {
		return fmt.Errorf("storage: check rule sample key: %w", err)
	}
	switch {
	case isProperty:
		r.SampleLane = rules.LaneProperty
	case isMetric:
		r.SampleLane = rules.LaneMetric
	default:
		return invalidRule("sample key %q is not a registered property or metric", r.SampleKey)
	}
	return nil
}

func resolveRuleSelector(ctx context.Context, q querier, r *EventRule, kept EventRuleSelector) error {
	keptTypes := make(map[string]bool, len(kept.ComponentTypes))
	for _, t := range kept.ComponentTypes {
		keptTypes[t] = true
	}
	keptIDs := make(map[string]bool, len(kept.Components))
	for _, id := range kept.Components {
		keptIDs[id] = true
	}
	for _, t := range r.Selector.ComponentTypes {
		if keptTypes[t] {
			continue
		}
		var ok bool
		if err := q.QueryRow(ctx, `select exists (select 1 from component_type where name = $1)`, t).Scan(&ok); err != nil {
			return fmt.Errorf("storage: check rule component type: %w", err)
		}
		if !ok {
			return invalidRule("component type %q does not exist", t)
		}
	}
	ids := make([]string, 0, len(r.Selector.Components))
	for _, ref := range r.Selector.Components {
		if keptIDs[ref] {
			ids = append(ids, ref)
			continue
		}
		c, err := scopedByName(ctx, q, componentConfig, ref)
		if err != nil {
			return invalidRule("component %q: %v", ref, err)
		}
		ids = append(ids, c.ID)
	}
	r.Selector.Components = ids
	return nil
}

// mapRuleWriteErr turns a write's constraint violation into a sentinel.
func mapRuleWriteErr(err error, name string) error {
	if isUniqueViolation(err) {
		return ErrEventRuleExists
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return invalidRule("severity level does not exist")
	}
	return fmt.Errorf("storage: write event rule %q: %w", name, err)
}

func ruleArgs(r EventRule) ([]byte, []byte, any, error) {
	sel, err := json.Marshal(r.Selector)
	if err != nil {
		return nil, nil, nil, err
	}
	fire, err := json.Marshal(r.FireCriteria)
	if err != nil {
		return nil, nil, nil, err
	}
	var clear any
	if r.ClearCriteria != nil {
		b, err := json.Marshal(r.ClearCriteria)
		if err != nil {
			return nil, nil, nil, err
		}
		clear = b
	}
	return sel, fire, clear, nil
}

// ruleCountsJoin aggregates every rule's live state once, joined to the rule
// rows by id: open alarms it raised, and its pending streaks, each counted
// only on a component the filter admits.
func ruleCountsJoin(f readFilter) string {
	return ` left join (select raised_by_rule_id as rule_id, count(*) as n from alarm
		where cleared_at is null and raised_by_rule_id is not null and ` + f.Pred("component_id") + `
		group by raised_by_rule_id) oa on oa.rule_id = r.id
	left join (select rule_id, count(*) as n from event_rule_state where ` + f.Pred("component_id") + `
		group by rule_id) ps on ps.rule_id = r.id`
}

// ListEventRules lists every rule, by name, each with its live state as
// counts in the same query. Rules read like a registry, so every rule is
// listed whatever the caller's scope; the counts are scoped, counting only
// the alarms and streaks on components the visible component read scope
// admits.
func (p *PG) ListEventRules(ctx context.Context, visible scope.Set) ([]EventRule, error) {
	f := newReadFilter(componentTable, visible, 1)
	rows, err := p.pool.Query(ctx, f.With+`select `+ruleCols("r.")+`, coalesce(oa.n, 0), coalesce(ps.n, 0), lj.last_judged_at
		from event_rule r`+ruleCountsJoin(f)+` left join event_rule_live lj on lj.rule_id = r.id order by r.name`, f.Args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list event rules: %w", err)
	}
	defer rows.Close()
	var out []EventRule
	for rows.Next() {
		var open, pending int
		var judged *time.Time
		r, err := scanEventRuleTrailing(rows, &open, &pending, &judged)
		if err != nil {
			return nil, err
		}
		r.OpenAlarms, r.PendingStreaks, r.LastJudgedAt = open, pending, judged
		out = append(out, r)
	}
	return out, rows.Err()
}

// countRuleState reads one rule's live state as counts, over the components
// the visible scope admits.
func countRuleState(ctx context.Context, q querier, r *EventRule, visible scope.Set) error {
	f := newReadFilter(componentTable, visible, 2)
	args := append([]any{r.ID}, f.Args...)
	if err := q.QueryRow(ctx, f.With+`select (select count(*) from alarm where raised_by_rule_id = $1 and cleared_at is null
			and `+f.Pred("component_id")+`),
		(select count(*) from event_rule_state where rule_id = $1 and `+f.Pred("component_id")+`)`,
		args...).Scan(&r.OpenAlarms, &r.PendingStreaks); err != nil {
		return fmt.Errorf("storage: count event rule %q state: %w", r.Name, err)
	}
	return nil
}

// GetEventRule reads one rule by name, whatever the caller's scope, with its
// live state as counts over the components the visible scope admits.
func (p *PG) GetEventRule(ctx context.Context, name string, visible scope.Set) (*EventRule, error) {
	var judged *time.Time
	r, err := scanEventRuleTrailing(p.pool.QueryRow(ctx, `select `+ruleCols("r.")+`, lj.last_judged_at from event_rule r
		left join event_rule_live lj on lj.rule_id = r.id where r.name = $1`, name), &judged)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventRuleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get event rule %q: %w", name, err)
	}
	r.LastJudgedAt = judged
	if err := countRuleState(ctx, p.pool, &r, visible); err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateEventRule authors an operator rule, audited; it needs an all-scope
// create grant, since a rule's selector reaches the whole fleet. The counts
// it returns are fleet-wide; a caller narrower than that re-reads the rule.
func (p *PG) CreateEventRule(ctx context.Context, actorID string, spec EventRuleSpec, create scope.Set) (*EventRule, error) {
	if !create.All {
		return nil, ErrEventRuleForbidden
	}
	r := EventRule{Name: spec.Name, Label: spec.Label, Description: spec.Description, Selector: spec.Selector,
		SampleKey: spec.SampleKey, FireCriteria: spec.Fire, ClearCriteria: spec.Clear, FireSustain: spec.FireSustain,
		ClearSustain: spec.ClearSustain, Severity: spec.Severity, Message: spec.Message, Enabled: true}
	if spec.Enabled != nil {
		r.Enabled = *spec.Enabled
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create event rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := prepareEventRule(ctx, tx, &r, checkAll); err != nil {
		return nil, err
	}
	sel, fire, clear, err := ruleArgs(r)
	if err != nil {
		return nil, err
	}
	created, err := scanEventRule(tx.QueryRow(ctx, `insert into event_rule (name, label, description, selector,
		sample_key, sample_lane, fire_criteria, clear_criteria, fire_sustain, clear_sustain, severity, message, enabled, official)
		values ($1, $2, $3, $4, $5, $6, $7, $8, make_interval(secs => $9::float8 / 1000000), make_interval(secs => $10::float8 / 1000000), $11, $12, $13, false)
		returning `+eventRuleCols,
		r.Name, labelOrNull(r.Label), r.Description, sel, r.SampleKey, string(r.SampleLane), fire, clear,
		r.FireSustain.Microseconds(), r.ClearSustain.Microseconds(), r.Severity, r.Message, r.Enabled))
	if err != nil {
		return nil, mapRuleWriteErr(err, r.Name)
	}
	if err := p.settleRuleWrite(ctx, tx, actorID, nil, created); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "event_rule", created.ID, nil, created); err != nil {
		return nil, err
	}
	if err := countRuleState(ctx, tx, &created, scope.Set{All: true}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create event rule: %w", err)
	}
	return &created, nil
}

func lockMutableEventRule(ctx context.Context, tx pgx.Tx, name string) (EventRule, error) {
	r, err := scanEventRule(tx.QueryRow(ctx, `select `+eventRuleCols+` from event_rule where name = $1 for update`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrEventRuleNotFound
	}
	if err != nil {
		return r, fmt.Errorf("storage: read event rule %q: %w", name, err)
	}
	if r.Official {
		return r, ErrEventRuleOfficial
	}
	return r, nil
}

// UpdateEventRule patches an operator rule, bumping its version, audited; it
// needs an all-scope update grant. Its counts are fleet-wide, as on create.
func (p *PG) UpdateEventRule(ctx context.Context, actorID, name string, patch EventRulePatch, action scope.Set) (*EventRule, error) {
	if !action.All {
		return nil, ErrEventRuleForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update event rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := lockMutableEventRule(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	r := old
	if patch.Label != nil {
		r.Label = *patch.Label
	}
	if patch.Description != nil {
		r.Description = *patch.Description
	}
	if patch.Selector != nil {
		r.Selector = *patch.Selector
	}
	if patch.SampleKey != nil {
		r.SampleKey = *patch.SampleKey
	}
	if patch.Fire != nil {
		r.FireCriteria = *patch.Fire
	}
	if patch.Clear != nil && patch.ClearNegates {
		return nil, invalidRule("set a clear criterion or clear_negates, not both")
	}
	if patch.Clear != nil {
		c := *patch.Clear
		r.ClearCriteria = &c
	}
	if patch.ClearNegates {
		r.ClearCriteria = nil
	}
	if patch.FireSustain != nil {
		r.FireSustain = *patch.FireSustain
	}
	if patch.ClearSustain != nil {
		r.ClearSustain = *patch.ClearSustain
	}
	if patch.Severity != nil {
		r.Severity = *patch.Severity
	}
	if patch.Message != nil {
		r.Message = *patch.Message
	}
	if patch.Enabled != nil {
		r.Enabled = *patch.Enabled
	}
	// Judged on what changed from the stored rule, not on what the patch
	// restates: an unchanged key keeps its lane, unchanged criteria were valid
	// on it, and a selector entry the rule already held is kept as stored.
	keyChanged := r.SampleKey != old.SampleKey
	check := ruleChecks{
		key:      keyChanged,
		criteria: keyChanged || !sameRuleJSON(r.FireCriteria, old.FireCriteria) || !sameRuleJSON(r.ClearCriteria, old.ClearCriteria),
		selector: patch.Selector != nil,
		kept:     old.Selector,
	}
	if err := prepareEventRule(ctx, tx, &r, check); err != nil {
		return nil, err
	}
	// A patch that changes nothing writes nothing: no version bump, no audit.
	if sameAuthoredRule(old, r) {
		if err := countRuleState(ctx, tx, &old, scope.Set{All: true}); err != nil {
			return nil, err
		}
		return &old, nil
	}
	sel, fire, clear, err := ruleArgs(r)
	if err != nil {
		return nil, err
	}
	updated, err := scanEventRule(tx.QueryRow(ctx, `update event_rule set label = $2, description = $3, selector = $4,
		sample_key = $5, sample_lane = $6, fire_criteria = $7, clear_criteria = $8,
		fire_sustain = make_interval(secs => $9::float8 / 1000000), clear_sustain = make_interval(secs => $10::float8 / 1000000),
		severity = $11, message = $12, enabled = $13, version = version + 1, updated_at = now()
		where id = $1 returning `+eventRuleCols,
		old.ID, labelOrNull(r.Label), r.Description, sel, r.SampleKey, string(r.SampleLane), fire, clear,
		r.FireSustain.Microseconds(), r.ClearSustain.Microseconds(), r.Severity, r.Message, r.Enabled))
	if err != nil {
		return nil, mapRuleWriteErr(err, name)
	}
	if err := p.settleRuleWrite(ctx, tx, actorID, &old, updated); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "event_rule", old.ID, old, updated); err != nil {
		return nil, err
	}
	if err := countRuleState(ctx, tx, &updated, scope.Set{All: true}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update event rule: %w", err)
	}
	return &updated, nil
}

// DeleteEventRule removes an operator rule, audited; it needs an all-scope
// delete grant.
func (p *PG) DeleteEventRule(ctx context.Context, actorID, name string, action scope.Set) error {
	if !action.All {
		return ErrEventRuleForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete event rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := lockMutableEventRule(ctx, tx, name)
	if err != nil {
		return err
	}
	// A rule deleted lets go of every alarm it holds open first: once it is
	// gone nothing would ever clear them.
	if err := p.clearRuleAlarms(ctx, tx, actorID, old, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from event_rule where id = $1`, old.ID); err != nil {
		return fmt.Errorf("storage: delete event rule %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "event_rule", old.ID, old, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// settleRuleWrite brings a rule's live state in line with a write, in the
// write's transaction; old is nil for a rule just inserted. A rule disabled
// lets go of every alarm it holds open and every streak. A rule created,
// enabled, or materially changed (what it reads, how it judges, which
// components, how long) starts its streaks over, lets go of the alarms on
// components it no longer selects, and primes from each series' newest
// sample, so a condition that already holds is not lost on an input that
// sends nothing further. A cosmetic edit (label, description, message,
// severity) touches nothing: a sustain measured against the same judgment
// is still one.
//
// The caller holds the rule's row (FOR UPDATE, or the insert's own lock)
// before settle takes any series or health lock, the lock order
// newRuleEvaluator documents; an ingest judging by the rule holds the row
// FOR SHARE, so the write waits for it and then sees what it raised.
func (p *PG) settleRuleWrite(ctx context.Context, tx pgx.Tx, actorID string, old *EventRule, updated EventRule) error {
	wasOn := old != nil && old.Enabled
	if !updated.Enabled {
		if !wasOn {
			return nil
		}
		if err := p.clearRuleAlarms(ctx, tx, actorID, updated, nil); err != nil {
			return err
		}
		return resetRuleStreaks(ctx, tx, updated.ID)
	}
	if wasOn && !materialRuleChange(*old, updated) {
		return nil
	}
	if old != nil {
		if err := resetRuleStreaks(ctx, tx, updated.ID); err != nil {
			return err
		}
		if wasOn && !sameRuleJSON(old.Selector, updated.Selector) {
			m := newRuleMatcher(p, nil)
			if err := p.clearRuleAlarms(ctx, tx, actorID, updated, func(o *ruleOwner) (bool, error) {
				return m.selects(ctx, tx, updated, o)
			}); err != nil {
				return err
			}
		}
	}
	return p.primeRule(ctx, tx, updated)
}

func resetRuleStreaks(ctx context.Context, tx pgx.Tx, ruleID string) error {
	if _, err := tx.Exec(ctx, `delete from event_rule_state where rule_id = $1`, ruleID); err != nil {
		return fmt.Errorf("storage: reset rule streaks: %w", err)
	}
	return nil
}

// materialRuleChange reports whether a write changed how a rule judges:
// its key or lane, its criteria, its selector, or its sustains.
func materialRuleChange(a, b EventRule) bool {
	type judgment struct {
		SampleKey                 string
		SampleLane                rules.Lane
		Selector                  EventRuleSelector
		Fire                      rules.Criteria
		Clear                     *rules.Criteria
		FireSustain, ClearSustain time.Duration
	}
	pick := func(r EventRule) judgment {
		return judgment{r.SampleKey, r.SampleLane, r.Selector, r.FireCriteria, r.ClearCriteria, r.FireSustain, r.ClearSustain}
	}
	return !sameRuleJSON(pick(a), pick(b))
}

func sameRuleJSON(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// sameAuthoredRule reports whether two versions of a rule say the same thing.
func sameAuthoredRule(a, b EventRule) bool {
	type authored struct {
		Label, Description, SampleKey, Severity, Message string
		Selector                                         EventRuleSelector
		Fire                                             rules.Criteria
		Clear                                            *rules.Criteria
		FireSustain, ClearSustain                        time.Duration
		Enabled                                          bool
	}
	pick := func(r EventRule) string {
		j, _ := json.Marshal(authored{r.Label, r.Description, r.SampleKey, r.Severity, r.Message, r.Selector,
			r.FireCriteria, r.ClearCriteria, r.FireSustain, r.ClearSustain, r.Enabled})
		return string(j)
	}
	return pick(a) == pick(b)
}

// UpsertEventRule installs an official rule from the boot seed,
// authoritative on conflict; an operator's rules keep distinct names and are
// untouched. An official rule never takes over an operator's: a name an
// operator rule already holds is refused, naming it. A re-seed that changes
// nothing writes nothing (no version bump); one that inserts or changes the
// rule settles its live state as an operator's create or edit does.
func (p *PG) UpsertEventRule(ctx context.Context, spec EventRuleSpec) error {
	r := EventRule{Name: spec.Name, Label: spec.Label, Description: spec.Description, Selector: spec.Selector,
		SampleKey: spec.SampleKey, FireCriteria: spec.Fire, ClearCriteria: spec.Clear, FireSustain: spec.FireSustain,
		ClearSustain: spec.ClearSustain, Severity: spec.Severity, Message: spec.Message, Enabled: true}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin upsert event rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := prepareEventRule(ctx, tx, &r, checkAll); err != nil {
		return fmt.Errorf("storage: official event rule %q: %w", r.Name, err)
	}
	existing, err := scanEventRule(tx.QueryRow(ctx, `select `+eventRuleCols+` from event_rule where name = $1 for update`, r.Name))
	var old *EventRule
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("storage: read event rule %q: %w", r.Name, err)
	case !existing.Official:
		return fmt.Errorf("%w: the operator rule %q holds the name of an official rule; rename it", ErrEventRuleExists, r.Name)
	default:
		if sameAuthoredRule(existing, r) {
			return nil
		}
		old = &existing
	}
	sel, fire, clear, err := ruleArgs(r)
	if err != nil {
		return err
	}
	upserted, err := scanEventRule(tx.QueryRow(ctx, `insert into event_rule (name, label, description, selector, sample_key, sample_lane,
		fire_criteria, clear_criteria, fire_sustain, clear_sustain, severity, message, enabled, official)
		values ($1, $2, $3, $4, $5, $6, $7, $8, make_interval(secs => $9::float8 / 1000000), make_interval(secs => $10::float8 / 1000000), $11, $12, true, true)
		on conflict (name) do update set label = excluded.label, description = excluded.description, selector = excluded.selector,
			sample_key = excluded.sample_key, sample_lane = excluded.sample_lane, fire_criteria = excluded.fire_criteria,
			clear_criteria = excluded.clear_criteria, fire_sustain = excluded.fire_sustain, clear_sustain = excluded.clear_sustain,
			severity = excluded.severity, message = excluded.message, version = event_rule.version + 1, updated_at = now()
		returning `+eventRuleCols,
		r.Name, labelOrNull(r.Label), r.Description, sel, r.SampleKey, string(r.SampleLane), fire, clear,
		r.FireSustain.Microseconds(), r.ClearSustain.Microseconds(), r.Severity, r.Message))
	if err != nil {
		return mapRuleWriteErr(err, r.Name)
	}
	if err := p.settleRuleWrite(ctx, tx, "", old, upserted); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
