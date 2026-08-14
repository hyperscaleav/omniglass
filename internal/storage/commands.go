package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/key"
)

// ErrCommandValueNotNumeric refuses an intended value that cannot open a metric
// series row: the metric lane stores doubles, so the value must be a JSON number.
var ErrCommandValueNotNumeric = errors.New("storage: intended value for a metric target must be numeric")

// ErrCommandParamsInvalid refuses an invocation whose params violate the command
// type's params_schema: the schema is the published contract for the params, so a
// violating payload refuses before anything is written rather than being recorded
// as if it satisfied it (#595).
var ErrCommandParamsInvalid = errors.New("storage: command params violate the command type's params_schema")

// Command is a recorded invocation: a component was told to do something. It carries
// the same owner exclusive-arc as a sample and an event, the command_type it
// invokes, its params, and the caused event it recorded. The settlement verdict is
// computed (see Settle / CommandSettlement); the terminal outcome is recorded on the
// row (#590): Status is issued until a settle-check stamps settled, failed, or
// timed-out, and SettledAt is the terminal moment.
type Command struct {
	ID            int64
	TS            time.Time
	OwnerKind     string
	OwnerID       string
	CommandType   string
	Instance      string
	Params        json.RawMessage
	CausedEventID int64
	Status        string
	SettledAt     *time.Time
}

// IssueCommand records a command invocation and, in one transaction, composes the
// halves of ADR-0063 it depends on: it writes a caused `event` (origin=caused, typed
// command-issued, #395) and, when the command_type targets a property or a metric,
// opens an `intended` series row (#394, #590) that the target's observed value
// settles against. The intended row names this command (command_id lineage) beside
// the caused event. A command that opens no intended value has nothing to settle and
// is recorded settled at issue; one that does is swept by the settle-check before
// the transaction commits, so a zero-window command returns already terminal.
// Audited. `value` is the intended value for the target (nil for a
// fire-and-forget command); a metric target requires a JSON number. `params` is
// the raw invocation payload stored on the command and the caused event.
//
// ownerID is a RESOLVED id, and this takes no scope of its own (#749). The fence
// on an actuation is the caller's command:issue scope, and applying it is not a
// check this method could make from one set: it decides the target against the
// caller's READ scope as well (the 404-versus-403 split, ADR-0116), so the
// resolve that applies both is ResolveActionTarget, at the one route that issues.
// A set re-checked here would either re-answer that settled question or answer a
// different one and disagree with it, and re-resolving the caller's raw reference
// is the second name resolve ruling 2 (#627) forbids. What is left for the id is
// an existence check, which is what ownerArcValue does. Same contract as
// AcknowledgeAlarm, whose component is resolved by its route and bound by id
// here for the same reason.
//
// A bare NAME still resolves through that existence check, estate-wide, and a
// route that passed one would be a route with no fence rather than one with a
// wide fence. That failure is loud rather than silent: an ambiguous name refuses
// as ErrAmbiguousName instead of picking a row, and an unambiguous one is a
// caller that never applied a scope at all, which the conformance matrix's
// readable-but-not-actionable branch fails on.
func (p *PG) IssueCommand(ctx context.Context, actorID, ownerKind, ownerID, commandType, instance string, value, params json.RawMessage) (*Command, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	ct, err := p.GetCommandType(ctx, commandType)
	if err != nil {
		return nil, err
	}
	// The params contract enforces at the door (#595): a violating payload
	// refuses before the transaction opens, so a refusal projects nothing.
	// Absent params validate as the empty object, so a schema with required
	// fields refuses them and one of optional fields accepts them.
	if len(bytes.TrimSpace(ct.ParamsSchema)) > 0 {
		checked := params
		if len(bytes.TrimSpace(checked)) == 0 {
			checked = json.RawMessage(`{}`)
		}
		if err := key.ValidateValue("json", checked, ct.ParamsSchema); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCommandParamsInvalid, err)
		}
	}
	opensIntended := (ct.TargetPropertyType != "" || ct.TargetMetricType != "") && len(value) > 0
	// A metric target's intended value parses before anything is written, so a
	// non-numeric value refuses loudly instead of half-recording the command.
	var metricValue float64
	if opensIntended && ct.TargetMetricType != "" {
		if err := json.Unmarshal(value, &metricValue); err != nil {
			return nil, fmt.Errorf("%w: %q", ErrCommandValueNotNumeric, value)
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin issue command: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The owner arrives resolved and in scope, so this binds the arc column and
	// confirms the row is still there (a component deleted between the resolve
	// and here is its own not-found rather than a foreign-key error). For the
	// three tree kinds the arc value IS the id it was handed; the node arm is
	// what makes the call worth making, since a node's arc value is its
	// principal_id.
	arc, err := p.ownerArcValue(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}

	// The caused event: a command-issued occurrence, origin=caused, carrying the
	// params. It stays stamped on the intended value as context, though the
	// value's lineage is the command itself now.
	var attrs any
	if len(params) > 0 {
		attrs = string(params)
	}
	var causedID int64
	eventSQL := fmt.Sprintf(`insert into event (owner_kind, %s, event_type_id, instance, origin, message, attributes, provenance, source)
		values ($1, $2, (select id from event_type where name = 'command-issued'), $3, 'caused', $4, $5, 'observed', 'command')
		returning id`, col)
	msg := fmt.Sprintf("command %s issued", commandType)
	if err := tx.QueryRow(ctx, eventSQL, ownerKind, arc, instance, msg, attrs).Scan(&causedID); err != nil {
		return nil, fmt.Errorf("storage: record caused event for %q: %w", commandType, err)
	}

	// The command row. One that opens no intended value goes straight to settled
	// (nothing was told to a series, so there is nothing to wait on); the shipped
	// verdict for it stays none.
	status := "issued"
	if !opensIntended {
		status = "settled"
	}
	var cmd Command
	cmdSQL := fmt.Sprintf(`insert into command (owner_kind, %s, command_type_id, instance, params, actor, caused_event_id, status, settled_at)
		values ($1, $2, (select id from command_type where name = $3), $4, $5, $6, $7, $8,
		        case when $8 = 'issued' then null else now() end)
		returning id, ts`, col)
	if err := tx.QueryRow(ctx, cmdSQL, ownerKind, arc, commandType, instance, params, actorID, causedID, status).Scan(&cmd.ID, &cmd.TS); err != nil {
		return nil, fmt.Errorf("storage: insert command %q: %w", commandType, err)
	}

	// A settleable command opens the intended value as a series row (#591): the
	// want the device is told to be, which the observed value settles against.
	// Appended, never upserted, so every command's want is history; the row names
	// this command (its lineage) and keeps the caused event stamped.
	if opensIntended {
		if ct.TargetMetricType != "" {
			metricSQL := fmt.Sprintf(`insert into metric (owner_kind, %s, metric_type_id, instance, provenance, value, source, event_id, command_id)
				values ($1, $2, (select id from metric_type where name = $3), $4, 'intended', $5, 'command', $6, $7)`, col)
			if _, err := tx.Exec(ctx, metricSQL, ownerKind, arc, ct.TargetMetricType, instance, metricValue, causedID, cmd.ID); err != nil {
				return nil, fmt.Errorf("storage: open intended metric for %q: %w", commandType, err)
			}
		} else {
			propSQL := fmt.Sprintf(`insert into property (owner_kind, %s, property_type_id, instance, provenance, value, source, event_id, command_id)
				values ($1, $2, (select id from property_type where name = $3), $4, 'intended',
				        $5::jsonb, 'command', $6, $7)`, col)
			if _, err := tx.Exec(ctx, propSQL, ownerKind, arc, ct.TargetPropertyType, instance, []byte(value), causedID, cmd.ID); err != nil {
				return nil, fmt.Errorf("storage: open intended value for %q: %w", commandType, err)
			}
		}
		// The settle-check runs at the write, so a command whose window is
		// already past (a zero window) is recorded terminal in this transaction.
		// arc is already resolved above; passed through rather than left for
		// settleCheck to re-derive. `now` comes from the database (dbNow, #667),
		// which here is the very timestamp the intended row above was stamped
		// with, since both are this transaction's now().
		now, err := dbNow(ctx, tx)
		if err != nil {
			return nil, err
		}
		if err := p.settleCheck(ctx, tx, ct, ownerKind, arc, instance, now); err != nil {
			return nil, err
		}
	}
	if err := tx.QueryRow(ctx, `select status, settled_at from command where id = $1`, cmd.ID).Scan(&cmd.Status, &cmd.SettledAt); err != nil {
		return nil, fmt.Errorf("storage: reload command %d status: %w", cmd.ID, err)
	}

	if err := writeAuditRes(ctx, tx, actorID, "issue", "command", fmt.Sprintf("%d", cmd.ID), nil, map[string]any{
		"command_type": commandType, "owner_kind": ownerKind, "owner": ownerID, "instance": instance,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit issue command: %w", err)
	}

	cmd.OwnerKind, cmd.OwnerID, cmd.CommandType, cmd.Instance = ownerKind, ownerID, commandType, instance
	cmd.Params = copyRaw(params)
	cmd.CausedEventID = causedID
	return &cmd, nil
}

// CommandSettlement computes the settlement verdict for a command_type's target on an
// owner: none for a fire-and-forget command (no target), else Settle over the target
// series' latest intended and observed values within the settle window. It is also
// the settle-check's read-side trigger: before computing, it sweeps the series'
// still-issued commands and records any terminal outcome it sees (settleCheck), so a
// command whose window expired since the last look is stamped here.
//
// ownerID is a RESOLVED id and this takes no scope, for the same reason
// IssueCommand does not (#749): its one caller is the issue route, which has
// already resolved this exact row through both the read and the command:issue
// sets and is now asking about the command it just recorded on it. A second set
// checked against the same row cannot narrow anything the resolve did not, and a
// read set checked here would be the wrong question anyway, since the row this
// verdict describes was reached by the ISSUE scope. A route that computes a
// settlement from somewhere other than an issue resolves its own target first,
// exactly as this one does.
func (p *PG) CommandSettlement(ctx context.Context, ownerKind, ownerID, commandType, instance string) (SettlementVerdict, error) {
	if _, ok := ownerContracts[ownerKind]; !ok {
		return "", ErrUnknownOwnerKind
	}
	ct, err := p.GetCommandType(ctx, commandType)
	if err != nil {
		return "", err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("storage: begin settle check: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The arc column, bound once inside this transaction from the id the caller
	// resolved, and threaded through every call below: passing it avoids three
	// separate re-derivations (settleCheck, twice through latestTargetValue),
	// each of which is then safe to resolve scope-blind because it is handed the
	// uuid, never the original possibly-ambiguous reference. Ruling 2 (#627) is
	// satisfied by there being no name resolve left on this path at all, rather
	// than by a scoped one: the reference this used to re-resolve is now an id.
	arc, err := p.ownerArcValue(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return "", err
	}
	// One clock for both ends of the comparison, and the database's, since that
	// is where a sample's ts comes from (dbNow, #667). That is what the change
	// bought: the verdict is a fact about elapsed time on one server rather than
	// about skew between two hosts.
	//
	// What it does NOT buy is an ordering (#718). now() is transaction_timestamp,
	// and this runs at READ COMMITTED, so an IssueCommand that BEGAN after this
	// transaction did can still commit before a statement here takes its
	// snapshot: the intended row this then reads carries a ts later than this
	// now(), and now.Sub(intended.TS) is negative. Nothing downstream is wrong,
	// and deliberately so rather than by luck: a negative delta is less than any
	// positive window, which is `pending`, the right answer for a command issued
	// a moment ago, and a zero window never consults a timestamp at all
	// (ADR-0108). The claim this comment used to make, that the transaction's
	// now() is strictly later than the intended row's ts, is one the isolation
	// level does not give, and a future change that leaned on it would be wrong.
	now, err := dbNow(ctx, tx)
	if err != nil {
		return "", err
	}
	if err := p.settleCheck(ctx, tx, ct, ownerKind, arc, instance, now); err != nil {
		return "", err
	}
	verdict := SettlementNone
	if ct.TargetPropertyType != "" || ct.TargetMetricType != "" {
		intended, err := p.latestTargetValue(ctx, tx, ct, ownerKind, arc, instance, "intended")
		if err != nil {
			return "", err
		}
		observed, err := p.latestTargetValue(ctx, tx, ct, ownerKind, arc, instance, "observed")
		if err != nil {
			return "", err
		}
		verdict = Settle(intended, observed, ct.SettleWindowSeconds, now)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("storage: commit settle check: %w", err)
	}
	return verdict, nil
}
