//go:build linux

package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The wire replay seam (#2826, trimmed by #2837). A wire transcript is the
// bytes a device put on the wire for a script of HTTP requests: each request's
// status, headers, body, whether it moved the device's state, the named events
// it published, and optionally the state after. replayWire plays a transcript's
// requests against a Go family and reports every difference, one line each.
//
// This was once the proof that each Go family read byte for byte as the YAML
// profile it replaced; the recorded goldens, the recorder and the parity hooks
// went when no YAML remained (ADR-2832 to ADR-2836 record what diverged). What
// stays is the seam a capture from a real device plugs into: write the capture
// as a wireTranscript (JSON, under testdata/wire/<product>.json), give the
// family under test as a wireFamily, and call replayWire. The device is
// deterministic: its clock is pinned to wireClock, its uptime anchor fixed, its
// behaviors held still, and its id and hostname are "wire-<product>".

// wireClock is the instant every replayed device reads.
var wireClock = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// wireBoot is the uptime anchor, an hour before wireClock.
var wireBoot = wireClock.Add(-time.Hour).Unix()

// wireStep is one HTTP request of a transcript.
type wireStep struct {
	Name, Method, Path, Body string
	// NoKey sends the request as a network client with no credentials, the
	// way a refused one arrives; every other request is the console's own.
	NoKey bool
	// Snapshot records (and, on replay, compares) the device's whole state
	// after the step.
	Snapshot bool
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

// wireTranscript is everything recorded for one product.
type wireTranscript struct {
	Product string `json:"product"`
	// Source says where the capture came from (a device, a date).
	Source string `json:"source"`
	// State is the device's state before the first step; empty skips the check.
	State json.RawMessage `json:"state,omitempty"`
	HTTP  []wireResult    `json:"http"`
}

// defaultWireDir is where a family's transcripts live unless it says otherwise.
const defaultWireDir = "testdata/wire"

// wireFamily is the family under replay.
type wireFamily struct {
	// Name is the family's name.
	Name string
	// Engine is an engine serving the family (a fresh one per call).
	Engine func(t testing.TB) *Engine
	// Dir is the transcript directory, defaultWireDir when empty.
	Dir string
	// BootEpoch is the state path the device's uptime anchor lives at,
	// "status.device.boot_epoch" when empty.
	BootEpoch string
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

func transcriptPath(f wireFamily, product string) string {
	return filepath.Join(f.dir(), product+".json")
}

// parseTranscript reads a transcript from its JSON.
func parseTranscript(b []byte) (wireTranscript, error) {
	var tr wireTranscript
	err := json.Unmarshal(b, &tr)
	return tr, err
}

func readTranscript(t *testing.T, f wireFamily, product string) wireTranscript {
	t.Helper()
	b, err := os.ReadFile(transcriptPath(f, product))
	if err != nil {
		t.Fatalf("the wire transcript of %s: %v", product, err)
	}
	tr, err := parseTranscript(b)
	if err != nil {
		t.Fatalf("the wire transcript of %s: %v", product, err)
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

// collectWire plays the transcript's requests against the family's device and
// returns what the family put on the wire.
func collectWire(t *testing.T, fam wireFamily, want wireTranscript) wireTranscript {
	t.Helper()
	d := wireDevice(t, fam, want.Product)
	tr := wireTranscript{Product: want.Product, Source: want.Source, State: stateJSON(d)}
	for _, w := range want.HTTP {
		tr.HTTP = append(tr.HTTP, wireRequest(t, d, w.wireStep))
	}
	return tr
}

// diffWire is what a replayed transcript differs in from the expected one, one
// line each.
func diffWire(t *testing.T, got, want wireTranscript) []string {
	t.Helper()
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	product := want.Product
	if len(want.State) > 0 {
		if g, w := canon(t, got.State), canon(t, want.State); g != w {
			add("initial state:\n got %s\nwant %s", g, w)
		}
	}
	if len(got.HTTP) != len(want.HTTP) {
		return append(out, fmt.Sprintf("%d steps replayed, %d recorded", len(got.HTTP), len(want.HTTP)))
	}
	for i, w := range want.HTTP {
		have := got.HTTP[i]
		name := have.Name
		if have.Status != w.Status {
			add("%s: %s: status %d, want %d", product, name, have.Status, w.Status)
		}
		if !reflect.DeepEqual(have.Headers, w.Headers) {
			add("%s: %s: headers %v, want %v", product, name, have.Headers, w.Headers)
		}
		if have.Response != w.Response {
			add("%s: %s: body differs from the recorded one:\n got %q\nwant %q", product, name, firstDiff(have.Response, w.Response), firstDiff(w.Response, have.Response))
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
	return out
}

// replayTranscript holds one product of a family to a transcript and returns
// the differences.
func replayTranscript(t *testing.T, fam wireFamily, want wireTranscript) []string {
	t.Helper()
	return diffWire(t, collectWire(t, fam, want), want)
}

// replayWire holds one product of a family to its transcript on disk.
func replayWire(t *testing.T, fam wireFamily, product string) {
	t.Helper()
	for _, d := range replayTranscript(t, fam, readTranscript(t, fam, product)) {
		t.Error(d)
	}
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
