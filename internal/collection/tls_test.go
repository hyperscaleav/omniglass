package collection_test

import (
	"bufio"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/nettest"
)

// The TLS dial primitive (#1035): one handshake under one of three modes
// (verify against roots, pin the public key on first use, skip), reporting
// the offered key's fingerprint and the certificate's not-after. A refusal
// retrying cannot fix (a changed key, a failed verification) is a terminal
// certificate refusal carrying its reason; anything else stays retryable.
// Proven over real sockets against a local TLS line server (the capability
// carve-out: the handshake is the environment risk).

// testKey is a fresh ed25519 key.
func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

type certOpts struct {
	key       crypto.Signer
	parent    *x509.Certificate // nil: self-signed
	parentKey crypto.Signer
	ca        bool
	notBefore time.Time
	notAfter  time.Time
	dns       []string
	ips       []net.IP
}

// testCert mints a certificate under opts.
func testCert(t *testing.T, o certOpts) (*x509.Certificate, tls.Certificate) {
	t.Helper()
	if o.notBefore.IsZero() {
		o.notBefore = time.Now().Add(-time.Hour)
	}
	if o.notAfter.IsZero() {
		o.notAfter = time.Now().Add(365 * 24 * time.Hour)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "panel"},
		NotBefore:             o.notBefore,
		NotAfter:              o.notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  o.ca,
		DNSNames:              o.dns,
		IPAddresses:           o.ips,
	}
	parent, parentKey := tmpl, o.key
	if o.parent != nil {
		parent, parentKey = o.parent, o.parentKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, o.key.Public(), parentKey)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c, tls.Certificate{Certificate: [][]byte{der}, PrivateKey: o.key, Leaf: c}
}

// selfSigned is a self-signed leaf for 127.0.0.1 with key.
func selfSigned(t *testing.T, key crypto.Signer, notAfter time.Time) (*x509.Certificate, tls.Certificate) {
	return testCert(t, certOpts{key: key, notAfter: notAfter, ips: []net.IP{net.ParseIP("127.0.0.1")}, dns: []string{"localhost"}})
}

// spki is the SHA256: fingerprint of a certificate's public key, computed
// independently of the code under test.
func spki(c *x509.Certificate) string {
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// tlsLineServer serves the line protocol over TLS with whatever certificate
// cert holds at each handshake (so a test can swap it), answering GET INPUT,
// and counts accepted TCP connections.
type tlsLineServer struct {
	addr    string
	accepts atomic.Int32
	mu      sync.Mutex
	cert    tls.Certificate
	conns   chan net.Conn
}

func (s *tlsLineServer) setCert(c tls.Certificate) {
	s.mu.Lock()
	s.cert = c
	s.mu.Unlock()
}

func newTLSLineServer(t *testing.T, cert tls.Certificate) *tlsLineServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s := &tlsLineServer{addr: ln.Addr().String(), cert: cert, conns: make(chan net.Conn, 16)}
	cfg := &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c := s.cert
		return &c, nil
	}}
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			s.accepts.Add(1)
			select {
			case s.conns <- raw:
			default:
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch strings.TrimRight(line, "\r\n") {
					case "GET INPUT":
						_, _ = c.Write([]byte("INPUT hdmi1\r\n"))
					default:
						_, _ = c.Write([]byte("-ERR\r\n"))
					}
				}
			}(tls.Server(raw, cfg))
		}
	}()
	return s
}

func dialRaw(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The pin is the public key, in the form ssh fingerprints a host key in, so
// a renewal that keeps the key is not a change (decision 2 of #1076).
func TestPublicKeyFingerprintIsTheSPKIInTheSSHForm(t *testing.T) {
	key := testKey(t)
	a, _ := selfSigned(t, key, time.Now().Add(24*time.Hour))
	b, _ := selfSigned(t, key, time.Now().Add(900*24*time.Hour)) // renewed, same key
	fp := collection.PublicKeyFingerprint(a)
	if fp != spki(a) || !strings.HasPrefix(fp, "SHA256:") || len(fp) != len("SHA256:")+43 {
		t.Fatalf("fingerprint %q, want %q (SHA256: and 43 base64 characters)", fp, spki(a))
	}
	if collection.PublicKeyFingerprint(b) != fp {
		t.Fatal("a renewal that kept the key moved the fingerprint")
	}
	c, _ := selfSigned(t, testKey(t), time.Now().Add(24*time.Hour))
	if collection.PublicKeyFingerprint(c) == fp {
		t.Fatal("a new key kept the fingerprint")
	}
}

func handshake(t *testing.T, addr string, trust collection.TLSTrust) (collection.TLSFacts, error) {
	t.Helper()
	conn, facts, err := collection.HandshakeTLS(context.Background(), dialRaw(t, addr), trust, 2*time.Second)
	if err == nil {
		_ = conn.Close()
	}
	return facts, err
}

func TestHandshakePinTrustsTheFirstKeyAndRefusesAChange(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket tls test")
	}
	notAfter := time.Now().Add(10 * 24 * time.Hour).Truncate(time.Second)
	leaf, cert := selfSigned(t, testKey(t), notAfter)
	srv := newTLSLineServer(t, cert)

	pin := collection.NewHostKeyPin("")
	var heard []string
	pin.OnTrust = func(fp string) { heard = append(heard, fp) }
	facts, err := handshake(t, srv.addr, collection.TLSTrust{Mode: driver.TLSModePin, Pin: pin})
	if err != nil {
		t.Fatalf("first use refused: %v", err)
	}
	if facts.Key != spki(leaf) || !facts.NotAfter.Equal(notAfter) {
		t.Fatalf("facts %+v, want key %s not-after %s", facts, spki(leaf), notAfter)
	}
	if pin.Get() != spki(leaf) || len(heard) != 1 || heard[0] != spki(leaf) {
		t.Fatalf("pin %q heard %v, want the first key trusted and heard once", pin.Get(), heard)
	}
	// The same key again passes.
	if _, err := handshake(t, srv.addr, collection.TLSTrust{Mode: driver.TLSModePin, Pin: pin}); err != nil {
		t.Fatalf("the pinned key refused: %v", err)
	}

	// The component is replaced: a new key at the same address.
	next, nextCert := selfSigned(t, testKey(t), notAfter)
	srv.setCert(nextCert)
	_, err = handshake(t, srv.addr, collection.TLSTrust{Mode: driver.TLSModePin, Pin: pin})
	terr, ok := collection.AsTerminal(err)
	if !ok || terr.Class != driver.ClassCertificate {
		t.Fatalf("a changed key = %v, want a terminal certificate refusal", err)
	}
	if !strings.Contains(terr.Reason, spki(leaf)) || !strings.Contains(terr.Reason, spki(next)) {
		t.Fatalf("refusal %q, want both fingerprints named", terr.Reason)
	}
	// Both keys ride the refusal as data, so no surface parses them out of
	// the reason to send one back as a pin.
	if terr.Pinned != spki(leaf) || terr.Offered != spki(next) {
		t.Fatalf("refusal carries pinned %q offered %q, want %q and %q", terr.Pinned, terr.Offered, spki(leaf), spki(next))
	}
	if pin.Get() != spki(leaf) {
		t.Fatalf("a refused key replaced the pin: %q", pin.Get())
	}
}

// Two connections racing on first use cannot each trust a different key:
// the pin is admitted atomically, and the loser is refused.
func TestHostKeyPinAdmitIsAtomic(t *testing.T) {
	pin := collection.NewHostKeyPin("")
	var heard []string
	pin.OnTrust = func(fp string) { heard = append(heard, fp) }
	if got, ok := pin.Admit("SHA256:a"); !ok || got != "SHA256:a" {
		t.Fatalf("first admit = %q, %v", got, ok)
	}
	if got, ok := pin.Admit("SHA256:b"); ok || got != "SHA256:a" {
		t.Fatalf("a second key admitted = %q, %v; want refused, the pin unchanged", got, ok)
	}
	if got, ok := pin.Admit("SHA256:a"); !ok || got != "SHA256:a" {
		t.Fatalf("the pin again = %q, %v", got, ok)
	}
	if len(heard) != 1 || heard[0] != "SHA256:a" {
		t.Fatalf("OnTrust heard %v, want the first use only", heard)
	}
}

func TestHandshakeVerify(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket tls test")
	}
	caKey := testKey(t)
	ca, _ := testCert(t, certOpts{key: caKey, ca: true})
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	signed := func(o certOpts) tls.Certificate {
		o.key, o.parent, o.parentKey = testKey(t), ca, caKey
		_, c := testCert(t, o)
		return c
	}

	// A certificate a root vouches for, naming the host, passes.
	good := newTLSLineServer(t, signed(certOpts{ips: []net.IP{net.ParseIP("127.0.0.1")}}))
	if _, err := handshake(t, good.addr, collection.TLSTrust{Mode: driver.TLSModeVerify, ServerName: "127.0.0.1", Roots: roots}); err != nil {
		t.Fatalf("a verified certificate refused: %v", err)
	}

	cases := []struct {
		name string
		srv  *tlsLineServer
		sn   string
		want string
	}{
		{"self-signed", newTLSLineServer(t, func() tls.Certificate { _, c := selfSigned(t, testKey(t), time.Time{}); return c }()), "127.0.0.1", "unknown authority"},
		{"another name", newTLSLineServer(t, signed(certOpts{dns: []string{"panel.example"}})), "127.0.0.1", "127.0.0.1"},
		{"expired", newTLSLineServer(t, signed(certOpts{ips: []net.IP{net.ParseIP("127.0.0.1")}, notBefore: time.Now().Add(-48 * time.Hour), notAfter: time.Now().Add(-24 * time.Hour)})), "127.0.0.1", "expired"},
	}
	for _, tc := range cases {
		_, err := handshake(t, tc.srv.addr, collection.TLSTrust{Mode: driver.TLSModeVerify, ServerName: tc.sn, Roots: roots})
		terr, ok := collection.AsTerminal(err)
		if !ok || terr.Class != driver.ClassCertificate {
			t.Errorf("%s: %v, want a terminal certificate refusal", tc.name, err)
			continue
		}
		if !strings.Contains(terr.Reason, tc.want) {
			t.Errorf("%s: reason %q, want it to name %q", tc.name, terr.Reason, tc.want)
		}
	}
}

func TestHandshakeSkipChecksNothingAndStillReports(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket tls test")
	}
	leaf, cert := selfSigned(t, testKey(t), time.Now().Add(-time.Hour)) // expired, self-signed
	srv := newTLSLineServer(t, cert)
	facts, err := handshake(t, srv.addr, collection.TLSTrust{Mode: driver.TLSModeSkip, ServerName: "wrong.example"})
	if err != nil {
		t.Fatalf("skip refused: %v", err)
	}
	if facts.Key != spki(leaf) || facts.HandshakeMS <= 0 {
		t.Fatalf("facts %+v, want the key reported and a handshake time", facts)
	}
}

// A port that does not speak TLS is not a refusal retrying cannot fix: the
// handshake fails retryably, never as certificate.
func TestHandshakeAgainstPlaintextIsRetryable(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket tls test")
	}
	addr, _, _ := lineServer(t, "PLAIN BANNER")
	_, err := handshake(t, addr, collection.TLSTrust{Mode: driver.TLSModePin, Pin: collection.NewHostKeyPin("")})
	if err == nil {
		t.Fatal("a plaintext port completed a tls handshake")
	}
	if _, terminal := collection.AsTerminal(err); terminal {
		t.Fatalf("a plaintext port = %v, want retryable", err)
	}
}

// The tls probe (#1035): the handshake rung, its timing, and the certificate
// facts (the not-after as a property, the days left as a metric) for a
// certificate the mode accepted.
func TestTLSProbeLandsTheHandshakeAndTheCertificateFacts(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket tls test")
	}
	notAfter := time.Now().Add(10 * 24 * time.Hour).UTC().Truncate(time.Second)
	_, cert := selfSigned(t, testKey(t), notAfter)
	srv := newTLSLineServer(t, cert)
	r := &collection.Runner{TLS: collection.NewTLSProber()}
	dps, res, err := r.CollectTLSResult(context.Background(), collection.TLSTask{Target: srv.addr, Trust: collection.TLSTrust{Mode: driver.TLSModeSkip}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]collection.Sample{}
	for _, d := range dps {
		got[d.Name] = d
	}
	if got["tcp-open"].Value != 1 || got["endpoint-responsive"].Text != "up" || got["tls-handshake-time"].Value <= 0 {
		t.Fatalf("samples %+v, want open, responsive, a handshake time", dps)
	}
	if exp := got["tls-certificate-expiry"]; !exp.IsText || exp.Text != notAfter.Format(time.RFC3339) {
		t.Fatalf("tls-certificate-expiry = %+v, want %s", exp, notAfter.Format(time.RFC3339))
	}
	if days := got["tls-certificate-days-left"].Value; days < 9.9 || days > 10 {
		t.Fatalf("tls-certificate-days-left = %v, want 10 (to the hundredth)", days)
	}
	if !res.Responded || !res.Accepted || res.Refusal != nil {
		t.Fatalf("result %+v", res)
	}

	// A refused certificate: the protocol answered, so the rung reads
	// responsive, the refusal rides the result for the gate, and no
	// certificate fact lands for a key nobody trusts.
	dps, res, err = r.CollectTLSResult(context.Background(), collection.TLSTask{Target: srv.addr,
		Trust: collection.TLSTrust{Mode: driver.TLSModePin, Pin: collection.NewHostKeyPin("SHA256:" + strings.Repeat("A", 43))}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Refusal == nil || res.Refusal.Class != driver.ClassCertificate || res.Accepted {
		t.Fatalf("result %+v, want a certificate refusal", res)
	}
	for _, d := range dps {
		if d.Name == "tls-certificate-expiry" || d.Name == "tls-certificate-days-left" {
			t.Fatalf("a refused certificate landed %s", d.Name)
		}
	}

	// A closed port: not open, not responsive, nothing else.
	dps, _, err = r.CollectTLSResult(context.Background(), collection.TLSTask{Target: nettest.RefusedAddr(t), Trust: collection.TLSTrust{Mode: driver.TLSModeSkip}})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dps {
		switch d.Name {
		case "tcp-open":
			if d.Value != 0 {
				t.Fatal("a closed port read open")
			}
		case "endpoint-responsive":
			if d.Text != "down" {
				t.Fatal("a closed port read responsive")
			}
		default:
			t.Fatalf("a closed port landed %s", d.Name)
		}
	}
}

// The held tls session: the line protocol over a real handshake, its
// connection pinned; a changed key rejects it as certificate at once and it
// dials no more.
func TestTLSSessionPinsAndRejectsAChangedKey(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket tls test")
	}
	leaf, cert := selfSigned(t, testKey(t), time.Time{})
	srv := newTLSLineServer(t, cert)
	pin := collection.NewHostKeyPin("")
	rec := newRecorder()
	clock := newFakeClock()
	s := collection.StartSession(collection.SessionConfig{
		Target: srv.addr, Timeout: 2 * time.Second, Dialer: collection.NewTLSSessionDialer(),
		TLS:          &collection.TLSTrust{Mode: driver.TLSModePin, Pin: pin},
		OnTransition: rec.onTransition, Backoff: func(int) time.Duration { return 10 * time.Millisecond },
		After: clock.After, Now: clock.Now,
	})
	defer s.Close()
	rec.waitStates(t, establishing, up)
	answer, err := s.Exchange(context.Background(), "GET INPUT", &driver.Reply{Prefix: "INPUT "}, 0)
	if err != nil || answer != "INPUT hdmi1" {
		t.Fatalf("exchange over tls = %q, %v", answer, err)
	}
	if pin.Get() != spki(leaf) {
		t.Fatalf("pin %q, want the first key", pin.Get())
	}

	// The key changes and the connection drops: the redial meets the new
	// key and is refused for good.
	_, nextCert := selfSigned(t, testKey(t), time.Time{})
	srv.setCert(nextCert)
	// Close the connections held now, and only those: the session redials
	// at once, and a drain that ran until the channel emptied could close
	// that redial too, failing its handshake retryably and leaving the
	// session waiting on a backoff the fake clock never fires (#1128).
	for held := len(srv.conns); held > 0; held-- {
		_ = (<-srv.conns).Close()
	}
	rec.waitStates(t, establishing, up, collection.SessionRecovering, rejected)
	tr := rec.lastTransition()
	if tr.Refusal == nil || tr.Refusal.Class != driver.ClassCertificate {
		t.Fatalf("rejected with %+v, want class certificate", tr.Refusal)
	}
	before := srv.accepts.Load()
	clock.Advance(time.Hour, time.Minute)
	if got := srv.accepts.Load(); got != before {
		t.Fatalf("a rejected session dialed %d more times", got-before)
	}
	if _, err := s.Exchange(context.Background(), "GET INPUT", nil, 0); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("an exchange over the rejected session = %v, want the refusal", err)
	}
}

// A session that declares a tls handshake over a dialer that cannot run one
// is a protocol refusal, never a plaintext connection.
func TestSessionRefusesTLSOverADialerWithoutIt(t *testing.T) {
	rec := newRecorder()
	s := collection.StartSession(collection.SessionConfig{
		Target: "10.0.0.5:51326", Dialer: &fakeSessionDialer{},
		TLS:          &collection.TLSTrust{Mode: driver.TLSModeVerify},
		OnTransition: rec.onTransition,
	})
	defer s.Close()
	rec.waitStates(t, establishing, rejected)
	if tr := rec.lastTransition(); tr.Refusal == nil || tr.Refusal.Class != driver.ClassProtocol {
		t.Fatalf("rejected with %+v, want a protocol refusal", tr.Refusal)
	}
}

func TestDaysLeft(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		notAfter time.Time
		want     float64
	}{
		{now.Add(10 * 24 * time.Hour), 10},
		{now.Add(10*24*time.Hour - time.Minute), 10}, // to the hundredth
		{now.Add(36 * time.Hour), 1.5},
		{now.Add(-48 * time.Hour), -2},
	} {
		if got := collection.DaysLeft(tc.notAfter, now); got != tc.want {
			t.Errorf("DaysLeft(%s) = %v, want %v", tc.notAfter.Sub(now), got, tc.want)
		}
	}
}
