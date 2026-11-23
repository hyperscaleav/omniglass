package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The health read: what a system or location's verdict is, WHY, and when it last
// changed. The why is the point. A bare "degraded" tells an operator nothing they
// can act on, so a system's report names the impaired slot, which assigned
// components are down, and the alarm that took each one down; a location's
// report names the systems beneath it with their verdicts, and the system read
// explains the rest.
//
// The verdict served is computed from the very evidence served beside it, so
// the two can never be produced by different code paths landing on different
// answers; a slot can still show impaired without moving the verdict, though,
// when it belongs to a choice's alternate that lost (#626): that slot's
// active flag is false, which is what tells a reader the impairment is not
// the reason for the headline, not a contradiction of it. Reading still
// WRITES nothing: the writes that change health record the transitions, so a
// page view can never stamp an edge at the time somebody looked. The
// transitions are that record.

// healthHistoryWindow is the default span of transitions returned with a
// report, the same shape the reachability strip uses: the last 30 days.
const healthHistoryWindow = 30 * 24 * time.Hour

// The range every health read takes (#2305), so a range picker can drive the
// system, location and component reads alike.

type systemHealthInput struct {
	Name  string    `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Since time.Time `query:"since" doc:"Only the transitions recorded at or after this RFC 3339 time. Defaults to 30 days before until, or before now when until is absent" example:"2026-11-01T00:00:00Z"`
	Until time.Time `query:"until" doc:"Only the transitions recorded at or before this RFC 3339 time. Defaults to now; must not be earlier than since" example:"2026-11-23T00:00:00Z"`
}

type locationHealthInput struct {
	Name  string    `path:"name" doc:"The location's name, or a dotted address (e.g. boi.17c.415a)"`
	Since time.Time `query:"since" doc:"Only the transitions recorded at or after this RFC 3339 time. Defaults to 30 days before until, or before now when until is absent" example:"2026-11-01T00:00:00Z"`
	Until time.Time `query:"until" doc:"Only the transitions recorded at or before this RFC 3339 time. Defaults to now; must not be earlier than since" example:"2026-11-23T00:00:00Z"`
}

type componentHealthInput struct {
	Name  string    `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)" example:"hq.west.level-2.boardroom-a.$comp.videobar-1"`
	Since time.Time `query:"since" doc:"Only the transitions recorded at or after this RFC 3339 time. Defaults to 30 days before until, or before now when until is absent" example:"2026-11-01T00:00:00Z"`
	Until time.Time `query:"until" doc:"Only the transitions recorded at or before this RFC 3339 time. Defaults to now; must not be earlier than since" example:"2026-11-23T00:00:00Z"`
}

// healthRange turns the query into the Gateway's range. since after until is a
// 400. An absent since is the default trailing 30 days: measured on the
// database's clock when until is absent too (so the server never names that
// boundary), else 30 days before the until the caller named.
func healthRange(since, until time.Time) (storage.HealthRange, error) {
	if !since.IsZero() && !until.IsZero() && since.After(until) {
		return storage.HealthRange{}, huma.Error400BadRequest("since must not be after until")
	}
	r := storage.HealthRange{Since: since, Until: until}
	if r.Since.IsZero() {
		if r.Until.IsZero() {
			r.Window = healthHistoryWindow
		} else {
			r.Since = r.Until.Add(-healthHistoryWindow)
		}
	}
	return r, nil
}

type healthAlarmBody struct {
	ID          string    `json:"id"`
	Component   string    `json:"component" doc:"The component the alarm is on, by name"`
	ComponentID string    `json:"component_id" doc:"The component's uuid"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
	RaisedAt    time.Time `json:"raised_at"`
	// The acknowledgement, with the same names and rules as the component alarm
	// list (alarmBody), so a system page can show and offer it without opening
	// each component (#990). Orthogonal to cleared (ADR-0109).
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty" doc:"When a human first recorded seeing this alarm; null while nobody has. Independent of cleared_at"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty" doc:"Who acknowledged it, by name; empty while unacknowledged, or once that user has been purged (the audit log keeps the name)"`
	Acknowledged   bool       `json:"acknowledged" doc:"Whether anybody has recorded seeing this alarm; says nothing about whether it is still raised"`
}

type healthSlotBody struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Impact     string `json:"impact" doc:"What an impaired slot means for its system: outage, degraded, or none"`
	Quorum     int    `json:"quorum"`
	Satisfying int    `json:"satisfying" doc:"How many assigned components currently occupy the slot (their own health is not outage; a degraded component still occupies)"`
	// Short and Spare are occupancy-aware, unlike the slots read's
	// understaffed (Quorum minus len(assigned_to), health-blind): a slot can
	// report understaffed 0 here and still be short if an assigned
	// component's own verdict is outage. They live here rather than on the
	// declaration read because computing them needs each occupant's
	// current verdict, which only this read resolves.
	Short      int               `json:"short" doc:"How many more occupants the slot needs to reach quorum, counting only those currently occupying (own health not outage); zero once it does. Diverges from the slots read's understaffed when an assigned component is down"`
	Spare      int               `json:"spare" doc:"How many occupants the slot has beyond quorum; zero at or below quorum"`
	Impaired   bool              `json:"impaired" doc:"True when satisfying is below quorum"`
	AssignedTo []string          `json:"assigned_to"`
	Down       []string          `json:"down" doc:"The assigned components whose own health is outage; empty when the slot is merely short-staffed or only degraded"`
	Alarms     []healthAlarmBody `json:"alarms" doc:"The active alarms on those down components"`
	// Choice, Alternate and Active are the #626 grouping: a slot can belong
	// to an exclusive-or group instead of contributing unconditionally.
	// Choice and Alternate are empty for an unconditional slot (it always
	// counts, active is always true). A slot whose Active is false can
	// still show Impaired true: it is impaired on its own terms, but it is
	// not why the system's verdict reads what it does, because its choice
	// was answered through a different alternate. Rendering impaired
	// without checking active is exactly the contradiction this field
	// exists to prevent: it reads as an impairment the verdict ignored,
	// when it is really one the verdict was never going to count.
	Choice    string `json:"choice,omitempty" doc:"The choice this slot belongs to; absent when the slot is unconditional"`
	Alternate string `json:"alternate,omitempty" doc:"The alternate within choice this slot belongs to; absent when the slot is unconditional"`
	Active    bool   `json:"active" doc:"True for an unconditional slot, or a slot whose alternate is the one currently answering its choice. False means this slot's own impaired/short/spare figures did not contribute to the system's health"`
}

type healthSystemBody struct {
	Name   string `json:"name"`
	Health string `json:"health"`
}

type healthTransitionBody struct {
	TS     time.Time `json:"ts"`
	Health string    `json:"health"`
}

type fleetHealthOutput struct {
	Body struct {
		OwnerKind   string                 `json:"owner_kind"`
		Owner       string                 `json:"owner"`
		Health      string                 `json:"health" doc:"healthy, incomplete, degraded, or outage: the rollup of the slots or systems served beside it. incomplete is a commissioning gap, a slot short because the hardware was never installed rather than because installed hardware is failing, and it ranks between healthy and degraded"`
		Slots       []healthSlotBody       `json:"slots" doc:"The contributing slots; empty for a location"`
		Systems     []healthSystemBody     `json:"systems" doc:"The systems beneath a location with their health; empty for a system"`
		Transitions []healthTransitionBody `json:"transitions" doc:"The recorded edges over the window, oldest first: one entry per change, never a sample"`
	}
}

func toHealthOutput(rep *storage.HealthReport) *fleetHealthOutput {
	out := &fleetHealthOutput{}
	out.Body.OwnerKind = rep.OwnerKind
	out.Body.Owner = rep.OwnerID
	out.Body.Health = rep.Verdict
	out.Body.Slots = make([]healthSlotBody, 0, len(rep.Slots))
	for i := range rep.Slots {
		out.Body.Slots = append(out.Body.Slots, toHealthSlotBody(&rep.Slots[i]))
	}
	out.Body.Systems = make([]healthSystemBody, 0, len(rep.Systems))
	for _, s := range rep.Systems {
		out.Body.Systems = append(out.Body.Systems, healthSystemBody{Name: s.Name, Health: s.Verdict})
	}
	out.Body.Transitions = make([]healthTransitionBody, 0, len(rep.Transitions))
	for _, t := range rep.Transitions {
		out.Body.Transitions = append(out.Body.Transitions, healthTransitionBody{TS: t.TS, Health: t.Value})
	}
	return out
}

func toHealthSlotBody(r *storage.HealthSlot) healthSlotBody {
	body := healthSlotBody{
		Name:       r.Name,
		Label:      r.Label,
		Impact:     r.Impact,
		Quorum:     r.Quorum,
		Satisfying: r.Satisfying,
		Short:      r.Short,
		Spare:      r.Spare,
		Impaired:   r.Impaired,
		AssignedTo: nonNil(r.AssignedTo),
		Down:       nonNil(r.Down),
		Alarms:     make([]healthAlarmBody, 0, len(r.Alarms)),
		Choice:     r.Choice,
		Alternate:  r.Alternate,
		Active:     r.Active,
	}
	for i := range r.Alarms {
		a := &r.Alarms[i]
		body.Alarms = append(body.Alarms, healthAlarmBody{
			ID:          a.ID,
			Component:   a.ComponentName,
			ComponentID: a.ComponentID,
			Severity:    a.Severity,
			Message:     a.Message,
			RaisedAt:    a.RaisedAt,

			AcknowledgedAt: a.AcknowledgedAt,
			AcknowledgedBy: a.AcknowledgedBy,
			Acknowledged:   a.Acknowledged(),
		})
	}
	return body
}

// nonNil keeps a nil slice out of the JSON, so a client never has to tell an
// absent list from an empty one.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// systemHealthListOutput is the BULK health read's body: one verdict per system the
// caller can see, keyed by uuid because that is what a list row holds and because
// a system name is scoped to its placement rather than unique fleet-wide.
//
// Deliberately not a list of health REPORTS. The report is the read a system's
// panel opens; this is the read a list paints a column from, and the whole point
// is that it carries the one fact the column renders and nothing else (#653).
type systemHealthListOutput struct {
	Body struct {
		Systems []systemHealthEntryBody `json:"systems" doc:"One entry per system in the caller's read scope, in no particular order."`
	}
}

type systemHealthEntryBody struct {
	System string `json:"system" doc:"The system's uuid"`
	Health string `json:"health" doc:"healthy, degraded, or outage"`
}

// registerHealthRoutes wires the system, location and component health reads.
func registerHealthRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-system-health",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/systems/{name}/health",
		Summary:     "Read a system's health",
		Description: "The system's current health and why: every slot it needs filled, whether it is impaired, what an impaired slot means for the system (impact), and for an impaired slot which assigned components are down plus the alarms that took them down. A slot that belongs to a choice (#626, an exclusive-or group such as an all-in-one alternate versus a component-built one) carries choice and alternate, and active is false when a different alternate answered the choice, meaning this slot's own impaired figure did not move the health. Transitions are the recorded edges inside since..until (default the last 30 days), one entry per change; since after until is a 400. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemHealthInput) (*fleetHealthOutput, error) {
		rng, err := healthRange(in.Since, in.Until)
		if err != nil {
			return nil, err
		}
		rep, err := gw.SystemHealth(ctx, in.Name, rng, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return toHealthOutput(rep), nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-system-health",
		Method:      http.MethodGet,
		Path:        "/systems:health",
		Summary:     "Read every system's health",
		Description: "The current health for every system in the caller's read scope, in one call: the bulk counterpart of the per-system health read, carrying the one fact a list's health column renders and none of the explanation. A console painting a page of systems reads this once instead of resolving every slot, occupant, alarm and transition per row. A system that has never been recomputed reads healthy. Gated by system:read; an empty scope is an empty list, not a 403.",
	}, "system", "read"), func(ctx context.Context, _ *struct{}) (*systemHealthListOutput, error) {
		vs, err := gw.SystemHealths(ctx, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		out := &systemHealthListOutput{}
		out.Body.Systems = make([]systemHealthEntryBody, 0, len(vs))
		for _, v := range vs {
			out.Body.Systems = append(out.Body.Systems, systemHealthEntryBody{System: v.SystemID, Health: v.Verdict})
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-location-health",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/locations/{name}/health",
		Summary:     "Read a location's health",
		Description: "The location's current health, worst-wins over every system placed anywhere beneath it, with those systems and their health as the drill-down (the system health read names the slot, which occupant is down, and the alarm). Transitions are the recorded edges inside since..until (default the last 30 days); since after until is a 400. Gated by location:read; an out-of-scope location is a non-disclosing 404.",
	}, "location", "read"), func(ctx context.Context, in *locationHealthInput) (*fleetHealthOutput, error) {
		rng, err := healthRange(in.Since, in.Until)
		if err != nil {
			return nil, err
		}
		rep, err := gw.LocationHealth(ctx, in.Name, rng, a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return toHealthOutput(rep), nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-component-health",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/health",
		Summary:     "Read a component's health",
		Description: "The component's own current health (healthy, degraded or outage from its active alarms; unknown for a device nothing has observed) beside the recorded edges that got it there, in the shape the system health read returns: slots and systems are empty, transitions are one entry per change inside since..until (default the last 30 days), oldest first. since after until is a 400. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentHealthInput) (*fleetHealthOutput, error) {
		rng, err := healthRange(in.Since, in.Until)
		if err != nil {
			return nil, err
		}
		rep, err := gw.ComponentHealthReport(ctx, in.Name, rng, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return toHealthOutput(rep), nil
	})
}
