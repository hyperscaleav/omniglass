//go:build labintegration

package lab

import (
	"bytes"
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
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
)

// censusFireCommand runs one command line on a simulated component's surface
// through the simulation:fire API and returns what the device answered.
func censusFireCommand(ctx context.Context, c *http.Client, base, component, surface, command string) (string, error) {
	body, _ := json.Marshal(map[string]string{"surface": surface, "command": command})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/api/v1/components/"+url.PathEscape(component)+"/simulation:fire", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fire %q: %s", command, resp.Status)
	}
	var res struct {
		Body  string `json:"body"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.Error != "" {
		return "", fmt.Errorf("fire %q: %s", command, res.Error)
	}
	return res.Body, nil
}

// censusGetJSON reads one API document.
func censusGetJSON(ctx context.Context, c *http.Client, base, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// The sim is; Omniglass learns (#2766): on a corporate lab the conference
// room's people counter, a real Census C1 on a real daemon, reports the head
// count its room's lab calendar holds, polled over its raw-TCP control band.
// The expectation is the plan's own, the world of the room's site and the
// calendar it implies; the counter reads the same calendar through its own
// clock and reports who is in the room, nothing from Omniglass.
func TestIntegrationCensusOccupancyFollowsItsRoomsCalendar(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	const system = "hq-divisible-conference-1"
	base, client := startCorporateLab(t, "census", systems)

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
	at, ok := w.PlaceSystem(system)
	if !ok {
		t.Fatalf("the plan's world has no system %s", system)
	}
	cal := w.Calendar()
	if cal == nil {
		t.Fatal("the corporate world holds no calendar")
	}
	room := at.CalRoom()
	// The standard's own people-counter slot names the product, not this test.
	var counter scenario.Ref
	for _, st := range m.Standards {
		if st.Name.Name == "divisible-conference" {
			counter = st.Products["people-counter"]
		}
	}
	if counter.Name == "" {
		t.Fatal("the corporate conference standard fills no people-counter slot")
	}

	var members struct {
		Members []struct {
			Component   string `json:"component"`
			ComponentID string `json:"component_id"`
		} `json:"members"`
	}
	if err := censusGetJSON(ctx, client, base, "/systems/"+url.PathEscape(system)+"/members", &members); err != nil {
		t.Fatal(err)
	}
	census := ""
	for _, mem := range members.Members {
		var comp struct {
			Product string `json:"product"`
		}
		if err := censusGetJSON(ctx, client, base, "/components/"+url.PathEscape(mem.ComponentID), &comp); err != nil {
			t.Fatal(err)
		}
		if comp.Product == counter.Name {
			census = mem.Component
		}
	}
	if census == "" {
		t.Fatalf("system %s has no people counter", system)
	}

	// allowed is what a read taken in [lo, hi] may say: the counter's own tick
	// (every 20 seconds) wrote it from the calendar at some instant in the 25
	// seconds before the read.
	allowed := func(lo, hi time.Time) map[int]bool {
		ok := map[int]bool{}
		for τ := lo.Add(-25 * time.Second); !τ.After(hi); τ = τ.Add(time.Second) {
			e, in := cal.Now(room, τ, calendar.MeetingKinds)
			if !in || caltest.M(e).Outcome == calendar.OutcomeNoShow {
				ok[0] = true
				continue
			}
			ok[caltest.M(e).Actual] = true
			if caltest.M(e).Actual > 1 {
				ok[caltest.M(e).Actual-1] = true
			}
		}
		return ok
	}

	deadline := time.Now().Add(8 * time.Minute)
	var reads, occupied int
	var settled bool
	for reads < 6 {
		lo := time.Now()
		out, err := censusFireCommand(ctx, client, base, census, "control", "COUNT?")
		hi := time.Now()
		if err != nil {
			if time.Now().After(deadline) {
				t.Fatalf("after eight minutes %s's people counter has not answered: %v", system, err)
			}
			time.Sleep(10 * time.Second)
			continue
		}
		got, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(out), "#COUNT="))
		if err != nil {
			t.Fatalf("COUNT? answered %q", out)
		}
		if want := allowed(lo, hi); !want[got] {
			if !settled && got == 0 {
				// A counter that has not ticked yet still holds its default
				// zero; give it one tick before holding it to the calendar.
				settled = true
				time.Sleep(25 * time.Second)
				continue
			}
			t.Fatalf("the counter reports %d people at %s; the room %s's calendar allows %v", got, hi.Format(time.RFC3339), room.Key(), want)
		}
		if got > 0 {
			occupied++
		}
		reads++
		time.Sleep(15 * time.Second)
	}
	t.Logf("%s: %s read in step with %s's calendar six times (%d with people in the room)", system, census, room.Key(), occupied)
}
