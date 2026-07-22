package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Interface-layer sentinel errors. An interface is not a scope-tree entity of its
// own; it hangs off a component (interface.component), so its scope cascades
// through that component (see componentInScope). NotFound doubles as the
// non-disclosing "out of read scope"; Forbidden is readable-but-not-actionable.
var (
	ErrInterfaceNotFound          = errors.New("storage: interface not found")
	ErrInterfaceForbidden         = errors.New("storage: action not permitted on this interface")
	ErrInterfaceExists            = errors.New("storage: interface name already exists on this component")
	ErrUnknownInterfaceType       = errors.New("storage: unknown interface_type")
	ErrInterfaceComponentNotFound = errors.New("storage: interface component not found")
	ErrInterfaceNodeNotFound      = errors.New("storage: interface node not found")
)

// Interface is a named, placement-bound connection: type is an interface_type,
// Component is the owner (nil for a server-hosted interface), Node is the
// server-assigned placement (nil until assigned), Params is the endpoint/target
// jsonb. ID is the surrogate primary key (a uuidv7); Name is the friendly address,
// unique within the owning component.
type Interface struct {
	ID          string
	Name        string
	Type        string
	Component   *string
	ComponentID *string
	Node        *string
	NodeID      *string
	Params      []byte
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// InterfaceSpec is the create input. The interface is protocol-named: its name is
// DERIVED from its type (the protocol it speaks), unique within the owning
// component, never operator-typed. The id is server-generated.
type InterfaceSpec struct {
	Type      string
	Component *string
	Node      *string
	Params    []byte
}

// InterfacePatch is the update input: nil fields unchanged. Type and component
// rebind are deferred (they cross the adapter kind and the scope boundary),
// mirroring the component tier's deferred reparent; the operationally useful
// Node (re)assignment and Params retarget move here.
type InterfacePatch struct {
	Node   *string
	Params []byte
}

// interfaceCols is the bare select list (scan order), for the un-aliased
// insert/update RETURNING and the by-id load; interfaceColsJoin is the same
// list aliased to `i` for the scoped join over the component table.
// The two arcs store primary keys (component.id, node.principal_id), so each is
// selected alongside a scalar subquery for the owner's current name: the id is
// what the row points at, the name is what an operator reads and types. The
// subquery form works identically in a plain select and in a RETURNING list, so
// the insert and update paths need no join. Both derived columns are aliased,
// since an unaliased `... c.name ...` would emit a second output column called
// `name` and make `order by name` ambiguous.
const (
	interfaceCols = `id, name, type,
		(select c.name from component c where c.id = interface.component) as component_name, component,
		(select n.name from node n where n.principal_id = interface.node_name) as node_name_ref, node_name,
		params, created_at, updated_at`
	interfaceColsJoin = `i.id, i.name, i.type,
		(select c2.name from component c2 where c2.id = i.component) as component_name, i.component,
		(select n.name from node n where n.principal_id = i.node_name) as node_name_ref, i.node_name,
		i.params, i.created_at, i.updated_at`
)

func scanInterface(row pgx.Row) (*Interface, error) {
	var it Interface
	if err := row.Scan(&it.ID, &it.Name, &it.Type, &it.Component, &it.ComponentID, &it.Node, &it.NodeID, &it.Params, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, err
	}
	return &it, nil
}

// componentInScope reports whether an interface/task owning componentName is
// inside a component-tier scope, the cascade both entities share: an all scope
// always holds; a nil owner (a server-hosted interface with no component) is in
// scope ONLY for an all scope (there is no component to cascade through, so a
// component-scoped operator cannot reach it); otherwise the component's row is
// checked against the scope subtree via inScopeTree on the component table. The
// set carries component-tier ids (applicableKinds maps interface/task to the
// component tier), so no id translation is needed beyond name -> component id.
func componentInScope(ctx context.Context, q querier, componentName *string, set scope.Set) (bool, error) {
	if set.All {
		return true, nil
	}
	if componentName == nil {
		return false, nil
	}
	var id string
	err := q.QueryRow(ctx, `select id from component where name = $1`, *componentName).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // component gone: nothing to place it in a rooted scope
	} else if err != nil {
		return false, fmt.Errorf("storage: resolve component %q for scope: %w", *componentName, err)
	}
	return inScopeTree(ctx, q, componentTable, id, set)
}

// interfaceComponentID resolves an optional component reference (a name or a
// uuid) to the component id the arc stores. A nil reference stays nil (a
// server-hosted interface owns no component); an unknown one is the 422 sentinel
// rather than a NULL that would silently detach the interface.
func interfaceComponentID(ctx context.Context, q querier, ref *string) (*string, error) {
	if ref == nil {
		return nil, nil
	}
	c, err := scopedByName(ctx, q, componentConfig, *ref)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, ErrInterfaceComponentNotFound
	} else if err != nil {
		return nil, err
	}
	return &c.ID, nil
}

// interfaceNodeID resolves an optional node reference (a name or a principal id)
// to the principal id the placement arc stores. An unknown node is
// ErrNodeNotFound, so an unassignable placement fails loudly.
func interfaceNodeID(ctx context.Context, q querier, ref *string) (*string, error) {
	if ref == nil {
		return nil, nil
	}
	col := "name"
	if isUUID(*ref) {
		col = "principal_id"
	}
	var pid string
	if err := q.QueryRow(ctx, `select principal_id from node where `+col+` = $1`, *ref).Scan(&pid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNodeNotFound
		}
		return nil, fmt.Errorf("storage: resolve node %q: %w", *ref, err)
	}
	return &pid, nil
}

// loadInterface reads one interface by id with no scope check; callers layer
// scope on top (via componentInScope).
func loadInterface(ctx context.Context, q querier, id string) (*Interface, error) {
	it, err := scanInterface(q.QueryRow(ctx, `select `+interfaceCols+` from interface where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInterfaceNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: load interface %q: %w", id, err)
	}
	return it, nil
}

// ListInterfaces returns the interfaces whose owning component is in the caller's
// read scope, ordered by name. A component-scoped read (the cascade) expands the
// component subtree and matches interfaces joined onto it; an all read returns
// every interface (including component-less ones); an empty scope returns none.
func (p *PG) ListInterfaces(ctx context.Context, read scope.Set) ([]Interface, error) {
	if read.Empty() {
		return nil, nil
	}
	var (
		rows pgx.Rows
		err  error
	)
	if read.All {
		rows, err = p.pool.Query(ctx, `select `+interfaceCols+` from interface order by name`)
	} else {
		roots := uuidRoots(read.IDs)
		selfIDs := uuidRoots(read.SelfIDs)
		if len(roots) == 0 && len(selfIDs) == 0 {
			return nil, nil
		}
		// The component subtree walk (read never excludes a root, so no exclude arm),
		// joined onto interface by its owning component; a component-less interface has
		// no join match and stays hidden outside an all scope.
		rows, err = p.pool.Query(ctx, `
			with recursive sub(id) as (
				select id from component where id = any($1::uuid[])
				union all
				select c.id from component c join sub on c.parent_id = sub.id
			) cycle id set is_cycle using path
			select `+interfaceColsJoin+` from interface i
			join component c on c.id = i.component
			where c.id in (select id from sub) or c.id = any($2::uuid[])
			order by i.name`, roots, selfIDs)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: list interfaces: %w", err)
	}
	defer rows.Close()
	var out []Interface
	for rows.Next() {
		it, err := scanInterface(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan interface: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// GetInterface resolves an interface by id within the caller's read scope;
// absent or out of scope is the same non-disclosing ErrInterfaceNotFound.
func (p *PG) GetInterface(ctx context.Context, id string, read scope.Set) (*Interface, error) {
	it, err := loadInterface(ctx, p.pool, id)
	if err != nil {
		return nil, err
	}
	in, err := componentInScope(ctx, p.pool, it.Component, read)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrInterfaceNotFound
	}
	return it, nil
}

// CreateInterface inserts an interface owned by an optional component, placed on
// an optional node, writing the audit row in the same transaction. The create
// scope is checked against the owning component (the cascade): a component-less
// interface requires an all create scope; a component-bound one requires that
// component in the create scope. A missing component is a 422, an out-of-scope
// one a 403, mirroring the component tier's parent-placement split.
func (p *PG) CreateInterface(ctx context.Context, actorID string, spec InterfaceSpec, create scope.Set) (*Interface, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create interface: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if spec.Component == nil {
		if !create.All {
			return nil, ErrInterfaceForbidden
		}
	} else {
		var id string
		err := tx.QueryRow(ctx, `select id from component where name = $1`, *spec.Component).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInterfaceComponentNotFound
		} else if err != nil {
			return nil, fmt.Errorf("storage: resolve component %q: %w", *spec.Component, err)
		}
		in, err := inScopeTree(ctx, tx, componentTable, id, create)
		if err != nil {
			return nil, err
		}
		if !in {
			return nil, ErrInterfaceForbidden
		}
	}

	params := spec.Params
	if len(params) == 0 {
		params = []byte("{}")
	}
	componentID, err := interfaceComponentID(ctx, tx, spec.Component)
	if err != nil {
		return nil, err
	}
	nodeID, err := interfaceNodeID(ctx, tx, spec.Node)
	if err != nil {
		return nil, err
	}
	it, err := scanInterface(tx.QueryRow(ctx, `
		insert into interface (name, type, component, node_name, params)
		values ($1, $1, $2, $3, $4)
		returning `+interfaceCols,
		spec.Type, componentID, nodeID, params))
	if err != nil {
		return nil, mapInterfaceWriteErr(err)
	}
	// The interface is protocol-named: its name IS its transport/type (unique per
	// component). Deriving the reachability poll task here, the node's unit of work
	// over this connection, makes task a derived artifact, never operator-authored.
	if err := deriveReachabilityTask(ctx, tx, it.ID); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "interface", it.ID, nil, it); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create interface: %w", err)
	}
	return it, nil
}

// UpdateInterface patches an interface's node placement or params with the
// read-then-action scope split (both evaluated against the owning component) and
// in-transaction audit. Type and component rebind are deferred.
func (p *PG) UpdateInterface(ctx context.Context, actorID, id string, patch InterfacePatch, read, action scope.Set) (*Interface, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update interface: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveInterfaceScoped(ctx, tx, id, read, action)
	if err != nil {
		return nil, err
	}
	nodeID, err := interfaceNodeID(ctx, tx, patch.Node)
	if err != nil {
		return nil, err
	}
	after, err := scanInterface(tx.QueryRow(ctx, `
		update interface set
			node_name = coalesce($2, node_name),
			params    = coalesce($3, params),
			updated_at = now()
		where id = $1
		returning `+interfaceCols,
		before.ID, nodeID, nullableJSON(patch.Params)))
	if err != nil {
		return nil, mapInterfaceWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "interface", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update interface: %w", err)
	}
	return after, nil
}

// DeleteInterface removes an interface by id with the read/action split (through
// the owning component) and in-transaction audit. Its derived tasks cascade-delete
// with it (task.interface_id ON DELETE CASCADE).
func (p *PG) DeleteInterface(ctx context.Context, actorID, id string, read, action scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete interface: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveInterfaceScoped(ctx, tx, id, read, action)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from interface where id = $1`, before.ID); err != nil {
		return mapInterfaceWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "interface", before.ID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete interface: %w", err)
	}
	return nil
}

// resolveInterfaceScoped loads an interface and enforces the read-then-action
// scope split through its owning component: out of read scope is the
// non-disclosing ErrInterfaceNotFound, readable but out of action scope is
// ErrInterfaceForbidden.
func resolveInterfaceScoped(ctx context.Context, q querier, id string, read, action scope.Set) (*Interface, error) {
	it, err := loadInterface(ctx, q, id)
	if err != nil {
		return nil, err
	}
	readable, err := componentInScope(ctx, q, it.Component, read)
	if err != nil {
		return nil, err
	}
	if !readable {
		return nil, ErrInterfaceNotFound
	}
	actionable, err := componentInScope(ctx, q, it.Component, action)
	if err != nil {
		return nil, err
	}
	if !actionable {
		return nil, ErrInterfaceForbidden
	}
	return it, nil
}

// nullableJSON passes a jsonb patch field through as a coalesce arg: an empty
// slice becomes a SQL NULL so the coalesce keeps the existing value, a non-empty
// slice is the new jsonb.
func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func mapInterfaceWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrInterfaceExists
		case "23503": // foreign_key_violation
			switch pgErr.ConstraintName {
			case "interface_type_fkey":
				return ErrUnknownInterfaceType
			case "interface_node_name_fkey":
				return ErrInterfaceNodeNotFound
			}
		}
	}
	return fmt.Errorf("storage: interface write: %w", err)
}
