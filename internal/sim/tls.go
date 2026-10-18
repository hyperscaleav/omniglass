package sim

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"sync"
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
