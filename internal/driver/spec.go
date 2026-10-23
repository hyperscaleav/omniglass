// Package driver holds the declarative driver spec: the versioned, data-not-code
// body a driver row carries (#813). A spec names the transport it rides, the
// inputs an attachment must supply (host, port, credentials as secret
// references), and the three function families one generic engine interprets:
// poll functions (schedule, request, per-sample extraction), listeners
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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/internal/transport"
)

// Catalog resolves the names a spec references against the platform catalogs.
// A nil Catalog validates structure only: that is the parse-time floor, and
// name resolution is explicitly the write gate's job.
type Catalog interface {
	// LaneOf resolves an emitted sample's name to its lane ("metric" or
	// "property"); ok is false for a name no catalog holds.
	LaneOf(name string) (lane string, ok bool)
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
	// Unmatched is what becomes of an inbound line on a held session that no
	// listener claims and no exchange is owed (#817): discard (the default;
	// the node counts it) or collection-failed (the node lands one
	// collection-failed per tick naming how many and the last).
	Unmatched string `json:"unmatched,omitempty"`
}

// The unmatched-line policies.
const (
	UnmatchedDiscard          = "discard"
	UnmatchedCollectionFailed = "collection-failed"
)

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
	// Reply is the protocol's answer shape for a line request (#817): over a
	// held session, only a line it matches answers this request, so a push
	// or a stray line is never taken as the answer. Absent, the first line
	// no listener claims answers it.
	Reply *Reply `json:"reply,omitempty"`
}

// Reply is a line request's declared answer: the shape of a good answer
// (exactly one of a prefix or a regex) and, optionally, the shape of the
// device's refusal, which answers the request as a failure carrying the
// device's text. A regex is Go RE2 with substring semantics: anchor it
// (^...$) or it also matches any line containing it, a push included.
type Reply struct {
	Prefix string `json:"prefix,omitempty"`
	Regex  string `json:"regex,omitempty"`
	Error  *Match `json:"error,omitempty"`
	// any makes the shape match every line (AnyLine), never authored.
	any bool
}

// AnyLine is the answer shape that takes the first line of any kind, one a
// listener claims included (the listener has it too): a subscribe's
// acknowledgement, which a device may send as a push.
func AnyLine() *Reply { return &Reply{any: true} }

// Accepts reads the declared answer shape against a line; no shape declared
// accepts any line.
func (r *Reply) Accepts(line string) bool {
	if r == nil || r.any {
		return true
	}
	return Match{Prefix: r.Prefix, Regex: r.Regex}.Matches(line)
}

// Fails reports a line matching the declared error shape.
func (r *Reply) Fails(line string) bool {
	return r != nil && r.Error != nil && r.Error.Matches(line)
}

// Empty reports whether the request says nothing at all.
func (r Request) Empty() bool {
	return len(r.Get) == 0 && r.Line == "" && r.Path == ""
}

// Extract locates one emitted value in a response: exactly one of the
// extractor kinds (an OID, a regex capture, a JSONPath, a key of a key-value
// body).
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

// Emit is one sample a function emits: a lane-catalog name (the lane itself
// is resolved from the catalogs at write and baked at attach, per ADR-0065's
// split: the name is the signal, the emission is a sample), where to find it
// in the response, and an optional transform.
type Emit struct {
	Name      string     `json:"name"`
	Extract   Extract    `json:"extract"`
	Transform *Transform `json:"transform,omitempty"`
}

// Poll is a scheduled ask: send Request every Schedule, extract Emits from
// the response.
type Poll struct {
	Name     string   `json:"name"`
	Schedule Schedule `json:"schedule"`
	Request  Request  `json:"request"`
	Emits    []Emit   `json:"emits"`
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
// Emits extract from a claimed one.
type Listener struct {
	Name  string   `json:"name"`
	Arm   []string `json:"arm,omitempty"`
	Match Match    `json:"match"`
	Emits []Emit   `json:"emits"`
}

// CommandBinding declares how one command type actuates over this protocol,
// and how a command of it settles.
type CommandBinding struct {
	CommandType string  `json:"command_type"`
	Request     Request `json:"request"`
	Settle      *Settle `json:"settle,omitempty"`
}

// Settle declares what settles a command: the device's reply to it
// (from: response), or the next observation of the command type's target
// (from: observe, the default). A response settle reads the reply with
// exactly one of Extract (the reply carries the new value, compared to the
// intended one) or Success (the reply only confirms).
type Settle struct {
	From    string   `json:"from,omitempty"`
	Extract *Extract `json:"extract,omitempty"`
	Success *Match   `json:"success,omitempty"`
}

// The settle sources.
const (
	SettleFromObserve  = "observe"
	SettleFromResponse = "response"
)

// ReplyVerdict is a binding's reading of a device's reply. Judged is false
// when the binding settles on observation, so the reply decides nothing.
type ReplyVerdict struct {
	Judged    bool
	Succeeded bool
	HasValue  bool
	Value     string
}

// JudgeReply reads a device's reply to a command the way the binding's
// settle declares: a value extracted (Succeeded when it is found), or a
// confirmation matched.
func (b CommandBinding) JudgeReply(reply string) ReplyVerdict {
	st := b.Settle
	if st == nil || st.From != SettleFromResponse {
		return ReplyVerdict{}
	}
	if st.Extract != nil {
		v, err := locate(*st.Extract, Payload{Text: reply})
		if err != nil {
			return ReplyVerdict{Judged: true}
		}
		return ReplyVerdict{Judged: true, Succeeded: true, HasValue: true, Value: v}
	}
	if st.Success != nil {
		return ReplyVerdict{Judged: true, Succeeded: matches(*st.Success, reply)}
	}
	return ReplyVerdict{Judged: true}
}

// matches applies a Match rule to a line.
func matches(m Match, line string) bool { return m.Matches(line) }

// Matches applies the rule to a line: the literal prefix, or the regex
// (Go RE2, substring semantics; compiled once and reused).
func (m Match) Matches(line string) bool {
	if m.Prefix != "" {
		return strings.HasPrefix(line, m.Prefix)
	}
	if m.Regex == "" {
		return false
	}
	re, err := compiled(m.Regex)
	return err == nil && re.MatchString(line)
}

// compiledPatterns caches every compiled regex by its pattern: a held
// session's reader matches each inbound line against the listeners and the
// reply shapes, and a spec's patterns are a small fixed set, so each is
// compiled once for the life of the process.
var (
	compiledPatterns sync.Map // pattern -> *regexp.Regexp
	compileCount     atomic.Int64
)

func compiled(pattern string) (*regexp.Regexp, error) {
	if re, ok := compiledPatterns.Load(pattern); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	compileCount.Add(1)
	actual, _ := compiledPatterns.LoadOrStore(pattern, re)
	return actual.(*regexp.Regexp), nil
}

// validateReply refuses a reply shape on a request with no line, or a
// malformed one.
func validateReply(what string, req Request) error {
	if req.Reply == nil {
		return nil
	}
	if req.Line == "" {
		return fmt.Errorf("driver: %s declares a reply shape, which only a line request has", what)
	}
	if err := validateMatch(what+" reply", Match{Prefix: req.Reply.Prefix, Regex: req.Reply.Regex}); err != nil {
		return err
	}
	if req.Reply.Error != nil {
		return validateMatch(what+" reply error shape", *req.Reply.Error)
	}
	return nil
}

// validateSettle refuses a malformed settle declaration.
func validateSettle(c CommandBinding) error {
	st := c.Settle
	if st == nil {
		return nil
	}
	switch st.From {
	case "", SettleFromObserve:
		if st.Extract != nil || st.Success != nil {
			return fmt.Errorf("driver: command binding %q settles on observe, which reads no reply (drop extract and success)", c.CommandType)
		}
		return nil
	case SettleFromResponse:
	default:
		return fmt.Errorf("driver: command binding %q settles from %q; want response or observe", c.CommandType, st.From)
	}
	if (st.Extract == nil) == (st.Success == nil) {
		return fmt.Errorf("driver: command binding %q settles from its response and needs exactly one of extract or success", c.CommandType)
	}
	if st.Extract != nil {
		// A reply is a line of text, so a regex is the extractor that applies.
		if st.Extract.Regex == "" || st.Extract.OID != "" || st.Extract.JSONPath != "" || st.Extract.Key != "" {
			return fmt.Errorf("driver: command binding %q extracts its reply's value with a regex", c.CommandType)
		}
		if _, err := regexp.Compile(st.Extract.Regex); err != nil {
			return fmt.Errorf("driver: command binding %q has an uncompilable reply regex: %v", c.CommandType, err)
		}
		return nil
	}
	return validateMatch(fmt.Sprintf("command binding %q settle", c.CommandType), *st.Success)
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
// a Catalog it also resolves every referenced name (emitted samples, command
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
	switch s.Unmatched {
	case "", UnmatchedDiscard, UnmatchedCollectionFailed:
	default:
		return fmt.Errorf("driver: unmatched policy %q; want discard or collection-failed", s.Unmatched)
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
		// Validate each emit first (exactly one extractor, a known name, a
		// compilable regex), then bind the request and extractors to the
		// transport: a spec that cannot be interpreted (an snmp poll with no
		// OIDs, a tcp emit that locates by oid/key/jsonpath, a poll on a
		// transport with no wired fetcher) refuses at authoring time rather than
		// faulting on every tick forever (ADR-0135). Shape runs after the
		// per-emit check so a malformed extractor names the extractor fault, not
		// the transport one.
		if err := validateEmits("poll", p.Name, p.Emits, cat); err != nil {
			return err
		}
		if err := validateReply(fmt.Sprintf("poll %q", p.Name), p.Request); err != nil {
			return err
		}
		if err := checkPollShape(s.Transport, p); err != nil {
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
		if err := validateMatch(fmt.Sprintf("listener %q", l.Name), l.Match); err != nil {
			return err
		}
		if err := validateEmits("listener", l.Name, l.Emits, cat); err != nil {
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
		// Only the line-protocol actuation wire exists today, so a binding on a
		// transport it cannot reach refuses at write rather than dispatching a
		// request no node can send.
		if s.Transport != "tcp" {
			return fmt.Errorf("driver: command binding %q rides transport %q, which has no actuation wire (only tcp actuates today)", c.CommandType, s.Transport)
		}
		if c.Request.Line == "" {
			return fmt.Errorf("driver: command binding %q on tcp needs a line request", c.CommandType)
		}
		if err := validateSettle(c); err != nil {
			return err
		}
		if err := validateReply(fmt.Sprintf("command binding %q", c.CommandType), c.Request); err != nil {
			return err
		}
		// Template references are checked at write: only ${value} (the
		// intended value) and ${arg.key} (an issue param) render, so a typo
		// refuses here rather than half-rendering at actuation.
		for _, tpl := range []string{c.Request.Line, c.Request.Path} {
			for _, m := range argPattern.FindAllStringSubmatch(tpl, -1) {
				ref := m[1]
				if ref != "value" && !strings.HasPrefix(ref, "arg.") {
					return fmt.Errorf("driver: command binding %q references unknown template field %q", c.CommandType, ref)
				}
			}
		}
	}
	return nil
}

func validateEmits(family, fn string, emits []Emit, cat Catalog) error {
	if len(emits) == 0 {
		return fmt.Errorf("driver: %s %q emits nothing", family, fn)
	}
	for _, em := range emits {
		if em.Name == "" {
			return fmt.Errorf("driver: %s %q emits a sample with no name", family, fn)
		}
		if cat != nil {
			if _, ok := cat.LaneOf(em.Name); !ok {
				return fmt.Errorf("driver: %s %q emits %q, which no catalog holds", family, fn, em.Name)
			}
		}
		if err := validateExtract(fn, em); err != nil {
			return err
		}
		if em.Transform != nil {
			if err := validateTransform(fn, em.Name, *em.Transform); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateExtract(fn string, em Emit) error {
	set := 0
	for _, v := range []string{em.Extract.OID, em.Extract.Regex, em.Extract.JSONPath, em.Extract.Key} {
		if v != "" {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("driver: %s emit %q needs exactly one extract kind, has %d", fn, em.Name, set)
	}
	if em.Extract.Regex != "" {
		if _, err := regexp.Compile(em.Extract.Regex); err != nil {
			return fmt.Errorf("driver: %s emit %q has an uncompilable regex: %v", fn, em.Name, err)
		}
	}
	return nil
}

func validateTransform(fn, name string, tr Transform) error {
	if tr.Cast == "" && tr.Scale == 0 && len(tr.Map) == 0 {
		return fmt.Errorf("driver: %s emit %q declares an empty transform", fn, name)
	}
	switch tr.Cast {
	case "", "int", "float", "text":
	default:
		return fmt.Errorf("driver: %s emit %q has unknown cast %q", fn, name, tr.Cast)
	}
	return nil
}

// checkPollShape binds a poll's request and extractors to the transport it
// rides, so an uninterpretable combination refuses at write. Only snmp and tcp
// have wired poll fetchers today (internal/collection); a poll on any other
// transport refuses rather than faulting every tick.
func checkPollShape(tr string, p Poll) error {
	switch tr {
	case "snmp":
		if len(p.Request.Get) == 0 {
			return fmt.Errorf("driver: snmp poll %q needs a get request (the scalar OIDs to read)", p.Name)
		}
		for _, em := range p.Emits {
			if em.Extract.OID == "" {
				return fmt.Errorf("driver: snmp poll %q emit %q must locate by oid", p.Name, em.Name)
			}
		}
	case "tcp":
		if p.Request.Line == "" {
			return fmt.Errorf("driver: tcp poll %q needs a line request", p.Name)
		}
		for _, em := range p.Emits {
			// A line response is text: only a regex extractor can read it. An
			// emit that names an oid, key, or jsonpath (the keyed and json forms)
			// would validate clean yet fault every tick, so refuse it here. An
			// emit with no extractor at all is the general "exactly one extractor"
			// refusal's job, not this one.
			if em.Extract.OID != "" || em.Extract.Key != "" || em.Extract.JSONPath != "" {
				return fmt.Errorf("driver: tcp poll %q emit %q must locate by regex (a line response carries no keyed or json body to read an oid, key, or jsonpath from)", p.Name, em.Name)
			}
		}
	default:
		return fmt.Errorf("driver: poll %q rides transport %q, which has no poll fetcher (only snmp and tcp poll today)", p.Name, tr)
	}
	return nil
}

func validateMatch(what string, m Match) error {
	set := 0
	if m.Prefix != "" {
		set++
	}
	if m.Regex != "" {
		set++
		if _, err := regexp.Compile(m.Regex); err != nil {
			return fmt.Errorf("driver: %s has an uncompilable match regex: %v", what, err)
		}
	}
	if set != 1 {
		return fmt.Errorf("driver: %s needs exactly one match rule, has %d", what, set)
	}
	return nil
}
