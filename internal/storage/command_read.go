package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/redact"
	"github.com/jackc/pgx/v5"
)

// A command's status, read back whole (#1155). The row records the wire
// (dispatched_at, executed_at, exec_error), the verdict (status, settled_at)
// and, since this read, the evidence and the why (reply, reason, settled_by,
// observed_value); what is derived here, never stored, is where an in-flight
// command sits right now (its phase) and how its binding says it settles
// (its settle mode), both functions of the row and the endpoint it rides.

// ErrCommandNotFound is a command that is not on the component it was read
// under: absent, or another component's. The route maps it to a 404, the
// same refusal a component outside the caller's scope gets.
var ErrCommandNotFound = errors.New("storage: command not found")

// Where an in-flight command sits: queued for its node, delivered to it (a
// binding that settles on observation, so the reply decides nothing),
// awaiting the reply that settles it, or awaiting the observation that does.
// A terminal command's phase is its status.
const (
	PhaseQueued              = "queued"
	PhaseDelivered           = "delivered"
	PhaseAwaitingReply       = "awaiting-reply"
	PhaseAwaitingObservation = "awaiting-observation"
)

// How a command's binding says it settles: from the component's reply, from
// the next observation of its target, or not at all (a fire-and-forget
// command sets nothing).
const (
	SettleModeReply       = "reply"
	SettleModeObservation = "observation"
	SettleModeNone        = "none"
)

// What decided a terminal command, recorded in settled_by: its issue (it set
// nothing, so nothing settles it), its execution report, an observation of
// its target, or its settle window closing on no evidence.
const (
	SettledByIssue       = "issue"
	SettledByReply       = "reply"
	SettledByObservation = "observation"
	SettledByWindow      = "window"
)

// The list's page bounds: a component's history is read newest first, a page
// at a time.
const (
	DefaultCommandListLimit = 50
	MaxCommandListLimit     = 500
)

// ReplyCap bounds a stored reply: a reply is evidence, a line or a small
// document, never a page, and it is read back on every command read.
const ReplyCap = 4096

// CommandRecord is one command read whole: what it was told, where it sits,
// what decided it and why, and who issued it.
type CommandRecord struct {
	ID          int64
	ComponentID string
	Component   string
	CommandType string
	Instance    string
	Params      json.RawMessage
	// Intended is the value the command set (its intended row, by the
	// command_id lineage), nil for a fire-and-forget command.
	Intended json.RawMessage
	// TargetKind is property or metric (the command type's target arm),
	// empty for a fire-and-forget command; Target is the key.
	TargetKind          string
	Target              string
	SettleWindowSeconds int
	// Deadline is when the settle window closes: past it, a command with no
	// evidence is timed out by the next sweep.
	Deadline   time.Time
	Status     string
	Phase      string
	SettleMode string
	SettledBy  string
	Reason     string
	ExecError  string
	// Reply is the component's answer, masked of credentials and capped; nil
	// until an execution report lands.
	Reply         *string
	ObservedValue json.RawMessage
	ObservedAt    *time.Time
	IssuedAt      time.Time
	DeliveredAt   *time.Time
	ExecutedAt    *time.Time
	SettledAt     *time.Time
	// Endpoint is the endpoint the command rides: the one a node took it
	// over, else the one a node would (its driver binds the type). Empty
	// when no endpoint of the component can carry it.
	Endpoint       string
	EndpointID     string
	Node           string
	Driver         string
	IssuerID       string
	Issuer         string
	CausedEventID  *int64
	SettledEventID *int64
}

// CommandFilter narrows a component's command history: by recorded status,
// by when the command was issued (inclusive bounds), and a page size.
type CommandFilter struct {
	Status string
	Since  *time.Time
	Until  *time.Time
	Limit  int
}

// CommandPhase is where a command sits: its status once terminal; while
// issued, queued until a node takes it (when one can), then delivered or
// awaiting the reply (by how it settles) until its report lands, then
// awaiting the observation that settles it. A command no endpoint can carry
// is awaiting observation from its issue: no node will take it, and only a
// sample of its target (a push, a poll) can settle it. Pure.
func CommandPhase(status, mode string, delivered, executed, deliverable bool) string {
	if status != "issued" {
		return status
	}
	switch {
	case executed || !deliverable:
		return PhaseAwaitingObservation
	case !delivered:
		return PhaseQueued
	case mode == SettleModeReply:
		return PhaseAwaitingReply
	default:
		return PhaseDelivered
	}
}

// SettleModeOf is how a command settles under its binding: reply when the
// binding settles from the response, else observation for a command that
// sets a value, else none. A nil binding (no driver carries it) settles on
// observation. Pure.
func SettleModeOf(b *driver.CommandBinding, settleable bool) string {
	if !settleable {
		return SettleModeNone
	}
	if b != nil && b.Settle != nil && b.Settle.From == driver.SettleFromResponse {
		return SettleModeReply
	}
	return SettleModeObservation
}

// commandRideSQL is the endpoint a command rides, as a lateral subquery over
// command c and command_type ct: the endpoint a node took it over, else the
// endpoint PendingNodeCommands would pick (the first, by name, of the
// component's endpoints whose driver binds the type). One row or none.
const commandRideSQL = `left join lateral (
		select e.id, e.name, e.node_name, e.driver_id from endpoint e
		where e.id = c.dispatched_endpoint_id
		union all
		select * from (
			select e.id, e.name, e.node_name, e.driver_id from endpoint e join driver dr on dr.id = e.driver_id
			where c.dispatched_endpoint_id is null and c.owner_kind = 'component' and e.component = c.component_id
			  and jsonb_path_exists(dr.spec, '$.commands[*] ? (@.command_type == $ct)', jsonb_build_object('ct', ct.name))
			order by e.name limit 1) candidate
		limit 1
	) ride on true`

// commandReadCols is the read shape, in the order scanCommandRecord reads it.
var commandReadCols = `c.id, c.ts, c.component_id::text, comp.name, ct.name, c.instance, c.params, c.status,
	c.dispatched_at, c.executed_at, c.settled_at, coalesce(c.exec_error, ''), c.reply, coalesce(c.reason, ''),
	coalesce(c.settled_by, ''), c.observed_value, c.observed_at, c.caused_event_id, c.settled_event_id,
	coalesce(c.actor::text, ''), ` + principalIdentCols("c.actor") + `,
	ct.settle_window_seconds, coalesce(pt.name, ''), coalesce(mt.name, ''),
	(select pr.value from property pr where pr.command_id = c.id and pr.provenance = 'intended' order by pr.ts desc, pr.id desc limit 1),
	(select m.value from metric m where m.command_id = c.id and m.provenance = 'intended' order by m.ts desc, m.id desc limit 1),
	coalesce(ride.id::text, ''), coalesce(ride.name, ''),
	coalesce((select n.name from node n where n.principal_id = ride.node_name), ''),
	coalesce(drv.name, ''), drv.spec`

// commandReadFrom joins what the read shape needs.
const commandReadFrom = `from command c
	join command_type ct on ct.id = c.command_type_id
	join component comp on comp.id = c.component_id
	left join property_type pt on pt.id = ct.target_property_type_id
	left join metric_type mt on mt.id = ct.target_metric_type_id
	` + commandRideSQL + `
	left join driver drv on drv.id = ride.driver_id`

// specCache parses each driver spec once per read.
type specCache map[string]*driver.Spec

func (sc specCache) binding(driverName string, raw []byte, commandType string) *driver.CommandBinding {
	if driverName == "" || len(raw) == 0 {
		return nil
	}
	sp, seen := sc[driverName]
	if !seen {
		parsed, err := driver.Parse(raw)
		if err != nil {
			parsed = nil // a stored spec is validated at write; read nothing from corruption
		}
		sc[driverName], sp = parsed, parsed
	}
	if sp == nil {
		return nil
	}
	for i := range sp.Commands {
		if sp.Commands[i].CommandType == commandType {
			return &sp.Commands[i]
		}
	}
	return nil
}

func scanCommandRecord(row pgx.Row, specs specCache) (*CommandRecord, error) {
	var (
		r                           CommandRecord
		username, service, nodeName *string
		intendedProp                []byte
		intendedMetric              *float64
		propTarget, metricTarget    string
		spec                        []byte
	)
	if err := row.Scan(&r.ID, &r.IssuedAt, &r.ComponentID, &r.Component, &r.CommandType, &r.Instance, &r.Params, &r.Status,
		&r.DeliveredAt, &r.ExecutedAt, &r.SettledAt, &r.ExecError, &r.Reply, &r.Reason,
		&r.SettledBy, &r.ObservedValue, &r.ObservedAt, &r.CausedEventID, &r.SettledEventID,
		&r.IssuerID, &username, &service, &nodeName,
		&r.SettleWindowSeconds, &propTarget, &metricTarget, &intendedProp, &intendedMetric,
		&r.EndpointID, &r.Endpoint, &r.Node, &r.Driver, &spec); err != nil {
		return nil, err
	}
	r.Issuer = principalIdent(username, service, nodeName)
	// A row stored before replies were masked carries its execution error as
	// the node reported it; the labeled-pair pass runs on every read, a
	// no-op on text already masked.
	r.ExecError = redact.Keyed(r.ExecError)
	if r.Reply != nil {
		masked := redact.Keyed(*r.Reply)
		r.Reply = &masked
	}
	switch {
	case propTarget != "":
		r.TargetKind, r.Target = "property", propTarget
	case metricTarget != "":
		r.TargetKind, r.Target = "metric", metricTarget
	}
	switch {
	case len(intendedProp) > 0 && string(intendedProp) != "null":
		r.Intended = copyRaw(intendedProp)
	case intendedMetric != nil:
		r.Intended = json.RawMessage(strconv.FormatFloat(*intendedMetric, 'g', -1, 64))
	}
	r.Deadline = r.IssuedAt.Add(time.Duration(r.SettleWindowSeconds) * time.Second)
	r.SettleMode = SettleModeOf(specs.binding(r.Driver, spec, r.CommandType), r.TargetKind != "" && r.Intended != nil)
	r.Phase = CommandPhase(r.Status, r.SettleMode, r.DeliveredAt != nil, r.ExecutedAt != nil, r.EndpointID != "")
	return &r, nil
}

// GetComponentCommand reads one command on a component, whole.
//
// componentID is RESOLVED, and this takes no scope, on the same contract as
// ListAlarms: the route resolves the component through the caller's read
// sets (the 404 versus 403 split) and binds it by id, and the command is
// then read only under that component, so a command id that belongs to
// another component is ErrCommandNotFound, the refusal an out-of-scope
// component gets. A command id is never a way around the component's scope.
func (p *PG) GetComponentCommand(ctx context.Context, componentID string, id int64) (*CommandRecord, error) {
	row := p.pool.QueryRow(ctx, `select `+commandReadCols+` `+commandReadFrom+`
		where c.owner_kind = 'component' and c.component_id = $1 and c.id = $2`, componentID, id)
	rec, err := scanCommandRecord(row, specCache{})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCommandNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: read command %d: %w", id, err)
	}
	return rec, nil
}

// ListComponentCommands reads a component's command history, newest first,
// narrowed by f. componentID is RESOLVED, as for GetComponentCommand. A
// limit outside 1..MaxCommandListLimit reads DefaultCommandListLimit or the
// ceiling.
func (p *PG) ListComponentCommands(ctx context.Context, componentID string, f CommandFilter) ([]CommandRecord, error) {
	limit := f.Limit
	switch {
	case limit <= 0:
		limit = DefaultCommandListLimit
	case limit > MaxCommandListLimit:
		limit = MaxCommandListLimit
	}
	rows, err := p.pool.Query(ctx, `select `+commandReadCols+` `+commandReadFrom+`
		where c.owner_kind = 'component' and c.component_id = $1
		  and ($2 = '' or c.status = $2)
		  and ($3::timestamptz is null or c.ts >= $3)
		  and ($4::timestamptz is null or c.ts <= $4)
		order by c.ts desc, c.id desc
		limit $5`, componentID, f.Status, f.Since, f.Until, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list commands: %w", err)
	}
	defer rows.Close()
	specs := specCache{}
	out := []CommandRecord{}
	for rows.Next() {
		rec, err := scanCommandRecord(rows, specs)
		if err != nil {
			return nil, fmt.Errorf("storage: scan command: %w", err)
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

// storedText makes a component's text storable and servable as evidence:
// valid UTF-8 with no NUL (Postgres text holds neither), and cut at ReplyCap
// on a rune boundary, marked. Masking runs before this, on the whole text,
// so the cut can never split a credential out of a form the mask knows.
func storedText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\x00", "�")
	if len(s) <= ReplyCap {
		return s
	}
	cut := ReplyCap
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + " [truncated]"
}

// Withheld is what a component's text reads as when the server could not
// read the endpoint's secrets to mask it.
const Withheld = "[withheld: the endpoint's credentials could not be read to mask it]"

// endpointMask is the masker for what came back over an endpoint: every
// secret field the endpoint's driver inputs name (and a basic login's
// authorization value), in every form redact knows, then anything the text
// labels a credential. It fails closed: when the secrets cannot be read (no
// key provider, an envelope that will not open, a driver detached since the
// dispatch), the node that sent the text may still hold them, so every
// non-empty text is withheld rather than stored half-masked. A secret input
// that names nothing the component owns was delivered to the node as
// absence, so there is nothing of it to mask.
func (p *PG) endpointMask(ctx context.Context, q querier, endpointID string) func(string) string {
	secrets, err := p.endpointSecretValues(ctx, q, endpointID)
	if err != nil {
		slog.Warn("a command's reply is withheld: its endpoint's credentials could not be read to mask it",
			"facility", "command", "endpoint", endpointID, "error", err.Error())
		return func(s string) string {
			if s == "" {
				return s
			}
			return Withheld
		}
	}
	return func(s string) string { return redact.Text(s, secrets...) }
}

// endpointSecretValues unseals the secret fields an endpoint authenticates
// with, through the same cascade-confined lookup its node's worklist uses.
// An error is a secret that may exist and could not be read.
func (p *PG) endpointSecretValues(ctx context.Context, q querier, endpointID string) ([]string, error) {
	var spec, inputs []byte
	var componentID *string
	if err := q.QueryRow(ctx, `select d.spec, e.inputs, e.component::text from endpoint e join driver d on d.id = e.driver_id where e.id = $1`,
		endpointID).Scan(&spec, &inputs, &componentID); err != nil {
		return nil, fmt.Errorf("storage: the driver of endpoint %s: %w", endpointID, err)
	}
	refs, err := secretRefsOf(spec, inputs)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}
	if componentID == nil {
		return nil, fmt.Errorf("storage: endpoint %s names secrets but has no component", endpointID)
	}
	var out []string
	for _, ref := range refs {
		fields, _, shape, err := p.nodeSecretShaped(ctx, ref, *componentID)
		var amb *ErrAmbiguousName
		if errors.Is(err, ErrSecretNotFound) || errors.As(err, &amb) {
			continue // delivered to the node as absence: nothing of it to mask
		}
		if err != nil {
			return nil, err
		}
		for _, f := range shape.Fields {
			if f.Secret {
				out = append(out, fields[f.Name])
			}
		}
		out = append(out, redact.BasicAuth(fields["username"], fields["password"]))
	}
	return out, nil
}
