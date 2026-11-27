package devseed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/template"
)

// A resolved scenario plan (internal/lab/scenario) seeds as configuration rows:
// the sites, buildings, floors and rooms, the systems in the rooms, the
// components staffing each system's slots, and the simulated endpoints on the
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
	// Outpost, when set, places the endpoints on that outpost (which must exist).
	Outpost string
	// OutpostFor, when set, places each site's endpoints on the outpost it names
	// for that site (a plan site's name), and takes precedence over Outpost. A
	// site it names "" for falls back to Outpost. Each outpost must exist.
	OutpostFor func(site string) string
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
	Locations, Systems, Components, Slots, Endpoints int
	// Pushers is the outside actors seeded (#1743): one service user
	// each, granted the pusher role on its systems, holding no token.
	Pushers int
	// Senders is the webhook senders seeded (#1747): one component each,
	// counted in Components, with one webhook endpoint (not counted in
	// Endpoints, which is the simulated fleet's), its own signing secret and
	// a binding per system it speaks for.
	Senders int
	// Services is the polled services seeded (#2765), each with an identity binding per room it covers.
	Services int
	Phases   []PlanPhase
	// Induced is the device InduceUnregistered attached, nil when off.
	Induced *Induced
}

// locationRow is one location of a plan; Parent is the key of another, empty
// for a site.
type locationRow struct{ Key, Name, Label, Type, Parent string }

// systemRow is one system of a plan, placed in the room Room (a location key).
type systemRow struct{ Key, Name, Label, Standard, SystemType, Room string }

// componentRow is one component of a plan: a product placed in a room of Site,
// filling Slot of System. Its Name is the plan's own (the world's, #2773):
// the seed onboards the component under exactly that name. A webhook
// sender's component (#1747) is placed at its site and fills no slot.
type componentRow struct {
	Key, Name, Product, Site, Room, System, Slot string
	// Control wires the device's control face too (scenario.SlotFill.Control).
	Control bool
	// Platform is the kind a meeting platform lists the component as, "" when
	// it lists none (scenario.SlotFill.Platform, #2790).
	Platform string
	// Label is the label the component is created with ("" lets the platform
	// make it): a polled service's, from its manifest entry (#2757).
	Label string
	// Ordinal is the component's place among a slot of several (1-based), 0
	// for a slot of one. A name the world gave carries no number the platform
	// owns, so the seed puts the ordinal in the label itself (#2757).
	Ordinal int
}

// label is the label the seed creates the component with, "" to let the
// platform make it: the manifest's for a service, else the component type's
// label and the slot ordinal when the slot holds several, as the platform's own
// rule reads for a name it minted (#2757). typeLabels is the label of each
// product's component type.
func (c componentRow) label(typeLabels map[string]string) string {
	if c.Label != "" {
		return c.Label
	}
	if base := typeLabels[c.Product]; c.Ordinal > 0 && base != "" {
		return fmt.Sprintf("%s %d", base, c.Ordinal)
	}
	return ""
}

// componentTypeLabelsByProduct is the label of the component type each shipped
// product is classified under.
func componentTypeLabelsByProduct() map[string]string {
	types := seed.ShippedComponentTypeLabels()
	out := map[string]string{}
	for _, p := range seed.ShippedProducts() {
		out[p.Name] = types[p.ComponentType]
	}
	return out
}

// planRows is a plan flattened to the rows a seed creates, in creation order.
// Creation order is part of the contract: a platform-minted name is the stem
// plus the lowest free ordinal in its placement, so the same plan always mints
// the same names.
type planRows struct {
	Locations  []locationRow
	Systems    []systemRow
	Components []componentRow
	Senders    []senderRow
	// Services are the polled services (#2765): components with no slot and no webhook.
	Services []serviceRow
}

// pusherComponents is how many components are a sender or a service rather
// than a slot's: one that is both counts once.
func (r planRows) pusherComponents() int {
	seen := map[int]bool{}
	for _, s := range r.Senders {
		seen[s.Comp] = true
	}
	for _, s := range r.Services {
		seen[s.Comp] = true
	}
	return len(seen)
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
			for _, fill := range s.Slots {
				for n := 1; n <= fill.Count; n++ {
					ordinal := 0
					if fill.Count > 1 {
						ordinal = n
					}
					r.Components = append(r.Components, componentRow{Ordinal: ordinal,
						Key: fmt.Sprintf("%s/%s/%d", s.Name, fill.Slot, n), Name: scenario.ComponentName(s.Name, fill.Slot, n), Product: fill.Product,
						Site: site.Name, Room: s.Room, System: s.Name, Slot: fill.Slot, Control: fill.Control, Platform: fill.Platform,
					})
				}
			}
		}
		for _, ps := range site.Pushers {
			if !ps.IsComponent() {
				continue
			}
			r.Components = append(r.Components, componentRow{Key: ps.Name, Name: ps.Name, Label: ps.Label, Product: ps.Profile, Site: site.Name, Room: site.Name})
			comp := len(r.Components) - 1
			if ps.Webhook {
				r.Senders = append(r.Senders, senderRow{Comp: comp, Systems: ps.Systems, Service: ps.Service})
			}
			if ps.Service {
				r.Services = append(r.Services, serviceRow{Comp: comp, Systems: ps.Systems})
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
	senders, err := shippedSenders(rows)
	if err != nil {
		return res, err
	}
	sysIDs, compIDs, err := seedRows(ctx, gw, actorID, rows, run) // compIDs by creation order, parallel to rows.Components
	if err != nil {
		return res, err
	}
	res.Locations, res.Systems, res.Components, res.Slots = len(rows.Locations), len(rows.Systems), len(rows.Components), len(rows.Components)-rows.pusherComponents()
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
	if res.Services, err = seedPlanServices(ctx, gw, actorID, rows, compIDs, sysIDs); err != nil {
		return res, err
	}
	run.done("services")
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
// user, granted the pusher role on exactly its systems (GrantPusher,
// the shape the dev fleet's pusher takes). Its token is the lab's to mint
// per run, so none is set here.
func seedPlanPushers(ctx context.Context, gw storage.Gateway, actorID string, p *scenario.Plan, sysIDs map[string]string) (int, error) {
	n := 0
	for _, site := range p.Sites {
		for _, ps := range site.Pushers {
			if ps.IsComponent() {
				continue // a sender speaks as its component (seedPlanSenders); a service is polled
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
		created, err := gw.CreateLocation(ctx, actorID, spec, all, all)
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
	typeLabels := componentTypeLabelsByProduct()
	for _, c := range rows.Components {
		prod, loc := c.Product, locIDs[c.Room]
		created, err := gw.CreateComponent(ctx, actorID, storage.ComponentSpec{Name: c.Name, Label: c.label(typeLabels), ProductName: &prod, LocationName: &loc}, all, all, all, all)
		if err != nil {
			return nil, nil, fmt.Errorf("devseed: create component %q: %w", c.Key, err)
		}
		compIDs = append(compIDs, created.ID)
	}
	run.done("components")
	for i, c := range rows.Components {
		if c.Slot == "" {
			continue // a sender fills no slot
		}
		if err := gw.AssignSlot(ctx, actorID, sysIDs[c.System], c.Slot, compIDs[i], all, all, all, all); err != nil {
			return nil, nil, fmt.Errorf("devseed: staff %s of %q: %w", c.Slot, c.System, err)
		}
	}
	run.done("slots")
	if err := seedRoomRules(ctx, gw, actorID); err != nil {
		return nil, nil, err
	}
	run.done("rules")
	return sysIDs, compIDs, nil
}

// roomRules are the calc rules that carry a member's reading up to its room
// (#2757): a standard declares a metric on the system, a device reports it on
// its component, and a rule reads it from every member the system's slots hold
// and writes one value as the room's own. Seeded with the plan, as the plan's
// components are, and inert in a room with no such member.
// It is a function, not a shared value: creating a rule writes the resolved
// key ids back into its inputs, which would carry one database's ids to the next.
func roomRules() []storage.CalcRuleSpec {
	return []storage.CalcRuleSpec{
		{
			Name: "room-occupancy", Label: "Room occupancy",
			Description: "A room's occupancy-count is the most people any of its people counters reports.",
			OwnerKind:   "system",
			Inputs:      []rules.CalcInput{{Alias: "c", Key: "people-count"}},
			Reduce:      rules.CalcReduce{Expr: "max(c)"},
			OutputKey:   "occupancy-count",
		},
		{
			Name: "room-temperature", Label: "Room temperature",
			Description: "A room's room-temperature is the average of what its members read, the control processor's sensor being the lab's.",
			OwnerKind:   "system",
			Inputs:      []rules.CalcInput{{Alias: "c", Key: "room-sensor-temperature"}},
			Reduce:      rules.CalcReduce{Expr: "avg(c)"},
			OutputKey:   "room-temperature",
		},
	}
}

// seedRoomRules seeds each of roomRules once; a plan seeded again finds them
// and leaves them as they are.
func seedRoomRules(ctx context.Context, gw storage.Gateway, actorID string) error {
	for _, spec := range roomRules() {
		if _, err := gw.GetCalcRule(ctx, spec.Name); err == nil {
			continue
		} else if !errors.Is(err, storage.ErrCalcRuleNotFound) {
			return fmt.Errorf("devseed: look up calc rule %q: %w", spec.Name, err)
		}
		if _, err := gw.CreateCalcRule(ctx, actorID, spec, scope.Set{All: true}); err != nil && !errors.Is(err, storage.ErrCalcRuleExists) {
			return fmt.Errorf("devseed: create calc rule %q: %w", spec.Name, err)
		}
	}
	return nil
}
