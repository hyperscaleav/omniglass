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
	ErrEventRuleForbidden = errors.New("storage: event rules are fleet-wide config and need an all-scope grant")
)

const eventRuleCols = `id, name, coalesce(label, ''), description, selector, sample_key, sample_lane,
	fire_criteria, clear_criteria,
	(extract(epoch from fire_sustain) * 1000000)::bigint, (extract(epoch from clear_sustain) * 1000000)::bigint,
	severity, message, enabled, official, version, created_at, updated_at`

func scanEventRule(row pgx.Row) (EventRule, error) {
	var r EventRule
	var sel, fire, clear []byte
	var lane string
	var fireUs, clearUs int64
	err := row.Scan(&r.ID, &r.Name, &r.Label, &r.Description, &sel, &r.SampleKey, &lane, &fire, &clear,
		&fireUs, &clearUs, &r.Severity, &r.Message, &r.Enabled, &r.Official, &r.Version, &r.CreatedAt, &r.UpdatedAt)
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

// prepareEventRule validates a rule against the registries and resolves its
// selector's components to ids, returning the lane its key reads.
func prepareEventRule(ctx context.Context, q querier, r *EventRule) error {
	if err := ValidateName("event_rule", r.Name); err != nil {
		return fmt.Errorf("%w: %w", ErrEventRuleInvalid, err)
	}
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
	if err := r.FireCriteria.Validate(r.SampleLane); err != nil {
		return invalidRule("fire criteria: %v", err)
	}
	if r.ClearCriteria != nil {
		if err := r.ClearCriteria.Validate(r.SampleLane); err != nil {
			return invalidRule("clear criteria: %v", err)
		}
	}
	if r.FireSustain < 0 || r.ClearSustain < 0 {
		return invalidRule("a sustain cannot be negative")
	}
	for _, t := range r.Selector.ComponentTypes {
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

// ListEventRules lists every rule, by name; a reader without an all-scope
// grant sees none, since rules are fleet-wide config.
func (p *PG) ListEventRules(ctx context.Context, read scope.Set) ([]EventRule, error) {
	if !read.All {
		return nil, nil
	}
	rows, err := p.pool.Query(ctx, `select `+eventRuleCols+` from event_rule order by name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list event rules: %w", err)
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

// GetEventRule reads one rule by name.
func (p *PG) GetEventRule(ctx context.Context, name string, read scope.Set) (*EventRule, error) {
	if !read.All {
		return nil, ErrEventRuleNotFound
	}
	r, err := scanEventRule(p.pool.QueryRow(ctx, `select `+eventRuleCols+` from event_rule where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventRuleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get event rule %q: %w", name, err)
	}
	return &r, nil
}

// CreateEventRule authors an operator rule, audited; it needs an all-scope
// create grant.
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
	if err := prepareEventRule(ctx, tx, &r); err != nil {
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
	if err := writeAuditRes(ctx, tx, actorID, "create", "event_rule", created.ID, nil, created); err != nil {
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

// UpdateEventRule patches an operator rule, bumping its version, audited.
func (p *PG) UpdateEventRule(ctx context.Context, actorID, name string, patch EventRulePatch, read, action scope.Set) (*EventRule, error) {
	if !read.All {
		return nil, ErrEventRuleNotFound
	}
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
	if err := prepareEventRule(ctx, tx, &r); err != nil {
		return nil, err
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
	if err := writeAuditRes(ctx, tx, actorID, "update", "event_rule", old.ID, old, updated); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update event rule: %w", err)
	}
	return &updated, nil
}

// DeleteEventRule removes an operator rule, audited.
func (p *PG) DeleteEventRule(ctx context.Context, actorID, name string, read, action scope.Set) error {
	if !read.All {
		return ErrEventRuleNotFound
	}
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
	if _, err := tx.Exec(ctx, `delete from event_rule where id = $1`, old.ID); err != nil {
		return fmt.Errorf("storage: delete event rule %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "event_rule", old.ID, old, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
