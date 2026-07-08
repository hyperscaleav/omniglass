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
	"github.com/jackc/pgx/v5/pgconn"
)

// Task-layer sentinel errors. A task is not a scope-tree entity of its own; it
// hangs off an interface (task.interface_id), which hangs off a component, so
// its scope cascades through that component. NotFound doubles as the
// non-disclosing "out of read scope"; Forbidden is readable-but-not-actionable.
var (
	ErrTaskNotFound          = errors.New("storage: task not found")
	ErrTaskForbidden         = errors.New("storage: action not permitted on this task")
	ErrTaskExists            = errors.New("storage: task already exists")
	ErrTaskInterfaceNotFound = errors.New("storage: task interface not found")
	ErrTaskNodeNotFound      = errors.New("storage: task node not found")
	ErrInvalidTaskMode       = errors.New("storage: task mode is not poll or listen")
)

// Task is a node's content-addressed unit of collection work: Mode is the
// poll/listen axis, InterfaceID the placement-bound connection it runs over,
// Node the server-assigned placement (nil until assigned), Spec the inline probe
// jsonb, Enabled the worklist toggle. ID is a content hash of the identity fields
// (interface + mode + spec) so identical work dedupes.
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

// TaskSpec is the create input. Enabled defaults to true when nil.
type TaskSpec struct {
	DisplayName string
	Mode        string
	InterfaceID string
	Node        *string
	Spec        []byte
	Enabled     *bool
}

// TaskPatch is the update input: nil fields unchanged. The identity fields
// (interface, mode, spec) that form the content-addressed id are immutable here
// (changing them is a new task); the operational toggles move.
type TaskPatch struct {
	DisplayName *string
	Enabled     *bool
	Node        *string
	Spec        []byte
}

// taskID is the content-addressed task id: a sha256 over the identity fields
// (interface, mode, spec) so identical work always maps to the same id and a
// re-create dedupes on the primary key. Display name, node placement, and the
// enabled toggle are metadata, not identity, so they do not perturb the id.
func taskID(interfaceID, mode string, spec []byte) string {
	h := sha256.New()
	h.Write([]byte(interfaceID))
	h.Write([]byte{0})
	h.Write([]byte(mode))
	h.Write([]byte{0})
	h.Write(spec)
	return hex.EncodeToString(h.Sum(nil))
}

// taskCols is the bare select list (scan order), for the un-aliased insert/update
// RETURNING; taskColsJoin is the same list aliased to `t` for the load (joined to
// interface for the owning component) and the scoped list join.
const (
	taskCols     = `id, display_name, mode, interface_id, node_name, spec, enabled, created_at, updated_at`
	taskColsJoin = `t.id, t.display_name, t.mode, t.interface_id, t.node_name, t.spec, t.enabled, t.created_at, t.updated_at`
)

func scanTask(row pgx.Row) (*Task, error) {
	var t Task
	if err := row.Scan(&t.ID, &t.DisplayName, &t.Mode, &t.InterfaceID, &t.Node, &t.Spec, &t.Enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// loadTask reads one task by id plus its interface's owning component (the scope
// anchor) with no scope check; callers layer the cascade on top.
func loadTask(ctx context.Context, q querier, id string) (*Task, *string, error) {
	var (
		t         Task
		component *string
	)
	err := q.QueryRow(ctx, `
		select `+taskColsJoin+`, i.component
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
		rows, err = p.pool.Query(ctx, `select `+taskCols+` from task order by id`)
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
			select `+taskColsJoin+` from task t
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

// CreateTask inserts a content-addressed task over an interface, writing the
// audit row in the same transaction. The create scope is checked against the
// interface's owning component (the cascade): a missing interface is a 422, an
// out-of-scope component a 403. A duplicate id (identical work) is ErrTaskExists.
func (p *PG) CreateTask(ctx context.Context, actorID string, spec TaskSpec, create scope.Set) (*Task, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create task: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var component *string
	err = tx.QueryRow(ctx, `select component from interface where id = $1`, spec.InterfaceID).Scan(&component)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTaskInterfaceNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: resolve task interface %q: %w", spec.InterfaceID, err)
	}
	in, err := componentInScope(ctx, tx, component, create)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrTaskForbidden
	}

	body := spec.Spec
	if len(body) == 0 {
		body = []byte("{}")
	}
	enabled := true
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	id := taskID(spec.InterfaceID, spec.Mode, body)
	t, err := scanTask(tx.QueryRow(ctx, `
		insert into task (id, display_name, mode, interface_id, node_name, spec, enabled)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning `+taskCols,
		id, spec.DisplayName, spec.Mode, spec.InterfaceID, spec.Node, body, enabled))
	if err != nil {
		return nil, mapTaskWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "task", t.ID, nil, t); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create task: %w", err)
	}
	return t, nil
}

// UpdateTask patches a task's display name, enabled toggle, node placement, or
// spec with the read-then-action scope split (through the interface's component)
// and in-transaction audit. The identity fields that form the id are immutable.
func (p *PG) UpdateTask(ctx context.Context, actorID, id string, patch TaskPatch, read, action scope.Set) (*Task, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update task: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveTaskScoped(ctx, tx, id, read, action)
	if err != nil {
		return nil, err
	}
	after, err := scanTask(tx.QueryRow(ctx, `
		update task set
			display_name = coalesce($2, display_name),
			enabled      = coalesce($3, enabled),
			node_name    = coalesce($4, node_name),
			spec         = coalesce($5, spec),
			updated_at   = now()
		where id = $1
		returning `+taskCols,
		before.ID, patch.DisplayName, patch.Enabled, patch.Node, nullableJSON(patch.Spec)))
	if err != nil {
		return nil, mapTaskWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "task", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update task: %w", err)
	}
	return after, nil
}

// DeleteTask removes a task by id with the read/action split (through the
// interface's component) and in-transaction audit.
func (p *PG) DeleteTask(ctx context.Context, actorID, id string, read, action scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete task: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveTaskScoped(ctx, tx, id, read, action)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from task where id = $1`, before.ID); err != nil {
		return fmt.Errorf("storage: delete task: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "task", before.ID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete task: %w", err)
	}
	return nil
}

// resolveTaskScoped loads a task and enforces the read-then-action scope split
// through its interface's owning component: out of read scope is the
// non-disclosing ErrTaskNotFound, readable but out of action scope is
// ErrTaskForbidden.
func resolveTaskScoped(ctx context.Context, q querier, id string, read, action scope.Set) (*Task, error) {
	t, component, err := loadTask(ctx, q, id)
	if err != nil {
		return nil, err
	}
	readable, err := componentInScope(ctx, q, component, read)
	if err != nil {
		return nil, err
	}
	if !readable {
		return nil, ErrTaskNotFound
	}
	actionable, err := componentInScope(ctx, q, component, action)
	if err != nil {
		return nil, err
	}
	if !actionable {
		return nil, ErrTaskForbidden
	}
	return t, nil
}

func mapTaskWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTaskExists
		case "23503": // foreign_key_violation
			switch pgErr.ConstraintName {
			case "task_interface_id_fkey":
				return ErrTaskInterfaceNotFound
			case "task_node_name_fkey":
				return ErrTaskNodeNotFound
			}
		case "23514": // check_violation (task_mode_check)
			return ErrInvalidTaskMode
		}
	}
	return fmt.Errorf("storage: task write: %w", err)
}

// TaskOwner is a task's resolved ingest binding: the component its interface
// dedicates its datapoints to, plus the interface identity the write records as
// source (type) and instance (name).
type TaskOwner struct {
	Component     string
	InterfaceName string
	InterfaceType string
}

// ResolveTaskOwner binds the component a node's task collects for and, in the
// same query, confines the node to its own tasks. Given a task id and the node
// that published the telemetry (extracted from the NATS subject), it returns the
// task's interface component. ok is false (the datapoint is an orphan the ingest
// consumer drops, never writes) when the task is unknown, belongs to a DIFFERENT
// node (the confinement fence: a node cannot land a datapoint for a component it
// was not placed on), or its interface has no component (a shared interface has
// no pre-bound owner in this checkpoint). err is reserved for a real DB failure,
// so the caller can leave the message unacked for redelivery.
func (p *PG) ResolveTaskOwner(ctx context.Context, taskID, nodeName string) (TaskOwner, bool, error) {
	var (
		owner     TaskOwner
		component *string
		taskNode  *string
	)
	err := p.pool.QueryRow(ctx, `
		select i.component, t.node_name, i.name, i.type
		from task t
		join interface i on i.id = t.interface_id
		where t.id = $1`, taskID).Scan(&component, &taskNode, &owner.InterfaceName, &owner.InterfaceType)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskOwner{}, false, nil
	} else if err != nil {
		return TaskOwner{}, false, fmt.Errorf("storage: resolve task owner %q: %w", taskID, err)
	}
	if taskNode == nil || *taskNode != nodeName {
		return TaskOwner{}, false, nil // confinement: not this node's task
	}
	if component == nil || *component == "" {
		return TaskOwner{}, false, nil // shared interface: no pre-bound owner
	}
	owner.Component = *component
	return owner, true, nil
}
