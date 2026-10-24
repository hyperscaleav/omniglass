package sim

import (
	"bufio"
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
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// The simulated panel's TLS face (#1035): the NVP line protocol its tcp,
// telnet and ssh faces serve, behind a real TLS handshake. Its key is the
// device's own, derived from its id, so it is the same on every run (a
// pinned endpoint stays pinned across a simulator restart); its certificate's
// validity window is the profile's; and a test can replace the key or the
// whole certificate to model a replaced or re-certified component.

const panelProfile = "newtron-panel-7"

func panelSnapshot(id, ip string) DeviceSnapshot {
	return DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: ip, ProfileName: panelProfile, Version: 1}
}

// tlsAsk handshakes with the face (verifying nothing: the test reads what
// it is offered), asks GET INPUT, and returns the answer and the leaf.
func tlsAsk(t *testing.T, addr string) (string, *x509.Certificate) {
	t.Helper()
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("tls dial %s: %v", addr, err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Write([]byte("GET INPUT\r\n")); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the answer: %v", err)
		}
		if line = strings.TrimRight(line, "\r\n"); strings.HasPrefix(line, "INPUT ") {
			return line, c.ConnectionState().PeerCertificates[0]
		}
	}
}

func spkiOf(c *x509.Certificate) string {
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func TestPanelServesNVPOverTLS(t *testing.T) {
	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{panelSnapshot("panel-tls", "127.88.7.1")}); err != nil {
		t.Fatal(err)
	}
	panel := device(t, e, "panel-tls")
	face := faceOf(t, panel, "nvp-tls")
	if face.Protocol != "tcp" || !face.TLS {
		t.Fatalf("nvp-tls face = %+v, want a tcp face behind TLS", face)
	}
	answer, leaf := tlsAsk(t, face.Addr.String())
	if answer != "INPUT hdmi1" {
		t.Fatalf("GET INPUT over tls = %q", answer)
	}
	if got, want := spkiOf(leaf), spkiOf(panel.TLSCertificate()); got != want {
		t.Fatalf("served key %s, the device reports %s", got, want)
	}
	if leaf.Subject.CommonName != "panel-tls" || len(leaf.IPAddresses) == 0 || !leaf.IPAddresses[0].Equal(net.ParseIP("127.88.7.1")) {
		t.Fatalf("leaf names %q / %v, want the device's hostname and address", leaf.Subject.CommonName, leaf.IPAddresses)
	}
	// The plain NVP face is unchanged beside it.
	if plain := faceOf(t, panel, "nvp"); plain.TLS {
		t.Fatalf("the raw nvp face reads tls: %+v", plain)
	}
}

// A simulator restart is a new engine: the same device id serves the same
// key, so an endpoint pinned to it stays up; another device serves another.
func TestTLSFaceKeyPersistsAcrossRestarts(t *testing.T) {
	serve := func(id, ip string) string {
		e := newCatalogEngine(t)
		if err := e.Sync([]DeviceSnapshot{panelSnapshot(id, ip)}); err != nil {
			t.Fatal(err)
		}
		defer e.StopAll()
		_, leaf := tlsAsk(t, faceAddr(t, device(t, e, id), "nvp-tls"))
		return spkiOf(leaf)
	}
	first := serve("panel-a", "127.88.7.2")
	if again := serve("panel-a", "127.88.7.2"); again != first {
		t.Fatalf("a restarted simulator served %s, want the same key %s", again, first)
	}
	if other := serve("panel-b", "127.88.7.3"); other == first {
		t.Fatal("two devices share a key")
	}
}

// A replaced component offers a new key, and a re-certified one exactly the
// certificate the test hands it; both take effect on the next handshake,
// with no listener restart.
func TestTLSFaceKeyAndCertificateCanBeReplaced(t *testing.T) {
	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{panelSnapshot("panel-r", "127.88.7.4")}); err != nil {
		t.Fatal(err)
	}
	panel := device(t, e, "panel-r")
	addr := faceAddr(t, panel, "nvp-tls")
	_, before := tlsAsk(t, addr)
	if err := panel.RotateTLSKey(); err != nil {
		t.Fatal(err)
	}
	_, after := tlsAsk(t, addr)
	if spkiOf(after) == spkiOf(before) {
		t.Fatal("a rotated key was not served")
	}

	_, key, _ := ed25519.GenerateKey(rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "re-certified"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(48 * time.Hour), IPAddresses: []net.IP{net.ParseIP("127.88.7.4")}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	panel.SetTLSCertificate(tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key})
	if _, leaf := tlsAsk(t, addr); leaf.Subject.CommonName != "re-certified" {
		t.Fatalf("served %q, want the certificate the test set", leaf.Subject.CommonName)
	}
}

// The profile sets the certificate's validity window, counted from the
// face's start.
func TestTLSFaceValidityComesFromTheProfile(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	p := store.Devices[panelProfile]
	found := false
	for i := range p.Interfaces {
		if p.Interfaces[i].Name == "nvp-tls" {
			p.Interfaces[i].TLSValidityDays, found = 10, true
		}
	}
	if !found {
		t.Fatal("the panel profile declares no nvp-tls interface")
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	start := time.Now()
	if err := e.Sync([]DeviceSnapshot{panelSnapshot("panel-v", "127.88.7.5")}); err != nil {
		t.Fatal(err)
	}
	_, leaf := tlsAsk(t, faceAddr(t, device(t, e, "panel-v"), "nvp-tls"))
	want := start.Add(10 * 24 * time.Hour)
	if leaf.NotAfter.Before(want.Add(-time.Minute)) || leaf.NotAfter.After(want.Add(time.Minute)) {
		t.Fatalf("not-after %s, want ten days from the start (%s)", leaf.NotAfter, want)
	}
}

// The harness can drop the tls face and bring it back on its port (a
// component that restarts), and counts the handshakes it was offered, so a
// test can tell a TCP connect from a handshake: a connect sends no hello.
func TestTLSFaceRestartsAndCountsHandshakes(t *testing.T) {
	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{panelSnapshot("panel-h", "127.88.7.6")}); err != nil {
		t.Fatal(err)
	}
	panel := device(t, e, "panel-h")
	face := faceOf(t, panel, "nvp-tls")
	before := panel.TLSHandshakes()
	tlsAsk(t, face.Addr.String())
	if got := panel.TLSHandshakes(); got != before+1 {
		t.Fatalf("handshakes = %d after one, want %d", got, before+1)
	}
	c, err := net.DialTimeout("tcp", face.Addr.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	time.Sleep(20 * time.Millisecond)
	if got := panel.TLSHandshakes(); got != before+1 {
		t.Fatalf("a bare connect counted as a handshake: %d", got)
	}

	panel.StopTCPSession()
	if _, err := net.DialTimeout("tcp", face.Addr.String(), 200*time.Millisecond); err == nil {
		t.Fatal("the stopped face still accepts")
	}
	if err := panel.StartTLSSession(int(face.Addr.Port())); err != nil {
		t.Fatal(err)
	}
	if answer, _ := tlsAsk(t, face.Addr.String()); answer != "INPUT hdmi1" {
		t.Fatalf("the restarted face answered %q", answer)
	}
}
