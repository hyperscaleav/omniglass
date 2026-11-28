package newtronpanel

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(nil, Spec())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Rows {
		res, ok := cat.ForProduct(r.Product)
		if !ok || res.Definition.ID() != Name {
			t.Fatalf("%s resolves to %+v, want the %s family", r.Product, res.Definition, Name)
		}
		if res.Identity.Model != r.Model || res.Identity.DeviceType != "touchpanel" || res.Identity.Summary != r.Summary() {
			t.Errorf("%s: identity = %+v", r.Product, res.Identity)
		}
		f := spectest.NewFake(Spec(), r.Product)
		page := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
		for _, mark := range []string{fmt.Sprintf("<title>%s, Newtron %s</title>", f.Hostname(), r.Model), fmt.Sprintf(`<div class="model">%s</div>`, r.Model)} {
			if !strings.Contains(page, mark) {
				t.Errorf("%s: the dashboard lacks %q", r.Product, mark)
			}
		}
		for _, o := range surfaceOf(f).OIDs {
			if o.OID == "1.3.6.1.2.1.1.1.0" && o.Get() != r.Model {
				t.Errorf("%s: sysDescr = %v", r.Product, o.Get())
			}
		}
	}
}

func route(t *testing.T, match string) spec.HTTPRoute {
	t.Helper()
	for _, r := range Spec().HTTP {
		if r.Match == match {
			return r
		}
	}
	t.Fatalf("no route %s", match)
	return spec.HTTPRoute{}
}

func post(t *testing.T, f *spectest.Fake, match string, body map[string]any) spec.HTTPResponse {
	t.Helper()
	return route(t, match).Handle(f, spec.HTTPRequest{Body: body})
}

// The scene, the source and the log line are text the panel takes as it is
// sent, and every acknowledgment of the room, source and brightness routes
// answers the room's scene, as the panel always has.
func TestTheRoomRoutesTakeTextAndAnswerTheScene(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-panel-7")
	if res := post(t, f, "POST /api/room/scene", map[string]any{"scene": "movie night"}); string(res.Body.([]byte)) != "{\"ok\": true, \"scene\": \"movie night\"}\n" {
		t.Errorf("scene = %s", res.Body)
	}
	if res := post(t, f, "POST /api/room/source", map[string]any{"source": "projector-cart"}); string(res.Body.([]byte)) != "{\"ok\": true, \"scene\": \"movie night\"}\n" || f.Read(pathSource) != "projector-cart" {
		t.Errorf("source = %s, state %v", res.Body, f.Read(pathSource))
	}
	if res := post(t, f, "POST /api/panel/brightness", map[string]any{"brightness": float64(55)}); string(res.Body.([]byte)) != "{\"ok\": true, \"scene\": \"movie night\"}\n" || f.Read(pathBrightness) != int64(55) {
		t.Errorf("brightness = %s, state %v", res.Body, f.Read(pathBrightness))
	}
	for name, body := range map[string]map[string]any{"scene": {"scene": 7.0}, "source": {"source": nil}} {
		g := spectest.NewFake(Spec(), "newtron-panel-7")
		if res := post(t, g, "POST /api/room/"+name, body); res.Status != 400 || len(g.Writes) != 0 {
			t.Errorf("%s with %v: %d, %d writes; want a refusal that writes nothing", name, body, res.Status, len(g.Writes))
		}
	}
}

// A log line is one line of up to two hundred characters, refused with the
// engine's validation body otherwise.
func TestTheLogTakesOneShortLine(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-panel-7")
	for _, line := range []string{"WARN backlight driver reset", strings.Repeat("y", 200)} {
		if res := post(t, f, "POST /api/diagnostics/log", map[string]any{"line": line}); res.Status != 0 || f.Read(pathLog) != line {
			t.Errorf("%q: %d, state %v", line, res.Status, f.Read(pathLog))
		}
	}
	g := spectest.NewFake(Spec(), "newtron-panel-7")
	for _, line := range []string{"", "a\nb", strings.Repeat("x", 201)} {
		res := post(t, g, "POST /api/diagnostics/log", map[string]any{"line": line})
		if res.Status != 400 || !strings.Contains(string(res.Body.([]byte)), `"pattern":"^[^\\r\\n]{1,200}$"`) || len(g.Writes) != 0 {
			t.Errorf("%q: %d %s", line, res.Status, res.Body)
		}
	}
}

// The status carries the panel and its identity and deliberately not the room:
// the shell's `show room` is the one way in for the scene and the source.
func TestTheStatusDocumentCarriesThePanelAndNotTheRoom(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-panel-7")
	_ = f.Write(pathScene, "vc")
	body := statusRoute(f, spec.HTTPRequest{}).Body.([]byte)
	var doc struct {
		Device struct{ Model, Serial, Firmware string }
		Panel  struct {
			Brightness float64
			Occupancy  bool
			Awake      bool
			Page       string
		}
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Device.Model != "Panel 7" || doc.Panel.Brightness != 85 || doc.Panel.Page != "idle" {
		t.Fatalf("status = %s (%v)", body, err)
	}
	if strings.Contains(string(body), "vc") || strings.Contains(string(body), "scene") {
		t.Errorf("the status names the room: %s", body)
	}
	if !strings.HasPrefix(string(body), "{\n  \"device\": {\n    \"uptime\": ") {
		t.Errorf("the identity block does not lead with the uptime: %s", body)
	}
}

// The room config version is the page's browser-only datapoint: it is on the
// page only as a base64 blob, in no route, OID or shell verb.
func TestTheConfigVersionIsOnlyInTheDashboardAsABlob(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-panel-7")
	const version = "rcfg-2026.05-7f3a"
	page := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
	blob := base64.StdEncoding.EncodeToString([]byte(version))
	if strings.Contains(page, version) || !strings.Contains(page, `data-cfgblob="`+blob+`"`) {
		t.Fatalf("the dashboard does not carry the config version as its blob alone")
	}
	for _, where := range []string{string(statusRoute(f, spec.HTTPRequest{}).Body.([]byte)), showStatus(f), showRoom(f)} {
		if strings.Contains(where, version) || strings.Contains(where, blob) {
			t.Errorf("the config version is also here: %s", where)
		}
	}
	for _, o := range surfaceOf(f).OIDs {
		if fmt.Sprint(o.Get()) == version {
			t.Errorf("OID %s serves the config version", o.OID)
		}
	}
}

// The dashboard is html/template: what a device says of itself is escaped, and
// the script and the markup comment are the page's own, never device data.
func TestDashboardEscapesWhatTheDeviceSays(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-panel-7")
	f.Host = `<b>x</b>&"`
	_ = f.Write(pathScene, `"><script>`)
	page := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
	if strings.Contains(page, "<b>x</b>") || !strings.Contains(page, "&lt;b&gt;x&lt;/b&gt;&amp;") {
		t.Fatal("a hostname with markup reached the page unescaped")
	}
	if !strings.Contains(page, "<!-- WEB-DRIVER-ONLY teaching surface") || !strings.Contains(page, "// WEB-DRIVER-ONLY datapoint: decode the room config version.") {
		t.Fatal("the page lost its comments")
	}
}

// The reboot and the factory reset are the web API's and the shell's alike, so
// the two never drift: a reboot clears the uptime and the panel's own state, a
// factory reset the room's scene and source besides.
func TestRebootAndFactoryResetAreTheSameFromTheShellAndTheWeb(t *testing.T) {
	dirty := func() *spectest.Fake {
		f := spectest.NewFake(Spec(), "newtron-panel-7")
		for path, v := range map[string]any{pathBrightness: 20, pathOccupancy: true, pathScene: "vc", pathSource: "laptop", pathBootEpoch: int64(5)} {
			_ = f.Write(path, v)
		}
		f.Writes = nil
		return f
	}
	cmds := map[string]spec.Command{}
	for _, c := range Spec().Commands {
		cmds[c.Match] = c
	}
	for _, tc := range []struct{ route, verb string }{{"POST /api/admin/reboot", "reboot"}, {"POST /api/admin/factory-reset", "factory-reset"}} {
		web, shell := dirty(), dirty()
		route(t, tc.route).Handle(web, spec.HTTPRequest{})
		if got := cmds[tc.verb].Handle(shell); got != "OK\n" {
			t.Errorf("%s answered %q", tc.verb, got)
		}
		if fmt.Sprint(web.State()) != fmt.Sprint(shell.State()) {
			t.Errorf("%s: the web API left\n %v\nthe shell\n %v", tc.verb, web.State(), shell.State())
		}
		if web.Read(pathBrightness) != 85 || web.Read(pathOccupancy) != false || web.Read(pathBootEpoch) != web.Clock.Unix() {
			t.Errorf("%s: brightness %v occupancy %v epoch %v", tc.verb, web.Read(pathBrightness), web.Read(pathOccupancy), web.Read(pathBootEpoch))
		}
		reset := web.Read(pathScene) == "idle" && web.Read(pathSource) == "none"
		if reset != (tc.verb == "factory-reset") {
			t.Errorf("%s: scene %v source %v", tc.verb, web.Read(pathScene), web.Read(pathSource))
		}
	}
	// The shell's commands are listed under the shell and nowhere else.
	for _, c := range Spec().Commands {
		if c.Face != faceShell {
			t.Errorf("%s is listed under %q", c.Match, c.Face)
		}
	}
}

// One room surface serves five faces: the raw port publishes the input, the
// log and the scene, and the others the input alone.
func TestEachFacePublishesItsOwnSubjects(t *testing.T) {
	sf := surfaceOf(spectest.NewFake(Spec(), ""))
	count := map[string]int{}
	for face, subjects := range sf.Subjects {
		count[face] = len(subjects)
	}
	want := map[string]int{faceNVP: 3, faceShell: 1, faceTLS: 1, faceTelnet: 1, faceUDP: 1}
	if fmt.Sprint(count) != fmt.Sprint(want) {
		t.Fatalf("subjects per face = %v, want %v", count, want)
	}
	if sf.ListedOn != faceNVP {
		t.Errorf("the room's commands are listed under %q", sf.ListedOn)
	}
	var lines []string
	for _, c := range sf.Commanders {
		lines = append(lines, c.Commands()...)
	}
	if len(lines) != 14 {
		t.Errorf("the room answers %d lines, want 14", len(lines))
	}
}

// The panel wakes and changes page with its meeting and keeps its state in a
// room with no calendar.
func TestThePanelFollowsItsMeeting(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-panel-7")
	tick := Spec().Ticks[0]
	step := func(room calendar.RoomState) (bool, string) {
		for _, ch := range tick.Run(f, spec.TickContext{Room: room}) {
			_ = f.Write(ch.Path, ch.Value)
		}
		return spec.Bool(f, pathAwake), spec.String(f, pathPage)
	}
	for _, tc := range []struct {
		name  string
		room  calendar.RoomState
		awake bool
		page  string
	}{
		{"waking", calendar.RoomState{HasCalendar: true, InUse: true}, true, "idle"},
		{"in a call", calendar.RoomState{HasCalendar: true, InUse: true, CallActive: true}, true, "in-call"},
		{"sharing", calendar.RoomState{HasCalendar: true, InUse: true, CallActive: true, Sharing: true}, true, "sharing"},
		{"no calendar", spectest.IdleRoom(), true, "sharing"},
		{"between meetings", calendar.RoomState{HasCalendar: true}, false, "idle"},
	} {
		if awake, page := step(tc.room); awake != tc.awake || page != tc.page {
			t.Errorf("%s: awake %v page %q, want %v and %q", tc.name, awake, page, tc.awake, tc.page)
		}
	}
}
