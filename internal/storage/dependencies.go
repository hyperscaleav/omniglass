package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Component dependency sentinels (#1648).
var (
	// ErrDependencyNotFound is a delete naming no edge of that kind between
	// the two components, or an upstream outside the caller's read.
	ErrDependencyNotFound = errors.New("storage: component dependency not found")
	// ErrDependencyUpstreamNotFound is an upstream that does not exist or is
	// outside the caller's read: the same answer for both, so a create never
	// discloses a component the caller cannot see.
	ErrDependencyUpstreamNotFound = errors.New("storage: upstream component not found")
	// ErrDependencyInvalid is an edge whose shape is wrong on its own: an
	// unknown kind, a power edge without an outlet, another kind with one,
	// or a component depending on itself.
	ErrDependencyInvalid = errors.New("storage: component dependency is invalid")
	// ErrDependencyCycle is an edge whose upstream already depends, through
	// edges of the same kind, on the dependent.
	ErrDependencyCycle = errors.New("storage: component dependency would close a cycle")
	// ErrDependencyExists is a second edge of the same kind between the same
	// two components.
	ErrDependencyExists = errors.New("storage: component dependency already exists")
)

// DependencyOutletMax is the longest outlet name a power edge holds, in
// characters; the table's CHECK holds the same bound.
const DependencyOutletMax = 64

// ComponentDependency is one edge: Component depends on Upstream, of Kind
// (power, host or path), with Outlet set on a power edge only. Each end comes
// back as uuid and name.
type ComponentDependency struct {
	ID            string
	ComponentID   string
	ComponentName string
	UpstreamID    string
	UpstreamName  string
	Kind          string
	Outlet        string
	CreatedAt     time.Time
}

// ComponentDependencySpec is a create's input: the upstream (name or uuid),
// the kind and, for a power edge, the outlet.
type ComponentDependencySpec struct {
	Upstream string
	Kind     string
	Outlet   string
}

// ComponentDependencyList is one component's edges in both directions as the
// caller may see them: Upstreams are what it depends on, Dependents what
// depends on it. Hidden counts the edges whose other end is outside the
// caller's read.
type ComponentDependencyList struct {
	Upstreams  []ComponentDependency
	Dependents []ComponentDependency
	Hidden     int
}

// ValidateDependencyEdge is the edge's shape on its own, before any row is
// read: a known kind, an outlet of 1 to 64 characters on a power edge and
// none on another kind. Every refusal wraps ErrDependencyInvalid.
func ValidateDependencyEdge(kind, outlet string) error {
	switch kind {
	case DependencyPower:
		if outlet == "" {
			return fmt.Errorf("%w: a power dependency names the outlet that feeds it", ErrDependencyInvalid)
		}
		if utf8.RuneCountInString(outlet) > DependencyOutletMax {
			return fmt.Errorf("%w: an outlet is at most %d characters", ErrDependencyInvalid, DependencyOutletMax)
		}
	case DependencyHost, DependencyPath:
		if outlet != "" {
			return fmt.Errorf("%w: only a power dependency names an outlet", ErrDependencyInvalid)
		}
	default:
		return fmt.Errorf("%w: kind %q is not power, host or path", ErrDependencyInvalid, kind)
	}
	return nil
}

type componentDependencyAudit struct {
	Component string `json:"component"`
	Upstream  string `json:"upstream"`
	Kind      string `json:"kind"`
	Outlet    string `json:"outlet,omitempty"`
}

func (d *ComponentDependency) audit() componentDependencyAudit {
	return componentDependencyAudit{Component: d.ComponentName, Upstream: d.UpstreamName, Kind: d.Kind, Outlet: d.Outlet}
}

const dependencyCols = `d.id, d.component_id, dc.name, d.upstream_id, uc.name, d.kind, coalesce(d.outlet, ''), d.created_at`

const dependencyFrom = ` from component_dependency d
	join component dc on dc.id = d.component_id
	join component uc on uc.id = d.upstream_id`

func scanDependency(row pgx.Row) (*ComponentDependency, error) {
	var d ComponentDependency
	if err := row.Scan(&d.ID, &d.ComponentID, &d.ComponentName, &d.UpstreamID, &d.UpstreamName, &d.Kind, &d.Outlet, &d.CreatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// resolveUpstream resolves a dependency's upstream within the caller's read:
// absent or out of read is ErrDependencyUpstreamNotFound. An ambiguous name
// passes through with its in-scope candidates.
func resolveUpstream(ctx context.Context, q querier, ref string, read scope.Set) (*Component, error) {
	c, err := scopedByNameInScope(ctx, q, componentConfig, ref, componentConfig.resource, read)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, ErrDependencyUpstreamNotFound
	}
	if err != nil {
		return nil, foldPathNotFound(err)
	}
	return c, nil
}

// CreateComponentDependency records that component depends on spec.Upstream,
// in one transaction with its audit row. The dependent resolves in read then
// update (the edge is a fact about it); the upstream in read only. An edge
// whose upstream already reaches the dependent through edges of the same kind
// is a cycle and refused: a per-kind advisory lock serializes the check and
// the insert, so two racing creates cannot close one between them.
func (p *PG) CreateComponentDependency(ctx context.Context, actorID, component string, spec ComponentDependencySpec, read, update scope.Set) (*ComponentDependency, error) {
	if err := ValidateDependencyEdge(spec.Kind, spec.Outlet); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create component dependency: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	dep, err := resolveScoped(ctx, tx, componentConfig, component, read, update)
	if err != nil {
		return nil, err
	}
	up, err := resolveUpstream(ctx, tx, spec.Upstream, read)
	if err != nil {
		return nil, err
	}
	if dep.ID == up.ID {
		return nil, fmt.Errorf("%w: a component does not depend on itself", ErrDependencyInvalid)
	}
	if err := lockAdvisory(ctx, tx, "component_dependency:"+spec.Kind); err != nil {
		return nil, err
	}
	var cycle bool
	if err := tx.QueryRow(ctx, `
		with recursive up(id) as (
			select upstream_id from component_dependency where component_id = $1 and kind = $3
			union
			select d.upstream_id from component_dependency d join up on d.component_id = up.id where d.kind = $3
		)
		select exists(select 1 from up where id = $2)`, up.ID, dep.ID, spec.Kind).Scan(&cycle); err != nil {
		return nil, fmt.Errorf("storage: check dependency cycle: %w", err)
	}
	if cycle {
		return nil, ErrDependencyCycle
	}
	var id string
	err = tx.QueryRow(ctx, `insert into component_dependency (component_id, upstream_id, kind, outlet)
		values ($1, $2, $3, nullif($4, '')) returning id`, dep.ID, up.ID, spec.Kind, spec.Outlet).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				return nil, ErrDependencyExists
			case "23514":
				return nil, fmt.Errorf("%w: %s", ErrDependencyInvalid, pgErr.ConstraintName)
			}
		}
		return nil, fmt.Errorf("storage: insert component dependency: %w", err)
	}
	after, err := scanDependency(tx.QueryRow(ctx, `select `+dependencyCols+dependencyFrom+` where d.id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("storage: read component dependency: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "component_dependency", after.ID, nil, after.audit()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create component dependency: %w", err)
	}
	return after, nil
}

// ListComponentDependencies names one component's edges both ways, the
// component resolved in the caller's read. An edge whose other end is
// outside that read is counted in Hidden and not named.
func (p *PG) ListComponentDependencies(ctx context.Context, component string, read scope.Set) (*ComponentDependencyList, error) {
	c, err := scopedByNameInScope(ctx, p.pool, componentConfig, component, componentConfig.resource, read)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `select `+dependencyCols+dependencyFrom+`
		where d.component_id = $1 or d.upstream_id = $1
		order by d.kind, uc.name, dc.name`, c.ID)
	if err != nil {
		return nil, fmt.Errorf("storage: list component dependencies: %w", err)
	}
	var edges []*ComponentDependency
	for rows.Next() {
		d, err := scanDependency(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan component dependency: %w", err)
		}
		edges = append(edges, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: list component dependencies: %w", err)
	}
	out := &ComponentDependencyList{Upstreams: []ComponentDependency{}, Dependents: []ComponentDependency{}}
	for _, d := range edges {
		other := d.UpstreamID
		if d.UpstreamID == c.ID {
			other = d.ComponentID
		}
		ok, err := inScopeTree(ctx, p.pool, componentTable, other, read)
		if err != nil {
			return nil, err
		}
		switch {
		case !ok:
			out.Hidden++
		case d.ComponentID == c.ID:
			out.Upstreams = append(out.Upstreams, *d)
		default:
			out.Dependents = append(out.Dependents, *d)
		}
	}
	return out, nil
}

// DeleteComponentDependency removes component's edge of kind to upstream, in
// one transaction with its audit row. The dependent resolves in read then
// update; an upstream outside the caller's read is not found, like the edge.
func (p *PG) DeleteComponentDependency(ctx context.Context, actorID, component, kind, upstream string, read, update scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete component dependency: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	dep, err := resolveScoped(ctx, tx, componentConfig, component, read, update)
	if err != nil {
		return err
	}
	up, err := resolveUpstream(ctx, tx, upstream, read)
	if errors.Is(err, ErrDependencyUpstreamNotFound) {
		return ErrDependencyNotFound
	}
	if err != nil {
		return err
	}
	before, err := scanDependency(tx.QueryRow(ctx, `select `+dependencyCols+dependencyFrom+`
		where d.component_id = $1 and d.upstream_id = $2 and d.kind = $3`, dep.ID, up.ID, kind))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDependencyNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: read component dependency: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from component_dependency where id = $1`, before.ID); err != nil {
		return fmt.Errorf("storage: delete component dependency: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "component_dependency", before.ID, before.audit(), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete component dependency: %w", err)
	}
	return nil
}
