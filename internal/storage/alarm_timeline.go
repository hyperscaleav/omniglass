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
// that carries the alarm (a rule's fire or clear event), or an audit row
// about it (its raise, acknowledgement or clear).
type AlarmTimelineEntry struct {
	At time.Time
	// Kind is "event" or "audit".
	Kind string
	// What is the event's type, or the audit row's action: raise, clear or
	// acknowledge.
	What string
	// Actor is the person who acted, by name; "" when the platform did (a
	// rule's evaluator), and then Rule names the rule it acted for.
	Actor    string
	Rule     string
	Instance string
	Message  string
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

// AlarmTimeline reads one alarm's timeline on a component (by name or id):
// its events and its audit rows merged by time, in two bounded statements
// (the alarm, then both sources in one ordered union over their indexes). At
// one instant an event comes before the audit row it caused, since a rule's
// event and the alarm write it moves share the transaction's clock. An
// alarm not on that component, or an id that names no alarm, is
// ErrAlarmNotFound. The caller has already resolved the component in scope.
func (p *PG) AlarmTimeline(ctx context.Context, componentName, alarmID string) ([]AlarmTimelineEntry, error) {
	if _, err := uuid.Parse(alarmID); err != nil {
		return nil, ErrAlarmNotFound
	}
	component, err := scopedByName(ctx, p.pool, componentConfig, componentName)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, ErrAlarmNotFound
	} else if err != nil {
		return nil, err
	}
	var rule string
	err = p.pool.QueryRow(ctx, `select coalesce((select r.name from event_rule r where r.id = a.raised_by_rule_id), '')
		from alarm a where a.id = $1::uuid and a.component_id = $2::uuid`, alarmID, component.ID).Scan(&rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAlarmNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: alarm %s timeline: %w", alarmID, err)
	}
	rows, err := p.pool.Query(ctx, `select at, kind, what, actor, rule, instance, message from (
			select e.ts as at, 'event' as kind, t.name as what, '' as actor, coalesce(e.source_rule, '') as rule,
				e.instance, e.message, 0 as rank, e.id as tiebreak
			from event e join event_type t on t.id = e.event_type_id
			where e.alarm_id = $1::uuid
			union all
			select l.ts, 'audit', l.verb, coalesce(l.actor_username, ''), '', '', coalesce(l.new->>'Message', ''), 1, l.seq
			from audit_log l
			where l.resource = 'alarm' and l.resource_id = $2
		) x order by at, rank, tiebreak`, alarmID, alarmID)
	if err != nil {
		return nil, fmt.Errorf("storage: alarm %s timeline: %w", alarmID, err)
	}
	defer rows.Close()
	out := []AlarmTimelineEntry{}
	for rows.Next() {
		var e AlarmTimelineEntry
		if err := rows.Scan(&e.At, &e.Kind, &e.What, &e.Actor, &e.Rule, &e.Instance, &e.Message); err != nil {
			return nil, fmt.Errorf("storage: scan alarm %s timeline: %w", alarmID, err)
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
