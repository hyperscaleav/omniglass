package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/key"
	"github.com/hyperscaleav/omniglass/internal/scope"
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
// Scope-guarded on the owner and audited. `value` is the intended value for the
// target (nil for a fire-and-forget command); a metric target requires a JSON
// number. `params` is the raw invocation payload stored on the command and the
// caused event.
func (p *PG) IssueCommand(ctx context.Context, actorID, ownerKind, ownerID, commandType, instance string, value, params json.RawMessage, write scope.Set) (*Command, error) {
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

	if err := p.guardOwnerScope(ctx, tx, ownerKind, ownerID, write); err != nil {
		return nil, err
	}
	// ownerArcValueInScope, not ownerArcValue: guardOwnerScope already
	// confirmed the owner within write, but a bare-name re-resolve here is
	// STILL scope-blind unless it also goes through the scoped variant
	// (ruling 2, #627).
	arc, err := p.ownerArcValueInScope(ctx, tx, ownerKind, ownerID, write)
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
// command whose window expired since the last look is stamped here. The owner is
// scope-guarded (an out-of-scope owner is its non-disclosing not-found).
func (p *PG) CommandSettlement(ctx context.Context, ownerKind, ownerID, commandType, instance string, read scope.Set) (SettlementVerdict, error) {
	oc, ok := ownerContracts[ownerKind]
	if !ok {
		return "", ErrUnknownOwnerKind
	}
	inScope, err := p.ownerInScope(ctx, p.pool, ownerKind, ownerID, read)
	if err != nil {
		return "", err
	}
	if !inScope {
		return "", oc.notFound
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
	// Resolved once, inside this transaction, via ownerArcValueInScope (not
	// the scope-blind ownerArcValue: ruling 2, #627, ownerInScope narrowing
	// to read first does not help if this next statement re-resolves the
	// same bare name scope-blind), and threaded through every call below:
	// passing the id avoids three separate re-derivations of it (settleCheck,
	// twice through latestTargetValue), each of which is then safe to
	// resolve scope-blind because it is handed the uuid, never the original
	// possibly-ambiguous reference.
	arc, err := p.ownerArcValueInScope(ctx, tx, ownerKind, ownerID, read)
	if err != nil {
		return "", err
	}
	// One clock for both ends of the comparison, and the database's, since that
	// is where a sample's ts comes from (dbNow, #667). This transaction started
	// after the one that wrote the intended value committed, so its now() is
	// strictly later than that row's ts: the verdict is a fact about elapsed
	// time rather than about skew between two hosts.
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
