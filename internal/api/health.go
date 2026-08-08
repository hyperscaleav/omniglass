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
// can act on, so a system's report names the impaired role, which assigned
// components are down, and the alarm that took each one down; a location's
// report names the systems beneath it with their verdicts, and the system read
// explains the rest.
//
// The verdict served is computed from the very evidence served beside it, so
// the two can never be produced by different code paths landing on different
// answers; a role can still show impaired without moving the verdict, though,
// when it belongs to a choice's alternate that lost (#626): that role's
// active flag is false, which is what tells a reader the impairment is not
// the reason for the headline, not a contradiction of it. Reading still
// WRITES nothing: the writes that change health record the transitions, so a
// page view can never stamp an edge at the time somebody looked. The
// transitions are that record.

// healthHistoryWindow bounds the transitions returned with a report, the same
// shape the reachability strip uses.
const healthHistoryWindow = 30 * 24 * time.Hour

type healthAlarmBody struct {
	ID        string    `json:"id"`
	Component string    `json:"component"`
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	RaisedAt  time.Time `json:"raised_at"`
}

type healthRoleBody struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Impact      string `json:"impact" doc:"What an impaired role means for its system: outage, degraded, or none"`
	Quorum      int    `json:"quorum"`
	Satisfying  int    `json:"satisfying" doc:"How many assigned components currently occupy the role (their own verdict is not outage; a degraded component still occupies)"`
	// Short and Spare are occupancy-aware, unlike the roles read's
	// understaffed (Quorum minus len(assigned_to), health-blind): a role can
	// report understaffed 0 here and still be short if an assigned
	// component's own verdict is outage. They live here rather than on the
	// declaration read because computing them needs each occupant's
	// current verdict, which only this read resolves.
	Short      int               `json:"short" doc:"How many more occupants the role needs to reach quorum, counting only those currently occupying (own verdict not outage); zero once it does. Diverges from the roles read's understaffed when an assigned component is down"`
	Spare      int               `json:"spare" doc:"How many occupants the role has beyond quorum; zero at or below quorum"`
	Impaired   bool              `json:"impaired" doc:"True when satisfying is below quorum"`
	AssignedTo []string          `json:"assigned_to"`
	Down       []string          `json:"down" doc:"The assigned components whose own verdict is outage; empty when the role is merely short-staffed or only degraded"`
	Alarms     []healthAlarmBody `json:"alarms" doc:"The active alarms on those down components"`
	// Choice, Alternate and Active are the #626 grouping: a role can belong
	// to an exclusive-or group instead of contributing unconditionally.
	// Choice and Alternate are empty for an unconditional role (it always
	// counts, active is always true). A role whose Active is false can
	// still show Impaired true: it is impaired on its own terms, but it is
	// not why the system's verdict reads what it does, because its choice
	// was answered through a different alternate. Rendering impaired
	// without checking active is exactly the contradiction this field
	// exists to prevent: it reads as an impairment the verdict ignored,
	// when it is really one the verdict was never going to count.
	Choice    string `json:"choice,omitempty" doc:"The choice this role belongs to; absent when the role is unconditional"`
	Alternate string `json:"alternate,omitempty" doc:"The alternate within choice this role belongs to; absent when the role is unconditional"`
	Active    bool   `json:"active" doc:"True for an unconditional role, or a role whose alternate is the one currently answering its choice. False means this role's own impaired/short/spare figures did not contribute to the system's verdict"`
}

type healthSystemBody struct {
	Name    string `json:"name"`
	Verdict string `json:"verdict"`
}

type healthTransitionBody struct {
	TS      time.Time `json:"ts"`
	Verdict string    `json:"verdict"`
}

type estateHealthOutput struct {
	Body struct {
		OwnerKind   string                 `json:"owner_kind"`
		Owner       string                 `json:"owner"`
		Verdict     string                 `json:"verdict" doc:"healthy, degraded, or outage: the rollup of the roles or systems served beside it"`
		Roles       []healthRoleBody       `json:"roles" doc:"The contributing roles; empty for a location"`
		Systems     []healthSystemBody     `json:"systems" doc:"The systems beneath a location with their verdicts; empty for a system"`
		Transitions []healthTransitionBody `json:"transitions" doc:"The recorded edges over the window, oldest first: one entry per change, never a sample"`
	}
}

func toHealthOutput(rep *storage.HealthReport) *estateHealthOutput {
	out := &estateHealthOutput{}
	out.Body.OwnerKind = rep.OwnerKind
	out.Body.Owner = rep.OwnerID
	out.Body.Verdict = rep.Verdict
	out.Body.Roles = make([]healthRoleBody, 0, len(rep.Roles))
	for i := range rep.Roles {
		out.Body.Roles = append(out.Body.Roles, toHealthRoleBody(&rep.Roles[i]))
	}
	out.Body.Systems = make([]healthSystemBody, 0, len(rep.Systems))
	for _, s := range rep.Systems {
		out.Body.Systems = append(out.Body.Systems, healthSystemBody{Name: s.Name, Verdict: s.Verdict})
	}
	out.Body.Transitions = make([]healthTransitionBody, 0, len(rep.Transitions))
	for _, t := range rep.Transitions {
		out.Body.Transitions = append(out.Body.Transitions, healthTransitionBody{TS: t.TS, Verdict: t.Value})
	}
	return out
}

func toHealthRoleBody(r *storage.HealthRole) healthRoleBody {
	body := healthRoleBody{
		Name:        r.Name,
		DisplayName: r.DisplayName,
		Impact:      r.Impact,
		Quorum:      r.Quorum,
		Satisfying:  r.Satisfying,
		Short:       r.Short,
		Spare:       r.Spare,
		Impaired:    r.Impaired,
		AssignedTo:  nonNil(r.AssignedTo),
		Down:        nonNil(r.Down),
		Alarms:      make([]healthAlarmBody, 0, len(r.Alarms)),
		Choice:      r.Choice,
		Alternate:   r.Alternate,
		Active:      r.Active,
	}
	for i := range r.Alarms {
		a := &r.Alarms[i]
		body.Alarms = append(body.Alarms, healthAlarmBody{
			ID:        a.ID,
			Component: a.ComponentID,
			Severity:  a.Severity,
			Message:   a.Message,
			RaisedAt:  a.RaisedAt,
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

// registerHealthRoutes wires the system and location health reads.
func registerHealthRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-system-health",
		Method:      http.MethodGet,
		Path:        "/systems/{name}/health",
		Summary:     "Read a system's health",
		Description: "The system's current verdict and why: every role it needs filled, whether it is impaired, what an impaired role means for the system (impact), and for an impaired role which assigned components are down plus the alarms that took them down. A role that belongs to a choice (#626, an exclusive-or group such as an all-in-one alternate versus a component-built one) carries choice and alternate, and active is false when a different alternate answered the choice, meaning this role's own impaired figure did not move the verdict. Transitions are the recorded edges over the last 30 days, one entry per change. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemPathInput) (*estateHealthOutput, error) {
		since := time.Now().UTC().Add(-healthHistoryWindow)
		rep, err := gw.SystemHealth(ctx, in.Name, since, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return toHealthOutput(rep), nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-location-health",
		Method:      http.MethodGet,
		Path:        "/locations/{name}/health",
		Summary:     "Read a location's health",
		Description: "The location's current verdict, worst-wins over every system placed anywhere beneath it, with those systems and their verdicts as the drill-down (the system health read names the role, which occupant is down, and the alarm). Transitions are the recorded edges over the last 30 days. Gated by location:read; an out-of-scope location is a non-disclosing 404.",
	}, "location", "read"), func(ctx context.Context, in *locationPathInput) (*estateHealthOutput, error) {
		since := time.Now().UTC().Add(-healthHistoryWindow)
		rep, err := gw.LocationHealth(ctx, in.Name, since, a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return toHealthOutput(rep), nil
	})
}
