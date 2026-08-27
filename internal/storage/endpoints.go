package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Endpoint-layer sentinel errors. An endpoint is not a scope-tree entity of its
// own; it hangs off a component (endpoint.component), so its scope cascades
// through that component (see componentInScope). NotFound doubles as the
// non-disclosing "out of read scope"; Forbidden is readable-but-not-actionable.
var (
	ErrEndpointNotFound          = errors.New("storage: endpoint not found")
	ErrEndpointForbidden         = errors.New("storage: action not permitted on this endpoint")
	ErrEndpointExists            = errors.New("storage: endpoint name already exists on this component")
	ErrUnknownTransport          = errors.New("storage: unknown transport")
	ErrEndpointComponentNotFound = errors.New("storage: endpoint component not found")
	ErrEndpointNodeNotFound      = errors.New("storage: endpoint node not found")
)

// Endpoint is a named, placement-bound connection: Transport names the wire it
// speaks over (a code-registry fact, internal/transport, ADR-0073), Component
// is the owner (nil for a server-hosted endpoint), Node is the server-assigned
// placement (nil until assigned), Params is the address/target jsonb. ID is the
// surrogate primary key (a uuidv7); Name is the friendly address, unique within
// the owning component.
type Endpoint struct {
	ID          string
	Name        string
	Label       string // the friendly string an operator reads; empty falls back to Name
	Transport   string
	Component   *string
	ComponentID *string
	Node        *string
	NodeID      *string
	Params      []byte
	// Driver and DriverID record the attachment that authored this endpoint
	// (#813): nil for a bare probe endpoint. Inputs is the effective input set
	// supplied at attach, defaults baked, secret inputs by reference name.
	Driver    *string
	DriverID  *string
	Inputs    []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

// EndpointSpec is the create input. The endpoint is transport-named: its name
// is DERIVED from the transport it speaks, unique within the owning component,
// never operator-typed. The id is server-generated. Transport must name a
// registry entry (internal/transport); an unknown one refuses at write.
//
// Which makes Label the ONE operator-typed identity string an endpoint has, and
// the reason it is here rather than only on the patch: on a component with three
// `ssh` endpoints the names collide by construction, so an endpoint that could
// only be labelled by a following call is one an operator cannot tell apart at
// the moment they make it (D2, #613).
type EndpointSpec struct {
	Transport string
	Label     string
	Component *string
	Node      *string
	Params    []byte
	// Driver switches the create into an attach (#813): the transport comes
	// from the driver's spec (setting both refuses), Inputs must satisfy the
	// spec's declared inputs, and the spec's functions derive the endpoint's
	// tasks in the same transaction.
	Driver *string
	Inputs map[string]string
}

// EndpointPatch is the update input: nil fields unchanged. Type and component
// rebind are deferred (they cross the adapter kind and the scope boundary),
// mirroring the component tier's deferred reparent; the operationally useful
// Node (re)assignment and Params retarget move here.
type EndpointPatch struct {
	Node   *string
	Params []byte
	// Label follows the platform's patch convention: nil leaves it alone, an
	// empty string clears it. It is the only identity field an operator may
	// move here, and moving it moves nothing else: the name stays derived.
	Label *string
}

// endpointCols is the bare select list (scan order), for the un-aliased
// insert/update RETURNING and the by-id load; endpointColsJoin is the same
// list aliased to `i` for the scoped join over the component table.
// The two arcs store primary keys (component.id, node.principal_id), so each is
// selected alongside a scalar subquery for the owner's current name: the id is
// what the row points at, the name is what an operator reads and types. The
// subquery form works identically in a plain select and in a RETURNING list, so
// the insert and update paths need no join. Both derived columns are aliased,
// since an unaliased `... c.name ...` would emit a second output column called
// `name` and make `order by name` ambiguous.
const (
	endpointCols = `id, name, coalesce(label, ''), transport,
		(select c.name from component c where c.id = endpoint.component) as component_name, component,
		(select n.name from node n where n.principal_id = endpoint.node_name) as node_name_ref, node_name,
		params,
		(select d.name from driver d where d.id = endpoint.driver_id) as driver_name, driver_id, inputs,
		created_at, updated_at`
	endpointColsJoin = `i.id, i.name, coalesce(i.label, ''), i.transport,
		(select c2.name from component c2 where c2.id = i.component) as component_name, i.component,
		(select n.name from node n where n.principal_id = i.node_name) as node_name_ref, i.node_name,
		i.params,
		(select d2.name from driver d2 where d2.id = i.driver_id) as driver_name, i.driver_id, i.inputs,
		i.created_at, i.updated_at`
)

func scanEndpoint(row pgx.Row) (*Endpoint, error) {
	var it Endpoint
	if err := row.Scan(&it.ID, &it.Name, &it.Label, &it.Transport, &it.Component, &it.ComponentID, &it.Node, &it.NodeID, &it.Params, &it.Driver, &it.DriverID, &it.Inputs, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, err
	}
	return &it, nil
}

// componentInScope reports whether an endpoint/task owning componentID is
// inside a component-tier scope, the cascade both entities share: an all scope
// always holds; a nil owner (a server-hosted endpoint with no component) is in
// scope ONLY for an all scope (there is no component to cascade through, so a
// component-scoped operator cannot reach it); otherwise the component's row is
// checked against the scope subtree via inScopeTree on the component table.
// Takes the id directly, not the name endpointCols also projects for display
// (Endpoint.Component): the two are read off the same endpoint.component
// column in one query, so passing the id here needs no lookup at all, and
// resolving from the name instead would risk landing on a different row
// sharing it once #627 lands.
func componentInScope(ctx context.Context, q querier, componentID *string, set scope.Set) (bool, error) {
	if set.All {
		return true, nil
	}
	if componentID == nil {
		return false, nil
	}
	return inScopeTree(ctx, q, componentTable, *componentID, set)
}

// endpointComponentID resolves an optional component reference (a name or a
// uuid) to the component id the arc stores, within the caller's create scope
// (resolveScopedRef, ruling 2, #627: ambiguity is judged inside create, not
// fleet-wide, so its one caller no longer needs a separate inScopeTree check
// afterward, and never learns an out-of-scope component's uuid from a
// collision). A nil reference stays nil (a server-hosted endpoint owns no
// component); an absent one is the 422 not-found sentinel, one that exists
// only outside create scope is the 403 forbidden sentinel (preserved from the
// pre-#627 two-step resolve, not collapsed into not-found: the caller supplied
// this reference itself in the same request).
func endpointComponentID(ctx context.Context, q querier, ref *string, create scope.Set) (*string, error) {
	if ref == nil {
		return nil, nil
	}
	c, err := resolveScopedRef(ctx, q, componentConfig, *ref, "endpoint", create)
	switch {
	case errors.Is(err, ErrComponentNotFound):
		return nil, ErrEndpointComponentNotFound
	case errors.Is(err, ErrComponentForbidden):
		return nil, ErrEndpointForbidden
	case err != nil:
		return nil, err
	}
	return &c.ID, nil
}

// endpointNodeID resolves an optional node reference (a name or a principal id)
// to the principal id the placement arc stores. An unknown node is
// ErrNodeNotFound, so an unassignable placement fails loudly.
func endpointNodeID(ctx context.Context, q querier, ref *string) (*string, error) {
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

// loadEndpoint reads one endpoint by id with no scope check; callers layer
// scope on top (via componentInScope).
func loadEndpoint(ctx context.Context, q querier, id string) (*Endpoint, error) {
	it, err := scanEndpoint(q.QueryRow(ctx, `select `+endpointCols+` from endpoint where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEndpointNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: load endpoint %q: %w", id, err)
	}
	return it, nil
}

// ListEndpoints returns the endpoints whose owning component is in the caller's
// read scope, ordered by the label an operator reads with the unlabelled last
// and the derived name breaking ties (D4, #613); both arms order the same way. A component-scoped read (the cascade) expands the
// component subtree and matches endpoints joined onto it; an all read returns
// every endpoint (including component-less ones); an empty scope returns none.
func (p *PG) ListEndpoints(ctx context.Context, read scope.Set) ([]Endpoint, error) {
	if read.Empty() {
		return nil, nil
	}
	var (
		rows pgx.Rows
		err  error
	)
	if read.All {
		rows, err = p.pool.Query(ctx, `select `+endpointCols+` from endpoint order by label nulls last, name`)
	} else {
		roots := uuidRoots(read.IDs)
		selfIDs := uuidRoots(read.SelfIDs)
		if len(roots) == 0 && len(selfIDs) == 0 {
			return nil, nil
		}
		// The component subtree walk (read never excludes a root, so no exclude arm),
		// joined onto endpoint by its owning component; a component-less endpoint has
		// no join match and stays hidden outside an all scope.
		rows, err = p.pool.Query(ctx, `
			with recursive sub(id) as (
				select id from component where id = any($1::uuid[])
				union all
				select c.id from component c join sub on c.parent_id = sub.id
			) cycle id set is_cycle using path
			select `+endpointColsJoin+` from endpoint i
			join component c on c.id = i.component
			where c.id in (select id from sub) or c.id = any($2::uuid[])
			order by i.label nulls last, i.name`, roots, selfIDs)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: list endpoints: %w", err)
	}
	defer rows.Close()
	var out []Endpoint
	for rows.Next() {
		it, err := scanEndpoint(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan endpoint: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// GetEndpoint resolves an endpoint by id within the caller's read scope;
// absent or out of scope is the same non-disclosing ErrEndpointNotFound.
func (p *PG) GetEndpoint(ctx context.Context, id string, read scope.Set) (*Endpoint, error) {
	it, err := loadEndpoint(ctx, p.pool, id)
	if err != nil {
		return nil, err
	}
	in, err := componentInScope(ctx, p.pool, it.ComponentID, read)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrEndpointNotFound
	}
	return it, nil
}

// CreateEndpoint inserts an endpoint owned by an optional component, placed on
// an optional node, writing the audit row in the same transaction. The create
// scope is checked against the owning component (the cascade): a component-less
// endpoint requires an all create scope; a component-bound one requires that
// component in the create scope. A missing component is a 422, an out-of-scope
// one a 403, mirroring the component tier's parent-placement split.
func (p *PG) CreateEndpoint(ctx context.Context, actorID string, spec EndpointSpec, create scope.Set) (*Endpoint, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create endpoint: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once, via endpointComponentID (uuid-or-name, ADR-0062,
	// resolved inside create scope per ruling 2, #627), and reused for the
	// insert's FK value: the old code resolved spec.Component a second time
	// here, via its own inline `select id from component where name = $1`,
	// which is exactly the shape that raises SQLSTATE 21000 the moment two
	// components share a name. No separate inScopeTree check follows:
	// endpointComponentID already refused a component outside create scope
	// (as the component's own not-found sentinel, never surfacing an
	// out-of-scope uuid), so a resolved non-nil componentID is always
	// in-scope by construction.
	componentID, err := endpointComponentID(ctx, tx, spec.Component, create)
	if err != nil {
		return nil, err
	}
	if componentID == nil && !create.All {
		return nil, ErrEndpointForbidden
	}

	// A driver reference switches the create into an attach (#813): the spec
	// derives the transport, the params, and (after the insert) the tasks.
	var plan *attachPlan
	if spec.Driver != nil {
		plan, err = resolveAttach(ctx, tx, spec)
		if err != nil {
			return nil, err
		}
		spec.Transport = plan.transport
		spec.Params = plan.params
	}

	params := spec.Params
	if len(params) == 0 {
		params = []byte("{}")
	}
	inputs := []byte("{}")
	var driverID *string
	if plan != nil {
		inputs = plan.inputs
		driverID = &plan.driverID
	}
	nodeID, err := endpointNodeID(ctx, tx, spec.Node)
	if err != nil {
		return nil, err
	}
	// The transport is a code-registry fact, so an unknown one refuses here,
	// at write, rather than surfacing as a null FK the way the retired
	// interface_type lookup used to.
	if _, ok := transport.ByName(spec.Transport); !ok {
		return nil, ErrUnknownTransport
	}
	it, err := scanEndpoint(tx.QueryRow(ctx, `
		insert into endpoint (name, transport, component, node_name, params, label, driver_id, inputs)
		values ($1, $1, $2, $3, $4, $5, $6, $7)
		returning `+endpointCols,
		spec.Transport, componentID, nodeID, params, labelOrNull(spec.Label), driverID, inputs))
	if err != nil {
		return nil, mapEndpointWriteErr(err)
	}
	// The endpoint is transport-named: its name IS its transport (unique per
	// component). Deriving the reachability poll task here, the node's unit of work
	// over this connection, makes task a derived artifact, never operator-authored.
	if err := deriveReachabilityTask(ctx, tx, it.ID); err != nil {
		return nil, err
	}
	// An attach also derives the spec's functions: a poll task per poll
	// function, a standing listen task per listener, lanes baked.
	if plan != nil {
		if err := deriveDriverTasks(ctx, tx, it.ID, plan); err != nil {
			return nil, err
		}
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "endpoint", it.ID, nil, it); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create endpoint: %w", err)
	}
	return it, nil
}

// UpdateEndpoint patches an endpoint's node placement, params or label with
// the read-then-action scope split (both evaluated against the owning component)
// and in-transaction audit. Type and component rebind are deferred, and the name
// is not patchable at all: it is derived from the type, so the label is the only
// identity string this route moves.
func (p *PG) UpdateEndpoint(ctx context.Context, actorID, id string, patch EndpointPatch, read, action scope.Set) (*Endpoint, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update endpoint: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveEndpointScoped(ctx, tx, id, read, action)
	if err != nil {
		return nil, err
	}
	nodeID, err := endpointNodeID(ctx, tx, patch.Node)
	if err != nil {
		return nil, err
	}
	setLabel, labelVal := labelPatch(patch.Label)
	after, err := scanEndpoint(tx.QueryRow(ctx, `
		update endpoint set
			node_name = coalesce($2, node_name),
			params    = coalesce($3, params),
			label     = case when $5::boolean then $4 else label end,
			updated_at = now()
		where id = $1
		returning `+endpointCols,
		before.ID, nodeID, nullableJSON(patch.Params), labelVal, setLabel))
	if err != nil {
		return nil, mapEndpointWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "endpoint", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update endpoint: %w", err)
	}
	return after, nil
}

// DeleteEndpoint removes an endpoint by id with the read/action split (through
// the owning component) and in-transaction audit. Its derived tasks cascade-delete
// with it (task.interface_id ON DELETE CASCADE).
func (p *PG) DeleteEndpoint(ctx context.Context, actorID, id string, read, action scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete endpoint: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveEndpointScoped(ctx, tx, id, read, action)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from endpoint where id = $1`, before.ID); err != nil {
		return mapEndpointWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "endpoint", before.ID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete endpoint: %w", err)
	}
	return nil
}

// resolveEndpointScoped loads an endpoint and enforces the read-then-action
// scope split through its owning component: out of read scope is the
// non-disclosing ErrEndpointNotFound, readable but out of action scope is
// ErrEndpointForbidden.
func resolveEndpointScoped(ctx context.Context, q querier, id string, read, action scope.Set) (*Endpoint, error) {
	it, err := loadEndpoint(ctx, q, id)
	if err != nil {
		return nil, err
	}
	readable, err := componentInScope(ctx, q, it.ComponentID, read)
	if err != nil {
		return nil, err
	}
	if !readable {
		return nil, ErrEndpointNotFound
	}
	actionable, err := componentInScope(ctx, q, it.ComponentID, action)
	if err != nil {
		return nil, err
	}
	if !actionable {
		return nil, ErrEndpointForbidden
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

func mapEndpointWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrEndpointExists
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "endpoint_node_name_fkey" {
				return ErrEndpointNodeNotFound
			}
		}
	}
	return fmt.Errorf("storage: endpoint write: %w", err)
}
