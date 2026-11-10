package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hyperscaleav/omniglass/internal/key"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
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
	// all, as SQL interval text (CommandDeliveryWindow is the same span): past
	// it, a command no node picked up is abandoned to its settlement arc (a
	// settleable one is judged by the settle-check when its window passes; a
	// fire-and-forget one, which only its execution report decides, is timed
	// out by the sweep, #1630).
	deliveryTTL = "10 minutes"
)

// CommandDeliveryWindow is deliveryTTL as a duration: how long a command stays
// deliverable, and so how long a fire-and-forget command waits on its
// execution report before it reads timed out (#1630).
const CommandDeliveryWindow = 10 * time.Minute

// commandOpensIntendedSQL is true of a command (aliased c) that opened an
// intended row, the value its observation settles against; one that opened
// none is fire-and-forget, decided by its execution report (#1630).
const commandOpensIntendedSQL = `(exists (select 1 from property pr where pr.command_id = c.id and pr.provenance = 'intended')
			or exists (select 1 from metric_declared md where md.command_id = c.id))`

// CommandDelivery is one command resolved for a node's queue: the rendered
// request (templates resolved against the command's intended value and
// params), the endpoint target it actuates over, and the transport.
type CommandDelivery struct {
	ID          int64
	CommandType string
	Transport   string
	Target      string
	Line        string
	// EndpointID is the endpoint the command dispatches to (a node holding a
	// session there actuates over it, #817); Reply is the binding's declared
	// reply shape.
	EndpointID string
	Reply      *driver.Reply
	// SessionOnly: the command rides the endpoint's held session or fails
	// (#1049), since its driver declares a session or its transport has no
	// stateless line.
	SessionOnly bool
	// Retries is how many times a udp command is re-sent when no answer
	// comes (#1037): what its binding declares, else none, since a command
	// sent twice may act twice.
	Retries int
	// Request is an http command's rendered request (#902), never carrying
	// a credential: the node applies the endpoint's scheme.
	Request *driver.Request
	// Disruptive: the command takes its device down (#1651). The node
	// closes the gate of every endpoint in HoldEndpoints before it sends,
	// runs nothing against them for QuietSeconds, then reopens each on its
	// own first up verdict, with HoldSeconds the backstop. Empty for any
	// other command. The hold set follows the dependency graph (#1648,
	// ResolveHoldSet): the target's in-band endpoints and its host
	// dependents', or for a power command what the named outlet feeds, each
	// placed on this node.
	Disruptive    bool
	QuietSeconds  int
	HoldSeconds   int
	HoldEndpoints []string
}

// commandCandidates is the one resolution of which node actuates a command
// (#1626), shared by the pull (PendingNodeCommands) and the queue's health
// read (CommandQueueHealth), so the depth the health read reports can never
// disagree with what a pull delivers. It is the body of a CTE: every
// unexecuted component command issued after lower, each with the ONE endpoint
// that would carry it. Only an endpoint whose driver spec binds the command's
// type is a candidate (the jsonb_path_exists predicate), so the distinct-on
// never collapses to a non-binding endpoint, and the same endpoint is chosen
// on every read whatever the command's dispatch state. A command no endpoint
// binds is no node's and is absent. The unexecuted predicate is
// command_unexecuted_idx's, which keeps the read off the executed history.
//
// components narrows the read to one node's own components (#1631): the name
// of a CTE yielding component_id, joined ahead of command so the scan of
// command is searched by component through command_deliverable_idx. Empty
// reads every component's commands (the fleet's health read). The actuating
// endpoint is chosen over all of a command's endpoints either way.
//
// issuedOnly keeps only the commands still issued (#1630), the pull's read: a
// terminal command is never handed to a node. The health read takes every
// unexecuted command and reads the status itself, since a command that aged
// out of its window unexecuted is a delivery loss to count whatever status
// its settlement then stamped.
func commandCandidates(lower, components string, issuedOnly bool) string {
	from := "command c"
	if components != "" {
		from = components + " h\n\t\tjoin command c on c.component_id = h.component_id"
	}
	status := ""
	if issuedOnly {
		status = "\n\t\t  and c.status = 'issued'"
	}
	return `
		select distinct on (c.id)
			c.id, c.ts, c.status, c.dispatched_at, c.first_dispatched_at, c.delivery_count, ct.name as command_type, c.params,
			i.id as endpoint_id, i.transport, i.params as endpoint_params, i.node_name, d.spec,
			ct.disruptive, ct.quiet_seconds, ct.hold_seconds,
			c.component_id::text as target_id, coalesce(ct.power_outlet_param, '') as power_outlet_param
		from ` + from + `
		join command_type ct on ct.id = c.command_type_id
		join endpoint i on i.component = c.component_id and i.driver_id is not null
		join driver d on d.id = i.driver_id
		where c.owner_kind = 'component'
		  and c.executed_at is null` + status + `
		  and c.ts > ` + lower + `
		  and jsonb_path_exists(d.spec, '$.commands[*] ? (@.command_type == $ct)', jsonb_build_object('ct', ct.name))
		order by c.id, i.name`
}

// PendingNodeCommands resolves and marks dispatched the commands a node
// should actuate now: unexecuted, still issued (a terminal command, timed out
// or failed, is never handed to a node, #1630), within the delivery TTL, not recently
// dispatched, owned by a component whose driver-attached endpoint is placed
// on this node, with the driver's spec binding the command's type. A command
// whose binding cannot render (a template reference the issue did not supply)
// is skipped with its execution error recorded, so it stops being redelivered
// and the fault is visible on the row.
func (p *PG) PendingNodeCommands(ctx context.Context, node string) ([]CommandDelivery, error) {
	// Read only this node's components' commands (#1631): the here CTE is
	// the components with a driver-attached endpoint placed on this node, and
	// the candidate fragment searches command by those components, through
	// command_deliverable_idx, so a pull costs the node's own backlog, never
	// the fleet's.
	//
	// The pull reads only commands still issued (#1630): a terminal one, timed
	// out or failed, is never handed to a node. command_deliverable_idx
	// carries the same status term, so it holds only the rows a pull can
	// deliver, never the unexecuted terminal commands ever issued.
	//
	// The candidate fragment still picks ONE actuating endpoint per command
	// over ALL of its component's endpoints, not only this node's, so a
	// command is delivered to exactly one node (never once per node), which
	// is what makes the render-fault terminal stamp below correct: there is no
	// other endpoint that would have rendered it differently. The node filter
	// and the redelivery wait apply to that chosen endpoint, outside the
	// fragment, so the health read resolves the same endpoint for every
	// command whether it is due now or not.
	rows, err := p.pool.Query(ctx, `
		with here as (
			select distinct e.component as component_id
			from endpoint e
			where e.node_name = (select principal_id from node where name = $1)
			  and e.driver_id is not null
			  and e.component is not null
		),
		candidate as (`+commandCandidates("now() - interval '"+deliveryTTL+"'", "here", true)+`)
		select k.id, k.command_type, k.params, k.endpoint_id, k.transport, k.endpoint_params, k.spec,
			(select pr.value from property pr where pr.command_id = k.id and pr.provenance = 'intended' order by pr.ts desc limit 1),
			(select m.value from metric_declared m where m.command_id = k.id order by m.ts desc limit 1),
			k.disruptive, k.quiet_seconds, k.hold_seconds, k.target_id, k.node_name::text, k.power_outlet_param
		from candidate k
		where k.node_name = (select principal_id from node where name = $1)
		  and (k.dispatched_at is null or k.dispatched_at < now() - interval '`+redeliverAfter+`')`, node)
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
		disruptive     bool
		quiet, hold    int
		targetID       string
		nodeName       string
		outletParam    string
	}
	var pending []pendingRow
	for rows.Next() {
		var r pendingRow
		if err := rows.Scan(&r.id, &r.commandType, &r.params, &r.endpointID, &r.transport, &r.endpointParams, &r.spec, &r.intendedProp, &r.intendedMetric,
			&r.disruptive, &r.quiet, &r.hold, &r.targetID, &r.nodeName, &r.outletParam); err != nil {
			return nil, fmt.Errorf("storage: scan pending command: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: pending commands for %q: %w", node, err)
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
		req, err := driver.RenderRequestTyped(binding.Request, intendedString(r.intendedProp, r.intendedMetric), intendedJSON(r.intendedProp, r.intendedMetric), paramsMap(r.params))
		if err != nil {
			// An unrenderable binding is terminal, not retryable, and there is
			// exactly one delivery endpoint per command, so recording the fault
			// on the row (which stops redelivery and shows the operator the
			// cause) cannot strand a delivery another endpoint would have made.
			if uerr := p.recordRenderFault(ctx, r.id, r.endpointID, err); uerr != nil {
				return nil, uerr
			}
			continue
		}
		d := CommandDelivery{
			ID:          r.id,
			CommandType: r.commandType,
			Transport:   r.transport,
			Target:      targetOf(r.endpointParams),
			Line:        req.Line,
			EndpointID:  r.endpointID,
			Reply:       req.Reply,
			SessionOnly: r.transport != "udp" && (sp.Session != nil || r.transport != "tcp"),
			Retries:     req.RetriesOr(driver.DefaultCommandRetries),
		}
		if r.disruptive {
			hold, err := p.holdSet(ctx, r.targetID, r.nodeName, r.outletParam != "", OutletParam(r.params, r.outletParam))
			if err != nil {
				return nil, err
			}
			d.Disruptive, d.QuietSeconds, d.HoldSeconds, d.HoldEndpoints = true, r.quiet, r.hold, hold
		}
		if driver.IsHTTP(r.transport) {
			// An http command is its own request (#902): no session to ride,
			// no line, the rendered request whole.
			d.Line, d.SessionOnly, d.Retries, d.Request = "", false, 0, &req
		}
		if driver.IsWebSocket(r.transport) {
			// A websocket command is a frame over the held session (#1034):
			// its line, or its JSON body rendered injection-safe; the node
			// mints the correlation into it at execute.
			frame, err := req.Frame()
			if err != nil {
				if uerr := p.recordRenderFault(ctx, r.id, r.endpointID, err); uerr != nil {
					return nil, uerr
				}
				continue
			}
			d.Line = frame
		}
		out = append(out, d)
		delivered = append(delivered, dispatch{r.id, r.endpointID})
	}
	if err := p.markDispatched(ctx, delivered); err != nil {
		return nil, err
	}
	return out, nil
}

// dispatch is one command a pull delivers and the endpoint it rides.
type dispatch struct {
	id         int64
	endpointID string
}

// markDispatched stamps one pull's deliveries (#1626, #1631): dispatched_at
// is when a node last took a command, first_dispatched_at when one first
// did, and delivery_count how many times it has been taken. One statement
// stamps the whole pull, so the stamp's round trips do not grow with the
// commands it delivers. Each first take writes the command's dispatch
// latency (first_dispatched_at - ts) on the taking node's platform
// component, in the same transaction as the stamp it derives from: one
// sample per command, never one per pull, because the delivery loss counts
// them. A redelivery writes no sample.
func (p *PG) markDispatched(ctx context.Context, delivered []dispatch) error {
	if len(delivered) == 0 {
		return nil
	}
	ids := make([]int64, len(delivered))
	endpoints := make([]string, len(delivered))
	for i, d := range delivered {
		ids[i], endpoints[i] = d.id, d.endpointID
	}
	return p.sampleWrite(ctx, "mark commands dispatched", func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			update command c set dispatched_at = now(), first_dispatched_at = coalesce(c.first_dispatched_at, now()),
				delivery_count = c.delivery_count + 1, dispatched_endpoint_id = u.endpoint_id
			from unnest($1::bigint[], $2::uuid[]) as u(id, endpoint_id)
			where c.id = u.id
			returning c.id, c.delivery_count, extract(epoch from c.first_dispatched_at - c.ts) * 1000, c.first_dispatched_at,
				(select n.platform_component_id::text from endpoint i join node n on n.principal_id = i.node_name where i.id = u.endpoint_id)`,
			ids, endpoints)
		if err != nil {
			return fmt.Errorf("storage: mark %d commands dispatched: %w", len(ids), err)
		}
		type firstTake struct {
			id        int64
			latencyMS float64
			at        time.Time
			platform  string
		}
		var firsts []firstTake
		for rows.Next() {
			var (
				f        firstTake
				count    int
				platform *string
			)
			if err := rows.Scan(&f.id, &count, &f.latencyMS, &f.at, &platform); err != nil {
				rows.Close()
				return fmt.Errorf("storage: scan dispatched command: %w", err)
			}
			if count == 1 && platform != nil {
				f.platform = *platform
				firsts = append(firsts, f)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("storage: mark %d commands dispatched: %w", len(ids), err)
		}
		for _, f := range firsts {
			p.recordCommandLatency(ctx, tx, f.id, PlatformMetric{ComponentID: f.platform, Key: writerclass.CommandDispatchLatency,
				Value: f.latencyMS, TS: MetricTime(f.at)})
		}
		return nil
	})
}

// recordCommandLatency writes one per-command latency sample under a
// savepoint of the stamp's transaction: a sample that cannot land (the
// platform component gone, a refusal) is logged and costs only itself, never
// the delivery or the execution stamp it measures.
func (p *PG) recordCommandLatency(ctx context.Context, tx pgx.Tx, id int64, m PlatformMetric) {
	sp, err := tx.Begin(ctx)
	if err == nil {
		err = p.writeMetricSamples(ctx, sp, gatewayWriter(writerclass.Process), []MetricSampleWrite{{OwnerKind: "component",
			OwnerID: m.ComponentID, Key: m.Key, Value: m.Value, TS: m.TS}})
		if err == nil {
			err = sp.Commit(ctx)
		} else {
			_ = sp.Rollback(ctx)
		}
	}
	if err != nil {
		slog.Warn("command latency sample not recorded", "facility", "command", "command", id, "key", m.Key, "error", err.Error())
	}
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
// The reply and the execution error are stored as evidence (#1155), masked of
// the dispatched endpoint's credentials and of anything the text labels a
// credential, then made storable and capped: a component that echoes its
// login never has it stored or served. When the endpoint's secrets cannot be
// read the masking fails closed and both are withheld (endpointMask). The
// judgment below reads the reply whole, as the node sent it.
//
// A fire-and-forget command (one that opened no intended row) is decided by
// the report itself (#1630): settled when the node carried it out, failed
// with the masked execution error when it did not.
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
		latencyMS                                         *float64
		executedAt                                        time.Time
		platform                                          *string
	)
	// The confining stamp first: a report the node had no standing to make
	// is refused before anything about the endpoint (its secrets included)
	// is read, and the mask below is the endpoint this stamp names, never a
	// guess read before it.
	err = tx.QueryRow(ctx, `
		update command c set executed_at = now()
		where c.id = $1
		  and c.executed_at is null
		  and c.dispatched_endpoint_id is not null
		  and exists (
			select 1 from endpoint i
			where i.id = c.dispatched_endpoint_id
			  and i.node_name = (select principal_id from node where name = $2))
		returning c.owner_kind, coalesce(c.component_id, c.system_id, c.location_id, c.node_id)::text,
			c.instance, (select name from command_type where id = c.command_type_id), c.dispatched_endpoint_id::text,
			extract(epoch from c.executed_at - c.first_dispatched_at) * 1000, c.executed_at,
			(select platform_component_id::text from node where name = $2)`,
		commandID, node).Scan(&ownerKind, &arc, &instance, &commandType, &endpointID, &latencyMS, &executedAt, &platform)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCommandNotDispatchedHere
	}
	if err != nil {
		return fmt.Errorf("storage: record execution of command %d: %w", commandID, err)
	}
	mask := p.endpointMask(ctx, p.pool, endpointID)
	storedErr := storedText(mask(execErr))
	if _, err := tx.Exec(ctx, `update command set exec_error = nullif($2, ''), reply = nullif($3, '') where id = $1`,
		commandID, storedErr, storedText(mask(reply))); err != nil {
		return fmt.Errorf("storage: record the report of command %d: %w", commandID, err)
	}
	// Settling is judged under a savepoint: whatever goes wrong reading the
	// reply fails the command, never the execution stamp, so an actuated
	// command is not redelivered.
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: savepoint for command %d settlement: %w", commandID, err)
	}
	serr := func() error {
		ff, err := fireAndForget(ctx, sp, commandID)
		if err != nil {
			return err
		}
		var o *settlementOutcome
		if ff {
			// Nothing was told to a series, so no observation can decide it:
			// the report itself is the outcome (#1630).
			r := reportOutcome(storedErr)
			o = &r
		} else if o, err = p.settleFromResponse(ctx, sp, commandID, ownerKind, arc, instance, commandType, endpointID, storedErr, reply); err != nil || o == nil {
			return err
		}
		o.reason = mask(o.reason)
		now, err := dbNow(ctx, sp)
		if err != nil {
			return err
		}
		return recordOutcome(ctx, sp, commandID, *o, now)
	}()
	if serr != nil {
		_ = sp.Rollback(ctx)
		slog.Warn("command reply could not settle it", "facility", "command", "command", commandID, "error", serr.Error())
		// The fallback records the failure plainly, with no lifecycle event:
		// what failed may be the very write that event needs.
		if _, err := tx.Exec(ctx, `update command set status = 'failed', settled_at = now(), settled_by = $2,
				reason = 'the reply could not be judged' where id = $1 and status = 'issued'`, commandID, SettledByReply); err != nil {
			return fmt.Errorf("storage: record command %d settlement: %w", commandID, err)
		}
	} else if err := sp.Commit(ctx); err != nil {
		return fmt.Errorf("storage: release savepoint for command %d: %w", commandID, err)
	}
	// The execution latency (#1626), first dispatch to report, on the
	// reporting node's platform component, in the stamp's transaction. A
	// command dispatched before its first delivery was kept has none.
	if latencyMS != nil && platform != nil {
		p.recordCommandLatency(ctx, tx, commandID, PlatformMetric{ComponentID: *platform, Key: writerclass.CommandExecutionLatency,
			Value: *latencyMS, TS: executedAt})
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit record execution: %w", err)
	}
	return nil
}

// fireAndForget reports a command that opened no intended row: nothing it
// was told can be observed, so its execution report decides it (#1630).
func fireAndForget(ctx context.Context, q querier, commandID int64) (bool, error) {
	var opens bool
	if err := q.QueryRow(ctx, `select `+commandOpensIntendedSQL+` from command c where c.id = $1`, commandID).Scan(&opens); err != nil {
		return false, fmt.Errorf("storage: intended row of command %d: %w", commandID, err)
	}
	return !opens, nil
}

// reportOutcome is a fire-and-forget command's outcome read off its execution
// report (#1630): carried out cleanly it settles, and an execution error
// (already masked) fails it with that error as the reason. Either way the
// reply decided it.
func reportOutcome(execErr string) settlementOutcome {
	if execErr != "" {
		return settlementOutcome{status: "failed", by: SettledByReply, reason: "the execution failed: " + execErr}
	}
	return settlementOutcome{status: "settled", by: SettledByReply, reason: "the node carried it out"}
}

// recordRenderFault stamps a command whose binding cannot render as executed
// with the render error, on the endpoint it would have gone out on, so it
// stops being redelivered and the fault is visible on the row. A
// fire-and-forget command, which only its report could decide, is failed at
// once with its lifecycle event (#1630); a settleable one is left to its
// settle-check, whose observation still decides it.
func (p *PG) recordRenderFault(ctx context.Context, id int64, endpointID string, fault error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin render fault for command %d: %w", id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `update command set executed_at = now(), exec_error = $2, dispatched_endpoint_id = $3 where id = $1 and executed_at is null`,
		id, fault.Error(), endpointID)
	if err != nil {
		return fmt.Errorf("storage: record render fault for command %d: %w", id, err)
	}
	if tag.RowsAffected() == 1 {
		ff, err := fireAndForget(ctx, tx, id)
		if err != nil {
			return err
		}
		if ff {
			now, err := dbNow(ctx, tx)
			if err != nil {
				return err
			}
			if err := recordOutcome(ctx, tx, id, reportOutcome(storedText(fault.Error())), now); err != nil {
				return fmt.Errorf("storage: fail command %d on its render fault: %w", id, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit render fault for command %d: %w", id, err)
	}
	return nil
}

// settleFromResponse applies the dispatched endpoint's binding to the reply,
// when that binding settles from the response, and returns the outcome it
// reaches; nil when it decides nothing and the command waits on observation.
// execErr is the stored (masked) execution error, which a failure's reason
// carries.
func (p *PG) settleFromResponse(ctx context.Context, tx pgx.Tx, commandID int64, ownerKind, arc, instance, commandType, endpointID, execErr, reply string) (*settlementOutcome, error) {
	var epName, transport string
	var spec []byte
	err := tx.QueryRow(ctx, `select e.name, e.transport, d.spec from endpoint e join driver d on d.id = e.driver_id where e.id = $1`,
		endpointID).Scan(&epName, &transport, &spec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: dispatched endpoint of command %d: %w", commandID, err)
	}
	sp, err := driver.Parse(spec)
	if err != nil {
		return nil, nil // a stored spec is validated at write; nothing to read it by
	}
	var binding *driver.CommandBinding
	for i := range sp.Commands {
		if sp.Commands[i].CommandType == commandType {
			binding = &sp.Commands[i]
		}
	}
	if binding == nil || binding.Settle == nil || binding.Settle.From != driver.SettleFromResponse {
		return nil, nil
	}
	failed := func(reason string) (*settlementOutcome, error) {
		return &settlementOutcome{status: "failed", by: SettledByReply, reason: reason}, nil
	}
	if execErr != "" {
		return failed("the execution failed: " + execErr)
	}
	// A reply that is not readable text says nothing a binding can read.
	if !utf8.ValidString(reply) || strings.ContainsRune(reply, 0) {
		return failed("the reply is not readable text")
	}
	verdict := binding.JudgeReply(reply)
	if !verdict.Succeeded {
		if binding.Settle.Extract != nil {
			return failed("the reply did not carry a value the binding reads")
		}
		return failed("the reply did not confirm it")
	}
	if !verdict.HasValue {
		return &settlementOutcome{status: "settled", by: SettledByReply, reason: "the reply confirmed it"}, nil
	}
	ct, err := scanCommandType(tx.QueryRow(ctx, `select `+commandTypeCols+` from command_type_in_effect() command_type where name = $1`, commandType))
	if err != nil {
		return nil, fmt.Errorf("storage: command type %s: %w", commandType, err)
	}
	series := instance
	if series == "" {
		series = epName
	}
	// The reply is an observation of a property target, recorded where the
	// driver's polls of it land: held to the property's validation like any
	// telemetry (a value outside it fails the command and records nothing),
	// and written only as a transition.
	if ct.TargetPropertyType != "" {
		var dataType string
		var validation []byte
		if err := tx.QueryRow(ctx, `select data_type, validation from property_type_in_effect() property_type where name = $1`, ct.TargetPropertyType).Scan(&dataType, &validation); err != nil {
			return nil, fmt.Errorf("storage: property type %s: %w", ct.TargetPropertyType, err)
		}
		if err := key.ValidateValue(dataType, replyJSON(dataType, verdict.Value), validation); err != nil {
			return failed(fmt.Sprintf("the reply carried %s, which %s does not accept", verdict.Value, ct.TargetPropertyType))
		}
		latest, err := p.latestTargetValue(ctx, tx, ct, ownerKind, arc, series, "observed")
		if err != nil {
			return nil, err
		}
		if latest == nil || !replyMatchesIntent(latest.Value, verdict.Value) {
			now, err := dbNow(ctx, tx)
			if err != nil {
				return nil, err
			}
			if err := writeReplyObservation(ctx, tx, ownerKind, arc, ct.TargetPropertyType, series, verdict.Value, transport, now); err != nil {
				return nil, fmt.Errorf("storage: record command %d reply as an observation: %w", commandID, err)
			}
		}
	}
	intended, err := p.commandIntendedValue(ctx, tx, ct, commandID)
	if err != nil {
		return nil, err
	}
	if intended != nil && !replyMatchesIntent(intended.Value, verdict.Value) {
		return failed(fmt.Sprintf("the reply carried %s, not the intended %s", verdict.Value, valueText(intended.Value)))
	}
	return &settlementOutcome{status: "settled", by: SettledByReply, reason: fmt.Sprintf("the reply carried %s, the intended value", verdict.Value)}, nil
}

// writeReplyObservation records a command's reply, read through its driver
// binding, as an observation of the command's property target. It is held to
// the target's writer class like every observation (ADR-0161): a command's
// reply never writes a key a command may not, whatever its type was stored
// targeting.
func writeReplyObservation(ctx context.Context, tx pgx.Tx, ownerKind, arc, key, instance, value, source string, ts time.Time) error {
	if err := admitWrites(ctx, tx, writeProperty, gatewayWriter(writerclass.CommandReply), []keyedWrite{{key: key, instance: instance}}); err != nil {
		return err
	}
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return err
	}
	sql := fmt.Sprintf(`insert into property (ts, owner_kind, %s, property_type_id, instance, value, provenance, source)
		values ($1, $2, $3, (select id from property_type_in_effect() property_type where name = $4), $5, to_jsonb($6::text), 'observed', $7)`, col)
	_, err = tx.Exec(ctx, sql, ts, ownerKind, arc, key, instance, value, source)
	return err
}

// replyJSON renders an extracted reply value as the JSON its property's data
// type expects, for validation.
func replyJSON(dataType, value string) json.RawMessage {
	switch dataType {
	case "int", "float":
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			return json.RawMessage(value)
		}
	case "bool":
		if value == "true" || value == "false" {
			return json.RawMessage(value)
		}
	}
	b, _ := json.Marshal(value)
	return b
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

// intendedJSON is the intended value's own JSON, which an http body leaf
// that is exactly ${value} takes with its type (#902): a metric's number, a
// property's stored JSON. nil when the command carries none.
func intendedJSON(prop []byte, metric *float64) json.RawMessage {
	if len(prop) > 0 {
		return json.RawMessage(prop)
	}
	if metric != nil {
		return json.RawMessage(strconv.FormatFloat(*metric, 'f', -1, 64))
	}
	return nil
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

// holdSet loads what ResolveHoldSet reads for one disruptive delivery: the
// components reachable from the target over host edges (and, for a power
// command, the power edges from the target), the power and host edges among
// them, and their endpoints placed on the delivering node. The walk is a
// union recursive CTE, so a cycle that got in anyway terminates.
func (p *PG) holdSet(ctx context.Context, target, nodeName string, power bool, outlet string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		with recursive reach(id) as (
			select $1::uuid
			union
			select d.component_id from component_dependency d join reach r on d.upstream_id = r.id
			where d.kind = 'host' or (d.kind = 'power' and $2 and d.upstream_id = $1::uuid)
		)
		select d.component_id::text, d.upstream_id::text, d.kind, coalesce(d.outlet, '')
		from component_dependency d join reach r on d.upstream_id = r.id
		where d.kind in ('host', 'power')`, target, power)
	if err != nil {
		return nil, fmt.Errorf("storage: hold-set edges for %s: %w", target, err)
	}
	in := HoldInput{Target: target, Node: nodeName, Power: power, Outlet: outlet}
	reach := []string{target}
	for rows.Next() {
		var e HoldEdge
		if err := rows.Scan(&e.ComponentID, &e.UpstreamID, &e.Kind, &e.Outlet); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan hold-set edge: %w", err)
		}
		in.Edges = append(in.Edges, e)
		reach = append(reach, e.ComponentID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: hold-set edges for %s: %w", target, err)
	}
	rows, err = p.pool.Query(ctx, `select id::text, component::text, node_name::text, out_of_band from endpoint
		where component = any($1::uuid[]) and node_name = $2`, reach, nodeName)
	if err != nil {
		return nil, fmt.Errorf("storage: hold-set endpoints for %s: %w", target, err)
	}
	defer rows.Close()
	for rows.Next() {
		var e HoldEndpoint
		if err := rows.Scan(&e.ID, &e.ComponentID, &e.Node, &e.OutOfBand); err != nil {
			return nil, fmt.Errorf("storage: scan hold-set endpoint: %w", err)
		}
		in.Endpoints = append(in.Endpoints, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: hold-set endpoints for %s: %w", target, err)
	}
	return ResolveHoldSet(in), nil
}
