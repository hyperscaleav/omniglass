package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/jackc/pgx/v5"
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
	// Pick ONE actuating endpoint per command, globally: only an endpoint whose
	// driver spec binds the command's type is a candidate (the jsonb_path_exists
	// predicate), so the distinct-on can never collapse to a non-binding
	// endpoint, and the same binding endpoint is chosen deterministically on
	// every pull. The node filter is applied to that chosen endpoint, so a
	// command is delivered to exactly one node (never once per node), which is
	// what makes the render-fault terminal stamp below correct: there is no
	// other endpoint that would have rendered it differently.
	rows, err := p.pool.Query(ctx, `
		with candidate as (
			select distinct on (c.id)
				c.id, ct.name as command_type, c.params, i.id as endpoint_id, i.transport,
				i.params as endpoint_params, i.node_name, d.spec,
				(select pr.value from property pr where pr.command_id = c.id and pr.provenance = 'intended' order by pr.ts desc limit 1) as intended_prop,
				(select m.value from metric m where m.command_id = c.id and m.provenance = 'intended' order by m.ts desc limit 1) as intended_metric
			from command c
			join command_type ct on ct.id = c.command_type_id
			join endpoint i on i.component = c.component_id and i.driver_id is not null
			join driver d on d.id = i.driver_id
			where c.owner_kind = 'component'
			  and c.executed_at is null
			  and (c.dispatched_at is null or c.dispatched_at < now() - interval '`+redeliverAfter+`')
			  and c.ts > now() - interval '`+deliveryTTL+`'
			  and jsonb_path_exists(d.spec, '$.commands[*] ? (@.command_type == $ct)', jsonb_build_object('ct', ct.name))
			order by c.id, i.name
		)
		select id, command_type, params, endpoint_id, transport, endpoint_params, spec, intended_prop, intended_metric
		from candidate
		where node_name = (select principal_id from node where name = $1)`, node)
	if err != nil {
		return nil, fmt.Errorf("storage: pending commands for %q: %w", node, err)
	}
	defer rows.Close()

	type pendingRow struct {
		id             int64
		commandType    string
		params         []byte
		endpointID     string
		transport      string
		endpointParams []byte
		spec           []byte
		intendedProp   []byte
		intendedMetric *float64
	}
	var pending []pendingRow
	for rows.Next() {
		var r pendingRow
		if err := rows.Scan(&r.id, &r.commandType, &r.params, &r.endpointID, &r.transport, &r.endpointParams, &r.spec, &r.intendedProp, &r.intendedMetric); err != nil {
			return nil, fmt.Errorf("storage: scan pending command: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: pending commands for %q: %w", node, err)
	}

	type dispatch struct {
		id         int64
		endpointID string
	}
	var out []CommandDelivery
	var delivered []dispatch
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
			continue // the jsonb predicate already ensured this, defensively kept
		}
		req, err := driver.RenderRequest(binding.Request, intendedString(r.intendedProp, r.intendedMetric), paramsMap(r.params))
		if err != nil {
			// An unrenderable binding is terminal, not retryable, and there is
			// exactly one delivery endpoint per command, so recording the fault
			// on the row (which stops redelivery and shows the operator the
			// cause) cannot strand a delivery another endpoint would have made.
			if _, uerr := p.pool.Exec(ctx, `update command set executed_at = now(), exec_error = $2, dispatched_endpoint_id = $3 where id = $1 and executed_at is null`, r.id, err.Error(), r.endpointID); uerr != nil {
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
		delivered = append(delivered, dispatch{r.id, r.endpointID})
	}
	for _, d := range delivered {
		if _, err := p.pool.Exec(ctx, `update command set dispatched_at = now(), dispatched_endpoint_id = $2 where id = $1`, d.id, d.endpointID); err != nil {
			return nil, fmt.Errorf("storage: mark command %d dispatched: %w", d.id, err)
		}
	}
	return out, nil
}

// RecordCommandExecution stamps a node's execution report, once: a redelivered
// report is a no-op, and only the node the command's DISPATCHED endpoint is
// placed on may stamp it. Confining on the dispatched endpoint (not merely on
// any endpoint of the command's component) is what keeps a second node that
// happens to hold an unrelated endpoint on the same component from stamping a
// command it never received. An update that matches no row (a report for a
// command dispatched to a different node, or already stamped) is ErrCommandNotDispatchedHere,
// which the bus logs rather than silently dropping.
//
// When the binding settles from the response (ADR-0141), the reply decides
// the command in the same transaction: a value it carries is recorded as an
// observation of the target on the dispatched endpoint and settles or fails
// the command against its intent; a confirmation settles it; a reply that
// fails the binding's reading, or an execution error, fails it at once.
func (p *PG) RecordCommandExecution(ctx context.Context, node string, commandID int64, execErr, reply string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin record execution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var (
		ownerKind, arc, instance, commandType, endpointID string
	)
	err = tx.QueryRow(ctx, `
		update command c set executed_at = now(), exec_error = nullif($3, '')
		where c.id = $1
		  and c.executed_at is null
		  and c.dispatched_endpoint_id is not null
		  and exists (
			select 1 from endpoint i
			where i.id = c.dispatched_endpoint_id
			  and i.node_name = (select principal_id from node where name = $2))
		returning c.owner_kind, coalesce(c.component_id, c.system_id, c.location_id, c.node_id)::text,
			c.instance, (select name from command_type where id = c.command_type_id), c.dispatched_endpoint_id::text`,
		commandID, node, execErr).Scan(&ownerKind, &arc, &instance, &commandType, &endpointID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCommandNotDispatchedHere
	}
	if err != nil {
		return fmt.Errorf("storage: record execution of command %d: %w", commandID, err)
	}
	if err := p.settleFromResponse(ctx, tx, commandID, ownerKind, arc, instance, commandType, endpointID, execErr, reply); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit record execution: %w", err)
	}
	return nil
}

// settleFromResponse applies the dispatched endpoint's binding to the reply,
// when that binding settles from the response; otherwise it decides nothing
// and the command waits on observation.
func (p *PG) settleFromResponse(ctx context.Context, tx pgx.Tx, commandID int64, ownerKind, arc, instance, commandType, endpointID, execErr, reply string) error {
	var epName, transport string
	var spec []byte
	err := tx.QueryRow(ctx, `select e.name, e.transport, d.spec from endpoint e join driver d on d.id = e.driver_id where e.id = $1`,
		endpointID).Scan(&epName, &transport, &spec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: dispatched endpoint of command %d: %w", commandID, err)
	}
	sp, err := driver.Parse(spec)
	if err != nil {
		return nil // a stored spec is validated at write; nothing to read it by
	}
	var binding *driver.CommandBinding
	for i := range sp.Commands {
		if sp.Commands[i].CommandType == commandType {
			binding = &sp.Commands[i]
		}
	}
	if binding == nil || binding.Settle == nil || binding.Settle.From != driver.SettleFromResponse {
		return nil
	}
	now, err := dbNow(ctx, tx)
	if err != nil {
		return err
	}
	if execErr != "" {
		return markSettlement(ctx, tx, commandID, "failed", now)
	}
	verdict := binding.JudgeReply(reply)
	if !verdict.Succeeded {
		return markSettlement(ctx, tx, commandID, "failed", now)
	}
	if !verdict.HasValue {
		return markSettlement(ctx, tx, commandID, "settled", now)
	}
	ct, err := scanCommandType(tx.QueryRow(ctx, `select `+commandTypeCols+` from command_type where name = $1`, commandType))
	if err != nil {
		return fmt.Errorf("storage: command type %s: %w", commandType, err)
	}
	series := instance
	if series == "" {
		series = epName
	}
	// The reply is an observation of the target, recorded where the
	// driver's polls of it land.
	if ct.TargetPropertyType != "" {
		col, err := ownerColumn(ownerKind)
		if err != nil {
			return err
		}
		sql := fmt.Sprintf(`insert into property (ts, owner_kind, %s, property_type_id, instance, value, provenance, source)
			values ($1, $2, $3, (select id from property_type where name = $4), $5, to_jsonb($6::text), 'observed', $7)`, col)
		if _, err := tx.Exec(ctx, sql, now, ownerKind, arc, ct.TargetPropertyType, series, verdict.Value, transport); err != nil {
			return fmt.Errorf("storage: record command %d reply as an observation: %w", commandID, err)
		}
	}
	intended, err := p.commandIntendedValue(ctx, tx, ct, commandID)
	if err != nil {
		return err
	}
	status := "settled"
	if intended != nil && !replyMatchesIntent(intended.Value, verdict.Value) {
		status = "failed"
	}
	return markSettlement(ctx, tx, commandID, status, now)
}

// replyMatchesIntent compares a reply's extracted text to an intended value:
// a JSON string by its text, anything else (a number) by its JSON form.
func replyMatchesIntent(intended json.RawMessage, value string) bool {
	var s string
	if json.Unmarshal(intended, &s) == nil {
		return s == value
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		var want float64
		if json.Unmarshal(intended, &want) == nil {
			return f == want
		}
	}
	return bytes.Equal(bytes.TrimSpace(intended), []byte(value))
}

// ErrCommandNotDispatchedHere marks an execution report a node had no standing
// to make: the command was not dispatched to an endpoint on that node, or was
// already stamped. The bus logs it; it is never a silent drop.
var ErrCommandNotDispatchedHere = errors.New("storage: command not dispatched to this node")

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

// paramsMap decodes a command's params for template args; nil on none. Numbers
// decode as json.Number (via UseNumber), so a preset like 1000000 renders as
// "1000000" rather than float64 "%v"'s "1e+06" when substituted into a request.
func paramsMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
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
