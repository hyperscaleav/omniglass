//go:build linux

package sim

import (
	"net"
	"strings"
	"testing"
	"time"
)

// The panel's NVP datagram face (#1037): the line protocol as datagrams,
// one line each, served per source address. A source that subscribes gets
// the pushes, and the face drops a seeded share of datagrams so a loss test
// runs on a real wire with no NET_ADMIN.

// udpClient is one source speaking to the face.
type udpClient struct {
	t *testing.T
	c *net.UDPConn
}

func dialFace(t *testing.T) *udpClient {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &udpClient{t: t, c: c}
}

func (u *udpClient) send(target, line string) {
	u.t.Helper()
	raddr, _ := net.ResolveUDPAddr("udp4", target)
	if _, err := u.c.WriteToUDP([]byte(line+"\r\n"), raddr); err != nil {
		u.t.Fatal(err)
	}
}

// recv reads the next datagram's line, or "" when none arrives in wait.
func (u *udpClient) recv(wait time.Duration) string {
	u.t.Helper()
	buf := make([]byte, 2048)
	_ = u.c.SetReadDeadline(time.Now().Add(wait))
	n, _, err := u.c.ReadFromUDP(buf)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(buf[:n]), "\r\n")
}

func panelEngine(t *testing.T, ip string) (*Engine, *Device) {
	t.Helper()
	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: ip,
		ProfileName: "newtron-panel-7", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return e, e.Devices()[0]
}

func TestPanelSpeaksNVPAsDatagrams(t *testing.T) {
	_, d := panelEngine(t, "127.88.9.31")
	d.SetDatagramLoss(0)
	target := faceAddr(t, d, "nvp-udp")
	a, b := dialFace(t), dialFace(t)
	a.send(target, "GET INPUT")
	if got := a.recv(2 * time.Second); got != "INPUT hdmi1" {
		t.Fatalf("GET INPUT = %q, want the default input", got)
	}
	a.send(target, "SUBSCRIBE INPUT")
	if got := a.recv(2 * time.Second); got != "OK" {
		t.Fatalf("SUBSCRIBE INPUT = %q, want OK", got)
	}
	// Another source changes the input: it hears its reply, and the
	// subscribed source hears the push; the other source subscribed to
	// nothing, so it hears no push.
	b.send(target, "SET INPUT hdmi2")
	if got := b.recv(2 * time.Second); got != "INPUT hdmi2" {
		t.Fatalf("SET INPUT = %q", got)
	}
	if got := a.recv(2 * time.Second); got != "EVT INPUT hdmi2" {
		t.Fatalf("the subscribed source heard %q, want the push", got)
	}
	if got := b.recv(200 * time.Millisecond); got != "" {
		t.Fatalf("the unsubscribed source heard %q", got)
	}
	a.send(target, "SET INPUT vga")
	if got := a.recv(2 * time.Second); !strings.HasPrefix(got, "-ERR") {
		t.Fatalf("an unknown input = %q, want the panel's -ERR refusal", got)
	}
}

// lossPattern sends n requests and records which were answered.
func lossPattern(t *testing.T, ip string, pct, n int) []bool {
	t.Helper()
	_, d := panelEngine(t, ip)
	d.SetDatagramLoss(pct)
	target := faceAddr(t, d, "nvp-udp")
	c := dialFace(t)
	out := make([]bool, n)
	for i := range out {
		c.send(target, "GET INPUT")
		out[i] = c.recv(150*time.Millisecond) != ""
	}
	return out
}

func TestTheDatagramFaceDropsASeededShare(t *testing.T) {
	if testing.Short() {
		t.Skip("sends a run of datagrams against a lossy face")
	}
	first := lossPattern(t, "127.88.9.32", 30, 40)
	answered := 0
	for _, ok := range first {
		if ok {
			answered++
		}
	}
	// 30% each way: about half the exchanges complete; the seed makes it
	// the same half on every run.
	if answered == 0 || answered == len(first) {
		t.Fatalf("answered %d of %d; want some lost and some answered", answered, len(first))
	}
	again := lossPattern(t, "127.88.9.33", 30, 40)
	for i := range first {
		if first[i] != again[i] {
			t.Fatalf("exchange %d: answered %v then %v; the seed makes the loss repeat", i, first[i], again[i])
		}
	}
	if none := lossPattern(t, "127.88.9.34", 100, 3); none[0] || none[1] || none[2] {
		t.Fatalf("a face dropping everything answered: %v", none)
	}
}
