package driver_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// fakeCatalog resolves datapoint and command names for validation the way the
// storage gateway does against the live catalogs: lanes for datapoint names,
// existence for command types and secret types.
type fakeCatalog struct {
	lanes    map[string]string // datapoint name -> lane ("metric" | "property")
	commands map[string]bool
	secrets  map[string]bool
}

func (f fakeCatalog) DatapointLane(name string) (string, bool) {
	lane, ok := f.lanes[name]
	return lane, ok
}
func (f fakeCatalog) CommandTypeExists(name string) bool { return f.commands[name] }
func (f fakeCatalog) SecretTypeExists(name string) bool  { return f.secrets[name] }

func catalog() fakeCatalog {
	return fakeCatalog{
		lanes: map[string]string{
			"uptime":       "metric",
			"model-number": "property",
			"video-input":  "property",
		},
		commands: map[string]bool{"set-input": true, "reboot": true},
		secrets:  map[string]bool{"snmp-community": true, "basic-auth": true},
	}
}

// validSpec is a full three-family spec that must validate: the baseline every
// mutation case below breaks one field of.
func validSpec() *driver.Spec {
	return &driver.Spec{
		Version:   1,
		Transport: "tcp",
		Inputs: []driver.Input{
			{Name: "host", Kind: "string", Required: true},
			{Name: "port", Kind: "number", Default: "4998"},
			{Name: "login", Kind: "secret", SecretType: "basic-auth", Required: true},
		},
		Polls: []driver.Poll{{
			Name:     "status",
			Schedule: driver.Schedule{Every: "30s"},
			Request:  driver.Request{Line: "GET STATUS"},
			Datapoints: []driver.Datapoint{{
				Name:    "video-input",
				Extract: driver.Extract{Regex: `^INPUT (\S+)`},
			}},
		}},
		Listeners: []driver.Listener{{
			Name:  "events",
			Arm:   []string{"SUBSCRIBE EVENTS"},
			Match: driver.Match{Prefix: "EVT "},
			Datapoints: []driver.Datapoint{{
				Name:    "video-input",
				Extract: driver.Extract{Regex: `^EVT INPUT (\S+)`},
			}},
		}},
		Commands: []driver.CommandBinding{{
			CommandType: "set-input",
			Request:     driver.Request{Line: "SET INPUT ${arg.input}"},
		}},
	}
}

func TestValidateAcceptsAFullSpec(t *testing.T) {
	if err := validSpec().Validate(catalog()); err != nil {
		t.Fatalf("valid spec refused: %v", err)
	}
}

func TestValidateRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*driver.Spec)
		want   string // a fragment the error must carry, naming the fault
	}{
		{"unsupported version", func(s *driver.Spec) { s.Version = 2 }, "version"},
		{"unknown transport", func(s *driver.Spec) { s.Transport = "galaxy" }, "transport"},
		{"empty transport", func(s *driver.Spec) { s.Transport = "" }, "transport"},
		{"no functions at all", func(s *driver.Spec) {
			s.Polls, s.Listeners, s.Commands = nil, nil, nil
		}, "function"},
		{"input without a name", func(s *driver.Spec) { s.Inputs[0].Name = "" }, "input"},
		{"duplicate input names", func(s *driver.Spec) { s.Inputs[1].Name = "host" }, "host"},
		{"unknown input kind", func(s *driver.Spec) { s.Inputs[0].Kind = "blob" }, "kind"},
		{"secret input without its type", func(s *driver.Spec) { s.Inputs[2].SecretType = "" }, "secret_type"},
		{"secret input with an unknown type", func(s *driver.Spec) { s.Inputs[2].SecretType = "warp-key" }, "warp-key"},
		{"secret_type on a non-secret input", func(s *driver.Spec) { s.Inputs[0].SecretType = "basic-auth" }, "secret_type"},
		{"poll without a name", func(s *driver.Spec) { s.Polls[0].Name = "" }, "poll"},
		{"poll with an unparseable schedule", func(s *driver.Spec) { s.Polls[0].Schedule.Every = "whenever" }, "schedule"},
		{"poll with a zero schedule", func(s *driver.Spec) { s.Polls[0].Schedule.Every = "0s" }, "schedule"},
		{"poll with an empty request", func(s *driver.Spec) { s.Polls[0].Request = driver.Request{} }, "request"},
		{"poll with no datapoints", func(s *driver.Spec) { s.Polls[0].Datapoints = nil }, "datapoint"},
		{"datapoint not in any catalog", func(s *driver.Spec) { s.Polls[0].Datapoints[0].Name = "warp-factor" }, "warp-factor"},
		{"datapoint with no extractor", func(s *driver.Spec) { s.Polls[0].Datapoints[0].Extract = driver.Extract{} }, "extract"},
		{"datapoint with two extractors", func(s *driver.Spec) {
			s.Polls[0].Datapoints[0].Extract = driver.Extract{Regex: "x", OID: "1.3"}
		}, "extract"},
		{"datapoint with a bad regex", func(s *driver.Spec) { s.Polls[0].Datapoints[0].Extract = driver.Extract{Regex: "("} }, "regex"},
		{"transform scale on nothing", func(s *driver.Spec) {
			s.Polls[0].Datapoints[0].Transform = &driver.Transform{}
		}, "transform"},
		{"transform with an unknown cast", func(s *driver.Spec) {
			s.Polls[0].Datapoints[0].Transform = &driver.Transform{Cast: "quaternion"}
		}, "cast"},
		{"listener without a match rule", func(s *driver.Spec) { s.Listeners[0].Match = driver.Match{} }, "match"},
		{"listener with no datapoints", func(s *driver.Spec) { s.Listeners[0].Datapoints = nil }, "datapoint"},
		{"duplicate function names", func(s *driver.Spec) { s.Listeners[0].Name = "status" }, "status"},
		{"command binding on an unknown command type", func(s *driver.Spec) { s.Commands[0].CommandType = "self-destruct" }, "self-destruct"},
		{"command binding with an empty request", func(s *driver.Spec) { s.Commands[0].Request = driver.Request{} }, "request"},
		{"duplicate command bindings", func(s *driver.Spec) {
			s.Commands = append(s.Commands, driver.CommandBinding{CommandType: "set-input", Request: driver.Request{Line: "x"}})
		}, "set-input"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpec()
			tc.mutate(s)
			err := s.Validate(catalog())
			if err == nil {
				t.Fatalf("mutated spec validated; want a refusal naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal %q does not name %q", err, tc.want)
			}
		})
	}
}

// TestParseRefusesUnknownFields pins reject-not-project at the authoring door:
// a typo like "pols" must refuse at write, not silently author a driver with
// no poll functions.
func TestParseRefusesUnknownFields(t *testing.T) {
	if _, err := driver.Parse([]byte(`{"version": 1, "transport": "tcp", "pols": []}`)); err == nil {
		t.Fatal("unknown field parsed; want a refusal")
	}
}

func TestParseRoundTrips(t *testing.T) {
	raw := []byte(`{
		"version": 1,
		"transport": "snmp",
		"inputs": [
			{"name": "host", "kind": "string", "required": true},
			{"name": "community", "kind": "secret", "secret_type": "snmp-community", "required": true}
		],
		"polls": [{
			"name": "scalars",
			"schedule": {"every": "60s"},
			"request": {"get": ["1.3.6.1.2.1.1.3.0"]},
			"datapoints": [
				{"name": "uptime", "extract": {"oid": "1.3.6.1.2.1.1.3.0"}, "transform": {"scale": 0.01}}
			]
		}]
	}`)
	s, err := driver.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := s.Validate(catalog()); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if s.Polls[0].Datapoints[0].Transform.Scale != 0.01 {
		t.Fatalf("scale = %v, want 0.01", s.Polls[0].Datapoints[0].Transform.Scale)
	}
	if got := s.Polls[0].Schedule.Interval(); got.Seconds() != 60 {
		t.Fatalf("interval = %v, want 60s", got)
	}
}

// TestValidateNilCatalogSkipsNameResolution pins the two-stage contract: a nil
// catalog checks structure only (the parse-time floor), so name resolution is
// explicitly the write gate's job, not a side effect of parsing.
func TestValidateStructureOnly(t *testing.T) {
	s := validSpec()
	s.Polls[0].Datapoints[0].Name = "not-in-any-catalog"
	if err := s.Validate(nil); err != nil {
		t.Fatalf("structure-only validation resolved names: %v", err)
	}
	s.Polls[0].Datapoints[0].Extract = driver.Extract{}
	if err := s.Validate(nil); err == nil {
		t.Fatal("structure-only validation must still refuse a missing extractor")
	}
}
