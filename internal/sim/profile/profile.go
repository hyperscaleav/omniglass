// Package profile loads and validates device and system profile YAML files.
package profile

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// DeviceProfile defines a device type's identity, interfaces, behaviors, and defaults.
type DeviceProfile struct {
	Name         string         `yaml:"name"`
	Manufacturer string         `yaml:"manufacturer"`
	Model        string         `yaml:"model"`
	DeviceType   string         `yaml:"device_type"`
	SlugPrefix   string         `yaml:"slug_prefix"`
	Doc          Doc            `yaml:"doc"`
	Interfaces   []Interface    `yaml:"interfaces"`
	Behaviors    []Behavior     `yaml:"behaviors"`
	Computed     []Computed     `yaml:"computed"`
	Overrides    []Override     `yaml:"overrides"`
	Defaults     map[string]any `yaml:"defaults"`
	DocFixture   map[string]any `yaml:"doc_fixture"`
	// Products are the other products this profile simulates: variants of
	// the same kind of device (the same component type) on the same driver,
	// a display line in its sizes. Each may overlay the profile's defaults
	// with its own identity. The catalog alignment test holds both rules.
	Products []ProductVariant `yaml:"products"`
	// Extends names a profile whose surface this one carries and adds to
	// (#896): a control processor answers everything the touch panel does,
	// and more. Resolved at load by extend.
	Extends string `yaml:"extends"`
}

// ProductVariant is one more product a profile simulates.
type ProductVariant struct {
	Name string `yaml:"name"`
	// Defaults are deep-merged over the profile's defaults for this product
	// (its model name, typically).
	Defaults map[string]any `yaml:"defaults"`
}

// Doc holds human-readable documentation metadata.
type Doc struct {
	Summary     string   `yaml:"summary"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
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
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"`
	Port     int    `yaml:"port"`
	TLS      bool   `yaml:"tls"`
	// TLSValidityDays is a tls face's certificate validity window, counted
	// from the face's start (#1035); zero is a year. Only on a face with tls.
	TLSValidityDays int `yaml:"tls_validity_days"`
	// LossPct is the share of datagrams a udp face drops, in percent, each
	// way (#1037): seeded from the device and the face, so a run's losses
	// repeat, and a loss test needs no NET_ADMIN. Only on a udp face.
	LossPct int  `yaml:"loss_pct"`
	Doc     Doc  `yaml:"doc"`
	Auth    Auth `yaml:"auth"`
	// Events is an http face's server-sent events stream (#1038).
	Events Events `yaml:"events"`
	// WebSocket is a websocket face's protocol (#1034).
	WebSocket     WebSocket         `yaml:"websocket"`
	APISurface    string            `yaml:"api_surface"`
	Routes        []Route           `yaml:"routes"`
	Commands      []Command         `yaml:"commands"`
	OIDMap        map[string]string `yaml:"oid_map"`
	ReachableWhen string            `yaml:"reachable_when"`

	// MIB names the SNMP MIB file this device's enterprise OID arc is
	// defined in (e.g. the Boreal Edge). Empty for devices with no
	// enterprise MIB (MIB-2-only panels).
	MIB string `yaml:"mib"`

	// Persistent enables the hot-socket shell loop on ssh/telnet/tcp
	// listeners. When true, callers can issue any number of req/res
	// Commands without reconnecting and may register subscriptions
	// via Action verbs. Wire-format quirks (delimiter, frame brackets,
	// checksums) are configured by the LineEnding / FrameStart /
	// FrameEnd / StripChecksumBytes fields below, applied
	// uniformly across SSH, Telnet, and raw TCP listeners since they
	// describe the protocol's wire shape, not the transport.
	Persistent bool `yaml:"persistent"`

	// LineEnding is the inter-frame delimiter for both directions:
	// the shell loop writes it after every outbound response, and
	// the inbound reader splits on the LAST byte of this string and
	// then trims the full string from the end. Default "\r\n". Set
	// to "\x00" for NUL-terminated protocols (Q-SYS QRC), "\r" for
	// CR-only (some legacy gear), or any single byte for binary
	// frame separators. Only consulted when Persistent is true.
	LineEnding string `yaml:"line_ending"`

	// FrameStart and FrameEnd, when non-empty, bracket every frame
	// on the wire. Inbound: stripped before the command match runs.
	// Outbound: wrapped around every response and push line, before
	// LineEnding is appended. Common pairs: "\x02" / "\x03" (STX/ETX
	// in some Polycom and Extron gear). Both empty (the default)
	// disables framing. Only consulted when Persistent is true.
	FrameStart string `yaml:"frame_start"`
	FrameEnd   string `yaml:"frame_end"`

	// StripChecksumBytes peels N bytes off the END of every inbound
	// frame after FrameEnd is stripped, before the command match
	// runs. Use for protocols that append a 1- or 2-byte checksum
	// (XOR sum, Fletcher, etc.), operators don't have to encode
	// the checksum into every Command match. The lab does not
	// validate the stripped bytes; this is a teaching surface, not
	// a wire conformance gate. Outbound checksums are computed by
	// profile templates via the xor_checksum / sum_byte_checksum_hex
	// helpers. Default 0.
	StripChecksumBytes int `yaml:"strip_checksum_bytes"`

	// Welcome is an optional banner the shell loop writes once after
	// successful auth (or immediately on connect for transports
	// without an auth dialog like raw TCP). FrameStart/FrameEnd are
	// NOT applied to the welcome, it's a free-form greeting.
	// LineEnding is appended.
	Welcome string `yaml:"welcome"`

	// Subjects is the subscription whitelist + push-line formatter. See
	// Subject. Only consulted when Persistent is true.
	Subjects []Subject `yaml:"subjects"`

	// Limits caps resource use per session. Zero values fall back to
	// SessionLimitsDefaults at engine startup.
	Limits SessionLimits `yaml:"limits"`
}

// Subject describes one subscribable state path on a Persistent
// interface. Match is a glob (suffix `*` supported) compared against
// the rendered Command.Subject string. Path is a JSONPath into the
// device state, the same JSONPath syntax already used by Route.Mutate
// and Command.Mutate. PublishToken is an optional symbolic name
// available to push_format as `{{ .Token }}`. PushFormat is a Go
// template that renders one push line; available data: `.Subject`
// (the rendered subject string), `.Path`, `.Token`, `.Value` (the
// state value at Path), `.Hostname`, `.DeviceID`.
//
// Subject lookup walks Subjects in declaration order and uses the
// first matching entry, so put narrow matches before catch-alls.
type Subject struct {
	Match        string `yaml:"match"`
	Path         string `yaml:"path"`
	PublishToken string `yaml:"publish_token"`
	PushFormat   string `yaml:"push_format"`
}

// SessionLimits caps resource use on a Persistent interface. All
// fields are per-session unless noted.
type SessionLimits struct {
	// MaxSubsPerSession bounds how many concurrent subscriptions a
	// single session may register. Default 64.
	MaxSubsPerSession int `yaml:"max_subs_per_session"`

	// PublishMinIntervalMs floors interval-driven subscriptions and
	// throttles on-change fanout per (session, subject). Default 50.
	PublishMinIntervalMs int `yaml:"publish_min_interval_ms"`

	// MaxSessionsPerDevice bounds how many concurrent sessions a single
	// device's listener will hold open. Default 16.
	MaxSessionsPerDevice int `yaml:"max_sessions_per_device"`

	// OutBuffer is the per-session push-line buffer depth. On overflow
	// the session is closed with a buffer-overrun error rather than
	// blocking the device's mutator. Default 256.
	OutBuffer int `yaml:"out_buffer"`
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
	Type            string `yaml:"type"`
	DefaultUser     string `yaml:"default_user"`
	DefaultPassword string `yaml:"default_password"`
	Community       string `yaml:"community"`
	// RefusedCommunity is a community an snmp face knows and refuses: it
	// answers it with authorizationError (#821), where any community but its
	// own is dropped silently.
	RefusedCommunity string `yaml:"refused_community"`
	Enforce          bool   `yaml:"enforce"`
	Permissive       bool   `yaml:"permissive"`
	APIKey           string `yaml:"api_key"`
	Header           string `yaml:"header"`
	// Query names the query parameter a query-auth face reads its api key
	// from (#902); empty is apikey.
	Query       string   `yaml:"query"`
	Realm       string   `yaml:"realm"`
	PublicPaths []string `yaml:"public_paths"`
	// LoginPath is a form or token face's login path (#1114): /login and
	// /api/login when empty. SessionTTL is the seconds a session it opens
	// lives (a token's expires_in; zero keeps a form session until it is
	// expired, and gives a token an hour). Algorithm is a digest face's
	// hash, MD5 (the default) or SHA-256. ExpirePath is the path the
	// console fires to expire every session and rotate the digest nonce.
	LoginPath  string `yaml:"login_path"`
	SessionTTL int    `yaml:"session_ttl"`
	Algorithm  string `yaml:"algorithm"`
	ExpirePath string `yaml:"expire_path"`
}

// Events declares an http face's server-sent events stream (#1038): the
// path it serves (behind the face's auth), the reconnection time it asks
// for (milliseconds; zero asks none), the state paths whose changes it
// streams (each with everything beneath it; none streams every change),
// and the path the console fires (POST) to drop every stream, as a restart
// would.
type Events struct {
	Path     string   `yaml:"path"`
	RetryMS  int      `yaml:"retry_ms"`
	Paths    []string `yaml:"paths"`
	DropPath string   `yaml:"drop_path"`
}

// WebSocket declares a websocket face (#1034): the path its upgrade is
// served at (behind the face's auth), the state paths a subscriber may
// watch and a get may read (each with everything beneath it; none is every
// path), the paths a set may write, and the path the console fires (POST)
// to drop every socket, as a restart would.
type WebSocket struct {
	Path     string   `yaml:"path"`
	Paths    []string `yaml:"paths"`
	Settable []string `yaml:"settable"`
	DropPath string   `yaml:"drop_path"`
}

// Route defines an HTTP route match and its template.
//
// Example is a raw request body prefilled into the console's operations
// panel for POST/PUT routes. Authored literally (JSON string or plain
// text); rendered unmodified so operators can edit before Send.
//
// Mutate is the legacy map shape (path -> value template). Mutations is
// the richer list-of-objects shape that supports per-mutation validation
// (enum, pattern). Both are accepted; profiles may use either or both.
type Route struct {
	Match     string         `yaml:"match"`
	Template  string         `yaml:"template"`
	Doc       Doc            `yaml:"doc"`
	Mutate    map[string]any `yaml:"mutate"`
	Mutations []Mutation     `yaml:"mutations"`
	Example   string         `yaml:"example"`

	// Visibility marks a route as part of the device's advertised
	// public API surface or as a private route the device's own web
	// UI calls internally but that is not advertised as a public API.
	// Allowed values: "" / "public" (the default; listed in docs and
	// the console as a normal API route) and "private" (served at
	// runtime exactly the same, but flagged so docgen and the console
	// can distinguish a hidden internal endpoint from an advertised
	// one). Runtime dispatch never consults this field, it is
	// documentation/console metadata only.
	Visibility string `yaml:"visibility"`

	// Events are named occurrences the route reports once its mutations
	// apply (#1145): a codec reporting a call starting and ending as named
	// events carrying the call's id, as xAPI's CallSuccessful and
	// CallDisconnect do, beside the state changes its events face streams.
	// Each data is a template rendered against the state the mutations
	// left. A device with no events face ignores them.
	Events []RouteEvent `yaml:"events"`

	// Emit is a webhook the route posts once its mutations apply (#1024),
	// rendered against the state they left and the request: a cloud
	// service posting a meeting's start the moment it begins.
	Emit *Emit `yaml:"emit"`
}

// RouteEvent is one named event a route reports: its name and its data (a
// template, usually a JSON document).
type RouteEvent struct {
	Name string `yaml:"name"`
	Data string `yaml:"data"`
}

// Command defines a console command match and its template.
//
// Example is an optional default command line shown in the console's
// operations panel; when unset, Match is the default.
//
// Mutate vs Mutations: same dual-shape rule as Route.
//
// Action enables persistent-session subscription verbs on Persistent
// interfaces. When unset, the command behaves as a normal req/res
// (matched, optionally mutated, response rendered). Allowed values:
// "subscribe" registers a subscription whose state path is resolved
// via Subject (rendered as a template against the command's match
// arguments) against the interface's Subjects whitelist; "unsubscribe"
// drops a previously registered subscription. Token and IntervalMs are
// optional templates available to subscribe actions: Token sets the
// subscription's publish token (overriding the Subject's default
// PublishToken when non-empty), IntervalMs converts to an integer and
// pushes that many milliseconds between heartbeats, 0 (or unset)
// means on-change only. Action fields are ignored on non-Persistent
// interfaces, so a profile may declare them defensively without
// changing one-shot behavior.
type Command struct {
	Match     string         `yaml:"match"`
	Template  string         `yaml:"template"`
	Doc       Doc            `yaml:"doc"`
	Mutate    map[string]any `yaml:"mutate"`
	Mutations []Mutation     `yaml:"mutations"`
	Example   string         `yaml:"example"`

	// MatchHex is an alternative to Match for protocols whose
	// commands include literal control bytes that don't survive
	// round-tripping through YAML escape rules. Whitespace-separated
	// hex pairs ("02 53 00 03") decode at profile-load to the same
	// binary string that Match would carry; the result is treated
	// identically to Match for dispatch. When both Match and MatchHex
	// are present, MatchHex wins.
	MatchHex string `yaml:"match_hex"`

	// Response is an inline response template that takes precedence
	// over Template (path) when non-empty. Useful for one-line
	// responses (e.g. "+OK") in persistent-session profiles where a
	// separate template file would be excessive.
	Response string `yaml:"response"`

	// Action selects subscribe / unsubscribe semantics on Persistent
	// interfaces. Empty = ordinary req/res. Allowed: "subscribe",
	// "unsubscribe".
	Action string `yaml:"action"`

	// Subject is a template rendered against the command's match
	// arguments. The rendered string is matched against
	// Interface.Subjects to resolve the state path and push format.
	// Required when Action is set.
	Subject string `yaml:"subject"`

	// Token is an optional template for the subscription's publish
	// token. Falls back to Subject.PublishToken when empty.
	Token string `yaml:"token"`

	// IntervalMs is an optional template that, after rendering, must
	// parse as a non-negative integer. 0 (or unset) means on-change
	// only; non-zero spawns an interval pump.
	IntervalMs string `yaml:"interval_ms"`
}

// Mutation is a single state write declared in the list-of-objects
// shape. Path and Value are templates evaluated per request. Enum, when
// non-empty, restricts the substituted value to one of the listed
// strings (case-sensitive). Pattern, when non-empty, is a regex the
// substituted value must match. Both Enum and Pattern are optional and
// may be combined; both must pass when both are set. Validation runs
// after template substitution, before the state write.
//
// Optional opts a mutation out of the fail-closed default. When false
// (the default), a runtime failure of this mutation causes the route
// handler to respond 400 with a structured error body and skip the
// success template render. When true, a failure logs a warning but does
// not block the request: the route still renders its success template
// and any other successful mutations still apply. Use this for
// best-effort writes that touch a path that may not exist on every
// device variant or on a fresh state tree.
type Mutation struct {
	Path     string   `yaml:"path"`
	Value    any      `yaml:"value"`
	Enum     []string `yaml:"enum"`
	Pattern  string   `yaml:"pattern"`
	Optional bool     `yaml:"optional"`
}

// Behavior is an autonomous, scheduled mutation that runs on a device
// without needing an external trigger. Behaviors reuse the route-mutation
// pipeline from internal/sim/mutate.go: each tick renders the
// listed mutations against the device's current state and applies them
// in place. Phase 1 of omniglass-lab#52.
//
// Determinism: when a Mutation's value template uses randStep, the
// per-device pseudo-random source is seeded from the device id so a
// 1000-device deployment running the same profile produces 1000
// individually phased random walks that replay the same sequence
// across runs.
//
// Phase offsets: at startup the scheduler delays the first tick of
// each behavior by hash(device_id, behavior_name) mod interval, so
// behaviors that share an interval don't all fire on the same wall
// clock tick across the fleet.
type Behavior struct {
	Name      string     `yaml:"name"`
	Schedule  Schedule   `yaml:"schedule"`
	Mutations []Mutation `yaml:"mutations"`

	// Emit, when set, makes the behavior POST an outbound webhook on each
	// tick (after its mutations apply). This is the device's only
	// egress-HTTP capability: a behavior with an emit block can notify an
	// external webhook receiver that an event
	// happened, e.g. a "meeting started" webhook from a ucc-service
	// instance. Delivery is best-effort: it only fires when the rendered
	// URL is non-empty, runs with a short timeout off the tick goroutine,
	// and logs-and-continues on failure so a dead collector never blocks
	// or crashes the behavior. See internal/sim/emit.go.
	Emit *Emit `yaml:"emit"`
}

// Emit declares a behavior's outbound webhook. Both fields are templates
// rendered against the device's tick context (.State, .Defaults,
// .Hostname, .DeviceID, .IP) so the target URL can be a single
// profile-level config value (e.g.
// "{{ .Defaults.configuration.webhook.target_url }}") that an operator
// sets once per device family, and the body can interpolate live state.
//
// URL: rendered per tick; an empty result disables the emit for that
// tick (the safe default, a profile ships the behavior, the operator
// opts in by configuring the URL). Body: the POST request body, sent
// with Content-Type application/json. Method is always POST.
type Emit struct {
	URL  string `yaml:"url"`
	Body string `yaml:"body"`
	// Headers are sent with the post, each value a template (a sender's
	// event-type header).
	Headers map[string]string `yaml:"headers"`
	// Sign signs the post as a webhook sender does (#1024).
	Sign *EmitSign `yaml:"sign"`
}

// EmitSign is how a sender authenticates its webhook (#1024): its scheme
// (hmac, token, basic, or none; a template, so a subscription the device's
// own api took can set it), its secret (the hmac key, the token, or
// user:password; a template), the header the signature or token rides and
// the prefix before it, the header carrying the signed unix timestamp (hmac
// then signs the timestamp, a dot and the body), the header carrying each
// post's delivery id, and a fault (a template) that posts as a sender gone
// wrong: bad-signature, stale (a timestamp ten minutes old) or redeliver
// (the same delivery twice).
type EmitSign struct {
	Scheme    string `yaml:"scheme"`
	Secret    string `yaml:"secret"`
	Header    string `yaml:"header"`
	Prefix    string `yaml:"prefix"`
	Timestamp string `yaml:"timestamp"`
	Delivery  string `yaml:"delivery"`
	Fault     string `yaml:"fault"`
}

// Computed declares a read-derived field: its value is calculated at
// read time from other state rather than written by a scheduled
// behavior. Where a Behavior is push (a timer mutates state and emits a
// StateChange event every tick), a Computed field is pull (no goroutine,
// no event, the value is resolved fresh whenever a reader observes it).
//
// kind: elapsed reports integer seconds since an anchor timestamp. The
// anchor lives at the Since state path as a Unix epoch (seconds); the
// simulator seeds it at device start and a profile resets it on reboot by
// mutating Since to "{{ now }}". Path is what every read surface (SNMP,
// HTTP, sessions, the console snapshot) observes, e.g. status.device.uptime.
//
// This is the flood-free replacement for the old uptime_counter behavior:
// uptime no longer emits an event every interval, it is computed as
// now - boot_epoch on demand.
type Computed struct {
	Path  string `yaml:"path"`  // path readers observe (e.g. status.device.uptime)
	Kind  string `yaml:"kind"`  // "elapsed" (only kind for now)
	Since string `yaml:"since"` // anchor state path holding a Unix-epoch seconds value
}

// Schedule says when a behavior fires. Phase 1 supports a fixed
// Interval only. Later phases will add Cron, OnEvent, and other
// triggers; existing Interval-based behaviors will keep working when
// those fields are introduced.
type Schedule struct {
	// Interval is parsed as a Go time.Duration ("1s", "500ms", "5m").
	// Required for phase 1 behaviors. Empty disables the behavior
	// (and is logged as a profile warning at load time).
	Interval string `yaml:"interval"`
}

// Override defines a state field override (e.g., drop a field for a reduced device).
type Override struct {
	Match string `yaml:"match"`
	Rule  string `yaml:"rule"`
}

// SystemProfile defines a room archetype, a collection of devices that form a system.
type SystemProfile struct {
	Name    string         `yaml:"name"`
	Doc     Doc            `yaml:"doc"`
	Devices []SystemDevice `yaml:"devices"`
}

// SystemDevice defines a device slot within a system profile.
type SystemDevice struct {
	Profile string   `yaml:"profile"`
	Role    string   `yaml:"role"`
	Suffix  string   `yaml:"suffix"`
	Count   int      `yaml:"count"`
	Tags    []string `yaml:"tags"`
}

// Store holds all loaded profiles indexed by name.
type Store struct {
	Devices map[string]*DeviceProfile
	Systems map[string]*SystemProfile
	// variants maps a product a profile lists to that profile and overlay.
	variants map[string]variantRef
}

type variantRef struct {
	profile string
	overlay map[string]any
}

// ForProduct resolves the profile that simulates a product: the profile of
// that name, or the profile that lists it among its products (with the
// product's defaults overlay). ok is false when no profile simulates it.
func (s *Store) ForProduct(product string) (profileName string, overlay map[string]any, ok bool) {
	if _, ok := s.Devices[product]; ok {
		return product, nil, true
	}
	if v, ok := s.variants[product]; ok {
		return v.profile, v.overlay, true
	}
	return "", nil, false
}

// indexVariants builds the product lookup, refusing a product two profiles
// would both answer for.
func (s *Store) indexVariants() error {
	s.variants = map[string]variantRef{}
	for name, p := range s.Devices {
		for _, v := range p.Products {
			if _, clash := s.Devices[v.Name]; clash {
				return fmt.Errorf("profile %q lists product %q, which is a profile of its own", name, v.Name)
			}
			if prev, dup := s.variants[v.Name]; dup {
				return fmt.Errorf("product %q is listed by both %q and %q", v.Name, prev.profile, name)
			}
			s.variants[v.Name] = variantRef{profile: name, overlay: v.Defaults}
		}
	}
	return nil
}

// LoadStore loads device and system profiles from the given profile root directory.
// It expects profiles/devices/*.yaml and profiles/systems/*.yaml under root.
//
// Thin wrapper over LoadStoreFS for callers that read a catalog from
// disk; the simulator itself loads the embedded catalog
// (internal/sim/profiles) through LoadStoreFS.
func LoadStore(root string) (*Store, error) {
	return LoadStoreFS(os.DirFS(root))
}

// LoadStoreFS loads profiles from any fs.FS rooted at the profile
// directory. Layout is `devices/*.yaml` and `systems/*.yaml`.
func LoadStoreFS(fsys fs.FS) (*Store, error) {
	s := &Store{
		Devices: make(map[string]*DeviceProfile),
		Systems: make(map[string]*SystemProfile),
	}
	if err := s.loadDevices(fsys, "devices"); err != nil {
		return nil, fmt.Errorf("loading device profiles: %w", err)
	}
	if err := s.resolveExtends(); err != nil {
		return nil, fmt.Errorf("resolving extends: %w", err)
	}
	if err := s.loadSystems(fsys, "systems"); err != nil {
		return nil, fmt.Errorf("loading system profiles: %w", err)
	}
	if err := s.indexVariants(); err != nil {
		return nil, fmt.Errorf("indexing products: %w", err)
	}
	return s, nil
}

// isFSNotExist mirrors os.IsNotExist for fs.PathError values arriving
// from an embedded FS where the file simply isn't included.
func isFSNotExist(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "file does not exist")
}

func (s *Store) loadDevices(fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if os.IsNotExist(err) || isFSNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		full := path.Join(dir, e.Name())
		data, err := fs.ReadFile(fsys, full)
		if err != nil {
			return fmt.Errorf("read %s: %w", full, err)
		}
		var p DeviceProfile
		if err := yaml.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("parsing %s: %w", full, err)
		}
		if p.Name == "" {
			return fmt.Errorf("%s: missing required field 'name'", full)
		}
		if _, exists := s.Devices[p.Name]; exists {
			return fmt.Errorf("duplicate device profile name: %s", p.Name)
		}
		if err := validateInterfaces(&p); err != nil {
			return fmt.Errorf("%s: %w", full, err)
		}
		if err := validateComputed(&p); err != nil {
			return fmt.Errorf("%s: %w", full, err)
		}
		s.Devices[p.Name] = &p
	}
	return nil
}

// validateInterfaces rejects what the simulator cannot serve faithfully: a
// device has one SSH, one Telnet and one SNMP listener, so a second
// interface of any of them would silently go unserved.
func validateInterfaces(p *DeviceProfile) error {
	seen := map[string]string{}
	for _, iface := range p.Interfaces {
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
// would resurrect the push/pull conflation this feature exists to remove.
func validateComputed(p *DeviceProfile) error {
	behaviorPaths := make(map[string]bool)
	for _, b := range p.Behaviors {
		for _, m := range b.Mutations {
			behaviorPaths[m.Path] = true
		}
	}
	seen := make(map[string]int)
	for i, c := range p.Computed {
		ctx := fmt.Sprintf("computed[%d]", i)
		if c.Path == "" {
			return fmt.Errorf("%s: missing required field 'path'", ctx)
		}
		if prev, dup := seen[c.Path]; dup {
			return fmt.Errorf("%s (%q): duplicate path, also declared by computed[%d]", ctx, c.Path, prev)
		}
		seen[c.Path] = i
		if behaviorPaths[c.Path] {
			return fmt.Errorf("%s (%q): path is also written by a behavior/override, a computed field is read-derived and must not be mutated on a timer", ctx, c.Path)
		}
		switch c.Kind {
		case "elapsed":
			if c.Since == "" {
				return fmt.Errorf("%s (%q): kind 'elapsed' requires field 'since' (the anchor state path holding a Unix-epoch seconds value)", ctx, c.Path)
			}
		case "":
			return fmt.Errorf("%s (%q): missing required field 'kind'", ctx, c.Path)
		default:
			return fmt.Errorf("%s (%q): unsupported kind %q, only 'elapsed' is supported", ctx, c.Path, c.Kind)
		}
	}
	return nil
}

func (s *Store) loadSystems(fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if os.IsNotExist(err) || isFSNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		full := path.Join(dir, e.Name())
		data, err := fs.ReadFile(fsys, full)
		if err != nil {
			return fmt.Errorf("read %s: %w", full, err)
		}
		var p SystemProfile
		if err := yaml.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("parsing %s: %w", full, err)
		}
		if p.Name == "" {
			return fmt.Errorf("%s: missing required field 'name'", full)
		}
		if _, exists := s.Systems[p.Name]; exists {
			return fmt.Errorf("duplicate system profile name: %s", p.Name)
		}
		s.Systems[p.Name] = &p
	}
	return nil
}

// Validate checks cross-profile invariants: every system profile's device
// references point to loaded device profiles. Per-profile validation runs
// at LoadStore time, so a malformed profile fails to load.
func (s *Store) Validate() error {
	for sysName, sys := range s.Systems {
		for _, dev := range sys.Devices {
			if _, ok := s.Devices[dev.Profile]; !ok {
				return fmt.Errorf("system profile %q references unknown device profile %q", sysName, dev.Profile)
			}
		}
	}
	return nil
}
