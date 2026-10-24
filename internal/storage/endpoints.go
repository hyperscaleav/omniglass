package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hyperscaleav/omniglass/internal/collection/rejection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"regexp"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
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
	// ErrEndpointPatchInvalid refuses an edit naming the field: a value the
	// driver's declared inputs refuse, an undeclared input, a transport the
	// driver does not speak, or a raw target on a driver-attached endpoint.
	ErrEndpointPatchInvalid = errors.New("storage: endpoint patch invalid")
	// ErrEndpointParamsInvalid refuses a bare endpoint's create whose params
	// carry a value its transport's dialer cannot read (a tls mode that is
	// not a mode), naming the field.
	ErrEndpointParamsInvalid = errors.New("storage: endpoint params invalid")
)

// checkBareParams reads what a bare endpoint's params hand its transport's
// dialer with meaning, refusing a value it cannot use: a tls endpoint's mode
// (#1035), which a driver-attached one has checked as an input instead.
func checkBareParams(transportName string, params []byte) error {
	if (transportName != "tls" && transportName != "udp") || len(params) == 0 {
		return nil
	}
	var p map[string]any
	if err := json.Unmarshal(params, &p); err != nil {
		return nil // params are passed through as given; their shape is not this check's
	}
	if transportName == "udp" {
		return checkListenPort(p)
	}
	raw, ok := p[driver.TLSModeInput]
	if !ok {
		return nil
	}
	mode, isString := raw.(string)
	if _, err := driver.ParseTLSMode(mode); err != nil || !isString {
		return fmt.Errorf("params.%s must be one of %s, and %v is not", driver.TLSModeInput, strings.Join(driver.TLSModes(), ", "), raw)
	}
	return nil
}

// checkListenPort refuses a bare udp endpoint's listen port the node could
// not bind (#1037), through the one reading the node itself uses.
func checkListenPort(p map[string]any) error {
	raw, ok := p[driver.ListenPortInput]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("params.%s: %w", driver.ListenPortInput, err)
	}
	if _, err := driver.ParseListenPort(b); err != nil {
		return fmt.Errorf("params.%s must be a port, 1 to 65535, and %v is not", driver.ListenPortInput, raw)
	}
	return nil
}

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
	// SecretRead and CanAdmin fence the attach's secret-reference resolution to
	// what the CALLER may read: a reference resolves only to a secret in this
	// read scope, and an admin-sensitive one only with the admin tier. Without
	// them a bare-name lookup would let an operator bind (and the worklist
	// deliver) any tenant's credential; the API tier fills them from
	// secret:read.
	SecretRead scope.Set
	CanAdmin   bool
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
	// Inputs patches a driver-attached endpoint's inputs in place (#1061):
	// the host, the port, the attached secret references, any declared
	// input. Merged over the stored ones and validated against the driver.
	Inputs map[string]string
	// Transport restates the transport; a driver-attached endpoint takes
	// only a transport its driver speaks.
	Transport *string
	// SecretRead and CanAdmin are the caller's secret scope and admin tier:
	// an edit re-resolves every attached secret under them, so it cannot
	// send a credential the caller could not use to a new address.
	SecretRead scope.Set
	CanAdmin   bool
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
	it, err := p.createEndpointTx(ctx, tx, actorID, spec, create)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create endpoint: %w", err)
	}
	return it, nil
}

// createEndpointTx is CreateEndpoint inside a transaction the caller owns, so a composite write (adding
// a simulated system) can create it alongside the rest and commit once.
func (p *PG) createEndpointTx(ctx context.Context, tx pgx.Tx, actorID string, spec EndpointSpec, create scope.Set) (*Endpoint, error) {

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
		plan, err = p.resolveAttach(ctx, tx, spec)
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
	if plan == nil {
		if err := checkBareParams(spec.Transport, params); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEndpointParamsInvalid, err)
		}
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
	return it, nil
}

// UpdateEndpoint patches an endpoint's node placement, params, label, or (on a
// driver-attached endpoint, #1061) its inputs in place, with the read-then-action
// scope split (both evaluated against the owning component)
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
	// Lock the row before reading what an edit merges over: two concurrent
	// edits (one the host, one the port) must not each merge over the same
	// stale inputs and lose one. FOR NO KEY UPDATE does not conflict with a
	// driver update's FOR KEY SHARE re-derive (#823).
	if _, err := tx.Exec(ctx, `select 1 from endpoint where id = $1 for no key update`, before.ID); err != nil {
		return nil, fmt.Errorf("storage: lock endpoint %q: %w", before.ID, err)
	}
	if before, err = loadEndpoint(ctx, tx, before.ID); err != nil {
		return nil, err
	}
	// A driver-attached endpoint's params are DERIVED from its inputs (its host
	// and port), and its credential rides the worklist to the placed node. Its
	// address therefore edits through its inputs (#1061), every attached secret
	// re-resolved under the editor's own scope, so an edit cannot send a
	// credential its editor could not use to a new address; a raw target patch
	// on it refuses.
	params, inputs, err := p.patchedConnection(ctx, tx, before, patch)
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
			inputs    = coalesce($6, inputs),
			label     = case when $5::boolean then $4 else label end,
			updated_at = now()
		where id = $1
		returning `+endpointCols,
		before.ID, nodeID, nullableJSON(params), labelVal, setLabel, nullableJSON(inputs)))
	if err != nil {
		return nil, mapEndpointWriteErr(err)
	}
	// An edited driver-attached endpoint re-derives its tasks in this
	// transaction, through the one derivation the driver update uses (#823).
	if before.DriverID != nil && (inputs != nil || patch.Transport != nil) {
		if err := p.rederiveAttached(ctx, tx, *before.DriverID, before.ID); err != nil {
			return nil, err
		}
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "endpoint", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update endpoint: %w", err)
	}
	return after, nil
}

// patchedConnection computes an edit's new params and inputs (nil for
// unchanged). A bare endpoint takes params as given and refuses inputs. A
// driver-attached one (#1061) takes its inputs in place, merged over the
// stored ones and re-resolved against the driver's declared inputs under
// the caller's secret scope, its target derived from them; a params patch
// on it may set only the timeout, and a transport only the one its driver
// speaks.
func (p *PG) patchedConnection(ctx context.Context, tx pgx.Tx, before *Endpoint, patch EndpointPatch) (params, inputs []byte, err error) {
	if before.DriverID == nil {
		if len(patch.Inputs) > 0 {
			return nil, nil, fmt.Errorf("%w: inputs: a bare endpoint has no driver inputs; set its params", ErrEndpointPatchInvalid)
		}
		if patch.Transport != nil && *patch.Transport != before.Transport {
			return nil, nil, fmt.Errorf("%w: transport: an endpoint is named by its transport, which is fixed", ErrEndpointPatchInvalid)
		}
		if err := checkBareParams(before.Transport, patch.Params); err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrEndpointPatchInvalid, err)
		}
		return patch.Params, nil, nil
	}
	d, err := loadDriverRef(ctx, tx, *before.DriverID)
	if err != nil {
		return nil, nil, err
	}
	sp, err := driver.Parse(d.Spec)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: stored spec for driver %q does not parse: %w", d.Name, err)
	}
	if patch.Transport != nil && *patch.Transport != before.Transport {
		return nil, nil, fmt.Errorf("%w: transport: the endpoint speaks %s, the one transport driver %q declares", ErrEndpointPatchInvalid, before.Transport, d.Name)
	}
	var current map[string]any
	if len(patch.Params) > 0 {
		var extra map[string]any
		if err := json.Unmarshal(patch.Params, &extra); err != nil {
			return nil, nil, fmt.Errorf("%w: params: %v", ErrEndpointPatchInvalid, err)
		}
		for k, v := range extra {
			if k != "timeout" {
				return nil, nil, fmt.Errorf("%w: params.%s: a driver-attached endpoint's address is derived from its host and port inputs; patch the inputs", ErrEndpointPatchInvalid, k)
			}
			if v == nil {
				continue // null clears the timeout
			}
			str, ok := v.(string)
			if d, err := time.ParseDuration(str); !ok || err != nil || d <= 0 {
				return nil, nil, fmt.Errorf("%w: params.timeout must be a positive duration such as \"5s\", and %v is not", ErrEndpointPatchInvalid, v)
			}
		}
		current = extra
	}
	if len(patch.Inputs) == 0 && current == nil {
		return nil, nil, nil
	}
	stored := map[string]string{}
	if len(before.Inputs) > 0 {
		if err := json.Unmarshal(before.Inputs, &stored); err != nil {
			return nil, nil, fmt.Errorf("storage: stored inputs of endpoint %q: %w", before.ID, err)
		}
	}
	// An input the driver no longer declares (a driver update dropped it) is
	// dead to the derivation and never blocks an edit; an empty value clears
	// an input (its default, if any, fills it again; a required one refuses).
	declared := map[string]bool{}
	for _, in := range sp.Inputs {
		declared[in.Name] = true
	}
	for k := range stored {
		if !declared[k] {
			delete(stored, k)
		}
	}
	for k, v := range patch.Inputs {
		if strings.TrimSpace(v) == "" {
			delete(stored, k)
			if !declared[k] {
				return nil, nil, fmt.Errorf("%w: input %q is not declared by driver %q", ErrEndpointPatchInvalid, k, d.Name)
			}
			for _, in := range sp.Inputs {
				if in.Name == k && in.Required {
					return nil, nil, fmt.Errorf("%w: required input %q cannot be cleared", ErrEndpointPatchInvalid, k)
				}
			}
			continue
		}
		stored[k] = v
	}
	effective, err := p.resolveInputs(ctx, tx, d, sp, stored, patch.SecretRead, patch.CanAdmin)
	if err != nil {
		if errors.Is(err, ErrAttachInvalid) {
			return nil, nil, fmt.Errorf("%w: %s", ErrEndpointPatchInvalid, strings.TrimPrefix(err.Error(), ErrAttachInvalid.Error()+": "))
		}
		return nil, nil, err
	}
	derived := map[string]any{}
	for k, v := range attachParams(effective) {
		derived[k] = v
	}
	var old map[string]any
	_ = json.Unmarshal(before.Params, &old)
	if t, ok := old["timeout"]; ok {
		derived["timeout"] = t
	}
	for k, v := range current {
		if v == nil {
			delete(derived, k)
			continue
		}
		derived[k] = v
	}
	if params, err = json.Marshal(derived); err != nil {
		return nil, nil, err
	}
	if inputs, err = json.Marshal(effective); err != nil {
		return nil, nil, err
	}
	return params, inputs, nil
}

// rederiveAttached re-derives one attached endpoint's tasks from its
// driver's current spec.
func (p *PG) rederiveAttached(ctx context.Context, tx pgx.Tx, driverID, endpointID string) error {
	d, err := loadDriverRef(ctx, tx, driverID)
	if err != nil {
		return err
	}
	sp, err := driver.Parse(d.Spec)
	if err != nil {
		return fmt.Errorf("storage: stored spec for driver %q does not parse: %w", d.Name, err)
	}
	cat, err := loadDriverCatalog(ctx, tx)
	if err != nil {
		return err
	}
	return rederiveEndpoint(ctx, tx, endpointID, d, sp, cat)
}

// RetryEndpoint records an operator's retry of the endpoint's held session
// (#1049): it increments the endpoint's session_retry counter, carried on
// the worklist and part of the endpoint's connection fingerprint (ADR-0158),
// so the gate a terminal refusal holds lifts and the node attempts the
// endpoint again. The read/action split
// runs through the owning component like every endpoint write, and the
// retry is audited in the same transaction.
//
// pin, when set, accepts a changed key: a host key on an ssh endpoint
// (#1032), a certificate's public key on a tls endpoint (#1035). It lands as
// the endpoint's declared ssh-host-key or tls-certificate, the pin the
// worklist delivers ahead of the key trusted on first use, so the node's
// session redials onto it. Each only on its transport, and only a SHA256
// fingerprint. While the endpoint's gate names an offered key (a key
// refusal's evidence, #1078), the key must be that key: any other is
// ErrRetryHostKeyStale and changes nothing, so a trust confirmed against a
// view the record has since moved past cannot pin a key nobody read.
// The audit row carries the pin replaced (old) and the pin set (new).
func (p *PG) RetryEndpoint(ctx context.Context, actorID, id string, pin RetryPin, read, action scope.Set) (*Endpoint, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin retry endpoint: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveEndpointScoped(ctx, tx, id, read, action)
	if err != nil {
		return nil, err
	}
	key, err := pin.keyFor(before)
	if err != nil {
		return nil, err
	}
	// replaced is the pin a trusted key replaces, for the audit row.
	replaced := ""
	if key != "" {
		published, err := latestRejection(ctx, tx, *before.ComponentID, before.Name)
		if err != nil {
			return nil, err
		}
		if gate, ok := rejection.Parse(published); ok && gate.HasKeys() && gate.OfferedKey != key {
			// The gate is the node's last word on what the component offers: a
			// gated node dials no more, so the component may have moved on
			// since. The refusal says what the record names, and how to read
			// the key again, never that the component offers it now.
			return nil, fmt.Errorf("%w: the refusal on record names the offered key %s, not the key sent; to trust another, retry the endpoint without a host key so the node reads the key the component offers now, then trust that one", ErrRetryHostKeyStale, gate.OfferedKey)
		}
		if _, replaced, err = keyPin(ctx, tx, *before.ComponentID, before.Name, pinKeyOf[before.Transport]); err != nil {
			return nil, err
		}
		// A pin (the ssh host key, a tls certificate's public key) is the one
		// declared row a node-probe key admits, and only through this path
		// (ADR-0161).
		if err := admitWrites(ctx, tx, writeProperty, gatewayWriter(writerclass.KeyPin),
			[]keyedWrite{{key: pinKeyOf[before.Transport], instance: before.Name}}); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			insert into property (owner_kind, component_id, property_type_id, instance, provenance, value)
			values ('component', $1::uuid, (select id from property_type where name = $4), $2, '`+declaredProvenance+`', to_jsonb($3::text))`,
			*before.ComponentID, before.Name, key, pinKeyOf[before.Transport]); err != nil {
			return nil, fmt.Errorf("storage: pin a key for endpoint %q: %w", before.ID, err)
		}
	}
	// The counter, not updated_at: an unrelated edit (a label) moves
	// updated_at and must leave a rejected session alone (#1056 ruling).
	after, err := scanEndpoint(tx.QueryRow(ctx, `
		update endpoint set session_retry = session_retry + 1
		where id = $1
		returning `+endpointCols, before.ID))
	if err != nil {
		return nil, mapEndpointWriteErr(err)
	}
	type pinned struct {
		*Endpoint
		HostKey     string `json:"host_key,omitempty"`
		Certificate string `json:"certificate,omitempty"`
	}
	audited, prior := any(after), any(before)
	switch {
	case key != "" && before.Transport == "tls":
		audited, prior = pinned{Endpoint: after, Certificate: key}, pinned{Endpoint: before, Certificate: replaced}
	case key != "":
		audited, prior = pinned{Endpoint: after, HostKey: key}, pinned{Endpoint: before, HostKey: replaced}
	}
	if err := writeAuditRes(ctx, tx, actorID, "retry", "endpoint", after.ID, prior, audited); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit retry endpoint: %w", err)
	}
	return after, nil
}

// RetryPin is a key an operator's retry accepts as the endpoint's new pin.
// HostKey names the key the endpoint's transport pins, whichever it is (an
// ssh host key, a tls certificate's public key), which is the field a key
// refusal's trust-this-key action sends (#1078); Certificate names a tls
// endpoint's alone. Both empty retries with the pin unchanged.
type RetryPin struct {
	HostKey     string
	Certificate string
}

// keyFor resolves the one key a retry pins on the endpoint, refusing a key
// its transport does not pin, two different keys, or a value that is not a
// SHA256 fingerprint.
func (p RetryPin) keyFor(ep *Endpoint) (string, error) {
	if p.HostKey == "" && p.Certificate == "" {
		return "", nil
	}
	if p.Certificate != "" && ep.Transport != "tls" {
		return "", fmt.Errorf("%w: only a tls endpoint pins a certificate key", ErrRetryHostKey)
	}
	if p.HostKey != "" && p.Certificate != "" && p.HostKey != p.Certificate {
		return "", fmt.Errorf("%w: host_key and certificate name different keys; send the one key to trust", ErrRetryHostKey)
	}
	key := p.HostKey
	if key == "" {
		key = p.Certificate
	}
	if _, pinned := pinKeyOf[ep.Transport]; !pinned || ep.ComponentID == nil {
		return "", fmt.Errorf("%w: only an ssh or tls endpoint pins a key", ErrRetryHostKey)
	}
	if !hostKeyFingerprint.MatchString(key) {
		return "", fmt.Errorf("%w: a key is a SHA256 fingerprint (SHA256: and 43 base64 characters)", ErrRetryHostKey)
	}
	return key, nil
}

// ErrRetryHostKey refuses a retry's key: one the endpoint's transport does
// not pin, two different keys at once, or not a SHA256 fingerprint.
var ErrRetryHostKey = errors.New("storage: retry host key")

// ErrRetryHostKeyStale refuses a retry's key that is not the key the
// endpoint's gate names as offered (#1078): the operator confirmed a key the
// refusal on record no longer names.
var ErrRetryHostKeyStale = errors.New("storage: retry host key stale")

// latestRejection is the endpoint-rejection the node last published for an
// endpoint (its gate), or "" when none was.
func latestRejection(ctx context.Context, q querier, componentID, instance string) (string, error) {
	var v string
	if err := q.QueryRow(ctx, `
		select coalesce((select value #>> '{}' from property
		 where owner_kind = 'component' and component_id = $1::uuid and instance = $2 and provenance = 'observed'
		   and property_type_id = (select id from property_type where name = 'endpoint-rejection')
		 order by ts desc, id desc limit 1), '')`, componentID, instance).Scan(&v); err != nil {
		return "", fmt.Errorf("storage: endpoint rejection: %w", err)
	}
	return v, nil
}

// EndpointKeyPin is the key an ssh or tls endpoint pins in force (keyPin):
// the operator's declared pin, else the key the node trusted on first use, ""
// for none or for a transport that pins nothing. An unknown component folds
// into "" as LatestProperty's does.
func (p *PG) EndpointKeyPin(ctx context.Context, componentRef, endpoint, transport string) (string, error) {
	property, ok := pinKeyOf[transport]
	if !ok {
		return "", nil
	}
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	_, pin, err := keyPin(ctx, p.pool, c.ID, endpoint, property)
	return pin, err
}

// keyPin is an endpoint's key pin under its property (ssh-host-key, #1032;
// tls-certificate, #1035): the operator's declared key, and the pin in force
// (the declared key, else the key the node trusted on first use).
func keyPin(ctx context.Context, q querier, componentID, instance, property string) (declared, pin string, err error) {
	if err := q.QueryRow(ctx, `
		select coalesce(
			(select nullif(value, 'null'::jsonb) #>> '{}' from property
			 where owner_kind = 'component' and component_id = $1::uuid and instance = $2 and provenance = 'declared'
			   and property_type_id = (select id from property_type where name = $3)
			 order by id desc limit 1), ''),
			coalesce((select value #>> '{}' from property
			 where owner_kind = 'component' and component_id = $1::uuid and instance = $2 and provenance = 'observed'
			   and property_type_id = (select id from property_type where name = $3)
			 order by ts desc, id desc limit 1), '')`, componentID, instance, property).Scan(&declared, &pin); err != nil {
		return "", "", fmt.Errorf("storage: %s pin: %w", property, err)
	}
	if declared != "" {
		pin = declared
	}
	return declared, pin, nil
}

var hostKeyFingerprint = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

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
