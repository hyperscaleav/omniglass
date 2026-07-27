package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// Command is a recorded invocation: a component was told to do something. It carries
// the same owner exclusive-arc as a sample and an event, the command_type it
// invokes, its params, and the caused event it recorded. Settlement is computed, not
// stored (see Settle / CommandSettlement).
type Command struct {
	ID            int64
	TS            time.Time
	OwnerKind     string
	OwnerID       string
	CommandType   string
	Instance      string
	Params        json.RawMessage
	CausedEventID int64
}

// IssueCommand records a command invocation and, in one transaction, composes the two
// halves of ADR-0063 it depends on: it writes a caused `event` (origin=caused, typed
// command.issued, #395) and, when the command_type targets a property, opens an
// `intended` value in the property cache (#394) that the target's observed value
// settles against. Scope-guarded on the owner and audited. `value` is the intended
// value for the target property (nil for a fire-and-forget command); `params` is the
// raw invocation payload stored on the command and the caused event.
func (p *PG) IssueCommand(ctx context.Context, actorID, ownerKind, ownerID, commandType, instance string, value, params json.RawMessage, write scope.Set) (*Command, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	ct, err := p.GetCommandType(ctx, commandType)
	if err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin issue command: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := p.guardOwnerScope(ctx, tx, ownerKind, ownerID, write); err != nil {
		return nil, err
	}
	arc, err := p.ownerArcValue(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}

	// The caused event: a command.issued occurrence, origin=caused, carrying the
	// params. It is the lineage cause of the intended value.
	var attrs any
	if len(params) > 0 {
		attrs = string(params)
	}
	var causedID int64
	eventSQL := fmt.Sprintf(`insert into event (owner_kind, %s, event_type_id, instance, origin, message, attributes, provenance, source)
		values ($1, $2, (select id from event_type where name = 'command.issued'), $3, 'caused', $4, $5, 'observed', 'command')
		returning id`, col)
	msg := fmt.Sprintf("command %s issued", commandType)
	if err := tx.QueryRow(ctx, eventSQL, ownerKind, arc, instance, msg, attrs).Scan(&causedID); err != nil {
		return nil, fmt.Errorf("storage: record caused event for %q: %w", commandType, err)
	}

	// The command row.
	var cmd Command
	cmdSQL := fmt.Sprintf(`insert into command (owner_kind, %s, command_type_id, instance, params, actor, caused_event_id)
		values ($1, $2, (select id from command_type where name = $3), $4, $5, $6, $7)
		returning id, ts`, col)
	if err := tx.QueryRow(ctx, cmdSQL, ownerKind, arc, commandType, instance, params, actorID, causedID).Scan(&cmd.ID, &cmd.TS); err != nil {
		return nil, fmt.Errorf("storage: insert command %q: %w", commandType, err)
	}

	// A settleable command opens the intended value in the cache: the want the
	// device is told to be, which the observed value settles against.
	if ct.TargetPropertyType != "" && len(value) > 0 {
		propSQL := fmt.Sprintf(`insert into property (owner_kind, %s, property_type_id, instance, provenance, value, ts)
			values ($1, $2, (select id from property_type where name = $3), $4, 'intended', $5, now())
			on conflict (owner_kind, component_id, system_id, location_id, node_id, property_type_id, instance, provenance)
			do update set value = excluded.value, ts = excluded.ts, updated_at = now()
			where property.ts is null or excluded.ts >= property.ts`, col)
		if _, err := tx.Exec(ctx, propSQL, ownerKind, arc, ct.TargetPropertyType, instance, []byte(value)); err != nil {
			return nil, fmt.Errorf("storage: open intended value for %q: %w", commandType, err)
		}
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
// property's latest intended and observed values within the settle window. The owner
// is scope-guarded (an out-of-scope owner is its non-disclosing not-found).
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
	if ct.TargetPropertyType == "" {
		return SettlementNone, nil
	}
	intended, err := p.latestValue(ctx, p.pool, ownerKind, ownerID, ct.TargetPropertyType, instance, "intended")
	if err != nil {
		return "", err
	}
	observed, err := p.latestValue(ctx, p.pool, ownerKind, ownerID, ct.TargetPropertyType, instance, "observed")
	if err != nil {
		return "", err
	}
	return Settle(intended, observed, ct.SettleWindowSeconds, time.Now().UTC()), nil
}
