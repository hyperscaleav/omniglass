// Package sim implements the device simulation engine.
//
// It manages a collection of simulated devices, each with its own state,
// per-protocol listeners, and template-based response rendering. Every
// interface declared in the device profile (HTTP, SNMP, SSH, Telnet, UDP)
// gets its own listener; unknown protocols are skipped with a log line.
package sim

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// requestBodyLimit caps how much of an inbound request body we will
// parse into .Request.Body. Lab requests are small JSON control
// payloads; this is defense against a misbehaving client, not a real
// security boundary.
const requestBodyLimit = 1 << 20 // 1 MiB

// RequestLog describes a single inbound request served by a simulated
// device. Emitted through Device.Logger (if set) for the sim run mode's
// hit reporter to consume. Protocol tags the listener that
// observed the request ("http", "ssh", "telnet"); the HTTP listener is
// the default when empty for backward compatibility with older loggers.
type RequestLog struct {
	DeviceID string
	Hostname string
	Protocol string
	Port     int
	Method   string
	Path     string
	// Remote is the raw peer address ("host:port") at the listener.
	// Resolving it to an operator-facing source label is left to the
	// hit consumer.
	Remote string
	// LocalAddr is the listener's raw address ("host:port") that
	// answered this request, so a consumer can key on the full
	// 4-tuple. Optional for protocols that don't bind a stable local
	// address.
	LocalAddr string
	Status    int
	// ConsoleUser is the value of the X-Omniglass-Console-User header
	// when the inbound HTTP request carried one. The console attaches
	// it on every outbound request, identifying the user the request
	// is being made on behalf of. A consumer can prefer it over the
	// address-derived label ("console:<user>") so events show the
	// human-facing source rather than the transport-layer agent.
	// Empty for non-HTTP protocols and for HTTP requests that didn't
	// route through the console.
	ConsoleUser string
	DurationMs  int64

	// Forensic details surfaced behind the console's click-to-expand
	// log entry. Optional for any protocol; HTTP populates request /
	// response bodies + headers + query params, other protocols may
	// add what they know. Bodies are captured up to a per-protocol
	// cap (see captureBody) so a multi-MB upload can't pin memory
	// even though the wire path streams fine. Carried to the hit
	// consumer for display as event details.
	Query       url.Values
	ReqHeaders  http.Header
	ReqBody     []byte
	RespHeaders http.Header
	RespBody    []byte
	BodiesTrunc bool // request or response body exceeded the capture cap
}

// RequestLogger receives a RequestLog per request. Implementations must not
// block the caller; if they need to buffer, they should do it internally.
type RequestLogger func(RequestLog)

// StateChange describes a batch of path-keyed writes the simulator just
// applied locally. DiffPublisher receives one per successful mutate
// batch so a consumer can mirror the simulator's in-memory state.
type StateChange struct {
	DeviceID string
	Version  int64
	Diffs    map[string]any
	// Cause attributes the change to its trigger so the console
	// can surface "what caused this" in the SOURCE column.
	// Conventional values:
	//   "behavior:<name>"   - autonomous time-driven behavior
	//                         (e.g. behavior:uptime_counter)
	//   "route:<method> <path>" - HTTP route mutation
	//                         (e.g. route:POST /api/volume)
	//   "command:<cmd>"     - SSH / Telnet command mutation
	//                         (e.g. command:set volume 75)
	//   "operator:<user>"   - operator-initiated via /state PUT
	//   ""                  - unknown (legacy callers)
	Cause string
}

// DiffPublisher receives one StateChange per successful mutate batch.
// Implementations must not block the caller; buffering is the
// publisher's responsibility. A nil publisher is a no-op.
type DiffPublisher func(StateChange)

// RequestEvent is the simulator's view of an inbound application-layer
// request. Publishers (HTTP / SNMP / future Telnet+SSH non-session
// probes) hand one of these to RequestPublisher per inbound request,
// for the sim run mode's hit reporter to consume. The simulator knows
// the app-protocol, the operation, the requested keys, and the values
// returned, so an event log can show forensic detail rather than just
// "DATAGRAM udp:161 → 200".
//
// Method follows the per-protocol convention:
//   - HTTP: actual method (GET, POST, PUT, ...)
//   - SNMP: GET / GETNEXT / SET / WALK
//   - probe-only: CONNECT
//
// Details is opaque per protocol; populate what the operator would
// want to forensically inspect, body, headers, OID list, response
// values. The console renders this behind click-to-expand.
type RequestEvent struct {
	DeviceID string
	Hostname string
	Protocol string
	Method   string
	Port     int
	Path     string
	Status   int
	// Remote is the raw peer address. Set by callers that know the
	// peer (SSH/Telnet sessions know via net.Conn); left empty by
	// callers that don't (gosnmp's OnGet hides the peer). Source
	// labelling is left to the hit consumer.
	Remote string
	// LocalAddr is the listener's raw bind address ("host:port") that
	// answered this request, the only locator available when Remote
	// is empty. Optional, callers that already know Remote can leave
	// LocalAddr empty.
	LocalAddr  string
	DurationMs int64
	Details    map[string]any
}

// RequestPublisher receives one RequestEvent per inbound application-
// layer request. Implementations must not block the caller; buffer
// upstream. Nil is a no-op (request runs as before, no event emitted).
type RequestPublisher func(RequestEvent)

// HTTPAuthMode names a supported HTTP auth scheme. Empty string means
// no auth: every request is accepted, matching the legacy behavior of
// every device profile that did not declare an HTTP gate.
type HTTPAuthMode string

const (
	// HTTPAuthNone is the default: ServeHTTP accepts all requests. Used
	// when a profile omits the auth section, declares no enforcement,
	// or sets type to anything outside the supported modes.
	HTTPAuthNone HTTPAuthMode = ""

	// HTTPAuthBasic enforces RFC 7617 HTTP Basic auth. A failed
	// challenge returns 401 with WWW-Authenticate so a browser shows
	// the native credential prompt.
	HTTPAuthBasic HTTPAuthMode = "basic"

	// HTTPAuthHeader enforces a header-based API key. A missing or
	// mismatched header returns 401 with a JSON error body. The
	// request header name is configurable; default "X-API-Key".
	HTTPAuthHeader HTTPAuthMode = "header"
)

// HTTPAuth holds the per-device HTTP auth gate. Zero value (Mode = "")
// disables the gate; any other Mode requires the listed credentials to
// match. Realm is sent in WWW-Authenticate challenges; HeaderName
// applies to HTTPAuthHeader and defaults to "X-API-Key".
//
// PublicPaths lists exact request paths the gate should let through
// regardless of credentials. Used by header-auth devices to expose a
// dashboard or token-acquisition page that JavaScript can call after
// to populate Authorization for the rest of the API. Empty means
// every path is gated.
type HTTPAuth struct {
	Mode        HTTPAuthMode
	User        string
	Password    string
	APIKey      string
	HeaderName  string
	Realm       string
	PublicPaths []string
}

// Device represents a single simulated device with per-protocol listeners.
type Device struct {
	DeviceID    string
	Hostname    string
	LoopbackIP  net.IP
	Port        int // primary HTTP port, populated by StartHTTP
	ProfileName string
	Version     int64

	// Logger receives one RequestLog per inbound request. Nil is safe.
	Logger RequestLogger

	// DiffPublisher receives a StateChange per successful mutate batch.
	// Nil is safe (mutations still apply locally; they just aren't
	// shipped upstream).
	DiffPublisher DiffPublisher

	// RequestPublisher receives one RequestEvent per inbound app-layer
	// request (HTTP, SNMP, …). Nil is safe, the listener still serves
	// requests as before, observation just isn't surfaced. The sim run
	// mode's hit reporter consumes this.
	RequestPublisher RequestPublisher

	// OIDMap is populated from the profile's SNMP interface. Used by the
	// SNMP server to resolve incoming OIDs to state paths.
	OIDMap map[string]string

	// HTTPAuth gates the HTTP listener. Zero value = no gate.
	HTTPAuth HTTPAuth

	// now is the clock source. Defaults to time.Now in NewDevice; tests
	// swap it via SetClock to make elapsed computed fields deterministic.
	now func() time.Time

	mu    sync.RWMutex
	state map[string]any

	// defaults is a read-only snapshot of the profile's `defaults:` block,
	// installed by the engine via SetDefaults after NewDevice returns.
	// Templates that want to reset a state path to its declared default
	// reach it via `{{ .Defaults.status.device.uptime }}` on the
	// templateData passed into every render. Read-only by convention: the
	// simulator never writes here, only the loader sets it once.
	defaults map[string]any

	routes   []routeEntry
	commands []commandEntry

	// behaviorMu guards the behaviors slice, the overrides slice, and
	// every entry's stop/done channels. Separate from mu (which guards
	// state) so a long applyMutations under mu doesn't block
	// StartBehaviors / StopBehaviors / SetBehaviorOverrides.
	behaviorMu sync.Mutex
	behaviors  []behaviorEntry
	// overrides are time-bounded mutations applied externally via
	// SetBehaviorOverrides (a future control path, e.g. impairments,
	// issue #889). Each
	// override runs its own goroutine on its declared interval (see
	// override.go). Profile-declared behaviors targeting an overlapping
	// path skip the write so the override wins; the rest of the
	// profile behavior continues to fire normally.
	overrides []overrideEntry
	// computed holds profile-declared read-derived fields (e.g. uptime as
	// elapsed-since-boot). Unlike behaviors, these never run a goroutine
	// and never emit a StateChange, they are resolved on every read by
	// computedLocked / resolvedStateCopyLocked. Installed once at startup
	// via AddComputedFromProfile, read-only thereafter (so no separate
	// lock is needed for the slice; reads of d.state still take d.mu).
	computed []computedEntry
	// rand is the per-device math/rand source used by the behavior
	// runner's randStep helper. Lazily initialized on the first
	// AddBehaviorFromProfile call so non-behavior tests don't pay for
	// it. Seeded from the device id so a multi-device fleet produces
	// deterministic, individually phased random walks across runs.
	rand *deviceRand

	// One listener per protocol. Future stages populate snmp/ssh/telnet/udp.
	httpMu         sync.Mutex
	httpServers    []*http.Server // one per declared http interface
	httpAddrs      []net.Addr
	snmpServer     snmpShutdowner // *GoSNMPServer.SNMPServer, kept as interface so device.go doesn't import the server package
	sshListener    net.Listener
	telnetListener net.Listener
	tcpMu          sync.Mutex
	tcpListeners   []net.Listener // raw TCP, persistent-session-only (no one-shot equivalent), one per interface
	udpConn        net.PacketConn

	// Persistent-session machinery. Populated by the engine when an
	// interface is declared with persistent: true. The SSH registry
	// binds to the SSH `shell` channel handler; the Telnet registry
	// binds to the telnet command loop after auth; the TCP registry
	// binds to the raw-TCP listener (no auth, no handshake, every
	// accept hands directly to runShellSession). Nil means the
	// corresponding listener stays in legacy one-shot mode (or, for
	// raw TCP, declines to serve).
	sshSession    *SessionRegistry
	telnetSession *SessionRegistry
	tcpSessions   []*SessionRegistry // one per persistent tcp interface
}

// applyExternalDiffs writes an externally applied diff batch into the
// device's local state, advances Version, and runs the per-device
// session fanout so any subscribed shells see the change. It is the
// entry point for external writes (a future control path, e.g.
// impairments, issue #889), where someone other than the device wrote
// to its state.
//
// Unlike the HTTP-route and command mutate paths, this does not
// re-publish via DiffPublisher: the diff already came from outside,
// re-publishing would create an echo loop.
func (d *Device) applyExternalDiffs(diffs map[string]any, newVersion int64) error {
	if len(diffs) == 0 {
		return nil
	}
	d.mu.Lock()
	for path, value := range diffs {
		if err := SetPath(d.state, path, value); err != nil {
			d.mu.Unlock()
			return fmt.Errorf("setting %q: %w", path, err)
		}
	}
	d.Version = newVersion
	d.fanoutToSessions(diffs)
	d.mu.Unlock()
	return nil
}

// CommandList returns the match strings of every command currently
// registered on the device. Used by the persistent-shell loop's
// built-in `help` verb to enumerate what an operator can type.
// Order matches registration order.
func (d *Device) CommandList() []string {
	out := make([]string, 0, len(d.commands))
	for _, c := range d.commands {
		out = append(out, c.Match)
	}
	return out
}

// fanoutToSessions delivers a mutate batch's diffs to every active
// session subscription on this device. Caller MUST hold the device's
// write lock; the registry's Fanout reads from `diffs` and the
// session Out channel without re-entering Device methods.
func (d *Device) fanoutToSessions(diffs map[string]any) {
	if d.sshSession != nil {
		d.sshSession.Fanout(diffs)
	}
	if d.telnetSession != nil {
		d.telnetSession.Fanout(diffs)
	}
	for _, r := range d.TCPSessions() {
		r.Fanout(diffs)
	}
}

// AttachSSHSession installs a SessionRegistry on the SSH listener,
// flipping it from exec-only to persistent shell mode. Call before
// StartSSH so the first inbound channel finds the registry. Idempotent;
// passing nil unbinds the registry.
func (d *Device) AttachSSHSession(r *SessionRegistry) { d.sshSession = r }

// AttachTelnetSession installs a SessionRegistry on the Telnet listener,
// flipping it from one-shot to persistent shell mode. Same lifecycle
// rules as AttachSSHSession.
func (d *Device) AttachTelnetSession(r *SessionRegistry) { d.telnetSession = r }

// AttachTCPSession adds a raw-TCP session registry, one per persistent
// tcp interface (a device may declare several, each on its own port with
// its own framing and commands). Raw TCP has no fallback "one-shot" mode:
// a listener exists solely to serve its registry's sessions. Passing nil
// unbinds them all.
func (d *Device) AttachTCPSession(r *SessionRegistry) {
	d.tcpMu.Lock()
	defer d.tcpMu.Unlock()
	if r == nil {
		d.tcpSessions = nil
		return
	}
	d.tcpSessions = append(d.tcpSessions, r)
}

// SSHSession returns the SSH session registry (may be nil).
func (d *Device) SSHSession() *SessionRegistry { return d.sshSession }

// TelnetSession returns the Telnet session registry (may be nil).
func (d *Device) TelnetSession() *SessionRegistry { return d.telnetSession }

// TCPSession returns the first raw-TCP session registry (may be nil).
func (d *Device) TCPSession() *SessionRegistry {
	if rs := d.TCPSessions(); len(rs) > 0 {
		return rs[0]
	}
	return nil
}

// TCPSessions returns every raw-TCP session registry, in attach order.
func (d *Device) TCPSessions() []*SessionRegistry {
	d.tcpMu.Lock()
	defer d.tcpMu.Unlock()
	return append([]*SessionRegistry(nil), d.tcpSessions...)
}

// snmpShutdowner is satisfied by GoSNMPServer.SNMPServer. Interface kept
// here so device.go's core types don't drag in the SNMP server package.
type snmpShutdowner interface {
	Shutdown()
}

// httpBodyCaptureCap caps how many bytes of an HTTP request or
// response body get retained for the forensic log entry. The wire
// path streams unbounded, this only bounds in-memory capture per
// request so a multi-MB upload / download can't pin RAM. 64KB is
// generous for typical AV-device API responses (state JSON, command
// echos) and small enough that 1000 in-flight requests = ~64MB
// worst-case capture, well within simulator headroom.
const httpBodyCaptureCap = 64 * 1024

// httpBodyTotalCap is the absolute ceiling on how many bytes
// captureRequestBody will read off the wire. Beyond this we stop
// reading entirely, the route handler still gets a body, but it's
// the truncated prefix. Protects against a client that streams an
// unbounded body just to exhaust memory; legitimate AV API bodies
// are well under 1MB.
const httpBodyTotalCap = 1 * 1024 * 1024

// captureRequestBody reads up to httpBodyTotalCap bytes off r.Body,
// retains the first httpBodyCaptureCap for the forensic log, and
// resets r.Body to a fresh reader over the full captured slice so
// route handlers downstream can still read it. Returns the captured
// prefix and a bool flag set when capture or read was truncated.
//
// Errors mid-read collapse to a no-op: r.Body stays as-is, captured
// is nil, trunc is false. The route handler will surface the error
// itself when it tries to read.
func captureRequestBody(r *http.Request) ([]byte, bool) {
	if r == nil || r.Body == nil || r.Body == http.NoBody {
		return nil, false
	}
	limited := io.LimitReader(r.Body, httpBodyTotalCap+1)
	buf, err := io.ReadAll(limited)
	r.Body.Close()
	if err != nil {
		return nil, false
	}
	totalTrunc := len(buf) > httpBodyTotalCap
	if totalTrunc {
		buf = buf[:httpBodyTotalCap]
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))
	captureTrunc := len(buf) > httpBodyCaptureCap
	captured := buf
	if captureTrunc {
		cp := make([]byte, httpBodyCaptureCap)
		copy(cp, buf[:httpBodyCaptureCap])
		captured = cp
	} else {
		// Defensive copy so the route handler reading r.Body can't
		// race with the publisher reading captured.
		cp := make([]byte, len(buf))
		copy(cp, buf)
		captured = cp
	}
	return captured, totalTrunc || captureTrunc
}

// statusRecorder wraps http.ResponseWriter to capture the status code
// and (up to bodyCap bytes of) the response body for forensic logging.
// The wire write is unaffected: the wrapper forwards every byte to the
// underlying writer; the capture buffer is bounded so a multi-MB
// download can't pin memory even though the network path streams fine.
type statusRecorder struct {
	http.ResponseWriter
	status   int
	body     bytes.Buffer
	bodyCap  int  // max bytes to retain; 0 = capture everything
	bodyTrim bool // true once a Write was truncated
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	// Capture into the buffer up to bodyCap; pass the full slice
	// through to the underlying writer regardless. bodyCap=0 means
	// capture everything (used when the caller doesn't care about
	// memory bounds, e.g. tests).
	if r.bodyCap == 0 || r.body.Len() < r.bodyCap {
		room := len(b)
		if r.bodyCap > 0 {
			remain := r.bodyCap - r.body.Len()
			if room > remain {
				room = remain
				r.bodyTrim = true
			}
		}
		r.body.Write(b[:room])
		if r.bodyCap > 0 && len(b) > room {
			r.bodyTrim = true
		}
	} else {
		r.bodyTrim = true
	}
	return r.ResponseWriter.Write(b)
}

// routeEntry pairs a parsed route match with a compiled template and
// pre-compiled mutations.
type routeEntry struct {
	Method    string
	Path      string
	Template  *template.Template
	Mutations []mutation
}

// commandEntry pairs a CLI command match with a compiled template. Used
// by SSH and Telnet listeners to answer profile-declared Commands.
// Match is exact-only for v1, mirroring HTTP route behavior.
//
// Action / Subject / Token / IntervalMs back the persistent-session
// subscribe and unsubscribe verbs. They are zero-valued for ordinary
// commands; the shell loop only consults them when the interface is
// Persistent and Action is non-empty. Subject / Token / IntervalMs are
// pre-compiled templates rendered with the standard templateData
// (.State, .Hostname, .DeviceID, .IP) so a profile may parameterize
// the resolved subject by device identity even without per-call
// argument capture (which is a planned follow-up).
type commandEntry struct {
	Match      string
	Template   *template.Template
	Mutations  []mutation
	Action     string
	Subject    *template.Template
	Token      *template.Template
	IntervalMs *template.Template
}

// CommandAction names a persistent-session verb a command may declare.
// Empty (the default) means a normal request/response command.
type CommandAction string

const (
	// ActionNone is the default: ordinary req/res with no session
	// interaction.
	ActionNone CommandAction = ""

	// ActionSubscribe registers a subscription on the session. Subject
	// (rendered) is matched against the interface's Subjects whitelist
	// to resolve the state path and push line format.
	ActionSubscribe CommandAction = "subscribe"

	// ActionUnsubscribe drops a previously registered subscription with
	// the same rendered Subject string.
	ActionUnsubscribe CommandAction = "unsubscribe"
)

// CommandResult carries the outcome of a Device.RunCommand call. The
// shell loop reads Response unconditionally and writes it to the
// session; the remaining fields are consulted only when Action is
// non-empty (subscribe / unsubscribe), in which case the loop
// registers or removes the subscription against the per-device
// SessionRegistry.
//
// Token and IntervalMs are derived from the matched commandEntry's
// templates. IntervalMs is the parsed integer (0 means on-change
// only); a malformed template result returns an error from RunCommand
// so the caller can surface it on the wire instead of silently
// downgrading to on-change.
type CommandResult struct {
	Response   string
	Action     CommandAction
	Subject    string
	Token      string
	IntervalMs int
}

// templateData is the data passed to every template execution. The
// Request field is zero-valued for CLI command renders (SSH/Telnet)
// since those don't have an HTTP request context.
//
// Defaults is a read-only view of the profile's `defaults:` block,
// installed once at device load. Templates use it to reset volatile
// state paths back to their declared defaults, for example, a
// reboot mutation that writes `value: "{{ .Defaults.status.audio.volume }}"`
// returns the path to whatever the profile declared, without the
// author having to repeat the literal value at every site that resets it.
type templateData struct {
	State    map[string]any
	Defaults map[string]any
	Hostname string
	DeviceID string
	IP       string
	Request  requestContext
}

// requestContext exposes the current HTTP request to templates and
// mutations. Body is the JSON-parsed request body (nil on empty or
// non-JSON bodies); Query is the first value per query parameter.
type requestContext struct {
	Method string
	Path   string
	Body   map[string]any
	Query  map[string]string
}

// templateFuncs provides helper functions available in all templates.
var templateFuncs = template.FuncMap{
	// now returns the current Unix epoch in seconds. Used by reboot/reset
	// mutations to re-anchor an elapsed computed field:
	//   { path: status.device.boot_epoch, value: "{{ now }}" }
	// so uptime (computed as now - boot_epoch) drops back to ~0.
	"now": func() int64 { return time.Now().Unix() },
	"marshal": func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			return "{}"
		}
		return string(b)
	},
	// get retrieves a key from a map. Pipeline-friendly: {{ .State | get "status" | get "device" | get "power" }}
	"get": func(key string, m any) any {
		switch v := m.(type) {
		case map[string]any:
			return v[key]
		default:
			return nil
		}
	},

	// xor_checksum returns the XOR of every byte in s as a single
	// raw byte (string of length 1). Used by AV protocols like LG
	// `ka 01 00 11` where the trailing byte is the XOR of the
	// preceding command bytes. Compose into a response template:
	//   {{ printf "ka %s %s" .Args.id .Args.val }}{{ xor_checksum .Args.command }}\r
	"xor_checksum": func(s string) string {
		var x byte
		for i := 0; i < len(s); i++ {
			x ^= s[i]
		}
		return string([]byte{x})
	},

	// sum_byte_checksum_hex returns the modulo-256 byte sum of s as
	// a two-character uppercase hex string. Used by Sony BVM, NEC
	// display, and various Crestron-derived protocols.
	"sum_byte_checksum_hex": func(s string) string {
		var sum byte
		for i := 0; i < len(s); i++ {
			sum += s[i]
		}
		return fmt.Sprintf("%02X", sum)
	},

	// ascii_hex formats an integer as a fixed-width uppercase hex
	// string, padded with leading zeros. Used by NEC monitor IDs,
	// LG channel numbers, and any protocol that frames a numeric
	// field as ASCII hex (`%02X` etc). Example:
	//   {{ ascii_hex .Args.monitor_id 2 }} → "0F" for monitor_id=15
	"ascii_hex": func(v any, width int) string {
		switch x := v.(type) {
		case int:
			return fmt.Sprintf("%0*X", width, x)
		case int64:
			return fmt.Sprintf("%0*X", width, x)
		case float64:
			return fmt.Sprintf("%0*X", width, int(x))
		case string:
			// Allow string args from .Args.<name>, parse as int.
			n, err := strconv.ParseInt(strings.TrimSpace(x), 0, 64)
			if err != nil {
				return ""
			}
			return fmt.Sprintf("%0*X", width, n)
		}
		return ""
	},

	// hex_byte returns a single raw byte from a 2-character hex
	// pair. Useful for embedding STX (02), ETX (03), or vendor
	// opcodes in inline response templates without YAML escape
	// gymnastics: `{{ hex_byte "02" }}` → "\x02".
	"hex_byte": func(pair string) string {
		s := strings.TrimSpace(pair)
		if len(s) != 2 {
			return ""
		}
		decoded, err := decodeHexBytes(s)
		if err != nil {
			return ""
		}
		return decoded
	},

	// byte_of returns a single raw byte holding the low 8 bits of an
	// integer value, for binary AV protocols whose response frames carry
	// a numeric field as one raw byte (not ASCII). The Tessellate Census
	// hex control band uses it to pack people_count into the count byte
	// of its `AA 81 <count>` response frame:
	//   {{ byte_of .State.status.occupancy.people_count }}
	// Accepts int/int64/float64/string (parsed). Out-of-range values are
	// masked to their low byte (0-255); a non-numeric string yields a NUL
	// byte rather than an error, keeping the frame fixed-width.
	"byte_of": func(v any) string {
		var n int64
		switch x := v.(type) {
		case int:
			n = int64(x)
		case int64:
			n = x
		case float64:
			n = int64(x)
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(x), 0, 64)
			if err != nil {
				return string([]byte{0})
			}
			n = parsed
		default:
			return string([]byte{0})
		}
		return string([]byte{byte(n)})
	},

	// round and sigfig are exposed to route response templates as
	// well as behaviors so authors can format precision-sensitive
	// values inline (drift fields, sensor reads, computed ratios)
	// without leaking float64's full 16-digit precision into JSON
	// bodies. See behavior_funcs.go for the implementation.
	"round":  roundN,
	"sigfig": sigfigN,

	// b64enc standard-base64-encodes its argument. Used by the newtron
	// touch-panel web template to embed a value into the served HTML as
	// an opaque blob, so a plaintext `curl GET /` scrape can't read it;
	// only client JS that runs atob() in a real browser renders it into
	// the DOM. The web-driver-only teaching surface. Accepts any value
	// (stringified via fmt) so authors can pipe a state field straight
	// in: {{ b64enc (.State | get "configuration" | get "room" | get "config_version") }}.
	"b64enc": func(v any) string {
		var s string
		switch x := v.(type) {
		case nil:
			s = ""
		case string:
			s = x
		default:
			s = fmt.Sprintf("%v", x)
		}
		return base64.StdEncoding.EncodeToString([]byte(s))
	},
}

// NewDevice creates a simulated device from snapshot data.
func NewDevice(deviceID, hostname string, ip net.IP, profileName string, version int64, state map[string]any) *Device {
	return &Device{
		DeviceID:    deviceID,
		Hostname:    hostname,
		LoopbackIP:  ip,
		ProfileName: profileName,
		Version:     version,
		state:       state,
		now:         time.Now,
	}
}

// SetClock overrides the device's clock source. Tests use it to make
// elapsed computed fields deterministic; production never calls it, so
// d.now stays time.Now. Safe to call only before the device starts
// serving (the engine installs computed fields and starts listeners on a
// single goroutine).
func (d *Device) SetClock(now func() time.Time) {
	d.now = now
}

// clock returns the device's clock, defaulting to time.Now for devices
// constructed without going through NewDevice (older test fixtures).
func (d *Device) clock() time.Time {
	if d.now == nil {
		return time.Now()
	}
	return d.now()
}

// computedEntry is the compiled form of a profile.Computed field.
type computedEntry struct {
	Path  string
	Kind  string
	Since string
}

// AddComputedFromProfile registers a read-derived field and seeds its
// anchor. For kind:elapsed, if the Since path is absent or non-positive
// the anchor is set to "now" so a freshly-booted device reads ~0 uptime
// and counts up from there. The engine calls this once per device at
// startup, before StartBehaviors.
func (d *Device) AddComputedFromProfile(c *profile.Computed) error {
	entry := computedEntry{Path: c.Path, Kind: c.Kind, Since: c.Since}
	d.computed = append(d.computed, entry)

	if c.Kind == "elapsed" {
		d.mu.Lock()
		if toInt64(lookupStatePath(d.state, c.Since)) <= 0 {
			if err := SetPath(d.state, c.Since, d.clock().Unix()); err != nil {
				d.mu.Unlock()
				return fmt.Errorf("seeding computed anchor %q: %w", c.Since, err)
			}
		}
		d.mu.Unlock()
	}
	return nil
}

// computedLocked returns the resolved value for path if path names a
// declared computed field. Caller must hold d.mu (read or write).
func (d *Device) computedLocked(path string) (any, bool) {
	for i := range d.computed {
		if d.computed[i].Path == path {
			return d.evalComputedLocked(&d.computed[i]), true
		}
	}
	return nil, false
}

// evalComputedLocked computes one field's current value. Caller holds d.mu.
func (d *Device) evalComputedLocked(c *computedEntry) any {
	switch c.Kind {
	case "elapsed":
		epoch := toInt64(lookupStatePath(d.state, c.Since))
		if epoch <= 0 {
			return int64(0)
		}
		elapsed := d.clock().Unix() - epoch
		if elapsed < 0 {
			return int64(0)
		}
		return elapsed
	default:
		return int64(0)
	}
}

// ReadPath resolves a single state path, preferring a computed field over
// stored state. Used by scalar read surfaces (SNMP, session pumps) so
// they observe the same derived value as the snapshot.
func (d *Device) ReadPath(path string) any {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if v, ok := d.computedLocked(path); ok {
		return v
	}
	return lookupStatePath(d.state, path)
}

// resolvedStateCopyLocked returns a deep copy of state with every
// computed field overlaid at its path. Used by whole-map read surfaces
// (the snapshot and the HTTP/command render) so derived values appear
// under their natural keys without ever being written to real state.
// Caller holds d.mu.
func (d *Device) resolvedStateCopyLocked() map[string]any {
	s := deepCopy(d.state)
	for i := range d.computed {
		c := &d.computed[i]
		if err := SetPath(s, c.Path, d.evalComputedLocked(c)); err != nil {
			slog.Warn("overlaying computed field failed",
				"device", d.DeviceID, "path", c.Path, "error", err)
		}
	}
	return s
}

// SetDefaults installs the profile's `defaults:` snapshot on the device
// for templates to reach via `{{ .Defaults.<path> }}`. The engine calls
// this once after NewDevice returns; the value is treated as read-only
// thereafter. Kept off the NewDevice signature so adding the field
// doesn't ripple through every test fixture and call site.
func (d *Device) SetDefaults(defaults map[string]any) {
	d.defaults = defaults
}

// AddRoute registers an HTTP route with a compiled template and
// pre-compiled mutations from the legacy map shape only. Mutations run
// before the response template renders, so the response reflects
// post-mutation state. Use AddRouteFromProfile when the profile may
// declare the list-of-objects mutation shape (with enum / pattern
// validation).
func (d *Device) AddRoute(match, tmplPath string, tmplContent string, mutate map[string]any) error {
	return d.addRoute(match, tmplPath, tmplContent, mutate, nil)
}

// AddRouteFromProfile registers a route from a profile.Route. Accepts
// both the legacy map shape (route.Mutate) and the list-of-objects
// shape (route.Mutations); both are compiled into a single mutation
// list with list items running first. Pattern regex compile errors
// surface here so a malformed profile fails loudly at load time.
func (d *Device) AddRouteFromProfile(route *profile.Route, tmplContent string) error {
	return d.addRoute(route.Match, route.Template, tmplContent, route.Mutate, route.Mutations)
}

func (d *Device) addRoute(match, tmplPath, tmplContent string, mutate map[string]any, items []profile.Mutation) error {
	method, path, err := parseMatch(match)
	if err != nil {
		return err
	}

	tmpl, err := template.New(tmplPath).Funcs(templateFuncs).Parse(tmplContent)
	if err != nil {
		return fmt.Errorf("parsing template %q: %w", tmplPath, err)
	}

	muts, err := compileMerged(mutate, items)
	if err != nil {
		return fmt.Errorf("compiling mutations for %q: %w", match, err)
	}

	d.routes = append(d.routes, routeEntry{
		Method:    method,
		Path:      path,
		Template:  tmpl,
		Mutations: muts,
	})
	return nil
}

// State returns a snapshot of the device's current state, with any
// computed fields resolved into the copy so observers (a consumer of
// the diff stream's snapshot, for one) see derived values like uptime.
func (d *Device) State() map[string]any {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.computed) == 0 {
		return deepCopy(d.state)
	}
	return d.resolvedStateCopyLocked()
}

// SetState replaces the device's state (used for reconciliation).
func (d *Device) SetState(state map[string]any, version int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state = state
	d.Version = version
}

// ServeHTTP handles an incoming HTTP request by matching against routes.
func (d *Device) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, bodyCap: httpBodyCaptureCap}
	// Pass the raw peer address through to the logger; source
	// labelling is left to the hit consumer. We deliberately do NOT
	// read X-Omniglass-Client / similar self-identification headers
	// here, a self-reported identity would be a second source of truth
	// that can quietly mask a missing label.
	//
	// X-Omniglass-Console-User is the one exception: it carries the
	// human user the console is acting on behalf of, which no address
	// can reveal. A consumer can prefer "console:<user>" over the
	// address-derived label when this header is present.
	localAddr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), d.Port)
	consoleUser := r.Header.Get("X-Omniglass-Console-User")

	// Capture the request body up to httpBodyCaptureCap so the
	// publisher can attach it to the event's Details. Re-set r.Body
	// to a fresh reader over the captured bytes plus a tail of the
	// remainder so route handlers downstream can still read it.
	reqBodyCap, reqBodyTrim := captureRequestBody(r)

	defer func() {
		if d.Logger == nil {
			return
		}
		respBody := rec.body.Bytes()
		// Defensive copy: the Buffer's internal slice could be
		// re-used after this defer returns; the publisher path
		// runs in a goroutine.
		if len(respBody) > 0 {
			cp := make([]byte, len(respBody))
			copy(cp, respBody)
			respBody = cp
		}
		d.Logger(RequestLog{
			DeviceID:    d.DeviceID,
			Hostname:    d.Hostname,
			Protocol:    "http",
			Port:        d.Port,
			Method:      r.Method,
			Path:        r.URL.Path,
			Remote:      r.RemoteAddr,
			LocalAddr:   localAddr,
			Status:      rec.status,
			ConsoleUser: consoleUser,
			DurationMs:  time.Since(start).Milliseconds(),
			Query:       r.URL.Query(),
			ReqHeaders:  r.Header.Clone(),
			ReqBody:     reqBodyCap,
			RespHeaders: rec.Header().Clone(),
			RespBody:    respBody,
			BodiesTrunc: reqBodyTrim || rec.bodyTrim,
		})
	}()

	// Auth gate runs before route matching. A 401 here still gets
	// logged via the deferred RequestLog above, so operators see the
	// rejected attempts in the event stream alongside successful
	// requests. The gate is a no-op when HTTPAuth.Mode is empty, which
	// preserves the open-by-default behavior every existing profile
	// relies on.
	if !d.checkHTTPAuth(rec, r) {
		return
	}

	reqCtx := parseRequestContext(r)

	for _, route := range d.routes {
		if r.Method != route.Method || r.URL.Path != route.Path {
			continue
		}

		// Mutations and render both touch state. Hold the write lock
		// across both so a concurrent reader never observes a state
		// that has been mutated but not yet rendered from.
		d.mu.Lock()
		data := templateData{
			State:    d.state,
			Defaults: d.defaults,
			Hostname: d.Hostname,
			DeviceID: d.DeviceID,
			IP:       d.LoopbackIP.String(),
			Request:  reqCtx,
		}
		diffs, mutErrs := applyMutations(d.state, route.Mutations, data)
		for _, err := range mutErrs {
			slog.Warn("mutation failed",
				"device", d.DeviceID,
				"route", route.Method+" "+route.Path,
				"error", err,
			)
		}
		// Required mutation failures translate to 400 so a misbehaving
		// client sees the failure instead of a green 200 over silently
		// unchanged state. Mutations marked optional in the profile log
		// a warning but stay on the success path. When every required
		// failure happens to be a validation error the response uses
		// the validation-specific body shape (fields[]); otherwise it
		// falls back to the generic failed_mutations body.
		requiredErrs := collectRequiredErrors(mutErrs)
		if len(requiredErrs) > 0 {
			d.mu.Unlock()
			if validationErrs := collectValidationErrors(requiredErrs); len(validationErrs) == len(requiredErrs) {
				writeValidationError(rec, validationErrs)
			} else {
				writeMutationError(rec, requiredErrs)
			}
			return
		}
		if len(diffs) > 0 {
			d.Version++
			// Publish under the lock so concurrent writers can never
			// deliver diffs to the publisher out of version order, two
			// requests racing to Publish after unlock would let the
			// later one be received by the consumer first, causing
			// version conflicts and dropped diffs. Publishers are
			// required to be non-blocking so holding the lock costs
			// only the channel send.
			if d.DiffPublisher != nil {
				d.DiffPublisher(StateChange{
					DeviceID: d.DeviceID,
					Version:  d.Version,
					Diffs:    diffs,
					Cause:    "route:" + route.Method + " " + route.Path,
				})
			}
			d.fanoutToSessions(diffs)
		}

		// Render against resolved state so a response template that emits
		// a computed field (e.g. status.device.uptime) reflects the
		// derived value. Mutations above ran against real state; only the
		// render sees the overlay, and only when computed fields exist.
		if len(d.computed) > 0 {
			data.State = d.resolvedStateCopyLocked()
		}
		var rendered bytes.Buffer
		tmplErr := route.Template.Execute(&rendered, data)
		d.mu.Unlock()

		if tmplErr != nil {
			slog.Error("template execution failed",
				"device", d.DeviceID,
				"template", route.Template.Name(),
				"error", tmplErr,
			)
			http.Error(rec, "template error", http.StatusInternalServerError)
			return
		}

		rec.Header().Set("Content-Type", contentTypeFor(route.Template.Name()))
		rec.WriteHeader(http.StatusOK)
		_, _ = rec.Write(rendered.Bytes())
		return
	}

	// Liveness fallback for HEAD / GET on "/". A collector's HTTPS
	// service check completes the TCP+TLS
	// handshake and then sends `HEAD /` to confirm the server is
	// alive, without this fallback every probe logged HEAD / 404.
	// A real device's web admin would answer the root with a redirect
	// or splash page; we don't model that, so a bare 200 OK is the
	// cheapest correct behavior. Only kicks in when the device
	// profile didn't define its own "/" route.
	if r.URL.Path == "/" && (r.Method == http.MethodHead || r.Method == http.MethodGet) {
		rec.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rec.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = rec.Write([]byte("ok\n"))
		}
		return
	}

	http.NotFound(rec, r)
}

// collectRequiredErrors returns the subset of mutation errors whose
// underlying mutation was not marked optional in the profile. These are
// the failures that turn a request into a 400. Optional failures are
// excluded so the caller can still render the success template after
// logging them. Non-MutationError values are treated as required since
// they originate outside the per-mutation flag plumbing.
func collectRequiredErrors(errs []error) []error {
	var out []error
	for _, e := range errs {
		var me *MutationError
		if errors.As(e, &me) && me.Optional {
			continue
		}
		out = append(out, e)
	}
	return out
}

// collectValidationErrors returns the *ValidationError entries from a
// mutation-error slice in their original order. Non-validation errors
// are dropped. Used to detect the all-validation case so the response
// body can use the existing validation-specific fields[] shape.
func collectValidationErrors(errs []error) []*ValidationError {
	var out []*ValidationError
	for _, e := range errs {
		var v *ValidationError
		if errors.As(e, &v) {
			out = append(out, v)
		}
	}
	return out
}

// writeMutationError serializes a 400 response body listing every
// required mutation that failed at runtime. Used when at least one
// failure is not a validation error (e.g. path render failed, state
// write failed): the validation-specific body shape would lose the
// underlying error text, so this body shape carries it explicitly.
// Path falls back to the raw template source when the rendered path
// is empty (path-template render failure).
func writeMutationError(w http.ResponseWriter, errs []error) {
	type failure struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	type body struct {
		Error           string    `json:"error"`
		FailedMutations []failure `json:"failed_mutations"`
	}
	out := body{Error: "mutation failed", FailedMutations: make([]failure, 0, len(errs))}
	for _, e := range errs {
		f := failure{Error: e.Error()}
		var me *MutationError
		if errors.As(e, &me) {
			f.Path = me.Path
		}
		out.FailedMutations = append(out.FailedMutations, f)
	}
	buf, err := json.Marshal(out)
	if err != nil {
		http.Error(w, "mutation failed", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(buf)
}

// writeValidationError serializes a 400 response body listing every
// validation failure in the request. Operators reading the response see
// the path, the offending value, and what the value was checked against,
// so a misbehaving caller can fix its payload without server-log access.
func writeValidationError(w http.ResponseWriter, errs []*ValidationError) {
	type field struct {
		Path    string   `json:"path"`
		Value   string   `json:"value"`
		Enum    []string `json:"enum,omitempty"`
		Pattern string   `json:"pattern,omitempty"`
	}
	type body struct {
		Error  string  `json:"error"`
		Fields []field `json:"fields"`
	}
	out := body{Error: "validation failed", Fields: make([]field, 0, len(errs))}
	for _, e := range errs {
		out.Fields = append(out.Fields, field{
			Path:    e.Path,
			Value:   e.Value,
			Enum:    e.Enum,
			Pattern: e.Pattern,
		})
	}
	buf, err := json.Marshal(out)
	if err != nil {
		http.Error(w, "validation failed", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(buf)
}

// parseRequestContext extracts body and query into the struct exposed
// to templates. JSON-parse failures are silent: the template just sees
// a nil Body. The body is read into memory and replaced so any
// downstream handler still sees a readable body.
func parseRequestContext(r *http.Request) requestContext {
	ctx := requestContext{
		Method: r.Method,
		Path:   r.URL.Path,
	}

	q := r.URL.Query()
	if len(q) > 0 {
		ctx.Query = make(map[string]string, len(q))
		for k, vs := range q {
			if len(vs) > 0 {
				ctx.Query[k] = vs[0]
			}
		}
	}

	if r.Body == nil {
		return ctx
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, requestBodyLimit))
	_ = r.Body.Close()
	if err != nil || len(bytes.TrimSpace(buf)) == 0 {
		return ctx
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))

	var body map[string]any
	if err := json.Unmarshal(buf, &body); err == nil {
		ctx.Body = body
	}
	return ctx
}

func contentTypeFor(tmplName string) string {
	switch {
	case strings.HasSuffix(tmplName, ".xml.gotempl"):
		return "application/xml"
	case strings.HasSuffix(tmplName, ".json.gotempl"):
		return "application/json"
	case strings.HasSuffix(tmplName, ".txt.gotempl"):
		return "text/plain"
	case strings.HasSuffix(tmplName, ".gotempl"):
		return "text/html"
	default:
		return "application/json"
	}
}

// StartHTTP starts the device's HTTP listener on its loopback IP. When
// tlsCert is non-nil the listener serves HTTPS; otherwise it serves
// plain HTTP.
func (d *Device) StartHTTP(port int, tlsCert *tls.Certificate) error {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	d.Port = port

	srv := &http.Server{Handler: d,
		// The server's own complaints (a TLS port check that never shakes
		// hands, a client gone mid-request) are routine for a device a
		// collector probes: debug, not a line per probe.
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
		ConnState: func(c net.Conn, s http.ConnState) {
			if s == http.StateNew {
				d.logConnect(c)
			}
		}}
	d.httpMu.Lock()
	d.httpServers = append(d.httpServers, srv)
	d.httpAddrs = append(d.httpAddrs, lis.Addr())
	d.httpMu.Unlock()
	scheme := "http"
	if tlsCert != nil {
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*tlsCert}}
		scheme = "https"
	}

	go func() {
		slog.Debug("device HTTP started", "device", d.Hostname, "scheme", scheme, "addr", addr)
		var err error
		if tlsCert != nil {
			err = srv.ServeTLS(lis, "", "")
		} else {
			err = srv.Serve(lis)
		}
		if err != nil && !strings.Contains(err.Error(), "closed") {
			slog.Error("device HTTP error", "device", d.Hostname, "error", err)
		}
	}()
	return nil
}

// HTTPAddrs returns the bound address of each HTTP listener the device is
// serving, in start order.
func (d *Device) HTTPAddrs() []net.Addr {
	d.httpMu.Lock()
	defer d.httpMu.Unlock()
	return append([]net.Addr(nil), d.httpAddrs...)
}

// StopHTTP closes every HTTP listener the device started and the
// connections they hold, so a stopped device stops answering clients that
// kept a connection alive.
func (d *Device) StopHTTP() {
	d.httpMu.Lock()
	servers := d.httpServers
	d.httpServers, d.httpAddrs = nil, nil
	d.httpMu.Unlock()
	for _, srv := range servers {
		_ = srv.Close()
	}
}

// AddCommand registers a CLI command with a compiled template. Reused
// by SSH and Telnet listeners: the inbound line is matched exact-only
// against Command.Match, and the template is executed with the same
// templateData the HTTP handler uses (.State, .Hostname, .DeviceID, .IP).
// Accepts the legacy map shape only; use AddCommandFromProfile for the
// list-of-objects mutation shape.
func (d *Device) AddCommand(match, tmplPath, tmplContent string, mutate map[string]any) error {
	return d.addCommand(match, tmplPath, tmplContent, mutate, nil, "", "", "", "")
}

// AddCommandFromProfile registers a CLI command from a profile.Command.
// Accepts both the legacy map shape (cmd.Mutate) and the list-of-objects
// shape (cmd.Mutations). When cmd.Action is set, the resulting entry
// also captures the subscribe/unsubscribe metadata for the
// persistent-session shell loop.
//
// MatchHex (when present) overrides Match, the hex-pair string is
// decoded once at registration so persistent-shell dispatch can match
// against the resulting binary literal without escape-rule contortions
// in the YAML.
func (d *Device) AddCommandFromProfile(cmd *profile.Command, tmplContent string) error {
	match := cmd.Match
	if cmd.MatchHex != "" {
		decoded, err := decodeHexBytes(cmd.MatchHex)
		if err != nil {
			return fmt.Errorf("command %q: %w", cmd.MatchHex, err)
		}
		match = decoded
	}
	return d.addCommand(match, cmd.Template, tmplContent, cmd.Mutate, cmd.Mutations,
		cmd.Action, cmd.Subject, cmd.Token, cmd.IntervalMs)
}

// decodeHexBytes parses whitespace-separated hex pairs into a string.
// Useful for command matches that carry literal control bytes (STX,
// ETX, vendor-defined opcodes) which would otherwise need awkward
// YAML escapes. Whitespace between pairs is ignored; case is
// insensitive. Returns an error on odd-length tokens or non-hex
// characters so a typo fails loudly at profile load.
func decodeHexBytes(s string) (string, error) {
	cleaned := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
	if len(cleaned)%2 != 0 {
		return "", fmt.Errorf("match_hex must contain whole bytes (even number of hex chars), got %d", len(cleaned))
	}
	out := make([]byte, len(cleaned)/2)
	for i := 0; i < len(cleaned); i += 2 {
		hi, err := hexNybble(cleaned[i])
		if err != nil {
			return "", fmt.Errorf("match_hex pos %d: %w", i, err)
		}
		lo, err := hexNybble(cleaned[i+1])
		if err != nil {
			return "", fmt.Errorf("match_hex pos %d: %w", i+1, err)
		}
		out[i/2] = hi<<4 | lo
	}
	return string(out), nil
}

func hexNybble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, fmt.Errorf("not a hex digit: %q", c)
}

func (d *Device) addCommand(match, tmplPath, tmplContent string, mutate map[string]any, items []profile.Mutation,
	action, subjectTmpl, tokenTmpl, intervalTmpl string) error {
	tmpl, err := template.New(tmplPath).Funcs(templateFuncs).Parse(tmplContent)
	if err != nil {
		return fmt.Errorf("parsing template %q: %w", tmplPath, err)
	}
	muts, err := compileMerged(mutate, items)
	if err != nil {
		return fmt.Errorf("compiling mutations for %q: %w", match, err)
	}
	entry := commandEntry{
		Match:     match,
		Template:  tmpl,
		Mutations: muts,
		Action:    action,
	}
	if subjectTmpl != "" {
		entry.Subject, err = template.New(match + ".subject").Funcs(templateFuncs).Parse(subjectTmpl)
		if err != nil {
			return fmt.Errorf("parsing subject template for %q: %w", match, err)
		}
	}
	if tokenTmpl != "" {
		entry.Token, err = template.New(match + ".token").Funcs(templateFuncs).Parse(tokenTmpl)
		if err != nil {
			return fmt.Errorf("parsing token template for %q: %w", match, err)
		}
	}
	if intervalTmpl != "" {
		entry.IntervalMs, err = template.New(match + ".interval_ms").Funcs(templateFuncs).Parse(intervalTmpl)
		if err != nil {
			return fmt.Errorf("parsing interval_ms template for %q: %w", match, err)
		}
	}
	d.commands = append(d.commands, entry)
	return nil
}

// RunCommand looks up cmd against the device's registered commands and
// returns a CommandResult. ok=false means no matching command; callers
// decide how to respond (SSH sends exit-status 127, telnet writes an
// unknown-command banner). Exact match only. Mutations are applied
// before rendering so the command's response reflects the
// post-mutation state.
//
// When the matched commandEntry has a non-empty Action, the result's
// Action / Subject / Token / IntervalMs fields are populated for the
// caller to drive the per-session SessionRegistry. Subject / Token /
// IntervalMs templates are rendered against the same templateData the
// response template sees. A malformed IntervalMs (template renders to
// non-integer) returns an error so the caller can surface "-ERR" to
// the wire instead of silently downgrading to on-change.
func (d *Device) RunCommand(cmd string) (result CommandResult, ok bool, err error) {
	cmd = strings.TrimSpace(cmd)
	for _, ce := range d.commands {
		if ce.Match != cmd {
			continue
		}
		d.mu.Lock()
		data := templateData{
			State:    d.state,
			Defaults: d.defaults,
			Hostname: d.Hostname,
			DeviceID: d.DeviceID,
			IP:       d.LoopbackIP.String(),
		}
		diffs, mutErrs := applyMutations(d.state, ce.Mutations, data)
		for _, e := range mutErrs {
			slog.Warn("command mutation failed",
				"device", d.DeviceID,
				"command", cmd,
				"error", e,
			)
		}
		if len(diffs) > 0 {
			d.Version++
			if d.DiffPublisher != nil {
				d.DiffPublisher(StateChange{
					DeviceID: d.DeviceID,
					Version:  d.Version,
					Diffs:    diffs,
					Cause:    "command:" + cmd,
				})
			}
			d.fanoutToSessions(diffs)
		}

		// Render against resolved state so computed fields surface in the
		// command response (see the route render for rationale).
		if len(d.computed) > 0 {
			data.State = d.resolvedStateCopyLocked()
		}
		var buf bytes.Buffer
		execErr := ce.Template.Execute(&buf, data)
		// Render Action templates while still holding the lock so the
		// rendered Subject reflects the same state snapshot as the
		// response. Errors are returned with ok=true so the caller
		// can wire-format the failure rather than treating it as
		// "no such command".
		if execErr == nil && ce.Action != "" {
			result.Action = CommandAction(ce.Action)
			if ce.Subject != nil {
				var sub bytes.Buffer
				if subErr := ce.Subject.Execute(&sub, data); subErr != nil {
					d.mu.Unlock()
					return CommandResult{}, true, fmt.Errorf("rendering subject: %w", subErr)
				}
				result.Subject = strings.TrimSpace(sub.String())
			}
			if ce.Token != nil {
				var tok bytes.Buffer
				if tokErr := ce.Token.Execute(&tok, data); tokErr != nil {
					d.mu.Unlock()
					return CommandResult{}, true, fmt.Errorf("rendering token: %w", tokErr)
				}
				result.Token = strings.TrimSpace(tok.String())
			}
			if ce.IntervalMs != nil {
				var iv bytes.Buffer
				if ivErr := ce.IntervalMs.Execute(&iv, data); ivErr != nil {
					d.mu.Unlock()
					return CommandResult{}, true, fmt.Errorf("rendering interval_ms: %w", ivErr)
				}
				ivStr := strings.TrimSpace(iv.String())
				if ivStr != "" {
					n, parseErr := strconv.Atoi(ivStr)
					if parseErr != nil || n < 0 {
						d.mu.Unlock()
						return CommandResult{}, true, fmt.Errorf("interval_ms must be a non-negative integer, got %q", ivStr)
					}
					result.IntervalMs = n
				}
			}
		}
		d.mu.Unlock()
		if execErr != nil {
			return CommandResult{}, true, execErr
		}
		result.Response = buf.String()
		return result, true, nil
	}
	return CommandResult{}, false, nil
}

// parseMatch splits "GET /path" into method and path.
func parseMatch(match string) (string, string, error) {
	parts := strings.SplitN(match, " ", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid match %q: expected 'METHOD /path'", match)
	}
	return parts[0], parts[1], nil
}

// deepCopy makes a deep copy of a map[string]any.
func deepCopy(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = deepCopyValue(v)
	}
	return cp
}

func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopy(val)
	case []any:
		sl := make([]any, len(val))
		for i, item := range val {
			sl[i] = deepCopyValue(item)
		}
		return sl
	default:
		return v
	}
}
