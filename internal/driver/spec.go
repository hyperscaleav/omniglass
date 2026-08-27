// Package driver holds the declarative driver spec: the versioned, data-not-code
// body a driver row carries (#813). A spec names the transport it rides, the
// inputs an attachment must supply (host, port, credentials as secret
// references), and the three function families one generic engine interprets:
// poll functions (schedule, request, per-datapoint extraction), listeners
// (instantiation commands, a match rule, the same extraction), and command
// bindings (how a command type actuates over this protocol). The package is a
// leaf like internal/transport: parsing and validation are pure, with catalog
// name resolution injected through the Catalog interface so the storage
// gateway supplies the live catalogs and unit tests supply a map.
package driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/hyperscaleav/omniglass/internal/transport"
)

// Catalog resolves the names a spec references against the platform catalogs.
// A nil Catalog validates structure only: that is the parse-time floor, and
// name resolution is explicitly the write gate's job.
type Catalog interface {
	// DatapointLane resolves a datapoint name to its lane ("metric" or
	// "property"); ok is false for a name no catalog holds.
	DatapointLane(name string) (lane string, ok bool)
	// CommandTypeExists reports whether the command-type catalog holds name.
	CommandTypeExists(name string) bool
	// SecretTypeExists reports whether the secret-type catalog holds name.
	SecretTypeExists(name string) bool
}

// Spec is a driver's declarative body. Version gates interpretation: this
// binary speaks version 1, and an unknown version refuses at write rather
// than surprising at collection time.
type Spec struct {
	Version   int              `json:"version"`
	Transport string           `json:"transport"`
	Inputs    []Input          `json:"inputs,omitempty"`
	Polls     []Poll           `json:"polls,omitempty"`
	Listeners []Listener       `json:"listeners,omitempty"`
	Commands  []CommandBinding `json:"commands,omitempty"`
}

// Input is one parameter an attachment must supply. Kind "secret" carries a
// reference to a secret row (by name), never a value; SecretType pins which
// shape of secret satisfies it.
type Input struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"` // string | number | secret
	SecretType string `json:"secret_type,omitempty"`
	Required   bool   `json:"required,omitempty"`
	Default    string `json:"default,omitempty"`
}

// Schedule is a poll function's cadence.
type Schedule struct {
	Every string `json:"every"`
}

// Interval parses the cadence; zero on a malformed value (Validate refuses
// those, so a validated spec never returns zero).
func (s Schedule) Interval() time.Duration {
	d, err := time.ParseDuration(s.Every)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// Request is what a function sends, shaped by the transport it rides: scalar
// OIDs for an SNMP get, a line for a line protocol, a path for HTTP. Exactly
// the fields the transport needs; an all-empty request is refused.
type Request struct {
	Get  []string `json:"get,omitempty"`
	Line string   `json:"line,omitempty"`
	Path string   `json:"path,omitempty"`
}

// Empty reports whether the request says nothing at all.
func (r Request) Empty() bool {
	return len(r.Get) == 0 && r.Line == "" && r.Path == ""
}

// Extract locates one datapoint in a response: exactly one of the extractor
// kinds (an OID, a regex capture, a JSONPath, a key of a key-value body).
type Extract struct {
	OID      string `json:"oid,omitempty"`
	Regex    string `json:"regex,omitempty"`
	JSONPath string `json:"jsonpath,omitempty"`
	Key      string `json:"key,omitempty"`
}

// Transform is the declared minimal shaping applied after extraction: a cast,
// a scale, an enum map. Declared data operations only; the expression engine
// is not a dependency (#603 thin cut).
type Transform struct {
	Cast  string            `json:"cast,omitempty"` // int | float | text
	Scale float64           `json:"scale,omitempty"`
	Map   map[string]string `json:"map,omitempty"`
}

// Datapoint is one emitted value: a catalog name (the lane is resolved from
// the catalogs at write and baked at attach), where to find it, and an
// optional transform.
type Datapoint struct {
	Name      string     `json:"name"`
	Extract   Extract    `json:"extract"`
	Transform *Transform `json:"transform,omitempty"`
}

// Poll is a scheduled ask: send Request every Schedule, extract Datapoints
// from the response.
type Poll struct {
	Name       string      `json:"name"`
	Schedule   Schedule    `json:"schedule"`
	Request    Request     `json:"request"`
	Datapoints []Datapoint `json:"datapoints"`
}

// Match classifies an unsolicited inbound payload to its listener: exactly
// one of a literal prefix or a regex.
type Match struct {
	Prefix string `json:"prefix,omitempty"`
	Regex  string `json:"regex,omitempty"`
}

// Listener is a declared wait: Arm holds the instantiation commands sent on
// session establish and re-sent on every recover (plumbing, not recorded
// command rows, per the #603 ruling), Match claims inbound payloads, and
// Datapoints extract from a claimed one.
type Listener struct {
	Name       string      `json:"name"`
	Arm        []string    `json:"arm,omitempty"`
	Match      Match       `json:"match"`
	Datapoints []Datapoint `json:"datapoints"`
}

// CommandBinding declares how one command type actuates over this protocol.
type CommandBinding struct {
	CommandType string  `json:"command_type"`
	Request     Request `json:"request"`
}

// Parse decodes a spec, refusing unknown fields: a typo authors nothing
// (reject-not-project at the authoring door).
func Parse(raw []byte) (*Spec, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s Spec
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("driver: parse spec: %w", err)
	}
	return &s, nil
}

// Validate refuses a spec that cannot be interpreted, naming the fault. With
// a Catalog it also resolves every referenced name (datapoints, command
// types, secret types); with nil it checks structure only.
func (s *Spec) Validate(cat Catalog) error {
	if s.Version != 1 {
		return fmt.Errorf("driver: spec version %d unsupported (this binary speaks version 1)", s.Version)
	}
	if s.Transport == "" {
		return fmt.Errorf("driver: spec names no transport")
	}
	if _, ok := transport.ByName(s.Transport); !ok {
		return fmt.Errorf("driver: unknown transport %q", s.Transport)
	}
	if len(s.Polls) == 0 && len(s.Listeners) == 0 && len(s.Commands) == 0 {
		return fmt.Errorf("driver: spec declares no function of any family")
	}

	inputs := map[string]bool{}
	for i, in := range s.Inputs {
		if in.Name == "" {
			return fmt.Errorf("driver: input %d has no name", i)
		}
		if inputs[in.Name] {
			return fmt.Errorf("driver: duplicate input %q", in.Name)
		}
		inputs[in.Name] = true
		switch in.Kind {
		case "string", "number":
			if in.SecretType != "" {
				return fmt.Errorf("driver: input %q is not a secret and cannot carry secret_type", in.Name)
			}
		case "secret":
			if in.SecretType == "" {
				return fmt.Errorf("driver: secret input %q needs a secret_type", in.Name)
			}
			if cat != nil && !cat.SecretTypeExists(in.SecretType) {
				return fmt.Errorf("driver: secret input %q references unknown secret type %q", in.Name, in.SecretType)
			}
		default:
			return fmt.Errorf("driver: input %q has unknown kind %q", in.Name, in.Kind)
		}
	}

	functions := map[string]bool{}
	for _, p := range s.Polls {
		if p.Name == "" {
			return fmt.Errorf("driver: a poll function has no name")
		}
		if functions[p.Name] {
			return fmt.Errorf("driver: duplicate function name %q", p.Name)
		}
		functions[p.Name] = true
		if d, err := time.ParseDuration(p.Schedule.Every); err != nil || d <= 0 {
			return fmt.Errorf("driver: poll %q has an uninterpretable schedule %q", p.Name, p.Schedule.Every)
		}
		if p.Request.Empty() {
			return fmt.Errorf("driver: poll %q has an empty request", p.Name)
		}
		if err := validateDatapoints("poll", p.Name, p.Datapoints, cat); err != nil {
			return err
		}
	}
	for _, l := range s.Listeners {
		if l.Name == "" {
			return fmt.Errorf("driver: a listener has no name")
		}
		if functions[l.Name] {
			return fmt.Errorf("driver: duplicate function name %q", l.Name)
		}
		functions[l.Name] = true
		if err := validateMatch(l.Name, l.Match); err != nil {
			return err
		}
		if err := validateDatapoints("listener", l.Name, l.Datapoints, cat); err != nil {
			return err
		}
	}

	bindings := map[string]bool{}
	for _, c := range s.Commands {
		if c.CommandType == "" {
			return fmt.Errorf("driver: a command binding names no command type")
		}
		if bindings[c.CommandType] {
			return fmt.Errorf("driver: duplicate command binding for %q", c.CommandType)
		}
		bindings[c.CommandType] = true
		if cat != nil && !cat.CommandTypeExists(c.CommandType) {
			return fmt.Errorf("driver: command binding references unknown command type %q", c.CommandType)
		}
		if c.Request.Empty() {
			return fmt.Errorf("driver: command binding %q has an empty request", c.CommandType)
		}
	}
	return nil
}

func validateDatapoints(family, fn string, dps []Datapoint, cat Catalog) error {
	if len(dps) == 0 {
		return fmt.Errorf("driver: %s %q emits no datapoints", family, fn)
	}
	for _, dp := range dps {
		if dp.Name == "" {
			return fmt.Errorf("driver: %s %q has a datapoint with no name", family, fn)
		}
		if cat != nil {
			if _, ok := cat.DatapointLane(dp.Name); !ok {
				return fmt.Errorf("driver: %s %q emits %q, which no catalog holds", family, fn, dp.Name)
			}
		}
		if err := validateExtract(fn, dp); err != nil {
			return err
		}
		if dp.Transform != nil {
			if err := validateTransform(fn, dp.Name, *dp.Transform); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateExtract(fn string, dp Datapoint) error {
	set := 0
	for _, v := range []string{dp.Extract.OID, dp.Extract.Regex, dp.Extract.JSONPath, dp.Extract.Key} {
		if v != "" {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("driver: %s datapoint %q needs exactly one extract kind, has %d", fn, dp.Name, set)
	}
	if dp.Extract.Regex != "" {
		if _, err := regexp.Compile(dp.Extract.Regex); err != nil {
			return fmt.Errorf("driver: %s datapoint %q has an uncompilable regex: %v", fn, dp.Name, err)
		}
	}
	return nil
}

func validateTransform(fn, dp string, tr Transform) error {
	if tr.Cast == "" && tr.Scale == 0 && len(tr.Map) == 0 {
		return fmt.Errorf("driver: %s datapoint %q declares an empty transform", fn, dp)
	}
	switch tr.Cast {
	case "", "int", "float", "text":
	default:
		return fmt.Errorf("driver: %s datapoint %q has unknown cast %q", fn, dp, tr.Cast)
	}
	return nil
}

func validateMatch(fn string, m Match) error {
	set := 0
	if m.Prefix != "" {
		set++
	}
	if m.Regex != "" {
		set++
		if _, err := regexp.Compile(m.Regex); err != nil {
			return fmt.Errorf("driver: listener %q has an uncompilable match regex: %v", fn, err)
		}
	}
	if set != 1 {
		return fmt.Errorf("driver: listener %q needs exactly one match rule, has %d", fn, set)
	}
	return nil
}
