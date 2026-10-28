package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/sandbox"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// The notification delivery row (#618, #1321): one message on its way, a
// stateful row holding its own delivery state. It is written in the SAME
// transaction as the alarm transition it is about, at the one seam every raise
// and clear passes (raiseAlarmTx, ClearAlarm, clearAlarmByDedupTx), so no
// transition is lost and none is written twice; it is rendered then, so a
// retry sends the same bytes; and a server tick drains it (ADR-0618), each
// row under FOR UPDATE SKIP LOCKED held across its send, so two replicas never
// send one row twice unless a replica dies between the send and its commit.

// Notification statuses.
const (
	NotificationQueued = "queued"
	NotificationSent   = "sent"
	NotificationFailed = "failed"
)

// The retry policy, code constants named in the docs (decision 5 of #618): a
// failed attempt waits NotificationBackoff, doubling each time, and the
// NotificationMaxAttempts-th failure is final (about 21 minutes from the
// first attempt to the last).
const (
	NotificationMaxAttempts = 8
	NotificationBackoff     = 10 * time.Second
)

// BackoffAfter is the wait after the n-th failed attempt (n from 1).
func BackoffAfter(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	return NotificationBackoff << (n - 1)
}

// Notification is one delivery row as it reads.
type Notification struct {
	ID            string
	AlarmID       string
	ComponentID   string
	ComponentName string
	Transition    string
	Rule          string
	Type          string
	Target        string
	Subject       string
	Text          string
	Status        string
	Attempts      int
	NextAttemptAt *time.Time
	LastStatus    *int
	LastError     string
	SentAt        *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Notification sentinels.
var (
	ErrNotificationNotFound  = errors.New("storage: notification not found")
	ErrNotificationForbidden = errors.New("storage: notification is outside the action's scope")
	// ErrNotificationNotRetryable is a :retry of a notification that has not
	// failed: a queued one is already on its way, a sent one arrived.
	ErrNotificationNotRetryable = errors.New("storage: only a failed notification that was attempted is retried")
)

const notificationCols = `n.id::text, n.alarm_id::text, n.component_id::text, coalesce((select c.name from component c where c.id = n.component_id), ''),
	n.transition, n.rule_name, n.type_name, n.target_name, n.subject, n.text, n.status, n.attempts,
	case when n.status = 'queued' then n.next_attempt_at end, n.last_status, coalesce(n.last_error, ''), n.sent_at, n.created_at, n.updated_at`

func scanNotification(row pgx.Row) (*Notification, error) {
	var n Notification
	if err := row.Scan(&n.ID, &n.AlarmID, &n.ComponentID, &n.ComponentName, &n.Transition, &n.Rule, &n.Type, &n.Target,
		&n.Subject, &n.Text, &n.Status, &n.Attempts, &n.NextAttemptAt, &n.LastStatus, &n.LastError, &n.SentAt, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	return &n, nil
}

// --- the queue: written in the alarm transition's transaction ---------------

// queuedRule is one enabled action rule that fires on a transition, with its
// type's templates (the rule's own, else the official one for the
// transition) and its enabled targets.
type queuedRule struct {
	id, name, when         string
	typeID, typeName       string
	subject, text          string
	targetIDs, targetNames []string
}

// queueNotificationsTx writes the notifications one alarm transition causes,
// in the caller's transaction: for every enabled action rule on the
// transition whose when holds over the alarm's facts, one row per enabled
// target. Nothing queues when no rule watches the transition, which is one
// indexed statement. A rule whose when fails to run, or whose type fails to
// render, still writes its rows, failed with the reason, so the problem is on
// the alarm's timeline rather than in a log; the transition itself never
// fails for a notification.
func (p *PG) queueNotificationsTx(ctx context.Context, tx pgx.Tx, componentID, alarmID, transition string) error {
	// Everything a notification writes runs under a savepoint: whatever goes
	// wrong in it (a rule deleted between the read and the insert, a value
	// the database refuses) rolls back to here and is logged, and the alarm
	// transition commits as if no rule had matched. The notification lane
	// never fails, or stalls into failing, the transition it is about.
	if _, err := tx.Exec(ctx, `savepoint notify_queue`); err != nil {
		return fmt.Errorf("storage: notification savepoint: %w", err)
	}
	if err := p.queueNotifications(ctx, tx, componentID, alarmID, transition); err != nil {
		if _, rerr := tx.Exec(ctx, `rollback to savepoint notify_queue`); rerr != nil {
			return fmt.Errorf("storage: roll back the notification savepoint: %w (after %v)", rerr, err)
		}
		slog.Warn("an alarm transition's notifications were not queued", "facility", "notify",
			"alarm", alarmID, "transition", transition, "error", err.Error())
		return nil
	}
	if _, err := tx.Exec(ctx, `release savepoint notify_queue`); err != nil {
		return fmt.Errorf("storage: release the notification savepoint: %w", err)
	}
	return nil
}

func (p *PG) queueNotifications(ctx context.Context, tx pgx.Tx, componentID, alarmID, transition string) error {
	rs, err := rulesOn(ctx, tx, transition)
	if err != nil || len(rs) == 0 {
		return err
	}
	facts, _, err := p.alarmFacts(ctx, tx, componentID, alarmID, transition)
	if err != nil {
		return fmt.Errorf("storage: notification facts for alarm %s: %w", alarmID, err)
	}
	in, err := actionInput(ctx, tx, facts)
	if err != nil {
		return err
	}
	eng, err := p.notificationEngine(ctx, tx)
	if err != nil {
		return err
	}
	factsJSON, err := json.Marshal(facts)
	if err != nil {
		return fmt.Errorf("storage: encode notification facts: %w", err)
	}
	for _, r := range rs {
		status, subject, text, reason := NotificationQueued, "", "", ""
		when, err := rules.CompileWhen(r.when)
		holds := false
		if err == nil {
			holds, err = when.Holds(in)
		}
		switch {
		case err != nil:
			status, reason = NotificationFailed, "the rule's when did not run: "+err.Error()
		case !holds:
			continue
		default:
			subject, text, err = renderNotification(eng, r.subject, r.text, facts)
			if err != nil {
				status, reason = NotificationFailed, "the notification did not render: "+TemplateRefusal(err)
			}
		}
		for i, targetID := range r.targetIDs {
			if _, err := tx.Exec(ctx, `insert into notification (alarm_id, component_id, transition, rule_id, rule_name,
					notification_type_id, type_name, target_id, target_name, subject, text, facts, status, last_error)
				values ($1::uuid, $2::uuid, $3, $4::uuid, $5, $6::uuid, $7, $8::uuid, $9, $10, $11, $12, $13, nullif($14, ''))`,
				alarmID, componentID, transition, r.id, r.name, r.typeID, r.typeName, targetID, r.targetNames[i],
				subject, text, factsJSON, status, reason); err != nil {
				return fmt.Errorf("storage: queue notification for rule %s: %w", r.name, err)
			}
		}
	}
	return nil
}

// rulesOn reads the enabled rules on a transition with their templates and
// enabled targets; none (the usual case) costs one indexed statement.
func rulesOn(ctx context.Context, q querier, transition string) ([]queuedRule, error) {
	rows, err := q.Query(ctx, `
		select r.id::text, r.name, r.when_expr, t.id::text, t.name, t.subject, t.text,
			coalesce(array_agg(nt.id::text order by nt.name) filter (where nt.id is not null), '{}'),
			coalesce(array_agg(nt.name order by nt.name) filter (where nt.id is not null), '{}')
		from action_rule r
		join notification_type t on t.id = coalesce(r.notification_type_id, (select id from notification_type where name = $2 and official))
		left join action_rule_target art on art.rule_id = r.id
		left join notification_target nt on nt.id = art.target_id and nt.enabled
		where r.enabled and $1 = any(r.on_transitions)
		group by r.id, r.name, r.when_expr, t.id, t.name, t.subject, t.text
		order by r.name`, transition, officialTypeFor(transition))
	if err != nil {
		return nil, fmt.Errorf("storage: action rules on %s: %w", transition, err)
	}
	defer rows.Close()
	var out []queuedRule
	for rows.Next() {
		var r queuedRule
		if err := rows.Scan(&r.id, &r.name, &r.when, &r.typeID, &r.typeName, &r.subject, &r.text, &r.targetIDs, &r.targetNames); err != nil {
			return nil, fmt.Errorf("storage: scan action rule: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// actionInput is the action site's input for a fact map: the facts in
// snake_case, and the severity's rank.
func actionInput(ctx context.Context, q querier, f sandbox.Data) (rules.ActionInput, error) {
	in := rules.ActionInput{
		Component: f["Component"], ComponentLabel: f["ComponentLabel"], ComponentType: f["ComponentType"], Place: f["Place"],
		Severity: f["Severity"], SeverityLabel: f["SeverityLabel"], Message: f["Message"], Transition: f["Transition"],
		RaisedAt: f["RaisedAt"], ClearedAt: f["ClearedAt"], Rule: f["Rule"], Link: f["Link"],
	}
	err := q.QueryRow(ctx, `select coalesce((select rank from severity_level where name = $1), 0)`, in.Severity).Scan(&in.SeverityRank)
	if err != nil {
		return in, fmt.Errorf("storage: severity rank: %w", err)
	}
	return in, nil
}

// --- the drain: one row per transaction, its send inside it -----------------

// NotificationSend is what a send needs for one due notification: the
// envelope's content and the target's destination and credential.
type NotificationSend struct {
	ID, AlarmID, Type, Transition string
	Subject, Text                 string
	Facts                         map[string]string
	Target                        TargetSend
	// Refused is set when there is no send to make (the target was deleted,
	// disabled, or its secret no longer unseals): the attempt fails with it.
	Refused string
}

// SendResult is what an attempt got back.
type SendResult struct {
	OK     bool
	Status int
	Reason string
}

// SendFunc makes one send; the storage layer never dials (the egress client
// lives outside the Storage Gateway).
type SendFunc func(context.Context, NotificationSend) SendResult

// DeliverNext delivers the next due notification, if any: it takes the
// oldest due row FOR UPDATE SKIP LOCKED (a row another replica holds is
// skipped, not waited on), sends it through send while holding that lock,
// and records the outcome in the same transaction: sent, or queued again
// after its backoff, or failed after the last attempt. It reports whether a
// row was taken. A send the caller's context cancels rolls back and leaves
// the row queued.
func (p *PG) DeliverNext(ctx context.Context, send SendFunc) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("storage: begin deliver notification: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var ns NotificationSend
	var targetID *string
	var attempts int
	var factsRaw []byte
	err = tx.QueryRow(ctx, `select id::text, alarm_id::text, type_name, transition, subject, text, facts, target_id::text, attempts
		from notification where status = 'queued' and next_attempt_at <= now()
		order by next_attempt_at, id for update skip locked limit 1`).
		Scan(&ns.ID, &ns.AlarmID, &ns.Type, &ns.Transition, &ns.Subject, &ns.Text, &factsRaw, &targetID, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: take due notification: %w", err)
	}
	_ = json.Unmarshal(factsRaw, &ns.Facts)
	switch {
	case targetID == nil:
		ns.Refused = "the target was deleted"
	default:
		ts, err := p.targetSend(ctx, tx, *targetID)
		switch {
		case errors.Is(err, ErrNotificationTargetNotFound):
			ns.Refused = "the target was deleted"
		case err != nil:
			return true, err
		case !ts.Enabled:
			ns.Refused = "the target is disabled"
		case ts.CredentialMissing:
			ns.Refused = "the target's secret no longer unseals"
		default:
			ns.Target = *ts
		}
	}
	res := SendResult{Reason: ns.Refused}
	if ns.Refused == "" {
		res = send(ctx, ns)
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	attempts++
	switch {
	case res.OK:
		_, err = tx.Exec(ctx, `update notification set status = 'sent', attempts = $2, last_status = $3, last_error = null,
			sent_at = now(), updated_at = now() where id = $1`, ns.ID, attempts, res.Status)
	case attempts >= NotificationMaxAttempts || ns.Refused != "":
		_, err = tx.Exec(ctx, `update notification set status = 'failed', attempts = $2, last_status = nullif($3, 0), last_error = $4,
			updated_at = now() where id = $1`, ns.ID, attempts, res.Status, res.Reason)
	default:
		_, err = tx.Exec(ctx, `update notification set attempts = $2, last_status = nullif($3, 0), last_error = $4,
			next_attempt_at = now() + make_interval(secs => $5), updated_at = now() where id = $1`,
			ns.ID, attempts, res.Status, res.Reason, BackoffAfter(attempts).Seconds())
	}
	if err != nil {
		return true, fmt.Errorf("storage: record notification %s: %w", ns.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return true, fmt.Errorf("storage: commit notification %s: %w", ns.ID, err)
	}
	if !res.OK {
		slog.Info("notification attempt failed", "facility", "notify", "notification", ns.ID, "attempt", attempts, "status", res.Status, "reason", res.Reason)
	}
	return true, nil
}

// NotificationBacklog counts the queued notifications, due now or later.
func (p *PG) NotificationBacklog(ctx context.Context) (int, error) {
	var n int
	if err := p.pool.QueryRow(ctx, `select count(*) from notification where status = 'queued'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: notification backlog: %w", err)
	}
	return n, nil
}

// --- the reads: scoped to the alarm's component ------------------------------

// NotificationFilter narrows a list: by alarm, target and status, newest
// first, at most Limit rows (100 when zero, 500 at most).
type NotificationFilter struct {
	AlarmID string
	Target  string
	Status  string
	Limit   int
}

// ListNotifications lists notifications about components the read scope
// admits, newest first.
func (p *PG) ListNotifications(ctx context.Context, f NotificationFilter, read scope.Set) ([]Notification, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if f.AlarmID != "" && !isUUID(f.AlarmID) {
		return []Notification{}, nil
	}
	rf := newReadFilter(componentTable, read, 1)
	args := append(rf.Args, nullIfEmpty(f.AlarmID), nullIfEmpty(f.Target), nullIfEmpty(f.Status), limit)
	n := len(rf.Args)
	ph := func(i int) string { return "$" + strconv.Itoa(n+i) }
	rows, err := p.pool.Query(ctx, rf.With+`select `+notificationCols+` from notification n
		where `+rf.Pred("n.component_id")+`
		  and (`+ph(1)+`::uuid is null or n.alarm_id = `+ph(1)+`::uuid)
		  and (`+ph(2)+`::text is null or n.target_name = `+ph(2)+`)
		  and (`+ph(3)+`::text is null or n.status = `+ph(3)+`)
		order by n.created_at desc, n.id desc limit `+ph(4), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list notifications: %w", err)
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		x, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan notification: %w", err)
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// GetNotification reads one notification by id; one about a component
// outside the read scope is the non-disclosing ErrNotificationNotFound.
func (p *PG) GetNotification(ctx context.Context, id string, read scope.Set) (*Notification, error) {
	return getNotification(ctx, p.pool, id, read, false)
}

func getNotification(ctx context.Context, q querier, id string, read scope.Set, forUpdate bool) (*Notification, error) {
	if !isUUID(id) {
		return nil, ErrNotificationNotFound
	}
	rf := newReadFilter(componentTable, read, 2)
	lock := ""
	if forUpdate {
		lock = " for update of n"
	}
	args := append([]any{id}, rf.Args...)
	x, err := scanNotification(q.QueryRow(ctx, rf.With+`select `+notificationCols+` from notification n
		where n.id = $1::uuid and `+rf.Pred("n.component_id")+lock, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotificationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get notification %s: %w", id, err)
	}
	return x, nil
}

// RetryNotification re-queues a failed notification for a fresh run of
// attempts, audited: one outside the read scope is not found, one inside it
// but outside the action scope (notification:retry, on the component tier)
// forbidden, one that has not failed not retryable.
func (p *PG) RetryNotification(ctx context.Context, actorID, id string, read, action scope.Set) (*Notification, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin retry notification: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := getNotification(ctx, tx, id, read, true)
	if err != nil {
		return nil, err
	}
	if ok, err := inScopeTree(ctx, tx, componentTable, before.ComponentID, action); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotificationForbidden
	}
	// Only a send that was attempted is retried: a row failed before any
	// attempt (its rule's when did not run, its type did not render) holds no
	// message, and sending it would page for an alarm nobody judged.
	if before.Status != NotificationFailed || before.Attempts == 0 {
		return nil, ErrNotificationNotRetryable
	}
	if _, err := tx.Exec(ctx, `update notification set status = 'queued', attempts = 0, next_attempt_at = now(), updated_at = now()
		where id = $1::uuid`, id); err != nil {
		return nil, fmt.Errorf("storage: retry notification %s: %w", id, err)
	}
	after, err := getNotification(ctx, tx, id, scope.Set{All: true}, false)
	if err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "retry", "notification", after.ID,
		map[string]any{"status": before.Status, "attempts": before.Attempts, "last_error": before.LastError},
		map[string]any{"status": after.Status}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit retry notification: %w", err)
	}
	return after, nil
}
