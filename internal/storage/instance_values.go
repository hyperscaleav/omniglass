package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/transport"
)

// An owner-level question over an instanced key (ADR-0456). `instance` names
// which one of several values of one key on one owner a row is: for an
// observed row, the observer (the endpoint whose task collected it, or the
// instance a push or a driver names). A reader that asks about the owner as a
// whole (the empty instance) reduces across every instance of the key on the
// owner that still has an observer; a reader that names an instance reads
// exactly that series and never reduces. These are the reads and the two
// reductions the readers share: reconciliation's worst case and settlement's
// any-instance-agrees.

// latestObservedPerInstanceSQL reads the newest observed row of each live
// instance of one key on an owner arc ($1 owner kind, $2 arc, $3 key, $4 the
// transport names), newest first. The instances are walked one index probe
// at a time and each one's newest row is a probe of its own (the series index
// leads with owner, type and instance, the walk readCalcInput makes), never a
// read of the key's whole history, which on the metric lane is the firehose.
// Observed only: the series index carries no provenance, so the same walk for
// a provenance the walked instances rarely hold (intended) would filter their
// every row (latestIntendedSQL reads told instead).
//
// Which instances are live is liveInstanceSQL's to say.
// lane is property or metric; value is the column expression read as value.
func latestObservedPerInstanceSQL(lane, value, col string) string {
	return fmt.Sprintf(`with recursive inst(instance) as (
			(select instance from %[1]s
				where owner_kind = $1 and %[3]s = $2 and %[1]s_type_id = (select id from %[1]s_type where name = $3)
				order by instance limit 1)
			union all
			select (select x.instance from %[1]s x
					where x.owner_kind = $1 and x.%[3]s = $2 and x.%[1]s_type_id = (select id from %[1]s_type where name = $3)
					  and x.instance > inst.instance
					order by x.instance limit 1)
				from inst where inst.instance is not null)
		select n.value, n.ts, n.provenance, i.instance from inst i,
			lateral (select %[2]s as value, ts, provenance, id from %[1]s
				where owner_kind = $1 and %[3]s = $2 and %[1]s_type_id = (select id from %[1]s_type where name = $3)
				  and instance = i.instance and provenance = 'observed'
				order by ts desc, id desc limit 1) n
		where i.instance is not null and n.value is not null
		  and %[4]s
		order by n.ts desc, n.id desc`, lane, value, col, liveInstanceSQL("i.instance", "$1", "$2", "$4"))
}

// liveInstanceSQL is the one test of whether an observed instance is live
// (ADR-0456), the predicate every owner-level read that reduces across
// instances applies, so no reader can draw the line elsewhere. A live instance
// is the owner-level one, one naming an endpoint the owner has now, or one
// that names no transport (an instance a push sender named, its own to keep or
// move). One naming a transport the owner has no endpoint of is a retired
// observer: an endpoint's name is its transport (ADR-1133), so it names an
// endpoint since deleted (a changed transport is a delete and a new endpoint),
// whose last reading is frozen, since nothing prunes a series' latest row. Age
// cannot tell it apart, because a property is transition-only and a live,
// unchanged value carries an old ts. Only a component has endpoints, so on any
// other owner an instance naming a transport is never live. Each argument is a
// SQL expression: the instance, the owner kind, the owner's arc (its uuid),
// and the transport names as a text array (transportNames).
func liveInstanceSQL(instance, ownerKind, arc, transports string) string {
	return fmt.Sprintf(`(%[1]s = '' or not (%[1]s = any(%[4]s::text[]))
		       or exists (select 1 from endpoint e where %[2]s = 'component' and e.component = %[3]s and e.name = %[1]s))`,
		instance, ownerKind, arc, transports)
}

// transportNames is every registered transport's name, the vocabulary an
// endpoint's name is drawn from (latestObservedPerInstanceSQL's $4).
func transportNames() []string {
	all := transport.All()
	names := make([]string, len(all))
	for i, t := range all {
		names[i] = t.Name
	}
	return names
}

// latestIntendedSQL reads told's candidates on an owner arc ($1 owner kind,
// $2 arc, $3 property key): the newest intended row of each instance a
// command targeting the key on the owner named, newest first. An intended row
// is a command's (ADR-0456: its stamper is the command, on the command's
// instance), so those instances are every series one can sit on, and each is
// one probe, never a walk of the observed instances.
func latestIntendedSQL(col string) string {
	return fmt.Sprintf(`select n.value, n.ts, n.provenance, i.instance from
			(select distinct c.instance from command c
				where c.owner_kind = $1 and c.%[2]s = $2
				  and c.command_type_id in (select ct.id from command_type ct
					where ct.target_property_type_id = (select id from property_type where name = $3))) i,
			lateral (select %[1]s as value, ts, provenance, id from property
				where owner_kind = $1 and %[2]s = $2 and property_type_id = (select id from property_type where name = $3)
				  and instance = i.instance and provenance = 'intended'
				order by ts desc, id desc limit 1) n
		where n.value is not null
		order by n.ts desc, n.id desc`, declaredCurrentExpr, col)
}

// latestObservedValues reads the current observed value of every live
// instance of one key on an owner: each instance's latest row (ts desc, id
// desc, the rule latestValue applies to one series), returned newest first, so
// the first element is the newest value across instances. An instance whose
// latest row is the unset tombstone has no current value and is left out, as
// is a retired observer (latestObservedPerInstanceSQL). Scope-guarded on the
// owner like latestValue: an unknown or out-of-scope owner folds to no values.
func (p *PG) latestObservedValues(ctx context.Context, q querier, ownerKind, ownerID, key string, s scope.Set) ([]CurrentValue, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	arc, err := p.ownerArcValueInScope(ctx, q, ownerKind, ownerID, s)
	if isOwnerNotFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return readPropertyValues(ctx, q, ownerID, key, latestObservedPerInstanceSQL("property", declaredCurrentExpr, col), ownerKind, arc, key, transportNames())
}

// readPropertyValues runs a per-instance property read whose rows are
// (value, ts, provenance, instance), newest first.
func readPropertyValues(ctx context.Context, q querier, ownerID, key, sql string, args ...any) ([]CurrentValue, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: latest values %s/%s: %w", ownerID, key, err)
	}
	defer rows.Close()
	var out []CurrentValue
	for rows.Next() {
		var (
			cv    CurrentValue
			value []byte
		)
		if err := rows.Scan(&value, &cv.TS, &cv.Provenance, &cv.Instance); err != nil {
			return nil, fmt.Errorf("storage: latest values %s/%s: %w", ownerID, key, err)
		}
		cv.Value = copyRaw(value)
		out = append(out, cv)
	}
	return out, rows.Err()
}

// latestObservedMetricValues is latestObservedValues' numeric-lane twin, the
// double rendered as canonical float text so a comparison is float equality.
// ownerID is an already-resolved arc on every path that reaches it
// (settlement), so it resolves scope-blind like latestMetricValue.
func (p *PG) latestObservedMetricValues(ctx context.Context, q querier, ownerKind, ownerID, key string) ([]CurrentValue, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	arc, err := p.ownerArcValue(ctx, q, ownerKind, ownerID)
	if isOwnerNotFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, latestObservedPerInstanceSQL("metric", "value", col), ownerKind, arc, key, transportNames())
	if err != nil {
		return nil, fmt.Errorf("storage: latest metric values %s/%s: %w", ownerID, key, err)
	}
	defer rows.Close()
	var out []CurrentValue
	for rows.Next() {
		var (
			cv CurrentValue
			v  float64
		)
		if err := rows.Scan(&v, &cv.TS, &cv.Provenance, &cv.Instance); err != nil {
			return nil, fmt.Errorf("storage: latest metric values %s/%s: %w", ownerID, key, err)
		}
		cv.Value = json.RawMessage(strconv.FormatFloat(v, 'g', -1, 64))
		out = append(out, cv)
	}
	return out, rows.Err()
}

// latestObservedTargetValues reads every live instance's current observed
// value of a command type's target, on whichever lane the target names (none
// for no target). ownerID is a resolved arc (see latestTargetValue).
func (p *PG) latestObservedTargetValues(ctx context.Context, q querier, ct *CommandType, ownerKind, ownerID string) ([]CurrentValue, error) {
	switch {
	case ct.TargetMetricType != "":
		return p.latestObservedMetricValues(ctx, q, ownerKind, ownerID, ct.TargetMetricType)
	case ct.TargetPropertyType != "":
		return p.latestObservedValues(ctx, q, ownerKind, ownerID, ct.TargetPropertyType, scope.Set{All: true})
	default:
		return nil, nil
	}
}

// worstCaseValue is reconciliation's reduction of the observed instances of a
// key against want (ADR-0456): drift when any instance disagrees with want,
// and the value shown is then the newest disagreeing one, else the newest
// agreeing one. With no want there is nothing to disagree with, so it is the
// newest value and no drift. values is newest first, as latestObservedValues returns
// it. Pure.
func worstCaseValue(want json.RawMessage, values []CurrentValue) (*CurrentValue, bool) {
	if len(values) == 0 {
		return nil, false
	}
	if want != nil {
		for i := range values {
			if !bytes.Equal(values[i].Value, want) {
				return &values[i], true
			}
		}
	}
	return &values[0], false
}

// agreeingValue is settlement's reduction of the observed instances of a
// command's target (ADR-0456): the newest instance that agrees with the
// intended value, else the newest observed on any instance, else nil (nothing
// observed). Judged by Settle, that is settled when any instance agrees,
// failed past the window when instances were observed and none agrees, and
// timed out when nothing was. values is newest first. Pure.
func agreeingValue(intended *CurrentValue, values []CurrentValue) *CurrentValue {
	if len(values) == 0 {
		return nil
	}
	if intended != nil {
		for i := range values {
			if bytes.Equal(values[i].Value, intended.Value) {
				return &values[i]
			}
		}
	}
	return &values[0]
}
