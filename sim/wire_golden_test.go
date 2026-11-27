//go:build linux

package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// Golden wire transcripts (#2832): the bytes a simulated product put on the
// wire when a YAML profile defined it, recorded once from that profile and
// kept as testdata. A family that replaces a profile is held to them: the same
// requests, in the same order, answered with the same statuses, headers,
// bytes and state, and the same SNMP types and values. This file is the
// recorder's script and the replay both families share; each family's own test
// supplies its product list and script.
//
// A transcript is deterministic: the device's clock is pinned, its uptime
// anchor fixed, its behaviors held still, and its id and hostname are the
// product's, so the serial, MAC and address the engine derives repeat.

// wireClock is the instant every golden device reads: the pinned room day's
// noon, so a transcript recorded today replays on any day.
var wireClock = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// wireBoot is the uptime anchor, an hour before wireClock.
var wireBoot = wireClock.Add(-time.Hour).Unix()

// wireStep is one HTTP request of a script.
type wireStep struct {
	Name, Method, Path, Body string
	// NoKey sends the request as a network client with no credentials, the
	// way a refused one arrives; every other request is the console's own.
	NoKey bool
	// Snapshot records the device's whole state after the step.
	Snapshot bool
	// Quirk marks a request the YAML profile answered with an accident of its
	// templates (an unquoted word in a JSON document, "<no value>" written to
	// state); the family refuses it instead, and the replay checks that
	// divergence rather than the bytes.
	Quirk string
}

// wireResult is what one step put on the wire.
type wireResult struct {
	wireStep
	Status      int                 `json:"status"`
	Headers     map[string][]string `json:"headers,omitempty"`
	Response    string              `json:"response"`
	State       json.RawMessage     `json:"state,omitempty"`
	StateChange bool                `json:"state_change"`
}

// wireOID is one SNMP variable as the agent answered it.
type wireOID struct {
	OID   string `json:"oid"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// wireChange is one watched state path moving, in a behavior transcript.
type wireChange struct {
	At    string `json:"at"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// wireTranscript is everything recorded for one product.
type wireTranscript struct {
	Product  string                `json:"product"`
	Source   string                `json:"source"`
	State    json.RawMessage       `json:"state"`
	HTTP     []wireResult          `json:"http"`
	SNMP     map[string][]wireOID  `json:"snmp,omitempty"`
	Walks    map[string][]wireOID  `json:"walks,omitempty"`
	Ticks    map[string][]any      `json:"ticks,omitempty"`
	RoomDays map[string][]wireChange `json:"room_days,omitempty"`
}

// wireSource says where a transcript was recorded from.
const wireSource = "recorded from the YAML profile before its deletion (#2832); see sim/testdata/wire/README.md"

func goldenPath(product string) string {
	return filepath.Join("testdata", "wire", product+".json")
}

func writeGolden(t *testing.T, tr wireTranscript) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(goldenPath(tr.Product)), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(tr); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goldenPath(tr.Product), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readGolden(t *testing.T, product string) wireTranscript {
	t.Helper()
	b, err := os.ReadFile(goldenPath(product))
	if err != nil {
		t.Fatalf("the golden transcript of %s: %v", product, err)
	}
	var tr wireTranscript
	if err := json.Unmarshal(b, &tr); err != nil {
		t.Fatalf("the golden transcript of %s: %v", product, err)
	}
	return tr
}

// wireDevice serves one product of the engine for a transcript: started, its
// behaviors held still, its clock pinned and its uptime anchor fixed.
func wireDevice(t *testing.T, e *Engine, product string) *Device {
	t.Helper()
	r, ok := e.cat.ForProduct(product)
	if !ok {
		t.Fatalf("the catalog simulates no %s", product)
	}
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	id := "wire-" + product
	snap := DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: "127.0.0.1", ProfileName: r.Definition.ID(), Product: product, Version: 1}
	if err := e.Sync([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, id)
	d.StopBehaviors()
	at := wireClock
	d.SetClock(func() time.Time { return at })
	s := d.State()
	s["status"].(map[string]any)["device"].(map[string]any)["boot_epoch"] = wireBoot
	d.SetState(s, d.Version)
	return d
}

// wireRequest runs one step against the device in process, as the console
// fires an operation: past a login face, with an api key's header set.
func wireRequest(t *testing.T, d *Device, st wireStep) wireResult {
	t.Helper()
	req := httptest.NewRequest(st.Method, st.Path, strings.NewReader(st.Body))
	req.RemoteAddr = "console"
	if st.NoKey {
		req.RemoteAddr = "192.0.2.1:4000"
	} else {
		req = asConsole(req)
		if d.HTTPAuth.Mode == HTTPAuthHeader {
			req.Header.Set(d.HTTPAuth.HeaderName, d.HTTPAuth.APIKey)
		}
	}
	if st.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	before := stateJSON(d)
	w := httptest.NewRecorder()
	d.ServeHTTP(w, req)
	res := wireResult{wireStep: st, Status: w.Code, Response: w.Body.String()}
	if len(w.Header()) > 0 {
		res.Headers = map[string][]string{}
		for k, v := range w.Header() {
			res.Headers[k] = v
		}
	}
	after := stateJSON(d)
	res.StateChange = !bytes.Equal(before, after)
	if st.Snapshot {
		res.State = after
	}
	return res
}

func stateJSON(d *Device) json.RawMessage {
	b, err := json.Marshal(d.State())
	if err != nil {
		panic(err)
	}
	return b
}

// wireSNMPGet reads the OIDs from the device's agent as a collector would.
func wireSNMPGet(t *testing.T, d *Device, oids []string) []wireOID {
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
	return wireVars(pkt.Variables)
}

// wireSNMPWalk walks a subtree of the device's agent.
func wireSNMPWalk(t *testing.T, d *Device, root string) []wireOID {
	t.Helper()
	g := &gosnmp.GoSNMP{
		Target: d.LoopbackIP.String(), Port: uint16(facePort(t, d, "snmp")), Community: "public",
		Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1,
	}
	if err := g.Connect(); err != nil {
		t.Fatalf("SNMP connect: %v", err)
	}
	defer g.Conn.Close()
	vars, err := g.WalkAll(root)
	if err != nil {
		t.Fatalf("SNMP walk %s: %v", root, err)
	}
	return wireVars(vars)
}

func wireVars(vars []gosnmp.SnmpPDU) []wireOID {
	out := make([]wireOID, 0, len(vars))
	for _, v := range vars {
		val := ""
		switch x := v.Value.(type) {
		case []byte:
			val = string(x)
		case nil:
			val = ""
		default:
			val = fmt.Sprint(x)
		}
		out = append(out, wireOID{OID: strings.TrimPrefix(v.Name, "."), Type: fmt.Sprint(v.Type), Value: val})
	}
	return out
}

// runWireHTTP plays a script and returns the results in order.
func runWireHTTP(t *testing.T, d *Device, steps []wireStep) []wireResult {
	t.Helper()
	out := make([]wireResult, 0, len(steps))
	for _, st := range steps {
		out = append(out, wireRequest(t, d, st))
	}
	return out
}

// ---- behavior transcripts ----

// watchChanges records, step by step, every watched path whose value moved.
type watchChanges struct {
	d     *Device
	paths []string
	last  map[string]string
	out   []wireChange
}

func newWatch(d *Device, paths ...string) *watchChanges {
	w := &watchChanges{d: d, paths: paths, last: map[string]string{}}
	return w
}

func (w *watchChanges) sample(at string) {
	for _, p := range w.paths {
		v := w.d.ReadPath(p)
		b, _ := json.Marshal(v)
		if w.last[p] != string(b) {
			w.last[p] = string(b)
			var norm any
			_ = json.Unmarshal(b, &norm)
			w.out = append(w.out, wireChange{At: at, Path: p, Value: norm})
		}
	}
}

// roomDay is the room day the behavior transcripts walk: the parity day
// (calendar's hand-checked room day) at the parity room.
func roomDayEngine(t *testing.T, e *Engine, product string, noCalendar bool) (*Device, *time.Time) {
	t.Helper()
	r, ok := e.cat.ForProduct(product)
	if !ok {
		t.Fatalf("the catalog simulates no %s", product)
	}
	e.SetCalendar(parityCal())
	at := parityAt("07:00:00")
	snap := DeviceSnapshot{DeviceID: "d-1", Hostname: "d-1", LoopbackIP: "127.0.0.1", ProfileName: r.Definition.ID(), Product: product, Version: 1,
		SiteCode: parityRoom.Site, Building: parityRoom.Building, Room: parityRoom.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.SetClock(func() time.Time { return at })
	if noCalendar {
		d.calSrc = func() *calendar.Calendar { return nil }
	}
	return d, &at
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var _ = http.MethodGet
