//go:build labintegration

package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/labcheck"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
)

// A lab test proves a meeting live, not only idle (#2800): the lab runs with
// the simulator's clock offset (--sim-clock-offset), chosen from the plan's
// own calendar so that "now" falls half a minute into a held, attended meeting that
// shares content, in a conference room of the corporate lab, with no wait for
// real 08:00 to 18:00 UTC. Then every reader of that room, each a real device
// on a real daemon read over the console's fire API, reads the room in the
// meeting: the video bar in a call, the people counter at the attendees, the
// display awake, the switcher on the conferencing input, and the meeting
// platform's room in a meeting. The offset moves what the simulated world
// reads; this test, the outpost's cadence and every timestamp the lab stamps run
// on real time.
func TestIntegrationCorporateRoomIsMidMeetingOnAClockOffset(t *testing.T) {
	ctx := context.Background()
	const systems = 20

	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := scenario.Resolve(m, cat, systems)
	if err != nil {
		t.Fatal(err)
	}
	w, err := scenario.SiteWorld(m, cat, plan, "hq")
	if err != nil {
		t.Fatal(err)
	}
	cal := w.Calendar()
	if cal == nil {
		t.Fatal("the corporate world holds no calendar")
	}

	// The conference rooms of hq with every device this test reads, by the
	// world's own naming (<system>-<slot>-<n>), and the meeting platform.
	slots := []string{"video-bar", "people-counter", "main-display", "video-switcher"}
	type target struct {
		system string
		room   calendar.Room
		dev    map[string]string
	}
	var targets []target
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			if r.Standard != "divisible-conference" {
				continue
			}
			found := map[string]string{}
			for _, d := range r.Devices {
				for _, slot := range slots {
					if strings.HasPrefix(d.Name, r.System+"-"+slot+"-") {
						found[slot] = d.Name
					}
				}
			}
			if len(found) == len(slots) {
				targets = append(targets, target{r.System, calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}, found})
			}
		}
	}
	if len(targets) == 0 {
		t.Fatalf("the world has no conference room with %v", slots)
	}
	// The huddle rooms of hq with a video bar: their occupancy is the bar's
	// camera count alone, there being no separate counter (#2757).
	type huddle struct {
		system string
		room   calendar.Room
	}
	var huddles []huddle
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			if r.Standard != "huddle-room" {
				continue
			}
			for _, d := range r.Devices {
				if strings.HasPrefix(d.Name, r.System+"-conf-bar-") {
					huddles = append(huddles, huddle{r.System, calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}})
					break
				}
			}
		}
	}
	if len(huddles) == 0 {
		t.Fatal("the world has no huddle room with a video bar")
	}
	var ucc string
	for _, svc := range w.Services {
		if svc.Webhook && svc.Profile == "ucc-platform" {
			ucc = svc.Name
		}
	}
	if ucc == "" {
		t.Fatal("the hq world has no meeting platform")
	}

	// A held, attended, booked meeting of the calendar's own, long enough to
	// share content and to hold the lab's bring-up (the corporate conference
	// slots run an hour at most, so its first third, some eighteen minutes, is
	// the conferencing input and the lab starts half a minute into it), with no
	// scripted problem about it, a day or more ahead of real time so that
	// real time is never in a meeting by luck.
	const into = 30 * time.Second
	var tgt target
	var meeting calendar.Entry
	// Beside it, a huddle room whose own held meeting runs through the first
	// half an hour after the lab starts, so its bar counts people while
	// the lab comes up and is read.
	var hud huddle
	var hudMeeting calendar.Entry
	from := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	for _, c := range targets {
		for _, e := range cal.Entries(c.room, from, from.Add(10*24*time.Hour), calendar.MeetingKinds) {
			mt := caltest.M(e)
			if mt.Origin != calendar.OriginBooked || mt.Outcome != calendar.OutcomeHeld || mt.Actual < 2 || e.End.Sub(e.Start) < 55*time.Minute {
				continue
			}
			if len(cal.Entries(c.room, e.Start.Add(-time.Hour), e.End.Add(time.Hour), calendar.ProblemKinds)) > 0 {
				continue
			}
			if tgt.system != "" && !e.Start.Before(meeting.Start) {
				continue
			}
			at := e.Start.Add(into)
			for _, h := range huddles {
				for _, he := range cal.Entries(h.room, at.Add(-time.Hour), at.Add(time.Hour), calendar.MeetingKinds) {
					hm := caltest.M(he)
					if hm.Outcome != calendar.OutcomeHeld || hm.Actual < 2 || he.Start.After(at) || he.End.Before(at.Add(30*time.Minute)) {
						continue
					}
					if len(cal.Entries(h.room, he.Start.Add(-time.Hour), he.End.Add(time.Hour), calendar.ProblemKinds)) > 0 {
						continue
					}
					tgt, meeting, hud, hudMeeting = c, e, h, he
				}
			}
		}
	}
	if tgt.system == "" {
		t.Fatal("the plan's calendar holds no long, held, attended meeting in a conference room, with a huddle room's meeting beside it, in the next ten days")
	}
	offset := time.Until(meeting.Start.Add(into)).Round(time.Second)
	t.Logf("%s: the offset %v puts the simulator at %s, %v into %s's meeting of %d (%s to %s)", tgt.system, offset,
		time.Now().Add(offset).UTC().Format(time.RFC3339), into, caltest.M(meeting).Organizer, caltest.M(meeting).Actual,
		meeting.Start.UTC().Format(time.RFC3339), meeting.End.UTC().Format(time.RFC3339))

	base, client := startCorporateLabWith(t, "clockoffset", systems, "--sim-clock-offset", offset.String())

	simAt := func(real time.Time) time.Time { return real.Add(offset) }
	heldAt := func(at time.Time) (calendar.Entry, bool) {
		e, ok := cal.Now(tgt.room, at, calendar.MeetingKinds)
		if !ok || caltest.M(e).Outcome == calendar.OutcomeNoShow {
			return calendar.Entry{}, false
		}
		return e, true
	}
	sharingAt := func(e calendar.Entry, at time.Time) bool {
		dur := e.End.Sub(e.Start)
		in := at.Sub(e.Start) * 3
		return dur >= 30*time.Minute && in >= dur && in < 2*dur
	}

	// A reading may be from a tick before its own start, so it must match the
	// calendar at some simulated instant of [start-70s, end]; and every one
	// of these readers reads the room in the meeting, so the instant it
	// matches must be inside it.
	agrees := func(name string, read func() (string, error), expect func(sim time.Time) []string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Minute)
		for {
			lo := time.Now().Add(-70 * time.Second)
			got, err := read()
			hi := time.Now()
			if err == nil {
				for at := lo; !at.After(hi); at = at.Add(5 * time.Second) {
					if _, in := heldAt(simAt(at)); !in {
						continue
					}
					for _, want := range expect(simAt(at)) {
						if want == got {
							t.Logf("%s reads %q at simulated %s, in the meeting", name, got, simAt(at).UTC().Format("15:04:05Z"))
							return
						}
					}
				}
				err = fmt.Errorf("read %q; the meeting's room expects %v at simulated %s", got, expect(simAt(hi)), simAt(hi).UTC().Format("15:04:05Z"))
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s after eight minutes: %v", name, err)
			}
			time.Sleep(10 * time.Second)
		}
	}
	httpJSON := func(slot, path string, into any) error {
		body, err := fireOn(ctx, client, base, tgt.system, tgt.dev[slot], map[string]string{"surface": "http-api", "method": "GET", "path": path})
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(body), into); err != nil {
			return fmt.Errorf("%s answered %q: %v", path, body, err)
		}
		return nil
	}

	agrees("video bar in a call", func() (string, error) {
		var s struct {
			Call struct {
				Active bool `json:"active"`
			} `json:"call"`
		}
		if err := httpJSON("video-bar", "/api/status", &s); err != nil {
			return "", err
		}
		return fmt.Sprint(s.Call.Active), nil
	}, func(time.Time) []string { return []string{"true"} })

	agrees("people counter at the attendees", func() (string, error) {
		out, err := censusFireCommand(ctx, client, base, tgt.dev["people-counter"], "control", "COUNT?")
		if err != nil {
			return "", err
		}
		n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(out), "#COUNT="))
		if err != nil {
			return "", fmt.Errorf("COUNT? answered %q", out)
		}
		// The counter reads who is in the room, one fewer for a moment as
		// somebody settles, up to the most the meeting brought.
		if n < 1 || n > caltest.M(meeting).Actual {
			return fmt.Sprintf("%d (outside 1 to %d)", n, caltest.M(meeting).Actual), nil
		}
		return "attended", nil
	}, func(time.Time) []string { return []string{"attended"} })

	// The room's own occupancy-count follows its people counter, which follows
	// the calendar: the counter's raw-TCP feed lands people-count, the plan's
	// room-occupancy rule carries it up to the system (#2757).
	agrees("room system's occupancy-count at the attendees", func() (string, error) {
		var series struct {
			Samples []struct {
				TS    time.Time `json:"ts"`
				Value float64   `json:"value"`
			} `json:"samples"`
		}
		if err := censusGetJSON(ctx, client, base, "/systems/"+url.PathEscape(tgt.system)+"/metrics/occupancy-count/samples?hours=1", &series); err != nil {
			return "", err
		}
		if len(series.Samples) == 0 {
			return "", fmt.Errorf("%s has no occupancy-count sample yet", tgt.system)
		}
		newest := series.Samples[0]
		for _, sm := range series.Samples {
			if sm.TS.After(newest.TS) {
				newest = sm
			}
		}
		if n := int(newest.Value); n < 1 || n > caltest.M(meeting).Actual {
			return fmt.Sprintf("%d (outside 1 to %d)", n, caltest.M(meeting).Actual), nil
		}
		return "attended", nil
	}, func(time.Time) []string { return []string{"attended"} })

	// A huddle room has a video bar and no separate counter: the bar's
	// camera counts the room, and the same room-occupancy rule carries it up
	// to the huddle room's occupancy-count (#2757), the meeting's attendees.
	agrees("huddle room's occupancy-count at its meeting's attendees", func() (string, error) {
		var series struct {
			Samples []struct {
				TS    time.Time `json:"ts"`
				Value float64   `json:"value"`
			} `json:"samples"`
		}
		if err := censusGetJSON(ctx, client, base, "/systems/"+url.PathEscape(hud.system)+"/metrics/occupancy-count/samples?hours=1", &series); err != nil {
			return "", err
		}
		if len(series.Samples) == 0 {
			return "", fmt.Errorf("%s has no occupancy-count sample yet", hud.system)
		}
		newest := series.Samples[0]
		for _, sm := range series.Samples {
			if sm.TS.After(newest.TS) {
				newest = sm
			}
		}
		if n := int(newest.Value); n < 1 || n > caltest.M(hudMeeting).Actual {
			return fmt.Sprintf("%d (outside 1 to %d)", n, caltest.M(hudMeeting).Actual), nil
		}
		return "attended", nil
	}, func(time.Time) []string { return []string{"attended"} })

	// The room's own room-temperature follows its control processor, which
	// reads the room phase: the processor's NVP reading lands
	// room-sensor-temperature on its component, the plan's room-temperature
	// rule carries it up to the system (#2757). A room in a meeting reads
	// warm from the setpoint, never outside a sane band.
	agrees("room system's room-temperature in a sane band", func() (string, error) {
		var series struct {
			Samples []struct {
				TS    time.Time `json:"ts"`
				Value float64   `json:"value"`
			} `json:"samples"`
		}
		if err := censusGetJSON(ctx, client, base, "/systems/"+url.PathEscape(tgt.system)+"/metrics/room-temperature/samples?hours=1", &series); err != nil {
			return "", err
		}
		if len(series.Samples) == 0 {
			return "", fmt.Errorf("%s has no room-temperature sample yet", tgt.system)
		}
		newest := series.Samples[0]
		for _, sm := range series.Samples {
			if sm.TS.After(newest.TS) {
				newest = sm
			}
		}
		if newest.Value < 20 || newest.Value > 27 {
			return fmt.Sprintf("%.1f (outside 20 to 27)", newest.Value), nil
		}
		return "in band", nil
	}, func(time.Time) []string { return []string{"in band"} })

	agrees("display awake", func() (string, error) {
		return fireOn(ctx, client, base, tgt.system, tgt.dev["main-display"], map[string]string{"surface": "snmp", "oid": "1.3.6.1.4.1.55520.1.1.1.0"})
	}, func(time.Time) []string { return []string{"on"} })

	// The conferencing input is hdmi2 until content is shared, and hdmi1
	// while it is; never dp1, the input nothing is plugged into.
	conferencing := false
	agrees("switcher on the conferencing input", func() (string, error) {
		var s struct {
			Switcher struct {
				Input string `json:"input"`
			} `json:"switcher"`
		}
		if err := httpJSON("video-switcher", "/api/status", &s); err != nil {
			return "", err
		}
		return s.Switcher.Input, nil
	}, func(at time.Time) []string {
		e, _ := heldAt(at)
		if sharingAt(e, at) {
			return []string{"hdmi1"}
		}
		conferencing = true
		return []string{"hdmi2"}
	})
	if !conferencing {
		t.Log("the lab took so long to come up that the room was already sharing; the switcher read the share input")
	}

	// The meeting platform lists the room in a meeting, by the system it is
	// bound to, with who is in it.
	var uccID string
	deadline := time.Now().Add(5 * time.Minute)
	for uccID == "" {
		var comp struct {
			ID string `json:"id"`
		}
		resp, err := client.Get(base + "/api/v1/components/" + url.PathEscape(ucc))
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				_ = json.NewDecoder(resp.Body).Decode(&comp)
			}
			resp.Body.Close()
		}
		if uccID = comp.ID; uccID == "" {
			if time.Now().After(deadline) {
				t.Fatalf("no meeting platform component %q (%v)", ucc, err)
			}
			time.Sleep(5 * time.Second)
		}
	}
	agrees("meeting platform's room in a meeting", func() (string, error) {
		body, err := fireOnComponent(ctx, client, base, uccID, labcheck.Fire{Surface: "http-api", Method: "GET", Path: "/api/rooms?page_size=300"})
		if err != nil {
			return "", err
		}
		var doc struct {
			Rooms []struct {
				ID      string `json:"id"`
				Status  string `json:"status"`
				Meeting struct {
					Participants int `json:"participants"`
				} `json:"meeting"`
			} `json:"rooms"`
		}
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			return "", fmt.Errorf("GET /api/rooms answered %q: %v", body, err)
		}
		for _, r := range doc.Rooms {
			if r.ID != tgt.system {
				continue
			}
			if r.Status == "in-meeting" && (r.Meeting.Participants < 1 || r.Meeting.Participants > caltest.M(meeting).Actual) {
				return fmt.Sprintf("in-meeting with %d (outside 1 to %d)", r.Meeting.Participants, caltest.M(meeting).Actual), nil
			}
			return r.Status, nil
		}
		return "", fmt.Errorf("the platform lists no room %s", tgt.system)
	}, func(time.Time) []string { return []string{"in-meeting"} })
}
