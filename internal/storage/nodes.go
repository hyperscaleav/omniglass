package storage

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Node-layer sentinel errors. A node is the edge runtime; it is fleet-wide
// (not tree-scoped), so its create/enroll/read paths require an all scope,
// mirroring principals.
var (
	ErrNodeNotFound      = errors.New("storage: node not found")
	ErrNodeExists        = errors.New("storage: node name already exists")
	ErrNodeForbidden     = errors.New("storage: action not permitted on nodes")
	ErrEnrollmentInvalid = errors.New("storage: enrollment token invalid")
	ErrInvalidNodeName   = errors.New("storage: node name is not a valid subject token")
)

// Node is the edge runtime's server-side record: the detail row of its
// kind='node' principal. PrincipalID is that principal's id. EnrolledAt is set
// the first time the node claims its identity; Enrolled is a convenience derived
// from it.
type Node struct {
	PrincipalID     string
	Name            string
	Label           string
	Description     string
	LocationName    *string
	LocationID      *string
	LastHeartbeatAt *time.Time
	EnrolledAt      *time.Time
	Enrolled        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NodeSpec is the create input. Label is the operator label (empty falls
// back to the name on read); LocationName is an optional descriptive placement.
type NodeSpec struct {
	Name         string
	Label        string
	Description  string
	LocationName *string
}

// NodePatch is the update input: a nil field is left unchanged. Name is not
// patchable (it is the immutable fleet address and enrollment identity). A
// LocationName pointing at "" clears the placement.
type NodePatch struct {
	Label        *string
	Description  *string
	LocationName *string
}

// WorklistTask is one enabled task resolved for a node: the content-addressed
// task plus the placement-bound endpoint it runs over. EndpointParams and Spec
// are raw jsonb passed through to the node. Secrets carries a driver task's
// unsealed secret inputs (input name to the secret's fields), resolved here so
// the node can present the credential to the device (#814); it is empty for
// every non-driver task.
type WorklistTask struct {
	ID             string
	Mode           string
	EndpointName   string
	Transport      string
	EndpointParams []byte
	Spec           []byte
	Secrets        map[string]map[string]string
}

// Worklist is a node's resolved work plus the config generation (the max
// endpoint updated_at across the node's endpoints, epoch seconds; 0 when the
// node has no endpoints). A steady generation lets the node serve from cache; a
// bump forces a refresh.
type Worklist struct {
	Tasks            []WorklistTask
	ConfigGeneration int64
}

// The placement arc stores location.id, so the name is read back beside it via a
// scalar subquery: the id is what the row points at, the name is what an operator
// reads and types. The subquery form works in a plain select and in a RETURNING
// list alike, so the insert and update paths need no join.
const nodeCols = `principal_id, name, coalesce(label, ''), description,
	(select l.name from location l where l.id = node.location_id) as location_name, location_id,
	last_heartbeat_at, enrolled_at, created_at, updated_at`

func scanNode(row pgx.Row) (*Node, error) {
	var n Node
	if err := row.Scan(&n.PrincipalID, &n.Name, &n.Label, &n.Description, &n.LocationName, &n.LocationID, &n.LastHeartbeatAt, &n.EnrolledAt, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	n.Enrolled = n.EnrolledAt != nil
	return &n, nil
}

// CreateNode inserts a node as a kind='node' principal plus its detail row,
// writing the audit row in the same transaction (mirroring the human/service
// create). A node is fleet-wide, so creation requires an all create scope (like
// a principal, unlike a tree-scoped location/system/component).
func (p *PG) CreateNode(ctx context.Context, actorID string, spec NodeSpec, create, locationRead scope.Set) (*Node, error) {
	if !create.All {
		return nil, ErrNodeForbidden
	}
	if err := ValidateName("node", spec.Name); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create node: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	locationID, err := nodeLocationID(ctx, tx, spec.LocationName, locationRead)
	if err != nil {
		return nil, err
	}
	var pid string
	if err := tx.QueryRow(ctx, `insert into principal (kind) values ('node') returning id`).Scan(&pid); err != nil {
		return nil, fmt.Errorf("storage: create node principal: %w", err)
	}
	n, err := scanNode(tx.QueryRow(ctx, `
		insert into node (principal_id, name, label, description, location_id)
		values ($1, $2, $3, $4, $5)
		returning `+nodeCols, pid, spec.Name, labelOrNull(spec.Label), spec.Description, locationID))
	if err != nil {
		return nil, mapNodeWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "node", n.PrincipalID, nil, n); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create node: %w", err)
	}
	return n, nil
}

// UpdateNode patches a node's label, description, and location (a nil
// field is left unchanged; a LocationName of "" clears the placement). name is
// not patched: it is the immutable fleet address and enrollment identity. A
// node is fleet-wide, so the update requires an all scope, like create. An
// unknown name is ErrNodeNotFound; an unknown location is ErrLocationNotFound.
func (p *PG) UpdateNode(ctx context.Context, actorID, name string, patch NodePatch, read, action, locationRead scope.Set) (*Node, error) {
	if err := RejectAddressForm("node", name); err != nil {
		return nil, err
	}
	if !read.All || !action.All {
		return nil, ErrNodeForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update node: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := scanNode(tx.QueryRow(ctx, `select `+nodeCols+` from node where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNodeNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: read node for update %q: %w", name, err)
	}
	locationID, err := nodeLocationID(ctx, tx, patch.LocationName, locationRead)
	if err != nil {
		return nil, err
	}
	setLabel, labelVal := labelPatch(patch.Label)
	after, err := scanNode(tx.QueryRow(ctx, `
		update node set
			label  = case when $6::boolean then $2 else label end,
			description   = coalesce($3, description),
			location_id   = case when $4 then $5 else location_id end,
			updated_at    = now()
		where name = $1
		returning `+nodeCols,
		name, labelVal, patch.Description, patch.LocationName != nil, locationID, setLabel))
	if err != nil {
		return nil, mapNodeWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "node", after.PrincipalID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update node: %w", err)
	}
	return after, nil
}

// DeleteNode decommissions a node: a hard delete of its kind='node' principal,
// which cascades the node detail row and, through it, everything keyed to the
// node, its interfaces and their derived tasks, its node-owned samples and tag
// bindings, and its enrollment credential (every referencing FK is ON DELETE
// CASCADE). A node is fleet-wide, so this requires an all scope, like create. An
// unknown name is ErrNodeNotFound. Audited before the row is gone; the actor is
// the deleter (unaffected by the cascade) and audit_log.resource_id is plain
// text, not a foreign key, so the deleted node's principal id survives there.
func (p *PG) DeleteNode(ctx context.Context, actorID, name string, read, action scope.Set) error {
	if err := RejectAddressForm("node", name); err != nil {
		return err
	}
	if !read.All || !action.All {
		return ErrNodeForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete node: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pid string
	err = tx.QueryRow(ctx, `select principal_id from node where name = $1`, name).Scan(&pid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNodeNotFound
	} else if err != nil {
		return fmt.Errorf("storage: delete node lookup %q: %w", name, err)
	}
	// The before image carries the name. Keying on the principal id is right (a rename
	// must not orphan the row), but without the image the row is unresolvable: the
	// principal is deleted in this same transaction and no API surface returns a
	// node's principal_id, so "which node was deleted" would have no answer at all.
	before := map[string]any{"name": name, "principal_id": pid}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "node", pid, before, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from principal where id = $1`, pid); err != nil {
		return fmt.Errorf("storage: delete node principal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete node: %w", err)
	}
	return nil
}

// SetEnrollmentToken installs the node's enrollment secret as a bearer
// credential ROW on its principal (the same machinery a service bearer token
// uses), taking the hex sha256 of a freshly minted token (the cleartext is shown
// once by the API and never stored). Re-enrolling replaces any existing bearer
// credential, so the previous token stops working. Audited. Requires an all
// action scope.
func (p *PG) SetEnrollmentToken(ctx context.Context, actorID, name, tokenHashHex string, action scope.Set) (*Node, error) {
	if err := RejectAddressForm("node", name); err != nil {
		return nil, err
	}
	if !action.All {
		return nil, ErrNodeForbidden
	}
	hash, err := hex.DecodeString(tokenHashHex)
	if err != nil {
		return nil, fmt.Errorf("storage: set enrollment token %q: bad hash: %w", name, err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set enrollment token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := scanNode(tx.QueryRow(ctx, `select `+nodeCols+` from node where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNodeNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: load node %q: %w", name, err)
	}
	// Replace any prior bearer credential so a re-enroll invalidates the old
	// token, then install the new one. The secret is stored only as its hash; the
	// prefix is the node name, a non-secret locator for scanners and audit.
	if _, err := tx.Exec(ctx,
		`delete from credential where principal_id = $1 and kind = 'bearer'`, before.PrincipalID); err != nil {
		return nil, fmt.Errorf("storage: clear node credential %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		before.PrincipalID, hash, name); err != nil {
		return nil, fmt.Errorf("storage: set node credential %q: %w", name, err)
	}
	after, err := scanNode(tx.QueryRow(ctx, `
		update node set updated_at = now()
		where name = $1
		returning `+nodeCols, name))
	if err != nil {
		return nil, fmt.Errorf("storage: set enrollment token %q: %w", name, err)
	}
	// The token hash itself is never written to the audit diff (it is a secret);
	// the audit records that an enroll happened on the node.
	if err := writeAuditRes(ctx, tx, actorID, "enroll", "node", after.PrincipalID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set enrollment token: %w", err)
	}
	return after, nil
}

// ClaimNode is the node-facing exchange: the node presents its enrollment token,
// and a bearer-credential match sets enrolled_at (first claim) and returns the
// node. No scope: the presented token is the authentication. A mismatch, an
// unenrolled node, or an unknown node is ErrEnrollmentInvalid (a claim must not
// disclose which nodes exist).
func (p *PG) ClaimNode(ctx context.Context, name, tokenHashHex string) (*Node, error) {
	pr, err := p.authenticateNodeCredential(ctx, name, tokenHashHex)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, ErrEnrollmentInvalid
	}
	// coalesce keeps the original enrolled_at on a re-claim (idempotent). Keyed by
	// the resolved principal id, so it stamps exactly the node that authenticated.
	n, err := scanNode(p.pool.QueryRow(ctx, `
		update node set enrolled_at = coalesce(enrolled_at, now()), updated_at = now()
		where principal_id = $1
		returning `+nodeCols, pr.ID))
	if err != nil {
		return nil, fmt.Errorf("storage: mark enrolled %q: %w", name, err)
	}
	return n, nil
}

// AuthenticateNode reports whether the presented token hash matches the node's
// bearer credential. The NATS auth callback calls this to admit a node
// connection; a non-match, an unenrolled node, or an unknown node is a clean
// false, not an error.
func (p *PG) AuthenticateNode(ctx context.Context, name, tokenHashHex string) (bool, error) {
	pr, err := p.authenticateNodeCredential(ctx, name, tokenHashHex)
	if err != nil {
		return false, err
	}
	return pr != nil, nil
}

// authenticateNodeCredential resolves the presented token hash to a bearer
// credential via the shared AuthenticateBearer helper and confirms the owning
// principal is the node of that name. It returns a nil principal (no error) when
// the hash matches no credential, the credential belongs to a non-node principal,
// or the node name does not match, so callers cannot use it to enumerate nodes.
func (p *PG) authenticateNodeCredential(ctx context.Context, name, tokenHashHex string) (*Principal, error) {
	hash, err := hex.DecodeString(tokenHashHex)
	if err != nil {
		return nil, nil // a malformed hash matches nothing
	}
	pr, err := p.AuthenticateBearer(ctx, hash)
	switch {
	case errors.Is(err, ErrCredentialNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("storage: authenticate node %q: %w", name, err)
	}
	if pr.Kind != "node" || pr.Node == nil || pr.Node.Name != name {
		return nil, nil
	}
	return pr, nil
}

// RecordHeartbeat stamps the node's last_heartbeat_at. Keyed by the node name the
// server extracts from the heartbeat subject (subject permissions guarantee a
// node can only publish to its own subject), so this trusts the name.
func (p *PG) RecordHeartbeat(ctx context.Context, name string) error {
	tag, err := p.pool.Exec(ctx, `update node set last_heartbeat_at = now() where name = $1`, name)
	if err != nil {
		return fmt.Errorf("storage: record heartbeat %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNodeNotFound
	}
	return nil
}

// NodeWorklist resolves a node's enabled tasks (joined to their endpoint) plus
// the config generation. Keyed by the node name the server extracts from the
// worklist subject. An unknown node returns an empty worklist, not an error.
func (p *PG) NodeWorklist(ctx context.Context, name string) (Worklist, error) {
	rows, err := p.pool.Query(ctx, `
		select t.id, t.mode, i.name, i.transport, i.params, t.spec, i.inputs, i.component, d.spec
		from task t
		join endpoint i on i.id = t.endpoint_id
		left join driver d on d.id = i.driver_id
		where i.node_name = (select principal_id from node where name = $1) and t.enabled = true
		order by t.id`, name)
	if err != nil {
		return Worklist{}, fmt.Errorf("storage: node worklist %q: %w", name, err)
	}
	defer rows.Close()
	var wl Worklist
	type secretRef struct{ input, ref, componentID string }
	pending := map[int][]secretRef{} // task index -> the secret inputs to unseal
	for rows.Next() {
		var wt WorklistTask
		var epInputs, driverSpec []byte
		var componentID *string
		if err := rows.Scan(&wt.ID, &wt.Mode, &wt.EndpointName, &wt.Transport, &wt.EndpointParams, &wt.Spec, &epInputs, &componentID, &driverSpec); err != nil {
			return Worklist{}, fmt.Errorf("storage: scan worklist task: %w", err)
		}
		// A driver task's secret inputs travel with it: the spec says which
		// inputs are secret, the endpoint's inputs say which secret row each
		// references, and the unseal below turns the references into fields.
		// The reachability probe (a bare spec) carries none.
		if len(driverSpec) > 0 && len(wt.Spec) > 2 {
			refs, err := secretRefsOf(driverSpec, epInputs)
			if err != nil {
				return Worklist{}, fmt.Errorf("storage: node worklist %q task %s: %w", name, wt.ID, err)
			}
			cid := ""
			if componentID != nil {
				cid = *componentID
			}
			for input, ref := range refs {
				pending[len(wl.Tasks)] = append(pending[len(wl.Tasks)], secretRef{input, ref, cid})
			}
		}
		wl.Tasks = append(wl.Tasks, wt)
	}
	if err := rows.Err(); err != nil {
		return Worklist{}, fmt.Errorf("storage: node worklist %q: %w", name, err)
	}
	// Unseal after the row scan (one connection at a time), caching by the
	// (reference, component) pair so two tasks on one component sharing a
	// credential unseal it once. Delivery is confined to a secret that CASCADES
	// ONTO the endpoint's component (platform, an ancestor location or system,
	// or the component itself): the attach scoped the reference to the operator,
	// and this re-check keeps a rename or a name collision from drifting a node
	// onto a secret its component does not own. A reference that resolves to
	// nothing deliverable is delivered as absence: the node lands a
	// collection-failed naming the missing credential.
	unsealed := map[string]map[string]string{}
	for idx, refs := range pending {
		for _, r := range refs {
			key := r.ref + "\x00" + r.componentID
			fields, ok := unsealed[key]
			if !ok {
				f, err := p.nodeSecretFields(ctx, r.ref, r.componentID)
				if err != nil {
					continue
				}
				fields, unsealed[key] = f, f
			}
			if wl.Tasks[idx].Secrets == nil {
				wl.Tasks[idx].Secrets = map[string]map[string]string{}
			}
			wl.Tasks[idx].Secrets[r.input] = fields
		}
	}
	// config_generation moves at operator-config pace: the max endpoint
	// updated_at (epoch seconds) across the node's endpoints, 0 when none.
	if err := p.pool.QueryRow(ctx, `
		select coalesce(extract(epoch from max(updated_at))::bigint, 0)
		from endpoint where node_name = (select principal_id from node where name = $1)`, name).Scan(&wl.ConfigGeneration); err != nil {
		return Worklist{}, fmt.Errorf("storage: node config generation %q: %w", name, err)
	}
	return wl, nil
}

// GetNode reads one node by name. Requires an all read scope (a node is
// fleet-wide reference, not a subtree row); an unknown name is ErrNodeNotFound.
func (p *PG) GetNode(ctx context.Context, name string, read scope.Set) (*Node, error) {
	if err := RejectAddressForm("node", name); err != nil {
		return nil, err
	}
	if !read.All {
		return nil, ErrNodeForbidden
	}
	n, err := scanNode(p.pool.QueryRow(ctx, `select `+nodeCols+` from node where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNodeNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: get node %q: %w", name, err)
	}
	return n, nil
}

// ListNodes returns every node. Requires an all read scope.
func (p *PG) ListNodes(ctx context.Context, read scope.Set) ([]Node, error) {
	if !read.All {
		return nil, ErrNodeForbidden
	}
	rows, err := p.pool.Query(ctx, `select `+nodeCols+` from node order by name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list nodes: %w", err)
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan node: %w", err)
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func mapNodeWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrNodeExists
		case "23514": // check_violation (node_name_subject_safe_check)
			return ErrInvalidNodeName
		case "23503": // foreign_key_violation (a placement resolved to a location that vanished mid-write)
			return ErrLocationNotFound
		}
	}
	return fmt.Errorf("storage: node write: %w", err)
}

// nodeLocationID resolves an optional placement reference (a location name or a
// uuid) to the location id the arc stores, within the caller's location:read
// scope. A nil reference leaves the placement unchanged and an empty one clears
// it, so both map to a nil id; an unknown location is ErrLocationNotFound rather
// than a NULL that would silently unplace the node.
//
// resolvePlacementRef is the seam #700 built for exactly this reference on the
// component and the system tiers, and this is the one caller-supplied reference
// that arc left resolving scope-blind (#705). Two of its three reasons apply
// here verbatim. The caller's own node create/update scope is the WRONG SET, not
// merely a stricter one: it is resolved for "node", and a location's scope tree
// is its own unrelated ancestor chain, so checking it against the location table
// could never match. And out of scope answers the same non-disclosing
// ErrLocationNotFound an absent location gives, because a refusal that separated
// them would confirm to a caller with no grant on that row that the row exists.
//
// What does NOT apply is the disclosure that made #700 urgent: no node label
// rule exists, so this write stamps nothing built from the location's own label
// and hands nothing back about it. What is left is the plain invariant, that
// ABAC scope is injected on every applicable query, and a caller-supplied
// reference resolved without it is an exception to that rather than a carve-out
// anybody decided on.
//
// The signature reconciles with the seam's by taking the read set the seam needs
// and nothing else: nodeLocationID already took a querier (the caller's tx, so a
// location written earlier in the same transaction resolves), which is exactly
// what resolvePlacementRef takes. The set is what the node path could not
// supply, so CreateNode and UpdateNode take it from the API layer, which injects
// location:read exactly as the system and component routes already do.
func nodeLocationID(ctx context.Context, q querier, ref *string, locationRead scope.Set) (*string, error) {
	if ref == nil || *ref == "" {
		return nil, nil
	}
	l, err := resolvePlacementRef(ctx, q, locationConfig, *ref, locationRead)
	if err != nil {
		return nil, err
	}
	return &l.ID, nil
}
