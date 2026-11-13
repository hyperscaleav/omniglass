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
	// InduceUnregistered makes one simulated device answer a property key no
	// catalog registers (#1698), so its samples are refused: off by default,
	// and only with Endpoints. See InduceSpec.
	InduceUnregistered bool
}

// PlanPhase is the time one stage of a seed took.
type PlanPhase struct {
	Name string
	Took time.Duration
}

// PlanResult is what a SeedPlan wrote and where the time went.
type PlanResult struct {
	Locations, Systems, Components, Roles, Endpoints int
	// Pushers is the outside actors seeded (#1743): one service principal
	// each, granted the pusher role on its systems, holding no token.
	Pushers int
	// Senders is the webhook senders seeded (#1747): one component each,
	// counted in Components, with one webhook endpoint (not counted in
	// Endpoints, which is the simulated fleet's), its own signing secret and
	// a binding per system it speaks for.
	Senders int
	Phases  []PlanPhase
	// Induced is the device InduceUnregistered attached, nil when off.
	Induced *Induced
}

// locationRow is one location of a plan; Parent is the key of another, empty
// for a site.
type locationRow struct{ Key, Name, Label, Type, Parent string }

// systemRow is one system of a plan, placed in the room Room (a location key).
type systemRow struct{ Key, Name, Label, Standard, SystemType, Room string }

// componentRow is one component of a plan: a product placed in a room of Site,
// filling Role of System. Its name is the platform's to mint. A webhook
// sender's component (#1747) is placed at its site and fills no role.
type componentRow struct{ Key, Product, Site, Room, System, Role string }

// planRows is a plan flattened to the rows a seed creates, in creation order.
// Creation order is part of the contract: a platform-minted name is the stem
// plus the lowest free ordinal in its placement, so the same plan always mints
// the same names.
type planRows struct {
	Locations  []locationRow
	Systems    []systemRow
	Components []componentRow
	Senders    []senderRow
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
		for _, ps := range site.Pushers {
			if !ps.Webhook {
				continue
			}
			r.Components = append(r.Components, componentRow{Key: ps.Name, Product: ps.Profile, Site: site.Name, Room: site.Name})
			r.Senders = append(r.Senders, senderRow{Comp: len(r.Components) - 1, Systems: ps.Systems})
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
	senders, err := shippedSenders(rows)
	if err != nil {
		return res, err
	}
	sysIDs, compIDs, err := seedRows(ctx, gw, actorID, rows, run) // compIDs by creation order, parallel to rows.Components
	if err != nil {
		return res, err
	}
	res.Locations, res.Systems, res.Components, res.Roles = len(rows.Locations), len(rows.Systems), len(rows.Components), len(rows.Components)-len(rows.Senders)
	n, err := seedPlanPushers(ctx, gw, actorID, p, sysIDs)
	if err != nil {
		return res, err
	}
	res.Pushers = n
	run.done("pushers")
	if res.Senders, err = seedPlanSenders(ctx, gw, actorID, senders, compIDs, sysIDs); err != nil {
		return res, err
	}
	run.done("senders")
	if opts.Endpoints {
		n, induced, err := seedPlanEndpoints(ctx, gw, actorID, rows, compIDs, opts, run)
		if err != nil {
			return res, err
		}
		res.Endpoints, res.Induced = n, induced
	} else if opts.InduceUnregistered {
		return res, fmt.Errorf("devseed: an induced refusal needs the simulated fleet's endpoints")
	}
	return res, nil
}

// seedPlanPushers seeds each pusher the plan declares as its own service
// principal, granted the pusher role on exactly its systems (GrantPusher,
// the shape the dev fleet's pusher takes). Its token is the lab's to mint
// per run, so none is set here.
func seedPlanPushers(ctx context.Context, gw storage.Gateway, actorID string, p *scenario.Plan, sysIDs map[string]string) (int, error) {
	n := 0
	for _, site := range p.Sites {
		for _, ps := range site.Pushers {
			if ps.Webhook {
				continue // a sender speaks as its component (seedPlanSenders)
			}
			for _, sys := range ps.Systems {
				id, ok := sysIDs[sys]
				if !ok {
					return n, fmt.Errorf("devseed: pusher %s is scoped to system %q, which the plan does not seed", ps.Name, sys)
				}
				if err := GrantPusher(ctx, gw, actorID, ps.Name, id); err != nil {
					return n, err
				}
			}
			n++
		}
	}
	return n, nil
}

// seedRows creates the plan's rows through the Gateway's Create methods, one
// transaction each, and returns the system ids by key and the component ids
// in creation order.
func seedRows(ctx context.Context, gw storage.Gateway, actorID string, rows planRows, run *planRun) (map[string]string, []string, error) {
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
			return nil, nil, fmt.Errorf("devseed: create location %q: %w", l.Key, err)
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
			return nil, nil, fmt.Errorf("devseed: create system %q: %w", s.Key, err)
		}
		sysIDs[s.Key] = created.ID
	}
	run.done("systems")
	compIDs := make([]string, 0, len(rows.Components))
	for _, c := range rows.Components {
		prod, loc := c.Product, locIDs[c.Room]
		created, err := gw.CreateComponent(ctx, actorID, storage.ComponentSpec{ProductName: &prod, LocationName: &loc}, all, all, all, all)
		if err != nil {
			return nil, nil, fmt.Errorf("devseed: create component %q: %w", c.Key, err)
		}
		compIDs = append(compIDs, created.ID)
	}
	run.done("components")
	for i, c := range rows.Components {
		if c.Role == "" {
			continue // a sender fills no role
		}
		if err := gw.AssignRole(ctx, actorID, sysIDs[c.System], c.Role, compIDs[i], all, all, all, all); err != nil {
			return nil, nil, fmt.Errorf("devseed: staff %s of %q: %w", c.Role, c.System, err)
		}
	}
	run.done("roles")
	return sysIDs, compIDs, nil
}
