package collection_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The datagram primitive against a real udp peer on loopback (#1037, the
// capability carve-out): genuine sockets, genuine datagrams, the kernel's own
// port-unreachable report.

// udpPeer is a component speaking a line protocol as datagrams: answer maps a
// request line to the lines it sends back, each its own datagram, and drop
// says whether to ignore a request (by its count, from 1).
type udpPeer struct {
	conn     *net.UDPConn
	requests atomic.Int32
	mu       sync.Mutex
	got      []string
	answer   func(line string) []string
	drop     func(n int32) bool
}

func newUDPPeer(t *testing.T, answer func(string) []string) *udpPeer {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("peer listen: %v", err)
	}
	p := &udpPeer{conn: c, answer: answer}
	t.Cleanup(func() { _ = c.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			line := strings.TrimRight(string(buf[:n]), "\r\n")
			k := p.requests.Add(1)
			p.mu.Lock()
			p.got = append(p.got, line)
			drop := p.drop
			p.mu.Unlock()
			if drop != nil && drop(k) {
				continue
			}
			for _, out := range p.answer(line) {
				_, _ = c.WriteToUDP([]byte(out+"\r\n"), from)
			}
		}
	}()
	return p
}

func (p *udpPeer) addr() string { return p.conn.LocalAddr().String() }

func (p *udpPeer) setDrop(drop func(n int32) bool) {
	p.mu.Lock()
	p.drop = drop
	p.mu.Unlock()
}

// push sends an unsolicited datagram to the node's socket.
func (p *udpPeer) push(t *testing.T, to net.Addr, line string) {
	t.Helper()
	if _, err := p.conn.WriteTo([]byte(line+"\r\n"), to); err != nil {
		t.Fatalf("peer push: %v", err)
	}
}

// claimer is an Inbound that claims lines by prefix and keeps what it saw.
type claimer struct {
	prefix    string
	mu        sync.Mutex
	claimed   []string
	unclaimed []string
}

func (c *claimer) Claim(line string) bool {
	if !strings.HasPrefix(line, c.prefix) {
		return false
	}
	c.mu.Lock()
	c.claimed = append(c.claimed, line)
	c.mu.Unlock()
	return true
}

func (c *claimer) Unclaimed(line string) {
	c.mu.Lock()
	c.unclaimed = append(c.unclaimed, line)
	c.mu.Unlock()
}

func (c *claimer) seen() (claimed, unclaimed []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.claimed...), append([]string(nil), c.unclaimed...)
}

func listenDatagram(t *testing.T, cfg collection.DatagramConfig) *collection.DatagramConn {
	t.Helper()
	c, err := collection.ListenDatagram(cfg)
	if err != nil {
		t.Fatalf("listen datagram: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDatagramExchangeTakesTheShapedAnswerAndAPushStillReachesItsListener(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	peer := newUDPPeer(t, func(line string) []string {
		if line == "GET INPUT" {
			// A push lands mid-exchange, ahead of the answer.
			return []string{"EVT INPUT hdmi3", "INPUT hdmi2"}
		}
		return nil
	})
	in := &claimer{prefix: "EVT "}
	c := listenDatagram(t, collection.DatagramConfig{Target: peer.addr(), Inbound: in})
	ans, err := c.Exchange(context.Background(), "GET INPUT", &driver.Reply{Prefix: "INPUT "}, time.Second, 0)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if ans.Line != "INPUT hdmi2" {
		t.Fatalf("answer = %q, want the shaped reply, never the push", ans.Line)
	}
	if ans.RTT <= 0 || ans.RTT > time.Second {
		t.Fatalf("rtt = %s, want the time the answer took", ans.RTT)
	}
	eventually(t, "the push claimed", func() bool { cl, _ := in.seen(); return len(cl) == 1 && cl[0] == "EVT INPUT hdmi3" })
}

func TestDatagramExchangeWithNoShapeTakesTheFirstUnclaimedLine(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	peer := newUDPPeer(t, func(string) []string { return []string{"EVT INPUT hdmi3", "OK"} })
	in := &claimer{prefix: "EVT "}
	c := listenDatagram(t, collection.DatagramConfig{Target: peer.addr(), Inbound: in})
	ans, err := c.Exchange(context.Background(), "SUBSCRIBE INPUT", nil, time.Second, 0)
	if err != nil || ans.Line != "OK" {
		t.Fatalf("exchange = %q, %v; want OK, the first line no listener claimed", ans.Line, err)
	}
}

func TestDatagramExchangeReadsTheErrorShapeAsTheComponentsRefusal(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	peer := newUDPPeer(t, func(string) []string { return []string{"-ERR no such input"} })
	c := listenDatagram(t, collection.DatagramConfig{Target: peer.addr()})
	_, err := c.Exchange(context.Background(), "SET INPUT vga", &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Prefix: "-ERR"}}, time.Second, 2)
	var refused *collection.RefusedError
	if !errors.As(err, &refused) || refused.Line != "-ERR no such input" {
		t.Fatalf("err = %v, want a refusal carrying the component's text", err)
	}
	if n := peer.requests.Load(); n != 1 {
		t.Fatalf("the peer saw %d requests; a refusal is an answer, never retried", n)
	}
}

func TestDatagramRetriesALostRequestWithinTheTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	peer := newUDPPeer(t, func(string) []string { return []string{"INPUT hdmi1"} })
	peer.setDrop(func(n int32) bool { return n <= 2 }) // the first two are lost
	c := listenDatagram(t, collection.DatagramConfig{Target: peer.addr()})
	start := time.Now()
	ans, err := c.Exchange(context.Background(), "GET INPUT", &driver.Reply{Prefix: "INPUT "}, 900*time.Millisecond, 2)
	if err != nil || ans.Line != "INPUT hdmi1" {
		t.Fatalf("exchange = %q, %v; want the third attempt answered", ans.Line, err)
	}
	if ans.Attempts != 3 || peer.requests.Load() != 3 {
		t.Fatalf("attempts = %d, peer saw %d; want 3 each", ans.Attempts, peer.requests.Load())
	}
	if took := time.Since(start); took > 900*time.Millisecond {
		t.Fatalf("took %s; the attempts share the one timeout", took)
	}
}

func TestDatagramWithNoRetriesSendsOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	peer := newUDPPeer(t, func(string) []string { return []string{"INPUT hdmi1"} })
	peer.setDrop(func(int32) bool { return true })
	c := listenDatagram(t, collection.DatagramConfig{Target: peer.addr()})
	_, err := c.Exchange(context.Background(), "SET INPUT hdmi1", &driver.Reply{Prefix: "INPUT "}, 200*time.Millisecond, 0)
	if !errors.Is(err, collection.ErrNoAnswer) {
		t.Fatalf("err = %v, want no answer", err)
	}
	if r, ok := collection.Classify(err); !ok || r != collection.Timedout {
		t.Fatalf("classify = %q, %v; want timeout", r, ok)
	}
	time.Sleep(50 * time.Millisecond)
	if n := peer.requests.Load(); n != 1 {
		t.Fatalf("the peer saw %d requests; with no retries the request is sent once", n)
	}
}

func TestDatagramToAClosedPortReadsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	// A port nothing listens on: bind one, note it, close it.
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	closed := probe.LocalAddr().String()
	_ = probe.Close()
	c := listenDatagram(t, collection.DatagramConfig{Target: closed})
	start := time.Now()
	_, err = c.Exchange(context.Background(), "GET INPUT", nil, 2*time.Second, 2)
	if r, ok := collection.Classify(err); !ok || r != collection.Refused {
		t.Fatalf("err = %v (classified %q), want refused", err, r)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %s; the host's refusal answers at once, it is not waited out", took)
	}
	// The refusal is consumed, not left to fail every later read: the
	// socket still reads (a stranger's datagram is counted).
	other := newUDPPeer(t, func(string) []string { return nil })
	other.push(t, c.LocalAddr(), "hello")
	eventually(t, "the socket reading on", func() bool { return c.ForeignCount() == 1 })
}

func TestDatagramFromAnotherSourceIsCountedAndDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	peer := newUDPPeer(t, func(string) []string { return nil })
	stranger := newUDPPeer(t, func(string) []string { return nil })
	in := &claimer{prefix: "EVT "}
	c := listenDatagram(t, collection.DatagramConfig{Target: peer.addr(), Inbound: in})
	stranger.push(t, c.LocalAddr(), "EVT INPUT spoofed")
	stranger.push(t, c.LocalAddr(), "EVT INPUT spoofed again")
	peer.push(t, c.LocalAddr(), "EVT INPUT hdmi2")
	eventually(t, "the component's push claimed", func() bool { cl, _ := in.seen(); return len(cl) == 1 })
	eventually(t, "both strangers counted", func() bool {
		return c.ForeignCount() == 2
	})
	n, last := c.DrainForeign()
	if n != 2 || last != stranger.addr() {
		t.Fatalf("foreign = %d from %q, want 2 from %s", n, last, stranger.addr())
	}
	if n, _ := c.DrainForeign(); n != 0 {
		t.Fatalf("a drain resets the count, got %d", n)
	}
	cl, uncl := in.seen()
	if len(cl) != 1 || cl[0] != "EVT INPUT hdmi2" || len(uncl) != 0 {
		t.Fatalf("claimed %v unclaimed %v: a stranger's datagram reaches nobody", cl, uncl)
	}
}

func TestDatagramTerminalReplyIsARefusalRetryingCannotFix(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	peer := newUDPPeer(t, func(string) []string { return []string{"-ERR locked out"} })
	ss := &driver.SessionSpec{Terminal: []driver.TerminalRule{{Class: driver.ClassAuth, Match: driver.Match{Prefix: "-ERR locked"}}}}
	c := listenDatagram(t, collection.DatagramConfig{Target: peer.addr(), Terminal: collection.TerminalClassifier(ss)})
	_, err := c.Exchange(context.Background(), "GET INPUT", &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Prefix: "-ERR"}}, time.Second, 2)
	terr, ok := collection.AsTerminal(err)
	if !ok || terr.Class != driver.ClassAuth || terr.Reason != "-ERR locked out" {
		t.Fatalf("err = %v, want a terminal auth refusal with the component's text", err)
	}
}

func TestDatagramBindsTheEndpointsListenPortAndItsRequestsShareIt(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	free, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	port := free.LocalAddr().(*net.UDPAddr).Port
	_ = free.Close()
	peer := newUDPPeer(t, func(string) []string { return []string{"OK"} })
	c := listenDatagram(t, collection.DatagramConfig{Target: peer.addr(), LocalPort: port})
	if got := c.LocalAddr().(*net.UDPAddr).Port; got != port {
		t.Fatalf("bound port %d, want the listen port %d", got, port)
	}
	if _, err := c.Exchange(context.Background(), "SUBSCRIBE INPUT", nil, time.Second, 0); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	// A second endpoint cannot share the port: each binds its own.
	if _, err := collection.ListenDatagram(collection.DatagramConfig{Target: peer.addr(), LocalPort: port}); err == nil {
		t.Fatal("a second bind of the same listen port succeeded")
	}
}

func TestDatagramCloseFailsALaterExchange(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	peer := newUDPPeer(t, func(string) []string { return []string{"OK"} })
	c, err := collection.ListenDatagram(collection.DatagramConfig{Target: peer.addr()})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if _, err := c.Exchange(context.Background(), "GET INPUT", nil, 100*time.Millisecond, 0); err == nil {
		t.Fatal("an exchange on a closed socket succeeded")
	}
}
