//go:build linux

package sim

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Newtron control processor and scheduling panel (#896) speak the NVP
// line protocol the touch panel does, each with the surface of the kind of
// device it is: a processor answers everything a touch panel does and its
// program, ports and panels besides; a scheduling panel answers its room's
// booking and nothing of the touch panel's room control.

func startNVP(t *testing.T, name string) (target string, d *Device) {
	t.Helper()
	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: name + "-1", Hostname: name + "-1", LoopbackIP: simtest.ClaimLoopback(t), ProfileName: name, Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d = deviceByProfile(t, e, name)
	d.StopBehaviors()
	return faceAddr(t, d, "nvp"), d
}

func nvpAsk(t *testing.T, target string) func(string) string {
	t.Helper()
	lx := collection.NewLineExchanger()
	return func(line string) string {
		t.Helper()
		got, err := lx.Exchange(context.Background(), target, line, 2*time.Second)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		return got
	}
}

// A processor answers the touch panel's room and source control, and its own
// program, control ports and connected panels.
func TestControlProcessorSpeaksThePanelsSurfaceAndItsOwn(t *testing.T) {
	target, _ := startNVP(t, "newtron-core-3")
	ask := nvpAsk(t, target)

	if got := ask("GET INPUT"); got != "INPUT hdmi1" {
		t.Errorf("GET INPUT = %q, want the panel's default input", got)
	}
	if got := ask("SET INPUT dp1"); got != "INPUT dp1" {
		t.Errorf("SET INPUT dp1 = %q", got)
	}
	if got := ask("GET INPUT"); got != "INPUT dp1" {
		t.Errorf("GET INPUT after the set = %q", got)
	}

	if got := ask("GET PROGRAM"); got != "PROGRAM running" {
		t.Errorf("GET PROGRAM = %q, want a running program at rest", got)
	}
	if got := ask("SET PROGRAM stopped"); got != "PROGRAM stopped" {
		t.Errorf("SET PROGRAM stopped = %q", got)
	}
	if got := ask("GET PROGRAM"); got != "PROGRAM stopped" {
		t.Errorf("GET PROGRAM after the stop = %q", got)
	}
	if got := ask("GET PORTS"); !strings.HasPrefix(got, "PORTS com1=") {
		t.Errorf("GET PORTS = %q, want the control ports with their states", got)
	}
	if got := ask("GET PANELS"); got != "PANELS 3" {
		t.Errorf("GET PANELS = %q, want the panels connected to it", got)
	}
	if got := ask("GET BOOKING"); !strings.HasPrefix(got, "-ERR") {
		t.Errorf("GET BOOKING = %q, want a processor to refuse a scheduling panel's verb", got)
	}
}

// A scheduling panel answers its room's booking and refuses the control
// verbs of a touch panel and a processor.
func TestSchedulingPanelSpeaksItsRoomsBooking(t *testing.T) {
	target, _ := startNVP(t, "newtron-slate-5")
	ask := nvpAsk(t, target)

	if got := ask("GET BOOKING"); got != "BOOKING free" {
		t.Errorf("GET BOOKING = %q, want a free room at rest", got)
	}
	if got := ask("BOOK"); got != "BOOKING booked" {
		t.Errorf("BOOK = %q", got)
	}
	if got := ask("GET BOOKING"); got != "BOOKING booked" {
		t.Errorf("GET BOOKING after the booking = %q", got)
	}
	if got := ask("RELEASE"); got != "BOOKING free" {
		t.Errorf("RELEASE = %q", got)
	}
	for _, verb := range []string{"GET INPUT", "GET PROGRAM"} {
		if got := ask(verb); !strings.HasPrefix(got, "-ERR") {
			t.Errorf("%s = %q, want a scheduling panel to refuse it", verb, got)
		}
	}
}

// A processor pushes the program it runs to a subscriber, and a scheduling
// panel the booking it takes, each after the reply to the verb that caused it.
func TestNVPDevicesPushTheirClassState(t *testing.T) {
	for _, tc := range []struct{ profile, subscribe, change, reply, push string }{
		{"newtron-core-3", "SUBSCRIBE PROGRAM", "SET PROGRAM stopped", "PROGRAM stopped", "EVT PROGRAM stopped"},
		{"newtron-slate-5", "SUBSCRIBE BOOKING", "BOOK", "BOOKING booked", "EVT BOOKING booked"},
	} {
		target, _ := startNVP(t, tc.profile)
		c, err := net.DialTimeout("tcp", target, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		r := bufio.NewReader(c)
		line := func(send string) string {
			t.Helper()
			if send != "" {
				fmt.Fprintf(c, "%s\r\n", send)
			}
			l, err := r.ReadString('\n')
			if err != nil {
				t.Fatalf("%s: read after %q: %v", tc.profile, send, err)
			}
			return strings.TrimRight(l, "\r\n")
		}
		if got := line(tc.subscribe); got != "OK" {
			t.Fatalf("%s: %s = %q", tc.profile, tc.subscribe, got)
		}
		if got := line(tc.change); got != tc.reply {
			t.Fatalf("%s: %s = %q, want the reply first", tc.profile, tc.change, got)
		}
		if got := line(""); got != tc.push {
			t.Errorf("%s: push = %q, want %q", tc.profile, got, tc.push)
		}
	}
}

// What a touch panel and a control processor answer alike is one definition:
// every command and subject of the panel's NVP face is on the processor's,
// served from the shared surface and written in neither profile.
func TestProcessorAnswersEverythingThePanelDoes(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	nvp := func(name string) profile.Interface {
		p, ok := store.Devices[name]
		if !ok {
			t.Fatalf("no %s profile", name)
		}
		for _, i := range p.Interfaces {
			if i.Name == "nvp" {
				return i
			}
		}
		t.Fatalf("%s has no nvp face", name)
		return profile.Interface{}
	}
	panel, core := nvp("newtron-panel-7"), nvp("newtron-core-3")
	if !slices.Equal(panel.Include, core.Include) || len(panel.Include) == 0 {
		t.Fatalf("panel includes %v, processor %v; want the same shared surface", panel.Include, core.Include)
	}
	have := map[string]bool{}
	for _, c := range core.Commands {
		have[c.Match] = true
	}
	for _, c := range panel.Commands {
		if !have[c.Match] {
			t.Errorf("the processor does not answer %q, which the panel does", c.Match)
		}
	}
	haveSubject := map[string]bool{}
	for _, s := range core.Subjects {
		haveSubject[s.Match] = true
	}
	for _, s := range panel.Subjects {
		if !haveSubject[s.Match] {
			t.Errorf("the processor does not publish %q, which the panel does", s.Match)
		}
	}
}
