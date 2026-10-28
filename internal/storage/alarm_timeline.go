package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AlarmTimelineEntry is one line of an alarm's timeline (#950): an event
// that carries the alarm (a rule's fire or clear event), an audit row about
// it (its raise, acknowledgement or clear), or a notification about it
// (#618), as it stands.
type AlarmTimelineEntry struct {
	At time.Time
	// Kind is "event", "audit" or "notification".
	Kind string
	// What is the event's type, the audit row's action (raise, clear or
	// acknowledge), or a notification's status (queued, sent or failed).
	What string
	// Actor is the person who acted, by name; "" when the platform did (a
	// rule's evaluator), and then Rule names the rule it acted for.
	Actor    string
	Rule     string
	Instance string
	Message  string
	// A notification's own fields: its id (what :retry names), the
	// transition it is about, its target, its attempts, the last error.
	NotificationID string
	Transition     string
	Target         string
	Attempts       int
	Error          string
	// Channel is the notification's channel (#1341); Recipient the person it
	// is addressed to, withheld as the RecipientView says.
	Channel   string
	Personal  bool
	Recipient string
}

// auditAlarmAction words an alarm audit row's verb as the timeline reads
// it: an alarm's only update is its clear.
func auditAlarmAction(verb string) string {
	switch verb {
	case "create":
		return "raise"
	case "update":
		return "clear"
	}
	return verb
}

// AlarmTimeline reads one alarm's timeline on a component, by the
// component's uuid the caller already resolved in scope: its events and its
// audit rows merged by time, in two bounded statements (the alarm, then both
// sources in one ordered union over their indexes). At one instant an event
// comes before the audit row it caused, since a rule's event and the alarm
// write it moves share the transaction's clock. The alarm id may be any
// form Postgres reads as a uuid (uppercase, braced); it is matched in its
// canonical form, the form the audit rows key on. An alarm not on that
// component, or an id that names no alarm, is ErrAlarmNotFound.
//
// The platform's audit lines name the rule as the alarm's first event
// recorded it (source_rule, which outlives the rule); before any event (a
// hand-raised alarm has none), the rule's current name. Each event line keeps
// the name it was written under, so a rule renamed mid-episode (#977) shows
// its old name on the fire event and its new one on the clear.
func (p *PG) AlarmTimeline(ctx context.Context, componentID, alarmID string, view RecipientView) ([]AlarmTimelineEntry, error) {
	parsed, err := uuid.Parse(alarmID)
	if err != nil {
		return nil, ErrAlarmNotFound
	}
	alarmID = parsed.String()
	if _, err := uuid.Parse(componentID); err != nil {
		return nil, ErrAlarmNotFound
	}
	var rule string
	err = p.pool.QueryRow(ctx, `select coalesce(
			(select e.source_rule from event e where e.alarm_id = a.id and e.source_rule is not null order by e.id limit 1),
			(select r.name from event_rule r where r.id = a.raised_by_rule_id), '')
		from alarm a where a.id = $1::uuid and a.component_id = $2::uuid`, alarmID, componentID).Scan(&rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAlarmNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: alarm %s timeline: %w", alarmID, err)
	}
	// A notification sits at the moment it was queued (the transition's own
	// transaction, so the same instant as the edge that caused it, after it),
	// reading its status as it stands.
	rows, err := p.pool.Query(ctx, `select at, kind, what, actor, rule, instance, message, nid, transition, target, attempts, error,
			channel, personal, recipient, rid from (
			select e.ts as at, 'event' as kind, t.name as what, '' as actor, coalesce(e.source_rule, '') as rule,
				e.instance, e.message, 0 as rank, e.id as tiebreak, '' as nid, '' as transition, '' as target, 0 as attempts, '' as error,
				'' as channel, false as personal, '' as recipient, '' as rid
			from event e join event_type t on t.id = e.event_type_id
			where e.alarm_id = $1::uuid
			union all
			select l.ts, 'audit', l.verb, coalesce(l.actor_username, ''), '', '', coalesce(l.new->>'Message', ''), 1, l.seq,
				'', '', '', 0, '', '', false, '', ''
			from audit_log l
			where l.resource = 'alarm' and l.resource_id = $2
			union all
			select n.created_at, 'notification', n.status, '', n.rule_name, '', n.subject, 2, 0,
				n.id::text, n.transition, n.target_name, n.attempts, coalesce(n.last_error, ''),
				n.channel, n.recipient_name is not null, coalesce(n.recipient_name, ''), coalesce(n.recipient_principal_id::text, '')
			from notification n
			where n.alarm_id = $1::uuid
		) x order by at, rank, tiebreak, nid`, alarmID, alarmID)
	if err != nil {
		return nil, fmt.Errorf("storage: alarm %s timeline: %w", alarmID, err)
	}
	defer rows.Close()
	out := []AlarmTimelineEntry{}
	for rows.Next() {
		var e AlarmTimelineEntry
		var rid string
		if err := rows.Scan(&e.At, &e.Kind, &e.What, &e.Actor, &e.Rule, &e.Instance, &e.Message,
			&e.NotificationID, &e.Transition, &e.Target, &e.Attempts, &e.Error, &e.Channel, &e.Personal, &e.Recipient, &rid); err != nil {
			return nil, fmt.Errorf("storage: scan alarm %s timeline: %w", alarmID, err)
		}
		if e.Personal {
			n := Notification{Personal: true, Recipient: e.Recipient, RecipientID: rid, Target: e.Target, Rule: e.Rule}
			view.withhold(&n)
			e.Recipient, e.Rule = n.Recipient, n.Rule
		}
		if e.Kind == "audit" {
			e.What = auditAlarmAction(e.What)
			// The platform acting on a rule alarm acted for its rule.
			if e.Actor == "" {
				e.Rule = rule
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
