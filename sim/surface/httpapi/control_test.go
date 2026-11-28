package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// fake is the smallest device a control route runs against: a flat state, the
// writes it was given, and defaults to reset to.
type fake struct {
	state    map[string]any
	defaults map[string]any
	writes   [][]surface.Change
	now      time.Time
}

func (f *fake) DeviceID() string            { return "d" }
func (f *fake) Hostname() string            { return "d" }
func (f *fake) IP() string                  { return "127.0.0.1" }
func (f *fake) Model() spec.Model           { return spec.Model{} }
func (f *fake) Read(p string) any           { return f.state[p] }
func (f *fake) Default(p string) any        { return f.defaults[p] }
func (f *fake) State() map[string]any       { return f.state }
func (f *fake) Now() time.Time              { return f.now }
func (f *fake) Publish(spec.Event)          {}
func (f *fake) Write(p string, v any) error { return f.WriteMany(surface.Change{Path: p, Value: v}) }
func (f *fake) WriteMany(cs ...surface.Change) error {
	f.writes = append(f.writes, cs)
	for _, c := range cs {
		f.state[c.Path] = c.Value
	}
	return nil
}

func newFake() *fake {
	return &fake{state: map[string]any{"p": "on", "n": int64(80), "m": false}, defaults: map[string]any{"p": "on", "n": int64(80)}}
}

func TestSpacedMatchesTheTemplatesAcknowledgmentLayout(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{struct {
			OK    bool   `json:"ok"`
			Power string `json:"power"`
		}{true, "standby"}, "{\"ok\": true, \"power\": \"standby\"}\n"},
		{struct {
			OK bool   `json:"ok"`
			V  string `json:"v"`
		}{true, `a, b: "c" & <d>\`}, "{\"ok\": true, \"v\": \"a, b: \\\"c\\\" & <d>\\\\\"}\n"},
		{map[string]float64{"a": 72.5}, "{\"a\": 72.5}\n"},
	} {
		if got := string(Spaced(tc.in)); got != tc.want {
			t.Errorf("Spaced = %q, want %q", got, tc.want)
		}
	}
}

func TestDocumentIsTwoSpaceIndentedWithMarkupAsWritten(t *testing.T) {
	type in struct {
		A string  `json:"a"`
		B float64 `json:"b"`
	}
	got := string(Document(struct {
		In in `json:"in"`
	}{in{"x&y", 1.5}}))
	if want := "{\n  \"in\": {\n    \"a\": \"x&y\",\n    \"b\": 1.5\n  }\n}\n"; got != want {
		t.Fatalf("Document = %q, want %q", got, want)
	}
}

type ack struct {
	OK    bool   `json:"ok"`
	Power string `json:"power"`
}

func powerControl() Control {
	return Control{
		Match: "POST /power", Summary: "Set power", Example: `{"power": "on"}`,
		Fields: []Field{{Path: "p", Name: "power", Kind: Choice, Values: []string{"on", "off", "standby"}}},
		Ack:    func(d spec.Device) any { return ack{true, spec.String(d, "p")} },
	}
}

func TestControlWritesAChoiceAndAnswersWhatItLeft(t *testing.T) {
	d := newFake()
	res := powerControl().Route().Handle(d, spec.HTTPRequest{Body: map[string]any{"power": "standby"}})
	if res.Status != 0 || res.ContentType != "application/json" || string(res.Body.([]byte)) != "{\"ok\": true, \"power\": \"standby\"}\n" {
		t.Fatalf("response = %+v %s", res, res.Body)
	}
	if len(d.writes) != 1 || d.state["p"] != "standby" {
		t.Fatalf("writes = %+v, state %v: want one write", d.writes, d.state)
	}
}

func TestControlRefusesAValueOutsideItsListWithTheEnginesBody(t *testing.T) {
	for body, value := range map[string]any{"bogus": "bogus", "none": nil, "bool": true, "number": float64(7)} {
		_ = body
		d := newFake()
		req := spec.HTTPRequest{Body: map[string]any{"power": value}}
		if value == nil {
			req.Body = nil
		}
		res := powerControl().Route().Handle(d, req)
		got := string(res.Body.([]byte))
		if res.Status != 400 || !strings.HasPrefix(got, `{"error":"validation failed","fields":[{"path":"p","value":"`) || !strings.HasSuffix(got, `"enum":["on","off","standby"]}]}`) {
			t.Errorf("%s: response = %d %s", body, res.Status, got)
		}
		if len(d.writes) != 0 {
			t.Errorf("%s: a refused request wrote %+v", body, d.writes)
		}
	}
	d := newFake()
	res := powerControl().Route().Handle(d, spec.HTTPRequest{})
	if got := string(res.Body.([]byte)); !strings.Contains(got, `"value":"\u003cno value\u003e"`) {
		t.Errorf("a missing field is reported as %s, want <no value> escaped as the engine does", got)
	}
}

func TestControlChecksEveryFieldBeforeWritingAny(t *testing.T) {
	c := Control{Match: "POST /x", Fields: []Field{
		{Path: "p", Name: "power", Kind: Choice, Values: []string{"on"}},
		{Path: "n", Name: "level", Kind: Number},
	}, Ack: func(spec.Device) any { return ack{} }}
	d := newFake()
	if res := c.Route().Handle(d, spec.HTTPRequest{Body: map[string]any{"power": "on", "level": "loud"}}); res.Status != 400 || len(d.writes) != 0 {
		t.Fatalf("a bad second field: %d, writes %+v", res.Status, d.writes)
	}
	if res := c.Route().Handle(d, spec.HTTPRequest{Body: map[string]any{"power": "on", "level": float64(55)}}); res.Status != 0 || len(d.writes) != 1 || len(d.writes[0]) != 2 {
		t.Fatalf("two good fields: %d, writes %+v, want one write of two paths", res.Status, d.writes)
	}
}

func TestControlReadsNumbersAndFlagsAsTheTemplatesCoercedThem(t *testing.T) {
	c := Control{Match: "POST /x", Fields: []Field{
		{Path: "n", Name: "n", Kind: Number}, {Path: "m", Name: "m", Kind: Flag},
	}, Ack: func(spec.Device) any { return ack{} }}
	for _, tc := range []struct {
		n, m  any
		wantN any
		wantM bool
		ok    bool
	}{
		{float64(55), true, int64(55), true, true},
		{72.5, false, 72.5, false, true},
		{"55", "true", int64(55), true, true},
		{float64(0), "false", int64(0), false, true},
		{"abc", true, nil, false, false},
		{float64(1), float64(7), nil, false, false},
		{nil, true, nil, false, false},
	} {
		d := newFake()
		res := c.Route().Handle(d, spec.HTTPRequest{Body: map[string]any{"n": tc.n, "m": tc.m}})
		if (res.Status == 0) != tc.ok {
			t.Errorf("n=%v m=%v: status %d, want ok=%v", tc.n, tc.m, res.Status, tc.ok)
			continue
		}
		if tc.ok && (!reflect.DeepEqual(d.state["n"], tc.wantN) || d.state["m"] != tc.wantM) {
			t.Errorf("n=%v m=%v: wrote %v and %v, want %v and %v", tc.n, tc.m, d.state["n"], d.state["m"], tc.wantN, tc.wantM)
		}
		if !tc.ok && len(d.writes) != 0 {
			t.Errorf("n=%v m=%v: a refused request wrote", tc.n, tc.m)
		}
	}
}

func TestControlListsItsChoicesAsQuickSets(t *testing.T) {
	r := Control{Match: "POST /x", Fields: []Field{
		{Path: "p", Name: "power", Kind: Choice, Values: []string{"on", "off"}}, {Path: "n", Name: "n", Kind: Number},
	}, Ack: func(spec.Device) any { return ack{} }}.Route()
	if len(r.Sets) != 1 || r.Sets[0].Path != "p" || r.Sets[0].Field != "power" || !reflect.DeepEqual(r.Sets[0].Values, []string{"on", "off"}) {
		t.Fatalf("sets = %+v, want only the choice", r.Sets)
	}
}

func TestActionWritesTheDeviceClockAndItsDefaultsInOneChange(t *testing.T) {
	d := newFake()
	d.now = time.Unix(1000, 0)
	d.state["n"], d.state["p"] = int64(5), "off"
	r := Action{Match: "POST /reboot", Writes: func(d spec.Device) []surface.Change {
		return append([]surface.Change{{Path: "boot", Value: d.Now().Unix()}}, Reset(d, "n")...)
	}, Ack: map[string]any{"action": "reboot"}}.Route()
	res := r.Handle(d, spec.HTTPRequest{})
	if string(res.Body.([]byte)) != "{\"action\": \"reboot\"}\n" || len(d.writes) != 1 || d.state["boot"] != int64(1000) || d.state["n"] != int64(80) || d.state["p"] != "off" {
		t.Fatalf("res %s, writes %+v, state %v", res.Body, d.writes, d.state)
	}
}

// A body the device could not read (not JSON, or JSON that is not an object)
// is refused with a 400 that says so, and nothing is written: it is not the
// same as an empty body, whose missing field is refused by name (#2845).
func TestControlRefusesAnUnreadableBodyAndWritesNothing(t *testing.T) {
	for name, raw := range map[string]string{"text": "power=on", "array": `["on"]`, "null": "null", "truncated": `{"power":`} {
		d := newFake()
		res := powerControl().Route().Handle(d, spec.HTTPRequest{Raw: []byte(raw)})
		got, _ := res.Body.([]byte)
		if res.Status != 400 || string(got) != `{"error":"the request body is not a JSON object"}` {
			t.Errorf("%s: response = %d %s", name, res.Status, got)
		}
		if len(d.writes) != 0 {
			t.Errorf("%s: an unreadable body wrote %+v", name, d.writes)
		}
	}
	// An empty body is still a missing field, refused by name as before.
	d := newFake()
	if res := powerControl().Route().Handle(d, spec.HTTPRequest{Raw: []byte("  ")}); res.Status != 400 || !strings.Contains(string(res.Body.([]byte)), "validation failed") {
		t.Errorf("a blank body: %d %s", res.Status, res.Body)
	}
}

// A text field takes a string and only a string: a number, a boolean, null or a
// missing field is refused 400 and nothing is written.
func TestATextFieldTakesOnlyAString(t *testing.T) {
	c := Control{
		Match:  "POST /api/webhook",
		Fields: []Field{{Path: "u", Name: "url", Kind: Text}},
		Ack:    func(d spec.Device) any { return map[string]any{"ok": true} },
	}
	for _, tc := range []struct {
		name, body string
		want       int
		wrote      bool
	}{
		{"a string", `{"url": "https://collector.example/x"}`, 200, true},
		{"an empty string", `{"url": ""}`, 200, true},
		{"a number", `{"url": 5}`, 400, false},
		{"a boolean", `{"url": true}`, 400, false},
		{"null", `{"url": null}`, 400, false},
		{"missing", `{}`, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			var body map[string]any
			_ = json.Unmarshal([]byte(tc.body), &body)
			res := c.Route().Handle(f, spec.HTTPRequest{Body: body, Raw: []byte(tc.body)})
			status := res.Status
			if status == 0 {
				status = 200
			}
			if status != tc.want || (len(f.writes) > 0) != tc.wrote {
				t.Errorf("status %d with %d writes, want %d (written: %v)", status, len(f.writes), tc.want, tc.wrote)
			}
		})
	}
}

// A control writes the changes it makes beside its fields in the same change,
// read from the values its fields were checked to hold, and a path it names
// again takes the later value (#2834).
func TestControlWritesItsAlsoChangesWithTheFieldsInOneChange(t *testing.T) {
	c := Control{Match: "POST /x", Fields: []Field{{Path: "p", Name: "power", Kind: Choice, Values: []string{"on", "off"}}},
		Also: func(_ spec.Device, w map[string]any) []surface.Change {
			return []surface.Change{{Path: "n", Value: int64(len(w["p"].(string)))}, {Path: "p", Value: "typed-" + w["p"].(string)}}
		},
		Ack: func(spec.Device) any { return ack{} }}
	d := newFake()
	if res := c.Route().Handle(d, spec.HTTPRequest{Body: map[string]any{"power": "off"}}); res.Status != 0 || len(d.writes) != 1 || len(d.writes[0]) != 3 {
		t.Fatalf("status %d, writes %+v: want one write of three", res.Status, d.writes)
	}
	if d.state["n"] != int64(3) || d.state["p"] != "typed-off" {
		t.Fatalf("state %v: the also change did not land", d.state)
	}
	d = newFake()
	if res := c.Route().Handle(d, spec.HTTPRequest{Body: map[string]any{"power": "dim"}}); res.Status != 400 || len(d.writes) != 0 {
		t.Fatalf("a refused field still ran the also change: %d, %+v", res.Status, d.writes)
	}
}
