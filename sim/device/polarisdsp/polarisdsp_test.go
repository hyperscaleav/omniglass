package polarisdsp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func route(t *testing.T, match string) spec.HTTPHandler {
	t.Helper()
	for _, r := range Spec().HTTP {
		if r.Match == match {
			return r.Handle
		}
	}
	t.Fatalf("no route %s", match)
	return nil
}

func body(raw string) spec.HTTPRequest {
	var obj map[string]any
	_ = json.Unmarshal([]byte(raw), &obj)
	return spec.HTTPRequest{Body: obj, Raw: []byte(raw)}
}

func fake() *spectest.Fake { return spectest.NewFake(Spec(), "polaris-dsp-16") }

// The model table is the line: a product the catalog resolves to the family,
// with sixteen strips of which the room's four microphones have a gate.
func TestTheRowIsASixteenStripProcessor(t *testing.T) {
	cat, err := spec.NewCatalog(Spec())
	if err != nil {
		t.Fatal(err)
	}
	res, ok := cat.ForProduct("polaris-dsp-16")
	if !ok || res.Definition.ID() != Name || res.Identity.Model != "DSP 16" || res.Identity.DeviceType != "audio-dsp" {
		t.Fatalf("polaris-dsp-16 resolves to %+v (%v)", res, ok)
	}
	if n := len(Rows[0].Strips); n != 16 {
		t.Fatalf("the row has %d strips, want 16", n)
	}
	if got := gatedStrips(Rows[0]); len(got) != 4 || got[0] != 0 || got[3] != 3 {
		t.Errorf("the gated strips are %v, want the four table and ceiling microphones", got)
	}
	f := fake()
	if f.Read(channelPath(15, "name")) != "Matrix Out D" || f.Read(channelPath(4, "mute")) != true || f.Read(channelPath(0, "gate")) != false || f.Read(channelPath(5, "gate")) != nil {
		t.Errorf("the strips do not read as the row says")
	}
}

// A channel is addressed by index: its mute or gain is written, anything that
// names no strip is refused in the state's own words and writes nothing.
func TestAChannelIsAddressedByIndex(t *testing.T) {
	f := fake()
	mute, gain := route(t, "POST /api/channels/mute"), route(t, "POST /api/channels/gain")
	if res := mute(f, body(`{"index": "4", "mute": false}`)); res.Status != 0 || f.Read(channelPath(4, "mute")) != false {
		t.Fatalf("an unmute of channel 4 answered %d and left %v", res.Status, f.Read(channelPath(4, "mute")))
	}
	if res := gain(f, body(`{"index": 1, "gain": -7.5}`)); res.Status != 0 || f.Read(channelPath(1, "gain")) != -7.5 {
		t.Fatalf("a gain of -7.5 answered %d and left %v", res.Status, f.Read(channelPath(1, "gain")))
	}
	n := len(f.Writes)
	for in, want := range map[string]string{
		`{"index": 16, "mute": true}`:    "index 16 out of range [0,16)",
		`{"index": -1, "mute": true}`:    "index -1 out of range [0,16)",
		`{"index": "all", "mute": true}`: `"all" is not an integer index`,
		`{"mute": true}`:                 `"<no value>" is not an integer index`,
	} {
		res := mute(f, body(in))
		raw, _ := json.Marshal(res.Body)
		var doc struct {
			Failed []struct{ Error string } `json:"failed_mutations"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil || res.Status != 400 || len(doc.Failed) != 1 || !strings.Contains(doc.Failed[0].Error, want) {
			t.Errorf("%s answered %d %s, want a refusal naming %q", in, res.Status, raw, want)
		}
	}
	if res := gain(f, body(`{"index": 0, "gain": "loud"}`)); res.Status != 400 {
		t.Errorf("a gain of a word answered %d, want 400", res.Status)
	}
	if len(f.Writes) != n {
		t.Errorf("a refused request wrote %d times", len(f.Writes)-n)
	}
}

// A reboot restarts the uptime and puts the volatile telemetry back; a factory
// reset puts back more. Both are the one list the CLI verb of the same name
// writes.
func TestRebootAndFactoryResetAreTheSameListOverHTTPAndTheCLI(t *testing.T) {
	for _, tc := range []struct{ match, verb string }{
		{"POST /api/admin/reboot", "system reboot"},
		{"POST /api/admin/factory-reset", "system factory-reset"},
	} {
		over := func(run func(f *spectest.Fake)) map[string]any {
			f := fake()
			for path, v := range map[string]any{pathCPU: 61, pathTemperature: 55, pathActive: 3, pathFan: 4300, pathOutMute: true, pathOutLevel: -3.5} {
				_ = f.Write(path, v)
			}
			f.Writes = nil
			run(f)
			out := map[string]any{}
			for _, w := range f.Writes {
				for _, c := range w {
					out[c.Path] = c.Value
				}
			}
			return out
		}
		http := over(func(f *spectest.Fake) { route(t, tc.match)(f, spec.HTTPRequest{}) })
		cli := over(func(f *spectest.Fake) {
			r, ok := newShell(f).Handle(tc.verb)
			if !ok || r.Text != "+OK\n" {
				t.Fatalf("%s answered %q (%v)", tc.verb, r.Text, ok)
			}
			for _, c := range r.Changes {
				_ = f.Write(c.Path, c.Value)
			}
		})
		if len(http) < 5 || len(http) != len(cli) {
			t.Fatalf("%s writes %v, %s writes %v", tc.match, http, tc.verb, cli)
		}
		for path, v := range http {
			if cli[path] != v {
				t.Errorf("%s sets %s to %v, %s to %v", tc.match, path, v, tc.verb, cli[path])
			}
		}
	}
}

// The shell answers the same lines on every face, in the order help lists them,
// and each line the console lists a face with is one the shell answers.
func TestEveryListedCommandIsOneTheShellAnswers(t *testing.T) {
	sh := newShell(fake())
	lines := map[string]bool{}
	for _, l := range sh.Commands() {
		lines[l] = true
		if _, ok := sh.Handle(l); !ok {
			t.Errorf("the shell lists %q and does not answer it", l)
		}
	}
	for _, face := range Spec().Interfaces {
		for _, c := range face.Commands {
			if !lines[c.Match] {
				t.Errorf("the %s face lists %q, which the shell does not answer", face.Name, c.Match)
			}
		}
	}
	if got := sh.Commands(); got[0] != "show dsp" || len(got) != 4+7+4+5 {
		t.Errorf("the shell lists %d lines starting %q, want the 4 verbs, 7 subscriptions, 4 unsubscribes and 5 calls", len(got), got[0])
	}
}

// A subscription names the subject it watches and the token it pushes under: the
// control plane's change group carries its own.
func TestSubscriptionsNameTheirSubjectAndToken(t *testing.T) {
	sh := newShell(fake())
	r, _ := sh.Handle("subscribe dsp.cpu_load 1000")
	if r.Action != surface.ActionSubscribe || r.Subject != pathCPU || r.IntervalMs != 1000 || r.Text != "+OK" {
		t.Errorf("the interval subscription = %+v", r)
	}
	add := text(call{JSONRPC: "2.0", Method: "ChangeGroup.AddControl", Params: changeGroupAdd{group, []string{component + ".gain"}}, ID: 3})
	r, ok := sh.Handle(add)
	if !ok || r.Action != surface.ActionSubscribe || r.Subject != channelPath(0, "gain") || r.Token != group || r.Text != `{"jsonrpc":"2.0","result":true,"id":3}` {
		t.Errorf("the change group's subscribe = %+v (%v)", r, ok)
	}
	if _, ok := sh.Handle(strings.Replace(add, `"id":3`, `"id":9`, 1)); ok {
		t.Error("a call with another id was answered: a call is matched whole")
	}
	// What a face pushes names the subject and token it was subscribed with.
	var push string
	for _, s := range cliSubjects() {
		if s.Match == "status.dsp.*" {
			push = s.PushLine(pathCPU, "dsp", 22.5)
		}
	}
	if push != `! "publishToken":"dsp" "subject":"status.dsp.cpu_load" "value":22.5` {
		t.Errorf("the CLI pushes %q", push)
	}
	if q := qrcFace().Subjects[0].PushLine("", group, -4.5); q != `{"jsonrpc":"2.0","method":"ChangeGroup.Poll","params":{"Id":"sub1","Changes":[{"Component":"Mixer1","Name":"gain","Value":-4.5}]}}` {
		t.Errorf("the control plane pushes %q", q)
	}
}

// The dashboard is html/template: what a device says of itself is escaped, and
// the script keys its stored token by the device's id.
func TestDashboardEscapesWhatTheDeviceSaysAndKeysItsTokenByTheDevice(t *testing.T) {
	f := fake()
	f.Host = `<b>x</b>&"`
	page := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
	if strings.Contains(page, "<b>x</b>") || !strings.Contains(page, "&lt;b&gt;x&lt;/b&gt;&amp;") {
		t.Fatal("a hostname with markup reached the page unescaped")
	}
	if !strings.Contains(page, "'polaris-token-"+f.DeviceID()+"'") {
		t.Errorf("the page does not key its token by the device id %q", f.DeviceID())
	}
	if !strings.Contains(page, `data-idx="15"`) || strings.Contains(page, `data-idx="16"`) {
		t.Error("the mixer does not list sixteen channels")
	}
	_ = f.Write(channelPath(2, "mute"), true)
	if page := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte)); !strings.Contains(page, "MUTED") {
		t.Error("a muted channel is not shown muted")
	}
}

// The strips follow the room: a gate opens for every two attendees, an operator's
// mute keeps a strip shut, and an empty room shuts all four.
func TestTheGatesOpenForEveryTwoAttendeesAndAMutedStripStaysShut(t *testing.T) {
	f := fake()
	var gates *spec.Tick
	for _, tk := range Spec().Ticks {
		if tk.Name == "room_signal" {
			gates = &tk
		}
	}
	if gates == nil {
		t.Fatal("no room_signal tick")
	}
	step := func(attendees int) {
		ctx := spec.TickContext{Room: calendar.RoomState{HasCalendar: true, Attendees: attendees}, Now: f.Clock, Rand: behave.NewSource("dsp")}
		for _, c := range gates.Run(f, ctx) {
			_ = f.Write(c.Path, c.Value)
		}
	}
	step(5)
	if f.Read(pathActive) != 3 || f.Read(channelPath(3, "gate")) != false || f.Read(channelPath(2, "gate")) != true {
		t.Errorf("five attendees opened %v gates (strip 2 %v, strip 3 %v), want three", f.Read(pathActive), f.Read(channelPath(2, "gate")), f.Read(channelPath(3, "gate")))
	}
	_ = f.Write(channelPath(1, "mute"), true)
	step(5)
	if f.Read(pathActive) != 2 || f.Read(channelPath(1, "gate")) != false {
		t.Errorf("a muted strip stayed open: %v active, strip 1 %v", f.Read(pathActive), f.Read(channelPath(1, "gate")))
	}
	step(0)
	if f.Read(pathActive) != 0 {
		t.Errorf("an empty room left %v gates open", f.Read(pathActive))
	}
}
