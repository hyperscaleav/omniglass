package collection

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The TLS dial primitive (#1035, ADR-0159). One client handshake over a
// connection already made, under one of three modes: verify the certificate
// against the node's roots with the server name from the host input, pin the
// public key on first use, or (the operator's explicit choice) skip every
// check. It reports what the handshake observed: the offered public key's
// fingerprint and the certificate's not-after. A refusal retrying cannot fix
// (a changed key, a certificate that fails verification) is a TerminalError
// of class certificate carrying its reason; anything else (a timeout, a port
// that does not speak TLS) stays retryable.
//
// The line protocol over tls consumes it first (the tls transport's held
// session and its probe); http, websocket and mqtt consume it later as their
// own slices, as a capability on their carrier.

// TLSTrust is how a connection decides what to trust. It carries no secret:
// a pin is a public key's fingerprint.
type TLSTrust struct {
	// Mode is driver.TLSModeVerify, TLSModePin or TLSModeSkip; empty is
	// verify.
	Mode string
	// Pin is the public key the endpoint pins (pin mode), shared across a
	// session's reconnects and the probe; empty trusts the first key offered.
	Pin *HostKeyPin
	// ServerName is the name verify checks the certificate against (the
	// host input).
	ServerName string
	// Roots is the pool verify checks against; nil is the node's system
	// roots. Only tests set it.
	Roots *x509.CertPool
}

// TLSFacts is what a handshake observed of the certificate offered.
type TLSFacts struct {
	// Key is the leaf's public key fingerprint (PublicKeyFingerprint).
	Key string
	// NotAfter is the leaf's expiry.
	NotAfter time.Time
	// HandshakeMS is how long the handshake took, from the connection
	// already open to the certificate judged.
	HandshakeMS float64
}

// PublicKeyFingerprint renders a certificate's public key (its
// SubjectPublicKeyInfo) as SHA256:<unpadded base64>, the form ssh
// fingerprints a host key in. The pin is the key, not the certificate, so a
// renewal that keeps the key is not a change.
func PublicKeyFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// keyChanged is the pin check's refusal: the key offered is not the key
// pinned.
type keyChanged struct{ pinned, offered string }

func (e *keyChanged) Error() string { return "the certificate's public key changed" }

func keyChangedRefusal(pinned, offered string) *TerminalError {
	return &TerminalError{Class: driver.ClassCertificate, Pinned: pinned, Offered: offered, Reason: fmt.Sprintf(
		"the certificate's public key changed: pinned %s, offered %s; retry the endpoint with the offered key to trust it", pinned, offered)}
}

// HandshakeTLS runs the client handshake over conn under trust, bounded by
// timeout and ctx. On success the returned connection carries the line
// protocol; on any failure conn is closed. In pin mode a first key is
// admitted to the pin only once the handshake completes.
func HandshakeTLS(ctx context.Context, conn net.Conn, trust TLSTrust, timeout time.Duration) (*tls.Conn, TLSFacts, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	mode, err := driver.ParseTLSMode(trust.Mode)
	if err != nil {
		_ = conn.Close()
		return nil, TLSFacts{}, &TerminalError{Class: driver.ClassProtocol, Reason: err.Error()}
	}
	var facts TLSFacts
	var offered *x509.Certificate
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: trust.ServerName,
		RootCAs:    trust.Roots,
		// Verify is the library's own chain and name check. Pin and skip
		// judge the key themselves in VerifyConnection, which runs whatever
		// InsecureSkipVerify says.
		InsecureSkipVerify: mode != driver.TLSModeVerify,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the component offered no certificate")
			}
			offered = cs.PeerCertificates[0]
			if mode != driver.TLSModePin || trust.Pin == nil {
				return nil
			}
			if pinned := trust.Pin.Get(); pinned != "" && pinned != PublicKeyFingerprint(offered) {
				return &keyChanged{pinned: pinned, offered: PublicKeyFingerprint(offered)}
			}
			return nil
		},
	}
	if cfg.ServerName == "" && mode == driver.TLSModeVerify {
		// A name to verify against is required: the target's host.
		if host, _, err := net.SplitHostPort(conn.RemoteAddr().String()); err == nil {
			cfg.ServerName = host
		}
	}
	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tc := tls.Client(conn, cfg)
	start := time.Now()
	herr := tc.HandshakeContext(hctx)
	facts.HandshakeMS = float64(time.Since(start)) / float64(time.Millisecond)
	if offered != nil {
		facts.Key, facts.NotAfter = PublicKeyFingerprint(offered), offered.NotAfter
	}
	if herr != nil {
		_ = tc.Close()
		var changed *keyChanged
		if errors.As(herr, &changed) {
			return nil, facts, keyChangedRefusal(changed.pinned, changed.offered)
		}
		var verr *tls.CertificateVerificationError
		if errors.As(herr, &verr) {
			if facts.Key == "" && len(verr.UnverifiedCertificates) > 0 {
				leaf := verr.UnverifiedCertificates[0]
				facts.Key, facts.NotAfter = PublicKeyFingerprint(leaf), leaf.NotAfter
			}
			return nil, facts, &TerminalError{Class: driver.ClassCertificate, Reason: "the certificate failed verification: " + verr.Err.Error()}
		}
		if ctx.Err() != nil {
			return nil, facts, ctx.Err()
		}
		return nil, facts, fmt.Errorf("collection: tls handshake: %w", herr)
	}
	if mode == driver.TLSModePin && trust.Pin != nil {
		// Admitted only once the component proved it holds the key, and
		// atomically: a connection racing this one on first use with another
		// key is refused rather than trusted beside it.
		if pinned, ok := trust.Pin.Admit(facts.Key); !ok {
			_ = tc.Close()
			return nil, facts, keyChangedRefusal(pinned, facts.Key)
		}
	}
	return tc, facts, nil
}

// TLSDialer is a staged dialer whose Connect runs a TLS handshake under the
// endpoint's trust (#1035): the session hands it the mode, the pin and the
// server name, so the handshake (and with it the certificate check) runs
// before anything rides the connection, a login included.
type TLSDialer interface {
	StagedDialer
	ConnectTLS(ctx context.Context, target string, timeout time.Duration, trust TLSTrust) (PendingConn, error)
}

// NewTLSSessionDialer returns the held tls dialer: a TCP connect, then the
// handshake, then the line protocol's own staging (its login, its reader).
func NewTLSSessionDialer() TLSDialer { return tlsSessionDialer{} }

type tlsSessionDialer struct{}

// Dial connects verifying (the safe default when no trust is declared) and
// starts routing.
func (d tlsSessionDialer) Dial(ctx context.Context, target string, timeout time.Duration, in Inbound) (HeldConn, error) {
	pc, err := d.Connect(ctx, target, timeout)
	if err != nil {
		return nil, err
	}
	return pc.Open(in, ChannelOptions{})
}

// Connect connects under verify, with the target's host as the server name.
func (d tlsSessionDialer) Connect(ctx context.Context, target string, timeout time.Duration) (PendingConn, error) {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		host = target
	}
	return d.ConnectTLS(ctx, target, timeout, TLSTrust{Mode: driver.TLSModeVerify, ServerName: host})
}

func (tlsSessionDialer) ConnectTLS(ctx context.Context, target string, timeout time.Duration, trust TLSTrust) (PendingConn, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	nd := net.Dialer{Timeout: timeout, KeepAlive: SessionKeepAlive}
	c, err := nd.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("collection: tls session dial %s: %w", target, err)
	}
	tc, _, err := HandshakeTLS(ctx, c, trust, timeout)
	if err != nil {
		if _, ok := AsTerminal(err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("collection: tls session %s: %w", target, err)
	}
	return &pendingLine{c: tc, target: target}, nil
}

// TLSProber is the tls reachability probe boundary (#1035), faked in unit
// tests: a TCP connect, then the handshake under the endpoint's trust. An
// outcome is DATA; err is reserved for a target that could not be attempted.
type TLSProber interface {
	Probe(ctx context.Context, target string, trust TLSTrust, timeout time.Duration) (TLSResult, error)
}

// TLSResult is one tls probe observation. Dialed is the TCP connect;
// Responded says the component answered the handshake with a certificate;
// Accepted says the mode accepted it (verified, the pinned key, or skip).
// Refusal is a certificate refusal the gate holds: a changed key, a failed
// verification.
type TLSResult struct {
	Dialed      bool
	Responded   bool
	Accepted    bool
	DialMS      float64
	HandshakeMS float64
	Reason      Reachability
	Facts       TLSFacts
	Refusal     *TerminalError
}

// NewTLSProber returns the real tls probe.
func NewTLSProber() TLSProber { return tlsProber{} }

type tlsProber struct{}

func (tlsProber) Probe(ctx context.Context, target string, trust TLSTrust, timeout time.Duration) (TLSResult, error) {
	if timeout <= 0 {
		timeout = defaultTCPTimeout
	}
	var res TLSResult
	nd := net.Dialer{Timeout: timeout}
	start := time.Now()
	c, err := nd.DialContext(ctx, "tcp", target)
	if err != nil {
		if reason, ok := Classify(err); ok {
			res.Reason = reason
			return res, nil
		}
		return TLSResult{}, fmt.Errorf("collection: tls probe %s: %w", target, err)
	}
	res.Dialed = true
	res.DialMS = float64(time.Since(start)) / float64(time.Millisecond)
	tc, facts, err := HandshakeTLS(ctx, c, trust, timeout)
	res.Facts = facts
	if err == nil {
		_ = tc.Close()
		res.Responded, res.Accepted, res.Reason, res.HandshakeMS = true, true, Responded, facts.HandshakeMS
		return res, nil
	}
	if terr, ok := AsTerminal(err); ok && terr.Class == driver.ClassCertificate {
		// The protocol answered with a certificate; it is the certificate
		// that is refused.
		res.Responded, res.Reason, res.HandshakeMS, res.Refusal = true, Responded, facts.HandshakeMS, terr
		return res, nil
	}
	res.Reason = Timedout
	if reason, ok := Classify(errors.Unwrap(err)); ok && reason != Responded {
		res.Reason = reason
	}
	return res, nil
}

// The tls probe keys (#1035): the handshake's timing, one observation per
// answered probe (decision 7a of #1076, folded into rtt by #1063), and the
// certificate facts: the not-after as a property (the fact) and the days
// left as a metric (the number a threshold rule reads, since an expression
// cannot read the clock), decision 4.
const (
	SignalTLSHandshakeTime       = probekeys.TLSHandshakeTime
	SignalTLSCertificate         = probekeys.TLSCertificate
	SignalTLSCertificateExpiry   = probekeys.TLSCertificateExpiry
	SignalTLSCertificateDaysLeft = probekeys.TLSCertificateDaysLeft
)

// TLSTask is the parsed tls probe unit: the dial target, the per-attempt
// timeout, and the trust the handshake runs under.
type TLSTask struct {
	Target  string
	Timeout time.Duration
	Trust   TLSTrust
}

// CollectTLSResult runs one tls probe: the L4 dial facts, the handshake rung
// (tls-handshake-time when the component answered, endpoint-responsive),
// and, for a certificate the mode accepted, its not-after
// (tls-certificate-expiry) and the days left (tls-certificate-days-left).
// A refused certificate lands no certificate fact: its key is nobody's.
func (r *Runner) CollectTLSResult(ctx context.Context, t TLSTask) ([]Sample, TLSResult, error) {
	if t.Target == "" {
		return nil, TLSResult{}, fmt.Errorf("collection: tls task: empty target")
	}
	if r.TLS == nil {
		return nil, TLSResult{}, fmt.Errorf("collection: tls task: no tls prober wired")
	}
	res, err := r.TLS.Probe(ctx, t.Target, t.Trust, t.Timeout)
	if err != nil {
		return nil, TLSResult{}, err
	}
	now := time.Now().UTC()
	openVal := 0.0
	if res.Dialed {
		openVal = 1.0
	}
	out := []Sample{{Name: SignalTCPOpen, Value: openVal, TS: now, Labels: map[string]string{ReasonLabel: string(res.Reason)}}}
	if res.Dialed {
		out = append(out, Sample{Name: SignalTCPConnectTime, Value: res.DialMS, TS: now})
	}
	responsive := "down"
	if res.Responded {
		responsive = "up"
		out = append(out, Sample{Name: SignalTLSHandshakeTime, Value: res.HandshakeMS, TS: now})
	}
	out = append(out, ladderVerdict(SignalEndpointResponsive, responsive, res.Reason, now))
	if res.Accepted && !res.Facts.NotAfter.IsZero() {
		out = append(out,
			Sample{Name: SignalTLSCertificateExpiry, Text: res.Facts.NotAfter.UTC().Format(time.RFC3339), IsText: true, TS: now},
			Sample{Name: SignalTLSCertificateDaysLeft, Value: DaysLeft(res.Facts.NotAfter, now), TS: now},
		)
	}
	return out, res, nil
}

// DaysLeft is the time from now to notAfter in days, to the hundredth;
// negative once the certificate has expired. Pure.
func DaysLeft(notAfter, now time.Time) float64 {
	return math.Round(notAfter.Sub(now).Hours()/24*100) / 100
}
