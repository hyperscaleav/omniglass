package kestrelvroom

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func fake() *spectest.Fake { return spectest.NewFake(Spec(), "kestrel-vroom") }

func call(t *testing.T, f *spectest.Fake, match string, body map[string]any) spec.HTTPResponse {
	t.Helper()
	for _, r := range Spec().HTTP {
		if r.Match == match {
			return r.Handle(f, spec.HTTPRequest{Body: body})
		}
	}
	t.Fatalf("no route %s", match)
	return spec.HTTPResponse{}
}

func tick(t *testing.T, name string) spec.Tick {
	t.Helper()
	for _, tk := range Spec().Ticks {
		if tk.Name == name {
			return tk
		}
	}
	t.Fatalf("no tick %s", name)
	return spec.Tick{}
}

// run is one turn of a tick as the engine takes it: its changes written, then
// its events reported.
func run(f *spectest.Fake, tk spec.Tick, c spec.TickContext) {
	for _, ch := range tk.Run(f, c) {
		_ = f.Write(ch.Path, ch.Value)
	}
	if tk.Emit != nil {
		for _, e := range tk.Emit(f, c) {
			f.Publish(e)
		}
	}
}

func events(f *spectest.Fake) []string {
	var out []string
	for _, e := range f.Events {
		out = append(out, e.Name+" "+e.Data)
	}
	return out
}

func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(nil, Spec())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Rows {
		res, ok := cat.ForProduct(r.Product)
		if !ok || res.Definition.ID() != Name || res.Identity.Model != r.Model || res.Identity.DeviceType != "video-codec" {
			t.Fatalf("%s resolves to %+v %+v", r.Product, res.Definition, res.Identity)
		}
		var doc systemDoc
		if err := json.Unmarshal(call(t, spectest.NewFake(Spec(), r.Product), "GET /api/system", nil).Body.([]byte), &doc); err != nil || doc.Model != r.Model || doc.Firmware != r.Firmware || doc.Revision != r.Revision {
			t.Errorf("%s: system = %+v (%v)", r.Product, doc, err)
		}
	}
}

// The log route is the private-API teaching point: listed as private, and what
// the page's script fetches.
func TestTheLogRouteIsPrivateAndTheDashboardFetchesIt(t *testing.T) {
	for _, r := range Spec().HTTP {
		if got, want := r.Private, r.Match == "GET /api/logs"; got != want {
			t.Errorf("%s: private %v, want %v", r.Match, got, want)
		}
	}
	if !strings.Contains(dashboardScript, "fetch(apiURL('/api/logs'))") {
		t.Error("the dashboard's script no longer fetches the private log route")
	}
}

// A document keeps the comment lines the template left in it, and the logs
// document is the lines the state holds.
func TestDocumentsKeepTheirCommentLines(t *testing.T) {
	f := fake()
	app := string(call(t, f, "GET /api/app", nil).Body.([]byte))
	if want := "{\n  \n  \"mode\": \"ucc\",\n  \n  \"ucc_signin\": "; !strings.HasPrefix(app, want) {
		t.Errorf("app band = %q", app)
	}
	status := string(call(t, f, "GET /api/status", nil).Body.([]byte))
	if !strings.Contains(status, "\"firmware\": \"2.7.3\",\n    \n    \"cpu_load\": 14,\n") || !strings.Contains(status, "\"media\": {\n    \n    \"packet_loss\": 0,") {
		t.Errorf("status = %q", status)
	}
	var logs struct{ Recent []string }
	if err := json.Unmarshal(call(t, f, "GET /api/logs", nil).Body.([]byte), &logs); err != nil || len(logs.Recent) != len(vroomLogs) {
		t.Errorf("logs = %+v (%v)", logs, err)
	}
}

// A dial is checked against the patterns the profile held it to before anything
// is written, names every field it refuses, and reports the call it placed.
func TestDialIsCheckedThenPlacedAndReported(t *testing.T) {
	f := fake()
	res := call(t, f, "POST /api/call/dial", map[string]any{"id": "bad id", "participants": "x"})
	want := `{"error":"validation failed","fields":[{"path":"status.call.id","value":"bad id","pattern":"^[A-Za-z0-9-]{1,32}$"},{"path":"status.call.participants","value":"x","pattern":"^([1-9]|[1-5][0-9]|6[0-4])$"}]}`
	if res.Status != 400 || string(res.Body.([]byte)) != want || len(f.Writes) != 0 || len(f.Events) != 0 {
		t.Fatalf("a bad dial: %d %s, %d writes, %d events", res.Status, res.Body, len(f.Writes), len(f.Events))
	}
	if res := call(t, f, "POST /api/call/dial", map[string]any{"id": "c9001", "participants": float64(65)}); res.Status != 400 {
		t.Errorf("65 peers: %d", res.Status)
	}
	res = call(t, f, "POST /api/call/dial", map[string]any{"id": "c9001", "participants": "3"})
	if res.Status != 0 || f.Read(pathCallID) != "c9001" || f.Read(pathParticipants) != int64(3) || f.Read(pathActive) != true || f.Read(pathDialed) != true {
		t.Fatalf("a dial: %d, id %v peers %v active %v dialed %v", res.Status, f.Read(pathCallID), f.Read(pathParticipants), f.Read(pathActive), f.Read(pathDialed))
	}
	got := events(f)
	if len(got) != 2 || got[0] != `call-started {"id":"c9001","participants":3}` ||
		got[1] != `log {"severity":"INFO","facility":"kvroom.call","text":"Call started (id=c9001, protocol=sip, peers=3, dialed)"}` {
		t.Errorf("events = %q", got)
	}
	// With no count the call is two.
	g := fake()
	call(t, g, "POST /api/call/dial", map[string]any{"id": "c9002"})
	if g.Read(pathParticipants) != int64(2) {
		t.Errorf("a dial with no count holds %v", g.Read(pathParticipants))
	}
}

func TestHangUpEndsTheCallNowAndMarksIt(t *testing.T) {
	f := fake()
	call(t, f, "POST /api/call/dial", map[string]any{"id": "c9001"})
	f.Events = nil
	call(t, f, "POST /api/call/disconnect", nil)
	for path, want := range map[string]any{pathActive: false, pathParticipants: 0, pathDialed: false, pathHungup: true, pathStartAt: 0, pathEndAt: 0, pathPlanAt: 0, pathDialTicks: 0} {
		if got := f.Read(path); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	if got := events(f); len(got) != 2 || got[0] != `call-ended {"id":"c9001"}` || !strings.Contains(got[1], "reason=local-hangup") {
		t.Errorf("events = %q", got)
	}
}

// The volume sets both the volume and the speaker level; a reboot restores both
// and the mute, and a factory reset the app too.
func TestVolumeMovesTheSpeakerAndResetsRestoreTheDefaults(t *testing.T) {
	f := fake()
	call(t, f, "POST /api/audio/volume", map[string]any{"volume": float64(77)})
	call(t, f, "POST /api/audio/mute", map[string]any{"mute": true})
	call(t, f, "POST /api/video/camera", map[string]any{"camera_on": false})
	if f.Read(pathVolume) != int64(77) || f.Read(pathSpeaker) != int64(77) || f.Read(pathMute) != true || f.Read(pathCamera) != false {
		t.Fatalf("volume %v speaker %v mute %v camera %v", f.Read(pathVolume), f.Read(pathSpeaker), f.Read(pathMute), f.Read(pathCamera))
	}
	call(t, f, "POST /api/admin/reboot", nil)
	if spec.Int(f, pathVolume) != 50 || f.Read(pathMute) != false || f.Read(pathCamera) != false {
		t.Errorf("after a reboot: volume %v mute %v camera %v", f.Read(pathVolume), f.Read(pathMute), f.Read(pathCamera))
	}
	call(t, f, "POST /api/admin/factory-reset", nil)
	if f.Read(pathCamera) != true {
		t.Errorf("after a factory reset the camera is %v", f.Read(pathCamera))
	}
}

// With no calendar the bar keeps its own schedule: it plans a slot, and when a
// dialed call has run its length the call ends, reported as the profile's feed
// did.
func TestADialedCallEndsAfterItsLengthAndSaysSo(t *testing.T) {
	f := fake()
	cycle, rng := tick(t, "call_cycle"), behave.NewSource("bar")
	ctx := spec.TickContext{Room: spectest.IdleRoom(), Now: f.Clock, Rand: rng}
	_ = f.Write(pathDialLength, 3)
	call(t, f, "POST /api/call/dial", map[string]any{"id": "c9001", "participants": float64(2)})
	f.Events = nil
	for i := 0; i < 10 && f.Read(pathActive) == true; i++ {
		run(f, cycle, ctx)
	}
	if f.Read(pathActive) != false || f.Read(pathDialed) != false {
		t.Fatalf("the dialed call did not end: active %v dialed %v", f.Read(pathActive), f.Read(pathDialed))
	}
	var ended []string
	for _, e := range events(f) {
		if strings.HasPrefix(e, "call-ended ") {
			ended = append(ended, e)
		}
	}
	if len(ended) != 1 || !strings.HasPrefix(ended[0], `call-ended {"id":"c9001","participants":`) {
		t.Errorf("call-ended events = %q in %q", ended, events(f))
	}
}

// The uplink's lines are its transitions, written from the loss the tick left:
// degraded once as loss crosses 3%, recovered once it is back under 2%.
func TestTheUplinkReportsItsTransitionsOnce(t *testing.T) {
	f := fake()
	stats := tick(t, "media_stats")
	ctx := spec.TickContext{Room: spectest.IdleRoom(), Now: f.Clock, Rand: behave.NewSource("net")}
	_ = f.Write(pathActive, true)
	_ = f.Write(pathLoss, 0.05)
	var lines []string
	for i := 0; i < 200; i++ {
		f.Events = nil
		run(f, stats, ctx)
		for _, e := range f.Events {
			lines = append(lines, e.Data)
		}
		if i == 100 {
			_ = f.Write(pathActive, false)
		}
	}
	var degraded, recovered int
	for _, l := range lines {
		switch {
		case strings.Contains(l, `"severity":"WARN"`) && strings.Contains(l, "Uplink degraded: packet loss "):
			degraded++
		case strings.Contains(l, "Uplink recovered: loss "):
			recovered++
			if strings.Contains(l, "%!") {
				t.Errorf("a malformed line: %s", l)
			}
		}
	}
	if degraded == 0 || recovered == 0 || degraded > recovered+1 {
		t.Errorf("%d degraded and %d recovered lines: want each transition once", degraded, recovered)
	}
	// Idle, the media rests at zero and the room is quiet.
	if spec.Float(f, pathLoss) != 0 || spec.Float(f, pathJitter) != 0 || spec.Float(f, pathMic) != -60 {
		t.Errorf("idle media: loss %v jitter %v mic %v", spec.Float(f, pathLoss), spec.Float(f, pathJitter), spec.Float(f, pathMic))
	}
}

func TestTheStatusLineNamesTheBarsPowerCallAndLoad(t *testing.T) {
	f := fake()
	run(f, tick(t, "status_log"), spec.TickContext{})
	want := `log {"severity":"INFO","facility":"kvroom.system","text":"Status: power=on, call=idle, cpu=14%, temp=44C, fan=2400rpm"}`
	if got := events(f); len(got) != 1 || got[0] != want {
		t.Errorf("status line = %q", got)
	}
}

// The page is the typed view: a bar in a call shows its hang-up and who is on,
// an idle one only that it is idle, and a stale text never reaches it.
func TestTheDashboardShowsTheCallItIsIn(t *testing.T) {
	f := fake()
	idle, err := renderDashboard(viewOf(f))
	if err != nil || strings.Contains(string(idle), "End call") || !strings.Contains(string(idle), `<span class="pill idle">idle</span>`) {
		t.Fatalf("idle page: %v", err)
	}
	call(t, f, "POST /api/call/dial", map[string]any{"id": "c9001", "participants": float64(4)})
	page, err := renderDashboard(viewOf(f))
	if err != nil || !strings.Contains(string(page), `id="hangup"`) || !strings.Contains(string(page), `<span class="k">participants</span><span class="v">4</span>`) {
		t.Fatalf("in-call page: %v", err)
	}
	if !strings.Contains(string(page), "<title>fake-kestrel-vroom, Kestrel VRoom</title>") || !strings.Contains(string(page), "// Browser carries the Basic auth credentials") {
		t.Error("the page lost its title or its script's comments")
	}
}

func TestLookedReadsTextAsTheTemplateEngineDid(t *testing.T) {
	for in, want := range map[string]any{"c9001": "c9001", "9001": int64(9001), "-5": int64(-5), "3.5": 3.5, "true": true, "": "", " 12 ": int64(12)} {
		if got := looked(in); got != want {
			t.Errorf("looked(%q) = %v (%T), want %v (%T)", in, got, got, want, want)
		}
	}
}
