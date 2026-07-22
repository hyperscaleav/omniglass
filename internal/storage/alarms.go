package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The alarm write side. An alarm is component-local and names the capabilities it
// degrades, which is the only way it reaches a system: a role requires
// capabilities, a component provides them, an alarm takes some away, and the
// rollup notices when what is left no longer meets a role's requirement at quorum.
//
// Raising and clearing both recompute health in the SAME transaction. That is the
// slice's load-bearing rule: an alarm and the verdict it caused must never be
// separately visible, and a verdict computed later would stamp its transition at
// the time somebody looked rather than the time the estate changed.

// Alarm is one raised condition on a component. ClearedAt is nil while the alarm
// is active; clearing keeps the row, so the record of what was wrong and when
// survives the fix.
type Alarm struct {
	ID           string
	ComponentID  string
	Severity     string
	Message      string
	RaisedAt     time.Time
	ClearedAt    *time.Time
	Capabilities []string
}

// Active reports whether the alarm is still raised.
func (a Alarm) Active() bool { return a.ClearedAt == nil }

// AlarmSpec is the raise input. Capabilities is what the condition takes away;
// an alarm naming none is a note on the component that never reaches a system.
type AlarmSpec struct {
	Severity     string
	Message      string
	Capabilities []string
}

// Alarm sentinels. A bad severity and an unknown capability are request faults
// (422), not server errors: both are things the caller sent.
var (
	ErrAlarmNotFound    = errors.New("storage: alarm not found or already cleared")
	ErrAlarmSeverity    = errors.New("storage: unknown alarm severity")
	ErrAlarmRefNotFound = errors.New("storage: alarm references a missing capability")
)

// alarmSeverities is the severity domain, mirroring the table's CHECK. Validating
// here turns an operator typo into a named refusal instead of a constraint
// violation surfacing as a 500.
var alarmSeverities = map[string]bool{"info": true, "warning": true, "critical": true}

const alarmCols = `a.id, a.component_id, a.severity, a.message, a.raised_at, a.cleared_at,
	coalesce(array_agg(ac.capability_id order by ac.capability_id)
	         filter (where ac.capability_id is not null), '{}')`

func scanAlarm(row pgx.Row) (*Alarm, error) {
	var a Alarm
	if err := row.Scan(&a.ID, &a.ComponentID, &a.Severity, &a.Message,
		&a.RaisedAt, &a.ClearedAt, &a.Capabilities); err != nil {
		return nil, err
	}
	return &a, nil
}

// RaiseAlarm records a condition on a component and the capabilities it degrades,
// then recomputes the health chain in the same transaction. An unknown component
// is ErrComponentNotFound; an unknown capability is ErrAlarmRefNotFound.
func (p *PG) RaiseAlarm(ctx context.Context, actorID, componentName string, spec AlarmSpec) (*Alarm, error) {
	if !alarmSeverities[spec.Severity] {
		return nil, ErrAlarmSeverity
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin raise alarm: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The component is resolved before the insert so a typo reads as a missing
	// component rather than an opaque foreign-key fault.
	if _, err := scopedByName(ctx, tx, componentConfig, componentName); err != nil {
		return nil, err
	}

	a := Alarm{ComponentID: componentName, Severity: spec.Severity, Message: spec.Message}
	if err := tx.QueryRow(ctx, `
		insert into alarm (component_id, severity, message)
		values ((select id from component where name = $1), $2, $3)
		returning id, raised_at`,
		componentName, spec.Severity, spec.Message).Scan(&a.ID, &a.RaisedAt); err != nil {
		return nil, fmt.Errorf("storage: insert alarm on %q: %w", componentName, err)
	}
	if len(spec.Capabilities) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into alarm_capability (alarm_id, capability_id)
			select $1, c from unnest($2::text[]) c
			on conflict (alarm_id, capability_id) do nothing`, a.ID, spec.Capabilities); err != nil {
			return nil, mapAlarmWriteErr(err)
		}
	}
	a.Capabilities = append([]string(nil), spec.Capabilities...)

	if err := writeAuditRes(ctx, tx, actorID, "create", "alarm", a.ID, nil, a); err != nil {
		return nil, err
	}
	if err := p.RecomputeHealth(ctx, tx, componentName); err != nil {
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

	var cleared time.Time
	if err := tx.QueryRow(ctx, `
		update alarm set cleared_at = now(), updated_at = now()
		where id = $1 and component_id = (select id from component where name = $2) and cleared_at is null
		returning cleared_at`, alarmID, componentName).Scan(&cleared); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAlarmNotFound
		}
		return fmt.Errorf("storage: clear alarm %s: %w", alarmID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "alarm", alarmID, nil,
		map[string]any{"component": componentName, "cleared_at": cleared}); err != nil {
		return err
	}
	if err := p.RecomputeHealth(ctx, tx, componentName); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit clear alarm: %w", err)
	}
	return nil
}

// ListAlarms returns a component's alarms, newest first: the active set by
// default, the whole history when includeCleared. An unknown component is
// ErrComponentNotFound rather than an empty list, so a typo is visible.
func (p *PG) ListAlarms(ctx context.Context, componentName string, includeCleared bool) ([]Alarm, error) {
	if _, err := scopedByName(ctx, p.pool, componentConfig, componentName); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		select `+alarmCols+`
		from alarm a
		left join alarm_capability ac on ac.alarm_id = a.id
		where a.component_id = (select id from component where name = $1) and ($2 or a.cleared_at is null)
		group by a.id
		order by a.raised_at desc, a.id desc`, componentName, includeCleared)
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
// component, with the capabilities each alarm degrades, so the report can name
// the alarm behind an impaired role.
func (p *PG) activeAlarms(ctx context.Context, q txQuerier, componentName string) ([]Alarm, error) {
	rows, err := q.Query(ctx, `
		select `+alarmCols+`
		from alarm a
		left join alarm_capability ac on ac.alarm_id = a.id
		where a.component_id = (select id from component where name = $1) and a.cleared_at is null
		group by a.id
		order by a.raised_at desc, a.id desc`, componentName)
	if err != nil {
		return nil, fmt.Errorf("storage: active alarms %q: %w", componentName, err)
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

// mapAlarmWriteErr turns a capability foreign-key violation into the request
// fault it is. Anything else is a real failure.
func mapAlarmWriteErr(err error) error {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23503" {
		return ErrAlarmRefNotFound
	}
	return fmt.Errorf("storage: alarm write: %w", err)
}
