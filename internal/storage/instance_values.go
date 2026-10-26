package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// An owner-level question over an instanced key (ADR-0456). `instance` names
// which one of several values of one key on one owner a row is: for an
// observed row, the observer (the endpoint whose task collected it, or the
// instance a push or a driver names). A reader that asks about the owner as a
// whole (the empty instance) reduces across every instance of the key on the
// owner; a reader that names an instance reads exactly that series and never
// reduces. These are the reads and the two reductions the readers share:
// reconciliation's worst case and settlement's any-instance-agrees.

// latestPerInstanceSQL reads the newest row of each instance of one key on an
// owner arc for one provenance ($1 owner kind, $2 arc, $3 key, $4
// provenance), newest first. The instances are walked one index probe at a
// time and each one's newest row is a probe of its own (the series index
// leads with owner, type and instance, the walk readCalcInput makes), never a
// read of the key's whole history, which on the metric lane is the firehose.
// lane is property or metric; value is the column expression read as value.
func latestPerInstanceSQL(lane, value, col string) string {
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
				  and instance = i.instance and provenance = $4
				order by ts desc, id desc limit 1) n
		where i.instance is not null and n.value is not null
		order by n.ts desc, n.id desc`, lane, value, col)
}

// latestValues reads the current value of every instance of one key on an
// owner for one provenance: each instance's latest row (ts desc, id desc, the
// rule latestValue applies to one series), returned newest first, so the
// first element is the newest value across instances. An instance whose
// latest row is the unset tombstone has no current value and is left out.
// Scope-guarded on the owner like latestValue: an unknown or out-of-scope
// owner folds to no values.
func (p *PG) latestValues(ctx context.Context, q querier, ownerKind, ownerID, key, provenance string, s scope.Set) ([]CurrentValue, error) {
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
	rows, err := q.Query(ctx, latestPerInstanceSQL("property", declaredCurrentExpr, col), ownerKind, arc, key, provenance)
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

// latestMetricValues is latestValues' numeric-lane twin, the double rendered
// as canonical float text so a comparison is float equality. ownerID is an
// already-resolved arc on every path that reaches it (settlement), so it
// resolves scope-blind like latestMetricValue.
func (p *PG) latestMetricValues(ctx context.Context, q querier, ownerKind, ownerID, key, provenance string) ([]CurrentValue, error) {
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
	rows, err := q.Query(ctx, latestPerInstanceSQL("metric", "value", col), ownerKind, arc, key, provenance)
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

// latestTargetValues reads every instance's current value of a command
// type's target for one provenance, on whichever lane the target names (none
// for no target). ownerID is a resolved arc (see latestTargetValue).
func (p *PG) latestTargetValues(ctx context.Context, q querier, ct *CommandType, ownerKind, ownerID, provenance string) ([]CurrentValue, error) {
	switch {
	case ct.TargetMetricType != "":
		return p.latestMetricValues(ctx, q, ownerKind, ownerID, ct.TargetMetricType, provenance)
	case ct.TargetPropertyType != "":
		return p.latestValues(ctx, q, ownerKind, ownerID, ct.TargetPropertyType, provenance, scope.Set{All: true})
	default:
		return nil, nil
	}
}

// worstCaseValue is reconciliation's reduction of the observed instances of a
// key against want (ADR-0456): drift when any instance disagrees with want,
// and the value shown is then the newest disagreeing one, else the newest
// agreeing one. With no want there is nothing to disagree with, so it is the
// newest value and no drift. values is newest first, as latestValues returns
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
