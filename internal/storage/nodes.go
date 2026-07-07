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

// Node-layer sentinel errors. A node is the edge runtime; it is estate-wide
// (not tree-scoped), so its create/enroll/read paths require an all scope,
// mirroring principals.
var (
	ErrNodeNotFound      = errors.New("storage: node not found")
	ErrNodeExists        = errors.New("storage: node name already exists")
	ErrNodeForbidden     = errors.New("storage: action not permitted on nodes")
	ErrEnrollmentInvalid = errors.New("storage: enrollment token invalid")
)

// Node is the edge runtime's server-side record. EnrolledAt is set the first
// time the node claims its identity; Enrolled is a convenience derived from it.
type Node struct {
	Name            string
	Description     string
	LastHeartbeatAt *time.Time
	EnrolledAt      *time.Time
	Enrolled        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NodeSpec is the create input.
type NodeSpec struct {
	Name        string
	Description string
}

// WorklistTask is one enabled task resolved for a node: the content-addressed
// task plus the placement-bound interface it runs over. InterfaceParams and Spec
// are raw jsonb passed through to the node.
type WorklistTask struct {
	ID              string
	Mode            string
	InterfaceName   string
	InterfaceType   string
	InterfaceParams []byte
	Spec            []byte
}

// Worklist is a node's resolved work plus the config generation (the max
// interface updated_at across the node's interfaces, epoch seconds; 0 when the
// node has no interfaces). A steady generation lets the node serve from cache; a
// bump forces a refresh.
type Worklist struct {
	Tasks            []WorklistTask
	ConfigGeneration int64
}

const nodeCols = `name, description, last_heartbeat_at, enrolled_at, created_at, updated_at`

func scanNode(row pgx.Row) (*Node, error) {
	var n Node
	if err := row.Scan(&n.Name, &n.Description, &n.LastHeartbeatAt, &n.EnrolledAt, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	n.Enrolled = n.EnrolledAt != nil
	return &n, nil
}

// CreateNode inserts a node, writing the audit row in the same transaction. A
// node is estate-wide, so creation requires an all create scope (like a
// principal, unlike a tree-scoped location/system/component).
func (p *PG) CreateNode(ctx context.Context, actorID string, spec NodeSpec, create scope.Set) (*Node, error) {
	if !create.All {
		return nil, ErrNodeForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create node: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	n, err := scanNode(tx.QueryRow(ctx, `
		insert into node (name, description)
		values ($1, $2)
		returning `+nodeCols, spec.Name, spec.Description))
	if err != nil {
		return nil, mapNodeWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "node", n.Name, nil, n); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create node: %w", err)
	}
	return n, nil
}

// SetEnrollmentToken stores the hex sha256 of a freshly minted enrollment token
// (the cleartext is shown once by the API and never stored), audited. Requires an
// all action scope.
func (p *PG) SetEnrollmentToken(ctx context.Context, actorID, name, tokenHashHex string, action scope.Set) (*Node, error) {
	if !action.All {
		return nil, ErrNodeForbidden
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
	after, err := scanNode(tx.QueryRow(ctx, `
		update node set enrollment_token = $2, updated_at = now()
		where name = $1
		returning `+nodeCols, name, tokenHashHex))
	if err != nil {
		return nil, fmt.Errorf("storage: set enrollment token %q: %w", name, err)
	}
	// The token hash itself is never written to the audit diff (it is a secret);
	// the audit records that an enroll happened on the node.
	if err := writeAuditRes(ctx, tx, actorID, "enroll", "node", name, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set enrollment token: %w", err)
	}
	return after, nil
}

// ClaimNode is the node-facing exchange: the node presents its enrollment token,
// and a hash match sets enrolled_at (first claim) and returns the node. No scope:
// the presented token is the authentication. A mismatch or an unset token is
// ErrEnrollmentInvalid; an unknown node is also ErrEnrollmentInvalid (a claim
// must not disclose which nodes exist).
func (p *PG) ClaimNode(ctx context.Context, name, tokenHashHex string) (*Node, error) {
	var stored *string
	if err := p.pool.QueryRow(ctx, `select enrollment_token from node where name = $1`, name).Scan(&stored); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEnrollmentInvalid
	} else if err != nil {
		return nil, fmt.Errorf("storage: claim node %q: %w", name, err)
	}
	if stored == nil || *stored != tokenHashHex {
		return nil, ErrEnrollmentInvalid
	}
	// coalesce keeps the original enrolled_at on a re-claim (idempotent).
	n, err := scanNode(p.pool.QueryRow(ctx, `
		update node set enrolled_at = coalesce(enrolled_at, now()), updated_at = now()
		where name = $1
		returning `+nodeCols, name))
	if err != nil {
		return nil, fmt.Errorf("storage: mark enrolled %q: %w", name, err)
	}
	return n, nil
}

// AuthenticateNode reports whether the presented token hash matches the node's
// stored enrollment token. The NATS auth callback calls this to admit a node
// connection; a non-match or unset token is a clean false, not an error.
func (p *PG) AuthenticateNode(ctx context.Context, name, tokenHashHex string) (bool, error) {
	var stored *string
	err := p.pool.QueryRow(ctx, `select enrollment_token from node where name = $1`, name).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("storage: authenticate node %q: %w", name, err)
	}
	return stored != nil && *stored == tokenHashHex, nil
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

// NodeWorklist resolves a node's enabled tasks (joined to their interface) plus
// the config generation. Keyed by the node name the server extracts from the
// worklist subject. An unknown node returns an empty worklist, not an error.
func (p *PG) NodeWorklist(ctx context.Context, name string) (Worklist, error) {
	rows, err := p.pool.Query(ctx, `
		select t.id, t.mode, t.interface_name, i.type, i.params, t.spec
		from task t
		join interface i on i.name = t.interface_name
		where t.node_name = $1 and t.enabled = true
		order by t.id`, name)
	if err != nil {
		return Worklist{}, fmt.Errorf("storage: node worklist %q: %w", name, err)
	}
	defer rows.Close()
	var wl Worklist
	for rows.Next() {
		var wt WorklistTask
		if err := rows.Scan(&wt.ID, &wt.Mode, &wt.InterfaceName, &wt.InterfaceType, &wt.InterfaceParams, &wt.Spec); err != nil {
			return Worklist{}, fmt.Errorf("storage: scan worklist task: %w", err)
		}
		wl.Tasks = append(wl.Tasks, wt)
	}
	if err := rows.Err(); err != nil {
		return Worklist{}, fmt.Errorf("storage: node worklist %q: %w", name, err)
	}
	// config_generation moves at operator-config pace: the max interface
	// updated_at (epoch seconds) across the node's interfaces, 0 when none.
	if err := p.pool.QueryRow(ctx, `
		select coalesce(extract(epoch from max(updated_at))::bigint, 0)
		from interface where node_name = $1`, name).Scan(&wl.ConfigGeneration); err != nil {
		return Worklist{}, fmt.Errorf("storage: node config generation %q: %w", name, err)
	}
	return wl, nil
}

// GetNode reads one node by name. Requires an all read scope (a node is
// estate-wide reference, not a subtree row); an unknown name is ErrNodeNotFound.
func (p *PG) GetNode(ctx context.Context, name string, read scope.Set) (*Node, error) {
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
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrNodeExists
	}
	return fmt.Errorf("storage: node write: %w", err)
}
