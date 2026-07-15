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
// DERIVED plumbing (a node's unit of collection work), read-only over the API. A
// task hangs off an interface (task.interface_id), which hangs off a component, so
// its read scope cascades through that component. NotFound doubles as the
// non-disclosing "out of read scope".
var (
	ErrTaskNotFound  = errors.New("storage: task not found")
	ErrTaskForbidden = errors.New("storage: action not permitted on this task")
)

// Task is a node's content-addressed unit of collection work: Mode is the
// poll/listen axis, InterfaceID the connection it runs over, Node the placement
// PROJECTED from its interface (a task carries no node column of its own; the
// interface's placement is authoritative), Spec the inline probe jsonb, Enabled
// the worklist toggle. ID is a content hash of the identity fields (interface +
// mode + spec) so identical work dedupes. A task is DERIVED when an interface is
// created, never operator-authored.
type Task struct {
	ID          string
	DisplayName string
	Mode        string
	InterfaceID string
	Node        *string
	Spec        []byte
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// taskID is the content-addressed task id: a sha256 over the identity fields
// (interface, mode, spec) so identical work always maps to the same id and a
// re-derive dedupes on the primary key. Display name and the enabled toggle are
// metadata, not identity, so they do not perturb the id.
func taskID(interfaceID, mode string, spec []byte) string {
	h := sha256.New()
	h.Write([]byte(interfaceID))
	h.Write([]byte{0})
	h.Write([]byte(mode))
	h.Write([]byte{0})
	h.Write(spec)
	return hex.EncodeToString(h.Sum(nil))
}

// taskSelectJoin is the task columns aliased to `t`, with Node PROJECTED from the
// joined interface (i.node_name): a task carries no node column, so every read
// joins interface to resolve placement. Callers always join `interface i on i.id
// = t.interface_id`.
const taskSelectJoin = `t.id, t.display_name, t.mode, t.interface_id, i.node_name, t.spec, t.enabled, t.created_at, t.updated_at`

func scanTask(row pgx.Row) (*Task, error) {
	var t Task
	if err := row.Scan(&t.ID, &t.DisplayName, &t.Mode, &t.InterfaceID, &t.Node, &t.Spec, &t.Enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// deriveReachabilityTask idempotently derives the poll task for an interface: the
// node's unit of collection work over that connection. It is called inside
// CreateInterface's transaction, never by an operator; the content-addressed id
// makes a re-derive a no-op (on conflict do nothing). The task carries no node
// column: its placement is a projection of the interface's node_name.
func deriveReachabilityTask(ctx context.Context, tx pgx.Tx, interfaceID string) error {
	body := []byte("{}")
	id := taskID(interfaceID, "poll", body)
	if _, err := tx.Exec(ctx, `
		insert into task (id, mode, interface_id, spec, enabled)
		values ($1, 'poll', $2, $3, true)
		on conflict (id) do nothing`, id, interfaceID, body); err != nil {
		return fmt.Errorf("storage: derive reachability task for interface %q: %w", interfaceID, err)
	}
	return nil
}

// loadTask reads one task by id plus its interface's owning component (the scope
// anchor) with no scope check; callers layer the cascade on top.
func loadTask(ctx context.Context, q querier, id string) (*Task, *string, error) {
	var (
		t         Task
		component *string
	)
	err := q.QueryRow(ctx, `
		select `+taskSelectJoin+`, i.component
		from task t join interface i on i.id = t.interface_id
		where t.id = $1`, id).Scan(
		&t.ID, &t.DisplayName, &t.Mode, &t.InterfaceID, &t.Node, &t.Spec, &t.Enabled, &t.CreatedAt, &t.UpdatedAt, &component)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrTaskNotFound
	} else if err != nil {
		return nil, nil, fmt.Errorf("storage: load task %q: %w", id, err)
	}
	return &t, component, nil
}

// ListTasks returns the tasks whose interface's owning component is in the
// caller's read scope, ordered by id. A component-scoped read (the cascade)
// expands the component subtree and matches tasks joined through their interface;
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
		rows, err = p.pool.Query(ctx, `select `+taskSelectJoin+` from task t join interface i on i.id = t.interface_id order by t.id`)
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
			join interface i on i.id = t.interface_id
			join component c on c.name = i.component
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
// interface's component); absent or out of scope is the same non-disclosing
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

// TaskOwner is a task's resolved ingest binding: the component its interface
// dedicates its datapoints to, plus the interface identity the write records as
// source (type) and instance (name).
type TaskOwner struct {
	Component     string
	InterfaceName string
	InterfaceType string
}

// ResolveTaskOwner binds the component a node's task collects for and, in the same
// query, confines the node to its own tasks. Given a task id and the node that
// published the telemetry (extracted from the NATS subject), it returns the task's
// interface component. Confinement is against the INTERFACE's placement
// (i.node_name), the authoritative node binding, since a task carries no node
// column. ok is false (the datapoint is an orphan the ingest consumer drops, never
// writes) when the task is unknown, its interface belongs to a DIFFERENT node (a
// node cannot land a datapoint for a component it was not placed on), or its
// interface has no component (a shared interface has no pre-bound owner). err is
// reserved for a real DB failure, so the caller can leave the message unacked for
// redelivery.
func (p *PG) ResolveTaskOwner(ctx context.Context, taskID, nodeName string) (TaskOwner, bool, error) {
	var (
		owner     TaskOwner
		component *string
		ifaceNode *string
	)
	err := p.pool.QueryRow(ctx, `
		select i.component, i.node_name, i.name, i.type
		from task t
		join interface i on i.id = t.interface_id
		where t.id = $1`, taskID).Scan(&component, &ifaceNode, &owner.InterfaceName, &owner.InterfaceType)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskOwner{}, false, nil
	} else if err != nil {
		return TaskOwner{}, false, fmt.Errorf("storage: resolve task owner %q: %w", taskID, err)
	}
	if ifaceNode == nil || *ifaceNode != nodeName {
		return TaskOwner{}, false, nil // confinement: not this node's interface
	}
	if component == nil || *component == "" {
		return TaskOwner{}, false, nil // shared interface: no pre-bound owner
	}
	owner.Component = *component
	return owner, true, nil
}
