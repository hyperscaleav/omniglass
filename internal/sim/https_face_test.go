package sim

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// An https face serves the device's own identity (#902): an ECDSA key
// derived from its id, so a pinned https endpoint survives a simulator
// restart, and a test can replace it to model a replaced component.
func TestAnHTTPSFaceKeyIsTheDevicesOwn(t *testing.T) {
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	snap := DeviceSnapshot{DeviceID: "bar-1", Hostname: "bar-1", LoopbackIP: ip, ProfileName: "kestrel-vroom", Version: 1}
	offered := func() string {
		e := NewEngine(store, profiles.Templates())
		e.SetPortMap(EphemeralPorts)
		defer e.StopAll()
		if err := e.Sync([]DeviceSnapshot{snap}); err != nil {
			t.Fatal(err)
		}
		dev := e.Devices()[0]
		addr := dev.HTTPAddrs()[0].String()
		c, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("handshake with %s: %v", addr, err)
		}
		defer func() { _ = c.Close() }()
		leaf := c.ConnectionState().PeerCertificates[0]
		if leaf.PublicKeyAlgorithm.String() != "ECDSA" {
			t.Fatalf("the https face offers a %s key, want ECDSA (a browser opens the page)", leaf.PublicKeyAlgorithm)
		}
		sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
		key := base64.RawStdEncoding.EncodeToString(sum[:])
		// The face still answers http behind the handshake (basic auth).
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
		resp, err := client.Get("https://" + addr + "/api/status")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("an unauthenticated status read = %d, want 401", resp.StatusCode)
		}
		if err := dev.RotateHTTPSKey(); err != nil {
			t.Fatal(err)
		}
		c2, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c2.Close() }()
		if sum2 := sha256.Sum256(c2.ConnectionState().PeerCertificates[0].RawSubjectPublicKeyInfo); sum2 == sum {
			t.Fatal("a rotated key offers the old one")
		}
		return key
	}
	if first, second := offered(), offered(); first != second {
		t.Fatalf("the https key moved across a restart: %s then %s", first, second)
	}
}
