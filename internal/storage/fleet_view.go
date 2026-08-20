package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// The fleet projection: the read behind the fleet canvas.
//
// The canvas paints one square per component across the whole fleet, which is
// the reason this exists rather than the console composing /locations,
// /systems and /components itself. A five-figure component list fetched to draw
// squares is a page that never loads; a dot needs an id, a verdict, and two
// flags, so that is all this carries. Everything an operator can only see after
// clicking (product, tags, properties, the role chain) is deliberately absent
// and is the drill-down read's job.
//
// The shape is FLAT on purpose: locations carry their parent id rather than
// nesting, and systems carry their location id. The client builds the tree, and
// more to the point it builds the BANDS, which is what lets the fleet be
// grouped by something other than place later without a second endpoint. The
// server says what exists and how it is placed; the view model decides how it
// is gathered.
//
// Scope is injected per tier, in the query, through the same scopedListSQL
// primitive every other tree read uses. The three sets are separate arguments
// rather than one, because they answer three different permissions: a principal
// who may read the place tree but not its components gets the shape of their
// fleet with no contents, which is a legitimate view and not an error.

// FleetLocation is one node of the place tree as the canvas needs it.
type FleetLocation struct {
	ID    string
	Name  string
	Label string
	// Both forms of the type reference: the name is what the band's chip reads,
	// the uuid is the stable handle a rename cannot move out from under a
	// client (TestReferencesCarryBothForms). Carried per location, which is tens
	// of rows, never per dot, which is thousands.
	LocationType   string
	LocationTypeID string
	ParentID       *string
	Verdict        string
}

// FleetDot is one component's presence in one system: the whole payload behind
// a single square.
type FleetDot struct {
	ComponentID string
	Name        string
	Verdict     string
	// Primary is true in the component's own primary system, the one cluster
	// that draws it solid. Shared is true when it belongs to more than one
	// system at all. Together they are the ring-and-ghost rule: a shared box is
	// drawn once as owned and as an outline everywhere else, so an operator can
	// see every system that depends on it without the fleet counting one
	// physical device several times.
	Primary bool
	Shared  bool
}

// FleetSystem is one system and the dots inside it.
type FleetSystem struct {
	ID         string
	Name       string
	Label      string
	LocationID *string
	Verdict    string
	Dots       []FleetDot
}

// FleetView is the whole projection.
type FleetView struct {
	Locations []FleetLocation
	Systems   []FleetSystem
}

// latestVerdict is the current recorded health for an owner column, the same
// read subtreeSystemHealth makes: the highest id wins, not the newest ts,
// because a health row's ts is taken before its identity is assigned and two
// concurrent writes can land with the two inverted (#356). An owner with
// nothing recorded reads healthy: nothing has been claimed about it.
func latestVerdict(ownerCol, table string) string {
	return `coalesce((select v.value #>> '{}' from property v
		where v.` + ownerCol + ` = ` + table + `.id
		  and v.property_type_id = (select id from property_type where name = 'health')
		  and v.instance = ''
		order by v.id desc limit 1), 'healthy')`
}

// Every derived column is aliased, the type subquery above all: scopedListSQL
// appends `order by name`, and an unaliased `select t.name from location_type`
// puts a second output column called name in scope, which makes that ORDER BY
// ambiguous rather than merely wrong (SQLSTATE 42702).
var fleetLocationCols = `id, name, coalesce(label, '') as label,
	(select t.name from location_type t where t.id = location.location_type) as location_type_name,
	location.location_type as location_type_id,
	parent_id, ` + latestVerdict("location_id", "location") + ` as verdict`

var fleetSystemCols = `id, name, coalesce(label, '') as label, location_id, ` +
	latestVerdict("system_id", "system") + ` as verdict`

// FleetProjection reads the whole in-scope fleet as the canvas draws it.
func (p *PG) FleetProjection(ctx context.Context, locRead, sysRead, compRead scope.Set) (*FleetView, error) {
	view := &FleetView{Locations: []FleetLocation{}, Systems: []FleetSystem{}}

	locs, err := p.fleetLocations(ctx, locRead)
	if err != nil {
		return nil, err
	}
	view.Locations = locs

	systems, err := p.fleetSystems(ctx, sysRead)
	if err != nil {
		return nil, err
	}
	view.Systems = systems
	if len(systems) == 0 {
		return view, nil
	}

	ids := make([]string, 0, len(systems))
	for i := range systems {
		ids = append(ids, systems[i].ID)
	}
	dots, err := p.fleetDots(ctx, ids, compRead)
	if err != nil {
		return nil, err
	}
	for i := range view.Systems {
		view.Systems[i].Dots = dots[view.Systems[i].ID]
		if view.Systems[i].Dots == nil {
			view.Systems[i].Dots = []FleetDot{}
		}
	}
	return view, nil
}

// scopedTreeQuery runs a read over one scoped tree table through the same
// scopedListSQL the scoped-CRUD primitive uses, so this projection cannot drift
// from the scope semantics every other tree read has. It returns nil rows (not
// an error) when the caller's scope selects nothing at all: an empty canvas is a
// legitimate answer, and refusing would tell a floor viewer their fleet exists.
func (p *PG) scopedTreeQuery(ctx context.Context, tbl scopeTable, cols string, read scope.Set) (pgx.Rows, error) {
	if read.Empty() {
		return nil, nil
	}
	sql := scopedListSQL(tbl, cols, read.All)
	if read.All {
		return p.pool.Query(ctx, sql)
	}
	roots := uuidRoots(read.IDs)
	selfIDs := uuidRoots(read.SelfIDs)
	if len(roots) == 0 && len(selfIDs) == 0 {
		return nil, nil
	}
	return p.pool.Query(ctx, sql, roots, selfIDs)
}

func (p *PG) fleetLocations(ctx context.Context, read scope.Set) ([]FleetLocation, error) {
	rows, err := p.scopedTreeQuery(ctx, locationTable, fleetLocationCols, read)
	if err != nil {
		return nil, fmt.Errorf("storage: fleet locations: %w", err)
	}
	if rows == nil {
		return []FleetLocation{}, nil
	}
	defer rows.Close()
	out := []FleetLocation{}
	for rows.Next() {
		var l FleetLocation
		if err := rows.Scan(&l.ID, &l.Name, &l.Label, &l.LocationType, &l.LocationTypeID, &l.ParentID, &l.Verdict); err != nil {
			return nil, fmt.Errorf("storage: scan fleet location: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (p *PG) fleetSystems(ctx context.Context, read scope.Set) ([]FleetSystem, error) {
	rows, err := p.scopedTreeQuery(ctx, systemTable, fleetSystemCols, read)
	if err != nil {
		return nil, fmt.Errorf("storage: fleet systems: %w", err)
	}
	if rows == nil {
		return []FleetSystem{}, nil
	}
	defer rows.Close()
	out := []FleetSystem{}
	for rows.Next() {
		var s FleetSystem
		if err := rows.Scan(&s.ID, &s.Name, &s.Label, &s.LocationID, &s.Verdict); err != nil {
			return nil, fmt.Errorf("storage: scan fleet system: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// fleetDots reads every membership of the given systems whose component the
// caller may read, as dots keyed by system id.
//
// The component scope is injected as a subquery over the SAME recursive tree
// expansion scopedListSQL builds, rather than filtered out in Go afterwards: a
// component the caller cannot read must never be fetched, let alone counted
// into a cluster (the unscoped-gateway-list class, #458).
func (p *PG) fleetDots(ctx context.Context, systemIDs []string, read scope.Set) (map[string][]FleetDot, error) {
	out := map[string][]FleetDot{}
	if read.Empty() {
		return out, nil
	}
	cols := `m.system_id, c.id, c.name, ` + latestVerdict("component_id", "c") + `,
		m.is_primary,
		(select count(*) from system_member peer where peer.component_id = m.component_id) > 1`
	from := ` from system_member m join component c on c.id = m.component_id
		where m.system_id = any($1::uuid[])`
	order := ` order by c.name`

	var (
		sql  string
		args []any
	)
	if read.All {
		sql = `select ` + cols + from + order
		args = []any{systemIDs}
	} else {
		roots := uuidRoots(read.IDs)
		selfIDs := uuidRoots(read.SelfIDs)
		if len(roots) == 0 && len(selfIDs) == 0 {
			return out, nil // every scope root is malformed: nothing is in scope
		}
		sql = `with recursive sub(id) as (
				select id from component where id = any($2::uuid[])
				union all
				select t.id from component t join sub on t.parent_id = sub.id
			) cycle id set is_cycle using path
			select ` + cols + from + `
			  and (c.id in (select id from sub) or c.id = any($3::uuid[]))` + order
		args = []any{systemIDs, roots, selfIDs}
	}

	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: fleet dots: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var systemID string
		var d FleetDot
		if err := rows.Scan(&systemID, &d.ComponentID, &d.Name, &d.Verdict, &d.Primary, &d.Shared); err != nil {
			return nil, fmt.Errorf("storage: scan fleet dot: %w", err)
		}
		out[systemID] = append(out[systemID], d)
	}
	return out, rows.Err()
}
