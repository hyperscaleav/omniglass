package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// The storage half of the on-demand node run (#1163, ADR-1163): checking a
// spec the way a save does without saving it, planning a preview (what a
// node is sent to run a spec's polls once against a target, its secrets
// unsealed), and resolving an attached endpoint's poll for a run now. The
// node runs the work; these decide what it may be sent, and audit the asking.

var (
	// ErrPreviewUnsupported marks a preview of a poll no node can run without
	// an endpoint: one that rides a held session (a login session, a line
	// protocol over telnet, ssh, tls or a websocket) or a bound udp socket.
	ErrPreviewUnsupported = errors.New("storage: preview unsupported")
	// ErrNoSuchPoll marks a run of a poll the endpoint does not derive.
	ErrNoSuchPoll = errors.New("storage: no such poll")
	// ErrEndpointUnplaced marks a run of an endpoint no node holds.
	ErrEndpointUnplaced = errors.New("storage: endpoint unplaced")
)

// SpecFindingsError is a spec refused with its findings: every fault the
// validator can pin down, the first the one a save names. It unwraps to
// ErrSpecInvalid, so a caller that only asks whether the spec was refused
// still can.
type SpecFindingsError struct{ Findings []driver.Finding }

func (e *SpecFindingsError) Error() string {
	if len(e.Findings) == 0 {
		return ErrSpecInvalid.Error()
	}
	return ErrSpecInvalid.Error() + ": " + e.Findings[0].Message
}

func (e *SpecFindingsError) Unwrap() error { return ErrSpecInvalid }

// DriverSpecFindings checks a spec by the write-time rules (the strict parse,
// then validation against the live catalogs) and writes nothing: no findings
// is a spec a save would accept.
func (p *PG) DriverSpecFindings(ctx context.Context, raw []byte) ([]driver.Finding, error) {
	cat, err := loadDriverCatalog(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	return driver.Findings(raw, cat), nil
}

// PreviewInput is a preview asked for: the node to run it, the spec (a stored
// driver by reference, or a spec inline), the inputs an attach would take,
// and the polls to run (none is every poll). The scopes are the caller's:
// node read, and secret read and bind, since a secret a preview uses leaves
// for a target the caller names, as an attach's does (#2047).
type PreviewInput struct {
	Node     string
	Driver   string
	Spec     []byte
	Inputs   map[string]string
	Polls    []string
	NodeRead scope.Set
	Secrets  SecretUse
}

// PreviewPlan is what a node is sent to run a preview: a task per poll, as a
// worklist would deliver it, with no endpoint. Sealed lists the plaintext of
// every secret field the tasks carry, which the answer is masked of.
type PreviewPlan struct {
	NodeID   string
	NodeName string
	Driver   string
	Target   string
	Polls    []string
	Tasks    []WorklistTask
	Sealed   []string
}

// previewEndpointName names the endpoint a preview task stands in for: none.
const previewEndpointName = "preview"

// previewableTransports are the transports whose polls a node runs without an
// endpoint: a request of its own, answered by itself (http, https, an snmp
// get, a tcp line on a throwaway connection). Every other transport's poll
// borrows a held session or a bound socket that only an endpoint has.
var previewableTransports = []string{"http", "https", "snmp", "tcp"}

// PlanDriverPreview turns a preview asked for into the tasks a node runs: the
// spec checked as a save would (refused with its findings), the polls chosen,
// the inputs resolved as an attach would (defaults baked, the dial target
// derived), and each secret input unsealed, but only a secret the caller may
// reveal. The asking is audited in one transaction: a preview row on the node,
// and a row on each secret it hands to the target.
func (p *PG) PlanDriverPreview(ctx context.Context, actorID string, in PreviewInput) (*PreviewPlan, error) {
	if p.secret == nil {
		return nil, ErrNoSecretProvider
	}
	n, err := p.GetNode(ctx, in.Node, in.NodeRead)
	if err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin preview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d := &Driver{Name: previewEndpointName}
	raw := in.Spec
	if in.Driver != "" {
		if d, err = loadDriverRef(ctx, tx, in.Driver); err != nil {
			return nil, err
		}
		if len(d.Spec) == 0 {
			return nil, fmt.Errorf("%w: driver %q is a stub with no spec, so it has no polls to preview", ErrAttachInvalid, d.Name)
		}
		raw = d.Spec
	}
	cat, err := loadDriverCatalog(ctx, tx)
	if err != nil {
		return nil, err
	}
	if findings := driver.Findings(raw, cat); len(findings) > 0 {
		return nil, &SpecFindingsError{Findings: findings}
	}
	sp, err := driver.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSpecInvalid, err)
	}
	if !slices.Contains(previewableTransports, sp.Transport) || (sp.Transport == "tcp" && sp.Session != nil) {
		return nil, fmt.Errorf("%w: a preview runs a poll that is its own request (over %s); a %s poll rides a held session or a bound socket that only an attached endpoint has, so attach it and run its poll now instead",
			ErrPreviewUnsupported, strings.Join(previewableTransports, ", "), sp.Transport)
	}
	polls, err := choosePolls(sp, in.Polls)
	if err != nil {
		return nil, err
	}

	effective, err := p.resolveInputs(ctx, tx, d, sp, in.Inputs, in.Secrets)
	if err != nil {
		return nil, err
	}
	params := attachParams(effective)
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("storage: marshal preview params: %w", err)
	}
	plan := &PreviewPlan{NodeID: n.PrincipalID, NodeName: n.Name, Target: params["target"]}
	if in.Driver != "" {
		plan.Driver = d.Name
	}
	secrets := map[string]map[string]string{}
	var secretNames []string
	for _, input := range sp.Inputs {
		ref, ok := effective[input.Name]
		if input.Kind != "secret" || !ok {
			continue
		}
		// Resolved under the same read and bind scopes the input check above
		// accepted it under, so the row unsealed is the row it accepted.
		row, shape, err := p.secretRowForAction(ctx, tx, ref, in.Secrets.Read, in.Secrets.Bind, in.Secrets.CanAdmin, true)
		if err != nil {
			return nil, fmt.Errorf("%w: input %q references a secret you may not bind, and a preview hands it to the target you name: %v", ErrAttachInvalid, input.Name, err)
		}
		fields, sealed, err := p.unsealSecretFields(ctx, row, shape)
		if err != nil {
			return nil, err
		}
		secrets[input.Name] = fields
		plan.Sealed = append(plan.Sealed, sealed...)
		secretNames = append(secretNames, row.name)
		secretID := row.id
		if err := writeAuditRes(ctx, tx, actorID, "preview", "secret", secretID, nil,
			map[string]any{"node": n.Name, "target": plan.Target, "input": input.Name}); err != nil {
			return nil, err
		}
	}
	for _, poll := range polls {
		emits, err := bakeEmits(poll.Emits, cat)
		if err != nil {
			return nil, err
		}
		if err := bakeIdentity(poll.Identity, cat); err != nil {
			return nil, err
		}
		sched, req := poll.Schedule, poll.Request
		baked, err := json.Marshal(driver.BakedFunction{Driver: d.Name, Version: d.Version, Function: poll.Name, Schedule: &sched, Request: &req, Emits: emits,
			Records: poll.Records, Identity: poll.Identity})
		if err != nil {
			return nil, fmt.Errorf("storage: marshal preview task: %w", err)
		}
		task := WorklistTask{
			ID: taskID(previewEndpointName, "poll", baked), Mode: "poll", EndpointName: previewEndpointName,
			Transport: sp.Transport, EndpointParams: paramsJSON, Spec: baked, Session: sp.Session,
		}
		if len(secrets) > 0 {
			task.Secrets = secrets
		}
		plan.Tasks = append(plan.Tasks, task)
		plan.Polls = append(plan.Polls, poll.Name)
	}
	nodeID := n.PrincipalID
	if err := writeAuditRes(ctx, tx, actorID, "preview", "node", nodeID, nil, map[string]any{
		"driver": plan.Driver, "transport": sp.Transport, "target": plan.Target, "polls": plan.Polls, "secrets": secretNames,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit preview: %w", err)
	}
	return plan, nil
}

// choosePolls picks the polls a preview runs: those named, else every poll.
func choosePolls(sp *driver.Spec, names []string) ([]driver.Poll, error) {
	if len(sp.Polls) == 0 {
		return nil, fmt.Errorf("%w: the spec declares no poll, so there is nothing to preview", ErrNoSuchPoll)
	}
	if len(names) == 0 {
		return sp.Polls, nil
	}
	byName := map[string]driver.Poll{}
	var all []string
	for _, p := range sp.Polls {
		byName[p.Name] = p
		all = append(all, p.Name)
	}
	var out []driver.Poll
	for _, name := range names {
		p, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%w: the spec has no poll named %q; its polls are %s", ErrNoSuchPoll, name, strings.Join(all, ", "))
		}
		out = append(out, p)
	}
	return out, nil
}

// EndpointPoll is an attached endpoint's poll, resolved for a run now: the
// endpoint, the node that holds it, and the poll's task. Sealed lists the
// plaintext of every secret field the endpoint's worklist carries, which the
// answer is masked of (the device may echo a credential back).
type EndpointPoll struct {
	EndpointID string
	Endpoint   string
	Component  string
	NodeName   string
	TaskID     string
	Poll       string
	Sealed     []string
}

// PlanEndpointRun resolves the poll an operator asked to run now: the
// endpoint under the caller's read and run scopes (outside the read scope a
// non-disclosing not-found, inside it but outside the run scope a refusal),
// the node that holds it, and the enabled poll task its driver derives for the
// named poll. The asking is audited on the endpoint.
func (p *PG) PlanEndpointRun(ctx context.Context, actorID, ref, poll string, read, action scope.Set) (*EndpointPoll, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin endpoint run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ep, err := resolveEndpointScoped(ctx, tx, ref, read, action)
	if err != nil {
		return nil, err
	}
	if ep.DriverID == nil {
		return nil, fmt.Errorf("%w: endpoint %q has no driver attached, so it has no polls (its reachability probe runs on its own)", ErrNoSuchPoll, ep.Name)
	}
	if ep.Node == nil {
		return nil, fmt.Errorf("%w: endpoint %q is placed on no node, so no node can run its poll", ErrEndpointUnplaced, ep.Name)
	}
	rows, err := tx.Query(ctx, `select id, coalesce(spec->>'function', '') from task where endpoint_id = $1 and mode = 'poll' and enabled order by 2`, ep.ID)
	if err != nil {
		return nil, fmt.Errorf("storage: endpoint %q polls: %w", ep.Name, err)
	}
	var taskID string
	var names []string
	for rows.Next() {
		var id, fn string
		if err := rows.Scan(&id, &fn); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan endpoint poll: %w", err)
		}
		names = append(names, fn)
		if fn == poll {
			taskID = id
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: endpoint %q polls: %w", ep.Name, err)
	}
	if taskID == "" {
		sort.Strings(names)
		polls := "none"
		if len(names) > 0 {
			polls = strings.Join(names, ", ")
		}
		return nil, fmt.Errorf("%w: endpoint %q has no poll named %q; its polls are %s", ErrNoSuchPoll, ep.Name, poll, polls)
	}
	run := &EndpointPoll{EndpointID: ep.ID, Endpoint: ep.Name, NodeName: *ep.Node, TaskID: taskID, Poll: poll}
	if ep.Component != nil {
		run.Component = *ep.Component
	}
	endpointID := ep.ID
	if err := writeAuditRes(ctx, tx, actorID, "run", "endpoint", endpointID, nil, map[string]any{"poll": poll, "node": run.NodeName}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit endpoint run: %w", err)
	}
	if run.Sealed, err = p.endpointSealed(ctx, ep); err != nil {
		return nil, err
	}
	return run, nil
}

// endpointSealed is the plaintext of every secret field the endpoint's tasks
// carry to its node: each secret input of its driver, resolved as the
// worklist resolves it (cascading onto the endpoint's own component), and of
// each, the fields its type marks secret. A reference that resolves to
// nothing is absent here as it is on the worklist; a real failure (no key
// provider, a DB error, an envelope that will not open) refuses the run,
// since an answer that could not be masked must not be sent.
func (p *PG) endpointSealed(ctx context.Context, ep *Endpoint) ([]string, error) {
	if ep.DriverID == nil {
		return nil, nil
	}
	var raw []byte
	if err := p.pool.QueryRow(ctx, `select spec from driver where id = $1`, *ep.DriverID).Scan(&raw); err != nil {
		return nil, fmt.Errorf("storage: endpoint %q driver spec: %w", ep.Name, err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	sp, err := driver.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("storage: endpoint %q driver spec does not parse: %w", ep.Name, err)
	}
	refs, err := secretRefsFrom(sp, ep.Inputs)
	if err != nil {
		return nil, fmt.Errorf("storage: endpoint %q: %w", ep.Name, err)
	}
	if len(refs) == 0 {
		return nil, nil
	}
	cid := ""
	if ep.ComponentID != nil {
		cid = *ep.ComponentID
	}
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref)
	}
	sort.Strings(names)
	asks := make([]nodeSecretRef, len(names))
	for i, name := range names {
		asks[i] = nodeSecretRef{name, cid}
	}
	got, err := p.nodeSecrets(ctx, asks)
	if err != nil {
		return nil, fmt.Errorf("storage: endpoint %q unseal: %w", ep.Name, err)
	}
	var sealed []string
	for _, g := range got {
		if g.err != nil {
			continue
		}
		for _, f := range g.shape.Fields {
			if v := g.fields[f.Name]; f.Secret && v != "" {
				sealed = append(sealed, v)
			}
		}
	}
	return sealed, nil
}
