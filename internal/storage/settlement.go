package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// SettlementVerdict is the computed state of a command's effect: never stored,
// always derived from the intended value it opened, the observed value the device
// reports, and the command_type's settle window (the driver's fact about how long
// the device takes to actuate). This is what turns the series' want/told/is
// pivot into a command-settlement judgment. The verdict stays computed; what a
// settle-check SEES it records onto the command row as a terminal status (#590),
// the same way health records its transitions at the write that evaluates them.
type SettlementVerdict string

const (
	// SettlementNone: nothing was commanded (no intended value), so there is
	// nothing to settle.
	SettlementNone SettlementVerdict = "none"
	// SettlementPending: still within the settle window since the command was
	// issued, so a difference from observed is not yet drift (the device is given
	// time to actuate).
	SettlementPending SettlementVerdict = "pending"
	// SettlementSettled: past the window, the observed value matches the intended
	// one, so the command took effect.
	SettlementSettled SettlementVerdict = "settled"
	// SettlementFailed: past the window, the observed value does not match the
	// intended one (or is absent), so the command did not take effect.
	SettlementFailed SettlementVerdict = "failed"
)

// Settle computes a command's settlement verdict. Pure: the whole judgment is a
// function of the intended value and its time, the observed value, the window, and
// now. Within the window the verdict is pending regardless of the observed value;
// past it, a match is settled and anything else is failed. A nil intended value is
// none (nothing was told).
func Settle(intended, observed *CurrentValue, windowSeconds int, now time.Time) SettlementVerdict {
	if intended == nil {
		return SettlementNone
	}
	if now.Sub(intended.TS) < time.Duration(windowSeconds)*time.Second {
		return SettlementPending
	}
	if observed != nil && bytes.Equal(observed.Value, intended.Value) {
		return SettlementSettled
	}
	return SettlementFailed
}

// terminalStatus maps a terminal settlement verdict onto the recorded command
// status. The computed verdict keeps its shipped meaning (past the window,
// anything but a match is failed); the record splits that collapsed failure into
// what the series can distinguish: a device that reported a different value
// failed, a window that expired with nothing observed timed out.
func terminalStatus(verdict SettlementVerdict, observed *CurrentValue) string {
	if verdict == SettlementSettled {
		return "settled"
	}
	if observed == nil {
		return "timed-out"
	}
	return "failed"
}

// settleCheck is the explicit settle-check write path (#590). Settlement has no
// stored state to piggyback on a data write, so the check runs wherever the
// verdict is evaluated (at issue, and on every CommandSettlement read): it sweeps
// the still-issued commands of one series, judges each against its own intended
// row (the command_id lineage) and the series' latest observed value, and stamps
// the terminal status with settled_at in the caller's transaction. A pending
// command stays issued; a command that opened no intended row (a pre-#590
// fire-and-forget) has nothing to wait on and settles.
// ownerID is resolved once here, via ownerArcValue (uuid-or-name, ADR-0062;
// ambiguity-safe for component/system/location, #627), and the resolved arc
// threaded into latestTargetValue below rather than left for it to re-derive.
// An unknown owner folds to "nothing to settle" (nil), matching the old inline
// subquery's silent no-match: settleCheck runs inside IssueCommand and
// CommandSettlement, neither of which must start hard-erroring on an owner
// check this function never used to perform.
func (p *PG) settleCheck(ctx context.Context, q txQuerier, ct *CommandType, ownerKind, ownerID, instance string, now time.Time) error {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return err
	}
	arc, err := p.ownerArcValue(ctx, q, ownerKind, ownerID)
	if isOwnerNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	sql := fmt.Sprintf(`select id from command
		where status = 'issued' and owner_kind = $1 and %s = $2
		  and command_type_id = $3 and instance = $4
		order by id`, col)
	rows, err := q.Query(ctx, sql, ownerKind, arc, ct.ID, instance)
	if err != nil {
		return fmt.Errorf("storage: settle check %s/%s: %w", ownerID, ct.Name, err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("storage: settle check scan: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: settle check %s/%s: %w", ownerID, ct.Name, err)
	}
	if len(ids) == 0 {
		return nil
	}
	observed, err := p.latestTargetValue(ctx, q, ct, ownerKind, arc, instance, "observed")
	if err != nil {
		return err
	}
	for _, id := range ids {
		intended, err := p.commandIntendedValue(ctx, q, ct, id)
		if err != nil {
			return err
		}
		var status string
		if intended == nil {
			status = "settled"
		} else if verdict := Settle(intended, observed, ct.SettleWindowSeconds, now); verdict == SettlementPending {
			continue
		} else {
			status = terminalStatus(verdict, observed)
		}
		if _, err := q.Exec(ctx, `update command set status = $2, settled_at = $3 where id = $1`, id, status, now); err != nil {
			return fmt.Errorf("storage: record command %d settlement: %w", id, err)
		}
	}
	return nil
}

// commandIntendedValue reads the intended row one command opened, by its
// command_id lineage on the target lane, or nil when the command opened none.
// The metric lane's double renders as canonical float text so Settle's byte
// equality is float equality.
func (p *PG) commandIntendedValue(ctx context.Context, q querier, ct *CommandType, commandID int64) (*CurrentValue, error) {
	cv := CurrentValue{Provenance: "intended"}
	switch {
	case ct.TargetMetricType != "":
		var v float64
		err := q.QueryRow(ctx, `select value, ts from metric where command_id = $1 order by ts desc, id desc limit 1`,
			commandID).Scan(&v, &cv.TS)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("storage: intended metric for command %d: %w", commandID, err)
		}
		cv.Value = json.RawMessage(strconv.FormatFloat(v, 'g', -1, 64))
	case ct.TargetPropertyType != "":
		var value []byte
		err := q.QueryRow(ctx, `select `+declaredCurrentExpr+`, ts from property where command_id = $1 order by ts desc, id desc limit 1`,
			commandID).Scan(&value, &cv.TS)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("storage: intended value for command %d: %w", commandID, err)
		}
		cv.Value = copyRaw(value)
	default:
		return nil, nil
	}
	return &cv, nil
}

// latestTargetValue reads the latest series row of a command_type's target for
// one provenance, on whichever lane the target arc names (nil for no target).
// ownerID here is always an already-resolved arc (a uuid), never the
// caller's original possibly-ambiguous reference: settleCheck's own callers
// (IssueCommand, CommandSettlement) both resolve the owner once, within
// their own scope, before passing the arc down through settleCheck to here.
// A uuid resolve is never ambiguous (it is a primary key lookup), so an all
// scope is passed through rather than threading one more scope parameter
// down a chain that never needs to narrow twice.
func (p *PG) latestTargetValue(ctx context.Context, q querier, ct *CommandType, ownerKind, ownerID, instance, provenance string) (*CurrentValue, error) {
	switch {
	case ct.TargetMetricType != "":
		return p.latestMetricValue(ctx, q, ownerKind, ownerID, ct.TargetMetricType, instance, provenance)
	case ct.TargetPropertyType != "":
		return p.latestValue(ctx, q, ownerKind, ownerID, ct.TargetPropertyType, instance, provenance, scope.Set{All: true})
	default:
		return nil, nil
	}
}

// latestMetricValue is latestValue's numeric-lane twin: the latest metric row by
// (type, owner, instance, provenance), the double rendered as canonical float
// text so the settlement comparison is float equality. ownerID is resolved
// once, folding an unknown owner to nil (see latestValue in current_values.go).
func (p *PG) latestMetricValue(ctx context.Context, q querier, ownerKind, ownerID, key, instance, provenance string) (*CurrentValue, error) {
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
	sql := fmt.Sprintf(`select value, ts, provenance from metric
		where owner_kind = $1 and %s = $2
		  and metric_type_id = (select id from metric_type where name = $3)
		  and instance = $4 and provenance = $5
		order by ts desc, id desc
		limit 1`, col)
	var (
		cv CurrentValue
		v  float64
	)
	err = q.QueryRow(ctx, sql, ownerKind, arc, key, instance, provenance).Scan(&v, &cv.TS, &cv.Provenance)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: latest metric value %s/%s[%s]: %w", ownerID, key, instance, err)
	}
	cv.Value = json.RawMessage(strconv.FormatFloat(v, 'g', -1, 64))
	return &cv, nil
}
