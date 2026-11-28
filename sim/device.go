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
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/world"
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

	// HTTPAuthBearer enforces a bearer token: Authorization: Bearer
	// <APIKey> (#902). A missing or mismatched token returns 401 with a
	// WWW-Authenticate bearer challenge.
	HTTPAuthBearer HTTPAuthMode = "bearer"

	// HTTPAuthQuery enforces an API key carried as a query parameter
	// (#902), named by QueryName (default "apikey"), the shape some gear's
	// web API takes its key in.
	HTTPAuthQuery HTTPAuthMode = "query"

	// HTTPAuthForm is a form login (#1114): POST LoginPath with the user
	// and password as a form opens a session cookie and redirects home; a
	// request with no live session is redirected to LoginPath.
	HTTPAuthForm HTTPAuthMode = "form"

	// HTTPAuthToken is a JSON login (#1114): POST LoginPath with the user
	// and password as JSON answers a token and its lifetime, which every
	// later request carries as a bearer token.
	HTTPAuthToken HTTPAuthMode = "token"

	// HTTPAuthDigest is RFC 7616 digest (#1114): a request with no answer
	// is challenged with a nonce, which goes stale when the sessions are
	// expired (a nonce rotation) or a count is replayed.
	HTTPAuthDigest HTTPAuthMode = "digest"
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
	QueryName   string
	Realm       string
	PublicPaths []string
	// LoginPath is a form or token login's path (#1114); SessionTTL the
	// seconds a session lives (zero: until expired); Algorithm a digest
	// face's hash (MD5 or SHA-256); ExpirePath the path the console alone
	// fires to expire every session (and rotate the digest nonce), as a
	// component's reboot would.
	LoginPath  string
	SessionTTL int
	Algorithm  string
	ExpirePath string
}

// Device represents a single simulated device with per-protocol listeners.
type Device struct {
	DeviceID    string
	Hostname    string
	LoopbackIP  net.IP
	Port        int // primary HTTP port, populated by StartHTTP
	ProfileName string
	Product     string // the product it simulates, when its profile serves several
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

	// room is where the device sits and calSrc reaches the engine's lab
	// calendar (#2757); both stay zero on a device standing alone.
	room   calendar.Room
	calSrc func() *calendar.Calendar
	// worldSrc reaches the engine's world (#2773), nil on a device standing
	// alone.
	worldSrc func() *world.World
	// arbSrc reaches the engine's impairment arbiter (#2799), nil on a device
	// standing alone.
	arbSrc func() *Arbiter
	// memo is the engine's memo of room readings (#2797), nil on a device
	// standing alone, which keeps its own.
	memo        *roomMemo
	ownMemo     *roomMemo
	ownMemoOnce sync.Once

	mu    sync.RWMutex
	state map[string]any

	// defaults is a read-only snapshot of the profile's `defaults:` block,
	// installed by the engine via SetDefaults after NewDevice returns.
	// Templates that want to reset a state path to its declared default
	// reach it via `{{ .Defaults.status.device.uptime }}` on the
	// templateData passed into every render. Read-only by convention: the
	// simulator never writes here, only the loader sets it once.
	defaults map[string]any
	// setPaths are the state paths the profile's routes set from a request
	// body, in the order first declared; Faults reads them. Set while the
	// profile loads, before the device serves.
	setPaths []string

	routes   []routeEntry
	commands []commandEntry
	// fam is what the device's Go family answers in Go (#2829), nil for a
	// device standing alone (a test's).
	fam *goHandlers

	// The Go-declared surfaces (sim/surface, #2831), attached before the
	// device serves; see gosurface.go.
	goCommands []surface.Commander
	goRoutes   []surface.Route
	goOIDs     []surface.OID

	// behaviorMu guards the behaviors slice, the overrides slice, and
	// every entry's stop/done channels. Separate from mu (which guards
	// state) so a long applyMutations under mu doesn't block
	// StartBehaviors / StopBehaviors / SetBehaviorOverrides.
	behaviorMu sync.Mutex
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
	computed []spec.Elapsed
	// push is the profile's push face (push.go), nil when it declares none.
	push *pushFace
	// rand is the per-device math/rand source used by the behavior
	// runner's randStep helper. Lazily initialized on the first
	// AddBehaviorFromProfile call so non-behavior tests don't pay for
	// it. Seeded from the device id so a multi-device fleet produces
	// deterministic, individually phased random walks across runs.
	rand *deviceRand

	// observe counts what each face was sent and how it settled (see
	// observe.go); never nil for a device built by NewDevice.
	observe *deviceObservation
	// beforeRoute, when set, runs before an http request is routed: a
	// test holds a request in flight with it.
	beforeRoute func(*http.Request)

	// faces records the address each face the engine started bound, in
	// profile order (Faces).
	faceMu sync.Mutex
	faces  []Face

	// One listener per protocol. Future stages populate snmp/ssh/telnet/udp.
	httpMu        sync.Mutex
	httpServers   []*http.Server // one per declared http interface
	httpListeners []net.Listener // the sockets under httpServers, closed by StopHTTP itself
	httpAddrs     []net.Addr
	httpServing   sync.WaitGroup // one per running serve goroutine
	snmpServer    snmpShutdowner // the SNMP read loop, kept as an interface so device.go doesn't import the server package
	// snmpRefused is the community the SNMP face answers with
	// authorizationError (#821): one the agent knows and refuses.
	snmpRefused string
	sshListener net.Listener
	// tlsID is the key and certificate the device's tls line face serves
	// (#1035), derived from its id so it is the same on every run.
	tlsID tlsIdentity
	// httpRefusals counts the requests the device's auth gate refused
	// (401), so a test tells a gated collector (no credentialed request)
	// from a retrying one; the requests themselves are observed (observe).
	httpRefusals atomic.Int64
	// httpCredentialRefusals counts the refused requests that carried an
	// Authorization header (#1038): a collector's credentialed attempts,
	// beside the credential-free probes the gate refuses too.
	httpCredentialRefusals atomic.Int64
	// httpLogins counts the logins a form or token face was sent (#1114),
	// and logins the sessions they opened, by id to expiry, with the
	// digest face's nonce and the counts answered under it.
	httpLogins atomic.Int64
	logins     loginSessions
	// events is the http face's server-sent events stream (#1038), nil for
	// none.
	events *eventHub
	// ws is the device's websocket face (#1034), nil without one.
	ws *wsFace
	// httpDigestAnswers counts the requests that answered a digest face's
	// challenge, right or wrong.
	httpDigestAnswers atomic.Int64
	// httpsID is the key and certificate the device's https faces serve
	// (#902): an ECDSA key derived from its id, so it is the same on every
	// run and a pinned https endpoint survives a simulator restart.
	httpsID tlsIdentity
	// sshHostKey is the device's SSH host key, derived from its id on the
	// first StartSSH (#1370), so it is the same on every run, as a real
	// device keeps its key; a test rotates it to model a replaced or
	// reimaged device (#1032).
	sshHostKey ssh.Signer
	// telnetSilentReject makes the telnet login hang up on a wrong password
	// with no text (#1056).
	telnetSilentReject atomic.Bool
	// telnetLogins counts the logins the telnet face was sent.
	telnetLogins atomic.Int64
	// sshPasswordTries counts the passwords the ssh face was sent.
	sshPasswordTries atomic.Int64
	telnetListener   net.Listener
	tcpMu            sync.Mutex
	tcpListeners     []net.Listener // raw TCP, persistent-session-only (no one-shot equivalent), one per interface
	// udpMu guards the udp faces: the echo responders, the datagram faces
	// (#1037), and the datagram faces' session registries.
	udpMu       sync.Mutex
	udpConns    []net.PacketConn
	udpFaces    []*udpFace
	udpSessions []*SessionRegistry

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
	for _, c := range d.goCommands {
		out = append(out, c.Commands()...)
	}
	return out
}

// fanoutToSessions delivers a mutate batch's diffs to every active
// session subscription on this device. Caller MUST hold the device's
// write lock; the registry's Fanout reads from `diffs` and the
// session Out channel without re-entering Device methods.
func (d *Device) fanoutToSessions(diffs map[string]any) {
	if d.events != nil {
		d.events.fanout(diffs)
	}
	if d.ws != nil {
		d.ws.fanout(diffs)
	}
	if d.sshSession != nil {
		d.sshSession.Fanout(diffs)
	}
	if d.telnetSession != nil {
		d.telnetSession.Fanout(diffs)
	}
	for _, r := range d.TCPSessions() {
		r.Fanout(diffs)
	}
	for _, r := range d.UDPSessions() {
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
	writeErr bool // a write to the client failed: it went before the answer
	streamed bool // the request opened the event stream
}

// Flush passes a stream's flush through to the connection (#1038).
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
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
	n, err := r.ResponseWriter.Write(b)
	if err != nil {
		r.writeErr = true
	}
	return n, err
}

// routeEntry is a family's HTTP route: its method and path, answered by the
// family's Go handler.
type routeEntry struct {
	Method string
	Path   string
	Go     spec.HTTPHandler
}

// commandEntry is a family's line command, answered by its Go handler. A
// persistent session's subscribe and unsubscribe verbs come from the surface
// a family attaches (sim/surface), which reports the action in its reply.
type commandEntry struct {
	Match string
	Go    spec.LineHandler
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
// Token and IntervalMs come from the reply of the surface that answered.
// IntervalMs is the parsed integer (0 means on-change only).
type CommandResult struct {
	Response   string
	Action     CommandAction
	Subject    string
	Token      string
	IntervalMs int
}

// templateData is the data an override's expression renders over: the
// device's state and its identity. Defaults is a read-only view of the
// device's default state, so an expression can put a path back to what the
// family declared without repeating the literal.
type templateData struct {
	State    map[string]any
	Defaults map[string]any
	Hostname string
	DeviceID string
	IP       string
}

// requestContext is the current HTTP request, as the family's handler reads it. Body is the JSON-parsed request body (nil on empty or
// non-JSON bodies); Query is the first value per query parameter.
type requestContext struct {
	Method string
	Path   string
	Body   map[string]any
	Query  map[string]string
}

// templateFuncs are the helpers an override's expression has in every
// device: the clock and a path lookup. behaviorFuncs adds the numeric,
// scheduling and seeded-random helpers per device.
var templateFuncs = template.FuncMap{
	// now returns the current Unix epoch in seconds, so an expression can
	// re-anchor an elapsed field: "{{ now }}".
	"now": func() int64 { return time.Now().Unix() },
	// get retrieves a key from a map. Pipeline-friendly: {{ .State | get "status" | get "device" | get "power" }}
	"get": func(key string, m any) any {
		switch v := m.(type) {
		case map[string]any:
			return v[key]
		default:
			return nil
		}
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
		// A device standing alone observes into its own row; the engine
		// points it at the engine's before it serves (installSeams).
		observe: newEndpointObservation().device(deviceID, hostname),
	}
}

// SetClock overrides the device's clock source. Tests use it to make
// elapsed computed fields and the wall clock a behavior reads (#2754)
// deterministic; production never calls it, so
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

// AddElapsed registers a read-derived uptime and seeds its anchor: if the
// Since path is absent or non-positive the anchor is set to "now" so a
// freshly-booted device reads ~0 uptime and counts up from there. The engine
// calls this once per device at startup, before StartBehaviors.
func (d *Device) AddElapsed(e spec.Elapsed) error {
	d.computed = append(d.computed, e)
	d.mu.Lock()
	defer d.mu.Unlock()
	if toInt64(lookupStatePath(d.state, e.Since)) <= 0 {
		if err := SetPath(d.state, e.Since, d.clock().Unix()); err != nil {
			return fmt.Errorf("seeding computed anchor %q: %w", e.Since, err)
		}
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
func (d *Device) evalComputedLocked(c *spec.Elapsed) any {
	return c.At(toInt64(lookupStatePath(d.state, c.Since)), d.clock())
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
	d.serveHTTP(w, r, nil)
}

// httpFace is the handler of one http listener, so the observation knows
// which face a request reached.
type httpFace struct {
	d    *Device
	face *faceObservation
}

func (h httpFace) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.d.serveHTTP(w, r, h.face) }

// httpOutcome is how an http request settled: dropped when the client went
// before the answer was written, otherwise read from the status.
func httpOutcome(r *http.Request, rec *statusRecorder) Outcome {
	if rec.writeErr || r.Context().Err() != nil {
		return OutcomeDropped
	}
	return outcomeForStatus(rec.status)
}

func (d *Device) serveHTTP(w http.ResponseWriter, r *http.Request, face *faceObservation) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, bodyCap: httpBodyCaptureCap}
	// A request for the event stream is a request until it opens the
	// stream (serveEvents counts that); any other is one from here.
	stream := d.isEventStream(r)
	if !stream {
		tok := d.observe.begin(obsHTTP, face)
		defer func() { tok.end(httpOutcome(r, rec)) }()
	} else {
		defer func() {
			if !rec.streamed {
				d.observe.begin(obsHTTP, face).end(httpOutcome(r, rec))
			}
		}()
	}
	// Pass the raw peer address through to the logger; source
	// labelling is left to the hit consumer. We deliberately do NOT
	// read X-Omniglass-Client / similar self-identification headers
	// here, a self-reported identity would be a second source of truth
	// that can quietly mask a missing label.
	//
	// X-Omniglass-Console-User is the one exception: it carries the
	// person the console is acting on behalf of, which no address
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
	if stream {
		d.events.attempts.Add(1)
	}
	if d.answerLogin(rec, r) {
		return
	}
	if !d.checkHTTPAuth(rec, r) {
		d.httpRefusals.Add(1)
		if r.Header.Get("Authorization") != "" {
			d.httpCredentialRefusals.Add(1)
		}
		return
	}
	if stream {
		d.serveEvents(rec, r)
		return
	}
	if d.beforeRoute != nil {
		d.beforeRoute(r)
	}

	reqCtx := parseRequestContext(r)

	if d.serveGoRoute(rec, r) {
		return
	}

	for _, route := range d.routes {
		if r.Method == route.Method && r.URL.Path == route.Path {
			d.serveFamilyRoute(rec, r, route.Go, reqCtx)
			return
		}
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

// StartHTTP starts the device's HTTP listener on its loopback IP. When
// tlsCert is non-nil the listener serves HTTPS; otherwise it serves
// plain HTTP. Port 0 takes a free port from the kernel (HTTPAddrs reads it).
func (d *Device) StartHTTP(port int, tlsCert *tls.Certificate) error {
	_, err := d.startHTTP(port, tlsCert)
	return err
}

// startHTTPS binds one https listener under the device's https identity
// (#902) and returns the address it bound.
func (d *Device) startHTTPS(port int) (net.Addr, error) {
	cfg, err := d.httpsServerConfig(0)
	if err != nil {
		return nil, fmt.Errorf("https identity: %w", err)
	}
	return d.startHTTPWith(port, cfg)
}

// startHTTP binds and serves one HTTP listener and returns the address it
// bound; a certificate serves it behind TLS.
func (d *Device) startHTTP(port int, tlsCert *tls.Certificate) (net.Addr, error) {
	var cfg *tls.Config
	if tlsCert != nil {
		cfg = &tls.Config{Certificates: []tls.Certificate{*tlsCert}}
	}
	return d.startHTTPWith(port, cfg)
}

// startHTTPWith binds and serves one HTTP listener, behind TLS when cfg is
// set, and returns the address it bound.
func (d *Device) startHTTPWith(port int, tlsCfg *tls.Config) (net.Addr, error) {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	d.Port = boundPort(lis.Addr())
	addr = lis.Addr().String()

	face := d.observe.face(strconv.Itoa(d.Port), obsHTTP)
	srv := &http.Server{Handler: httpFace{d: d, face: face},
		// The server's own complaints (a TLS port check that never shakes
		// hands, a client gone mid-request) are routine for a device a
		// collector probes: debug, not a line per probe.
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
		// Each connection reaches StateNew once and then exactly one of
		// StateClosed or StateHijacked.
		ConnState: func(c net.Conn, s http.ConnState) {
			switch s {
			case http.StateNew:
				d.observe.connected(obsHTTP)
				d.logConnect(c)
			case http.StateClosed, http.StateHijacked:
				d.observe.disconnected(obsHTTP)
			}
		}}
	d.httpMu.Lock()
	d.httpServers = append(d.httpServers, srv)
	d.httpListeners = append(d.httpListeners, lis)
	d.httpAddrs = append(d.httpAddrs, lis.Addr())
	d.httpServing.Add(1)
	d.httpMu.Unlock()
	scheme := "http"
	if tlsCfg != nil {
		srv.TLSConfig = tlsCfg
		scheme = "https"
	}

	go func() {
		defer d.httpServing.Done()
		slog.Debug("device HTTP started", "device", d.Hostname, "scheme", scheme, "addr", addr)
		var err error
		if tlsCfg != nil {
			err = srv.ServeTLS(lis, "", "")
		} else {
			err = srv.Serve(lis)
		}
		if err != nil && !strings.Contains(err.Error(), "closed") {
			slog.Error("device HTTP error", "device", d.Hostname, "error", err)
		}
	}()
	return lis.Addr(), nil
}

// HTTPRequests counts the requests the device's http faces were sent.
// It reads the observation: every request plus every stream opened.
func (d *Device) HTTPRequests() int64 {
	c := d.Observed("http")
	return c.Received + c.StreamsOpened
}

// HTTPCredentialRefusals counts the refused requests that carried an
// Authorization header.
func (d *Device) HTTPCredentialRefusals() int64 { return d.httpCredentialRefusals.Load() }

// HTTPRefusals counts the requests the device's http auth gate refused.
func (d *Device) HTTPRefusals() int64 { return d.httpRefusals.Load() }

// HTTPAddrs returns the bound address of each HTTP listener the device is
// serving, in start order.
func (d *Device) HTTPAddrs() []net.Addr {
	d.httpMu.Lock()
	defer d.httpMu.Unlock()
	return append([]net.Addr(nil), d.httpAddrs...)
}

// StopHTTP closes every HTTP listener the device started and the
// connections they hold, so a stopped device stops answering clients that
// kept a connection alive. Every port is released by the time it returns,
// so a rebuild can bind the same address straight after (#946): the
// server's Close only reaches listeners its serve goroutine has already
// taken up, so the sockets are closed here directly and the serve
// goroutines are awaited.
func (d *Device) StopHTTP() {
	d.httpMu.Lock()
	servers, listeners := d.httpServers, d.httpListeners
	d.httpServers, d.httpListeners, d.httpAddrs = nil, nil, nil
	d.httpMu.Unlock()
	for _, srv := range servers {
		_ = srv.Close()
	}
	for _, lis := range listeners {
		_ = lis.Close()
	}
	d.httpServing.Wait()
}

// RunCommand looks up cmd against the device's registered commands and
// returns a CommandResult. ok=false means no matching command; callers
// decide how to respond (SSH sends exit-status 127, telnet writes an
// unknown-command banner). Exact match only. A command the family lists
// answers with its Go handler, else the attached surfaces answer, and the
// result carries the subscription verb a persistent session acts on.
func (d *Device) RunCommand(cmd string) (result CommandResult, ok bool, err error) {
	cmd = strings.TrimSpace(cmd)
	for _, ce := range d.commands {
		if ce.Match == cmd {
			return d.runFamilyCommand(ce), true, nil
		}
	}
	return d.runGoCommand(cmd)
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
