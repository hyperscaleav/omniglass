package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The alarm write side. An alarm is component-local and impairs it wholesale
// (#626): a component's own verdict is the worst of its active alarms'
// severities, and the rollup notices when that verdict is no longer Healthy,
// which is the only way an alarm reaches a system now.
//
// Raising and clearing both recompute health in the SAME transaction. That is the
// slice's load-bearing rule: an alarm and the verdict it caused must never be
// separately visible, and a verdict computed later would stamp its transition at
// the time somebody looked rather than the time the fleet changed.

// Alarm is one raised condition on a component. ClearedAt is nil while the alarm
// is active; clearing keeps the row, so the record of what was wrong and when
// survives the fix.
type Alarm struct {
	ID            string
	ComponentID   string
	ComponentName string
	Severity      string
	Message       string
	// DedupKey names WHICH condition this incident is about (ADR-0075): the
	// one-open invariant is per (component, dedup_key), enforced by the partial
	// unique index while the row is open.
	DedupKey  string
	RaisedAt  time.Time
	ClearedAt *time.Time
	// AcknowledgedAt and AcknowledgedBy record that a HUMAN has seen this
	// incident, and are ORTHOGONAL to ClearedAt (#728): raised state belongs to
	// the condition, acknowledgement to a person, so an alarm can be raised and
	// unacknowledged (the queue an operator works), raised and acknowledged
	// (somebody is on it), or cleared having never been acknowledged.
	// AcknowledgedBy is the acknowledger's IDENTIFIER, not their uuid and not a
	// label: a human's username, a service account's name, or a node's name,
	// resolved by principalIdent. It is what an operator surface renders
	// (ADR-0062), and it reads empty once that principal is purged, while
	// audit_log keeps the identifier it denormalized.
	AcknowledgedAt *time.Time
	AcknowledgedBy string
	// RaisedByRuleID is the event rule that raised the alarm (#919), nil for
	// a hand-raised one or once its rule is deleted.
	RaisedByRuleID *string
	// RaisedByRule is that rule's name, carried beside its id.
	RaisedByRule *string
	// SnoozedUntil and SnoozedBy are the snooze on the alarm's CONDITION
	// (#1318, ADR-1318), not on this row: every alarm of the same
	// (component, dedup_key) reads the same pair while the snooze is in
	// force, and none once its deadline passes. SnoozedBy is the identifier
	// of who set it, resolved as AcknowledgedBy is.
	SnoozedUntil *time.Time
	SnoozedBy    string
}

// Active reports whether the alarm is still raised.
func (a Alarm) Active() bool { return a.ClearedAt == nil }

// Acknowledged reports whether somebody has recorded that they have seen this
// alarm. It says nothing about whether the alarm is still raised.
func (a Alarm) Acknowledged() bool { return a.AcknowledgedAt != nil }

// Snoozed reports whether the alarm's condition is snoozed.
func (a Alarm) Snoozed() bool { return a.SnoozedUntil != nil }

// AlarmFilter narrows a component's alarm list. The zero value is what is wrong
// now: active alarms, acknowledged or not.
type AlarmFilter struct {
	// IncludeCleared widens the list from the active set to the whole history.
	IncludeCleared bool
	// Unacknowledged keeps only the alarms nobody has looked at. With the zero
	// IncludeCleared this is exactly "raised and unacknowledged", the queue an
	// operator actually works; with IncludeCleared it also returns the incidents
	// that came and went unattended.
	Unacknowledged bool
}

// AlarmSpec is the raise input: a severity, an operator's note on what is
// wrong, and the condition's dedup identity.
type AlarmSpec struct {
	Severity string
	Message  string
	// DedupKey is the condition identity (required): a re-raise of the same key
	// on the same component returns the existing open alarm, never a duplicate.
	DedupKey string
}

// Alarm sentinels. A bad severity is a request fault (422), not a server error:
// it is something the caller sent.
var (
	ErrAlarmNotFound = errors.New("storage: alarm not found or already cleared")
	ErrAlarmSeverity = errors.New("storage: unknown alarm severity")
	// ErrAlarmInvalid is a hand raise the platform refuses (a dedup key in the
	// rule namespace): a 422.
	ErrAlarmInvalid = errors.New("storage: alarm is invalid")
)

// alarmCols is the read shape. acknowledged_by surfaces the acting principal's
// IDENTIFIER rather than its uuid, so every alarm read hands the API something
// an operator recognises (ADR-0062); it reads empty once that principal is
// purged, which is honest, and audit_log still names them.
//
// The identifier sources are projected as their own columns and folded in Go
// (principalIdent), which is where that policy lives since #564 dropped the
// stored function. A projection rather than a join, because one of the four
// statements reading this shape is an UPDATE ... RETURNING: RETURNING cannot
// left-join, and giving the UPDATE a FROM clause would change which rows it
// updates.
//
// The component's name comes the same way, as a scalar subquery rather than a
// join, so every read path carries the component's uuid AND its name (#557: the
// winning raise used to echo the caller's ref while the others scanned a uuid).
var alarmCols = `a.id, a.component_id, (select c.name from component c where c.id = a.component_id),
	a.severity, a.message, a.dedup_key, a.raised_at, a.cleared_at, a.raised_by_rule_id::text,
	(select r.name from event_rule r where r.id = a.raised_by_rule_id),
	a.acknowledged_at, ` + principalIdentCols("a.acknowledged_by") + `,
	` + snoozeOf("snoozed_until") + `, ` + principalIdentCols(snoozeOf("snoozed_by"))

// snoozeOf reads one column of the snooze in force on the alarm's condition
// (#1318): a scalar subquery on the primary key, so it rides the UPDATE ...
// RETURNING reads as the acknowledger's projection does. A snooze past its
// deadline that the sweep has not yet ended reads as no snooze: the row is
// only waiting to be swept.
func snoozeOf(col string) string {
	return "(select s." + col + " from alarm_snooze s where s.component_id = a.component_id and s.dedup_key = a.dedup_key and s.snoozed_until > now())"
}

func scanAlarm(row pgx.Row) (*Alarm, error) {
	var a Alarm
	var ackUsername, ackServiceName, ackNodeName *string
	var snzUsername, snzServiceName, snzNodeName *string
	if err := row.Scan(&a.ID, &a.ComponentID, &a.ComponentName, &a.Severity, &a.Message,
		&a.DedupKey, &a.RaisedAt, &a.ClearedAt, &a.RaisedByRuleID, &a.RaisedByRule, &a.AcknowledgedAt, &ackUsername, &ackServiceName, &ackNodeName,
		&a.SnoozedUntil, &snzUsername, &snzServiceName, &snzNodeName); err != nil {
		return nil, err
	}
	a.AcknowledgedBy = principalIdent(ackUsername, ackServiceName, ackNodeName)
	a.SnoozedBy = principalIdent(snzUsername, snzServiceName, snzNodeName)
	return &a, nil
}

// RaiseAlarm records a condition on a component, then recomputes the health
// chain in the same transaction. An unknown component is ErrComponentNotFound.
func (p *PG) RaiseAlarm(ctx context.Context, actorID, componentName string, spec AlarmSpec) (*Alarm, error) {
	// The rule: prefix is the rule key namespace (ruleDedupKey): a hand raise
	// keyed in it would stand in for a rule's condition, or be cleared as one.
	// The key defaults to the message, so a message is judged the same way.
	key := spec.DedupKey
	if key == "" {
		key = spec.Message
	}
	if strings.HasPrefix(key, "rule:") {
		return nil, fmt.Errorf("%w: a hand-raised alarm's dedup_key (its message when none is given) may not begin rule:, which names a rule's condition", ErrAlarmInvalid)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin raise alarm: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The component is resolved once, before the insert, so a typo reads as a
	// missing component rather than an opaque foreign-key fault, and its id is
	// bound directly everywhere below rather than re-resolved by name: under
	// scoped name uniqueness (#627) a second name-lookup could land on a
	// different row sharing the same name, or fail outright with SQLSTATE
	// 21000 ("more than one row returned by a subquery used as an
	// expression").
	component, err := scopedByName(ctx, tx, componentConfig, componentName)
	if err != nil {
		return nil, err
	}

	a, err := p.raiseAlarmTx(ctx, tx, actorID, component, spec, nil)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit raise alarm: %w", err)
	}
	return a, nil
}

// raiseAlarmTx is RaiseAlarm's body inside a transaction the caller owns: the
// hand raise and the rule evaluator (#919) raise through the same guarded
// conditional insert, audit row and health recompute. ruleID names the rule
// that raised it, nil for a hand raise. A raise that lands on an open alarm
// of the same condition returns that alarm and writes nothing.
func (p *PG) raiseAlarmTx(ctx context.Context, tx pgx.Tx, actorID string, component *Component, spec AlarmSpec, ruleID *string) (*Alarm, error) {
	if spec.DedupKey == "" {
		// The message is the condition identity when the raiser supplies no
		// explicit key (ADR-0075): the same condition text re-raised is the
		// same condition. A raise with neither key nor message has no known
		// identity, and the insert below mints a unique placeholder so it
		// dedups with nothing, exactly the pre-key behavior.
		spec.DedupKey = spec.Message
	}
	// The guarded conditional insert (ADR-0075): the partial unique index
	// carries the one-open-per-condition invariant, and a losing raise reads
	// the existing open incident back instead of minting a duplicate. The
	// no-op path writes no audit row and recomputes nothing: nothing changed.
	//
	// Every read path, this one included, carries the component's uuid and
	// its name (#557).
	a := Alarm{ComponentID: component.ID, ComponentName: component.Name, Severity: spec.Severity,
		Message: spec.Message, DedupKey: spec.DedupKey, RaisedByRuleID: ruleID}
	err := tx.QueryRow(ctx, `
		insert into alarm (component_id, severity, message, dedup_key, raised_by_rule_id)
		values ($1::uuid, $2, $3, coalesce(nullif($4, ''), gen_random_uuid()::text), $5::uuid)
		on conflict (component_id, dedup_key) where cleared_at is null do nothing
		returning id, raised_at, dedup_key`,
		component.ID, spec.Severity, spec.Message, spec.DedupKey, ruleID).Scan(&a.ID, &a.RaisedAt, &a.DedupKey)
	if errors.Is(err, pgx.ErrNoRows) {
		// The severity names a level in the registry (#917). A raise that
		// lands on an open alarm's dedup key inserts nothing, so the foreign
		// key never checks it: check here, on this path only, so the winning
		// raise costs no extra statement.
		var known bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from severity_level where name = $1)`, spec.Severity).Scan(&known); err != nil {
			return nil, fmt.Errorf("storage: check alarm severity: %w", err)
		}
		if !known {
			return nil, ErrAlarmSeverity
		}
		existing, gerr := scanAlarm(tx.QueryRow(ctx, `
			select `+alarmCols+`
			from alarm a
			where a.component_id = $1::uuid
			  and a.dedup_key = $2 and a.cleared_at is null`, component.ID, spec.DedupKey))
		if gerr != nil {
			return nil, fmt.Errorf("storage: read existing open alarm on %q: %w", component.Name, gerr)
		}
		return existing, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "alarm_severity_fkey" {
		return nil, ErrAlarmSeverity
	}
	if err != nil {
		return nil, fmt.Errorf("storage: insert alarm on %q: %w", component.Name, err)
	}

	if err := writeAuditRes(ctx, tx, actorID, "create", "alarm", a.ID, nil, a); err != nil {
		return nil, err
	}
	if err := p.RecomputeHealth(ctx, tx, component.ID); err != nil {
		return nil, err
	}
	// The raise's notifications, in its transaction (#618): a hand raise
	// and a rule's raise both pass here, and a re-raise that landed on an
	// open alarm returned above, so each opened incident notifies once.
	if err := p.queueNotificationsTx(ctx, tx, component.ID, a.ID, TransitionRaised); err != nil {
		return nil, err
	}
	return &a, nil
}

// ClearAlarm marks an active alarm cleared and recomputes health in the same
// transaction. Clearing an alarm that is already cleared, belongs to another
// component, or does not exist is ErrAlarmNotFound: clearing twice is an explicit
// miss, not a silent success.
func (p *PG) ClearAlarm(ctx context.Context, actorID, componentName, alarmID string) error {
	// A malformed id is a miss rather than a server error: the address simply does
	// not name an alarm, and letting it reach Postgres would be a 500 for a typo.
	if _, err := uuid.Parse(alarmID); err != nil {
		return ErrAlarmNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin clear alarm: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once, id bound below instead of re-derived by name (see
	// RaiseAlarm). Unlike RaiseAlarm this function never checked component
	// existence explicitly before: an unknown name's subquery used to resolve
	// to no id, which then matched no alarm row, reading as ErrAlarmNotFound.
	// ErrComponentNotFound folds into that same sentinel here to preserve
	// that exact prior behavior; ErrAmbiguousName, which could not have
	// occurred before scoped uniqueness exists, is left to propagate.
	component, err := scopedByName(ctx, tx, componentConfig, componentName)
	if errors.Is(err, ErrComponentNotFound) {
		return ErrAlarmNotFound
	} else if err != nil {
		return err
	}

	var cleared time.Time
	if err := tx.QueryRow(ctx, `
		update alarm set cleared_at = now(), updated_at = now()
		where id = $1 and component_id = $2::uuid and cleared_at is null
		returning cleared_at`, alarmID, component.ID).Scan(&cleared); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAlarmNotFound
		}
		return fmt.Errorf("storage: clear alarm %s: %w", alarmID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "alarm", alarmID, nil,
		map[string]any{"component": componentName, "cleared_at": cleared}); err != nil {
		return err
	}
	if err := p.RecomputeHealth(ctx, tx, component.ID); err != nil {
		return err
	}
	// A clear is the condition's edge whoever records it (decision 6 of
	// #618), so a hand clear notifies as a rule's does.
	if err := p.queueNotificationsTx(ctx, tx, component.ID, alarmID, TransitionCleared); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit clear alarm: %w", err)
	}
	return nil
}

// AcknowledgeAlarm records that a human has seen this alarm, and changes nothing
// else. It deliberately does NOT recompute health: an alarm's raised state
// belongs to its condition, so acknowledging is not fixing, and a recompute here
// would stamp a transition at a moment when nothing about the fleet changed.
//
// Acknowledging twice is IDEMPOTENT, not a refusal (#728). The recorded fact is
// "a human has seen this", which is monotonic, and the FIRST sighting is the
// operationally meaningful one (time to acknowledge), so a second call keeps the
// first person and the first time, writes no second audit row, and returns the
// row. It is the same shape as a re-raise of an open condition returning the
// existing incident (ADR-0075): the no-op leg changed nothing, so it records
// nothing.
//
// A cleared alarm is acknowledgeable too. The two facts are independent in both
// directions, and the record of who read an incident outlives the fix exactly as
// the row itself does.
func (p *PG) AcknowledgeAlarm(ctx context.Context, actorID, componentName, alarmID string) (*Alarm, error) {
	// A malformed id is a miss rather than a server error: the address simply does
	// not name an alarm (see ClearAlarm).
	if _, err := uuid.Parse(alarmID); err != nil {
		return nil, ErrAlarmNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin acknowledge alarm: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once and bound by id below, never re-derived by name: under scoped
	// name uniqueness (#627) a second lookup could land on a different row. An
	// unknown component folds into the alarm miss, as it does in ClearAlarm, so a
	// typo in either half of the address reads the same way.
	component, err := scopedByName(ctx, tx, componentConfig, componentName)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, ErrAlarmNotFound
	} else if err != nil {
		return nil, err
	}

	// The guarded conditional update: `acknowledged_at is null` is what makes the
	// second acknowledgement a no-op rather than an overwrite, decided by the
	// database rather than by a read-then-write this transaction could lose.
	acked, err := scanAlarm(tx.QueryRow(ctx, `
		update alarm a set acknowledged_at = now(), acknowledged_by = nullif($3, '')::uuid, updated_at = now()
		where a.id = $1 and a.component_id = $2::uuid and a.acknowledged_at is null
		returning `+alarmCols, alarmID, component.ID, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the alarm is already acknowledged (the idempotent leg) or the id
		// names no alarm on this component (a miss). One read tells them apart.
		existing, gerr := scanAlarm(tx.QueryRow(ctx, `
			select `+alarmCols+` from alarm a
			where a.id = $1 and a.component_id = $2::uuid`, alarmID, component.ID))
		if errors.Is(gerr, pgx.ErrNoRows) {
			return nil, ErrAlarmNotFound
		}
		if gerr != nil {
			return nil, fmt.Errorf("storage: read acknowledged alarm %s: %w", alarmID, gerr)
		}
		return existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: acknowledge alarm %s: %w", alarmID, err)
	}

	if err := writeAuditRes(ctx, tx, actorID, "acknowledge", "alarm", alarmID, nil,
		map[string]any{"component": componentName, "acknowledged_at": acked.AcknowledgedAt}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit acknowledge alarm: %w", err)
	}
	return acked, nil
}

// ListAlarms returns a component's alarms, newest first: what is wrong now by
// default, the whole history with IncludeCleared, and only what nobody has looked
// at with Unacknowledged. An unknown component is ErrComponentNotFound rather
// than an empty list, so a typo is visible.
func (p *PG) ListAlarms(ctx context.Context, componentName string, filter AlarmFilter) ([]Alarm, error) {
	component, err := scopedByName(ctx, p.pool, componentConfig, componentName)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		select `+alarmCols+`
		from alarm a
		where a.component_id = $1::uuid and ($2 or a.cleared_at is null)
		  and (not $3 or a.acknowledged_at is null)
		order by a.raised_at desc, a.id desc`, component.ID, filter.IncludeCleared, filter.Unacknowledged)
	if err != nil {
		return nil, fmt.Errorf("storage: list alarms %q: %w", componentName, err)
	}
	defer rows.Close()

	out := []Alarm{}
	for rows.Next() {
		a, err := scanAlarm(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan alarm: %w", err)
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ListSystemAlarms returns a system's members' alarms in one query, newest
// first (#940): every active alarm whatever its age, and every alarm cleared
// inside window (a zero window keeps every cleared one), capped at limit. It
// is what a system's Activity polls to explain its incidents, so the poll is
// one request however many members the system has. The system is resolved
// (and scope-checked) by the caller and passed by id.
func (p *PG) ListSystemAlarms(ctx context.Context, systemID string, window time.Duration, limit int) ([]Alarm, error) {
	bound, args := tsSince("a.cleared_at", window, systemID, limit)
	rows, err := p.pool.Query(ctx, `
		select `+alarmCols+`
		from alarm a
		where a.component_id in (select m.component_id from system_member m where m.system_id = $1::uuid)
		  and (a.cleared_at is null or `+bound+`)
		order by a.raised_at desc, a.id desc
		limit $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list system alarms %s: %w", systemID, err)
	}
	defer rows.Close()

	out := []Alarm{}
	for rows.Next() {
		a, err := scanAlarm(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan system alarm: %w", err)
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// activeAlarms is the health report's read: what is currently wrong with a
// component, so the report can name the alarm behind a down occupant. Takes the
// component's id directly (resolveHealthRoles' assignee join already has it;
// see resolvedRole.AssignedIDs), not its name: two components can share a name
// once #627 lands, but system_role_assignment.component_id never does.
func (p *PG) activeAlarms(ctx context.Context, q txQuerier, componentID string) ([]Alarm, error) {
	rows, err := q.Query(ctx, `
		select `+alarmCols+`
		from alarm a
		where a.component_id = $1::uuid and a.cleared_at is null
		order by a.raised_at desc, a.id desc`, componentID)
	if err != nil {
		return nil, fmt.Errorf("storage: active alarms %q: %w", componentID, err)
	}
	defer rows.Close()

	var out []Alarm
	for rows.Next() {
		a, err := scanAlarm(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan active alarm: %w", err)
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// clearAlarmByDedupTx clears the open alarm of one condition on a component
// raised by ruleID (the rule evaluator's clear, #919; a rule clears only its
// own alarms), audited and with health recomputed in
// the caller's transaction. It returns the cleared alarm's id, which the
// rule's clear event carries (#949). No open alarm of that condition is not
// an error: there is nothing to clear, nothing is written, and the id is "".
func (p *PG) clearAlarmByDedupTx(ctx context.Context, tx pgx.Tx, actorID string, component *Component, dedupKey string, ruleID *string) (string, error) {
	var alarmID string
	var cleared time.Time
	err := tx.QueryRow(ctx, `
		update alarm set cleared_at = now(), updated_at = now()
		where component_id = $1::uuid and dedup_key = $2 and cleared_at is null
		  and raised_by_rule_id is not distinct from $3::uuid
		returning id, cleared_at`, component.ID, dedupKey, ruleID).Scan(&alarmID, &cleared)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: clear alarm %s on %q: %w", dedupKey, component.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "alarm", alarmID, nil,
		map[string]any{"component": component.Name, "dedup_key": dedupKey, "cleared_at": cleared, "raised_by_rule_id": ruleID}); err != nil {
		return "", err
	}
	if err := p.RecomputeHealth(ctx, tx, component.ID); err != nil {
		return "", err
	}
	return alarmID, p.queueNotificationsTx(ctx, tx, component.ID, alarmID, TransitionCleared)
}
