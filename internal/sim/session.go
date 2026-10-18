package sim

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// SessionConfig is the per-interface persistent-session parameters,
// pre-compiled at engine startup. Wire-format quirks (LineEnding,
// FrameStart, FrameEnd, StripChecksumBytes) describe the protocol's
// shape on the wire and apply uniformly across SSH, Telnet, and
// raw TCP transports, the listener handles handshake and hands a
// Reader+Writer to runShellSession, which honors these fields.
// Limits is always populated via profile.SessionLimits.WithDefaults so
// the registry can rely on non-zero values.
type SessionConfig struct {
	Protocol           string
	Port               int
	LineEnding         string
	FrameStart         string
	FrameEnd           string
	StripChecksumBytes int
	Welcome            string
	Subjects           []*sessionSubject
	Limits             profile.SessionLimits
}

// ReadDelim returns the byte to pass to bufio.Reader.ReadBytes for
// inbound framing. We split on the LAST byte of LineEnding; the
// caller then trims the full LineEnding from the end. Multi-byte
// terminators (e.g. "\r\n") work as long as the last byte is unique
// in normal traffic, which holds for every AV protocol the lab
// targets.
func (c *SessionConfig) ReadDelim() byte {
	if c.LineEnding == "" {
		return '\n'
	}
	return c.LineEnding[len(c.LineEnding)-1]
}

// WrapOutbound wraps a payload in FrameStart / FrameEnd brackets and
// appends LineEnding. Used for command responses, push lines, the
// welcome banner, and the bye marker. payload is whatever the
// profile template rendered (or the shell-builtin produced); this
// function adds the wire envelope.
//
// FrameStart / FrameEnd are skipped on the welcome banner specifically
// (callers that need a bare line use WriteWelcome instead), the
// banner is operator-facing greeting text, not a protocol frame.
func (c *SessionConfig) WrapOutbound(payload string) string {
	var b strings.Builder
	b.Grow(len(c.FrameStart) + len(payload) + len(c.FrameEnd) + len(c.LineEnding))
	b.WriteString(c.FrameStart)
	b.WriteString(payload)
	b.WriteString(c.FrameEnd)
	b.WriteString(c.LineEnding)
	return b.String()
}

// StripInbound removes LineEnding, the trailing checksum bytes,
// FrameEnd, and FrameStart from a raw inbound frame. Returns the
// payload bytes ready for command-match dispatch. Order matters
// because the typical wire layout is
// FrameStart + payload + FrameEnd + Checksum + LineEnding, so we
// peel from the outside in: LineEnding, then checksum, then
// FrameEnd, then FrameStart. Anything that fails to match (e.g. no
// FrameStart present) is silently passed through, robust against
// half-formed inbound from a misbehaving client.
func (c *SessionConfig) StripInbound(raw []byte) []byte {
	if len(c.LineEnding) > 0 {
		raw = bytesTrimSuffix(raw, c.LineEnding)
	}
	if c.StripChecksumBytes > 0 && len(raw) >= c.StripChecksumBytes {
		raw = raw[:len(raw)-c.StripChecksumBytes]
	}
	if len(c.FrameEnd) > 0 {
		raw = bytesTrimSuffix(raw, c.FrameEnd)
	}
	if len(c.FrameStart) > 0 {
		raw = bytesTrimPrefix(raw, c.FrameStart)
	}
	return raw
}

func bytesTrimSuffix(b []byte, suf string) []byte {
	if suf == "" || len(b) < len(suf) {
		return b
	}
	if string(b[len(b)-len(suf):]) == suf {
		return b[:len(b)-len(suf)]
	}
	return b
}

func bytesTrimPrefix(b []byte, pre string) []byte {
	if pre == "" || len(b) < len(pre) {
		return b
	}
	if string(b[:len(pre)]) == pre {
		return b[len(pre):]
	}
	return b
}

// CompileSessionConfig pre-compiles an Interface's session block. Returns
// nil when the interface is not Persistent so callers can switch on a
// nil result to take the legacy one-shot code path.
func CompileSessionConfig(iface *profile.Interface) (*SessionConfig, error) {
	if !iface.Persistent {
		return nil, nil
	}
	cfg := &SessionConfig{
		Protocol:           iface.Protocol,
		Port:               iface.Port,
		LineEnding:         iface.LineEnding,
		FrameStart:         iface.FrameStart,
		FrameEnd:           iface.FrameEnd,
		StripChecksumBytes: iface.StripChecksumBytes,
		Welcome:            iface.Welcome,
		Limits:             iface.Limits.WithDefaults(),
	}
	if cfg.LineEnding == "" {
		cfg.LineEnding = "\r\n"
	}
	if cfg.StripChecksumBytes < 0 {
		return nil, fmt.Errorf("strip_checksum_bytes must be >= 0, got %d", cfg.StripChecksumBytes)
	}
	for i := range iface.Subjects {
		s, err := compileSessionSubject(&iface.Subjects[i])
		if err != nil {
			return nil, err
		}
		cfg.Subjects = append(cfg.Subjects, s)
	}
	return cfg, nil
}

// sessionSubject is a Subject with its push template pre-compiled.
type sessionSubject struct {
	def      *profile.Subject
	pushTmpl *template.Template
	pathTmpl *template.Template // nil when Path is a literal
}

func compileSessionSubject(s *profile.Subject) (*sessionSubject, error) {
	if s.Match == "" {
		return nil, fmt.Errorf("subject: match is required")
	}
	if s.PushFormat == "" {
		return nil, fmt.Errorf("subject %q: push_format is required", s.Match)
	}
	t, err := template.New("push:" + s.Match).Funcs(templateFuncs).Parse(s.PushFormat)
	if err != nil {
		return nil, fmt.Errorf("subject %q push_format: %w", s.Match, err)
	}
	out := &sessionSubject{def: s, pushTmpl: t}
	if strings.Contains(s.Path, "{{") {
		pt, err := template.New("path:" + s.Match).Funcs(templateFuncs).Parse(s.Path)
		if err != nil {
			return nil, fmt.Errorf("subject %q path: %w", s.Match, err)
		}
		out.pathTmpl = pt
	}
	return out, nil
}

// matches returns true if subject is covered by this Subject's Match
// glob. Wildcard `*` is per-segment: each `*` segment matches exactly
// one dot-separated segment of subject. The trailing `**` is special:
// it matches one-or-more remaining segments. Examples:
//
//	"status.channels.*.level"   matches "status.channels.0.level"
//	"status.channels.0.*"       matches "status.channels.0.level" but not deeper
//	"status.**"                 matches "status.x" and "status.x.y.z"
//	"*"                         matches a single-segment subject
//	"**"                        matches anything (catch-all)
func (s *sessionSubject) matches(subject string) bool {
	pat := s.def.Match
	if pat == "**" {
		return true
	}
	patSegs := strings.Split(pat, ".")
	subSegs := strings.Split(subject, ".")
	for i, ps := range patSegs {
		if ps == "**" {
			return true
		}
		if i >= len(subSegs) {
			return false
		}
		if ps == "*" {
			continue
		}
		if ps != subSegs[i] {
			return false
		}
	}
	return len(patSegs) == len(subSegs)
}

// resolvePath returns the state path for the rendered subject. When
// the Subject's Path is a literal (no template syntax) it's returned
// verbatim; when Path templates `.Subject` it's rendered against the
// rendered subject string. Empty Path falls through to the subject
// string itself, useful for the catch-all subject pattern.
func (s *sessionSubject) resolvePath(subject string) string {
	if s.def.Path == "" {
		return subject
	}
	if s.pathTmpl == nil {
		return s.def.Path
	}
	var buf bytes.Buffer
	if err := s.pathTmpl.Execute(&buf, struct{ Subject string }{Subject: subject}); err != nil {
		return s.def.Path
	}
	return buf.String()
}

// SessionEvent is the simulator-side event a session emits for the
// sim run mode's hit reporter to consume.
// Kind is one of "open", "close", "push".
type SessionEvent struct {
	Kind      string
	SessionID string
	Protocol  string
	Port      int
	Remote    string
	DeviceID  string
	Hostname  string
	Subject   string
	Value     any
}

// SessionEventPublisher receives session lifecycle and push events.
// Implementations must not block the caller. Nil is safe (events are
// suppressed; sessions still work).
type SessionEventPublisher func(SessionEvent)

// SessionRegistry holds all open sessions for a device's Persistent
// interface and fans out state-change diffs to subscribed sessions.
//
// Concurrency: Open / Close / Subscribe / Unsubscribe each take their
// own mutexes. Fanout is called from the device's mutate path while
// the device's write lock is already held, so it must not call back
// into device methods that take the lock.
type SessionRegistry struct {
	device *Device
	cfg    *SessionConfig
	bus    SessionEventPublisher

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewSessionRegistry returns a registry bound to a device + cfg. cfg
// must be non-nil; bus may be nil.
func NewSessionRegistry(d *Device, cfg *SessionConfig, bus SessionEventPublisher) *SessionRegistry {
	return &SessionRegistry{
		device:   d,
		cfg:      cfg,
		bus:      bus,
		sessions: make(map[string]*Session),
	}
}

// SetBus swaps the event publisher on a live registry. Used by
// Engine.SetSessionPublisher to retroactively wire registries that
// were constructed before the publisher was attached. Safe to call
// at any time; ongoing Open/Close emit through whichever publisher
// is current at the moment of the call.
func (r *SessionRegistry) SetBus(bus SessionEventPublisher) {
	r.mu.Lock()
	r.bus = bus
	r.mu.Unlock()
}

// Open creates and registers a new session. Returns an error when the
// per-device cap is exceeded; the caller should reject the connection.
func (r *SessionRegistry) Open(remote string) (*Session, error) {
	r.mu.Lock()
	if len(r.sessions) >= r.cfg.Limits.MaxSessionsPerDevice {
		r.mu.Unlock()
		return nil, fmt.Errorf("session cap reached (%d)", r.cfg.Limits.MaxSessionsPerDevice)
	}
	id := newSessionID()
	s := &Session{
		ID:       id,
		Remote:   remote,
		registry: r,
		Out:      make(chan []byte, r.cfg.Limits.OutBuffer),
		Subs:     make(map[string]*Subscription),
		closed:   make(chan struct{}),
	}
	r.sessions[id] = s
	r.mu.Unlock()

	if r.bus != nil {
		r.bus(SessionEvent{
			Kind:      "open",
			SessionID: id,
			Protocol:  r.cfg.Protocol,
			Port:      r.cfg.Port,
			Remote:    remote,
			DeviceID:  r.device.DeviceID,
			Hostname:  r.device.Hostname,
		})
	}
	return s, nil
}

// Close drops a session, cancels all interval pumps, and emits a close
// event. Idempotent: closing a session twice is a no-op.
func (r *SessionRegistry) Close(s *Session) {
	r.mu.Lock()
	if _, ok := r.sessions[s.ID]; !ok {
		r.mu.Unlock()
		return
	}
	delete(r.sessions, s.ID)
	r.mu.Unlock()

	s.closeOnce.Do(func() {
		s.cancelAllSubs()
		close(s.closed)
	})

	if r.bus != nil {
		r.bus(SessionEvent{
			Kind:      "close",
			SessionID: s.ID,
			Protocol:  r.cfg.Protocol,
			Port:      r.cfg.Port,
			Remote:    s.Remote,
			DeviceID:  r.device.DeviceID,
			Hostname:  r.device.Hostname,
		})
	}
}

// SessionCount returns the number of currently open sessions.
func (r *SessionRegistry) SessionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

// Fanout publishes push lines for every active subscription whose
// state path is touched by `diffs`. Caller MUST hold the device's
// write lock; Fanout reads state via diffs and does not re-acquire
// the lock.
func (r *SessionRegistry) Fanout(diffs map[string]any) {
	if len(diffs) == 0 {
		return
	}
	r.mu.Lock()
	sessions := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()

	for _, sess := range sessions {
		sess.subsMu.RLock()
		subs := make([]*Subscription, 0, len(sess.Subs))
		for _, sub := range sess.Subs {
			subs = append(subs, sub)
		}
		sess.subsMu.RUnlock()

		for _, sub := range subs {
			value, present := diffs[sub.path]
			if !present {
				continue
			}
			sess.publish(sub, value)
		}
	}
}

// Session represents a single open shell connection.
type Session struct {
	ID        string
	Remote    string
	Out       chan []byte
	registry  *SessionRegistry
	closed    chan struct{}
	closeOnce sync.Once

	subsMu sync.RWMutex
	Subs   map[string]*Subscription

	// holdMu guards a hold on this session's pushes, taken while a command
	// the session sent is running, so its reply reaches the wire before the
	// push the command's own change causes.
	holdMu  sync.Mutex
	holding bool
	held    []heldPush
}

type heldPush struct {
	line  []byte
	sub   *Subscription
	value any
}

// Subscription is one live subject watch on a session.
type Subscription struct {
	subject  string
	path     string
	token    string
	def      *sessionSubject
	interval time.Duration
	cancel   context.CancelFunc
}

// Closed returns a channel that fires when the session is closed.
func (s *Session) Closed() <-chan struct{} { return s.closed }

// Subscribe registers a subscription to subject. token overrides the
// Subject's default PublishToken when non-empty. intervalMs > 0
// spawns an interval pump that emits every intervalMs (floored at the
// SessionLimits PublishMinIntervalMs); 0 means on-change only via
// Fanout. Re-subscribing to the same subject is a no-op (idempotent).
func (s *Session) Subscribe(subject, token string, intervalMs int) error {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return fmt.Errorf("subject is required")
	}

	def := s.registry.lookupSubject(subject)
	if def == nil {
		return fmt.Errorf("no subject matches %q", subject)
	}

	s.subsMu.Lock()
	if _, exists := s.Subs[subject]; exists {
		s.subsMu.Unlock()
		return nil
	}
	if len(s.Subs) >= s.registry.cfg.Limits.MaxSubsPerSession {
		s.subsMu.Unlock()
		return fmt.Errorf("subscription cap reached (%d)", s.registry.cfg.Limits.MaxSubsPerSession)
	}

	resolvedToken := token
	if resolvedToken == "" {
		resolvedToken = def.def.PublishToken
	}

	sub := &Subscription{
		subject: subject,
		path:    def.resolvePath(subject),
		token:   resolvedToken,
		def:     def,
	}

	floor := time.Duration(s.registry.cfg.Limits.PublishMinIntervalMs) * time.Millisecond
	if intervalMs > 0 {
		d := time.Duration(intervalMs) * time.Millisecond
		if d < floor {
			d = floor
		}
		sub.interval = d
		ctx, cancel := context.WithCancel(context.Background())
		sub.cancel = cancel
		go s.intervalPump(ctx, sub)
	}

	s.Subs[subject] = sub
	s.subsMu.Unlock()
	return nil
}

// Unsubscribe drops a subscription. No-op if not present.
func (s *Session) Unsubscribe(subject string) {
	s.subsMu.Lock()
	sub, ok := s.Subs[subject]
	if !ok {
		s.subsMu.Unlock()
		return
	}
	delete(s.Subs, subject)
	s.subsMu.Unlock()
	if sub.cancel != nil {
		sub.cancel()
	}
}

// Subjects returns the currently subscribed subject strings.
func (s *Session) Subjects() []string {
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()
	out := make([]string, 0, len(s.Subs))
	for k := range s.Subs {
		out = append(out, k)
	}
	return out
}

func (s *Session) cancelAllSubs() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for _, sub := range s.Subs {
		if sub.cancel != nil {
			sub.cancel()
		}
	}
	s.Subs = map[string]*Subscription{}
}

// publish formats a push line and tries to enqueue it. On a full Out
// channel the session is closed: subscriptions care about every line
// and silent loss would mask the failure. Closing happens in a
// goroutine to avoid re-entering registry.Close from a Fanout caller
// that is already holding the device's write lock.
func (s *Session) publish(sub *Subscription, value any) {
	line, err := s.renderPush(sub, value)
	if err != nil {
		return
	}
	s.holdMu.Lock()
	if s.holding {
		s.held = append(s.held, heldPush{line: line, sub: sub, value: value})
		s.holdMu.Unlock()
		return
	}
	s.holdMu.Unlock()
	s.emit(line, sub, value)
}

// holdPushes defers this session's pushes until releasePushes.
func (s *Session) holdPushes() {
	s.holdMu.Lock()
	s.holding = true
	s.holdMu.Unlock()
}

// releasePushes sends the pushes held since holdPushes, in order.
func (s *Session) releasePushes() {
	s.holdMu.Lock()
	held := s.held
	s.held, s.holding = nil, false
	s.holdMu.Unlock()
	for _, h := range held {
		s.emit(h.line, h.sub, h.value)
	}
}

// emit enqueues a rendered push and reports it.
func (s *Session) emit(line []byte, sub *Subscription, value any) {
	select {
	case s.Out <- line:
		if pub := s.registry.bus; pub != nil {
			pub(SessionEvent{
				Kind:      "push",
				SessionID: s.ID,
				Protocol:  s.registry.cfg.Protocol,
				Port:      s.registry.cfg.Port,
				Remote:    s.Remote,
				DeviceID:  s.registry.device.DeviceID,
				Hostname:  s.registry.device.Hostname,
				Subject:   sub.subject,
				Value:     value,
			})
		}
	default:
		go s.registry.Close(s)
	}
}

func (s *Session) renderPush(sub *Subscription, value any) ([]byte, error) {
	data := struct {
		Subject  string
		Path     string
		Token    string
		Value    string // JSON-encoded: typed and unambiguous
		Raw      string // the bare value, for line protocols that print it as is
		Hostname string
		DeviceID string
	}{
		Subject:  sub.subject,
		Path:     sub.path,
		Token:    sub.token,
		Value:    encodePushValue(value),
		Raw:      rawPushValue(value),
		Hostname: s.registry.device.Hostname,
		DeviceID: s.registry.device.DeviceID,
	}
	var buf bytes.Buffer
	if err := sub.def.pushTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	// Wrap the rendered payload in the same wire envelope used for
	// command responses: FrameStart + payload + FrameEnd + LineEnding.
	// Push lines must conform to the device's wire format exactly the
	// way responses do, otherwise a Q-SYS-style NUL-terminated client
	// would never see them framed correctly.
	return []byte(s.registry.cfg.WrapOutbound(buf.String())), nil
}

// encodePushValue renders a state value into the wire-friendly text
// the push template references via {{ .Value }}. JSON-encodes
// everything so numbers, booleans, and strings all come out
// unambiguously typed; callers that want to embed quoted strings
// directly can do so without a separate quote step.
// rawPushValue renders a value bare: a string as itself, anything else as
// its JSON form.
func rawPushValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return encodePushValue(v)
}

func encodePushValue(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// intervalPump emits one push per tick. Ticker is stopped on session
// close or Unsubscribe. State reads briefly take the device's read
// lock, fine because the mutator only takes the write lock for the
// brief duration of an applyMutations call, and the read lock is
// released between ticks.
func (s *Session) intervalPump(ctx context.Context, sub *Subscription) {
	t := time.NewTicker(sub.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.closed:
			return
		case <-t.C:
			value := s.registry.device.ReadPath(sub.path)
			s.publish(sub, value)
		}
	}
}

func (r *SessionRegistry) lookupSubject(subject string) *sessionSubject {
	for _, s := range r.cfg.Subjects {
		if s.matches(subject) {
			return s
		}
	}
	return nil
}

// newSessionID returns a 16-hex-char random ID. Collision-resistant for
// any plausible session count.
func newSessionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
