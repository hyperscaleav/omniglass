package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// Personal action rules (#1341, #1353): a user's own subscription to the
// alarms they can see. The same action_rule row as a platform rule, with an
// owner and the channels it tells its owner on, and no targets: its only
// recipient is its owner. Every read and write is the owner's alone (an
// ownership predicate on every query; another user's rule is the
// non-disclosing ErrActionRuleNotFound), and it only ever matches an alarm
// whose component the owner's component read scope admits, judged before its
// when runs (ADR-1341).

// PersonalRuleSpec is a personal rule's create input. Targets is accepted
// only to be refused: a personal rule delivers to its owner alone.
type PersonalRuleSpec struct {
	Name             string
	Label            string
	Description      string
	On               []string
	When             string
	NotificationType string
	Channels         []string
	Targets          []string
	Enabled          *bool
}

// PersonalRulePatch carries a personal rule's mutable fields; nil is
// unchanged.
type PersonalRulePatch struct {
	Label            *string
	Description      *string
	On               *[]string
	When             *string
	NotificationType *string
	Channels         *[]string
	Enabled          *bool
}

// PersonalRuleCap is the most personal rules one person may own: every rule
// is judged inside every alarm transition, so a viewer must not be able to
// grow that work without bound.
const PersonalRuleCap = 50

// ErrPersonalRuleCap is a personal rule past PersonalRuleCap.
var ErrPersonalRuleCap = errors.New("storage: a person may own at most 50 personal rules")

// ErrPersonalRuleOwner is a personal rule for a principal that is not a
// person: a service or a node is no one to tell.
var ErrPersonalRuleOwner = errors.New("storage: a personal rule belongs to a person")

// ListPersonalRules lists the owner's rules, by name.
func (p *PG) ListPersonalRules(ctx context.Context, ownerID string) ([]ActionRule, error) {
	out := []ActionRule{}
	if !isUUID(ownerID) {
		return out, nil
	}
	rows, err := p.pool.Query(ctx, `select `+actionRuleCols+actionRuleFrom+` where r.owner_principal_id = $1::uuid order by r.name`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("storage: list personal rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanActionRule(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan personal rule: %w", err)
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetPersonalRule reads one of the owner's rules by name or uuid.
func (p *PG) GetPersonalRule(ctx context.Context, ownerID, ref string) (*ActionRule, error) {
	if !isUUID(ownerID) {
		return nil, ErrActionRuleNotFound
	}
	return getActionRule(ctx, p.pool, ref, ownerID, false)
}

func checkRuleChannels(chs []string) ([]string, error) {
	var out []string
	for _, c := range chs {
		if !slices.Contains(PersonChannels, c) {
			return nil, fmt.Errorf("%w: channels: %q is not a channel (%s)", ErrActionRuleInvalid, c, strings.Join(PersonChannels, ", "))
		}
	}
	for _, c := range PersonChannels {
		if slices.Contains(chs, c) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: channels: name at least one (%s)", ErrActionRuleInvalid, strings.Join(PersonChannels, ", "))
	}
	return out, nil
}

// CreatePersonalRule adds a rule owned by ownerID, audited.
func (p *PG) CreatePersonalRule(ctx context.Context, actorID, ownerID string, spec PersonalRuleSpec) (*ActionRule, error) {
	if len(spec.Targets) > 0 {
		return nil, fmt.Errorf("%w: targets: a personal rule tells its owner alone, on its channels; it names no target", ErrActionRuleInvalid)
	}
	if err := ValidateName("action_rule", spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrActionRuleInvalid, err)
	}
	on, err := checkTransitions(spec.On)
	if err != nil {
		return nil, err
	}
	if err := checkWhen(spec.When); err != nil {
		return nil, err
	}
	channels, err := checkRuleChannels(spec.Channels)
	if err != nil {
		return nil, err
	}
	enabled := true
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create personal rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var human bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from human where principal_id = $1::uuid)`, ownerID).Scan(&human); err != nil {
		return nil, fmt.Errorf("storage: personal rule owner: %w", err)
	}
	if !human {
		return nil, ErrPersonalRuleOwner
	}
	// Serialize one owner's creates so the cap holds under concurrency.
	var owned int
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('personal_rule'), hashtext($1))`, ownerID); err != nil {
		return nil, fmt.Errorf("storage: lock personal rules: %w", err)
	}
	if err := tx.QueryRow(ctx, `select count(*) from action_rule where owner_principal_id = $1::uuid`, ownerID).Scan(&owned); err != nil {
		return nil, fmt.Errorf("storage: count personal rules: %w", err)
	}
	if owned >= PersonalRuleCap {
		return nil, ErrPersonalRuleCap
	}
	typeID, err := resolveRuleType(ctx, tx, spec.NotificationType)
	if err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRow(ctx, `insert into action_rule (name, label, description, on_transitions, when_expr, notification_type_id, enabled, owner_principal_id, channels)
		values ($1, $2, $3, $4, $5, $6::uuid, $7, $8::uuid, $9) returning id::text`,
		spec.Name, labelOrNull(spec.Label), spec.Description, on, spec.When, typeID, enabled, ownerID, channels).Scan(&id)
	if isUniqueViolation(err) {
		return nil, ErrActionRuleExists
	}
	if err != nil {
		return nil, fmt.Errorf("storage: insert personal rule %q: %w", spec.Name, err)
	}
	r, err := getActionRule(ctx, tx, id, ownerID, false)
	if err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "action_rule", r.ID, nil, r); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create personal rule: %w", err)
	}
	return r, nil
}

// UpdatePersonalRule patches one of the owner's rules, audited.
func (p *PG) UpdatePersonalRule(ctx context.Context, actorID, ownerID, ref string, patch PersonalRulePatch) (*ActionRule, error) {
	if !isUUID(ownerID) {
		return nil, ErrActionRuleNotFound
	}
	var on, channels []string
	var err error
	if patch.On != nil {
		if on, err = checkTransitions(*patch.On); err != nil {
			return nil, err
		}
	}
	if patch.When != nil {
		if err := checkWhen(*patch.When); err != nil {
			return nil, err
		}
	}
	if patch.Channels != nil {
		if channels, err = checkRuleChannels(*patch.Channels); err != nil {
			return nil, err
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update personal rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := getActionRule(ctx, tx, ref, ownerID, true)
	if err != nil {
		return nil, err
	}
	setType := patch.NotificationType != nil
	var typeID *string
	if setType {
		if typeID, err = resolveRuleType(ctx, tx, *patch.NotificationType); err != nil {
			return nil, err
		}
	}
	setLabel, labelVal := labelPatch(patch.Label)
	if _, err := tx.Exec(ctx, `update action_rule set
			label = case when $2::boolean then $3 else label end,
			description = coalesce($4, description),
			on_transitions = coalesce($5, on_transitions),
			when_expr = coalesce($6, when_expr),
			notification_type_id = case when $7::boolean then $8::uuid else notification_type_id end,
			enabled = coalesce($9, enabled),
			channels = coalesce($10, channels),
			updated_at = now()
		where id = $1 and owner_principal_id = $11::uuid`, before.ID, setLabel, labelVal, patch.Description, on, patch.When, setType, typeID,
		patch.Enabled, channels, ownerID); err != nil {
		return nil, fmt.Errorf("storage: update personal rule %q: %w", ref, err)
	}
	after, err := getActionRule(ctx, tx, before.ID, ownerID, false)
	if err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "action_rule", before.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update personal rule: %w", err)
	}
	return after, nil
}

// DeletePersonalRule removes one of the owner's rules, audited; its
// notifications keep its name.
func (p *PG) DeletePersonalRule(ctx context.Context, actorID, ownerID, ref string) error {
	if !isUUID(ownerID) {
		return ErrActionRuleNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete personal rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := getActionRule(ctx, tx, ref, ownerID, true)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from action_rule where id = $1 and owner_principal_id = $2::uuid`, before.ID, ownerID); err != nil {
		return fmt.Errorf("storage: delete personal rule %q: %w", ref, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "action_rule", before.ID, before, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RuleDraft overrides a rule's on and when for a :test, so an edit is judged
// before it is saved; nil keeps the rule's own.
type RuleDraft struct {
	On   *[]string
	When *string
}

// RuleMatch is one alarm a :test judged: what the rule would have done on one
// of its transitions.
type RuleMatch struct {
	AlarmID       string
	ComponentName string
	Severity      string
	Message       string
	Transition    string
	At            time.Time
	// Matched is whether the when held; Error is why it did not run.
	Matched bool
	Error   string
}

// TestPersonalRule judges one of the owner's rules, or a draft of its on and
// when, against the owner's most recent alarms (at most limit, 20 when zero,
// 50 at most), each about a component the owner's read scope admits: for
// every transition the rule fires on that the alarm has had, whether the
// when holds over that transition's facts. It writes nothing and sends
// nothing: the teaching surface of a subscription.
func (p *PG) TestPersonalRule(ctx context.Context, ownerID, ref string, draft RuleDraft, read scope.Set, limit int) ([]RuleMatch, error) {
	r, err := p.GetPersonalRule(ctx, ownerID, ref)
	if err != nil {
		return nil, err
	}
	on, src := r.On, r.When
	if draft.On != nil {
		if on, err = checkTransitions(*draft.On); err != nil {
			return nil, err
		}
	}
	if draft.When != nil {
		src = *draft.When
	}
	when, err := rules.CompileWhen(src)
	if err != nil {
		return nil, fmt.Errorf("%w: when: %w", ErrActionRuleInvalid, err)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	type alarmRow struct {
		id, componentID, component, severity, message string
		raisedAt                                      time.Time
		clearedAt                                     *time.Time
	}
	rf := newReadFilter(componentTable, read, 1)
	rows, err := p.pool.Query(ctx, rf.With+`select a.id::text, a.component_id::text, c.name, a.severity, a.message, a.raised_at, a.cleared_at
		from alarm a join component c on c.id = a.component_id
		where `+rf.Pred("a.component_id")+`
		order by a.raised_at desc, a.id desc limit `+fmt.Sprint(limit), rf.Args...)
	if err != nil {
		return nil, fmt.Errorf("storage: recent alarms: %w", err)
	}
	var alarms []alarmRow
	for rows.Next() {
		var a alarmRow
		if err := rows.Scan(&a.id, &a.componentID, &a.component, &a.severity, &a.message, &a.raisedAt, &a.clearedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan recent alarm: %w", err)
		}
		alarms = append(alarms, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: recent alarms: %w", err)
	}
	out := []RuleMatch{}
	for _, a := range alarms {
		for _, tr := range on {
			at := a.raisedAt
			if tr == TransitionCleared {
				if a.clearedAt == nil {
					continue
				}
				at = *a.clearedAt
			}
			m := RuleMatch{AlarmID: a.id, ComponentName: a.component, Severity: a.severity, Message: a.message, Transition: tr, At: at}
			facts, _, err := p.alarmFacts(ctx, p.pool, a.componentID, a.id, tr)
			if err != nil {
				return nil, err
			}
			in, err := actionInput(ctx, p.pool, facts)
			if err != nil {
				return nil, err
			}
			if m.Matched, err = when.Holds(in); err != nil {
				m.Error = "the when did not run: " + err.Error()
			}
			out = append(out, m)
		}
	}
	return out, nil
}
