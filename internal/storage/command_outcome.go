package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
	"github.com/jackc/pgx/v5"
)

// A command's terminal outcome, recorded once (#590, #1155): the status, what
// decided it, why in operator words, and the sample that decided it when an
// observation did. Recording it lands the lifecycle event on the owner's
// timeline in the same transaction, so a command's verdict and the event
// that tells it are never separately visible.

// settlementOutcome is a terminal verdict and its evidence.
type settlementOutcome struct {
	status   string
	by       string
	reason   string
	observed *CurrentValue
}

// recordOutcome stamps a command's terminal outcome, once: a command already
// terminal is left as it is (the first verdict stands) and writes no second
// event. The stamp and its lifecycle event share the caller's transaction.
func recordOutcome(ctx context.Context, q txQuerier, id int64, o settlementOutcome, now time.Time) error {
	var (
		obsValue []byte
		obsAt    *time.Time
	)
	if o.observed != nil {
		obsValue, obsAt = o.observed.Value, &o.observed.TS
	}
	var ownerKind, arc, instance, commandType string
	err := q.QueryRow(ctx, `update command c set status = $2, settled_at = $3, settled_by = $4, reason = nullif($5, ''),
			observed_value = $6, observed_at = $7
		where c.id = $1 and c.status = 'issued'
		returning c.owner_kind, coalesce(c.component_id, c.system_id, c.location_id, c.node_id)::text, c.instance,
			(select name from command_type where id = c.command_type_id)`,
		id, o.status, now, o.by, o.reason, obsValue, obsAt).Scan(&ownerKind, &arc, &instance, &commandType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: record command %d settlement: %w", id, err)
	}
	return writeLifecycleEvent(ctx, q, id, ownerKind, arc, instance, commandType, o)
}

// writeLifecycleEvent lands a terminal command's lifecycle event (command-
// settled, command-failed or command-timed-out) on its owner's timeline, a
// caused event naming the command, and points the command at it. It is held
// to the key's writer class like every event (ADR-0161): command-lifecycle
// admits a command alone.
func writeLifecycleEvent(ctx context.Context, q txQuerier, id int64, ownerKind, arc, instance, commandType string, o settlementOutcome) error {
	key, ok := writerclass.CommandOutcomeEvent(o.status)
	if !ok {
		return nil
	}
	if err := admitWrites(ctx, q, writeEvent, gatewayWriter(writerclass.Command), []keyedWrite{{key: key, instance: instance}}); err != nil {
		return err
	}
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return err
	}
	attrs, err := json.Marshal(map[string]any{"command_id": id, "status": o.status, "settled_by": o.by})
	if err != nil {
		return err
	}
	// The event says what happened and to which command, and no more: the
	// reason and the evidence (a reply, an execution error, an observed
	// value) stay on the command, behind command:read, rather than on a
	// timeline that event:read reaches.
	msg := fmt.Sprintf("command %s %s (id %d)", commandType, statusWords(o.status), id)
	var eventID int64
	sql := fmt.Sprintf(`insert into event (owner_kind, %s, event_type_id, instance, origin, message, attributes, provenance, source)
		values ($1, $2, (select id from event_type_in_effect() event_type where name = $3), $4, 'caused', $5, $6, 'observed', 'command')
		returning id`, col)
	if err := q.QueryRow(ctx, sql, ownerKind, arc, key, instance, msg, string(attrs)).Scan(&eventID); err != nil {
		return fmt.Errorf("storage: record command %d lifecycle event: %w", id, err)
	}
	if _, err := q.Exec(ctx, `update command set settled_event_id = $2 where id = $1`, id, eventID); err != nil {
		return fmt.Errorf("storage: link command %d to its lifecycle event: %w", id, err)
	}
	return nil
}

// statusWords is a terminal status as a sentence reads it.
func statusWords(status string) string {
	if status == "timed-out" {
		return "timed out"
	}
	return status
}

// valueText renders a stored value for a reason: a JSON string by its text,
// anything else (a number) as its JSON.
func valueText(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return string(v)
}

// windowText is a settle window as a reason names it.
func windowText(seconds int) string { return fmt.Sprintf("%ds", seconds) }

// targetKey is the key a command type sets, empty for fire-and-forget.
func targetKey(ct *CommandType) string {
	if ct.TargetPropertyType != "" {
		return ct.TargetPropertyType
	}
	return ct.TargetMetricType
}

// observationOutcome is the recorded outcome of a terminal settlement
// verdict. The computed verdict keeps its shipped meaning (past the window,
// anything but a match is failed); the record splits that collapsed failure
// into what the series can distinguish: a component that reported another
// value failed, a window that closed with nothing observed timed out, and
// the reason says which and where the command stopped. A command still
// queued for the endpoint that carries it never reached the device, so a
// reading that disagrees is not the device's answer to it: its window
// closing is a timeout where it stopped, never a failure (ADR-0456).
func (p *PG) observationOutcome(ctx context.Context, q querier, ct *CommandType, id int64, verdict SettlementVerdict, intended, observed *CurrentValue) (settlementOutcome, error) {
	if verdict == SettlementSettled {
		return settlementOutcome{status: "settled", by: SettledByObservation, observed: observed,
			reason: fmt.Sprintf("observed %s, the intended value", valueText(observed.Value))}, nil
	}
	stop, err := p.commandStop(ctx, q, id)
	if err != nil {
		return settlementOutcome{}, err
	}
	if observed != nil && (stop.delivered || !stop.carried) {
		return settlementOutcome{status: "failed", by: SettledByObservation, observed: observed,
			reason: fmt.Sprintf("observed %s, not the intended %s, when its %s settle window closed",
				valueText(observed.Value), valueText(intended.Value), windowText(ct.SettleWindowSeconds))}, nil
	}
	return settlementOutcome{status: "timed-out", by: SettledByWindow, reason: stop.words(ct)}, nil
}

// commandStopFacts is how far a command got: delivered to a node, executed
// (and with what error), and carried by an endpoint (the one it rides,
// commandRideSQL) placed on a node.
type commandStopFacts struct {
	delivered, executed, carried bool
	execErr, endpoint, node      string
}

// words is the timeout reason the facts give (timeoutWords).
func (f commandStopFacts) words(ct *CommandType) string {
	return timeoutWords(ct.Name, targetKey(ct), windowText(ct.SettleWindowSeconds), f.delivered, f.executed, f.carried, f.execErr, f.endpoint, f.node)
}

// commandStop reads how far a command got.
func (p *PG) commandStop(ctx context.Context, q querier, id int64) (commandStopFacts, error) {
	var f commandStopFacts
	err := q.QueryRow(ctx, `select c.dispatched_at is not null, c.executed_at is not null, coalesce(c.exec_error, ''),
			ride.id is not null, coalesce(ride.name, ''),
			coalesce((select n.name from node n where n.principal_id = ride.node_name), '')
		from command c join command_type ct on ct.id = c.command_type_id
		`+commandRideSQL+`
		where c.id = $1`, id).Scan(&f.delivered, &f.executed, &f.execErr, &f.carried, &f.endpoint, &f.node)
	if err != nil {
		return commandStopFacts{}, fmt.Errorf("storage: where command %d stopped: %w", id, err)
	}
	return f, nil
}

// timeoutReason says where a command stopped when its window closed on no
// evidence: answered with nothing observed after, delivered with no report,
// queued for a node that never took it, or carried by no endpoint at all.
func (p *PG) timeoutReason(ctx context.Context, q querier, ct *CommandType, id int64) (string, error) {
	f, err := p.commandStop(ctx, q, id)
	if err != nil {
		return "", err
	}
	return f.words(ct), nil
}

// timeoutWords is timeoutReason's sentence, from the facts it read. Pure.
func timeoutWords(commandType, target, window string, delivered, executed, carried bool, execErr, endpoint, node string) string {
	switch {
	case executed && execErr != "":
		return fmt.Sprintf("the execution failed (%s), and no observation of %s arrived within its %s settle window", execErr, target, window)
	case executed:
		return fmt.Sprintf("the component answered, but no observation of %s arrived within its %s settle window", target, window)
	case delivered && node != "":
		return fmt.Sprintf("delivered to node %s, which sent no execution report within its %s settle window", node, window)
	case delivered:
		return fmt.Sprintf("delivered, but no execution report arrived within its %s settle window", window)
	case carried && node != "":
		return fmt.Sprintf("queued for node %s, which did not take it within its %s settle window", node, window)
	case carried:
		return fmt.Sprintf("queued on endpoint %s, which is placed on no node, so nothing took it within its %s settle window", endpoint, window)
	default:
		return fmt.Sprintf("no endpoint of this component has a driver that binds %s, and no observation of %s arrived within its %s settle window", commandType, target, window)
	}
}
