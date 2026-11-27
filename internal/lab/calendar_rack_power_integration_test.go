//go:build labintegration

package lab

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// The rack power conditioner follows the lab calendar (#2777): on a real
// corporate lab, a conference room's PDU (a real device on a real daemon)
// draws, outlet by outlet, what its room's day says: video, processing and
// amplifier gear in standby, awake by the displays' rule, and in a call. The
// three metered outlets are read over the console's fire API, off the
// simulator, and compared with the calendar the plan's world implies.
func TestIntegrationCorporateRackPowerFollowsTheCalendar(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	base, client := startCorporateLab(t, "rackpower", systems)

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

	// The PDU in a slot of a conference room, by the world's own naming.
	var system, pdu string
	var room calendar.Room
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			for _, d := range r.Devices {
				if system == "" && r.Standard == "divisible-conference" && strings.HasPrefix(d.Name, r.System+"-power-") {
					system, pdu = r.System, d.Name
					room = calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}
				}
			}
		}
	}
	if system == "" {
		t.Fatal("the world has no power conditioner in a conference room")
	}

	// The PDU ticks every 30 seconds, so a reading may be from a tick before
	// its own start: it must match the calendar at some instant of
	// [start-40s, end].
	expect := func(at time.Time) string {
		call := false
		if e, ok := cal.Now(room, at); ok && e.Meeting.Outcome != calendar.OutcomeNoShow && e.Meeting.Actual > 0 {
			call = true
		}
		v, p, a := 150, 180, 120
		if awakeAt(cal, room, at) {
			v, p, a = 700, 300, 500
		}
		if call {
			v, a = 950, 800
		}
		return fmt.Sprintf("%d,%d,%d", v, p, a)
	}
	read := func() (string, error) {
		var got []string
		for n := 1; n <= 3; n++ {
			v, err := fireOn(ctx, client, base, system, pdu, map[string]string{"surface": "snmp", "oid": fmt.Sprintf("1.3.6.1.4.1.55560.2.1.3.%d", n)})
			if err != nil {
				return "", err
			}
			got = append(got, strings.TrimSpace(v))
		}
		return strings.Join(got, ","), nil
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		lo := time.Now().Add(-40 * time.Second)
		got, err := read()
		hi := time.Now()
		if err == nil {
			for at := lo; !at.After(hi); at = at.Add(5 * time.Second) {
				if expect(at) == got {
					t.Logf("outlets read %q, the calendar agrees at %s", got, at.Format("15:04:05Z"))
					return
				}
			}
			err = fmt.Errorf("read %q, the calendar says %q to %q", got, expect(lo), expect(hi))
		}
		if time.Now().After(deadline) {
			t.Fatalf("after five minutes: %v", err)
		}
		time.Sleep(10 * time.Second)
	}
}
