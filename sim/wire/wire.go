// Package wire is the vocabulary a Go device family declares its faces in
// (#2837): its interfaces (protocol, port, TLS, auth, the framing of a line
// protocol), the subjects a persistent session may subscribe to, the webhook it
// subscribes, and the listing of routes and commands the console shows. The
// engine serves a family from these declarations and the family's Go handlers;
// nothing here is read from a file, and nothing here is a template.
package wire

import (
	"fmt"
	"strings"
)

// WebhookSubscribe names the route a simulated service's admin would call to
// point its webhooks at a receiver: a POST taking a JSON body of url, scheme
// and secret. The simulator calls it for a device whose component holds a
// webhook endpoint (ADR-1370), so its posts flow from the start.
type WebhookSubscribe struct {
	Subscribe string
	// Driver names the shipped driver whose endpoint receives the service's
	// posts, for a lab that seeds the service as a webhook sender (#1747):
	// the plan reads the endpoint's signing input and the room identity
	// from it. Empty when the service is only ever subscribed by hand.
	Driver string
	// Provision is the path of a POST route taking a JSON body of id that
	// adds one room to the service's account, as its admin would. The
	// simulator calls it for each identity bound on the service's webhook
	// endpoint, so the service's reports name every bound room.
	Provision string
}

// Doc holds human-readable documentation metadata.
type Doc struct {
	Summary     string
	Description string
	Tags        []string
}

// Interface defines a protocol surface on a device.
//
// Persistent flips an SSH or Telnet interface from one-shot exec mode
// into a hot-socket shell loop that holds the connection open across
// many commands and supports asynchronous push notifications via
// Subscribe/Unsubscribe verbs declared in Commands. When false (the
// default), the interface behaves exactly as before: open, run one
// command, close. On a udp interface it serves the line protocol as
// datagrams, one session per source address (#1037), where without it the
// face echoes. Persistent has no effect on http/snmp/icmp.
//
// Subjects is the subscription whitelist for a Persistent interface:
// each Subject describes a state path that can be subscribed to and
// the line format for push notifications. A Command with Action
// "subscribe" or "unsubscribe" resolves its rendered Subject string
// against this list to decide what state to watch and how to format
// the push line. Empty Subjects means the interface accepts no
// subscriptions even if it is Persistent.
type Interface struct {
	Name     string
	Protocol string
	Port     int
	TLS      bool
	// TLSValidityDays is a tls face's certificate validity window, counted
	// from the face's start (#1035); zero is a year. Only on a face with tls.
	TLSValidityDays int
	// LossPct is the share of datagrams a udp face drops, in percent, each
	// way (#1037): seeded from the device and the face, so a run's losses
	// repeat, and a loss test needs no NET_ADMIN. Only on a udp face.
	LossPct int
	Doc     Doc
	Auth    Auth
	// Events is an http face's server-sent events stream (#1038).
	Events Events
	// WebSocket is a websocket face's protocol (#1034).
	WebSocket  WebSocket
	APISurface string
	Routes     []Route
	Commands   []Command
	// OIDs are the OIDs an snmp face serves, state path to OID, as the
	// console lists them; the family's surface answers them.
	OIDs          map[string]string
	ReachableWhen string

	// Persistent enables the hot-socket shell loop on ssh/telnet/tcp
	// listeners. When true, callers can issue any number of req/res
	// Commands without reconnecting and may register subscriptions
	// via Action verbs. Wire-format quirks (delimiter, frame brackets,
	// checksums) are configured by the LineEnding / FrameStart /
	// FrameEnd / StripChecksumBytes fields below, applied
	// uniformly across SSH, Telnet, and raw TCP listeners since they
	// describe the protocol's wire shape, not the transport.
	Persistent bool

	// LineEnding is the inter-frame delimiter for both directions:
	// the shell loop writes it after every outbound response, and
	// the inbound reader splits on the LAST byte of this string and
	// then trims the full string from the end. Default "\r\n". Set
	// to "\x00" for NUL-terminated protocols (Q-SYS QRC), "\r" for
	// CR-only (some legacy gear), or any single byte for binary
	// frame separators. Only consulted when Persistent is true.
	LineEnding string

	// FrameStart and FrameEnd, when non-empty, bracket every frame
	// on the wire. Inbound: stripped before the command match runs.
	// Outbound: wrapped around every response and push line, before
	// LineEnding is appended. Common pairs: "\x02" / "\x03" (STX/ETX
	// in some Polycom and Extron gear). Both empty (the default)
	// disables framing. Only consulted when Persistent is true.
	FrameStart string
	FrameEnd   string

	// StripChecksumBytes peels N bytes off the END of every inbound
	// frame after FrameEnd is stripped, before the command match
	// runs. Use for protocols that append a 1- or 2-byte checksum
	// (XOR sum, Fletcher, etc.), operators don't have to encode
	// the checksum into every Command match. The lab does not
	// validate the stripped bytes; this is a teaching surface, not
	// a wire conformance gate. Outbound checksums are computed by
	// profile templates via the xor_checksum / sum_byte_checksum_hex
	// helpers. Default 0.
	StripChecksumBytes int

	// Welcome is an optional banner the shell loop writes once after
	// successful auth (or immediately on connect for transports
	// without an auth dialog like raw TCP). FrameStart/FrameEnd are
	// NOT applied to the welcome, it's a free-form greeting.
	// LineEnding is appended.
	Welcome string

	// Subjects is the subscription whitelist + push-line formatter. See
	// Subject. Only consulted when Persistent is true.
	Subjects []Subject

	// Limits caps resource use per session. Zero values fall back to
	// SessionLimitsDefaults at engine startup.
	Limits SessionLimits
}

// Subject describes one subscribable state path on a Persistent interface.
// Match is a glob compared against a subscription's subject (`*` matches one
// dot-separated segment, a trailing `**` the rest). Path is the state path it
// watches (the subject itself when empty). PublishToken is the symbolic name a
// push line may carry. A subject's push line is Go: Push, a function of the new
// value, or PushLine, which also names the subject that changed (a wildcard
// Match covers many) and the token the subscription carries, and wins.
//
// Subject lookup walks Subjects in declaration order and uses the first
// matching entry, so put narrow matches before catch-alls.
type Subject struct {
	Match        string
	Path         string
	PublishToken string
	Push         func(value any) string
	PushLine     func(subject, token string, value any) string
}

// SessionLimits caps resource use on a Persistent interface. All
// fields are per-session unless noted.
type SessionLimits struct {
	// MaxSubsPerSession bounds how many concurrent subscriptions a
	// single session may register. Default 64.
	MaxSubsPerSession int

	// PublishMinIntervalMs floors interval-driven subscriptions and
	// throttles on-change fanout per (session, subject). Default 50.
	PublishMinIntervalMs int

	// MaxSessionsPerDevice bounds how many concurrent sessions a single
	// device's listener will hold open. Default 16.
	MaxSessionsPerDevice int

	// OutBuffer is the per-session push-line buffer depth. On overflow
	// the session is closed with a buffer-overrun error rather than
	// blocking the device's mutator. Default 256.
	OutBuffer int
}

// SessionLimitsDefaults are applied wherever SessionLimits has zero
// values, so a profile that omits limits still gets sensible bounds.
var SessionLimitsDefaults = SessionLimits{
	MaxSubsPerSession:    64,
	PublishMinIntervalMs: 50,
	MaxSessionsPerDevice: 16,
	OutBuffer:            256,
}

// WithDefaults returns a copy of l with zero fields filled in from
// SessionLimitsDefaults. Use at engine startup so downstream code
// never needs to test for zero values.
func (l SessionLimits) WithDefaults() SessionLimits {
	out := l
	if out.MaxSubsPerSession == 0 {
		out.MaxSubsPerSession = SessionLimitsDefaults.MaxSubsPerSession
	}
	if out.PublishMinIntervalMs == 0 {
		out.PublishMinIntervalMs = SessionLimitsDefaults.PublishMinIntervalMs
	}
	if out.MaxSessionsPerDevice == 0 {
		out.MaxSessionsPerDevice = SessionLimitsDefaults.MaxSessionsPerDevice
	}
	if out.OutBuffer == 0 {
		out.OutBuffer = SessionLimitsDefaults.OutBuffer
	}
	return out
}

// Auth defines authentication configuration for an interface.
//
// Type semantics by protocol:
//
//   - ssh / telnet: "password", credentials are required to log in.
//   - snmp: Community is the v2c community string.
//   - http: "basic" enforces HTTP Basic; "header" enforces a header-based
//     API key. Auth is only enforced when Enforce is true; profiles can
//     declare credentials for documentation without gating traffic. Other
//     values (or empty Type with Enforce=false) leave the listener open.
//
// HTTP-specific fields:
//
//   - DefaultUser / DefaultPassword carry the basic-auth credentials.
//   - APIKey is the expected value for header auth.
//   - Header is the request header name for header auth (default "X-API-Key").
//   - Realm is the value sent in WWW-Authenticate for basic challenges
//     (default: the device hostname).
//   - PublicPaths exempts specific request paths from the gate. Useful
//     for a header-auth device that needs to expose an unauthenticated
//     landing page so the user can paste their token before calling
//     the gated API endpoints.
//
// SSH/Telnet-specific fields:
//
//   - Permissive opts a CLI listener into the legacy accept-anything
//     posture. Default behavior on ssh/telnet is to require the
//     declared DefaultUser + DefaultPassword. Set Permissive: true on
//     a profile that intentionally wants to accept any credentials,
//     for example a fixture used by an integration test.
type Auth struct {
	Type            string
	DefaultUser     string
	DefaultPassword string
	Community       string
	// RefusedCommunity is a community an snmp face knows and refuses: it
	// answers it with authorizationError (#821), where any community but its
	// own is dropped silently.
	RefusedCommunity string
	Enforce          bool
	Permissive       bool
	APIKey           string
	Header           string
	// Query names the query parameter a query-auth face reads its api key
	// from (#902); empty is apikey.
	Query       string
	Realm       string
	PublicPaths []string
	// LoginPath is a form or token face's login path (#1114): /login and
	// /api/login when empty. SessionTTL is the seconds a session it opens
	// lives (a token's expires_in; zero keeps a form session until it is
	// expired, and gives a token an hour). Algorithm is a digest face's
	// hash, MD5 (the default) or SHA-256. ExpirePath is the path the
	// console fires to expire every session and rotate the digest nonce.
	LoginPath  string
	SessionTTL int
	Algorithm  string
	ExpirePath string
}

// Events declares an http face's server-sent events stream (#1038): the
// path it serves (behind the face's auth), the reconnection time it asks
// for (milliseconds; zero asks none), the state paths whose changes it
// streams (each with everything beneath it; none streams every change),
// and the path the console fires (POST) to drop every stream, as a restart
// would.
type Events struct {
	Path     string
	RetryMS  int
	Paths    []string
	DropPath string
}

// WebSocket declares a websocket face (#1034): the path its upgrade is
// served at (behind the face's auth), the state paths a subscriber may
// watch and a get may read (each with everything beneath it; none is every
// path), the paths a set may write, and the path the console fires (POST)
// to drop every socket, as a restart would.
type WebSocket struct {
	Path     string
	Paths    []string
	Settable []string
	DropPath string
}

// Route is one HTTP route a family lists on its interface: the console shows
// it, and an operator can fire it. What answers it is the family's Go handler.
//
// Example is a raw request body prefilled into the console's operations
// panel for POST/PUT routes, authored literally.
type Route struct {
	Match   string
	Doc     Doc
	Example string
	// Sets are the quick sets the route declares: each state path it writes
	// from one body field, to one of a fixed list of values.
	Sets []Set
	// Visibility marks a route as part of the device's advertised public API
	// surface ("" or "public") or as a private route the device's own web UI
	// calls internally ("private"). It is console metadata only; dispatch
	// never consults it.
	Visibility string
}

// Command is one line command a family lists on its interface, as the console
// shows it. Example is a default command line shown in the operations panel;
// when unset, Match is the default. Action is "subscribe" or "unsubscribe"
// for a verb that registers or drops a subscription on a persistent face.
type Command struct {
	Match   string
	Doc     Doc
	Example string
	Action  string
}

// ValidateInterfaces refuses what the simulator cannot serve faithfully: a
// device has one SSH, one Telnet and one SNMP listener, so a second interface
// of any of them would silently go unserved, and a setting on a face that does
// not take it.
func ValidateInterfaces(ifaces []Interface) error {
	seen := map[string]string{}
	for _, iface := range ifaces {
		if iface.TLSValidityDays != 0 && !iface.TLS {
			return fmt.Errorf("interface %q sets tls_validity_days but serves no tls", iface.Name)
		}
		if iface.TLSValidityDays < 0 {
			return fmt.Errorf("interface %q sets a negative tls_validity_days", iface.Name)
		}
		if iface.Auth.RefusedCommunity != "" && iface.Protocol != "snmp" {
			return fmt.Errorf("interface %q sets a refused_community but is not an snmp face", iface.Name)
		}
		if iface.Auth.RefusedCommunity != "" && iface.Auth.RefusedCommunity == iface.Auth.Community {
			return fmt.Errorf("interface %q refuses its own community", iface.Name)
		}
		if iface.LossPct != 0 && iface.Protocol != "udp" {
			return fmt.Errorf("interface %q sets loss_pct but is not a udp face", iface.Name)
		}
		if iface.LossPct < 0 || iface.LossPct > 100 {
			return fmt.Errorf("interface %q sets loss_pct %d; want 0 to 100", iface.Name, iface.LossPct)
		}
		if err := validateWebSocketFace(iface); err != nil {
			return err
		}
		if iface.Protocol != "ssh" && iface.Protocol != "telnet" && iface.Protocol != "snmp" {
			continue
		}
		if prev, dup := seen[iface.Protocol]; dup {
			return fmt.Errorf("interfaces %q and %q are both %s; a device serves one %s interface", prev, iface.Name, iface.Protocol, iface.Protocol)
		}
		seen[iface.Protocol] = iface.Name
	}
	return nil
}

// validateWebSocketFace rejects a websocket block the simulator could not
// serve: one on another protocol, a websocket face with no path, and an
// auth whose credential only a login face issues (the face's auth is its
// own, one of the static modes).
func validateWebSocketFace(iface Interface) error {
	ws := iface.WebSocket
	declared := ws.Path != "" || len(ws.Paths) > 0 || len(ws.Settable) > 0 || ws.DropPath != ""
	if iface.Protocol != "websocket" {
		if declared {
			return fmt.Errorf("interface %q declares a websocket block but is not a websocket face", iface.Name)
		}
		return nil
	}
	if !strings.HasPrefix(ws.Path, "/") {
		return fmt.Errorf("websocket interface %q needs a websocket.path starting with /", iface.Name)
	}
	if ws.DropPath != "" && !strings.HasPrefix(ws.DropPath, "/") {
		return fmt.Errorf("websocket interface %q drop_path %q does not start with /", iface.Name, ws.DropPath)
	}
	switch iface.Auth.Type {
	case "", "basic", "header", "bearer", "query":
	default:
		return fmt.Errorf("websocket interface %q declares %s auth; a websocket face's own auth is basic, header, bearer or query", iface.Name, iface.Auth.Type)
	}
	return nil
}

// validateComputed checks the per-profile `computed:` block. Each entry
// must name a target path, a supported kind, and (for elapsed) an anchor
// path. A computed path must not also be written by a behavior, that
