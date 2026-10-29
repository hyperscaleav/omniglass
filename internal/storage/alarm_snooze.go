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
	// Every write to one condition's snooze serializes here, held to the end
	// of the transaction: a row lock cannot, since the first snooze of a
	// condition has no row to lock, and two of them would both audit an empty
	// before.
	if err := lockAdvisory(ctx, tx, snoozeLockKey(component.ID, key)); err != nil {
		return nil, "", err
	}
	return component, key, nil
}

// snoozeLockKey names one condition's snooze for the advisory lock.
func snoozeLockKey(componentID, dedupKey string) string {
	return "alarm-snooze:" + componentID + "/" + dedupKey
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
	if err := endExpiredTx(ctx, p, tx, component, key); err != nil {
		return nil, err
	}
	// The deadline in force before, read under the condition's lock so two
	// snoozes of one condition audit a true before each.
	var before *time.Time
	err = tx.QueryRow(ctx, `select snoozed_until from alarm_snooze
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
	// The before image always names the deadline, null when the condition was
	// not snoozed: an alarm's audit rows outlive it (#1339), so each says what
	// it changed on its own.
	old := map[string]any{"snoozed_until": before}
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
// on, early, audited on that alarm as unsnooze naming the caller, and tells
// people again about the condition if it is still raised, exactly as the
// deadline passing would (endSnoozeTx). A condition
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
	// A snooze past its deadline that the sweep has not reached was not
	// ended by this caller: it is ended as the sweep would end it (snooze-end
	// from the platform, and the announcement it owes), and what is left to
	// unsnooze is nothing.
	if err := endExpiredTx(ctx, p, tx, component, key); err != nil {
		return nil, err
	}
	var until time.Time
	err = tx.QueryRow(ctx, `delete from alarm_snooze where component_id = $1::uuid and dedup_key = $2
		returning snoozed_until`, component.ID, key).Scan(&until)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: unsnooze alarm %s: %w", alarmID, err)
	default:
		if err := endSnoozeTx(ctx, p, tx, actorID, "unsnooze", alarmID, component, key, until); err != nil {
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

// SnoozeReason is the reason a suppressed row carries (#1318): when the
// snooze on its condition ends and who set it. The deadline is written in
// UTC, the one zone every reader of the row shares.
func SnoozeReason(until time.Time, by string) string {
	r := "the condition is snoozed until " + until.UTC().Format("2006-01-02 15:04 UTC")
	if by != "" {
		r += " by " + by
	}
	return r
}

// conditionSnooze reads the snooze in force on an alarm's condition, by the
// alarm's id, as the reason a row about it is suppressed: "" when the
// condition is not snoozed. Two primary-key lookups (the alarm, then its
// condition's snooze), so the queue seam and the tick can afford it on every
// transition and every row.
//
// With lock (the queue seam) it first takes the condition's snooze lock SHARED, to the end of the
// caller's transaction, against the verbs' exclusive hold: a snooze cannot
// commit between this read and the commit of the rows it judged (an inbox
// row is delivered by that commit, so no later check could catch it), and a
// transition of the condition waits for a snooze in flight. Shared, so
// transitions never wait on each other. The verbs take nothing before the
// lock and the sweep only tries it, so no order of the three can deadlock.
func conditionSnooze(ctx context.Context, q txQuerier, alarmID string, lock bool) (string, error) {
	var componentID, key string
	err := q.QueryRow(ctx, `select component_id::text, dedup_key from alarm where id = $1::uuid`, alarmID).Scan(&componentID, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: read alarm %s: %w", alarmID, err)
	}
	if lock {
		if _, err := q.Exec(ctx, `select pg_advisory_xact_lock_shared(hashtextextended($1, 0))`, snoozeLockKey(componentID, key)); err != nil {
			return "", fmt.Errorf("storage: lock the snooze on alarm %s: %w", alarmID, err)
		}
	}
	var until time.Time
	var username, serviceName, nodeName *string
	err = q.QueryRow(ctx, `select s.snoozed_until, `+principalIdentCols("s.snoozed_by")+`
		from alarm_snooze s where s.component_id = $1::uuid and s.dedup_key = $2 and s.snoozed_until > now()`,
		componentID, key).Scan(&until, &username, &serviceName, &nodeName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: read the snooze on alarm %s: %w", alarmID, err)
	}
	return SnoozeReason(until, principalIdent(username, serviceName, nodeName)), nil
}

// endSnoozeTx is what the end of a snooze does, by hand or at the deadline,
// in the transaction that deleted its row (#1318, ADR-1318): audit the end
// on auditAlarmID (the alarm :unsnooze named, or the condition's latest alarm
// for the sweep), then, if an alarm of the condition is still open, announce
// it again as a raised transition through the one queue seam, judged against
// the rules and memberships as they stand now. The audit row goes first, so
// at one instant the timeline reads the end before the rows it caused. It is
// the one operator act that queues notifications: a snooze notifies nothing,
// and its end re-sends only a transition the platform already announced.
func endSnoozeTx(ctx context.Context, p *PG, tx pgx.Tx, actorID, verb, auditAlarmID string, component *Component, key string, until time.Time) error {
	if auditAlarmID != "" {
		if err := writeAuditRes(ctx, tx, actorID, verb, "alarm", auditAlarmID,
			map[string]any{"snoozed_until": until}, map[string]any{"component": component.Name, "dedup_key": key, "snoozed_until": nil}); err != nil {
			return err
		}
	}
	var open string
	err := tx.QueryRow(ctx, `select id::text from alarm where component_id = $1::uuid and dedup_key = $2 and cleared_at is null`,
		component.ID, key).Scan(&open)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: read the open alarm of a snoozed condition: %w", err)
	}
	return p.queueNotificationsTx(ctx, tx, component.ID, open, TransitionRaised)
}

// endExpiredTx ends the condition's snooze if it is past its deadline, as
// the sweep does: deleted, snooze-end audited from the platform on the
// condition's latest alarm, and a still-raised condition announced again. A
// verb calls it first under the condition's lock, so a snooze whose deadline
// passed is never swallowed by a later act (an unsnooze or a re-snooze in
// the moments before the sweep reaches it) without the announcement it owes.
func endExpiredTx(ctx context.Context, p *PG, tx pgx.Tx, component *Component, key string) error {
	var until time.Time
	err := tx.QueryRow(ctx, `delete from alarm_snooze where component_id = $1::uuid and dedup_key = $2 and snoozed_until <= now()
		returning snoozed_until`, component.ID, key).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: end an expired snooze: %w", err)
	}
	var latest string
	err = tx.QueryRow(ctx, `select id::text from alarm where component_id = $1::uuid and dedup_key = $2
		order by raised_at desc, id desc limit 1`, component.ID, key).Scan(&latest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("storage: read a snoozed condition's latest alarm: %w", err)
	}
	return endSnoozeTx(ctx, p, tx, "", "snooze-end", latest, component, key, until)
}

// EndExpiredSnoozes is the end-of-snooze sweep (#1318), run on the
// notifier's tick until the time primitive (#419) exists: it ends up to max
// snoozes past their deadline, each in its own transaction, taken FOR UPDATE
// SKIP LOCKED (a snooze another replica holds is skipped, not waited on) and
// deleted in the same transaction that audits snooze-end from the platform on
// the condition's latest alarm and announces a still-raised condition again.
// Two replicas therefore never end one snooze twice, and a restart loses
// nothing. A condition whose lock is held (a verb or a transition of it in
// flight) is skipped for the rest of the pass, so it never holds up the
// others. It reports how many it ended.
func (p *PG) EndExpiredSnoozes(ctx context.Context, max int) (int, error) {
	ended := 0
	skip := []string{}
	for ended < max {
		took, held, err := p.endNextExpiredSnooze(ctx, skip)
		if err != nil {
			return ended, err
		}
		switch {
		case held != "":
			skip = append(skip, held)
		case !took:
			return ended, nil
		default:
			ended++
		}
	}
	return ended, nil
}

// endNextExpiredSnooze ends the oldest expired snooze not in skip. It
// reports whether it ended one, or the condition (its lock key) it found
// held and left for a later tick.
func (p *PG) endNextExpiredSnooze(ctx context.Context, skip []string) (bool, string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, "", fmt.Errorf("storage: begin end snooze: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var c Component
	var key string
	err = tx.QueryRow(ctx, `select s.component_id::text, c.name, s.dedup_key
		from alarm_snooze s join component c on c.id = s.component_id
		where s.snoozed_until <= now() and not (('alarm-snooze:' || s.component_id::text || '/' || s.dedup_key) = any($1::text[]))
		order by s.snoozed_until, s.component_id, s.dedup_key
		for update of s skip locked limit 1`, skip).Scan(&c.ID, &c.Name, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("storage: take an expired snooze: %w", err)
	}
	// The condition's lock, tried and never waited on: a verb holding it may
	// be waiting on the row lock just taken, so waiting here could deadlock.
	// A held lock means a verb or a transition of this condition is in
	// flight; the condition is skipped for this pass and the next tick sees
	// what it left.
	lockKey := snoozeLockKey(c.ID, key)
	var locked bool
	if err := tx.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtextextended($1, 0))`, lockKey).Scan(&locked); err != nil {
		return false, "", fmt.Errorf("storage: lock an expired snooze: %w", err)
	}
	if !locked {
		return false, lockKey, nil
	}
	if err := endExpiredTx(ctx, p, tx, &c, key); err != nil {
		return false, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, "", fmt.Errorf("storage: commit end snooze: %w", err)
	}
	return true, "", nil
}
