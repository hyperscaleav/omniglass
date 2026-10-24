package bus

import (
	"net"
	"strconv"
	"testing"
)

// Port 0 is an ephemeral port, as it is to net.Listen: the bus binds
// whatever port the kernel hands it and reports the one it got. nats-server
// alone reads 0 as its default 4222, so a test or a supervisor asking for 0
// would have bound the one port every other bus on the host wants.
func TestPortZeroBindsAnEphemeralPort(t *testing.T) {
	srv, err := New(Config{Host: "127.0.0.1", Port: 0}, fakeStore{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	bound, ok := srv.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("Addr() = %v, want the TCP address the bus bound", srv.Addr())
	}
	if bound.Port == 0 || bound.Port == 4222 {
		t.Fatalf("asked for port 0, bound %s; want a port the kernel handed out", bound)
	}
	want := "nats://127.0.0.1:" + strconv.Itoa(bound.Port)
	if got := srv.LocalURL(); got != want {
		t.Fatalf("LocalURL() = %q, want %q", got, want)
	}
	c, err := net.Dial("tcp", bound.String())
	if err != nil {
		t.Fatalf("the reported address %s does not answer: %v", bound, err)
	}
	_ = c.Close()
}

// The URL a client on this host dials is the address the bus bound, with a
// wildcard host read as loopback: nothing dials 0.0.0.0 or ::.
func TestLocalURLReadsAWildcardHostAsLoopback(t *testing.T) {
	for _, tc := range []struct {
		addr net.Addr
		want string
	}{
		{&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4222}, "nats://127.0.0.1:4222"},
		{&net.TCPAddr{IP: net.IPv4zero, Port: 4222}, "nats://127.0.0.1:4222"},
		{&net.TCPAddr{IP: net.IPv6unspecified, Port: 4300}, "nats://127.0.0.1:4300"},
		{&net.TCPAddr{Port: 4300}, "nats://127.0.0.1:4300"},
		{&net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 4222}, "nats://10.1.2.3:4222"},
		{&net.TCPAddr{IP: net.IPv6loopback, Port: 4222}, "nats://[::1]:4222"},
	} {
		if got := localURL(tc.addr); got != tc.want {
			t.Errorf("localURL(%v) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}
