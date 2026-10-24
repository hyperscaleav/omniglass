package node

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The node's tls endpoints (#1035): the verification mode and the server
// name come off the endpoint's params, the public key pin rides the worklist
// and is trusted on first use, one pin per endpoint is shared by the held
// session and the probe (so neither trusts a key the other refused), a pin
// the operator changes recycles the session, and a certificate refusal gates
// the endpoint at the transport rung: beneath it only the TCP connect runs,
// across a restart too.

const (
	keyFirst  = "SHA256:first"
	keySecond = "SHA256:second"
)

func tlsPollTask(id, endpointID, params, pin string) collection.TaskSpec {
	return collection.TaskSpec{
		ID: id, Mode: "poll", Transport: "tls", EndpointID: endpointID, EndpointName: "tls",
		EndpointParams: json.RawMessage(params),
		Spec: json.RawMessage(`{"driver":"newtron-nvp-tls","function":"status","schedule":{"every":"30s"},
			"request":{"line":"GET INPUT"},
			"emits":[{"name":"video-input","lane":"property","extract":{"regex":"^INPUT (\\S+)$"}}]}`),
		CertificatePin: pin,
	}
}

const pinParams = `{"target":"10.0.0.5:51326","tls-mode":"pin"}`

func TestTLSSessionWantCarriesTheModeAndTheServerName(t *testing.T) {
	dialers := map[string]collection.SessionDialer{"tls": &tlsFakeDialer{}}
	want, refused := desiredSessions([]collection.TaskSpec{tlsPollTask("p1", "ep-1", pinParams, "")}, dialers)
	if len(refused) != 0 {
		t.Fatalf("refused = %v", refused)
	}
	if w := want["ep-1"]; w.tls == nil || w.tls.Mode != driver.TLSModePin || w.tls.ServerName != "10.0.0.5" {
		t.Fatalf("tls want = %+v", w.tls)
	}
	// A driver that declares no mode verifies.
	want, _ = desiredSessions([]collection.TaskSpec{tlsPollTask("p1", "ep-1", `{"target":"panel.example:51326"}`, "")}, dialers)
	if w := want["ep-1"]; w.tls == nil || w.tls.Mode != driver.TLSModeVerify || w.tls.ServerName != "panel.example" {
		t.Fatalf("tls want = %+v, want verify against panel.example", w.tls)
	}
	_, refused = desiredSessions([]collection.TaskSpec{tlsPollTask("p1", "ep-1", `{"target":"10.0.0.5:51326","tls-mode":"trust-me"}`, "")}, dialers)
	if r := refused["ep-1"]; !strings.Contains(r, "trust-me") {
		t.Fatalf("an unknown mode refused = %q, want it named", r)
	}
}

// tlsFakeDialer judges the key it offers as the real dialer does: a pin
// that differs is a certificate refusal, an empty one admits the offered key.
type tlsFakeDialer struct {
	heldFakeDialer
	mu       sync.Mutex
	connects int
	offered  string
	trusts   []collection.TLSTrust
}

func (d *tlsFakeDialer) Connect(ctx context.Context, target string, timeout time.Duration) (collection.PendingConn, error) {
	return d.ConnectTLS(ctx, target, timeout, collection.TLSTrust{Mode: driver.TLSModeVerify})
}

func (d *tlsFakeDialer) ConnectTLS(_ context.Context, target string, _ time.Duration, trust collection.TLSTrust) (collection.PendingConn, error) {
	d.mu.Lock()
	d.connects++
	d.trusts = append(d.trusts, trust)
	offered := d.offered
	d.mu.Unlock()
	if trust.Mode == driver.TLSModePin && trust.Pin != nil {
		if pinned, ok := trust.Pin.Admit(offered); !ok {
			return nil, &collection.TerminalError{Class: driver.ClassCertificate, Pinned: pinned, Offered: offered, Reason: "the certificate's public key changed"}
		}
	}
	return &tlsFakePending{d: d, target: target}, nil
}

func (d *tlsFakeDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connects
}

func (d *tlsFakeDialer) setOffered(fp string) {
	d.mu.Lock()
	d.offered = fp
	d.mu.Unlock()
}

type tlsFakePending struct {
	d      *tlsFakeDialer
	target string
}

func (p *tlsFakePending) Authenticate(context.Context, *collection.SessionAuth, collection.Classifier, time.Duration) error {
	return nil
}

func (p *tlsFakePending) Open(in collection.Inbound, _ collection.ChannelOptions) (collection.HeldConn, error) {
	return p.d.heldFakeDialer.Dial(context.Background(), p.target, 0, in)
}

func (p *tlsFakePending) Close() error { return nil }

type keyLog struct {
	mu   sync.Mutex
	keys []string
}

func (l *keyLog) record(_, _ string, fp string) {
	l.mu.Lock()
	l.keys = append(l.keys, fp)
	l.mu.Unlock()
}

func (l *keyLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.keys...)
}

func TestTLSKeyTrustedOnFirstUsePublishesAndAPinChangeRecycles(t *testing.T) {
	dialer := &tlsFakeDialer{offered: keyFirst}
	log, keys := &transitionLog{}, &keyLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tls": dialer}, sessionHooks{transition: log.record, certificate: keys.record})
	defer m.closeAll()

	m.reconcile([]collection.TaskSpec{tlsPollTask("p1", "ep-1", pinParams, "")})
	log.waitFor(t, "p1", collection.SessionUp)
	if got := keys.all(); len(got) != 1 || got[0] != keyFirst {
		t.Fatalf("published keys = %v, want the key trusted on first use", got)
	}
	// The server delivers the trusted key back as the pin: nothing recycles,
	// and nothing republishes.
	m.reconcile([]collection.TaskSpec{tlsPollTask("p1", "ep-1", pinParams, keyFirst)})
	time.Sleep(20 * time.Millisecond)
	if n := dialer.count(); n != 1 {
		t.Fatalf("delivering the trusted pin recycled the session: %d connects", n)
	}
	if got := keys.all(); len(got) != 1 {
		t.Fatalf("published keys = %v after the server held the pin, want no republish", got)
	}

	// The component is replaced and the operator accepts its new key.
	dialer.setOffered(keySecond)
	m.reconcile([]collection.TaskSpec{tlsPollTask("p1", "ep-1", pinParams, keySecond)})
	waitFor(t, func() bool { return dialer.count() == 2 }, "the operator's new pin recycled the session")
	dialer.mu.Lock()
	last := dialer.trusts[len(dialer.trusts)-1]
	dialer.mu.Unlock()
	if last.Pin == nil || last.Pin.Get() != keySecond {
		t.Fatalf("the recycled session pinned %v, want the operator's key", last.Pin)
	}
}

// The trusted key reaches the server even when its first publish is lost
// (it republishes while the worklist delivers no pin), and the probe shares
// the session's pin: it never trusts a key of its own beside it.
func TestTLSPinSurvivesALostPublishAndIsTheProbesToo(t *testing.T) {
	dialer := &tlsFakeDialer{offered: keyFirst}
	log, keys := &transitionLog{}, &keyLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tls": dialer}, sessionHooks{transition: log.record, certificate: keys.record})
	defer m.closeAll()
	task := tlsPollTask("p1", "ep-1", pinParams, "")
	m.reconcile([]collection.TaskSpec{task})
	log.waitFor(t, "p1", collection.SessionUp)
	m.reconcile([]collection.TaskSpec{task})
	if got := keys.all(); len(got) < 2 {
		t.Fatalf("published the trusted key %d times with the server lacking it, want a republish", len(got))
	}

	prober := &fakeTLSProber{offered: "SHA256:mitm"}
	runner := &collection.Runner{TLS: prober, TCP: &countingTCPDialer{}}
	if _, err := collectProbe(t.Context(), runner, task, m); err != nil {
		t.Fatal(err)
	}
	if got := prober.last(); got.Pin == nil || got.Pin.Get() != keyFirst {
		t.Fatalf("the probe pinned %v, want the session's trusted key", got.Pin)
	}
}

// fakeTLSProber judges its offered key against the pin as the real probe
// does, recording the trust each probe ran under.
type fakeTLSProber struct {
	mu      sync.Mutex
	offered string
	trusts  []collection.TLSTrust
}

func (p *fakeTLSProber) Probe(_ context.Context, _ string, trust collection.TLSTrust, _ time.Duration) (collection.TLSResult, error) {
	p.mu.Lock()
	p.trusts = append(p.trusts, trust)
	offered := p.offered
	p.mu.Unlock()
	res := collection.TLSResult{Dialed: true, Responded: true, Reason: collection.Responded, Facts: collection.TLSFacts{Key: offered, NotAfter: time.Now().Add(240 * time.Hour)}}
	if trust.Mode == driver.TLSModePin && trust.Pin != nil {
		if pinned, ok := trust.Pin.Admit(offered); !ok {
			res.Refusal = &collection.TerminalError{Class: driver.ClassCertificate, Pinned: pinned, Offered: offered, Reason: "the certificate's public key changed"}
			return res, nil
		}
	}
	res.Accepted = true
	return res, nil
}

func (p *fakeTLSProber) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.trusts)
}

func (p *fakeTLSProber) last() collection.TLSTrust {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.trusts[len(p.trusts)-1]
}

// countingTCPDialer answers every connect open, counting them.
type countingTCPDialer struct {
	mu sync.Mutex
	n  int
}

func (d *countingTCPDialer) Dial(context.Context, string, time.Duration) (float64, collection.Reachability, error) {
	d.mu.Lock()
	d.n++
	d.mu.Unlock()
	return 1.5, collection.Responded, nil
}

func (d *countingTCPDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.n
}

// A changed key the probe meets gates the endpoint at the transport rung,
// naming both keys as data; beneath the gate only the TCP connect runs (no
// handshake reaches the new key), and a restarted node honours the gate the
// worklist carries back.
func TestTLSProbeCertificateRefusalGatesTheTransportRung(t *testing.T) {
	prober := &fakeTLSProber{offered: keySecond}
	tcp := &countingTCPDialer{}
	runner := &collection.Runner{TLS: prober, TCP: tcp}
	rej := &rejectionLog{}
	m := newSessionManager(nil, sessionHooks{rejection: rej.record})
	task := withFingerprint(tlsPollTask("r1", "ep-1", pinParams, keyFirst), "fp-1", "")
	task.Spec = json.RawMessage(`{}`) // the reachability probe
	m.reconcile([]collection.TaskSpec{task})
	dps, err := collectProbe(t.Context(), runner, task, m)
	if err != nil {
		t.Fatal(err)
	}
	if prober.calls() != 1 {
		t.Fatalf("the probe ran %d handshakes, want 1", prober.calls())
	}
	for _, d := range dps {
		if d.Name == collection.SignalTLSCertificateExpiry {
			t.Fatal("a refused key landed its certificate's expiry")
		}
	}
	gt, ok := m.blocked("ep-1", rungResponsive)
	if !ok || gt.rung != rungResponsive || gt.class != driver.ClassCertificate {
		t.Fatalf("gate = %+v, %v, want responsive certificate", gt, ok)
	}
	published := ""
	if got := rej.all(); len(got) > 0 {
		published = got[len(got)-1]
	}
	if published != "responsive certificate fp-1 "+keyFirst+" "+keySecond {
		t.Fatalf("published rejection %q, want the rung, class, fingerprint, then the pinned and offered keys", published)
	}

	// Beneath the gate: the connect only, never the handshake.
	before := tcp.count()
	for i := 0; i < 3; i++ {
		m.reconcile([]collection.TaskSpec{task})
		if _, err := collectProbe(t.Context(), runner, task, m); err != nil {
			t.Fatal(err)
		}
	}
	if prober.calls() != 1 || tcp.count() != before+3 {
		t.Fatalf("beneath the gate: %d handshakes, %d connects; want no handshake and a connect a tick", prober.calls(), tcp.count()-before)
	}

	// A restarted node, the rejection carried back: still no handshake.
	restarted := newSessionManager(nil, sessionHooks{})
	carried := withFingerprint(task, "fp-1", published)
	restarted.reconcile([]collection.TaskSpec{carried})
	if _, err := collectProbe(t.Context(), runner, carried, restarted); err != nil {
		t.Fatal(err)
	}
	if prober.calls() != 1 {
		t.Fatal("a restarted node ran a handshake against an unchanged fingerprint")
	}
	gt, ok = restarted.blocked("ep-1", rungResponsive)
	if !ok || gt.refusal().Pinned != keyFirst || gt.refusal().Offered != keySecond {
		t.Fatalf("the carried gate = %+v, want both keys kept", gt)
	}
	// The fingerprint moves (an operator's :retry with the new key): the
	// handshake runs again.
	moved := withFingerprint(tlsPollTask("r1", "ep-1", pinParams, keySecond), "fp-2", published)
	moved.Spec = json.RawMessage(`{}`)
	restarted.reconcile([]collection.TaskSpec{moved})
	if _, err := collectProbe(t.Context(), runner, moved, restarted); err != nil {
		t.Fatal(err)
	}
	if prober.calls() != 2 {
		t.Fatal("a moved fingerprint did not run the handshake again")
	}
}

// A held tls session under a certificate gate starts rejected: a restarted
// node attempts no handshake under the unchanged fingerprint.
func TestATLSSessionUnderACertificateGateStartsRejected(t *testing.T) {
	dialer := &tlsFakeDialer{offered: keySecond}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tls": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()
	carried := "responsive certificate fp-1 " + keyFirst + " " + keySecond
	task := withFingerprint(tlsPollTask("p1", "ep-1", pinParams, keyFirst), "fp-1", carried)
	m.reconcile([]collection.TaskSpec{task})
	log.waitFor(t, "p1", collection.SessionRejected)
	time.Sleep(20 * time.Millisecond)
	if n := dialer.count(); n != 0 {
		t.Fatalf("a gated tls session made %d handshakes", n)
	}
}

func TestGateRendersAndParsesTheRefusedKeys(t *testing.T) {
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.record("ep-1", "fp-a", &collection.TerminalError{Class: driver.ClassCertificate, Pinned: keyFirst, Offered: keySecond, Reason: "changed"})
	published := g.rejectionOf("ep-1")
	if published != "responsive certificate fp-a "+keyFirst+" "+keySecond {
		t.Fatalf("published %q", published)
	}
	restarted := newGates()
	restarted.observe("ep-1", "fp-a", published)
	gt, ok := restarted.blocked("ep-1", rungResponsive)
	if !ok || gt.pinned != keyFirst || gt.offered != keySecond {
		t.Fatalf("parsed back %+v", gt)
	}
	// A refusal that names no key renders the three fields it always has.
	g.record("ep-1", "fp-a", &collection.TerminalError{Class: driver.ClassCertificate, Reason: "the certificate failed verification"})
	if got := g.rejectionOf("ep-1"); got != "responsive certificate fp-a" {
		t.Fatalf("a verification refusal published %q", got)
	}
}

// verifyRefusingProber answers every handshake with a verification refusal.
type verifyRefusingProber struct{ fakeTLSProber }

func (p *verifyRefusingProber) Probe(ctx context.Context, target string, trust collection.TLSTrust, timeout time.Duration) (collection.TLSResult, error) {
	res, _ := p.fakeTLSProber.Probe(ctx, target, trust, timeout)
	res.Accepted = false
	res.Refusal = &collection.TerminalError{Class: driver.ClassCertificate, Reason: "the certificate failed verification: x509: certificate signed by unknown authority"}
	return res, nil
}

// Review of #1035, finding 2: an endpoint with no held session (a bare tls
// endpoint) has no session-reason, so the reason a certificate was refused
// (an unknown authority, a name mismatch, an expiry, each its own fix) is
// self-logged once, when the probe meets it; beneath the gate nothing
// repeats it.
func TestTLSProbeLogsTheRefusalsReasonOnce(t *testing.T) {
	var buf bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prior)

	runner := &collection.Runner{TLS: &verifyRefusingProber{}, TCP: &countingTCPDialer{}}
	m := newSessionManager(nil, sessionHooks{})
	task := withFingerprint(tlsPollTask("r1", "ep-1", `{"target":"10.0.0.5:51326","tls-mode":"verify"}`, ""), "fp-1", "")
	task.Spec = json.RawMessage(`{}`)
	for i := 0; i < 3; i++ {
		m.reconcile([]collection.TaskSpec{task})
		if _, err := collectProbe(t.Context(), runner, task, m); err != nil {
			t.Fatal(err)
		}
	}
	out := buf.String()
	if n := strings.Count(out, "unknown authority"); n != 1 {
		t.Fatalf("the refusal's reason was logged %d times, want once:\n%s", n, out)
	}
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "endpoint=tls") {
		t.Fatalf("the refusal log = %s, want a warning naming the endpoint", out)
	}
}

// A tls pin the worklist delivered and then stops delivering was withdrawn by
// the server: the node forgets it, never republishes it, and the session
// redials trusting on first use.
func TestTLSWithdrawnPinIsForgottenAndTrustsOnFirstUseAgain(t *testing.T) {
	dialer := &tlsFakeDialer{offered: "SHA256:planted"}
	log, keys := &transitionLog{}, &keyLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tls": dialer}, sessionHooks{transition: log.record, certificate: keys.record})
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{tlsPollTask("p1", "ep-1", pinParams, "SHA256:planted")})
	log.waitFor(t, "p1", collection.SessionUp)

	dialer.setOffered(keySecond)
	m.reconcile([]collection.TaskSpec{tlsPollTask("p1", "ep-1", pinParams, "")})
	waitFor(t, func() bool { return dialer.count() == 2 }, "the withdrawn pin recycled the session")
	waitFor(t, func() bool { return slices.Contains(keys.all(), keySecond) }, "the key trusted on first use was published")
	m.reconcile([]collection.TaskSpec{tlsPollTask("p1", "ep-1", pinParams, "")})
	if got := keys.all(); slices.Contains(got, "SHA256:planted") {
		t.Fatalf("the node republished the withdrawn pin as its own key: %v", got)
	}
	dialer.mu.Lock()
	last := dialer.trusts[len(dialer.trusts)-1]
	dialer.mu.Unlock()
	if last.Pin == nil || last.Pin.Get() != keySecond {
		t.Fatalf("the recycled session pinned %v, want the key it trusted on first use", last.Pin)
	}
}
