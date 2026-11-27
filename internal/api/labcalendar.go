package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// MaxLabCalendarWindow is the widest window GET /lab/calendar answers.
const MaxLabCalendarWindow = 31 * 24 * time.Hour

type labCalendarInput struct {
	Start    time.Time `query:"start" required:"true" doc:"Start of the window (RFC 3339)."`
	End      time.Time `query:"end" required:"true" doc:"End of the window (RFC 3339), after start and at most 31 days past it."`
	Site     string    `query:"site" required:"false" doc:"Only rooms at this site."`
	Building string    `query:"building" required:"false" doc:"Only rooms in this building."`
	Room     string    `query:"room" required:"false" doc:"Only this room."`
	System   string    `query:"system" required:"false" doc:"Only the room of this system, by name."`
}

type labCalendarEntry struct {
	Kind     string         `json:"kind" doc:"What the entry is: meeting, or a scripted problem (#2769)."`
	Start    time.Time      `json:"start" doc:"When the entry begins on the simulator's clock."`
	End      time.Time      `json:"end" doc:"When it ends."`
	Site     string         `json:"site"`
	Building string         `json:"building"`
	Room     string         `json:"room"`
	System   string         `json:"system" doc:"The system built in the room."`
	Payload  map[string]any `json:"payload" nullable:"false" doc:"The entry's own fields, by kind. A meeting carries origin (booked or walk-in), organizer, expected, actual, outcome (held, no-show, ended-early, ended-late), slot_start, slot_end and recorded. A problem carries name, role, unplugged, loss_pct and, when it pins a path, pin_path and pin_value."`
}

type labCalendarOutput struct {
	Body struct {
		Start   time.Time          `json:"start"`
		End     time.Time          `json:"end"`
		Entries []labCalendarEntry `json:"entries" nullable:"false" doc:"What the simulator intends in the window, in start order, for the rooms whose system the caller may read."`
	}
}

// labCalendarPayload is the entry's kind-specific fields as an open object.
func labCalendarPayload(e calendar.Entry) map[string]any {
	p := map[string]any{}
	if m, ok := e.AsMeeting(); ok {
		p["origin"] = string(m.Origin)
		p["organizer"] = m.Organizer
		p["expected"] = m.Expected
		p["actual"] = m.Actual
		p["outcome"] = string(m.Outcome)
		p["slot_start"] = m.SlotStart.UTC().Format(time.RFC3339)
		p["slot_end"] = m.SlotEnd.UTC().Format(time.RFC3339)
		p["recorded"] = m.Recorded
	}
	if pr, ok := e.AsProblem(); ok {
		p["name"] = pr.Name
		p["role"] = pr.Role
		p["unplugged"] = pr.Impairment.Unplugged
		p["loss_pct"] = pr.Impairment.LossPct
		if pin := pr.Impairment.Pin; pin != nil {
			p["pin_path"] = pin.Path
			p["pin_value"] = pin.Value
		}
	}
	return p
}

func registerLabCalendarRoutes(api huma.API, a *authenticator, gw storage.Gateway, src LabCalendarSource) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-lab-calendar",
		Method:      http.MethodGet,
		Path:        "/lab/calendar",
		Summary:     "What the lab's simulator intends in a window",
		Description: "The lab calendar (#2757) for a window: the meetings and scripted problems every simulated room acts out, the ground truth to lay beside what Omniglass detected. Derived from the lab's scenario and seed (the same calendar the simulator computes), never stored or learned from collected data. Lab only: a server that is not a lab answers 404. Gated by simulation:read; entries are limited to rooms whose system is in the caller's system read scope. The window is at most 31 days; a wider one is a 422. site, building, room and system narrow it further.",
	}, "simulation", "read"), func(ctx context.Context, in *labCalendarInput) (*labCalendarOutput, error) {
		if !a.lab {
			return nil, huma.Error404NotFound("not found")
		}
		if !in.End.After(in.Start) {
			return nil, huma.Error422UnprocessableEntity("end must be after start")
		}
		if in.End.Sub(in.Start) > MaxLabCalendarWindow {
			return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("the window is at most %d days; ask for a narrower one", int(MaxLabCalendarWindow/(24*time.Hour))))
		}
		out := &labCalendarOutput{}
		out.Body.Start, out.Body.End = in.Start.UTC(), in.End.UTC()
		out.Body.Entries = []labCalendarEntry{}
		if src == nil {
			return out, nil
		}
		systems, err := gw.ListSystems(ctx, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("list systems")
		}
		readable := make(map[string]bool, len(systems))
		for _, s := range systems {
			readable[s.Name] = true
		}
		for _, r := range src.Rooms() {
			switch {
			case !readable[r.System],
				in.Site != "" && in.Site != r.Site,
				in.Building != "" && in.Building != r.Building,
				in.Room != "" && in.Room != r.Room.Room,
				in.System != "" && in.System != r.System:
				continue
			}
			for _, e := range src.Entries(r.Room, in.Start, in.End, calendar.AllKinds) {
				out.Body.Entries = append(out.Body.Entries, labCalendarEntry{
					Kind: string(e.Kind), Start: e.Start.UTC(), End: e.End.UTC(),
					Site: r.Site, Building: r.Building, Room: r.Room.Room, System: r.System,
					Payload: labCalendarPayload(e),
				})
			}
		}
		sort.SliceStable(out.Body.Entries, func(i, j int) bool {
			x, y := out.Body.Entries[i], out.Body.Entries[j]
			if !x.Start.Equal(y.Start) {
				return x.Start.Before(y.Start)
			}
			return x.System < y.System
		})
		return out, nil
	})
}
