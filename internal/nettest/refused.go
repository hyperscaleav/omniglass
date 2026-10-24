//go:build unix

// Package nettest holds network helpers for tests.
package nettest

import (
	"fmt"
	"net"
	"strconv"
	"syscall"
	"testing"
)

// RefusedAddr returns a loopback TCP address that refuses every dial for the
// life of the test, for a test that needs a connect to be refused.
//
// The address is held by a socket bound to it that never listens. A connect
// finds no listener there and is answered with a reset, and because the
// holder is bound without SO_REUSEADDR, nothing else on the host can listen
// where the dial lands while the test runs: a listen on the address, on the
// IPv4 wildcard or on the dual-stack wildcard fails with EADDRINUSE, with or
// without SO_REUSEPORT, and bind(:0) never hands the port out.
//
// That is what a port picked by listening on :0 and closing does not give:
// it is free the moment it closes, and a concurrent run on the host can be
// handed it before the dial, which then reaches a stranger's listener
// (#1089). A guard in internal/build fails any test that uses a released
// port.
func RefusedAddr(t testing.TB) string {
	t.Helper()
	addr, release, err := holdUnlistened()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return addr
}

// holdUnlistened binds a stream socket to a free port on 127.0.0.1 and never
// listens on it, returning the address and the release of the hold.
func holdUnlistened() (string, func(), error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return "", nil, fmt.Errorf("refused address: socket: %w", err)
	}
	syscall.CloseOnExec(fd)
	release := func() { _ = syscall.Close(fd) }
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		release()
		return "", nil, fmt.Errorf("refused address: bind: %w", err)
	}
	sa, err := syscall.Getsockname(fd)
	if err != nil {
		release()
		return "", nil, fmt.Errorf("refused address: getsockname: %w", err)
	}
	in4, ok := sa.(*syscall.SockaddrInet4)
	if !ok {
		release()
		return "", nil, fmt.Errorf("refused address: bound %T, want an IPv4 address", sa)
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(in4.Port)), release, nil
}
