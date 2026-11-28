package httpapi

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// A Text field is the typed form of a route's un-enumerated mutation (#2833):
// whatever string the body carries is written as sent, a pattern refuses what
// does not match with the engine's validation body, and anything that is not a
// string is refused rather than written as the word the template made of it.

func textControl(pattern string) Control {
	return Control{
		Match:  "POST /say",
		Fields: []Field{{Path: "p", Name: "say", Kind: Text, Pattern: pattern}},
		Ack:    func(d spec.Device) any { return ack{true, spec.String(d, "p")} },
	}
}

func TestControlWritesATextAsSentWhateverItSays(t *testing.T) {
	for _, say := range []string{"presentation", "a word nobody listed", "movie night", "  padded  ", ""} {
		d := newFake()
		res := textControl("").Route().Handle(d, spec.HTTPRequest{Body: map[string]any{"say": say}})
		if res.Status != 0 || d.state["p"] != say {
			t.Errorf("%q: status %d, wrote %v", say, res.Status, d.state["p"])
		}
	}
}

func TestControlRefusesAnythingButAStringForAText(t *testing.T) {
	for name, body := range map[string]map[string]any{"missing": {}, "nil body": nil, "number": {"say": float64(7)}, "bool": {"say": true}, "list": {"say": []any{"a"}}} {
		d := newFake()
		res := textControl("").Route().Handle(d, spec.HTTPRequest{Body: body})
		got := string(res.Body.([]byte))
		if res.Status != 400 || got != `{"error":"say must be text"}` || len(d.writes) != 0 {
			t.Errorf("%s: %d %s, writes %+v", name, res.Status, got, d.writes)
		}
	}
}

func TestControlRefusesATextOutsideItsPatternWithTheEnginesBody(t *testing.T) {
	const pattern = `^[^\r\n]{1,200}$`
	c := textControl(pattern).Route()
	for name, say := range map[string]string{"a newline": "a\nb", "empty": "", "long": strings.Repeat("x", 201)} {
		d := newFake()
		res := c.Handle(d, spec.HTTPRequest{Body: map[string]any{"say": say}})
		got := string(res.Body.([]byte))
		want := `{"error":"validation failed","fields":[{"path":"p","value":`
		if res.Status != 400 || !strings.HasPrefix(got, want) || !strings.HasSuffix(got, `,"pattern":"^[^\\r\\n]{1,200}$"}]}`) || len(d.writes) != 0 {
			t.Errorf("%s: %d %s, writes %+v", name, res.Status, got, d.writes)
		}
	}
	for _, say := range []string{"x", strings.Repeat("y", 200), "WARN  two  spaces "} {
		d := newFake()
		if res := c.Handle(d, spec.HTTPRequest{Body: map[string]any{"say": say}}); res.Status != 0 || d.state["p"] != say {
			t.Errorf("%q: %d, wrote %v", say, res.Status, d.state["p"])
		}
	}
}

func TestControlTextIsNotAQuickSet(t *testing.T) {
	if r := textControl("").Route(); len(r.Sets) != 0 {
		t.Fatalf("a text is free, so the console offers it no quick sets: %+v", r.Sets)
	}
}
