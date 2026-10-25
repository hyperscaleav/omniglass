package driver_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A server-sent events stream on http (#1038, slice 3 of #1113): the
// session block declares the stream a held session opens, and listeners
// claim its events by name, by a prefix or regex on the data, or by a JSON
// field, extracting through the one primitive.

// streamSpec is the credentialed http driver holding a stream of change
// events, each a JSON document naming the state path it changed.
func streamSpec() *driver.Spec {
	s := httpSpec()
	s.Session.Stream = &driver.Request{Path: "/api/events"}
	s.Listeners = []driver.Listener{{
		Name:  "volume",
		Match: driver.Match{Event: "change", Field: &driver.FieldMatch{Path: "path", Equals: "status.audio.volume"}},
		Emits: []driver.Emit{{Name: "volume", Extract: driver.Extract{JSONPath: "value"}}},
	}}
	return s
}

func TestAStreamDriverValidates(t *testing.T) {
	if err := streamSpec().Validate(httpCatalog()); err != nil {
		t.Fatalf("validate = %v, want clean", err)
	}
	text := streamSpec()
	text.Session.Stream.Format = driver.FormatText
	text.Listeners[0].Match = driver.Match{Regex: `^volume `}
	text.Listeners[0].Emits[0].Extract = driver.Extract{Regex: `^volume (\d+)$`}
	if err := text.Validate(httpCatalog()); err != nil {
		t.Fatalf("a text stream = %v, want clean", err)
	}
	named := streamSpec()
	named.Listeners[0].Match = driver.Match{Event: "volume"}
	if err := named.Validate(httpCatalog()); err != nil {
		t.Fatalf("a listener claiming by event name alone = %v, want clean", err)
	}
	// An uncredentialed stream holds a session too.
	open := streamSpec()
	open.Session.Credential, open.Session.Scheme = "", ""
	open.Inputs = open.Inputs[:2]
	if err := open.Validate(httpCatalog()); err != nil {
		t.Fatalf("an uncredentialed stream = %v, want clean", err)
	}
	// Behind a login, the stream opens once it has logged in.
	login := tokenLoginSpec()
	login.Session.Stream = &driver.Request{Path: "/api/events"}
	login.Listeners = streamSpec().Listeners
	if err := login.Validate(httpCatalog()); err != nil {
		t.Fatalf("a stream behind a login = %v, want clean", err)
	}
}

func TestStreamRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*driver.Spec)
		want   string
	}{
		{"a listener with no stream", func(s *driver.Spec) { s.Session.Stream = nil }, "session.stream"},
		{"a stream with no listener", func(s *driver.Spec) { s.Listeners = nil }, "listener"},
		{"a listener that arms", func(s *driver.Spec) { s.Listeners[0].Arm = []string{"SUBSCRIBE"} }, "arm"},
		{"a stream posted", func(s *driver.Spec) { s.Session.Stream.Method = "POST" }, "GET"},
		{"a stream with a body", func(s *driver.Spec) { s.Session.Stream.Body = []byte(`{"a":1}`) }, "body"},
		{"a stream read as xml", func(s *driver.Spec) { s.Session.Stream.Format = driver.FormatXML }, "json or text"},
		{"a stream with no path", func(s *driver.Spec) { s.Session.Stream.Path = "" }, "path"},
		{"a stream setting Accept", func(s *driver.Spec) { s.Session.Stream.Headers = map[string]string{"accept": "x"} }, "accept"},
		{"a stream sending Last-Event-ID", func(s *driver.Spec) {
			s.Session.Stream.Headers = map[string]string{"Last-Event-ID": "7"}
		}, "Last-Event-ID"},
		{"a field match with no path", func(s *driver.Spec) { s.Listeners[0].Match.Field.Path = "" }, "path"},
		{"a field match both equal and matching", func(s *driver.Spec) { s.Listeners[0].Match.Field.Regex = "x" }, "one of"},
		{"a field match neither", func(s *driver.Spec) { s.Listeners[0].Match.Field.Equals = "" }, "one of"},
		{"an uncompilable field regex", func(s *driver.Spec) {
			s.Listeners[0].Match.Field = &driver.FieldMatch{Path: "path", Regex: "("}
		}, "regex"},
		{"a field match beside a prefix", func(s *driver.Spec) { s.Listeners[0].Match.Prefix = "{" }, "one data rule"},
		{"a field match on a text stream", func(s *driver.Spec) { s.Session.Stream.Format = driver.FormatText }, "json"},
		{"an xpath on a json stream", func(s *driver.Spec) { s.Listeners[0].Emits[0].Extract = driver.Extract{XPath: "//v"} }, "format"},
		{"a listener matching nothing", func(s *driver.Spec) { s.Listeners[0].Match = driver.Match{} }, "match"},
		{"a re-probe beside the stream", func(s *driver.Spec) { s.Session.Reprobe = "1h" }, "reprobe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := streamSpec()
			tc.mutate(s)
			err := s.Validate(httpCatalog())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
	// An event name is a stream's alone: a line has none.
	line := validSpec()
	line.Listeners[0].Match = driver.Match{Event: "change", Prefix: "EVT "}
	if err := line.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "event") {
		t.Fatalf("an event name on a line listener = %v, want a refusal", err)
	}
	// A line listener may claim a JSON line by a field.
	jsonLine := validSpec()
	jsonLine.Listeners[0].Match = driver.Match{Field: &driver.FieldMatch{Path: "kind", Equals: "input"}}
	jsonLine.Listeners[0].Emits[0].Extract = driver.Extract{JSONPath: "value"}
	if err := jsonLine.Validate(catalog()); err != nil {
		t.Fatalf("a field match on a json line = %v, want clean", err)
	}
	// The field match is the router's: a terminal rule, a ready or a settle
	// still declares a prefix or a regex.
	term := streamSpec()
	term.Session.Terminal = []driver.TerminalRule{{Class: driver.ClassAuth, Match: driver.Match{Field: &driver.FieldMatch{Path: "e", Equals: "x"}}}}
	if err := term.Validate(httpCatalog()); err == nil {
		t.Fatal("a terminal rule took a field match")
	}
}

func TestAMatchReadsAMessage(t *testing.T) {
	change := driver.Message{Event: "change", ID: "7", Format: driver.FormatJSON, Data: `{"path":"status.audio.volume","value":42}`}
	cases := []struct {
		name string
		m    driver.Match
		msg  driver.Message
		want bool
	}{
		{"the event and the field", driver.Match{Event: "change", Field: &driver.FieldMatch{Path: "path", Equals: "status.audio.volume"}}, change, true},
		{"another field value", driver.Match{Event: "change", Field: &driver.FieldMatch{Path: "path", Equals: "status.audio.mute"}}, change, false},
		{"another event", driver.Match{Event: "status", Field: &driver.FieldMatch{Path: "path", Equals: "status.audio.volume"}}, change, false},
		{"the event alone", driver.Match{Event: "change"}, change, true},
		{"a field regex", driver.Match{Field: &driver.FieldMatch{Path: "path", Regex: `^status\.audio\.`}}, change, true},
		{"a number field", driver.Match{Field: &driver.FieldMatch{Path: "value", Equals: "42"}}, change, true},
		{"an absent field", driver.Match{Field: &driver.FieldMatch{Path: "nope", Regex: "."}}, change, false},
		{"a field on data that is not json", driver.Match{Field: &driver.FieldMatch{Path: "path", Regex: "."}}, driver.Message{Data: "not json"}, false},
		{"a prefix on the data", driver.Match{Prefix: `{"path"`}, change, true},
		{"an event rule on a line", driver.Match{Event: "message"}, driver.LineMessage("EVT INPUT hdmi1"), false},
		{"a prefix on a line", driver.Match{Prefix: "EVT "}, driver.LineMessage("EVT INPUT hdmi1"), true},
		{"no rule", driver.Match{}, change, false},
	}
	for _, tc := range cases {
		if got := tc.m.MatchesMessage(tc.msg); got != tc.want {
			t.Errorf("%s: matches = %v, want %v", tc.name, got, tc.want)
		}
	}
	// A message's payload reads in its format: a JSON event by jsonpath, a
	// line as text.
	fn := &driver.BakedFunction{Driver: "d", Function: "volume", Emits: []driver.BakedEmit{{Name: "volume", Lane: "metric", Extract: driver.Extract{JSONPath: "value"}}}}
	got, faults := fn.Interpret(change.Payload(), time.Now())
	if len(faults) != 0 || len(got) != 1 || got[0].Value != 42 {
		t.Fatalf("interpret a json event = %+v, %v", got, faults)
	}
	if p := driver.LineMessage("INPUT hdmi1").Payload(); p.Text != "INPUT hdmi1" || p.JSON != nil {
		t.Fatalf("a line's payload = %+v", p)
	}
}
