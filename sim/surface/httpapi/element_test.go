package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

func strips() *fake {
	f := newFake()
	f.state["l"] = []any{map[string]any{"mute": false}, map[string]any{"mute": true}}
	return f
}

func muteStrip() Element {
	return Element{
		Match: "POST /api/strips/mute", IndexField: "index", List: "l",
		Fields: []Field{{Path: "mute", Name: "mute", Kind: Flag}},
		Ack:    func(spec.Device) any { return struct{}{} },
	}
}

// body is a request as the engine hands it over: the parsed object, nil when
// the body is empty or no JSON object, and the bytes as sent.
func body(raw string) spec.HTTPRequest {
	var obj map[string]any
	_ = json.Unmarshal([]byte(raw), &obj)
	return spec.HTTPRequest{Body: obj, Raw: []byte(raw)}
}

func TestElementWritesTheNamedElementsFieldAndNothingElse(t *testing.T) {
	for _, tc := range []struct{ name, in, path string }{
		{"a whole index", `{"index": 1, "mute": false}`, "l.1.mute"},
		{"an index as text", `{"index": "0", "mute": true}`, "l.0.mute"},
		{"a whole number written with a point", `{"index": 1.0, "mute": true}`, "l.1.mute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := strips()
			res := muteStrip().handle(f, body(tc.in))
			if res.Status != 0 && res.Status != 200 {
				t.Fatalf("status %d: %v", res.Status, res.Body)
			}
			if len(f.writes) != 1 || len(f.writes[0]) != 1 || f.writes[0][0].Path != tc.path {
				t.Fatalf("writes = %+v, want one write of %s", f.writes, tc.path)
			}
		})
	}
}

func TestElementRefusesAnIndexNoElementHasInTheStatesOwnWords(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"past the end", `{"index": 2, "mute": true}`, `index 2 out of range [0,2)`},
		{"negative", `{"index": -1, "mute": true}`, `index -1 out of range [0,2)`},
		{"a word", `{"index": "all", "mute": true}`, `"all" is not an integer index`},
		{"missing", `{"mute": true}`, `"<no value>" is not an integer index`},
		{"a boolean", `{"index": true, "mute": true}`, `"true" is not an integer index`},
		{"no body", ``, `"<no value>" is not an integer index`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := strips()
			res := muteStrip().handle(f, body(tc.in))
			doc, ok := res.Body.(mutationFailed)
			if res.Status != 400 || !ok || len(doc.Failed) != 1 || !strings.Contains(doc.Failed[0].Error, tc.want) {
				t.Fatalf("answered %d %+v, want a mutation-failed document naming %q", res.Status, res.Body, tc.want)
			}
			if !strings.HasPrefix(doc.Failed[0].Error, `setting "`+doc.Failed[0].Path+`": segment 1 (`) {
				t.Errorf("the error %q does not open as the state's own write error does", doc.Failed[0].Error)
			}
			if len(f.writes) != 0 {
				t.Fatalf("a refused request wrote %+v", f.writes)
			}
		})
	}
}

func TestElementRefusesWhatItCannotReadAndWritesNothing(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"a fractional index", `{"index": 1.5, "mute": true}`},
		{"a fractional text index", `{"index": "0.5", "mute": true}`},
		{"a field that is no flag", `{"index": 0, "mute": "loud"}`},
		{"a field with no value", `{"index": 0}`},
		{"an array body", `[0, true]`},
		{"a text body", `index=0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := strips()
			res := muteStrip().handle(f, body(tc.in))
			if res.Status != 400 || len(f.writes) != 0 {
				t.Fatalf("answered %d, wrote %+v; want a refusal that writes nothing", res.Status, f.writes)
			}
		})
	}
}

func TestSwitchIsABooleanRefusedAsAChoiceIs(t *testing.T) {
	c := Control{
		Match: "POST /m", Fields: []Field{{Path: "m", Name: "mute", Kind: Switch}},
		Ack: func(spec.Device) any { return struct{}{} },
	}
	for in, want := range map[string]bool{`{"mute": true}`: true, `{"mute": "false"}`: false, `{"mute": "true"}`: true} {
		f := newFake()
		if res := c.handle(f, body(in)); res.Status != 0 && res.Status != 200 {
			t.Fatalf("%s: answered %d", in, res.Status)
		}
		if got := f.writes[0][0].Value; got != want {
			t.Errorf("%s wrote %#v, want the boolean %v", in, got, want)
		}
	}
	for _, in := range []string{`{"mute": "loud"}`, `{"mute": 1}`, `{}`, ``} {
		f := newFake()
		res := c.handle(f, body(in))
		doc, _ := res.Body.([]byte)
		if res.Status != 400 || !strings.Contains(string(doc), `"enum":["true","false"]`) || len(f.writes) != 0 {
			t.Errorf("%q: answered %d %s, wrote %+v; want the validation document and no write", in, res.Status, doc, f.writes)
		}
	}
	if sets := c.Route().Sets; len(sets) != 1 || len(sets[0].Values) != 2 {
		t.Errorf("the console's quick sets = %+v, want true and false", sets)
	}
}
