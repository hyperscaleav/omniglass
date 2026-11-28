//go:build linux

package sim

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// Golden wire transcripts (#2832, generalized by #2845): the bytes a simulated
// product put on the wire, recorded once and kept as testdata. A family that
// replaces a profile is held to the transcript the profile recorded: the same
// requests, in the same order, answered with the same statuses, headers, bytes
// and state, and the same SNMP types and values. A family with no profile
// behind it records its own and holds itself to it.
//
// Nothing here is a family's: each family supplies a wireFamily (its products,
// its script of requests, SNMP reads, drifting behaviors and room day), and the
// recorder and the replay below run it. Recording the next family is a script
// file and one command, documented in testdata/wire/README.md.
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
	// Hand is state set on the device before the request, an operator's hand
	// (a cable pulled, a link down).
	Hand map[string]any `json:"Hand,omitempty"`
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
	// Events are the named events the request published on the device's live
	// feed, each "name data" in the order the feed numbered them.
	Events []string `json:"events,omitempty"`
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
	Product  string                  `json:"product"`
	Source   string                  `json:"source"`
	State    json.RawMessage         `json:"state"`
	HTTP     []wireResult            `json:"http"`
	SNMP     map[string][]wireOID    `json:"snmp,omitempty"`
	Walks    map[string][]wireOID    `json:"walks,omitempty"`
	Ticks    map[string][]any        `json:"ticks,omitempty"`
	RoomDays map[string][]wireChange `json:"room_days,omitempty"`
	// Extra is what a family's own Extra hook recorded: the line-protocol
	// conversations, event streams and webhook posts the generic script has no
	// step for (wire_extra_test.go).
	Extra map[string]json.RawMessage `json:"extra,omitempty"`
	// Lines are the steps of a line protocol's script, Help the commands the
	// device lists (see wire_lines_test.go).
	Lines []wireLineResult `json:"lines,omitempty"`
	Help  []string         `json:"help,omitempty"`
}

// defaultWireDir is where a family's goldens live unless it says otherwise.
const defaultWireDir = "testdata/wire"

// wireFamily is everything the harness needs to record and replay one family's
// transcripts: it is the only thing a family writes.
type wireFamily struct {
	// Name is the family's name, what the recorder is asked for.
	Name string
	// Products are the products it records, one transcript each.
	Products []string
	// Engine is an engine serving the family (a fresh one per call).
	Engine func(t testing.TB) *Engine
	// Dir is the golden directory, defaultWireDir when empty.
	Dir string
	// Source is the transcripts' provenance note.
	Source string
	// Frozen, when set, says why the family must never be re-recorded: its
	// transcripts came from a profile that no longer exists, and recording
	// the family from itself would make the hold on that profile vacuous.
	Frozen string
	// BootEpoch is the state path the device's uptime anchor lives at,
	// "status.device.boot_epoch" when empty.
	BootEpoch string
	// Script is the HTTP requests of the transcript, Quirk steps last.
	Script []wireStep
	// Writes move SNMP-visible state away from rest between the two SNMP
	// reads; OIDs are the scalars read, Walks the subtrees walked. All three
	// are empty for a family with no SNMP face.
	Writes      []wireStep
	OIDs, Walks []string
	// Ticks are the drifting behaviors ticked tickRuns times each.
	Ticks []tickPlan
	// Lines is the script of the family's line protocol, played on the TCP face
	// named LineFace ("nvp" when empty) or direct against its command table.
	Lines    []wireLine
	LineFace string
	// RoomDay walks the device through the parity room day and returns what
	// moved; the harness runs it once with the lab calendar and once without.
	// Nil for a family that does not follow a room.
	RoomDay func(d *Device, at *time.Time) []wireChange
	// Extra records what the steps above cannot: a family with a line protocol,
	// an event stream or webhook posts returns each as a named, JSON-encodable
	// value (see wire_extra_test.go for the helpers). Nil records none.
	Extra func(t *testing.T, fam wireFamily, product string) map[string]any
	// Body maps a recorded response body to what the family answers now, for
	// a difference the family means (a product naming its own model where the
	// profile named one for all). Nil compares the bytes as recorded.
	Body func(t *testing.T, product, golden string) string
	// Diverges maps a recorded transcript to what the family means to say now,
	// for a difference that is not a response body (a log line the profile
	// wrote wrongly, in a room day). Nil compares the transcript as recorded.
	Diverges func(t *testing.T, product string, golden wireTranscript) wireTranscript
}

func (f wireFamily) dir() string {
	if f.Dir != "" {
		return f.Dir
	}
	return defaultWireDir
}

func (f wireFamily) bootEpoch() string {
	if f.BootEpoch != "" {
		return f.BootEpoch
	}
	return "status.device.boot_epoch"
}

// wireFamilies is every family the recorder knows; a family's script file adds
// itself here.
var wireFamilies []wireFamily

func goldenPath(f wireFamily, product string) string {
	return filepath.Join(f.dir(), product+".json")
}

func writeGolden(t *testing.T, f wireFamily, tr wireTranscript) {
	t.Helper()
	if err := os.MkdirAll(f.dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(tr); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goldenPath(f, tr.Product), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readGolden(t *testing.T, f wireFamily, product string) wireTranscript {
	t.Helper()
	b, err := os.ReadFile(goldenPath(f, product))
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
func wireDevice(t *testing.T, fam wireFamily, product string) *Device {
	t.Helper()
	e := fam.Engine(t)
	r, ok := e.cat.ForProduct(product)
	if !ok {
		t.Fatalf("the catalog simulates no %s", product)
	}
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	// A behavior that ticks as the device starts reads the world's clock, which
	// is the transcript's, so what it writes at start repeats on any day.
	e.SetClock(Clock{Offset: wireClock.Sub(time.Now())})
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
	if err := SetPath(s, fam.bootEpoch(), wireBoot); err != nil {
		t.Fatal(err)
	}
	d.SetState(s, d.Version)
	return d
}

// wireRequest runs one step against the device in process, as the console
// fires an operation: past a login face, with an api key's header set.
func wireRequest(t *testing.T, d *Device, st wireStep) wireResult {
	t.Helper()
	if st.Hand != nil {
		handSet(t, d, st.Hand)
	}
	req := httptest.NewRequest(st.Method, st.Path, strings.NewReader(st.Body))
	req.RemoteAddr = "console"
	if st.NoKey {
		req.RemoteAddr = "192.0.2.1:4000"
	} else {
		req = asConsole(req)
		switch d.HTTPAuth.Mode {
		case HTTPAuthHeader:
			req.Header.Set(d.HTTPAuth.HeaderName, d.HTTPAuth.APIKey)
		case HTTPAuthBasic:
			req.SetBasicAuth(d.HTTPAuth.User, d.HTTPAuth.Password)
		}
	}
	if st.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	before := stateJSON(d)
	seen := feedSeq(d)
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
	res.Events = feedSince(d, seen)
	res.StateChange = !bytes.Equal(before, after)
	if st.Snapshot {
		res.State = after
	}
	return res
}

// feedSeq is how many events the device's live feed has numbered, 0 for a
// device with no feed; feedSince is the named events published after it (the
// state changes the feed streams are not listed: the state records them).
func feedSeq(d *Device) uint64 {
	if d.events == nil {
		return 0
	}
	d.events.mu.Lock()
	defer d.events.mu.Unlock()
	return d.events.seq
}

func feedSince(d *Device, seq uint64) []string {
	if d.events == nil {
		return nil
	}
	d.events.mu.Lock()
	defer d.events.mu.Unlock()
	var out []string
	for _, ev := range d.events.ring {
		if ev.id > seq && ev.name != "change" {
			out = append(out, ev.name+" "+ev.data)
		}
	}
	return out
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

// roomDayEngine serves the product in the parity room (calendar's hand-checked
// room day), at the day's start, with the lab calendar or without it.
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

// tickRuns is how many times each drifting behavior is ticked in a
// transcript, and the sequence of the paths it moves is recorded.
const tickRuns = 24

// tickPlan names a behavior to tick and the paths whose values are recorded
// after each tick.
type tickPlan struct {
	name  string
	paths []string
}

// recordTicks ticks each behavior tickRuns times and records the values.
func recordTicks(d *Device, plans []tickPlan) map[string][]any {
	out := map[string][]any{}
	for _, p := range plans {
		var seq []any
		for i := 0; i < tickRuns; i++ {
			seen := feedSeq(d)
			if !d.TickBehavior(p.name) {
				panic("no behavior " + p.name)
			}
			row := map[string]any{}
			for _, path := range p.paths {
				row[path] = d.ReadPath(path)
			}
			if ev := feedSince(d, seen); len(ev) > 0 {
				row["#events"] = ev
			}
			seq = append(seq, row)
		}
		out[p.name] = seq
	}
	return out
}

// roomDayWalk is the pinned room day a room-following device is walked
// through: from..to in steps, an operator's hand in the middle of meetings
// where the parity day puts one.
func roomDayWalk(d *Device, at *time.Time, w *watchChanges, from, to string, step time.Duration, tick func(hms string), operator func(hms string)) {
	for tm := parityAt(from); !tm.After(parityAt(to)); tm = tm.Add(step) {
		*at = tm
		hms := tm.Format("15:04:05")
		operator(hms)
		tick(hms)
		w.sample(hms)
	}
}

// roomDays walks the product through the family's room day, with the lab
// calendar and without it.
func roomDays(t *testing.T, fam wireFamily, product string) map[string][]wireChange {
	out := map[string][]wireChange{}
	for _, noCal := range []bool{false, true} {
		key := "calendar"
		if noCal {
			key = "no-calendar"
		}
		d, at := roomDayEngine(t, fam.Engine(t), product, noCal)
		out[key] = fam.RoomDay(d, at)
	}
	return out
}

// collectWire plays the family's script against one product and returns the
// transcript: the recorder writes it, the replay compares it. Both run this one
// path, so what is recorded and what is replayed cannot differ in how it is
// gathered.
func collectWire(t *testing.T, fam wireFamily, product string) wireTranscript {
	t.Helper()
	tr := wireTranscript{Product: product, Source: fam.Source}
	d := wireDevice(t, fam, product)
	tr.State = stateJSON(d)
	if fam.OIDs != nil {
		tr.SNMP = map[string][]wireOID{"rest": wireSNMPGet(t, d, fam.OIDs)}
		tr.Walks = map[string][]wireOID{}
		for _, w := range fam.Walks {
			tr.Walks[w+"@rest"] = wireSNMPWalk(t, d, w)
		}
		runWireHTTP(t, d, fam.Writes)
		tr.SNMP["written"] = wireSNMPGet(t, d, fam.OIDs)
		for _, w := range fam.Walks {
			tr.Walks[w+"@written"] = wireSNMPWalk(t, d, w)
		}
		if len(tr.Walks) == 0 {
			tr.Walks = nil
		}
		d = wireDevice(t, fam, product)
	}
	tr.HTTP = runWireHTTP(t, d, fam.Script)
	if len(fam.Lines) > 0 {
		ld := wireDevice(t, fam, product)
		tr.Help = ld.CommandList()
		tr.Lines = runWireLines(t, ld, fam.LineFace, fam.Lines)
	}
	if len(fam.Ticks) > 0 {
		tr.Ticks = recordTicks(wireDevice(t, fam, product), fam.Ticks)
	}
	if fam.RoomDay != nil {
		tr.RoomDays = roomDays(t, fam, product)
	}
	if fam.Extra != nil {
		tr.Extra = map[string]json.RawMessage{}
		for k, v := range fam.Extra(t, fam, product) {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("extra %s: %v", k, err)
			}
			tr.Extra[k] = b
		}
	}
	return tr
}

// diffWire is what a replayed transcript differs in from the golden, one line
// each. A step marked Quirk is an accident of the profile's templates the
// family refuses instead: it must answer 400 and change nothing, and its bytes
// are not compared.
func diffWire(t *testing.T, fam wireFamily, product string, got, want wireTranscript) []string {
	t.Helper()
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if g, w := canon(t, got.State), canon(t, want.State); g != w {
		add("initial state:\n got %s\nwant %s", g, w)
	}
	for _, k := range []string{"rest", "written"} {
		if !reflect.DeepEqual(got.SNMP[k], want.SNMP[k]) {
			add("SNMP %s:\n got %+v\nwant %+v", k, got.SNMP[k], want.SNMP[k])
		}
	}
	for k, w := range want.Walks {
		if !reflect.DeepEqual(got.Walks[k], w) {
			add("SNMP walk %s:\n got %+v\nwant %+v", k, got.Walks[k], w)
		}
	}
	if len(got.HTTP) != len(want.HTTP) {
		return append(out, fmt.Sprintf("%d steps replayed, %d recorded", len(got.HTTP), len(want.HTTP)))
	}
	for i, w := range want.HTTP {
		have := got.HTTP[i]
		name := have.Name
		if w.Quirk != "" {
			if have.Status != 400 || have.StateChange {
				add("%s: %s: status %d, state changed %v; want a refusal that changes nothing (the profile: %s)", product, name, have.Status, have.StateChange, w.Quirk)
			}
			continue
		}
		if have.Status != w.Status {
			add("%s: %s: status %d, want %d", product, name, have.Status, w.Status)
		}
		if !reflect.DeepEqual(have.Headers, w.Headers) {
			add("%s: %s: headers %v, want %v", product, name, have.Headers, w.Headers)
		}
		wantBody := w.Response
		if fam.Body != nil {
			wantBody = fam.Body(t, product, wantBody)
		}
		if have.Response != wantBody {
			add("%s: %s: body differs from the recorded one:\n got %q\nwant %q", product, name, firstDiff(have.Response, wantBody), firstDiff(wantBody, have.Response))
		}
		if have.StateChange != w.StateChange {
			add("%s: %s: state changed %v, recorded %v", product, name, have.StateChange, w.StateChange)
		}
		if !reflect.DeepEqual(have.Events, w.Events) {
			add("%s: %s: live feed events %q, recorded %q", product, name, have.Events, w.Events)
		}
		if w.State != nil && canon(t, have.State) != canon(t, w.State) {
			add("%s: %s: state after:\n got %s\nwant %s", product, name, canon(t, have.State), canon(t, w.State))
		}
	}
	out = append(out, diffWireLines(t, product, got.Lines, want.Lines)...)
	if !reflect.DeepEqual(got.Help, want.Help) {
		add("%s: the command list (a shell's help) differs:\n got %v\nwant %v", product, got.Help, want.Help)
	}
	if !sameJSON(t, got.Ticks, want.Ticks) {
		add("%s: drift sequences differ from the recorded seeded run:\n got %v\nwant %v", product, got.Ticks, want.Ticks)
	}
	for k, w := range want.Extra {
		g, ok := got.Extra[k]
		if !ok {
			add("%s: %s was recorded and not replayed", product, k)
		} else if canon(t, g) != canon(t, w) {
			gs, ws := canon(t, g), canon(t, w)
			add("%s: %s differs from the recorded one:\n got %q\nwant %q", product, k, firstDiff(gs, ws), firstDiff(ws, gs))
		}
	}
	gd, wd := canonChanges(t, got.RoomDays), canonChanges(t, want.RoomDays)
	for key := range want.RoomDays {
		if !reflect.DeepEqual(gd[key], wd[key]) {
			add("%s: the %s room day differs from the recorded one:\n got %v\nwant %v", product, key, firstChanges(got.RoomDays[key]), firstChanges(want.RoomDays[key]))
		}
	}
	return out
}

// replayWire holds one product of a family to its golden transcript.
func replayWire(t *testing.T, fam wireFamily, product string) {
	t.Helper()
	want := readGolden(t, fam, product)
	if fam.Diverges != nil {
		want = fam.Diverges(t, product, want)
	}
	for _, d := range diffWire(t, fam, product, collectWire(t, fam, product), want) {
		t.Error(d)
	}
}

// recordable is why a family may not be recorded, nil when it may.
func recordable(fam wireFamily) error {
	if fam.Frozen != "" {
		return fmt.Errorf("family %s is frozen: %s", fam.Name, fam.Frozen)
	}
	return nil
}

// wireRecordFamily names the family TestRecordWire records, e.g.
// go test ./sim -run TestRecordWire -wire.family=<name>.
var wireRecordFamily = flag.String("wire.family", "", "record the golden wire transcripts of the named family (see testdata/wire/README.md)")

// TestRecordWire is the recorder: it plays the named family's script and
// writes a transcript per product. It does nothing unless -wire.family is set,
// and refuses a family whose goldens came from a profile that is gone.
func TestRecordWire(t *testing.T) {
	if *wireRecordFamily == "" {
		t.Skip("set -wire.family=<name> to record a family's transcripts")
	}
	for _, fam := range wireFamilies {
		if fam.Name != *wireRecordFamily {
			continue
		}
		if err := recordable(fam); err != nil {
			t.Fatal(err)
		}
		for _, product := range fam.Products {
			writeGolden(t, fam, collectWire(t, fam, product))
		}
		return
	}
	names := make([]string, len(wireFamilies))
	for i, f := range wireFamilies {
		names[i] = f.Name
	}
	t.Fatalf("no family %q to record; known: %v", *wireRecordFamily, names)
}

func canon(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("not JSON: %v: %s", err, raw)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func canonChanges(t *testing.T, m map[string][]wireChange) map[string][]string {
	out := map[string][]string{}
	for k, v := range m {
		for _, c := range v {
			b, _ := json.Marshal(c)
			out[k] = append(out[k], string(b))
		}
	}
	return out
}

func firstChanges(c []wireChange) []wireChange {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// firstDiff is the text of a from where it first differs from b, to a few
// lines on.
func firstDiff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	start := max(i-40, 0)
	return a[start:min(i+80, len(a))]
}
