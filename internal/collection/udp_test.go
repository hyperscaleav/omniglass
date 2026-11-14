package collection_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// stubResolver answers the udp target resolutions of a DatagramConn from a
// settable address, and counts the calls (a literal IP target must make none).
type stubResolver struct {
	mu    sync.Mutex
	addr  *net.UDPAddr
	err   error
	calls int
}

func (s *stubResolver) set(addr string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	if addr != "" {
		s.addr, _ = net.ResolveUDPAddr("udp4", addr)
	}
}

func (s *stubResolver) resolve(network, target string) (*net.UDPAddr, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.addr, nil
}

func (s *stubResolver) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func answerOK(string) []string { return []string{"OK"} }

func exchangeOK(t *testing.T, c *collection.DatagramConn) {
	t.Helper()
	if _, err := c.Exchange(context.Background(), "PING", nil, time.Second, 0); err != nil {
		t.Fatalf("exchange: %v", err)
	}
}

func TestDatagramRetargetFollowsTheNameToItsNewAddress(t *testing.T) {
	x := newUDPPeer(t, answerOK)
	y := newUDPPeer(t, answerOK)
	res := &stubResolver{}
	res.set(x.addr(), nil)
	c := listenDatagram(t, collection.DatagramConfig{Target: "component.test:5000", Resolve: res.resolve})
	exchangeOK(t, c)
	if x.requests.Load() != 1 || y.requests.Load() != 0 {
		t.Fatalf("first request went to x=%d y=%d, want x=1 y=0", x.requests.Load(), y.requests.Load())
	}

	res.set(y.addr(), nil)
	from, to, moved, err := c.Retarget()
	if err != nil || !moved {
		t.Fatalf("retarget: moved=%v err=%v, want a move", moved, err)
	}
	if from != x.addr() || to != y.addr() {
		t.Fatalf("retarget reported %s -> %s, want %s -> %s", from, to, x.addr(), y.addr())
	}
	exchangeOK(t, c)
	if x.requests.Load() != 1 || y.requests.Load() != 1 {
		t.Fatalf("after the move x=%d y=%d requests, want x=1 y=1", x.requests.Load(), y.requests.Load())
	}

	// Unmoved: no move reported, so nothing is logged.
	if _, _, moved, err := c.Retarget(); moved || err != nil {
		t.Fatalf("retarget of an unmoved name: moved=%v err=%v", moved, err)
	}
}

func TestDatagramRetargetKeepsTheLastAddressWhenResolveFails(t *testing.T) {
	x := newUDPPeer(t, answerOK)
	res := &stubResolver{}
	res.set(x.addr(), nil)
	c := listenDatagram(t, collection.DatagramConfig{Target: "component.test:5000", Resolve: res.resolve})
	bound := c.LocalAddr().String()

	res.set("", &net.DNSError{Err: "no such host", Name: "component.test", IsNotFound: true})
	_, _, moved, err := c.Retarget()
	if moved || err == nil {
		t.Fatalf("a failed resolve: moved=%v err=%v, want an error and no move", moved, err)
	}
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		t.Fatalf("a failed resolve is %v, want the dns error to classify", err)
	}
	exchangeOK(t, c)
	if x.requests.Load() != 1 {
		t.Fatalf("the last address got %d requests, want 1", x.requests.Load())
	}
	if got := c.LocalAddr().String(); got != bound {
		t.Fatalf("the socket rebound: %s, was %s", got, bound)
	}
	select {
	case <-c.Done():
		t.Fatal("a failed resolve closed the socket")
	default:
	}
}

func TestDatagramLiteralIPTargetNeverResolves(t *testing.T) {
	x := newUDPPeer(t, answerOK)
	res := &stubResolver{}
	res.set(x.addr(), nil)
	c := listenDatagram(t, collection.DatagramConfig{Target: x.addr(), Resolve: res.resolve})
	if _, _, moved, err := c.Retarget(); moved || err != nil {
		t.Fatalf("retarget of a literal ip: moved=%v err=%v", moved, err)
	}
	if n := res.count(); n != 0 {
		t.Fatalf("a literal ip target called the resolver %d times, want 0", n)
	}
	exchangeOK(t, c)
}

func TestDatagramRetargetAcceptsOnlyTheNewAddressAsTheComponent(t *testing.T) {
	x := newUDPPeer(t, answerOK)
	y := newUDPPeer(t, answerOK)
	res := &stubResolver{}
	res.set(x.addr(), nil)
	c := listenDatagram(t, collection.DatagramConfig{Target: "component.test:5000", Resolve: res.resolve})
	res.set(y.addr(), nil)
	if _, _, moved, _ := c.Retarget(); !moved {
		t.Fatal("no move")
	}
	// The old address is now foreign: counted, never taken as the component's.
	x.push(t, c.LocalAddr(), "OK")
	eventually(t, "the old address's datagram to be counted foreign", func() bool { return c.ForeignCount() == 1 })
}
