//go:build linux

package sim

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Newtron control processor and scheduling panel (#896). The Core CP-3
// extends the Panel 7: every NVP verb the panel answers it answers too, and
// it adds the program, the panels connected and its control ports, over NVP
// and the enterprise SNMP the panel withholds. The Slate 5 is its own device:
// booking state and the next meeting over NVP, booked and freed from its
// buttons.

func startNewtron(t *testing.T, name string) *Device {
	t.Helper()
	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: name + "-1", Hostname: name + "-1", LoopbackIP: simtest.ClaimLoopback(t),
		ProfileName: name, Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := deviceByProfile(t, e, name)
	d.StopBehaviors()
	return d
}

func nvpAsk(t *testing.T, d *Device, line string) string {
	t.Helper()
	got, err := collection.NewLineExchanger().Exchange(context.Background(), faceAddr(t, d, "nvp"), line, 2*time.Second)
	if err != nil {
		t.Fatalf("%s: %v", line, err)
	}
	return got
}

func newtronSNMP(t *testing.T, d *Device, oids ...string) map[string]string {
	t.Helper()
	g := &gosnmp.GoSNMP{
		Target: d.LoopbackIP.String(), Port: uint16(facePort(t, d, "snmp")), Community: "public",
		Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1,
	}
	if err := g.Connect(); err != nil {
		t.Fatalf("SNMP connect: %v", err)
	}
	defer g.Conn.Close()
	pkt, err := g.Get(oids)
	if err != nil {
		t.Fatalf("SNMP GET %v: %v", oids, err)
	}
	out := map[string]string{}
	for _, v := range pkt.Variables {
		switch val := v.Value.(type) {
		case []byte:
			out[v.Name[1:]] = string(val)
		case nil:
			out[v.Name[1:]] = "absent"
		default:
			out[v.Name[1:]] = fmt.Sprint(gosnmp.ToBigInt(val))
		}
	}
	return out
}

// TestCoreAnswersThePanelsSurfaceAndItsOwn: the processor answers the
// panel's NVP verbs (GET and SET INPUT) and its own (GET PROGRAM, GET
// PANELS); a program stopped over its HTTP route reads back over NVP and
// SNMP; its SNMP agent answers the enterprise branch the panel's withholds.
func TestCoreAnswersThePanelsSurfaceAndItsOwn(t *testing.T) {
	core := startNewtron(t, "newtron-core-3")
	if got := nvpAsk(t, core, "GET INPUT"); got != "INPUT hdmi1" {
		t.Errorf("the panel's GET INPUT on the processor = %q", got)
	}
	if got := nvpAsk(t, core, "SET INPUT hdmi2"); got != "INPUT hdmi2" {
		t.Errorf("the panel's SET INPUT on the processor = %q", got)
	}
	if got := nvpAsk(t, core, "GET PROGRAM"); got != "PROGRAM running" {
		t.Errorf("GET PROGRAM = %q", got)
	}
	if got := nvpAsk(t, core, "GET PANELS"); got != "PANELS 2" {
		t.Errorf("GET PANELS = %q", got)
	}
	got := newtronSNMP(t, core, "1.3.6.1.4.1.55510.10.1.0", "1.3.6.1.4.1.55510.10.2.0", "1.3.6.1.4.1.55510.10.3.1", "1.3.6.1.2.1.1.1.0")
	if got["1.3.6.1.4.1.55510.10.1.0"] != "running" || got["1.3.6.1.4.1.55510.10.2.0"] != "2" || got["1.3.6.1.4.1.55510.10.3.1"] != "online" {
		t.Errorf("processor SNMP = %v", got)
	}
	if got["1.3.6.1.2.1.1.1.0"] != "Core CP-3" {
		t.Errorf("sysDescr = %q, want its own model", got["1.3.6.1.2.1.1.1.0"])
	}

	if res := core.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/program", Body: `{"state": "stopped"}`}); res.Status != 200 || !strings.Contains(res.Body, `"stopped"`) {
		t.Fatalf("stop the program: %+v", res)
	}
	if got := nvpAsk(t, core, "GET PROGRAM"); got != "PROGRAM stopped" {
		t.Errorf("GET PROGRAM after stopping = %q", got)
	}
	if v := newtronSNMP(t, core, "1.3.6.1.4.1.55510.10.1.0")["1.3.6.1.4.1.55510.10.1.0"]; v != "stopped" {
		t.Errorf("SNMP program after stopping = %q", v)
	}
	if res := core.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/program", Body: `{"state": "paused"}`}); res.Status != 400 {
		t.Errorf("program %q: %+v, want 400", "paused", res)
	}

	// The panel itself still withholds the enterprise branch.
	panel := startNewtron(t, "newtron-panel-7")
	if v := newtronSNMP(t, panel, "1.3.6.1.4.1.55510.10.1.0")["1.3.6.1.4.1.55510.10.1.0"]; v != "absent" {
		t.Errorf("the panel answers the processor's program OID: %q", v)
	}
	if got := nvpAsk(t, panel, "GET PROGRAM"); strings.HasPrefix(got, "PROGRAM") {
		t.Errorf("the panel answers GET PROGRAM: %q", got)
	}
}

// TestCorePushesProgramChanges: SUBSCRIBE PROGRAM on the processor's NVP
// face pushes the program's new state when it changes.
func TestCorePushesProgramChanges(t *testing.T) {
	core := startNewtron(t, "newtron-core-3")
	conn, err := net.DialTimeout("tcp", faceAddr(t, core, "nvp"), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	if _, err := fmt.Fprint(conn, "SUBSCRIBE PROGRAM\r\n"); err != nil {
		t.Fatal(err)
	}
	if line, _ := r.ReadString('\n'); strings.TrimSpace(line) != "OK" {
		t.Fatalf("SUBSCRIBE PROGRAM = %q", line)
	}
	core.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/program", Body: `{"state": "stopped"}`})
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("no program push: %v", err)
		}
		if strings.TrimSpace(line) == "EVT PROGRAM stopped" {
			return
		}
	}
}

// TestSlateReportsAndTakesBookings: the scheduling panel answers its booking
// state and the next meeting; book-now reads booked; an off-enum state is
// refused; the day's cycle turns the space over; and it answers none of the
// touch panel's verbs.
func TestSlateReportsAndTakesBookings(t *testing.T) {
	slate := startNewtron(t, "newtron-slate-5")
	if got := nvpAsk(t, slate, "GET BOOKING"); got != "BOOKING free" {
		t.Errorf("GET BOOKING = %q", got)
	}
	if got := nvpAsk(t, slate, "GET NEXT"); !strings.HasPrefix(got, "NEXT ") || len(got) < 8 {
		t.Errorf("GET NEXT = %q", got)
	}
	if res := slate.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/booking", Body: `{"state": "booked"}`}); res.Status != 200 {
		t.Fatalf("book now: %+v", res)
	}
	if got := nvpAsk(t, slate, "GET BOOKING"); got != "BOOKING booked" {
		t.Errorf("GET BOOKING after booking = %q", got)
	}
	if res := slate.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/booking", Body: `{"state": "maybe"}`}); res.Status != 400 {
		t.Errorf("booking %q: %+v, want 400", "maybe", res)
	}
	slate.behaviorMu.Lock()
	var cycle *behaviorEntry
	for i := range slate.behaviors {
		if slate.behaviors[i].Name == "booking_cycle" {
			cycle = &slate.behaviors[i]
		}
	}
	slate.behaviorMu.Unlock()
	if cycle == nil {
		t.Fatalf("no booking_cycle; have %v", slate.Behaviors())
	}
	slate.tickBehavior(cycle)
	if got := nvpAsk(t, slate, "GET BOOKING"); got != "BOOKING free" {
		t.Errorf("after a half hour = %q, want free", got)
	}
	slate.tickBehavior(cycle)
	if got := nvpAsk(t, slate, "GET BOOKING"); got != "BOOKING booked" {
		t.Errorf("after an hour = %q, want booked", got)
	}
	if got := nvpAsk(t, slate, "GET INPUT"); strings.HasPrefix(got, "INPUT") {
		t.Errorf("the scheduling panel answers the touch panel's GET INPUT: %q", got)
	}
}
