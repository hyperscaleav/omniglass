// Package settings: schema.go is the single source of truth for the settings
// document. Each namespace is a struct of tagged fields; adding a setting is one
// field. Reflection over Settings (settings.go) builds the code-defaults layer and
// the namespace registry from these tags, so there is no second place to keep in
// sync.
package settings

import (
	"reflect"
	"slices"
)

// Settings is the canonical settings document, one field per namespace. The
// settings tag is "<domain>[,client]": domain is "profile" or "platform", and the
// client token marks a client-visible namespace (fed to /settings/me).
type Settings struct {
	UI          UISettings        `json:"ui" settings:"profile,client"`
	Keybindings Keybindings       `json:"keybindings" settings:"profile,client"`
	Label       LabelSettings     `json:"label" settings:"platform,client"`
	Retention   RetentionSettings `json:"retention" settings:"platform,client"`
	// Collection is how the install's collection reports on itself (#1369):
	// platform domain, client-visible, as retention is, since a chart of a
	// collection telemetry series reads its window.
	Collection CollectionSettings `json:"collection" settings:"platform,client"`
	// Notification is the outbound lane's install-wide policy (#618): platform
	// domain and NOT client-visible, since it names where the platform may send
	// (an admin's business) rather than how anything renders.
	Notification NotificationSettings `json:"notification" settings:"platform"`
	// Database is how the server watches its own queries (#1627): platform
	// domain and not client-visible, an admin's tuning of the server's log.
	Database DatabaseSettings `json:"database" settings:"platform"`
	// Agent is what an agent may do on a person's behalf (#2063): platform
	// domain and not client-visible; a caller reads its own resolved policy
	// from /auth/me/agent-policy instead.
	Agent AgentSettings `json:"agent" settings:"platform"`
}

// UISettings is the ui namespace.
type UISettings struct {
	Theme          string `json:"theme" enum:"omniglass-dark,omniglass-light" default:"omniglass-dark" doc:"Console color theme"`
	DefaultLanding string `json:"default_landing" pattern:"^/" default:"/" doc:"Route the console opens to (an absolute path)"`
}

// Keybindings is the keymap namespace: a closed set of developer-defined actions.
type Keybindings struct {
	OpenDetail     string `json:"open_detail" default:"d" doc:"Open the detail blade"`
	OpenEdit       string `json:"open_edit" default:"e" doc:"Open the edit pane"`
	CloseBlade     string `json:"close_blade" default:"Escape" doc:"Close the top blade"`
	CommandPalette string `json:"command_palette" default:"mod+k" doc:"Open the command palette"`
}

// LabelSettings is the label namespace: how the label rule engine renders, as
// opposed to WHAT it renders (the rules themselves are per-tier columns on the
// catalogs, not a setting).
//
// It is the first PLATFORM-domain namespace, and the first that is platform and
// client-visible at once. The two halves say different things and both are
// wanted here: platform domain means one install-wide list an admin owns, never
// cascading to a group or a user, because a label is stored once and read by
// everybody, so a per-user dictionary would be a per-user spelling of a shared
// row. Client-visible means any authenticated caller may READ the effective
// list, which is what lets the console render a label the same way the server
// did without asking for the admin settings read.
type LabelSettings struct {
	// Acronyms is the dictionary the rule engine's `title` function consults.
	// Matching is whole-word and case-insensitive, and the entry is the form
	// emitted, so "PoE" titles "poe" and "Poe" alike as "PoE".
	//
	// The tag is huma's comma-separated array form, which coerceDefault parses
	// into the same list huma reflects into the schema.
	//
	// nullable:"false" turns off huma's default array nullability. A list-valued
	// setting is empty or populated, never null: null is the merge patch's delete
	// sentinel, and a schema that admitted it would be inviting a write that means
	// something else entirely.
	Acronyms []string `json:"acronyms" nullable:"false" default:"AV,BYOD,DSP,EDID,HDBaseT,HDCP,HDMI,IP,IR,KVM,LAN,LCD,LED,MTR,NDI,NVR,OLED,PC,PoE,PTZ,RF,SDI,SIP,TV,UC,USB,VC,VLAN,4K,8K" doc:"Words a generated label title-cases to a fixed form, whole-word and case-insensitive"`
}

// NotificationSettings is the notification namespace (#618).
type NotificationSettings struct {
	// ConsoleURL is the console's base URL as a person outside the install
	// reaches it, the prefix of the Link fact a notification template reads
	// (a component's page). Empty leaves Link empty: the server cannot know the
	// name it is reached by behind a proxy.
	ConsoleURL string `json:"console_url" default:"" doc:"The console's base URL as a receiver of a notification reaches it (https://omniglass.example.com); the prefix of the Link a template reads. Empty leaves Link empty"`
	// EgressAllow opens internal destinations to the outbound client
	// (internal/egress): each entry a CIDR, an address, or a host name. A
	// private, loopback or link-local destination is refused unless named
	// here; a cloud metadata address is refused even when it is. Plain http
	// is admitted only to a destination named here.
	EgressAllow []string `json:"egress_allow" nullable:"false" default:"" doc:"The internal destinations a notification may reach, each a CIDR (10.20.0.0/16), an address, or a host name (chat.corp.example). Everything private, loopback or link-local is refused unless named here; a cloud metadata address is refused even when it is; plain http is admitted only to a destination named here"`
	// The mail relay (#1317): where an email notification is handed over, as
	// host:port, judged by the same egress contract a webhook is. Empty turns
	// email off: an email row then fails with the reason. MailSecret names a
	// platform basic-auth secret the session authenticates with; binding it
	// takes secret:bind on it, as binding a target's secret does.
	MailRelay    string `json:"mail_relay" default:"" doc:"The SMTP relay email notifications go through, host:port (smtp.example.com:587). Empty turns email off. An internal relay is reached only when egress_allow names it"`
	MailSecurity string `json:"mail_security" enum:"starttls,tls,none" default:"starttls" doc:"How the session to the relay is protected: starttls (the relay must offer it), tls (implicit TLS, port 465), or none, admitted only for a relay egress_allow names (a lab's mail sim)"`
	MailFrom     string `json:"mail_from" default:"" doc:"The From address of an email notification (Omniglass <alerts@example.com>)"`
	MailSecret   string `json:"mail_secret" default:"" doc:"A platform basic-auth secret, by name, the relay session authenticates with; empty sends without authenticating. Setting it, or moving mail_relay while one is set, takes secret:bind on that secret"`
}

// RetentionSettings is the retention namespace (#417): how long each sample
// lane is kept before the server's retention pass removes it. Platform domain,
// because a window is a fact about the install's storage; client-visible,
// because a window is no secret and a chart of a series' history can say how
// far back it reaches.
//
// A window is whole days, from 1 to RetentionMaxDays: a prune keeps every
// declared row and the latest row of every series however old (ADR-0080), so a
// window bounds history, not current values. The ceiling is a century; a cutoff
// much further back overflows the timestamp encoding and reaches the database as
// a date in the far future, which every row is older than. One default per lane;
// a per-type override is a later slice of #417.
//
// A log line (a component's log_line, an outpost's outpost_log) is kept by its
// severity's band (logline.RetentionBands): four windows rather than eight, and
// a line with no severity is kept as long as info.
//
// An outpost's own logs may keep a band for a window of their own (#1350): the
// outpost_log_* settings declare no default, so until an admin sets one it is
// absent from the effective document, reads as zero here, and outpost_log follows
// the shared band (OutpostLogDays). Restoring one (a null in the merge patch)
// returns outpost_log to the shared window. A set value is held to the same range
// as every other window.
type RetentionSettings struct {
	MetricDays     int `json:"metric_days" minimum:"1" maximum:"36500" default:"14" doc:"Days a metric sample is kept, unless its type keeps its own (a metric type's retention_days); a sample on the lane's window leaves with its UTC day, up to a day later"`
	PropertyDays   int `json:"property_days" minimum:"1" maximum:"36500" default:"90" doc:"Days a property transition is kept, unless its type keeps its own (a property type's retention_days)"`
	TrendDays      int `json:"trend_days" minimum:"1" maximum:"36500" default:"730" doc:"Days an hourly metric trend is kept (#1276)"`
	DebugLogDays   int `json:"log_debug_days" minimum:"1" maximum:"36500" default:"3" doc:"Days a debug log line is kept"`
	InfoLogDays    int `json:"log_info_days" minimum:"1" maximum:"36500" default:"14" doc:"Days an info or notice log line, or one with no severity, is kept"`
	WarningLogDays int `json:"log_warning_days" minimum:"1" maximum:"36500" default:"30" doc:"Days a warning log line is kept"`
	ErrorLogDays   int `json:"log_error_days" minimum:"1" maximum:"36500" default:"90" doc:"Days an error, critical, alert or emergency log line is kept"`

	OutpostDebugLogDays   int `json:"outpost_log_debug_days,omitempty" minimum:"1" maximum:"36500" doc:"Days an outpost's own debug log line is kept; unset follows log_debug_days"`
	OutpostInfoLogDays    int `json:"outpost_log_info_days,omitempty" minimum:"1" maximum:"36500" doc:"Days an outpost's own info or notice log line, or one with no severity, is kept; unset follows log_info_days"`
	OutpostWarningLogDays int `json:"outpost_log_warning_days,omitempty" minimum:"1" maximum:"36500" doc:"Days an outpost's own warning log line is kept; unset follows log_warning_days"`
	OutpostErrorLogDays   int `json:"outpost_log_error_days,omitempty" minimum:"1" maximum:"36500" doc:"Days an outpost's own error, critical, alert or emergency log line is kept; unset follows log_error_days"`
}

// CollectionSettings is the collection namespace (#1369): the platform
// defaults of the collection cadence model (#1373), which an operator's
// override and a driver's declaration take precedence over once they exist.
//
// ReportSeconds is the window each endpoint's collection telemetry covers:
// the outpost aggregates its polls' durations and failures and its listeners'
// claimed and unclaimed pushes over it, and the server its webhook posts, so
// the telemetry costs a few rows per endpoint per window however fast a
// driver polls. The outpost reads it off its worklist, at heartbeat granularity.
type CollectionSettings struct {
	ReportSeconds int `json:"report_seconds" minimum:"10" maximum:"3600" default:"60" doc:"Seconds each endpoint's collection telemetry covers: its polls' mean duration and failures, and the pushes its listeners claimed or did not, reported once a window"`
}

// DatabaseSettings is the database namespace (#1627): how the server watches
// its own queries.
//
// SlowQueryMS is the threshold past which a query logs a WARN with its
// statement's fingerprint and duration (never its arguments) and counts
// toward the server's db-slow-query-count. Zero turns the log off. The server
// re-reads it each report window, so a change takes without a restart.
type DatabaseSettings struct {
	SlowQueryMS int `json:"slow_query_ms" minimum:"0" maximum:"600000" default:"500" doc:"Milliseconds a query may run before the server logs it as slow (its statement's shape and duration, never its values) and counts it; 0 turns the log off"`
}

// AgentSettings is the agent namespace (#2063): the install's policy for each
// class of operation an agent (an agent token, or any request over MCP) may
// call. always lets it run; ask lets it run and tells the client to confirm
// with the person first; never refuses it at the server, on every channel.
// Every class but read defaults to ask, so a fresh install refuses nothing.
type AgentSettings struct {
	Read                  string `json:"read" enum:"always,ask,never" default:"always" doc:"Policy for an agent's reads (a GET, or a POST gated on a read permission)"`
	Write                 string `json:"write" enum:"always,ask,never" default:"ask" doc:"Policy for an agent's writes that create or act (a POST that is not a read)"`
	Destructive           string `json:"destructive" enum:"always,ask,never" default:"ask" doc:"Policy for an agent's writes that overwrite or delete (a PATCH, PUT or DELETE)"`
	CredentialDestination string `json:"credential_destination" enum:"always,ask,never" default:"ask" doc:"Policy for an agent's calls that may point a stored credential somewhere (any operation gated on secret:bind)"`
	// Roles and Tools narrow the class policies above (#2088); neither widens.
	Roles map[string]AgentClassOverride `json:"roles,omitempty" doc:"Per-role overrides, keyed by role name: a class set here applies to the agents of anyone granted the role, when it is stricter"`
	Tools map[string]string             `json:"tools,omitempty" doc:"Per-tool overrides, keyed by operation ID (an MCP tool's name): always, ask or never, applied on top of the tool's class when stricter"`
}

// AgentClassOverride is one role's override of the class policies (#2088). An
// unset class leaves the platform's value.
type AgentClassOverride struct {
	Read                  string `json:"read,omitempty" enum:"always,ask,never" doc:"Policy for this role's agents' reads"`
	Write                 string `json:"write,omitempty" enum:"always,ask,never" doc:"Policy for this role's agents' writes that create or act"`
	Destructive           string `json:"destructive,omitempty" enum:"always,ask,never" doc:"Policy for this role's agents' writes that overwrite or delete"`
	CredentialDestination string `json:"credential_destination,omitempty" enum:"always,ask,never" doc:"Policy for this role's agents' calls that may point a stored credential somewhere"`
}

func (o AgentClassOverride) policy(class string) string {
	switch class {
	case "read":
		return o.Read
	case "write":
		return o.Write
	case "destructive":
		return o.Destructive
	case "credential_destination":
		return o.CredentialDestination
	}
	return ""
}

// The three agent policies.
const (
	AgentAlways = "always"
	AgentAsk    = "ask"
	AgentNever  = "never"
)

// The layers an agent policy is resolved from (#2088).
const (
	AgentSourcePlatform = "platform"
	AgentSourceRole     = "role"
	AgentSourceTool     = "tool"
)

// AgentDecision is a resolved agent policy and the layer that set it; Role
// names the role when the layer is a role's override.
type AgentDecision struct {
	Policy string
	Source string
	Role   string
}

// agentStrictness orders the policies: never is stricter than ask, ask than
// always. An unknown policy ranks with ask.
func agentStrictness(p string) int {
	switch p {
	case AgentAlways:
		return 0
	case AgentNever:
		return 2
	}
	return 1
}

// ResolveClass is the policy for one class for a person granted roles: the
// strictest of the platform's value and each held role's override. A tie
// keeps the earlier layer, the platform before any role and roles by name, so
// the named source is the one an admin would have to change.
func (a AgentSettings) ResolveClass(class string, roles []string) AgentDecision {
	d := AgentDecision{Policy: a.Policy(class), Source: AgentSourcePlatform}
	held := append([]string(nil), roles...)
	slices.Sort(held)
	for _, r := range slices.Compact(held) {
		o, ok := a.Roles[r]
		if !ok {
			continue
		}
		if p := o.policy(class); p != "" && agentStrictness(p) > agentStrictness(d.Policy) {
			d = AgentDecision{Policy: p, Source: AgentSourceRole, Role: r}
		}
	}
	return d
}

// ResolveTool is the policy for one tool of a class: its class's resolved
// policy, narrowed by the tool's override when that is stricter.
func (a AgentSettings) ResolveTool(tool, class string, roles []string) AgentDecision {
	d := a.ResolveClass(class, roles)
	if p, ok := a.Tools[tool]; ok && agentStrictness(p) > agentStrictness(d.Policy) {
		d = AgentDecision{Policy: p, Source: AgentSourceTool}
	}
	return d
}

// Policy is the policy for one operation class, falling back to the class's
// default when unset (a zero value, as the unimplemented gateway returns), and
// to ask for a class it does not know.
func (a AgentSettings) Policy(class string) string {
	var v, def string
	switch class {
	case "read":
		v, def = a.Read, AgentAlways
	case "write":
		v, def = a.Write, AgentAsk
	case "destructive":
		v, def = a.Destructive, AgentAsk
	case "credential_destination":
		v, def = a.CredentialDestination, AgentAsk
	default:
		return AgentAsk
	}
	if v == "" {
		return def
	}
	return v
}

// LogDays is the window for one log retention band; 0 for an unknown band.
func (r RetentionSettings) LogDays(band string) int {
	switch band {
	case "debug":
		return r.DebugLogDays
	case "info":
		return r.InfoLogDays
	case "warning":
		return r.WarningLogDays
	case "error":
		return r.ErrorLogDays
	}
	return 0
}

// OutpostLogDays is the window for one band of an outpost's own logs (outpost_log): the
// outpost override when an admin set one, else the shared band (LogDays).
func (r RetentionSettings) OutpostLogDays(band string) int {
	override := map[string]int{
		"debug":   r.OutpostDebugLogDays,
		"info":    r.OutpostInfoLogDays,
		"warning": r.OutpostWarningLogDays,
		"error":   r.OutpostErrorLogDays,
	}[band]
	if override != 0 {
		return override
	}
	return r.LogDays(band)
}

// RetentionMaxDays is the longest retention window, the schema's maximum above.
const RetentionMaxDays = 36500

// namespaceType indexes each namespace's json name to its sub-struct type, for the
// write validator (validate.go). Built once by reflecting Settings.
var namespaceType = func() map[string]reflect.Type {
	m := map[string]reflect.Type{}
	t := reflect.TypeOf(Settings{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		m[jsonName(f)] = f.Type
	}
	return m
}()

// jsonName returns a struct field's json name (the tag before any comma, else the
// Go field name).
func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	if i := indexByte(tag, ','); i >= 0 {
		tag = tag[:i]
	}
	return tag
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
