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
// can act on, so a system's report names the impaired role, the required
// capability an alarm took away, and the alarm that took it; a location's report
// names the systems beneath it with their verdicts, and the system read explains
// the rest.
//
// The verdict served is computed from the very evidence served beside it, so the
// headline and the reason can never disagree. Reading still WRITES nothing: the
// writes that change health record the transitions, so a page view can never stamp
// an edge at the time somebody looked. The transitions are that record.

// healthHistoryWindow bounds the transitions returned with a report, the same
// shape the reachability strip uses.
const healthHistoryWindow = 30 * 24 * time.Hour

type healthAlarmBody struct {
	ID           string    `json:"id"`
	Component    string    `json:"component"`
	Severity     string    `json:"severity"`
	Message      string    `json:"message"`
	Capabilities []string  `json:"capabilities"`
	RaisedAt     time.Time `json:"raised_at"`
}

type healthRoleBody struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Impact      string            `json:"impact" doc:"What an impaired role means for its system: outage, degraded, or none"`
	Required    []string          `json:"required" doc:"The capabilities a component must ALL provide to fill this role"`
	Quorum      int               `json:"quorum"`
	Satisfying  int               `json:"satisfying" doc:"How many assigned components can currently fill the role"`
	Impaired    bool              `json:"impaired" doc:"True when satisfying is below quorum"`
	AssignedTo  []string          `json:"assigned_to"`
	Degraded    []string          `json:"degraded" doc:"The required capabilities an active alarm has taken away; empty when the role is merely short-staffed"`
	Alarms      []healthAlarmBody `json:"alarms" doc:"The active alarms that degraded them"`
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
		Required:    nonNil(r.Required),
		Quorum:      r.Quorum,
		Satisfying:  r.Satisfying,
		Impaired:    r.Impaired,
		AssignedTo:  nonNil(r.AssignedTo),
		Degraded:    nonNil(r.Degraded),
		Alarms:      make([]healthAlarmBody, 0, len(r.Alarms)),
	}
	for i := range r.Alarms {
		a := &r.Alarms[i]
		body.Alarms = append(body.Alarms, healthAlarmBody{
			ID:           a.ID,
			Component:    a.ComponentID,
			Severity:     a.Severity,
			Message:      a.Message,
			Capabilities: nonNil(a.Capabilities),
			RaisedAt:     a.RaisedAt,
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
		Description: "The system's current verdict and why: every role it needs filled, whether it is impaired, what an impaired role means for the system (impact), and for an impaired role the required capabilities an alarm has taken away plus the alarms that took them. Transitions are the recorded edges over the last 30 days, one entry per change. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
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
		Description: "The location's current verdict, worst-wins over every system placed anywhere beneath it, with those systems and their verdicts as the drill-down (the system health read names the role, the capability, and the alarm). Transitions are the recorded edges over the last 30 days. Gated by location:read; an out-of-scope location is a non-disclosing 404.",
	}, "location", "read"), func(ctx context.Context, in *locationPathInput) (*estateHealthOutput, error) {
		since := time.Now().UTC().Add(-healthHistoryWindow)
		rep, err := gw.LocationHealth(ctx, in.Name, since, a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return toHealthOutput(rep), nil
	})
}
