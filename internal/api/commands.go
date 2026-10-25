package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The command surface: the "do" primitive's write and its reads. Issuing a
// command records the invocation, writes a caused event, and (for a
// settleable command) opens an intended value in the property cache that the
// target's observed value settles against. It is an AIP custom method
// (:issue), gated by command:issue and scope-injected through the component.
//
// The fence is the ISSUE scope, not the read scope (#749). This route actuates a
// physical device, so the set that decides which components it reaches has to come
// from the permission that authorizes the actuation: a principal holding a wide
// component read beside a room-scoped command grant may command that room and no
// more. The read scope is still passed, and still grants nothing: it decides only
// which refusal the caller is owed (ADR-0116).
//
// The reads (#1155) carry a command whole, from issue to settlement: one by its
// id under its component, and a component's history. They are gated by
// command:read and resolve the component the same split way, the caller's
// component:read for the refusal it is owed and command:read for what it may
// see, on the component tier (a command hangs off a component, as an alarm
// does). A command id is only ever read under the component it was resolved
// through, so an id never reaches past the component's scope.

type issueCommandInput struct {
	Name        string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	Wait        bool   `query:"wait" doc:"Block until the command settles, fails or times out, or until wait_timeout passes, and answer with where it stands then. Refused with a 429, before anything is issued, while too many commands are already being waited on"`
	WaitTimeout int    `query:"wait_timeout" minimum:"1" maximum:"120" default:"30" doc:"How long a wait may block, in seconds (1 to 120). A command still in flight when it passes is answered as it stands, its phase saying where"`
	Body        struct {
		CommandType string          `json:"command_type" minLength:"1" doc:"The command_type to invoke"`
		Instance    string          `json:"instance,omitempty" doc:"The series discriminator (e.g. an endpoint), when the target is instanced"`
		Value       json.RawMessage `json:"value,omitempty" doc:"The intended value for the target property (a settleable command)"`
		Params      json.RawMessage `json:"params,omitempty" doc:"The invocation params, stored on the command and the caused event"`
	}
}

// commandBody is one command read whole.
type commandBody struct {
	ID                  int64               `json:"id" doc:"The command's id: with its component, its address (a command has no name)"`
	Component           string              `json:"component" doc:"The component it was issued to, by name"`
	ComponentID         string              `json:"component_id" doc:"The component's uuid, the stable form of component"`
	CommandType         string              `json:"command_type" doc:"What the component was told to do"`
	Instance            string              `json:"instance,omitempty" doc:"The series discriminator it was issued with, if any"`
	Params              json.RawMessage     `json:"params,omitempty" doc:"The invocation params"`
	Intended            json.RawMessage     `json:"intended,omitempty" doc:"The value it set; absent for a fire-and-forget command"`
	TargetKind          string              `json:"target_kind,omitempty" enum:"property,metric" doc:"The lane of the value it sets; absent for a fire-and-forget command"`
	Target              string              `json:"target,omitempty" doc:"The property or metric it sets, by name"`
	Status              string              `json:"status" enum:"issued,settled,failed,timed-out" doc:"The recorded status: issued until a terminal settled, failed, or timed-out"`
	Phase               string              `json:"phase" enum:"queued,delivered,awaiting-reply,awaiting-observation,settled,failed,timed-out" doc:"Where it sits now: queued for its node; delivered to it (a binding that settles on observation); awaiting the reply that settles it; awaiting the observation that does, within its window; or, once terminal, its status"`
	SettleMode          string              `json:"settle_mode" enum:"reply,observation,none" doc:"How its driver binding says it settles: from the component's reply, from the next observation of its target, or not at all (it sets nothing)"`
	SettleWindowSeconds int                 `json:"settle_window_seconds" doc:"How long it waits for its evidence"`
	Deadline            time.Time           `json:"deadline" doc:"When its settle window closes; a command still issued past it is timed out by the next sweep"`
	SettledBy           string              `json:"settled_by,omitempty" enum:"issue,reply,observation,window" doc:"What decided it, once terminal: its issue (it sets nothing), its reply, an observation of its target, or its window closing on no evidence"`
	Reason              string              `json:"reason,omitempty" doc:"Why it reached its terminal status, in words: the reply that confirmed it, the value observed instead, where it stopped when it timed out"`
	ExecError           string              `json:"exec_error,omitempty" doc:"The execution's failure, as its node reported it, masked of credentials"`
	Reply               *string             `json:"reply,omitempty" doc:"The component's answer, as its execution report carried it, masked of the endpoint's credentials and anything labeled a credential, and capped; absent until a report lands"`
	Observed            *commandObservation `json:"observed,omitempty" doc:"The sample of its target that decided it, when an observation did"`
	IssuedAt            time.Time           `json:"issued_at" doc:"When it was issued"`
	DeliveredAt         *time.Time          `json:"delivered_at,omitempty" doc:"When a node last took it from its queue"`
	ExecutedAt          *time.Time          `json:"executed_at,omitempty" doc:"When its node's execution report landed"`
	SettledAt           *time.Time          `json:"settled_at,omitempty" doc:"When it reached its terminal status"`
	Endpoint            string              `json:"endpoint,omitempty" doc:"The endpoint it rides, by name: the one a node took it over, else the one a node would (its driver binds the type); absent when no endpoint can carry it"`
	EndpointID          string              `json:"endpoint_id,omitempty" doc:"The endpoint's uuid"`
	Node                string              `json:"node,omitempty" doc:"The node that endpoint is placed on, by name"`
	Driver              string              `json:"driver,omitempty" doc:"The driver whose binding renders and settles it, by name"`
	Issuer              string              `json:"issuer,omitempty" doc:"Who issued it, by name; empty once that principal is purged (the audit log keeps the name)"`
	IssuerID            string              `json:"issuer_id,omitempty" doc:"The issuer's uuid, the actor on the command's audit row"`
	CausedEventID       *int64              `json:"caused_event_id,omitempty" doc:"The command-issued event it put on the component's timeline"`
	SettledEventID      *int64              `json:"settled_event_id,omitempty" doc:"The event its terminal status put there: command-settled, command-failed or command-timed-out"`
	Settlement          string              `json:"settlement,omitempty" enum:"none,pending,settled,failed" doc:"On an issue's answer only: the computed settlement verdict at that moment (none/pending/settled/failed)"`
}

// commandObservation is the sample that decided a command.
type commandObservation struct {
	Value json.RawMessage `json:"value" doc:"The observed value"`
	At    time.Time       `json:"at" doc:"When it was observed"`
}

// toCommandBody renders a record.
func toCommandBody(r *storage.CommandRecord) commandBody {
	b := commandBody{
		ID: r.ID, Component: r.Component, ComponentID: r.ComponentID, CommandType: r.CommandType, Instance: r.Instance,
		Params: r.Params, Intended: r.Intended, TargetKind: r.TargetKind, Target: r.Target,
		Status: r.Status, Phase: r.Phase, SettleMode: r.SettleMode, SettleWindowSeconds: r.SettleWindowSeconds, Deadline: r.Deadline,
		SettledBy: r.SettledBy, Reason: r.Reason, ExecError: r.ExecError, Reply: r.Reply,
		IssuedAt: r.IssuedAt, DeliveredAt: r.DeliveredAt, ExecutedAt: r.ExecutedAt, SettledAt: r.SettledAt,
		Endpoint: r.Endpoint, EndpointID: r.EndpointID, Node: r.Node, Driver: r.Driver,
		Issuer: r.Issuer, IssuerID: r.IssuerID, CausedEventID: r.CausedEventID, SettledEventID: r.SettledEventID,
	}
	if r.ObservedAt != nil && len(r.ObservedValue) > 0 {
		b.Observed = &commandObservation{Value: r.ObservedValue, At: *r.ObservedAt}
	}
	return b
}

type commandPathInput struct {
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)" example:"hq.west.level-2.boardroom-a.$comp.videobar-1"`
	ID   int64  `path:"id" doc:"The command's id, as its issue answered it" example:"42"`
}

type commandOutput struct {
	Body commandBody
}

type listCommandsInput struct {
	Name   string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)" example:"hq.west.level-2.boardroom-a.$comp.videobar-1"`
	Status string `query:"status" enum:"issued,settled,failed,timed-out" doc:"Only the commands with this recorded status; issued is every command still in flight"`
	Since  string `query:"since" doc:"Only the commands issued at or after this RFC3339 time"`
	Until  string `query:"until" doc:"Only the commands issued at or before this RFC3339 time"`
	Limit  int    `query:"limit" minimum:"1" maximum:"500" default:"50" doc:"How many to return, newest first (1 to 500)"`
}

type listCommandsOutput struct {
	Body struct {
		Component string        `json:"component" doc:"The component, by name"`
		Commands  []commandBody `json:"commands" doc:"Its commands, newest first"`
	}
}

// Waiting on an issue holds a request open, so it is bounded: in time (the
// wait_timeout ceiling), in how many waits run at once across the server,
// and in how many one principal holds, so one caller cannot take every slot.
const (
	defaultCommandWaiters             = 64
	defaultCommandWaitersPerPrincipal = 4
	// The poll behind a wait starts quick (a reply settles a command within
	// a node's cycle) and backs off to once a second.
	commandWaitFirstPoll = 100 * time.Millisecond
	commandWaitMaxPoll   = time.Second
)

// waitLimiter counts the waits in flight, in all and per principal.
type waitLimiter struct {
	mu          sync.Mutex
	total, per  int
	inFlight    int
	byPrincipal map[string]int
}

func newWaitLimiter(total, per int) *waitLimiter {
	if total <= 0 {
		total = defaultCommandWaiters
	}
	if per <= 0 {
		per = defaultCommandWaitersPerPrincipal
	}
	return &waitLimiter{total: total, per: per, byPrincipal: map[string]int{}}
}

// acquire takes a slot for principal, or reports none is free.
func (l *waitLimiter) acquire(principal string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight >= l.total || l.byPrincipal[principal] >= l.per {
		return false
	}
	l.inFlight++
	l.byPrincipal[principal]++
	return true
}

func (l *waitLimiter) release(principal string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inFlight--
	if l.byPrincipal[principal]--; l.byPrincipal[principal] <= 0 {
		delete(l.byPrincipal, principal)
	}
}

// registerCommandRoutes wires the command issue custom method and the
// command reads.
func registerCommandRoutes(api huma.API, a *authenticator, gw storage.Gateway, waits *waitLimiter) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "issue-component-command",
		Extensions:  mcpExposed(tooldef.Hints{Destructive: true, OpenWorld: true}),
		Method:      http.MethodPost,
		Path:        "/components/{name}/commands:issue",
		Summary:     "Issue a command to a component",
		Description: "Records a command invocation, writes a caused event, and (for a settleable command) opens an intended value the observed value settles against. Answers with the command as it stands (as the command read returns it) and the computed settlement verdict; with wait, once it settles, fails or times out, or wait_timeout passes. Gated by command:issue, whose scope is resolved on the component tier from that permission (not from component:read); a component outside the caller's component:read is a non-disclosing 404, and one it can read but not command is a 403.",
	}, "command", "issue"), func(ctx context.Context, in *issueCommandInput) (*commandOutput, error) {
		// The ACTION scope comes from command:issue, so only the grants whose
		// role actually carries the actuation contribute their scope. The READ
		// scope is the caller's own component:read and decides only which
		// refusal that principal is owed (#749, ADR-0116): a component outside
		// it is the non-disclosing 404, and one inside it that the issue scope
		// does not reach is the truthful 403. It grants nothing.
		//
		// Resolved ONCE, here, and bound by id from this point on: the gateway
		// calls below take that id rather than the caller's raw reference, so
		// there is no second name resolve to disagree with the first (or to
		// raise an ambiguity the first already settled).
		compID, err := gw.ResolveActionTarget(ctx, "component", in.Name,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "command", "issue"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		// The answer is the command's status, evidence included, which is a
		// read: an issuer holds it by the :read floor (any permission on a
		// resource confers its read, on the same grants), so command:issue
		// here is command:read on this component as well.
		if in.Wait {
			// The slot is taken before anything is issued, so a refused wait
			// leaves nothing behind to retry around.
			principal := actorID(ctx)
			if !waits.acquire(principal) {
				return nil, huma.NewError(http.StatusTooManyRequests, "too many commands are being waited on: issue without wait and read the command back, or retry")
			}
			defer waits.release(principal)
		}
		cmd, err := gw.IssueCommand(ctx, actorID(ctx), "component", compID, in.Body.CommandType, in.Body.Instance,
			in.Body.Value, in.Body.Params)
		if err != nil {
			return nil, mapCommandErr(err)
		}
		verdict, err := gw.CommandSettlement(ctx, "component", compID, in.Body.CommandType, in.Body.Instance)
		if err != nil {
			return nil, mapCommandErr(err)
		}
		rec, err := gw.GetComponentCommand(ctx, compID, cmd.ID)
		if err != nil {
			return nil, mapCommandErr(err)
		}
		if in.Wait {
			if rec, verdict, err = waitForOutcome(ctx, gw, compID, rec, verdict, time.Duration(in.WaitTimeout)*time.Second); err != nil {
				return nil, mapCommandErr(err)
			}
		}
		out := &commandOutput{Body: toCommandBody(rec)}
		out.Body.Settlement = string(verdict)
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-component-command",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/commands/{id}",
		Summary:     "Read a command",
		Description: "One command, whole: what it was told and the value it set, its recorded status with a time for each transition (issued, delivered, executed, settled), where it sits now while in flight (queued, delivered, awaiting the reply, awaiting the observation), how its binding settles it, the evidence that decided it (the reply, masked of credentials, or the observed sample) and why, the endpoint, node and driver that carry it, and who issued it. Gated by command:read, resolved on the component tier; a component outside the caller's component:read, or a command not on that component, is a non-disclosing 404, and a component it can read but whose commands it may not is a 403.",
	}, "command", "read"), func(ctx context.Context, in *commandPathInput) (*commandOutput, error) {
		compID, err := resolveCommandReader(ctx, a, gw, in.Name)
		if err != nil {
			return nil, err
		}
		rec, err := gw.GetComponentCommand(ctx, compID, in.ID)
		if err != nil {
			return nil, mapCommandErr(err)
		}
		return &commandOutput{Body: toCommandBody(rec)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-commands",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/commands",
		Summary:     "List a component's commands",
		Description: "The component's command history, newest first, each command whole (as the single read returns it), narrowed by recorded status and by when it was issued, a page at a time. status=issued is what is in flight now. Gated by command:read, resolved on the component tier; a component outside the caller's component:read is a non-disclosing 404, and one it can read but whose commands it may not is a 403.",
	}, "command", "read"), func(ctx context.Context, in *listCommandsInput) (*listCommandsOutput, error) {
		f := storage.CommandFilter{Status: in.Status, Limit: in.Limit}
		var err error
		if f.Since, err = parseCommandTime("since", in.Since); err != nil {
			return nil, err
		}
		if f.Until, err = parseCommandTime("until", in.Until); err != nil {
			return nil, err
		}
		compID, err := resolveCommandReader(ctx, a, gw, in.Name)
		if err != nil {
			return nil, err
		}
		recs, err := gw.ListComponentCommands(ctx, compID, f)
		if err != nil {
			return nil, mapCommandErr(err)
		}
		comp, err := gw.GetComponent(ctx, compID, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		out := &listCommandsOutput{}
		out.Body.Component = comp.Name
		out.Body.Commands = make([]commandBody, 0, len(recs))
		for i := range recs {
			out.Body.Commands = append(out.Body.Commands, toCommandBody(&recs[i]))
		}
		return out, nil
	})
}

// resolveCommandReader resolves the component a command read hangs off: the
// caller's component:read decides the refusal it is owed (outside it, the
// non-disclosing 404), and its command:read, on the component tier, whether
// it may see the component's commands (inside the read but outside that, the
// truthful 403).
func resolveCommandReader(ctx context.Context, a *authenticator, gw storage.Gateway, ref string) (string, error) {
	id, err := gw.ResolveActionTarget(ctx, "component", ref, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "command", "read"))
	if err != nil {
		return "", mapComponentErr(err)
	}
	return id, nil
}

// parseCommandTime reads an optional RFC3339 bound, refusing a malformed one
// with a 422 that names the parameter.
func parseCommandTime(name, v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(name + " must be an RFC3339 time, such as 2026-10-25T09:00:00Z")
	}
	return &t, nil
}

// waitForOutcome polls a command until it is terminal or timeout passes,
// backing off from a quick first look to once a second. Once the command's
// deadline has passed it judges it then and there (the settle-check a read
// runs), so a wait answers timed out at the window rather than at the next
// sweep. A caller that goes away ends the wait.
func waitForOutcome(ctx context.Context, gw storage.Gateway, compID string, rec *storage.CommandRecord, verdict storage.SettlementVerdict, timeout time.Duration) (*storage.CommandRecord, storage.SettlementVerdict, error) {
	deadline := time.Now().Add(timeout)
	poll := commandWaitFirstPoll
	for rec.Status == "issued" {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		t := time.NewTimer(min(poll, remaining))
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, "", ctx.Err()
		case <-t.C:
		}
		poll = min(poll*3/2, commandWaitMaxPoll)
		if !time.Now().Before(rec.Deadline) {
			v, err := gw.CommandSettlement(ctx, "component", compID, rec.CommandType, rec.Instance)
			if err != nil {
				return nil, "", err
			}
			verdict = v
		}
		next, err := gw.GetComponentCommand(ctx, compID, rec.ID)
		if err != nil {
			return nil, "", err
		}
		rec = next
	}
	return rec, verdict, nil
}

// mapCommandErr maps the command sentinels: a request that names a command_type that
// does not exist (or an invalid one) is a 422, as is a non-numeric intended value for
// a metric target or a params payload that violates the type's params_schema; a
// command not on the component it was read under is a 404; an out-of-scope
// component falls through to the component mapping (a non-disclosing 404).
func mapCommandErr(err error) error {
	if refused, ok := mapKeyWriterErr(err); ok {
		return refused
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrCommandTypeNotFound), errors.Is(err, storage.ErrCommandTypeInvalid):
		return huma.Error422UnprocessableEntity("unknown or invalid command_type")
	case errors.Is(err, storage.ErrCommandValueNotNumeric):
		return huma.Error422UnprocessableEntity("intended value for a metric target must be numeric")
	// The params refusal keeps the storage detail: the 422 names the violated
	// constraint, not just the fact of violation.
	case errors.Is(err, storage.ErrCommandParamsInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, storage.ErrCommandNotFound):
		return huma.Error404NotFound("command not found")
	default:
		return mapComponentErr(err)
	}
}
