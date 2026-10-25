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
	"github.com/hyperscaleav/omniglass/internal/writerclass"
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
	// WriterClass resolves an emitted name's writer class (ADR-0161): a
	// driver emits only a collected key, so a calc rule's output is refused
	// here as a reserved key is refused by name.
	WriterClass(name string) writerclass.Class
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
	// Session declares the held session's auth step, its terminal replies
	// and its re-probe (#1049); absent, the session has no auth step and
	// every failure is retryable.
	Session *SessionSpec `json:"session,omitempty"`
	// Ladder is the reachability ladder (ADR-0158); absent, the transport
	// registry's defaults.
	Ladder *Ladder `json:"ladder,omitempty"`
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
// OIDs for an SNMP get, a line for a line protocol, a request for http (its
// method, path, query, headers and body, and the format its answer is read
// in, #902). Exactly the fields the transport needs; an all-empty request is
// refused.
type Request struct {
	Get  []string `json:"get,omitempty"`
	Line string   `json:"line,omitempty"`
	Path string   `json:"path,omitempty"`
	// Method is an http request's method (GET when absent); Query its query
	// parameters and Headers its headers, each value a template a command
	// renders; Body its body, a JSON value rendered in its BodyFormat (json,
	// form, xml, text). Format is how the answer is read (json, xml, html,
	// text), and Namespaces binds the prefixes an xpath over an xml answer
	// names.
	Method     string            `json:"method,omitempty"`
	Query      map[string]string `json:"query,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       json.RawMessage   `json:"body,omitempty"`
	BodyFormat string            `json:"body_format,omitempty"`
	Format     string            `json:"format,omitempty"`
	Namespaces map[string]string `json:"namespaces,omitempty"`
	// Reply is the protocol's answer shape for a line request (#817): over a
	// held session, only a line it matches answers this request, so a push
	// or a stray line is never taken as the answer. Absent, the first line
	// no listener claims answers it.
	Reply *Reply `json:"reply,omitempty"`
	// Retries is how many times a datagram request is sent again when no
	// answer comes (#1037), udp alone: absent, a poll's and the request
	// rung's default (DefaultPollRetries) or a command's
	// (DefaultCommandRetries). The endpoint's timeout bounds the whole
	// exchange, each attempt an equal share of it.
	Retries *int `json:"retries,omitempty"`
}

// The retry defaults and bound (#1037). A poll or the request rung re-asks
// a lost datagram twice, since asking again is harmless; a command is sent
// once unless its binding declares otherwise, since a command may be
// relative (a volume step) and a lost reply must not send it twice.
const (
	DefaultPollRetries    = 2
	DefaultCommandRetries = 0
	MaxRetries            = 5
)

// RetriesOr is the request's declared retries, else def.
func (r Request) RetriesOr(def int) int {
	if r.Retries == nil {
		return def
	}
	return *r.Retries
}

// validateRetries refuses retries on a transport that does not resend (only
// a datagram is lost silently) or outside 0 to MaxRetries, and over udp a
// request that declares no reply shape: a re-sent datagram draws a reply per
// send, and a reply of any shape could be a late one to an earlier ask, so
// only a declared shape tells this ask's answer from another's.
func validateRetries(transportName, what string, r Request) error {
	if transportName == "udp" && r.Reply == nil {
		return fmt.Errorf("driver: %s over udp declares no reply shape (request.reply): a datagram's answer is told from a late reply to another ask only by its shape", what)
	}
	if r.Retries == nil {
		return nil
	}
	if transportName != "udp" {
		return fmt.Errorf("driver: %s declares retries, which only a udp datagram has (%q resends nothing)", what, transportName)
	}
	if *r.Retries < 0 || *r.Retries > MaxRetries {
		return fmt.Errorf("driver: %s declares %d retries; want 0 to %d", what, *r.Retries, MaxRetries)
	}
	return nil
}

// Reply is a line request's declared answer: the shape of a good answer
// (exactly one of a prefix or a regex) and, optionally, the shape of the
// device's refusal, which answers the request as a failure carrying the
// device's text. A regex is Go RE2 with substring semantics: anchor it
// (^...$) or it also matches any line containing it, a push included.
type Reply struct {
	Prefix string `json:"prefix,omitempty"`
	Regex  string `json:"regex,omitempty"`
	// Field picks a JSON answer by one field (#1034); Correlate names the
	// field of a JSON frame a node writes a fresh id into, the answer being
	// the frame that carries it back (websocket, #1034).
	Field     *FieldMatch `json:"field,omitempty"`
	Correlate string      `json:"correlate,omitempty"`
	Error     *Match      `json:"error,omitempty"`
	// any makes the shape match every line (AnyLine), never authored.
	any bool
	// bound marks a reply Correlated made for one exchange: its error shape
	// answers only a frame carrying the exchange's id, and a frame carrying
	// it that the error shape matches is the refusal, not the answer.
	bound bool
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
	if r.Field != nil {
		return r.Field.matches(line) && !(r.bound && r.Error != nil && r.Error.Matches(line))
	}
	return Match{Prefix: r.Prefix, Regex: r.Regex}.Matches(line)
}

// Fails reports a line matching the declared error shape; a correlated
// exchange's, only one carrying its id.
func (r *Reply) Fails(line string) bool {
	if r == nil || r.Error == nil || !r.Error.Matches(line) {
		return false
	}
	return !r.bound || r.Field.matches(line)
}

// Empty reports whether the request says nothing at all.
func (r Request) Empty() bool {
	return len(r.Get) == 0 && r.Line == "" && r.Path == "" && !r.hasHTTPFields()
}

// Extract locates one emitted value in a response: exactly one of the
// selector kinds of the one extraction primitive (an OID, a regex capture, a
// JSONPath, a key of a key-value body, an XPath over an xml or html answer,
// #902), never a second extractor per format.
type Extract struct {
	OID      string `json:"oid,omitempty"`
	Regex    string `json:"regex,omitempty"`
	JSONPath string `json:"jsonpath,omitempty"`
	Key      string `json:"key,omitempty"`
	XPath    string `json:"xpath,omitempty"`
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
// one of a literal prefix or a regex. A listener's match may also name a
// server-sent event (Event) and claim JSON data by a field (Field) (#1038),
// which only the router reads (MatchesMessage).
type Match struct {
	Prefix string      `json:"prefix,omitempty"`
	Regex  string      `json:"regex,omitempty"`
	Event  string      `json:"event,omitempty"`
	Field  *FieldMatch `json:"field,omitempty"`
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
		v, err := locate(*st.Extract, b.replyPayload(reply))
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

// replyPayload is a reply in the forms its binding reads it: a line as text,
// an http answer in its request's declared format.
func (b CommandBinding) replyPayload(reply string) Payload {
	if b.Request.Path == "" {
		// A line or a frame: text a regex reads, a JSON one a jsonpath too.
		return Payload{Text: reply, JSON: []byte(reply)}
	}
	p := HTTPPayload([]byte(reply), &b.Request)
	p.docs = &parsedDocs{}
	return p
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
func validateReply(transportName, what string, req Request) error {
	if req.Reply == nil {
		return nil
	}
	// A reply shape picks an answer out of what a held session reads: a
	// line, or a websocket's frame (a line or a JSON body). An http request
	// is answered by its own response.
	if IsHTTP(transportName) || (req.Line == "" && !(IsWebSocket(transportName) && len(req.Body) > 0)) {
		return fmt.Errorf("driver: %s declares a reply shape, which only a line or a frame request has", what)
	}
	rep := req.Reply
	kinds := 0
	for _, set := range []bool{rep.Prefix != "", rep.Regex != "", rep.Field != nil, rep.Correlate != ""} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return fmt.Errorf("driver: %s reply declares %d shapes; want one of a prefix, a regex, a field or a correlate", what, kinds)
	}
	switch {
	case rep.Correlate != "" && (!IsWebSocket(transportName) || len(req.Body) == 0):
		return fmt.Errorf("driver: %s reply correlates by %s, which needs a JSON object frame (a websocket body)", what, rep.Correlate)
	case rep.Field != nil:
		if err := validateListenerMatch(what+" reply", Match{Field: rep.Field}, "", nil); err != nil {
			return err
		}
	case rep.Correlate == "":
		if err := validateMatch(what+" reply", Match{Prefix: rep.Prefix, Regex: rep.Regex}); err != nil {
			return err
		}
	}
	if req.Reply.Error != nil {
		return validateMatch(what+" reply error shape", *req.Reply.Error)
	}
	return nil
}

// argRefs lists a template's references.
func argRefs(s string) []string {
	var out []string
	for _, m := range argPattern.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// validateSettle refuses a malformed settle declaration; httpBinding reads
// the reply as an http answer.
func validateSettle(c CommandBinding, httpBinding bool) error {
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
		if httpBinding {
			// An http reply is read in its request's format, by any selector
			// that format admits (#902).
			if err := validateExtract(c.CommandType, Emit{Name: "settle", Extract: *st.Extract}); err != nil {
				return err
			}
			return checkHTTPEmit(fmt.Sprintf("command binding %q settle", c.CommandType), *st.Extract, c.Request)
		}
		// A reply is a line of text, so a regex is the extractor that applies.
		if st.Extract.Regex == "" || st.Extract.OID != "" || st.Extract.JSONPath != "" || st.Extract.Key != "" || st.Extract.XPath != "" {
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
		if IsWebSocket(s.Transport) {
			if err := validateFrameRequest(fmt.Sprintf("poll %q", p.Name), p.Request); err != nil {
				return err
			}
		} else if err := validateHTTPFieldsOff(s.Transport, fmt.Sprintf("poll %q", p.Name), p.Request); err != nil {
			return err
		}
		if IsHTTP(s.Transport) {
			if err := validateHTTPRequest(fmt.Sprintf("poll %q", p.Name), p.Request, false, s.Session); err != nil {
				return err
			}
		}
		if err := validateReply(s.Transport, fmt.Sprintf("poll %q", p.Name), p.Request); err != nil {
			return err
		}
		if err := validateRetries(s.Transport, fmt.Sprintf("poll %q", p.Name), p.Request); err != nil {
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
		var stream *Request
		switch {
		case IsHTTP(s.Transport):
			// Over http a listener rides the session's server-sent events
			// stream (#1038), which has no upstream to arm.
			if s.Session == nil || s.Session.Stream == nil {
				return fmt.Errorf("driver: listener %q rides %s, whose listen path is a server-sent events stream; declare session.stream", l.Name, s.Transport)
			}
			if len(l.Arm) > 0 {
				return fmt.Errorf("driver: listener %q declares arm, and a server-sent events stream has no upstream to send it on", l.Name)
			}
			if err := validateStreamShape(s.Session.Stream); err != nil {
				return err
			}
			stream = s.Session.Stream
		case !canListen(s.Transport):
			return fmt.Errorf("driver: listener %q rides %q, which has no listen path (a listener runs over a held line session, a udp socket or an http stream), so no node would run it", l.Name, s.Transport)
		}
		for i, a := range l.Arm {
			if strings.ContainsAny(a, "\r\n") {
				return fmt.Errorf("driver: listener %q arm %d carries a line break; each arm is sent as one line (one frame over a websocket), so write it compact", l.Name, i+1)
			}
		}
		if err := validateListenerMatch(fmt.Sprintf("listener %q", l.Name), l.Match, s.Transport, stream); err != nil {
			return err
		}
		if err := validateEmits("listener", l.Name, l.Emits, cat); err != nil {
			return err
		}
		if stream != nil {
			for _, e := range l.Emits {
				if err := checkHTTPEmit(fmt.Sprintf("listener %q emit %q", l.Name, e.Name), e.Extract, *stream); err != nil {
					return err
				}
			}
		}
	}
	if err := s.validateTLSInputs(); err != nil {
		return err
	}
	if err := s.validateUDPInputs(); err != nil {
		return err
	}
	if err := s.validateSession(); err != nil {
		return err
	}
	if err := s.validateLadder(); err != nil {
		return err
	}
	// A held session is held for an endpoint's poll and listen tasks; command
	// bindings derive none. Commands that ride only a held session (the driver
	// declares one, or the transport has no stateless line) need a poll or a
	// listener to hold it, or they could never run (#1049).
	if IsHTTP(s.Transport) && len(s.Polls) == 0 && len(s.Listeners) == 0 && s.Session.IsCredentialed() {
		// A credential reaches a node only with a task (#902): a credentialed
		// http driver with commands alone could never authenticate one.
		return fmt.Errorf("driver: a credentialed http driver needs a poll or a listener to carry its credential to the node; its commands alone derive no task")
	}
	if !IsHTTP(s.Transport) && len(s.Polls) == 0 && len(s.Listeners) == 0 && (s.Session != nil || (isLineTransport(s.Transport) && s.Transport != "tcp")) {
		return fmt.Errorf("driver: commands that ride only a held session need a poll or a listener to hold it")
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
		// Only the line-protocol actuation wire exists today (over a line
		// transport, or as a datagram over udp, #1037), so a binding on a
		// transport it cannot reach refuses at write rather than dispatching a
		// request no node can send.
		if !isLineTransport(s.Transport) && s.Transport != "udp" && !IsHTTP(s.Transport) {
			return fmt.Errorf("driver: command binding %q rides transport %q, which has no actuation wire (only the line transports, udp and http actuate today)", c.CommandType, s.Transport)
		}
		if err := validateRetries(s.Transport, fmt.Sprintf("command binding %q", c.CommandType), c.Request); err != nil {
			return err
		}
		what := fmt.Sprintf("command binding %q", c.CommandType)
		if IsWebSocket(s.Transport) {
			if err := validateFrameRequest(what, c.Request); err != nil {
				return err
			}
			if err := validateFrameSettle(c); err != nil {
				return err
			}
			if err := validateReply(s.Transport, what, c.Request); err != nil {
				return err
			}
			refs, err := httpTemplateRefs(c.Request)
			if err != nil {
				return fmt.Errorf("driver: %s %v", what, err)
			}
			for _, tpl := range append(refs, argRefs(c.Request.Line)...) {
				if tpl != "value" && !strings.HasPrefix(tpl, "arg.") {
					return fmt.Errorf("driver: command binding %q references unknown template field %q", c.CommandType, tpl)
				}
			}
			continue
		}
		if err := validateHTTPFieldsOff(s.Transport, what, c.Request); err != nil {
			return err
		}
		if IsHTTP(s.Transport) {
			if err := validateHTTPRequest(what, c.Request, true, s.Session); err != nil {
				return err
			}
			if err := validateSettle(c, true); err != nil {
				return err
			}
			continue
		}
		if c.Request.Line == "" {
			return fmt.Errorf("driver: command binding %q on %s needs a line request", c.CommandType, s.Transport)
		}
		if err := validateSettle(c, false); err != nil {
			return err
		}
		if err := validateReply(s.Transport, fmt.Sprintf("command binding %q", c.CommandType), c.Request); err != nil {
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
	if err := s.validateSNMPCommunity(); err != nil {
		return err
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
		// A reserved key (a pin, the gate, a held session's account of
		// itself, a verdict or a probe timing, collection-failed, health) has
		// one writer, never a driver's emits: emitted by a driver it would land
		// as the observed value, and for a pin the worklist would deliver it
		// back as the pin (#1035 review). The writer-class ruling decides it
		// (ADR-0161), the one the push lane and the gateway read.
		if err := emittable(em.Name); err != nil {
			return fmt.Errorf("driver: %s %q may not emit %w", family, fn, err)
		}
		if cat != nil {
			if _, ok := cat.LaneOf(em.Name); !ok {
				return fmt.Errorf("driver: %s %q emits %q, which no catalog holds", family, fn, em.Name)
			}
			if c := cat.WriterClass(em.Name); c != writerclass.Collected {
				return fmt.Errorf("driver: %s %q may not emit %q: %s", family, fn, em.Name, writerclass.Refusal(c, em.Name, writerclass.Node, ""))
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

// emittable refuses a name a driver may never emit, whatever the catalog
// holds (ADR-0161): a reserved key (a native probe key, collection-failed, a
// platform key) has one writer, and it is never a driver's function. The spec
// write and the node's interpreter both ask, so a stored spec or a task
// derived before the refusal existed still cannot land one.
func emittable(name string) error {
	if c, ok := writerclass.Reserved(name); ok {
		return fmt.Errorf("%q: %s", name, writerclass.Refusal(c, strings.TrimSpace(name), writerclass.Push, ""))
	}
	return nil
}

func validateExtract(fn string, em Emit) error {
	set := 0
	for _, v := range []string{em.Extract.OID, em.Extract.Regex, em.Extract.JSONPath, em.Extract.Key, em.Extract.XPath} {
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
// rides, so an uninterpretable combination refuses at write. snmp, udp, http,
// https and the line transports have wired poll fetchers (internal/collection);
// a poll on any other transport refuses rather than faulting every tick.
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
	case "http", "https":
		// The request is checked whole by validateHTTPRequest; here each
		// emit's selector is bound to the response's declared format.
		for _, em := range p.Emits {
			if err := checkHTTPEmit(fmt.Sprintf("http poll %q emit %q", p.Name, em.Name), em.Extract, p.Request); err != nil {
				return err
			}
		}
	case "udp":
		// A datagram carries one line (#1037): the request is a line, and an
		// answer is text a regex reads.
		if p.Request.Line == "" {
			return fmt.Errorf("driver: udp poll %q needs a line request (the datagram it sends)", p.Name)
		}
		for _, em := range p.Emits {
			if em.Extract.OID != "" || em.Extract.Key != "" || em.Extract.JSONPath != "" || em.Extract.XPath != "" {
				return fmt.Errorf("driver: udp poll %q emit %q must locate by regex (a datagram's line carries no keyed or json body)", p.Name, em.Name)
			}
		}
	case "websocket", "wss":
		// A frame is a line or a JSON document: a regex reads either, a
		// jsonpath a JSON one.
		for _, em := range p.Emits {
			if em.Extract.OID != "" || em.Extract.Key != "" || em.Extract.XPath != "" {
				return fmt.Errorf("driver: websocket poll %q emit %q locates by jsonpath or regex (a frame carries no oid, key or markup)", p.Name, em.Name)
			}
		}
	case "tcp", "tls", "telnet", "ssh":
		if !isLineTransport(tr) {
			return fmt.Errorf("driver: poll %q rides transport %q, which has no poll fetcher yet", p.Name, tr)
		}
		if p.Request.Line == "" {
			return fmt.Errorf("driver: %s poll %q needs a line request", tr, p.Name)
		}
		for _, em := range p.Emits {
			// A line response is text: only a regex extractor can read it. An
			// emit that names an oid, key, or jsonpath (the keyed and json forms)
			// would validate clean yet fault every tick, so refuse it here. An
			// emit with no extractor at all is the general "exactly one extractor"
			// refusal's job, not this one.
			if em.Extract.OID != "" || em.Extract.Key != "" || em.Extract.JSONPath != "" || em.Extract.XPath != "" {
				return fmt.Errorf("driver: %s poll %q emit %q must locate by regex (a line response carries no keyed or json body to read an oid, key, jsonpath or xpath from)", tr, p.Name, em.Name)
			}
		}
	default:
		return fmt.Errorf("driver: poll %q rides transport %q, which has no poll fetcher (only snmp, udp, http, https and the line transports poll today)", p.Name, tr)
	}
	return nil
}

func validateMatch(what string, m Match) error {
	if m.Event != "" || m.Field != nil {
		return fmt.Errorf("driver: %s names an event or a field, which only a listener's match reads; declare a prefix or a regex", what)
	}
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
