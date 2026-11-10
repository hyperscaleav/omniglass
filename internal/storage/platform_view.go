package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The platform projection (#1644): the read behind the Platform health page.
//
// The page answers one question, whether Omniglass itself is well, and it
// answers it from parts the fleet already has: every node and every server is
// a component of the platform system (#1625, #1627), and every figure on the
// page is a sample on one of those components. So this read invents no data;
// it gathers the running build, the platform verdict and each node's current
// figure per key (PlatformWindowKeys) into one body, in a fixed number of statements however many
// nodes there are.
//
// Scope is injected per tier, as FleetProjection does. node:read lets the
// caller see the rows at all, and only an all-scope read reaches a node
// (ListNodes). component:read is the figures, the verdicts and the server's
// identity: the platform components are unplaced, so only an all-scope read
// reaches them, and anything less leaves the figures out rather than zeroing
// them. system:read is the platform system's own verdict, null short of an
// all-scope read for the same reason.

// PlatformViewGroup is one titled run of columns in the node table.
type PlatformViewGroup struct {
	Name  string
	Label string
	Keys  []string
}

// PlatformViewGroups is the node table's columns, in order: the one place the
// page's key set is written. The page renders the groups the view returns and
// has no list of its own, so a key added here is a column there. Each key is
// a seeded platform metric (TestPlatformViewKeysAreSeeded).
var PlatformViewGroups = []PlatformViewGroup{
	{Name: "checkin", Label: "Checkin", Keys: []string{writerclass.CheckinAge}},
	{Name: "schedule", Label: "Schedule", Keys: []string{
		writerclass.ScheduleLateness, writerclass.TickOverrunCount, writerclass.ScheduleSkipCount,
	}},
	// The node's own delivery counts, until #1646 folds them into its Loss
	// group: this slice prints no loss figure.
	{Name: "delivery", Label: "Delivery", Keys: []string{
		writerclass.SpoolDropCount, writerclass.PublishFailureCount,
	}},
	{Name: "commands", Label: "Commands", Keys: []string{
		writerclass.CommandPendingCount, writerclass.CommandPendingAge,
		writerclass.CommandStuckCount, writerclass.CommandExpiredCount,
	}},
}

// platformViewKeys is every key in PlatformViewGroups, in order.
func platformViewKeys() []string {
	var out []string
	for _, g := range PlatformViewGroups {
		out = append(out, g.Keys...)
	}
	return out
}

// PlatformKey is one column's catalog facts: its metric type's label, unit
// and description, the tooltip the page shows (ADR-0127). Read from the
// catalog so no prose about a key is written twice.
type PlatformKey struct {
	Name        string
	Label       string
	Unit        string
	Description string
}

// PlatformFigure is a key's current value on one component and when it was
// observed: a gauge's latest sample, or a window count's newest window summed
// over the instances written in it.
type PlatformFigure struct {
	Value      float64
	ObservedAt time.Time
}

// PlatformSystem is the platform system and its recorded verdict.
type PlatformSystem struct {
	ID      string
	Name    string
	Verdict string
}

// PlatformServer is one server's platform component and the identity its
// boot wrote: the build it runs and when it started. Version and StartedAt
// are nil before a boot has written them.
type PlatformServer struct {
	ComponentID string
	Name        string
	Version     *string
	StartedAt   *time.Time
	Verdict     string
}

// PlatformNode is one node's row. ComponentID is nil for a node the boot has
// not yet given a platform component. Verdict and Figures are the
// component's, nil without an all-scope component read; a key the node has
// never reported is absent from Figures, never zero.
type PlatformNode struct {
	Name            string
	Label           string
	Enrolled        bool
	LastHeartbeatAt *time.Time
	ComponentID     *string
	Verdict         *string
	Figures         map[string]PlatformFigure
}

// PlatformView is the whole projection. FiguresRead says whether the caller's
// component read reached the platform components: false means Servers is
// empty and every node's Verdict and Figures are nil because the caller may
// not read them, not because nothing was reported.
type PlatformView struct {
	System      *PlatformSystem
	Servers     []PlatformServer
	Groups      []PlatformViewGroup
	Keys        []PlatformKey
	Nodes       []PlatformNode
	FiguresRead bool
}

// PlatformView reads the platform as the Platform health page shows it. A
// node read short of all-scope is ErrNodeForbidden, as ListNodes answers.
func (p *PG) PlatformView(ctx context.Context, nodeRead, sysRead, compRead scope.Set) (*PlatformView, error) {
	nodes, err := p.ListNodes(ctx, nodeRead)
	if err != nil {
		return nil, err
	}
	view := &PlatformView{
		Servers: []PlatformServer{},
		Groups:  PlatformViewGroups,
		Nodes:   make([]PlatformNode, 0, len(nodes)),
	}
	if view.Keys, err = p.platformKeys(ctx); err != nil {
		return nil, err
	}
	var componentIDs []string
	for _, n := range nodes {
		view.Nodes = append(view.Nodes, PlatformNode{
			Name: n.Name, Label: n.Label, Enrolled: n.Enrolled,
			LastHeartbeatAt: n.LastHeartbeatAt, ComponentID: n.PlatformComponentID,
		})
		if n.PlatformComponentID != nil {
			componentIDs = append(componentIDs, *n.PlatformComponentID)
		}
	}
	if sysRead.All {
		if view.System, err = p.platformSystem(ctx); err != nil {
			return nil, err
		}
	}
	if !compRead.All {
		return view, nil
	}
	view.FiguresRead = true
	if view.Servers, err = p.platformServers(ctx); err != nil {
		return nil, err
	}
	verdicts, err := p.componentVerdicts(ctx, componentIDs)
	if err != nil {
		return nil, err
	}
	figures, err := p.platformFigures(ctx, componentIDs)
	if err != nil {
		return nil, err
	}
	for i := range view.Nodes {
		id := view.Nodes[i].ComponentID
		if id == nil {
			continue
		}
		if v, ok := verdicts[*id]; ok {
			view.Nodes[i].Verdict = &v
		}
		view.Nodes[i].Figures = figures[*id]
		if view.Nodes[i].Figures == nil {
			view.Nodes[i].Figures = map[string]PlatformFigure{}
		}
	}
	return view, nil
}

// platformKeys reads the catalog facts of every key in PlatformViewGroups, in
// group order. A key the catalog does not hold is left out.
func (p *PG) platformKeys(ctx context.Context) ([]PlatformKey, error) {
	rows, err := p.pool.Query(ctx, `
		select mt.name, coalesce(mt.label, ''), coalesce(mt.unit, ''), coalesce(mt.description, '')
		from unnest($1::text[]) with ordinality as k(name, ord)
		join metric_type mt on mt.namespace = 'og' and mt.name = k.name
		order by k.ord`, platformViewKeys())
	if err != nil {
		return nil, fmt.Errorf("storage: platform view keys: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PlatformKey, error) {
		var k PlatformKey
		err := r.Scan(&k.Name, &k.Label, &k.Unit, &k.Description)
		return k, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: platform view keys: %w", err)
	}
	return out, nil
}

// platformSystem reads the platform system and its verdict, nil on a database
// the boot seed has not reached.
func (p *PG) platformSystem(ctx context.Context) (*PlatformSystem, error) {
	var s PlatformSystem
	err := p.pool.QueryRow(ctx, `
		select system.id::text, system.name, `+latestVerdict("system_id", "system")+`
		from system join system_type st on st.id = system.system_type_id
		where st.namespace = 'og' and st.name = $1`, platformTypeName).Scan(&s.ID, &s.Name, &s.Verdict)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: platform view system: %w", err)
	}
	return &s, nil
}

// latestIdentity is a server component's newest observed value of one of its
// boot's identity properties, as text.
func latestIdentity(key string) string {
	return `(select v.value #>> '{}' from property v
		where v.component_id = component.id
		  and v.property_type_id = (select id from property_type where namespace = 'og' and name = '` + key + `')
		  and v.instance = '' and v.provenance = 'observed'
		order by v.id desc limit 1)`
}

// platformServers reads every server's platform component, its identity and
// its verdict, in name order.
func (p *PG) platformServers(ctx context.Context) ([]PlatformServer, error) {
	rows, err := p.pool.Query(ctx, `
		select component.id::text, component.name,
		       `+latestIdentity(writerclass.ServerVersion)+`,
		       `+latestIdentity(writerclass.ServerStartedAt)+`,
		       `+latestVerdict("component_id", "component")+`
		from platform_server ps join component on component.id = ps.platform_component_id
		order by component.name`)
	if err != nil {
		return nil, fmt.Errorf("storage: platform view servers: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PlatformServer, error) {
		var (
			s       PlatformServer
			started *string
		)
		if err := r.Scan(&s.ComponentID, &s.Name, &s.Version, &started, &s.Verdict); err != nil {
			return s, err
		}
		if started != nil {
			if t, err := time.Parse(time.RFC3339Nano, *started); err == nil {
				s.StartedAt = &t
			}
		}
		return s, nil
	})
	if err != nil {
		return nil, fmt.Errorf("storage: platform view servers: %w", err)
	}
	return out, nil
}

// componentVerdicts reads the recorded verdict of each component in ids.
func (p *PG) componentVerdicts(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx, `
		select component.id::text, `+latestVerdict("component_id", "component")+`
		from component where component.id = any($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("storage: platform view verdicts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, v string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, fmt.Errorf("storage: platform view verdicts: %w", err)
		}
		out[id] = v
	}
	return out, rows.Err()
}

// PlatformWindowKeys are the PlatformViewGroups keys a writer reports once a
// window and only when nonzero (the server's command pass); the node's report
// window counts are written every window, 0 when quiet (ADR-1625), so they
// read as current like a gauge. Their last sample is the last window that counted
// something, not a current reading: a later window that counted nothing
// writes no row. So the view shows one only while it is the newest window's
// and leaves it absent after (platformWindowHorizon). Every other key is a
// gauge written whether or not it moved, whose latest sample is current
// however old. The catalog describes exactly these as "reported only when
// nonzero" (TestPlatformWindowKeysAreTheNonzeroCounts).
var PlatformWindowKeys = map[string]bool{
	writerclass.CommandExpiredCount: true,
}

// platformWindowHorizon is how far back a window count still reads as the
// current window: two report windows, so the newest window has ended and a
// later one has too. The command pass's window (15 s) is shorter than the
// shortest report window doubled (2 x 10 s), so the same horizon holds it.
func platformWindowHorizon(reportSeconds int) time.Duration {
	return 2 * time.Duration(reportSeconds) * time.Second
}

// platformFigures reads, in one statement, the current figure of every
// PlatformViewGroups key on every component in ids. Per series instance it
// takes the newest row (metricNewestSQL, the read EffectiveMetrics makes)
// across the provenances that report reality (observed, calculated); an
// intended row is what a command asked for, never a figure. A key's figure is
// then the sum of its instances written at its newest time: a key split by
// instance (command-expired-count's undelivered and unexecuted) is one count
// written at one pass, and an instance from an earlier pass is not this
// one's. A window count past the horizon is left out (PlatformWindowKeys).
func (p *PG) platformFigures(ctx context.Context, ids []string) (map[string]map[string]PlatformFigure, error) {
	out := map[string]map[string]PlatformFigure{}
	if len(ids) == 0 {
		return out, nil
	}
	collection, err := p.CollectionSettings(ctx)
	if err != nil {
		return nil, err
	}
	var windowKeys []string
	for _, k := range platformViewKeys() {
		if PlatformWindowKeys[k] {
			windowKeys = append(windowKeys, k)
		}
	}
	rows, err := p.pool.Query(ctx, `
		with latest as (
			select distinct on (s.component_id, mt.name, s.instance)
			       s.component_id, mt.name, n.value, n.ts
			from metric_series s
			join metric_type mt on mt.id = s.metric_type_id
			cross join lateral (`+metricNewestSQL("s.id")+`) n
			where s.owner_kind = 'component' and s.component_id = any($1::uuid[])
			  and mt.namespace = 'og' and mt.name = any($2::text[])
			  and s.provenance in ('observed', 'calculated')
			order by s.component_id, mt.name, s.instance, n.ts desc, n.id desc
		), ranked as (
			select *, max(ts) over (partition by component_id, name) as newest from latest
		)
		select component_id::text, name, sum(value), newest
		from ranked
		where ts = newest
		  and not (name = any($3::text[]) and newest < now() - $4::interval)
		group by component_id, name, newest`,
		ids, platformViewKeys(), windowKeys, platformWindowHorizon(collection.ReportSeconds))
	if err != nil {
		return nil, fmt.Errorf("storage: platform view figures: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, key string
			f       PlatformFigure
		)
		if err := rows.Scan(&id, &key, &f.Value, &f.ObservedAt); err != nil {
			return nil, fmt.Errorf("storage: platform view figures: %w", err)
		}
		if out[id] == nil {
			out[id] = map[string]PlatformFigure{}
		}
		out[id][key] = f
	}
	return out, rows.Err()
}
