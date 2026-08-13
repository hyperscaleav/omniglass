package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The alarm write side. An alarm is component-local and impairs it wholesale
// (#626): a component's own verdict is the worst of its active alarms'
// severities, and the rollup notices when that verdict is no longer Healthy,
// which is the only way an alarm reaches a system now.
//
// Raising and clearing both recompute health in the SAME transaction. That is the
// slice's load-bearing rule: an alarm and the verdict it caused must never be
// separately visible, and a verdict computed later would stamp its transition at
// the time somebody looked rather than the time the estate changed.

// Alarm is one raised condition on a component. ClearedAt is nil while the alarm
// is active; clearing keeps the row, so the record of what was wrong and when
// survives the fix.
type Alarm struct {
	ID          string
	ComponentID string
	Severity    string
	Message     string
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
	// AcknowledgedBy is the acknowledger's label, not their uuid: it is what an
	// operator surface renders (ADR-0062), and it reads empty once that
	// principal is purged, while audit_log keeps the name it denormalized.
	AcknowledgedAt *time.Time
	AcknowledgedBy string
}

// Active reports whether the alarm is still raised.
func (a Alarm) Active() bool { return a.ClearedAt == nil }

// Acknowledged reports whether somebody has recorded that they have seen this
// alarm. It says nothing about whether the alarm is still raised.
func (a Alarm) Acknowledged() bool { return a.AcknowledgedAt != nil }

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
)

// alarmSeverities is the severity domain, mirroring the table's CHECK. Validating
// here turns an operator typo into a named refusal instead of a constraint
// violation surfacing as a 500.
var alarmSeverities = map[string]bool{"info": true, "warning": true, "critical": true}

// alarmCols is the read shape. acknowledged_by resolves through principal_label
// rather than surfacing the uuid, so every alarm read hands the API an
// operator-facing name (ADR-0062); it reads null once that principal is purged,
// which is honest, and audit_log still names them.
const alarmCols = `a.id, a.component_id, a.severity, a.message, a.dedup_key, a.raised_at, a.cleared_at,
	a.acknowledged_at, coalesce(principal_label(a.acknowledged_by), '')`

func scanAlarm(row pgx.Row) (*Alarm, error) {
	var a Alarm
	if err := row.Scan(&a.ID, &a.ComponentID, &a.Severity, &a.Message,
		&a.DedupKey, &a.RaisedAt, &a.ClearedAt, &a.AcknowledgedAt, &a.AcknowledgedBy); err != nil {
		return nil, err
	}
	return &a, nil
}

// RaiseAlarm records a condition on a component, then recomputes the health
// chain in the same transaction. An unknown component is ErrComponentNotFound.
func (p *PG) RaiseAlarm(ctx context.Context, actorID, componentName string, spec AlarmSpec) (*Alarm, error) {
	if !alarmSeverities[spec.Severity] {
		return nil, ErrAlarmSeverity
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
	// ComponentID is seeded from componentName (the caller's ref), not
	// component.ID, on purpose: that already mismatched the DB-scanned shape
	// alarmCols reads on the on-conflict re-read path below (a real uuid)
	// before this refactor ever touched the file, and fixing the mismatch is
	// a separate concern from this task's no-op mandate (TestRaiseAlarmWithoutCapabilities
	// pins the existing, mismatched contract).
	a := Alarm{ComponentID: componentName, Severity: spec.Severity, Message: spec.Message, DedupKey: spec.DedupKey}
	err = tx.QueryRow(ctx, `
		insert into alarm (component_id, severity, message, dedup_key)
		values ($1::uuid, $2, $3, coalesce(nullif($4, ''), gen_random_uuid()::text))
		on conflict (component_id, dedup_key) where cleared_at is null do nothing
		returning id, raised_at, dedup_key`,
		component.ID, spec.Severity, spec.Message, spec.DedupKey).Scan(&a.ID, &a.RaisedAt, &a.DedupKey)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, gerr := scanAlarm(tx.QueryRow(ctx, `
			select `+alarmCols+`
			from alarm a
			where a.component_id = $1::uuid
			  and a.dedup_key = $2 and a.cleared_at is null`, component.ID, spec.DedupKey))
		if gerr != nil {
			return nil, fmt.Errorf("storage: read existing open alarm on %q: %w", componentName, gerr)
		}
		return existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: insert alarm on %q: %w", componentName, err)
	}

	if err := writeAuditRes(ctx, tx, actorID, "create", "alarm", a.ID, nil, a); err != nil {
		return nil, err
	}
	if err := p.RecomputeHealth(ctx, tx, component.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit raise alarm: %w", err)
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
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit clear alarm: %w", err)
	}
	return nil
}

// AcknowledgeAlarm records that a human has seen this alarm, and changes nothing
// else. It deliberately does NOT recompute health: an alarm's raised state
// belongs to its condition, so acknowledging is not fixing, and a recompute here
// would stamp a transition at a moment when nothing about the estate changed.
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
