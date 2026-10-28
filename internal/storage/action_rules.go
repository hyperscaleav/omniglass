package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// The action rule (#618, #1321): the subscription that wires an alarm
// transition to a notification, kept apart from the event rule that detected
// it (the event rule never contains its response). ON one or both alarm
// transitions, WHEN a predicate over the alarm facts holds (the action
// expression site), SEND a notification type to its targets. One step,
// notify; flows extend this row rather than replace it. Authorized as a rule
// (rule:*), its writes take an all-scope grant like an event rule's: a rule
// watches the whole fleet.

// ActionRule is one subscription.
type ActionRule struct {
	ID          string
	Name        string
	Label       string
	Description string
	// On is the transitions it fires on: raised, cleared, or both.
	On []string
	// When is the predicate; empty always holds.
	When string
	// NotificationType is the type it sends, by name; empty sends the
	// official type for the transition (alarm-raised or alarm-cleared).
	NotificationType   string
	NotificationTypeID string
	// Targets are the targets it sends to, by name, in name order.
	Targets []string
	// OwnerID is a personal rule's owner (#1353), empty for a platform rule;
	// Channels are the channels a personal rule tells its owner on.
	OwnerID  string
	Channels []string
	// OptOutAllowed lets a member of a group this platform rule targets opt
	// out of it for themselves (#1354); false by default.
	OptOutAllowed bool
	Enabled       bool
	Official      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ActionRuleSpec is the create input.
type ActionRuleSpec struct {
	Name             string
	Label            string
	Description      string
	On               []string
	When             string
	NotificationType string
	Targets          []string
	OptOutAllowed    bool
	Enabled          *bool
}

// ActionRulePatch carries the mutable fields; nil is unchanged. An empty
// NotificationType returns the rule to the official type per transition.
type ActionRulePatch struct {
	Label            *string
	Description      *string
	On               *[]string
	When             *string
	NotificationType *string
	Targets          *[]string
	OptOutAllowed    *bool
	Enabled          *bool
}

// Action rule sentinels.
var (
	ErrActionRuleNotFound  = errors.New("storage: action rule not found")
	ErrActionRuleExists    = errors.New("storage: an action rule with this name already exists")
	ErrActionRuleInvalid   = errors.New("storage: action rule is invalid")
	ErrActionRuleForbidden = errors.New("storage: writing an action rule needs an all-scope grant")
)

// officialTypeFor is the official notification type a rule naming none sends
// on a transition.
func officialTypeFor(transition string) string {
	if transition == TransitionCleared {
		return "alarm-cleared"
	}
	return "alarm-raised"
}

const actionRuleCols = `r.id, r.name, coalesce(r.label, ''), r.description, r.on_transitions, r.when_expr,
	coalesce(nt.name, ''), coalesce(r.notification_type_id::text, ''),
	coalesce((select array_agg(t.name order by t.name) from action_rule_target art join notification_target t on t.id = art.target_id where art.rule_id = r.id), '{}'),
	coalesce(r.owner_principal_id::text, ''), r.channels, r.opt_out_allowed, r.enabled, r.official, r.created_at, r.updated_at`

const actionRuleFrom = ` from action_rule r left join notification_type nt on nt.id = r.notification_type_id`

func scanActionRule(row pgx.Row) (*ActionRule, error) {
	var r ActionRule
	if err := row.Scan(&r.ID, &r.Name, &r.Label, &r.Description, &r.On, &r.When, &r.NotificationType, &r.NotificationTypeID,
		&r.Targets, &r.OwnerID, &r.Channels, &r.OptOutAllowed, &r.Enabled, &r.Official, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListActionRules lists every platform rule, by name. Rules read like a
// registry; a personal rule is its owner's alone and never listed here.
func (p *PG) ListActionRules(ctx context.Context) ([]ActionRule, error) {
	rows, err := p.pool.Query(ctx, `select `+actionRuleCols+actionRuleFrom+` where r.owner_principal_id is null order by r.name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list action rules: %w", err)
	}
	defer rows.Close()
	out := []ActionRule{}
	for rows.Next() {
		r, err := scanActionRule(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan action rule: %w", err)
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetActionRule reads one rule by name or uuid.
func (p *PG) GetActionRule(ctx context.Context, ref string) (*ActionRule, error) {
	return getActionRule(ctx, p.pool, ref, "", false)
}

// getActionRule reads one rule by name or uuid: a platform rule when owner
// is empty, else that owner's personal rule (another's is not found, never
// forbidden).
func getActionRule(ctx context.Context, q querier, ref, owner string, forUpdate bool) (*ActionRule, error) {
	if err := RejectAddressForm("action_rule", ref); err != nil {
		return nil, err
	}
	lock := ""
	if forUpdate {
		lock = " for update of r"
	}
	if owner != "" && !isUUID(owner) {
		return nil, ErrActionRuleNotFound
	}
	r, err := scanActionRule(q.QueryRow(ctx, `select `+actionRuleCols+actionRuleFrom+` where r.`+registryRefCol(ref)+` = $1
		and r.owner_principal_id is not distinct from nullif($2, '')::uuid`+lock, ref, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrActionRuleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get action rule %q: %w", ref, err)
	}
	return r, nil
}

// checkTransitions refuses an empty or unknown transition set and returns it
// deduplicated in its canonical order.
func checkTransitions(on []string) ([]string, error) {
	var out []string
	for _, t := range []string{TransitionRaised, TransitionCleared} {
		if slices.Contains(on, t) {
			out = append(out, t)
		}
	}
	for _, t := range on {
		if t != TransitionRaised && t != TransitionCleared {
			return nil, fmt.Errorf("%w: on: %q is not a transition (raised or cleared)", ErrActionRuleInvalid, t)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: on: name at least one transition (raised, cleared)", ErrActionRuleInvalid)
	}
	return out, nil
}

// checkWhen compiles a predicate at the action site; a refusal wraps the
// positioned *expression.Error.
func checkWhen(src string) error {
	if _, err := rules.CompileWhen(src); err != nil {
		return fmt.Errorf("%w: when: %w", ErrActionRuleInvalid, err)
	}
	return nil
}

func resolveRuleType(ctx context.Context, q querier, ref string) (*string, error) {
	if ref == "" {
		return nil, nil
	}
	t, err := getNotificationType(ctx, q, ref)
	if errors.Is(err, ErrNotificationTypeNotFound) {
		return nil, fmt.Errorf("%w: notification_type %q is not registered", ErrActionRuleInvalid, ref)
	}
	if err != nil {
		return nil, err
	}
	return &t.ID, nil
}

func resolveRuleTargets(ctx context.Context, q querier, refs []string) ([]string, error) {
	if len(refs) == 0 {
		return nil, fmt.Errorf("%w: targets: name at least one notification target", ErrActionRuleInvalid)
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		t, err := getNotificationTarget(ctx, q, ref, false)
		if errors.Is(err, ErrNotificationTargetNotFound) {
			return nil, fmt.Errorf("%w: target %q is not a notification target", ErrActionRuleInvalid, ref)
		}
		if err != nil {
			return nil, err
		}
		if !slices.Contains(ids, t.ID) {
			ids = append(ids, t.ID)
		}
	}
	return ids, nil
}

func setRuleTargets(ctx context.Context, tx pgx.Tx, ruleID string, targetIDs []string) error {
	if _, err := tx.Exec(ctx, `delete from action_rule_target where rule_id = $1`, ruleID); err != nil {
		return fmt.Errorf("storage: clear action rule targets: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into action_rule_target (rule_id, target_id) select $1, unnest($2::uuid[])`, ruleID, targetIDs); err != nil {
		return fmt.Errorf("storage: set action rule targets: %w", err)
	}
	return nil
}

// CreateActionRule adds a rule, audited; an all-scope write.
func (p *PG) CreateActionRule(ctx context.Context, actorID string, spec ActionRuleSpec, create scope.Set) (*ActionRule, error) {
	if !create.All {
		return nil, ErrActionRuleForbidden
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
	enabled := true
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create action rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	typeID, err := resolveRuleType(ctx, tx, spec.NotificationType)
	if err != nil {
		return nil, err
	}
	targetIDs, err := resolveRuleTargets(ctx, tx, spec.Targets)
	if err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRow(ctx, `insert into action_rule (name, label, description, on_transitions, when_expr, notification_type_id, enabled, opt_out_allowed)
		values ($1, $2, $3, $4, $5, $6::uuid, $7, $8) returning id::text`,
		spec.Name, labelOrNull(spec.Label), spec.Description, on, spec.When, typeID, enabled, spec.OptOutAllowed).Scan(&id)
	if isUniqueViolation(err) {
		return nil, ErrActionRuleExists
	}
	if err != nil {
		return nil, fmt.Errorf("storage: insert action rule %q: %w", spec.Name, err)
	}
	if err := setRuleTargets(ctx, tx, id, targetIDs); err != nil {
		return nil, err
	}
	r, err := getActionRule(ctx, tx, id, "", false)
	if err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "action_rule", r.ID, nil, r); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create action rule: %w", err)
	}
	return r, nil
}

// UpdateActionRule patches a rule, audited; an all-scope write.
func (p *PG) UpdateActionRule(ctx context.Context, actorID, ref string, patch ActionRulePatch, action scope.Set) (*ActionRule, error) {
	if !action.All {
		return nil, ErrActionRuleForbidden
	}
	var on []string
	if patch.On != nil {
		var err error
		if on, err = checkTransitions(*patch.On); err != nil {
			return nil, err
		}
	}
	if patch.When != nil {
		if err := checkWhen(*patch.When); err != nil {
			return nil, err
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update action rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := getActionRule(ctx, tx, ref, "", true)
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
			opt_out_allowed = coalesce($10, opt_out_allowed),
			updated_at = now()
		where id = $1`, before.ID, setLabel, labelVal, patch.Description, on, patch.When, setType, typeID, patch.Enabled, patch.OptOutAllowed); err != nil {
		return nil, fmt.Errorf("storage: update action rule %q: %w", ref, err)
	}
	// Taking opt-out back off withdraws every opt-out, rather than suspending
	// them to return silently if it is allowed again (#1354).
	withdrawn := int64(0)
	if patch.OptOutAllowed != nil && !*patch.OptOutAllowed && before.OptOutAllowed {
		tag, err := tx.Exec(ctx, `delete from action_rule_opt_out where rule_id = $1`, before.ID)
		if err != nil {
			return nil, fmt.Errorf("storage: withdraw opt-outs of %q: %w", ref, err)
		}
		withdrawn = tag.RowsAffected()
	}
	if patch.Targets != nil {
		ids, err := resolveRuleTargets(ctx, tx, *patch.Targets)
		if err != nil {
			return nil, err
		}
		if err := setRuleTargets(ctx, tx, before.ID, ids); err != nil {
			return nil, err
		}
	}
	after, err := getActionRule(ctx, tx, before.ID, "", false)
	if err != nil {
		return nil, err
	}
	img := any(after)
	if withdrawn > 0 {
		img = map[string]any{"rule": after, "opt_outs_withdrawn": withdrawn}
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "action_rule", before.ID, before, img); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update action rule: %w", err)
	}
	return after, nil
}

// DeleteActionRule removes a rule, audited; its notifications keep its name.
func (p *PG) DeleteActionRule(ctx context.Context, actorID, ref string, action scope.Set) error {
	if !action.All {
		return ErrActionRuleForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete action rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := getActionRule(ctx, tx, ref, "", true)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from action_rule where id = $1`, before.ID); err != nil {
		return fmt.Errorf("storage: delete action rule %q: %w", ref, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "action_rule", before.ID, before, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ErrOptOutNotAllowed is an opt-out of a rule whose admin has not allowed it.
var ErrOptOutNotAllowed = errors.New("storage: this action rule does not allow opting out")

// SetRuleOptOut opts the principal out of a platform rule (out true) or back
// in, audited and idempotent (#1354). Opting out takes a rule marked
// opt_out_allowed; opting back in is always allowed. An opt-out reaches the
// rule's group targets: a person a target names directly is told regardless.
func (p *PG) SetRuleOptOut(ctx context.Context, actorID, principalID, ref string, out bool) (*ActionRule, error) {
	if !isUUID(principalID) {
		return nil, ErrActionRuleNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin opt-out: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	r, err := getActionRule(ctx, tx, ref, "", false)
	if err != nil {
		return nil, err
	}
	verb := "opt_in"
	var tag pgconn.CommandTag
	if out {
		if !r.OptOutAllowed {
			return nil, ErrOptOutNotAllowed
		}
		verb = "opt_out"
		tag, err = tx.Exec(ctx, `insert into action_rule_opt_out (rule_id, principal_id) values ($1, $2::uuid) on conflict do nothing`, r.ID, principalID)
	} else {
		tag, err = tx.Exec(ctx, `delete from action_rule_opt_out where rule_id = $1 and principal_id = $2::uuid`, r.ID, principalID)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: %s of %s: %w", verb, r.Name, err)
	}
	if tag.RowsAffected() > 0 {
		if err := writeAuditRes(ctx, tx, actorID, verb, "action_rule", r.ID, nil, map[string]any{"principal_id": principalID, "opted_out": out}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit opt-out: %w", err)
	}
	return r, nil
}
