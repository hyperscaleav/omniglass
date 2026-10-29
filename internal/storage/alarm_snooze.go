package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Snooze (#1318, ADR-1318): a fact about an alarm's CONDITION, the
// (component, dedup_key) identity of ADR-0075, set from any alarm of that
// condition and lasting until a deadline. Not a fact about one alarm row: a
// row has only two edges, and by the time an operator sees a raised alarm its
// raise has gone out, so a snooze on that row could only ever hold back its
// clear. A flapping condition opens a new row on every re-raise, and the
// snooze on the condition covers each of them until the deadline.
//
// A snooze governs notifications and nothing else: it does not acknowledge,
// does not recompute health, and is not ended by a clear.

// The bound on a snooze's duration (decision 3 of #1318): anything longer is
// a maintenance window or a paused rule, not a snooze.
const (
	SnoozeMin = time.Minute
	SnoozeMax = 7 * 24 * time.Hour
)

// ErrSnoozeDuration is a duration outside the bound: a 422 naming it.
var ErrSnoozeDuration = errors.New("storage: a snooze lasts from 1m to 7d (168h)")

// snoozeTarget resolves the alarm a snooze verb was called on, inside the
// verb's transaction: its component (by the name or id the route already
// resolved in scope) and its condition. Every miss (a malformed id, an
// unknown component, an id on another component) is ErrAlarmNotFound, as an
// acknowledgement's is.
func snoozeTarget(ctx context.Context, tx pgx.Tx, componentName, alarmID string) (*Component, string, error) {
	if _, err := uuid.Parse(alarmID); err != nil {
		return nil, "", ErrAlarmNotFound
	}
	component, err := scopedByName(ctx, tx, componentConfig, componentName)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, "", ErrAlarmNotFound
	} else if err != nil {
		return nil, "", err
	}
	var key string
	err = tx.QueryRow(ctx, `select dedup_key from alarm where id = $1 and component_id = $2::uuid`, alarmID, component.ID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrAlarmNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("storage: read alarm %s: %w", alarmID, err)
	}
	return component, key, nil
}

// readAlarmTx reads one alarm in the caller's transaction, so a verb returns
// the row as its own write left it.
func readAlarmTx(ctx context.Context, q querier, componentID, alarmID string) (*Alarm, error) {
	a, err := scanAlarm(q.QueryRow(ctx, `select `+alarmCols+` from alarm a where a.id = $1 and a.component_id = $2::uuid`, alarmID, componentID))
	if err != nil {
		return nil, fmt.Errorf("storage: read alarm %s: %w", alarmID, err)
	}
	return a, nil
}

// SnoozeAlarm snoozes the condition of the alarm it is called on for d from
// now, setting the deadline or replacing it (shorter or longer), and audits
// the call on that alarm with the deadline before (when one was in force)
// and after. It queues nothing: a snooze is an operator act, not a condition
// edge. A d outside [SnoozeMin, SnoozeMax] is ErrSnoozeDuration.
func (p *PG) SnoozeAlarm(ctx context.Context, actorID, componentName, alarmID string, d time.Duration) (*Alarm, error) {
	if d < SnoozeMin || d > SnoozeMax {
		return nil, ErrSnoozeDuration
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin snooze alarm: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	component, key, err := snoozeTarget(ctx, tx, componentName, alarmID)
	if err != nil {
		return nil, err
	}
	// The deadline in force before, read under the row's lock so two
	// snoozes of one condition audit a true before each. A row past its
	// deadline the sweep has not reached is no snooze in force.
	var before *time.Time
	err = tx.QueryRow(ctx, `select case when snoozed_until > now() then snoozed_until end from alarm_snooze
		where component_id = $1::uuid and dedup_key = $2 for update`, component.ID, key).Scan(&before)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("storage: read snooze: %w", err)
	}
	var until time.Time
	if err := tx.QueryRow(ctx, `insert into alarm_snooze (component_id, dedup_key, snoozed_until, snoozed_by)
		values ($1::uuid, $2, now() + make_interval(secs => $3), nullif($4, '')::uuid)
		on conflict (component_id, dedup_key) do update set snoozed_until = excluded.snoozed_until,
			snoozed_by = excluded.snoozed_by, updated_at = now()
		returning snoozed_until`, component.ID, key, d.Seconds(), actorID).Scan(&until); err != nil {
		return nil, fmt.Errorf("storage: snooze alarm %s: %w", alarmID, err)
	}
	var old map[string]any
	if before != nil {
		old = map[string]any{"snoozed_until": before}
	}
	if err := writeAuditRes(ctx, tx, actorID, "snooze", "alarm", alarmID, old,
		map[string]any{"component": component.Name, "dedup_key": key, "snoozed_until": until}); err != nil {
		return nil, err
	}
	a, err := readAlarmTx(ctx, tx, component.ID, alarmID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit snooze alarm: %w", err)
	}
	return a, nil
}

// UnsnoozeAlarm ends the snooze on the condition of the alarm it is called
// on, early, audited on that alarm as unsnooze naming the caller. A condition
// that is not snoozed is not an error: nothing changed, so nothing is
// written, and the alarm is returned as it reads.
func (p *PG) UnsnoozeAlarm(ctx context.Context, actorID, componentName, alarmID string) (*Alarm, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin unsnooze alarm: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	component, key, err := snoozeTarget(ctx, tx, componentName, alarmID)
	if err != nil {
		return nil, err
	}
	var until time.Time
	err = tx.QueryRow(ctx, `delete from alarm_snooze where component_id = $1::uuid and dedup_key = $2 returning snoozed_until`,
		component.ID, key).Scan(&until)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: unsnooze alarm %s: %w", alarmID, err)
	default:
		if err := writeAuditRes(ctx, tx, actorID, "unsnooze", "alarm", alarmID,
			map[string]any{"snoozed_until": until}, map[string]any{"component": component.Name, "dedup_key": key}); err != nil {
			return nil, err
		}
	}
	a, err := readAlarmTx(ctx, tx, component.ID, alarmID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit unsnooze alarm: %w", err)
	}
	return a, nil
}
