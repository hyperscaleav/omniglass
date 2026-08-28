package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The command wire's storage side (#815): resolving a node's pending command
// queue (the commands whose owning component has a driver-attached endpoint
// placed on that node, with a binding for the command's type), and recording
// the execution report. Delivery is at-least-once: a dispatched command whose
// execution report never lands is redelivered after redeliverAfter, and
// execution idempotence lives at the node (per command id) and in the
// once-only executed_at stamp here.

const (
	// redeliverAfter is how long a dispatched command waits for its execution
	// report before the next pull redelivers it.
	redeliverAfter = "30 seconds"
	// deliveryTTL bounds how long an unexecuted command stays deliverable at
	// all: past it, a command no node picked up is abandoned to its
	// settlement arc (a settleable one is stamped failed by the settle-check
	// when its window passes; a fire-and-forget one simply never actuated).
	deliveryTTL = "10 minutes"
)

// CommandDelivery is one command resolved for a node's queue: the rendered
// request (templates resolved against the command's intended value and
// params), the endpoint target it actuates over, and the transport.
type CommandDelivery struct {
	ID          int64
	CommandType string
	Transport   string
	Target      string
	Line        string
}

// PendingNodeCommands resolves and marks dispatched the commands a node
// should actuate now: unexecuted, within the delivery TTL, not recently
// dispatched, owned by a component whose driver-attached endpoint is placed
// on this node, with the driver's spec binding the command's type. A command
// whose binding cannot render (a template reference the issue did not supply)
// is skipped with its execution error recorded, so it stops being redelivered
// and the fault is visible on the row.
func (p *PG) PendingNodeCommands(ctx context.Context, node string) ([]CommandDelivery, error) {
	rows, err := p.pool.Query(ctx, `
		select distinct on (c.id) c.id, ct.name, c.params, i.transport, i.params, d.spec,
			(select pr.value from property pr where pr.command_id = c.id and pr.provenance = 'intended' order by pr.ts desc limit 1),
			(select m.value from metric m where m.command_id = c.id and m.provenance = 'intended' order by m.ts desc limit 1)
		from command c
		join command_type ct on ct.id = c.command_type_id
		join endpoint i on i.component = c.component_id and i.driver_id is not null
		join driver d on d.id = i.driver_id
		where c.owner_kind = 'component'
		  and c.executed_at is null
		  and (c.dispatched_at is null or c.dispatched_at < now() - interval '`+redeliverAfter+`')
		  and c.ts > now() - interval '`+deliveryTTL+`'
		  and i.node_name = (select principal_id from node where name = $1)
		order by c.id, i.name`, node)
	if err != nil {
		return nil, fmt.Errorf("storage: pending commands for %q: %w", node, err)
	}
	defer rows.Close()

	type pendingRow struct {
		id             int64
		commandType    string
		params         []byte
		transport      string
		endpointParams []byte
		spec           []byte
		intendedProp   []byte
		intendedMetric *float64
	}
	var pending []pendingRow
	for rows.Next() {
		var r pendingRow
		if err := rows.Scan(&r.id, &r.commandType, &r.params, &r.transport, &r.endpointParams, &r.spec, &r.intendedProp, &r.intendedMetric); err != nil {
			return nil, fmt.Errorf("storage: scan pending command: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: pending commands for %q: %w", node, err)
	}

	var out []CommandDelivery
	var delivered []int64
	for _, r := range pending {
		sp, err := driver.Parse(r.spec)
		if err != nil {
			continue // a stored spec is validated at write; skip corruption
		}
		var binding *driver.CommandBinding
		for i := range sp.Commands {
			if sp.Commands[i].CommandType == r.commandType {
				binding = &sp.Commands[i]
				break
			}
		}
		if binding == nil {
			continue // this driver does not actuate this command type
		}
		req, err := driver.RenderRequest(binding.Request, intendedString(r.intendedProp, r.intendedMetric), paramsMap(r.params))
		if err != nil {
			// An unrenderable binding is terminal, not retryable: record it as
			// the execution error so the row stops being redelivered and the
			// fault is visible where the operator looks.
			if _, uerr := p.pool.Exec(ctx, `update command set executed_at = now(), exec_error = $2 where id = $1 and executed_at is null`, r.id, err.Error()); uerr != nil {
				return nil, fmt.Errorf("storage: record render fault for command %d: %w", r.id, uerr)
			}
			continue
		}
		out = append(out, CommandDelivery{
			ID:          r.id,
			CommandType: r.commandType,
			Transport:   r.transport,
			Target:      targetOf(r.endpointParams),
			Line:        req.Line,
		})
		delivered = append(delivered, r.id)
	}
	if len(delivered) > 0 {
		if _, err := p.pool.Exec(ctx, `update command set dispatched_at = now() where id = any($1)`, delivered); err != nil {
			return nil, fmt.Errorf("storage: mark commands dispatched: %w", err)
		}
	}
	return out, nil
}

// RecordCommandExecution stamps a node's execution report, once: a redelivered
// report is a no-op, and only the node the command's endpoint is placed on may
// stamp it (the same confinement the worklist carries).
func (p *PG) RecordCommandExecution(ctx context.Context, node string, commandID int64, execErr string) error {
	tag, err := p.pool.Exec(ctx, `
		update command c set executed_at = now(), exec_error = nullif($3, '')
		where c.id = $1
		  and c.executed_at is null
		  and exists (
			select 1 from endpoint i
			where i.component = c.component_id
			  and i.node_name = (select principal_id from node where name = $2))`,
		commandID, node, execErr)
	if err != nil {
		return fmt.Errorf("storage: record execution of command %d: %w", commandID, err)
	}
	_ = tag
	return nil
}

// intendedString renders a command's intended value for the request template:
// the property arm's jsonb unquoted, or the metric arm's number.
func intendedString(prop []byte, metric *float64) string {
	if len(prop) > 0 {
		var s string
		if err := json.Unmarshal(prop, &s); err == nil {
			return s
		}
		return string(prop) // a non-string jsonb scalar renders verbatim
	}
	if metric != nil {
		return strconv.FormatFloat(*metric, 'f', -1, 64)
	}
	return ""
}

// paramsMap decodes a command's params for template args; nil on none.
func paramsMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// targetOf reads the probed target off endpoint params.
func targetOf(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var p struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ""
	}
	return p.Target
}
