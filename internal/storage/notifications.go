package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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
	// NotificationSuppressed is a row written but never sent, because its
	// alarm's condition is snoozed (#1318): terminal, its reason in
	// last_error. The tick never drains it, the inbox never shows it, and
	// :retry refuses it.
	NotificationSuppressed = "suppressed"
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
	// Channel is how it travels: webhook, inbox or email (#1341).
	Channel string
	// Personal is a row addressed to one person. Recipient is that person by
	// name and RecipientID by uuid, both empty when the reader may not see
	// who it is (RecipientView) or the person was purged.
	Personal      bool
	Recipient     string
	RecipientID   string
	ReadAt        *time.Time
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
	n.transition, n.rule_name, n.type_name, n.target_name, n.channel, n.recipient_name is not null, coalesce(n.recipient_name, ''),
	coalesce(n.recipient_principal_id::text, ''), n.read_at, n.subject, n.text, n.status, n.attempts,
	case when n.status = 'queued' then n.next_attempt_at end, n.last_status, coalesce(n.last_error, ''), n.sent_at, n.created_at, n.updated_at`

// RecipientView is who may see a person-addressed row's recipient (#1341,
// decision 5): the recipient themselves (Self, their principal uuid) and a
// holder of the principal directory's admin-tier read (All). Everyone else
// sees the row, so an alarm's history stays complete, without the name.
type RecipientView struct {
	Self string
	All  bool
}

// withhold blanks a recipient the view may not see.
func (v RecipientView) withhold(n *Notification) {
	if v.All || (n.RecipientID != "" && n.RecipientID == v.Self) {
		return
	}
	n.Recipient, n.RecipientID = "", ""
	// A relay's refusal can quote the person's address (550 <who@...>:
	// rejected), so a withheld row's reason is withheld with the name.
	if n.Personal && n.LastError != "" {
		n.LastError = "(withheld with the recipient)"
	}
	// A personal rule's row (a person's, through no target) is named by its
	// owner's rule, which can say whose it is: withheld with the name.
	if n.Personal && n.Target == "" {
		n.Rule = ""
	}
}

func scanNotification(row pgx.Row) (*Notification, error) {
	var n Notification
	if err := row.Scan(&n.ID, &n.AlarmID, &n.ComponentID, &n.ComponentName, &n.Transition, &n.Rule, &n.Type, &n.Target,
		&n.Channel, &n.Personal, &n.Recipient, &n.RecipientID, &n.ReadAt,
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
	id, name, when                      string
	typeID, typeName                    string
	subject, text                       string
	targetIDs, targetNames, targetKinds []string
	// A personal rule's owner, its only recipient, and its channels (#1353).
	ownerID, ownerName string
	channels           []string
	// optOutAllowed lets a group member opt out (#1354); optedOut are those
	// who have, read only when the rule allows it.
	optOutAllowed bool
	optedOut      map[string]bool
}

// queuedRow is one row a firing rule writes: to a webhook target, or to one
// person on one channel through a group or person target.
type queuedRow struct {
	targetID, targetName, channel, status string
	recipientID, recipientName            *string
	// The firing rule's own: set when the transition's rows are gathered.
	rule                  *queuedRule
	subject, text, reason string
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
	// Judge every rule first: the people a firing rule names are expanded
	// and scope-filtered together, in a fixed handful of statements.
	type fired struct {
		r                             queuedRule
		status, subject, text, reason string
	}
	var fires []fired
	var personTargets []string
	// A personal rule is gated by its owner's component read scope BEFORE its
	// when runs (ADR-1341, decision 2): an alarm outside that scope is never
	// matched, so neither a delivery nor the when's result (a failure is a row
	// too) can tell the owner anything about it. Every owner is judged in one
	// set-based pass.
	var owners []string
	for _, r := range rs {
		if r.ownerID != "" && !slices.Contains(owners, r.ownerID) {
			owners = append(owners, r.ownerID)
		}
	}
	ownerReads := map[string]bool{}
	if len(owners) > 0 {
		if ownerReads, err = subscribersOf(ctx, tx, componentID, owners); err != nil {
			return err
		}
	}
	for _, r := range rs {
		if r.ownerID != "" && !ownerReads[r.ownerID] {
			continue
		}
		f := fired{r: r, status: NotificationQueued}
		when, err := rules.CompileWhen(r.when)
		holds := false
		if err == nil {
			holds, err = when.Holds(in)
		}
		switch {
		case err != nil:
			f.status, f.reason = NotificationFailed, "the rule's when did not run: "+err.Error()
		case !holds:
			continue
		default:
			f.subject, f.text, err = renderNotification(eng, r.subject, r.text, facts)
			if err != nil {
				f.status, f.reason = NotificationFailed, "the notification did not render: "+TemplateRefusal(err)
			}
		}
		fires = append(fires, f)
		for i, k := range r.targetKinds {
			if isPersonKind(k) && !slices.Contains(personTargets, r.targetIDs[i]) {
				personTargets = append(personTargets, r.targetIDs[i])
			}
		}
	}
	// A snoozed condition (#1318, ADR-1318): one lookup once some rule fires,
	// and every row a firing rule would send or deliver is written suppressed
	// with the reason instead, on every channel. A rule that failed stays
	// failed, so a broken rule is still visible during a snooze.
	if len(fires) > 0 {
		reason, err := conditionSnooze(ctx, tx, alarmID)
		if err != nil {
			return err
		}
		for i := range fires {
			if reason != "" && fires[i].status == NotificationQueued {
				fires[i].status, fires[i].reason = NotificationSuppressed, reason
			}
		}
	}
	firing := make([]*queuedRule, len(fires))
	for i := range fires {
		firing[i] = &fires[i].r
	}
	if err := loadOptOuts(ctx, tx, firing); err != nil {
		return err
	}
	people := map[string][]recipient{}
	if len(personTargets) > 0 {
		if people, err = recipientsFor(ctx, tx, componentID, personTargets); err != nil {
			return err
		}
	}
	var all []queuedRow
	for _, f := range fires {
		for _, row := range rowsFor(f.r, f.status, people) {
			r := f.r
			row.rule, row.subject, row.text, row.reason = &r, f.subject, f.text, f.reason
			all = append(all, row)
		}
	}
	if len(all) == 0 {
		return nil
	}
	return insertNotifications(ctx, tx, alarmID, componentID, transition, factsJSON, all)
}

// loadOptOuts reads, in one statement, who opted out of each firing rule
// that allows it (#1354); a rule that does not allow it reads none, so an
// admin who takes the permission back takes every opt-out with it.
func loadOptOuts(ctx context.Context, q querier, rs []*queuedRule) error {
	var ids []string
	at := map[string]*queuedRule{}
	for _, r := range rs {
		if r.optOutAllowed {
			ids = append(ids, r.id)
			at[r.id] = r
			r.optedOut = map[string]bool{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `select rule_id::text, principal_id::text from action_rule_opt_out where rule_id = any($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("storage: read opt-outs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rid, pid string
		if err := rows.Scan(&rid, &pid); err != nil {
			return fmt.Errorf("storage: scan opt-out: %w", err)
		}
		at[rid].optedOut[pid] = true
	}
	return rows.Err()
}

// rowsFor lays out the rows one firing rule writes: one per webhook target,
// and one per person per channel through its group and person targets, a
// person reached twice by one rule (a member of two of its groups, or named
// directly too) told once per channel. An inbox row is delivered by its own
// commit, so it is written sent; a rule that failed writes every row failed.
func rowsFor(r queuedRule, status string, people map[string][]recipient) []queuedRow {
	var out []queuedRow
	told := map[string]bool{}
	if r.ownerID != "" {
		// A personal rule names no target: its owner, on its channels.
		for _, ch := range r.channels {
			st := status
			if st == NotificationQueued && ch == DeliveryInbox {
				st = NotificationSent
			}
			pid, pname := r.ownerID, r.ownerName
			out = append(out, queuedRow{channel: ch, status: st, recipientID: &pid, recipientName: &pname})
		}
		return out
	}
	// A person a target names directly is attributed to that target before
	// any group reaches them, so their own opt-out of the group never stops
	// what names them by name.
	order := make([]int, 0, len(r.targetIDs))
	for i, k := range r.targetKinds {
		if k == TargetPrincipal {
			order = append(order, i)
		}
	}
	for i, k := range r.targetKinds {
		if k != TargetPrincipal {
			order = append(order, i)
		}
	}
	for _, i := range order {
		id := r.targetIDs[i]
		if !isPersonKind(r.targetKinds[i]) {
			out = append(out, queuedRow{targetID: id, targetName: r.targetNames[i], channel: deliveryFor(r.targetKinds[i]), status: status})
			continue
		}
		for _, p := range people[id] {
			if r.targetKinds[i] == TargetPrincipalGroup && r.optedOut[p.id] {
				continue
			}
			for _, ch := range p.channels {
				if told[p.id+"/"+ch] {
					continue
				}
				told[p.id+"/"+ch] = true
				st := status
				if st == NotificationQueued && ch == DeliveryInbox {
					st = NotificationSent
				}
				pid, pname := p.id, p.name
				out = append(out, queuedRow{targetID: id, targetName: r.targetNames[i], channel: ch, status: st,
					recipientID: &pid, recipientName: &pname})
			}
		}
	}
	return out
}

// insertNotifications writes every row one transition queues, across all its
// firing rules, in one statement whatever their number.
func insertNotifications(ctx context.Context, tx pgx.Tx, alarmID, componentID, transition string, factsJSON []byte, rows []queuedRow) error {
	n := len(rows)
	cols := make([][]*string, 13)
	for i := range cols {
		cols[i] = make([]*string, n)
	}
	str := func(v string) *string { return &v }
	for i, x := range rows {
		vals := []*string{str(x.rule.id), str(x.rule.name), str(x.rule.typeID), str(x.rule.typeName), str(x.subject), str(x.text),
			str(x.reason), str(x.targetID), str(x.targetName), str(x.status), str(x.channel), x.recipientID, x.recipientName}
		for c, v := range vals {
			cols[c][i] = v
		}
	}
	if _, err := tx.Exec(ctx, `insert into notification (alarm_id, component_id, transition, rule_id, rule_name,
			notification_type_id, type_name, target_id, target_name, subject, text, facts, status, last_error,
			channel, recipient_principal_id, recipient_name, sent_at)
		select $1::uuid, $2::uuid, $3, x.rule_id::uuid, x.rule_name, x.type_id::uuid, x.type_name, nullif(x.target_id, '')::uuid, x.target_name,
			x.subject, x.text, $4, x.status, nullif(x.reason, ''), x.channel, x.rid::uuid, x.rname, case when x.status = 'sent' then now() end
		from unnest($5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::text[], $12::text[], $13::text[],
			$14::text[], $15::text[], $16::text[], $17::text[])
			with ordinality as x(rule_id, rule_name, type_id, type_name, subject, text, reason, target_id, target_name, status, channel, rid, rname, ord)
		order by x.ord`,
		alarmID, componentID, transition, factsJSON,
		cols[0], cols[1], cols[2], cols[3], cols[4], cols[5], cols[6], cols[7], cols[8], cols[9], cols[10], cols[11], cols[12]); err != nil {
		return fmt.Errorf("storage: queue notifications: %w", err)
	}
	return nil
}

// rulesOn reads the enabled rules on a transition with their templates and
// enabled targets; none (the usual case) costs one indexed statement.
func rulesOn(ctx context.Context, q querier, transition string) ([]queuedRule, error) {
	rows, err := q.Query(ctx, `
		select r.id::text, r.name, r.when_expr, t.id::text, t.name, t.subject, t.text,
			coalesce(array_agg(nt.id::text order by nt.name) filter (where nt.id is not null), '{}'),
			coalesce(array_agg(nt.name order by nt.name) filter (where nt.id is not null), '{}'),
			coalesce(array_agg(nt.kind order by nt.name) filter (where nt.id is not null), '{}'),
			coalesce(r.owner_principal_id::text, ''), coalesce(oh.username, ''), r.channels, r.opt_out_allowed
		from action_rule r
		join notification_type t on t.id = coalesce(r.notification_type_id, (select id from notification_type where name = $2 and official))
		left join action_rule_target art on art.rule_id = r.id
		left join notification_target nt on nt.id = art.target_id and nt.enabled
		left join principal op on op.id = r.owner_principal_id
		left join human oh on oh.principal_id = op.id
		where r.enabled and $1 = any(r.on_transitions)
		  and (r.owner_principal_id is null or (op.active and op.archived_at is null and oh.principal_id is not null))
		group by r.id, r.name, r.when_expr, t.id, t.name, t.subject, t.text, oh.username, r.opt_out_allowed
		order by r.name`, transition, officialTypeFor(transition))
	if err != nil {
		return nil, fmt.Errorf("storage: action rules on %s: %w", transition, err)
	}
	defer rows.Close()
	var out []queuedRule
	for rows.Next() {
		var r queuedRule
		if err := rows.Scan(&r.id, &r.name, &r.when, &r.typeID, &r.typeName, &r.subject, &r.text, &r.targetIDs, &r.targetNames, &r.targetKinds,
			&r.ownerID, &r.ownerName, &r.channels, &r.optOutAllowed); err != nil {
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
	// Channel is webhook or email; Target is a webhook's send, Email an
	// email's (#1317).
	Channel string
	Target  TargetSend
	Email   *EmailSend
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
	var targetID, recipientID, ruleID *string
	var targetName string
	var componentID string
	var createdAt time.Time
	var attempts int
	var factsRaw []byte
	err = tx.QueryRow(ctx, `select id::text, alarm_id::text, type_name, transition, subject, text, facts, target_id::text, attempts,
			channel, recipient_principal_id::text, component_id::text, created_at, rule_id::text, target_name
		from notification where status = 'queued' and next_attempt_at <= now()
		order by next_attempt_at, id for update skip locked limit 1`).
		Scan(&ns.ID, &ns.AlarmID, &ns.Type, &ns.Transition, &ns.Subject, &ns.Text, &factsRaw, &targetID, &attempts,
			&ns.Channel, &recipientID, &componentID, &createdAt, &ruleID, &targetName)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: take due notification: %w", err)
	}
	_ = json.Unmarshal(factsRaw, &ns.Facts)
	// The re-check where a row waits (#1318, the pattern of ADR-1341 point
	// 3): a row of a condition snoozed since it was queued, a retry waiting
	// out its backoff included, is suppressed with the reason, not sent.
	suppressed, err := conditionSnooze(ctx, tx, ns.AlarmID)
	if err != nil {
		return true, err
	}
	if suppressed != "" {
		if _, err := tx.Exec(ctx, `update notification set status = 'suppressed', last_error = $2, updated_at = now() where id = $1`,
			ns.ID, suppressed); err != nil {
			return true, fmt.Errorf("storage: suppress notification %s: %w", ns.ID, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return true, fmt.Errorf("storage: commit notification %s: %w", ns.ID, err)
		}
		return true, nil
	}
	switch {
	case ns.Channel == DeliveryEmail:
		var es *EmailSend
		var refused string
		if targetID == nil && targetName == "" {
			es, refused, err = p.personalEmailSend(ctx, tx, ruleID, recipientID, componentID, createdAt)
		} else {
			es, refused, err = p.emailSend(ctx, tx, targetID, ruleID, recipientID, componentID, createdAt)
		}
		if err != nil {
			return true, err
		}
		ns.Email, ns.Refused = es, refused
	case ns.Channel != DeliveryWebhook:
		ns.Refused = "an inbox entry is delivered by its own commit, never sent"
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
func (p *PG) ListNotifications(ctx context.Context, f NotificationFilter, read scope.Set, view RecipientView) ([]Notification, error) {
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
		view.withhold(x)
		out = append(out, *x)
	}
	return out, rows.Err()
}

// GetNotification reads one notification by id; one about a component
// outside the read scope is the non-disclosing ErrNotificationNotFound.
func (p *PG) GetNotification(ctx context.Context, id string, read scope.Set, view RecipientView) (*Notification, error) {
	n, err := getNotification(ctx, p.pool, id, read, false)
	if err != nil {
		return nil, err
	}
	view.withhold(n)
	return n, nil
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
func (p *PG) RetryNotification(ctx context.Context, actorID, id string, read, action scope.Set, view RecipientView) (*Notification, error) {
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
	view.withhold(after)
	return after, nil
}

// --- the inbox: a person's own rows, scoped again at read (#1341) ----------

// InboxFilter narrows an inbox read: unread entries only, at most Limit
// (100 when zero, 500 at most), newest first.
type InboxFilter struct {
	Unread bool
	Limit  int
}

// Inbox is one person's inbox as it reads: the entries and the unread count,
// both over the same scope.
type Inbox struct {
	Notifications []Notification
	Unread        int
}

// inboxPred is the inbox's own predicate over the caller and a read filter
// whose roots bind from $2: the caller's delivered inbox rows about
// components their read scope admits now. A grant lost since the row was
// written makes the entry unreadable at once (the read-time recheck).
func inboxPred(rf readFilter) string {
	return `n.recipient_principal_id = $1::uuid and n.channel = 'inbox' and n.status = 'sent' and ` + rf.Pred("n.component_id")
}

// ListInbox reads a person's inbox: their own delivered inbox rows, about
// components their component read scope admits now, newest first, with the
// unread count over the same set.
func (p *PG) ListInbox(ctx context.Context, principalID string, f InboxFilter, read scope.Set) (*Inbox, error) {
	out := &Inbox{Notifications: []Notification{}}
	if !isUUID(principalID) {
		return out, nil
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rf := newReadFilter(componentTable, read, 2)
	args := append([]any{principalID}, rf.Args...)
	n := len(args)
	rows, err := p.pool.Query(ctx, rf.With+`select `+notificationCols+` from notification n
		where `+inboxPred(rf)+` and (not $`+strconv.Itoa(n+1)+`::boolean or n.read_at is null)
		order by n.created_at desc, n.id desc limit $`+strconv.Itoa(n+2), append(args, f.Unread, limit)...)
	if err != nil {
		return nil, fmt.Errorf("storage: list inbox: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		x, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan inbox entry: %w", err)
		}
		out.Notifications = append(out.Notifications, *x)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: list inbox: %w", err)
	}
	if err := p.pool.QueryRow(ctx, rf.With+`select count(*) from notification n where `+inboxPred(rf)+` and n.read_at is null`, args...).Scan(&out.Unread); err != nil {
		return nil, fmt.Errorf("storage: count unread inbox: %w", err)
	}
	return out, nil
}

// MarkInboxRead marks one of the caller's own inbox entries read, audited.
// An entry that is not theirs, not an inbox entry, or about a component they
// can no longer read is the non-disclosing ErrNotificationNotFound; marking
// one already read keeps its first read time.
func (p *PG) MarkInboxRead(ctx context.Context, actorID, principalID, id string, read scope.Set) (*Notification, error) {
	if !isUUID(id) || !isUUID(principalID) {
		return nil, ErrNotificationNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin mark read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rf := newReadFilter(componentTable, read, 2)
	args := append([]any{principalID}, rf.Args...)
	idArg := "$" + strconv.Itoa(len(args)+1)
	var before *time.Time
	err = tx.QueryRow(ctx, rf.With+`select n.read_at from notification n where n.id = `+idArg+`::uuid and `+inboxPred(rf)+` for update of n`,
		append(args, id)...).Scan(&before)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotificationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: mark read %s: %w", id, err)
	}
	if before == nil {
		if _, err := tx.Exec(ctx, `update notification set read_at = now(), updated_at = now() where id = $1::uuid`, id); err != nil {
			return nil, fmt.Errorf("storage: mark read %s: %w", id, err)
		}
	}
	after, err := getNotification(ctx, tx, id, scope.Set{All: true}, false)
	if err != nil {
		return nil, err
	}
	if before == nil {
		if err := writeAuditRes(ctx, tx, actorID, "read", "notification", after.ID, map[string]any{"read": false}, map[string]any{"read": true}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit mark read: %w", err)
	}
	return after, nil
}

// MarkInboxAllRead marks every unread entry the caller can read in their
// inbox read, audited as one row on the caller, and reports how many.
func (p *PG) MarkInboxAllRead(ctx context.Context, actorID, principalID string, read scope.Set) (int, error) {
	if !isUUID(principalID) {
		return 0, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: begin mark all read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rf := newReadFilter(componentTable, read, 2)
	args := append([]any{principalID}, rf.Args...)
	tag, err := tx.Exec(ctx, rf.With+`update notification n set read_at = now(), updated_at = now()
		where `+inboxPred(rf)+` and n.read_at is null`, args...)
	if err != nil {
		return 0, fmt.Errorf("storage: mark all read: %w", err)
	}
	marked := int(tag.RowsAffected())
	if marked > 0 {
		if err := writeAuditRes(ctx, tx, actorID, "read_all", "principal", principalID, nil, map[string]any{"inbox_entries_read": marked}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("storage: commit mark all read: %w", err)
	}
	return marked, nil
}
