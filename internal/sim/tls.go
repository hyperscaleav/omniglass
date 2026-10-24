package sim

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// generateSelfSignedCert returns a fresh in-memory RSA certificate so
// devices declaring `tls: true` can complete a real TLS handshake.
// The cert exists to make the wire protocol right, not to protect
// anything, the whole lab runs inside one host's container network
// namespace with no external traffic. Every client in the lab (agent
// sidecar, e2e probes, serve_test) uses InsecureSkipVerify; there is
// no CA, no persistence, no rotation.
//
// If a scenario ever needs a strict-verifying client, the right move
// is probably a dedicated test-only CA pool, not padding this SAN
// list.
func generateSelfSignedCert() (*tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generating RSA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating serial: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Omniglass Lab"},
			CommonName:   "omniglass-sim",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("creating certificate: %w", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
	}, nil
}

// certCache produces the shared self-signed cert exactly once per
// Engine. A per-device cert is overkill for a lab and multiplies
// handshake cost for nothing.
type certCache struct {
	once sync.Once
	cert *tls.Certificate
	err  error
}

func (c *certCache) get() (*tls.Certificate, error) {
	c.once.Do(func() {
		c.cert, c.err = generateSelfSignedCert()
	})
	return c.cert, c.err
}

// The tls line face's identity (#1035): the key and certificate a tcp face
// declared with tls: true serves, one per device. The key is derived from
// the device's id, so it is the same on every run of the simulator and an
// endpoint that pinned it stays up across a restart (a per-run key would
// reject every pinned endpoint each time the simulator came back). The
// simulator protects nothing, and a key anyone can derive from a public id
// says so. The certificate is self-signed for the device's hostname and
// address, valid for the profile's window from the face's first start, and
// minted once per run: a renewal keeps the key, so a pin survives it. A test
// replaces the key (a replaced component) or the whole certificate (one a
// test root vouches for) through the harness hooks below; either is served
// from the next handshake, with no listener restart.
type tlsIdentity struct {
	mu       sync.Mutex
	key      ed25519.PrivateKey
	cert     *tls.Certificate
	validity time.Duration
	// face is the tls line face's session registry, so the harness can
	// start it again on its port (StartTLSSession).
	face *SessionRegistry
	// handshakes counts the client hellos the face was offered: a test
	// tells a TCP connect (no hello) from a handshake by it.
	handshakes atomic.Int64
}

// defaultTLSValidity is a tls face's validity window when its profile sets
// none.
const defaultTLSValidity = 365 * 24 * time.Hour

// deviceTLSKey derives a device's tls key from its id.
func deviceTLSKey(deviceID string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("omniglass-sim-tls\x00" + deviceID))
	return ed25519.NewKeyFromSeed(seed[:])
}

// mintTLSCertificate self-signs key for the device, valid for validity from
// now.
func mintTLSCertificate(d *Device, key ed25519.PrivateKey, validity time.Duration) (*tls.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating serial: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"Omniglass Lab"}, CommonName: d.Hostname},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{d.Hostname},
		IPAddresses:           []net.IP{d.LoopbackIP},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, fmt.Errorf("creating certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, nil
}

// tlsServerConfig is the config a tls line face serves: the device's
// identity, minted on first use with validity (zero is a year), read at
// every handshake so a replacement takes effect at once.
func (d *Device) tlsServerConfig(validity time.Duration) (*tls.Config, error) {
	if validity <= 0 {
		validity = defaultTLSValidity
	}
	id := &d.tlsID
	id.mu.Lock()
	defer id.mu.Unlock()
	if id.key == nil {
		id.key = deviceTLSKey(d.DeviceID)
	}
	if id.cert == nil {
		c, err := mintTLSCertificate(d, id.key, validity)
		if err != nil {
			return nil, err
		}
		id.cert, id.validity = c, validity
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			id.handshakes.Add(1)
			id.mu.Lock()
			defer id.mu.Unlock()
			return id.cert, nil
		},
	}, nil
}

// startTLSFace binds the tls line face for reg on port, with the device's
// identity and the given validity window, and remembers the registry so the
// harness can start it again.
func (d *Device) startTLSFace(port int, reg *SessionRegistry, validity time.Duration) (net.Addr, error) {
	cfg, err := d.tlsServerConfig(validity)
	if err != nil {
		return nil, fmt.Errorf("tls identity: %w", err)
	}
	d.tlsID.mu.Lock()
	d.tlsID.face = reg
	d.tlsID.mu.Unlock()
	return d.startTCPSession(port, reg, cfg)
}

// StartTLSSession starts the device's tls line face again on port, after
// StopTCPSession (a component that restarts), with its identity unchanged.
// A test harness hook.
func (d *Device) StartTLSSession(port int) error {
	d.tlsID.mu.Lock()
	reg := d.tlsID.face
	d.tlsID.mu.Unlock()
	if reg == nil {
		return fmt.Errorf("device %s serves no tls line face", d.DeviceID)
	}
	_, err := d.startTLSFace(port, reg, 0)
	return err
}

// TLSHandshakes counts the client hellos the device's tls face was offered.
func (d *Device) TLSHandshakes() int64 { return d.tlsID.handshakes.Load() }

// TLSCertificate is the certificate the device's tls face serves now, nil
// before the face first starts.
func (d *Device) TLSCertificate() *x509.Certificate {
	d.tlsID.mu.Lock()
	defer d.tlsID.mu.Unlock()
	if d.tlsID.cert == nil {
		return nil
	}
	return d.tlsID.cert.Leaf
}

// RotateTLSKey replaces the device's tls key with a fresh one and re-mints
// its certificate over the same window, served from the next handshake: a
// replaced or reimaged component, whose pin the collector must notice. A
// test harness hook, not an operator-facing simulator operation.
func (d *Device) RotateTLSKey() error {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	id := &d.tlsID
	id.mu.Lock()
	defer id.mu.Unlock()
	validity := id.validity
	if validity <= 0 {
		validity = defaultTLSValidity
	}
	c, err := mintTLSCertificate(d, key, validity)
	if err != nil {
		return err
	}
	id.key, id.cert, id.validity = key, c, validity
	return nil
}

// SetTLSCertificate makes the device's tls face serve exactly cert from the
// next handshake (one a test root vouches for, an expired one). A test
// harness hook.
func (d *Device) SetTLSCertificate(cert tls.Certificate) {
	if cert.Leaf == nil && len(cert.Certificate) > 0 {
		cert.Leaf, _ = x509.ParseCertificate(cert.Certificate[0])
	}
	d.tlsID.mu.Lock()
	d.tlsID.cert = &cert
	d.tlsID.mu.Unlock()
}
