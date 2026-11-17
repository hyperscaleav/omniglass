package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// Task-layer sentinel errors. A task is not an operator-authored entity: it is
// DERIVED plumbing (an outpost's unit of collection work), read-only over the API. A
// task hangs off an endpoint (task.endpoint_id), which hangs off a component, so
// its read scope cascades through that component. NotFound doubles as the
// non-disclosing "out of read scope".
var (
	ErrTaskNotFound  = errors.New("storage: task not found")
	ErrTaskForbidden = errors.New("storage: action not permitted on this task")
)

// Task is an outpost's content-addressed unit of collection work: Mode is the
// poll/listen axis, EndpointID the connection it runs over, Outpost the placement
// PROJECTED from its endpoint (a task carries no outpost column of its own; the
// endpoint's placement is authoritative), Spec the inline probe jsonb, Enabled
// the worklist toggle. ID is a content hash of the identity fields (endpoint +
// mode + spec) so identical work dedupes. A task is DERIVED when an endpoint is
// created, never operator-authored.
type Task struct {
	ID         string
	Label      string
	Mode       string
	EndpointID string
	Outpost    *string
	OutpostID  *string
	Spec       []byte
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// taskID is the content-addressed task id: a sha256 over the identity fields
// (endpoint, mode, spec) so identical work always maps to the same id and a
// re-derive dedupes on the primary key. Label and the enabled toggle are
// metadata, not identity, so they do not perturb the id.
func taskID(endpointID, mode string, spec []byte) string {
	h := sha256.New()
	h.Write([]byte(endpointID))
	h.Write([]byte{0})
	h.Write([]byte(mode))
	h.Write([]byte{0})
	h.Write(spec)
	return hex.EncodeToString(h.Sum(nil))
}

// taskSelectJoin is the task columns aliased to `t`, with Outpost PROJECTED from the
// joined endpoint: a task carries no outpost column, so every read joins endpoint
// to resolve placement. The endpoint arc stores outpost.user_id, so the
// projection resolves it to the outpost's name, which is what Task.Outpost carries.
// Callers always join `endpoint i on i.id = t.endpoint_id`.
const taskSelectJoin = `t.id, coalesce(t.label, ''), t.mode, t.endpoint_id,
	(select n.name from outpost n where n.user_id = i.outpost_id) as outpost_id, i.outpost_id,
	t.spec, t.enabled, t.created_at, t.updated_at`

func scanTask(row pgx.Row) (*Task, error) {
	var t Task
	if err := row.Scan(&t.ID, &t.Label, &t.Mode, &t.EndpointID, &t.Outpost, &t.OutpostID, &t.Spec, &t.Enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// deriveReachabilityTask idempotently derives the poll task for an endpoint: the
// outpost's unit of collection work over that connection. It is called inside
// CreateEndpoint's transaction, never by an operator; the content-addressed id
// makes a re-derive a no-op (on conflict do nothing). The task carries no outpost
// column: its placement is a projection of the endpoint's outpost_id.
func deriveReachabilityTask(ctx context.Context, tx pgx.Tx, endpointID string) error {
	body := []byte("{}")
	id := taskID(endpointID, "poll", body)
	if _, err := tx.Exec(ctx, `
		insert into task (id, mode, endpoint_id, spec, enabled)
		values ($1, 'poll', $2, $3, true)
		on conflict (id) do nothing`, id, endpointID, body); err != nil {
		return fmt.Errorf("storage: derive reachability task for endpoint %q: %w", endpointID, err)
	}
	return nil
}

// loadTask reads one task by id plus its endpoint's owning component id (the
// scope anchor, componentInScope's own parameter) with no scope check; callers
// layer the cascade on top. i.component is the raw FK column, not a name
// derived from it: componentInScope binds it straight into inScopeTree, and a
// name here would 22P02 (invalid uuid) the moment it reached that bind, since
// #627 preparation switched componentInScope from a name lookup to an id.
func loadTask(ctx context.Context, q querier, id string) (*Task, *string, error) {
	var (
		t         Task
		component *string
	)
	err := q.QueryRow(ctx, `
		select `+taskSelectJoin+`, i.component
		from task t join endpoint i on i.id = t.endpoint_id
		where t.id = $1`, id).Scan(
		&t.ID, &t.Label, &t.Mode, &t.EndpointID, &t.Outpost, &t.OutpostID, &t.Spec, &t.Enabled, &t.CreatedAt, &t.UpdatedAt, &component)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrTaskNotFound
	} else if err != nil {
		return nil, nil, fmt.Errorf("storage: load task %q: %w", id, err)
	}
	return &t, component, nil
}

// ListTasks returns the tasks whose endpoint's owning component is in the
// caller's read scope, ordered by id. A component-scoped read (the cascade)
// expands the component subtree and matches tasks joined through their endpoint;
// an all read returns every task; an empty scope returns none.
func (p *PG) ListTasks(ctx context.Context, read scope.Set) ([]Task, error) {
	if read.Empty() {
		return nil, nil
	}
	var (
		rows pgx.Rows
		err  error
	)
	if read.All {
		rows, err = p.pool.Query(ctx, `select `+taskSelectJoin+` from task t join endpoint i on i.id = t.endpoint_id order by t.id`)
	} else {
		roots := uuidRoots(read.IDs)
		selfIDs := uuidRoots(read.SelfIDs)
		if len(roots) == 0 && len(selfIDs) == 0 {
			return nil, nil
		}
		rows, err = p.pool.Query(ctx, `
			with recursive sub(id) as (
				select id from component where id = any($1::uuid[])
				union all
				select c.id from component c join sub on c.parent_id = sub.id
			) cycle id set is_cycle using path
			select `+taskSelectJoin+` from task t
			join endpoint i on i.id = t.endpoint_id
			join component c on c.id = i.component
			where c.id in (select id from sub) or c.id = any($2::uuid[])
			order by t.id`, roots, selfIDs)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: list tasks: %w", err)
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan task: %w", err)
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTask resolves a task by id within the caller's read scope (through its
// endpoint's component); absent or out of scope is the same non-disclosing
// ErrTaskNotFound.
func (p *PG) GetTask(ctx context.Context, id string, read scope.Set) (*Task, error) {
	t, component, err := loadTask(ctx, p.pool, id)
	if err != nil {
		return nil, err
	}
	in, err := componentInScope(ctx, p.pool, component, read)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrTaskNotFound
	}
	return t, nil
}

// TaskOwner is a task's resolved ingest binding: the component its endpoint
// dedicates its samples to, plus the endpoint identity the write records as
// source (transport) and instance (name).
//
// ComponentID is the component's uuid, never its name: it is what
// ResolveTaskOwner reads directly off the endpoint's own FK column
// (i.component), and every downstream write (deriveMetrics, deriveProperties,
// deriveEvents, and the batch's landing) stamps it as OwnerID verbatim. #627 scopes
// component name uniqueness to placement, so a name here would be resolved a
// second time, ambiguously, by ownerArcValue on the write side; the id needs no
// second resolution and can never collide.
type TaskOwner struct {
	ComponentID string
	// EndpointID is the task's endpoint, which ingest stamps as seen (#933);
	// empty on the push lane, which has no endpoint.
	EndpointID   string
	EndpointName string
	Transport    string
	// OutpostID is the outpost the task's endpoint is placed on (its user id),
	// the collector a metric series records (#1275).
	OutpostID string
}

// ResolveTaskOwner binds the component an outpost's task collects for and, in the same
// query, confines the outpost to its own tasks. Given a task id and the outpost that
// published the telemetry (extracted from the NATS subject), it returns the task's
// endpoint component id. Confinement is against the ENDPOINT's placement
// (i.outpost_id), the authoritative outpost binding, since a task carries no outpost
// column. ok is false (the sample is an orphan the ingest consumer drops, never
// writes) when the task is unknown, its endpoint belongs to a DIFFERENT outpost (a
// outpost cannot land a sample for a component it was not placed on), or its
// endpoint has no component (a shared endpoint has no pre-bound owner). err is
// reserved for a real DB failure, so the caller can leave the message unacked for
// redelivery.
func (p *PG) ResolveTaskOwner(ctx context.Context, taskID, outpostName string) (TaskOwner, bool, error) {
	var (
		owner        TaskOwner
		componentID  *string
		ifaceOutpost *string
	)
	// i.component is bound directly (the FK column, not a name derived from
	// it): the row already has it, and re-deriving one from a name only to
	// have the write side resolve it a second time is exactly the shape #627
	// makes ambiguous.
	err := p.pool.QueryRow(ctx, `
		select i.component, (select n.name from outpost n where n.user_id = i.outpost_id), i.id, i.name, i.transport,
			coalesce(i.outpost_id::text, '')
		from task t
		join endpoint i on i.id = t.endpoint_id
		where t.id = $1`, taskID).Scan(&componentID, &ifaceOutpost, &owner.EndpointID, &owner.EndpointName, &owner.Transport, &owner.OutpostID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskOwner{}, false, nil
	} else if err != nil {
		return TaskOwner{}, false, fmt.Errorf("storage: resolve task owner %q: %w", taskID, err)
	}
	if ifaceOutpost == nil || *ifaceOutpost != outpostName {
		return TaskOwner{}, false, nil // confinement: not this outpost's endpoint
	}
	if componentID == nil || *componentID == "" {
		return TaskOwner{}, false, nil // shared endpoint: no pre-bound owner
	}
	owner.ComponentID = *componentID
	return owner, true, nil
}

// ResolveTaskOwners is ResolveTaskOwner for every task of one outpost's batch in
// one statement (#1582): an outpost's tick carries many tasks, and binding each
// with a read of its own would grow the ingest's reads with the tick. Each
// task id the outpost may land for maps to the owner ResolveTaskOwner gives it;
// a task ResolveTaskOwner refuses (unknown, another outpost's endpoint, a shared
// endpoint with no component) is absent, never an error. err is reserved for
// a real DB failure, so the caller can leave the message unacked for
// redelivery.
func (p *PG) ResolveTaskOwners(ctx context.Context, outpostName string, taskIDs []string) (map[string]TaskOwner, error) {
	out := make(map[string]TaskOwner, len(taskIDs))
	if len(taskIDs) == 0 {
		return out, nil
	}
	// The single read's confinement and its no-component rule, held in the
	// statement's own condition: a row it returns is one ResolveTaskOwner
	// would have bound.
	rows, err := p.pool.Query(ctx, `
		select t.id, i.component::text, i.id, i.name, i.transport, i.outpost_id::text
		from task t
		join endpoint i on i.id = t.endpoint_id
		join outpost n on n.user_id = i.outpost_id
		where t.id = any($1::text[]) and n.name = $2 and i.component is not null`, taskIDs, outpostName)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve task owners for %s: %w", outpostName, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var o TaskOwner
		if err := rows.Scan(&id, &o.ComponentID, &o.EndpointID, &o.EndpointName, &o.Transport, &o.OutpostID); err != nil {
			return nil, fmt.Errorf("storage: resolve task owners for %s: %w", outpostName, err)
		}
		out[id] = o
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: resolve task owners for %s: %w", outpostName, err)
	}
	return out, nil
}
