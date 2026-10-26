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
// now. A match observed at or after the intent is settled at once (ADR-0141);
// otherwise, within the window the verdict is pending, and past it a match is
// settled and anything else is failed. A nil intended value is none (nothing was
// told).
//
// A window of zero is terminal BY CONSTRUCTION, never by arithmetic (#667). It is
// the documented way to say "settle immediately", which is a statement about
// intent rather than about time, and it is exactly the setting the arithmetic
// served worst: at zero the comparison reduces to `now.Sub(intended.TS) < 0`,
// true only when the sample is stamped AHEAD of now, so the verdict for the most
// decisive setting in the catalog was decided by whatever skew separated the two
// values. Callers now supply a `now` read from the database, the same clock the
// sample's ts comes from, which closes the skew for every other window; this
// branch is what makes the zero case independent of the clock rather than merely
// agreeing with it.
func Settle(intended, observed *CurrentValue, windowSeconds int, now time.Time) SettlementVerdict {
	if intended == nil {
		return SettlementNone
	}
	// The evidence settles it as soon as it arrives (ADR-0141): an observation
	// taken at or after the intent that matches it. The window bounds the wait,
	// not the settling.
	if observed != nil && !observed.TS.Before(intended.TS) && bytes.Equal(observed.Value, intended.Value) {
		return SettlementSettled
	}
	if windowSeconds > 0 && now.Sub(intended.TS) < time.Duration(windowSeconds)*time.Second {
		return SettlementPending
	}
	if observed != nil && bytes.Equal(observed.Value, intended.Value) {
		return SettlementSettled
	}
	return SettlementFailed
}

// dbNow reads the caller's transaction timestamp: the one clock a settlement
// comparison is allowed to use (#667).
//
// Settle compares `now` against a sample's ts, and every ts on the telemetry
// tables defaults to Postgres's now(), so a `now` taken with time.Now() in the
// server process put two clocks on the two ends of one comparison. On a real
// deployment the database is not on the server's host and the two do not agree;
// the operator-visible cost was a command that genuinely failed being reported
// pending, on every zero-window command, decided by which way the skew ran.
//
// now() is transaction_timestamp, which is the point: a settle-check running
// inside the transaction that just opened the intended value reads exactly the
// ts that row was stamped with, so the two ends of the comparison are one
// reading of one clock rather than two readings that happen to be close. It also
// keeps `settled_at` in the same currency as the `now()` the insert path already
// stamps a fire-and-forget command with.
//
// Settle itself stays pure and still takes `now` as an argument. What changed is
// who supplies it, which is the repo's own rule about the clock being an edge
// concern pushed out of the core.
func dbNow(ctx context.Context, q querier) (time.Time, error) {
	var now time.Time
	if err := q.QueryRow(ctx, `select now()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("storage: read database time: %w", err)
	}
	return now.UTC(), nil
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
	// Each command is judged on its series (commandSeriesSQL): its own
	// instance, else the endpoint it was dispatched over, else none, an
	// owner-level intent judged on every instance (ADR-0456).
	sql := fmt.Sprintf(`select c.id, `+commandSeriesSQL+`
		from command c
		where c.status = 'issued' and c.owner_kind = $1 and c.%s = $2
		  and c.command_type_id = $3 and c.instance = $4
		order by c.id`, col)
	pending, err := p.pendingCommands(ctx, q, sql, ownerKind, arc, ct.ID, instance)
	if err != nil {
		return fmt.Errorf("storage: settle check %s/%s: %w", ownerID, ct.Name, err)
	}
	return p.judgeCommands(ctx, q, ct, ownerKind, arc, pending, now)
}

// commandSeriesSQL is the series a command (aliased c) is judged on
// (ADR-0456): its own instance when it names one; else the endpoint it was
// dispatched over, where that driver's observations of the target land
// (#900); else the empty string, an owner-level intent no endpoint has
// carried, judged on every observed instance of its target (agreeingValue).
const commandSeriesSQL = `coalesce(nullif(c.instance, ''), (select e.name from endpoint e where e.id = c.dispatched_endpoint_id), '')`

// pendingCommand is an issued command and the series instance its target is
// observed on (empty for every instance, commandSeriesSQL).
type pendingCommand struct {
	id       int64
	instance string
}

func (p *PG) pendingCommands(ctx context.Context, q querier, sql string, args ...any) ([]pendingCommand, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pendingCommand
	for rows.Next() {
		var pc pendingCommand
		if err := rows.Scan(&pc.id, &pc.instance); err != nil {
			return nil, err
		}
		out = append(out, pc)
	}
	return out, rows.Err()
}

// judgeCommands settles each pending command against its own intended row
// and the observation of its target it is judged on (judgedObservation),
// stamping a terminal verdict with settled_at; a pending verdict leaves it
// issued.
func (p *PG) judgeCommands(ctx context.Context, q txQuerier, ct *CommandType, ownerKind, arc string, pending []pendingCommand, now time.Time) error {
	reads := &observationReads{on: map[string]*CurrentValue{}}
	for _, pc := range pending {
		intended, err := p.commandIntendedValue(ctx, q, ct, pc.id)
		if err != nil {
			return err
		}
		observed, err := p.judgedObservation(ctx, q, ct, ownerKind, arc, pc.instance, intended, reads)
		if err != nil {
			return err
		}
		var o settlementOutcome
		if intended == nil {
			o = settlementOutcome{status: "settled", by: SettledByIssue, reason: "nothing was told, so there was nothing to settle"}
		} else if verdict := Settle(intended, observed, ct.SettleWindowSeconds, now); verdict == SettlementPending {
			continue
		} else if o, err = p.observationOutcome(ctx, q, ct, pc.id, verdict, intended, observed); err != nil {
			return err
		}
		if err := recordOutcome(ctx, q, pc.id, o, now); err != nil {
			return err
		}
	}
	return nil
}

// observationReads holds what one settle-check has read of a target's
// observed series, so commands sharing a series read it once: the latest per
// named series, and every instance's latest for the owner-level ones.
type observationReads struct {
	on        map[string]*CurrentValue
	every     []CurrentValue
	readEvery bool
}

// judgedObservation is the observation a command is judged against
// (ADR-0456). A command with a series (commandSeriesSQL: its own instance,
// or the endpoint it was dispatched over) reads that series alone and never
// another. One with none is an owner-level intent: it reads every observed
// instance of its target and is judged on the one that agrees, else the
// newest (agreeingValue).
func (p *PG) judgedObservation(ctx context.Context, q querier, ct *CommandType, ownerKind, arc, series string, intended *CurrentValue, reads *observationReads) (*CurrentValue, error) {
	if series != "" {
		if observed, seen := reads.on[series]; seen {
			return observed, nil
		}
		observed, err := p.latestTargetValue(ctx, q, ct, ownerKind, arc, series, "observed")
		if err != nil {
			return nil, err
		}
		reads.on[series] = observed
		return observed, nil
	}
	if !reads.readEvery {
		every, err := p.latestTargetValues(ctx, q, ct, ownerKind, arc, "observed")
		if err != nil {
			return nil, err
		}
		reads.every, reads.readEvery = every, true
	}
	return agreeingValue(intended, reads.every), nil
}

// intendedSeries is the series the command behind an owner's latest intended
// row of a target on instance is judged on (commandSeriesSQL), so the
// computed verdict reads the observation the recorded one did: empty for no
// row, a row no command opened, or an owner-level intent no endpoint carried.
func (p *PG) intendedSeries(ctx context.Context, q querier, ct *CommandType, ownerKind, arc, instance string) (string, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return "", err
	}
	lane, catalog, key := "property", "property_type", ct.TargetPropertyType
	if ct.TargetMetricType != "" {
		lane, catalog, key = "metric", "metric_type", ct.TargetMetricType
	}
	sql := fmt.Sprintf(`select coalesce((select `+commandSeriesSQL+` from command c where c.id = s.command_id), '')
		from %[1]s s
		where s.owner_kind = $1 and s.%[3]s = $2
		  and s.%[1]s_type_id = (select id from %[2]s where name = $3)
		  and s.instance = $4 and s.provenance = 'intended'
		order by s.ts desc, s.id desc
		limit 1`, lane, catalog, col)
	var series string
	err = q.QueryRow(ctx, sql, ownerKind, arc, key, instance).Scan(&series)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: series of the latest intended %s: %w", key, err)
	}
	return series, nil
}

// settleOnObservation runs when an observation of a property lands: every
// issued command whose type targets that property, on that owner, and whose
// series is the observation's instance or none at all (an owner-level intent
// judged on every instance, ADR-0456), is judged. The second way a command
// settles (ADR-0141).
func (p *PG) settleOnObservation(ctx context.Context, q txQuerier, ownerKind, arc, propertyKey, instance string, now time.Time) error {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return err
	}
	rows, err := q.Query(ctx, `select distinct ct.name from command_type ct
		join property_type pt on pt.id = ct.target_property_type_id
		where pt.name = $1`, propertyKey)
	if err != nil {
		return fmt.Errorf("storage: command types targeting %s: %w", propertyKey, err)
	}
	var types []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		types = append(types, n)
	}
	rows.Close()
	for _, name := range types {
		ct, err := scanCommandType(q.QueryRow(ctx, `select `+commandTypeCols+` from command_type where name = $1`, name))
		if err != nil {
			return fmt.Errorf("storage: command type %s: %w", name, err)
		}
		// The series settleCheck judges a command on: the observation's
		// instance reaches a command on that series and every owner-level
		// one, and never a command on another series.
		sql := fmt.Sprintf(`select c.id, `+commandSeriesSQL+` from command c
			where c.status = 'issued' and c.owner_kind = $1 and c.%s = $2 and c.command_type_id = $3
			  and `+commandSeriesSQL+` in ($4::text, '')
			order by c.id`, col)
		pending, err := p.pendingCommands(ctx, q, sql, ownerKind, arc, ct.ID, instance)
		if err != nil {
			return fmt.Errorf("storage: commands awaiting %s: %w", propertyKey, err)
		}
		if err := p.judgeCommands(ctx, q, ct, ownerKind, arc, pending, now); err != nil {
			return err
		}
	}
	return nil
}

// SweepCommandSettlements judges every issued command whose settle window
// has passed, so a command nothing else settles reaches timed-out (or
// failed) without waiting for another command or read. The server runs it
// on a short interval.
func (p *PG) SweepCommandSettlements(ctx context.Context) error {
	rows, err := p.pool.Query(ctx, `select distinct c.owner_kind,
			coalesce(c.component_id, c.system_id, c.location_id, c.node_id)::text, ct.name, c.instance
		from command c join command_type ct on ct.id = c.command_type_id
		where c.status = 'issued' and c.ts < now() - make_interval(secs => ct.settle_window_seconds)`)
	if err != nil {
		return fmt.Errorf("storage: sweep settlements: %w", err)
	}
	type series struct{ ownerKind, arc, commandType, instance string }
	var due []series
	for rows.Next() {
		var s series
		if err := rows.Scan(&s.ownerKind, &s.arc, &s.commandType, &s.instance); err != nil {
			rows.Close()
			return fmt.Errorf("storage: sweep scan: %w", err)
		}
		due = append(due, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: sweep settlements: %w", err)
	}
	// One series failing does not starve the rest: each is its own
	// transaction, and the failures come back together.
	var errs []error
	for _, s := range due {
		if err := p.sweepSeries(ctx, s.ownerKind, s.arc, s.commandType, s.instance); err != nil {
			errs = append(errs, fmt.Errorf("storage: sweep %s %s: %w", s.commandType, s.arc, err))
		}
	}
	return errors.Join(errs...)
}

func (p *PG) sweepSeries(ctx context.Context, ownerKind, arc, commandType, instance string) error {
	ct, err := p.GetCommandType(ctx, commandType)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now, err := dbNow(ctx, tx)
	if err != nil {
		return err
	}
	if err := p.settleCheck(ctx, tx, ct, ownerKind, arc, instance, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// commandTargetedProperties is the set of property types some command type
// targets: the observations settlement has to look at.
func commandTargetedProperties(ctx context.Context, q querier) (map[string]bool, error) {
	rows, err := q.Query(ctx, `select distinct pt.name from command_type ct join property_type pt on pt.id = ct.target_property_type_id`)
	if err != nil {
		return nil, fmt.Errorf("storage: command-targeted properties: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
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
	cv.Instance = instance
	return &cv, nil
}
