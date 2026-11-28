package seed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/simtest/shipped"
)

// shippedCatalog is the catalog a shipped driver validates against, read
// from the embedded seed the way the database holds it after a boot: each
// metric, property and event type on its lane, the command and secret types.
type shippedCatalog struct {
	lanes    map[string]string
	enums    map[string][]string
	patterns map[string]string
	commands map[string]bool
	targets  map[string]bool
	secrets  map[string]bool
}

func (c shippedCatalog) LaneOf(name string) (string, bool) { l, ok := c.lanes[name]; return l, ok }
func (c shippedCatalog) EventPayloadSchema(string) []byte  { return nil }
func (c shippedCatalog) CommandTypeExists(n string) bool   { return c.commands[n] }
func (c shippedCatalog) CommandHasTarget(n string) bool    { return c.targets[n] }
func (c shippedCatalog) SecretTypeExists(n string) bool    { return c.secrets[n] }
func (c shippedCatalog) WriterClass(n string) writerclass.Class {
	return writerclass.Of(n, false)
}

func loadShippedCatalog(t *testing.T) shippedCatalog {
	t.Helper()
	c := shippedCatalog{lanes: map[string]string{}, enums: map[string][]string{}, patterns: map[string]string{},
		commands: map[string]bool{}, targets: map[string]bool{}, secrets: map[string]bool{}}
	var props propertyTypesDoc
	var metrics metricTypesDoc
	var events struct {
		EventTypes []struct {
			Name string `yaml:"name"`
		} `yaml:"event_types"`
	}
	var commands struct {
		CommandTypes []struct {
			Name   string `yaml:"name"`
			Prop   string `yaml:"target_property_type"`
			Metric string `yaml:"target_metric_type"`
		} `yaml:"command_types"`
	}
	var secrets secretTypesDoc
	for raw, into := range map[*[]byte]any{&propertyTypesYAML: &props, &metricTypesYAML: &metrics, &eventTypesYAML: &events, &commandTypesYAML: &commands, &secretTypesYAML: &secrets} {
		if err := yaml.Unmarshal(*raw, into); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range props.PropertyTypes {
		c.lanes[p.Name] = driver.LaneProperty
		if e, ok := p.Validation["enum"].([]any); ok {
			for _, v := range e {
				c.enums[p.Name] = append(c.enums[p.Name], fmt.Sprint(v))
			}
		}
		if pat, ok := p.Validation["pattern"].(string); ok {
			c.patterns[p.Name] = pat
		}
	}
	for _, m := range metrics.MetricTypes {
		c.lanes[m.Name] = driver.LaneMetric
	}
	for _, e := range events.EventTypes {
		c.lanes[e.Name] = driver.LaneEvent
	}
	for _, ct := range commands.CommandTypes {
		c.commands[ct.Name] = true
		c.targets[ct.Name] = ct.Prop != "" || ct.Metric != ""
	}
	for _, s := range secrets.SecretTypes {
		c.secrets[s.ID] = true
	}
	return c
}

// bakeOn carries a shipped function whole, each emit on the lane the
// shipped catalog gives its name (a log line on the log lane), as attach
// does.
func bakeOn(cat shippedCatalog, fn string, emits []driver.Emit) *driver.BakedFunction {
	b := &driver.BakedFunction{Driver: "test", Function: fn}
	for _, e := range emits {
		lane := cat.lanes[e.Name]
		if e.Log != nil {
			lane = driver.LaneLog
		}
		b.Emits = append(b.Emits, driver.BakedEmit{Name: e.Name, Lane: lane, Extract: e.Extract, Transform: e.Transform, Message: e.Message, Attributes: e.Attributes, Log: e.Log, TS: e.TS})
	}
	return b
}

// function is a shipped driver's poll or listener by name.
func function(t *testing.T, sp *driver.Spec, name string) (*driver.Request, []driver.Emit) {
	t.Helper()
	for i, p := range sp.Polls {
		if p.Name == name {
			return &sp.Polls[i].Request, p.Emits
		}
	}
	for _, l := range sp.Listeners {
		if l.Name == name {
			return nil, l.Emits
		}
	}
	t.Fatalf("no function %q", name)
	return nil, nil
}

// interpretAll runs a function over a payload and keys what it emitted by
// name, failing on any fault; it also holds each property to the shipped
// catalog's validation, as the write path does.
func interpretAll(t *testing.T, cat shippedCatalog, fn *driver.BakedFunction, p driver.Payload) map[string]driver.Emitted {
	t.Helper()
	got, faults := fn.Interpret(p, time.Now())
	if len(faults) > 0 {
		t.Fatalf("%s faulted: %v", fn.Function, faults)
	}
	out := map[string]driver.Emitted{}
	for _, e := range got {
		if e.Lane == driver.LaneProperty {
			if enum := cat.enums[e.Name]; len(enum) > 0 && !slices.Contains(enum, e.Text) {
				t.Errorf("%s: %s = %q is outside the catalog's %v", fn.Function, e.Name, e.Text, enum)
			}
			if pat := cat.patterns[e.Name]; pat != "" && !regexp.MustCompile(pat).MatchString(e.Text) {
				t.Errorf("%s: %s = %q does not match the catalog's %s", fn.Function, e.Name, e.Text, pat)
			}
		}
		key := e.Name
		if e.Lane == driver.LaneLog {
			key = "log"
		}
		out[key] = e
	}
	return out
}

// vroomInACall is a simulated VRoom mid-call, its stats drifted, as the
// corporate lab serves one.
func vroomInACall(t *testing.T) *sim.Device {
	t.Helper()
	e := shipped.Engine(t)
	if err := e.LoadDevices([]sim.DeviceSnapshot{{DeviceID: "bar", Hostname: "hq-conf-1-videobar-1", LoopbackIP: "127.60.0.9", ProfileName: "kestrel-bar", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	if res := d.Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/call/dial", Body: `{"id":"c9001","participants":3}`}); res.Status != http.StatusOK {
		t.Fatalf("dial = %+v", res)
	}
	d.TickBehavior("media_stats")
	return d
}

func vroomGet(t *testing.T, d *sim.Device, path string) []byte {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.SetBasicAuth("admin", "kestrel")
	d.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
	}
	return rec.Body.Bytes()
}

// The video bar's drivers validate against the shipped catalog (#2754):
// every name they emit is a metric, property or event type the seed ships.
func TestTheVideoBarAndDSPDriversValidateAgainstTheShippedCatalog(t *testing.T) {
	cat := loadShippedCatalog(t)
	for _, id := range []string{"kestrel-api", "kestrel-ws", "polaris-dsp-snmp"} {
		if err := shippedSpec(t, id).Validate(cat); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

// kestrel-api's status poll reads what the bar serves and no driver mapped
// (#2754): its app mode, UCC sign-in, touch pairing, local presentation,
// camera and resolution as properties, and its own load, temperature, fan
// and memory and the call's stats as metrics, each in the canon's unit
// (a ratio for a fraction, the uniform-measurements convention).
func TestKestrelAPIStatusPollReadsTheBarsStateAndCallStats(t *testing.T) {
	cat := loadShippedCatalog(t)
	d := vroomInACall(t)
	req, emits := function(t, shippedSpec(t, "kestrel-api"), "status")
	got := interpretAll(t, cat, bakeOn(cat, "status", emits), driver.HTTPPayload(vroomGet(t, d, req.Path), req))
	text := map[string]string{
		"app-mode": "ucc", "ucc-signin": fmt.Sprint(d.ReadPath("status.app.ucc_signin")),
		"touch-pair": fmt.Sprint(d.ReadPath("status.peripherals.touch_pair")), "content-sharing-state": "idle",
		"camera-state": "on", "video-resolution": "3840x2160",
	}
	for name, want := range text {
		if e, ok := got[name]; !ok || e.Lane != driver.LaneProperty || e.Text != want {
			t.Errorf("%s = %+v, want the property %q", name, got[name], want)
		}
	}
	ratio := func(path string) float64 {
		f, ok := asFloat(d.ReadPath(path))
		if !ok {
			t.Fatalf("%s = %v", path, d.ReadPath(path))
		}
		return f
	}
	metrics := map[string]float64{
		"cpu-utilization":    ratio("status.device.cpu_load") / 100,
		"temperature":        ratio("status.device.temperature"),
		"fan-speed":          ratio("status.device.fan_rpm"),
		"memory-utilization": ratio("status.device.memory_used"),
		"packet-loss-ratio":  ratio("status.media.packet_loss"),
		"jitter":             ratio("status.media.jitter_ms"),
		"bitrate":            ratio("status.media.bitrate_kbps"),
		"frame-rate":         ratio("status.media.frame_rate"),
		"speaker-level":      ratio("status.audio.speaker_level") / 100,
		"audio-level":        ratio("status.audio.mic_level_db"),
	}
	for name, want := range metrics {
		e, ok := got[name]
		if !ok || e.Lane != driver.LaneMetric || fmt.Sprintf("%.4f", e.Value) != fmt.Sprintf("%.4f", want) {
			t.Errorf("%s = %+v, want the metric %v", name, got[name], want)
		}
	}
	for _, name := range []string{"cpu-utilization", "memory-utilization", "packet-loss-ratio", "speaker-level"} {
		if v := got[name].Value; v < 0 || v > 1 {
			t.Errorf("%s = %v, want a ratio (0 to 1)", name, v)
		}
	}
	if got["bitrate"].Value == 0 || got["frame-rate"].Value == 0 {
		t.Errorf("mid-call bitrate %v and frame rate %v, want a call's", got["bitrate"].Value, got["frame-rate"].Value)
	}
}

// kestrel-api's system poll reads the bar's identity (#2754): beside the
// model, serial and firmware it always read, the hardware revision, the
// hostname, the address and the MAC, slow-changing properties on the 5 min
// identity poll that the console shows under "What it is".
func TestKestrelAPISystemPollReadsTheBarsIdentity(t *testing.T) {
	cat := loadShippedCatalog(t)
	d := vroomInACall(t)
	req, emits := function(t, shippedSpec(t, "kestrel-api"), "system")
	got := interpretAll(t, cat, bakeOn(cat, "system", emits), driver.HTTPPayload(vroomGet(t, d, req.Path), req))
	for name, path := range map[string]string{
		"model-number": "configuration.device.model", "serial-number": "configuration.device.serial",
		"firmware-version": "configuration.firmware.version", "hardware-revision": "configuration.device.hardware_revision",
		"hostname": "configuration.network.hostname", "ip-address": "status.network.ipv4.address",
		"mac-address": "status.network.ipv4.mac",
	} {
		want := fmt.Sprint(d.ReadPath(path))
		if e, ok := got[name]; !ok || e.Lane != driver.LaneProperty || e.Text != want || want == "" {
			t.Errorf("%s = %+v, want the property %q", name, got[name], want)
		}
	}
}

// kestrel-api's feed listeners land the bar's own occurrences (#2754): a
// participant joining or leaving is an event naming the call and the count,
// and an `event: log` line is a log line with the codec's severity, facility
// and text.
func TestKestrelAPIListenersLandParticipantsAndLogLines(t *testing.T) {
	cat := loadShippedCatalog(t)
	sp := shippedSpec(t, "kestrel-api")
	for _, tc := range []struct{ fn, event, data string }{
		{"participant-joined", "participant-joined", `{"call_id":"c9001","participant":"guest-4","participants":4}`},
		{"participant-left", "participant-left", `{"call_id":"c9001","participant":"guest-4","participants":3}`},
	} {
		var l driver.Listener
		for _, x := range sp.Listeners {
			if x.Name == tc.fn {
				l = x
			}
		}
		if l.Match.Event != tc.event {
			t.Fatalf("%s matches %+v, want the feed's %s event", tc.fn, l.Match, tc.event)
		}
		got := interpretAll(t, cat, bakeOn(cat, tc.fn, l.Emits), driver.Payload{Text: tc.data, JSON: []byte(tc.data), Format: driver.FormatJSON})
		e, ok := got[tc.event]
		if !ok || e.Lane != driver.LaneEvent || !strings.Contains(string(e.Attributes), `"call_id":"c9001"`) || !strings.Contains(string(e.Attributes), `"participants":`) {
			t.Errorf("%s = %+v, want the event naming the call and its count", tc.fn, got)
		}
	}
	var logs driver.Listener
	for _, x := range sp.Listeners {
		if x.Match.Event == "log" {
			logs = x
		}
	}
	data := `{"severity":"WARN","facility":"kvroom.net","text":"Uplink degraded: packet loss 4.1%, jitter 38ms, enabling rate adaptation"}`
	got := interpretAll(t, cat, bakeOn(cat, logs.Name, logs.Emits), driver.Payload{Text: data, JSON: []byte(data), Format: driver.FormatJSON})
	line, ok := got["log"]
	if !ok || line.Severity != "warning" || line.Facility != "kvroom.net" || !strings.HasPrefix(line.Text, "Uplink degraded") {
		t.Fatalf("log listener landed %+v, want a warning on kvroom.net carrying the text", got)
	}
}

// polaris-dsp-snmp reads what the DSP's agent already served and no driver
// collected (#2754): its DSP load (a ratio), temperature, fan and active
// channel count on the status poll, and its identity on the identity poll.
func TestPolarisDSPSNMPReadsTheDSPsLoadAndIdentity(t *testing.T) {
	cat := loadShippedCatalog(t)
	e := shipped.Engine(t)
	if err := e.LoadDevices([]sim.DeviceSnapshot{{DeviceID: "dsp", Hostname: "hq-conf-1-dsp-1", LoopbackIP: "127.60.0.10", ProfileName: "polaris-dsp-16", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	// The agent's answer: each OID the poll gets, as the agent encodes its
	// state path (a number as an integer, a string as its text).
	answer := func(oids []string) driver.Payload {
		vals := map[string]string{}
		for _, oid := range oids {
			path, ok := d.OIDMap[oid]
			if !ok {
				t.Fatalf("the DSP's agent serves no %s", oid)
			}
			v := d.ReadPath(path)
			if f, ok := asFloat(v); ok {
				vals[oid] = fmt.Sprint(int64(f))
			} else {
				vals[oid] = fmt.Sprint(v)
			}
		}
		return driver.Payload{Values: vals}
	}
	sp := shippedSpec(t, "polaris-dsp-snmp")
	req, emits := function(t, sp, "status")
	got := interpretAll(t, cat, bakeOn(cat, "status", emits), answer(req.Get))
	load, _ := asFloat(d.ReadPath("status.dsp.cpu_load"))
	for name, want := range map[string]float64{
		"cpu-utilization": float64(int64(load)) / 100, "temperature": num(t, d, "status.dsp.temperature"),
		"fan-speed": num(t, d, "status.dsp.fan_rpm"), "active-channel-count": num(t, d, "status.dsp.active_channels"),
	} {
		if e, ok := got[name]; !ok || e.Lane != driver.LaneMetric || fmt.Sprintf("%.4f", e.Value) != fmt.Sprintf("%.4f", want) {
			t.Errorf("%s = %+v, want the metric %v", name, got[name], want)
		}
	}
	req, emits = function(t, sp, "identity")
	got = interpretAll(t, cat, bakeOn(cat, "identity", emits), answer(req.Get))
	for name, path := range map[string]string{
		"hardware-revision": "configuration.device.hardware_revision", "hostname": "configuration.network.hostname",
		"ip-address": "status.network.ipv4.address", "mac-address": "status.network.ipv4.mac",
	} {
		want := fmt.Sprint(d.ReadPath(path))
		if e, ok := got[name]; !ok || e.Lane != driver.LaneProperty || e.Text != want || want == "" {
			t.Errorf("%s = %+v, want the property %q", name, got[name], want)
		}
	}
}

func num(t *testing.T, d *sim.Device, path string) float64 {
	t.Helper()
	f, ok := asFloat(d.ReadPath(path))
	if !ok {
		t.Fatalf("%s = %v, not a number", path, d.ReadPath(path))
	}
	return float64(int64(f))
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
