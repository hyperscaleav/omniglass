//go:build linux

package sim

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Ferrite PDU 4C's faces (#1368): a networked rack power conditioner
// with four switched, metered outlets. It answers over SNMP on the Ferrite arc and over a read-only JSON document,
// each from one state: the mains voltage, the total current, and per outlet
// its name, state and current. It cannot be switched.

const (
	ferriteOIDVoltage = "1.3.6.1.4.1.55560.1.1.0"
	ferriteOIDCurrent = "1.3.6.1.4.1.55560.1.2.0"
)

// ferriteOutletOID is a cell of the outlet table, .2.1.<column>.<outlet>:
// column 1 the name, 2 the state, 3 the current in mA; outlets number from 1.
func ferriteOutletOID(column, n int) string {
	return fmt.Sprintf("1.3.6.1.4.1.55560.2.1.%d.%d", column, n)
}

func startPDU(t *testing.T) *Device {
	t.Helper()
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "pdu-1", Hostname: "pdu-1", LoopbackIP: ip, ProfileName: "ferrite-pdu-4c", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := deviceByProfile(t, e, "ferrite-pdu-4c")
	d.StopBehaviors() // hold the readings still between reads
	return d
}

// pduSNMP gets oids as text; an OID the agent does not serve reads "absent".
func pduSNMP(t *testing.T, d *Device, oids ...string) map[string]string {
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

type pduDocument struct {
	Agent         struct{ Model, Version string }
	GlobalMeasure struct {
		VoltageMv    int
		TotalCurrent int
	}
	Outputs []struct {
		ID      int
		Name    string
		State   int
		Current int
	}
}

// pduRequest is a request to the PDU's JSON face, with its lab login.
func pduRequest(t *testing.T, d *Device, method, path string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://"+faceAddr(t, d, "http-api")+path, strings.NewReader("{}"))
	req.SetBasicAuth("ferrite", "ferrite")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// TestFerritePDUServesItsStateOverSNMPAndHTTP: the SNMP agent and the JSON
// document answer one state, the outlets in the table as the document lists
// them, and the total is what the outlets draw.
func TestFerritePDUServesItsStateOverSNMPAndHTTP(t *testing.T) {
	d := startPDU(t)
	code, body := pduRequest(t, d, http.MethodGet, "/pdu.json")
	if code != http.StatusOK {
		t.Fatalf("GET /pdu.json: %d %s", code, body)
	}
	var doc pduDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if doc.Agent.Model != "PDU 4C" || len(doc.Outputs) != 4 {
		t.Fatalf("document = %+v, want the PDU 4C with four outputs", doc)
	}

	oids := []string{ferriteOIDVoltage, ferriteOIDCurrent}
	for n := 1; n <= 4; n++ {
		oids = append(oids, ferriteOutletOID(1, n), ferriteOutletOID(2, n), ferriteOutletOID(3, n))
	}
	got := pduSNMP(t, d, oids...)

	// Millivolts and milliamps, as an agent reports them; the document carries
	// the same units.
	if want := fmt.Sprint(doc.GlobalMeasure.VoltageMv); got[ferriteOIDVoltage] != want {
		t.Errorf("SNMP voltage = %q mV, document %d mV", got[ferriteOIDVoltage], doc.GlobalMeasure.VoltageMv)
	}
	if got[ferriteOIDCurrent] != fmt.Sprint(doc.GlobalMeasure.TotalCurrent) {
		t.Errorf("SNMP total current = %q mA, document %d", got[ferriteOIDCurrent], doc.GlobalMeasure.TotalCurrent)
	}
	sum, on := 0, 0
	for i, o := range doc.Outputs {
		n := i + 1
		if o.ID != n {
			t.Errorf("output %d is numbered %d", n, o.ID)
		}
		state := map[int]string{0: "off", 1: "on"}[o.State]
		if got[ferriteOutletOID(1, n)] != o.Name || got[ferriteOutletOID(2, n)] != state ||
			got[ferriteOutletOID(3, n)] != fmt.Sprint(o.Current) {
			t.Errorf("outlet %d: SNMP %q/%q/%q, document %q/%s/%d", n,
				got[ferriteOutletOID(1, n)], got[ferriteOutletOID(2, n)], got[ferriteOutletOID(3, n)], o.Name, state, o.Current)
		}
		sum += o.Current
		on += o.State
	}
	if sum != doc.GlobalMeasure.TotalCurrent {
		t.Errorf("the outlets draw %d mA, the total reports %d", sum, doc.GlobalMeasure.TotalCurrent)
	}
	if on == 0 || on == 4 {
		t.Errorf("%d of 4 outlets on at rest: the fleet's PDU should show a mix", on)
	}
}

// TestFerritePDUCannotBeSwitched: the document is read-only (the ticket's
// line: outlet control is not here), and a refused login reads nothing.
func TestFerritePDUCannotBeSwitched(t *testing.T) {
	d := startPDU(t)
	before := pduSNMP(t, d, ferriteOutletOID(2, 4))
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		if code, _ := pduRequest(t, d, method, "/pdu.json"); code < 400 {
			t.Errorf("%s /pdu.json = %d, want a refusal", method, code)
		}
	}
	if after := pduSNMP(t, d, ferriteOutletOID(2, 4)); after[ferriteOutletOID(2, 4)] != before[ferriteOutletOID(2, 4)] {
		t.Errorf("outlet 4 moved from %v to %v", before, after)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+faceAddr(t, d, "http-api")+"/pdu.json", nil)
	req.SetBasicAuth("ferrite", "wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a wrong password read %d, want 401", resp.StatusCode)
	}
}

// TestFerritePDUDrifts: the mains wobbles in its band, an outlet that is on
// draws within its range and one that is off draws nothing, and the total is
// always the sum of the outlets.
func TestFerritePDUDrifts(t *testing.T) {
	d := startPDU(t)
	tick := func(name string) {
		t.Helper()
		d.behaviorMu.Lock()
		var b *behaviorEntry
		for i := range d.behaviors {
			if d.behaviors[i].Name == name {
				b = &d.behaviors[i]
			}
		}
		d.behaviorMu.Unlock()
		if b == nil {
			t.Fatalf("no %s behavior; have %v", name, d.Behaviors())
		}
		d.tickBehavior(b)
	}
	read := func(path string) float64 {
		f, _, err := toFloat(d.ReadPath(path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return f
	}
	for i := 0; i < 30; i++ {
		tick("mains_drift")
		if v := read("status.mains.voltage_mv"); v < 226000 || v > 234000 {
			t.Fatalf("voltage after %d ticks = %v mV, outside 226000 to 234000", i+1, v)
		}
		tick("load_drift")
		total := 0.0
		for n := 0; n < 4; n++ {
			c := read(fmt.Sprintf("status.outlets.%d.current_ma", n))
			if d.ReadPath(fmt.Sprintf("status.outlets.%d.state", n)) == "on" {
				if c < 100 || c > 1200 {
					t.Fatalf("outlet %d draws %v mA after %d ticks, outside 100 to 1200", n+1, c, i+1)
				}
			} else if c != 0 {
				t.Fatalf("outlet %d is off and draws %v mA", n+1, c)
			}
			total += c
		}
		if got := read("status.load.total_current_ma"); got != total {
			t.Fatalf("total after %d ticks = %v mA, the outlets draw %v", i+1, got, total)
		}
	}
}
