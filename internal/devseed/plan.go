package devseed

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/template"
)

// A resolved scenario plan (internal/lab/scenario) seeds as configuration rows:
// the sites, buildings, floors and rooms, the systems in the rooms, the
// components staffing each system's roles, and the simulated endpoints on the
// components. SeedPlan is the one entry point: it drives the Gateway's Create*
// methods one row per transaction, the path every other seed takes, so names,
// labels, audit rows and derived health come out as they do for an operator's
// own writes. Metric history and telemetry are not seeded, only configuration
// (#1555).

// PlanOptions tunes a SeedPlan.
type PlanOptions struct {
	// Endpoints plans the simulated fleet: a loopback address and the
	// endpoints for every component whose product the simulator serves.
	Endpoints bool
	// Node, when set, places the endpoints on that node (which must exist).
	Node string
	// NodeFor, when set, places each site's endpoints on the node it names
	// for that site (a plan site's name), and takes precedence over Node. A
	// site it names "" for falls back to Node. Each node must exist.
	NodeFor func(site string) string
	// PortOffset is the simulator's lift for ports below 1024 (see Options).
	PortOffset int
}

// PlanPhase is the time one stage of a seed took.
type PlanPhase struct {
	Name string
	Took time.Duration
}

// PlanResult is what a SeedPlan wrote and where the time went.
type PlanResult struct {
	Locations, Systems, Components, Roles, Endpoints int
	Phases                                           []PlanPhase
}

// locationRow is one location of a plan; Parent is the key of another, empty
// for a site.
type locationRow struct{ Key, Name, Label, Type, Parent string }

// systemRow is one system of a plan, placed in the room Room (a location key).
type systemRow struct{ Key, Name, Label, Standard, SystemType, Room string }

// componentRow is one component of a plan: a product placed in a room of Site,
// filling Role of System. Its name is the platform's to mint.
type componentRow struct{ Key, Product, Site, Room, System, Role string }

// planRows is a plan flattened to the rows a seed creates, in creation order.
// Creation order is part of the contract: a platform-minted name is the stem
// plus the lowest free ordinal in its placement, so the same plan always mints
// the same names.
type planRows struct {
	Locations  []locationRow
	Systems    []systemRow
	Components []componentRow
}

// rowsOf flattens a plan. Pure.
func rowsOf(p *scenario.Plan) planRows {
	var r planRows
	seen := map[string]bool{}
	add := func(l locationRow) {
		if !seen[l.Key] {
			seen[l.Key] = true
			r.Locations = append(r.Locations, l)
		}
	}
	for _, site := range p.Sites {
		add(locationRow{Key: site.Name, Name: site.Name, Label: site.Label, Type: "campus"})
		for _, b := range site.Buildings {
			add(locationRow{Key: b.Name, Name: b.Name, Type: "building", Parent: site.Name})
			for f := 1; f <= b.Floors; f++ {
				fl := fmt.Sprintf("%s-%d", b.Name, f)
				add(locationRow{Key: fl, Name: fl, Type: "floor", Parent: b.Name})
			}
		}
		for _, s := range site.Systems {
			add(locationRow{Key: s.Room, Name: s.Room, Type: "room", Parent: s.Floor})
			r.Systems = append(r.Systems, systemRow{
				Key: s.Name, Name: s.Name, Label: s.Label, Standard: s.Standard, SystemType: s.SystemType, Room: s.Room,
			})
			for _, fill := range s.Roles {
				for n := 1; n <= fill.Count; n++ {
					r.Components = append(r.Components, componentRow{
						Key: fmt.Sprintf("%s/%s/%d", s.Name, fill.Role, n), Product: fill.Product,
						Site: site.Name, Room: s.Room, System: s.Name, Role: fill.Role,
					})
				}
			}
		}
	}
	return r
}

// planRun times the stages of a seed.
type planRun struct {
	res  *PlanResult
	last time.Time
}

func (r *planRun) done(name string) {
	now := time.Now()
	r.res.Phases = append(r.res.Phases, PlanPhase{Name: name, Took: now.Sub(r.last)})
	r.last = now
}

// SeedPlan seeds a resolved plan as configuration rows. actorID is the audit
// actor (empty for a system actor). It returns the counts written and the time
// each stage took.
func SeedPlan(ctx context.Context, gw storage.Gateway, actorID string, p *scenario.Plan, opts PlanOptions) (PlanResult, error) {
	var res PlanResult
	run := &planRun{res: &res, last: time.Now()}
	if _, err := template.CreateAllStandards(ctx, gw, template.Shipped(), actorID); err != nil {
		return res, fmt.Errorf("devseed: create the standards from their templates: %w", err)
	}
	run.done("standards")
	rows := rowsOf(p)
	compIDs, err := seedRows(ctx, gw, actorID, rows, run) // by creation order, parallel to rows.Components
	if err != nil {
		return res, err
	}
	res.Locations, res.Systems, res.Components, res.Roles = len(rows.Locations), len(rows.Systems), len(rows.Components), len(rows.Components)
	if opts.Endpoints {
		n, err := seedPlanEndpoints(ctx, gw, actorID, rows, compIDs, opts, run)
		if err != nil {
			return res, err
		}
		res.Endpoints = n
	}
	return res, nil
}

// seedRows creates the plan's rows through the Gateway's Create methods, one
// transaction each, and returns the component ids in creation order.
func seedRows(ctx context.Context, gw storage.Gateway, actorID string, rows planRows, run *planRun) ([]string, error) {
	all := scope.Set{All: true}
	locIDs := make(map[string]string, len(rows.Locations))
	for _, l := range rows.Locations {
		spec := storage.LocationSpec{Name: l.Name, Label: l.Label, LocationType: l.Type}
		if l.Parent != "" {
			pid := locIDs[l.Parent]
			spec.ParentName = &pid
		}
		created, err := gw.CreateLocation(ctx, actorID, spec, all)
		if err != nil {
			return nil, fmt.Errorf("devseed: create location %q: %w", l.Key, err)
		}
		locIDs[l.Key] = created.ID
	}
	run.done("locations")
	sysIDs := make(map[string]string, len(rows.Systems))
	for _, s := range rows.Systems {
		std, st, loc := s.Standard, s.SystemType, locIDs[s.Room]
		created, err := gw.CreateSystem(ctx, actorID, storage.SystemSpec{
			Name: s.Name, Label: s.Label, StandardID: &std, SystemTypeID: &st, LocationName: &loc,
		}, all, all)
		if err != nil {
			return nil, fmt.Errorf("devseed: create system %q: %w", s.Key, err)
		}
		sysIDs[s.Key] = created.ID
	}
	run.done("systems")
	compIDs := make([]string, 0, len(rows.Components))
	for _, c := range rows.Components {
		prod, loc := c.Product, locIDs[c.Room]
		created, err := gw.CreateComponent(ctx, actorID, storage.ComponentSpec{ProductName: &prod, LocationName: &loc}, all, all, all, all)
		if err != nil {
			return nil, fmt.Errorf("devseed: create component %q: %w", c.Key, err)
		}
		compIDs = append(compIDs, created.ID)
	}
	run.done("components")
	for i, c := range rows.Components {
		if err := gw.AssignRole(ctx, actorID, sysIDs[c.System], c.Role, compIDs[i], all, all, all, all); err != nil {
			return nil, fmt.Errorf("devseed: staff %s of %q: %w", c.Role, c.System, err)
		}
	}
	run.done("roles")
	return compIDs, nil
}
