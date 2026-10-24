// Package simtest holds helpers for tests that drive the simulator.
package simtest

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"syscall"
	"testing"
)

// claimPort is the port a claim holds on its address: above the kernel's
// default ephemeral range (32768 to 60999) and declared by no profile, so
// nothing but another claim binds it.
const claimPort = "64999"

// claimTries bounds the addresses a claim tries before it gives up.
const claimTries = 64

// ClaimLoopback returns a loopback address in 127.250.0.0/16 that no other
// test on the host holds, for a test that drives the fixed-port simulator.
// The sim run mode binds each face at its profile's port (lifted by the port
// offset) on the address the endpoint targets, so an ephemeral port is not an
// option there, and two test processes serving one fixed address would
// collide, or answer each other's probes. A test serving the engine in
// process takes sim.EphemeralPorts instead.
//
// The claim is a listener on the address that lives as long as the test: a
// second claimant's bind fails and it moves to another address, so two runs
// side by side never share one.
func ClaimLoopback(t testing.TB) string {
	t.Helper()
	addr, release, err := claim(rand.IntN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return addr
}

// claim picks addresses with pick until one binds its claim port, and
// returns it with the release of its claim.
func claim(pick func(n int) int) (string, func(), error) {
	for range claimTries {
		n := pick(1 << 16)
		addr := fmt.Sprintf("127.250.%d.%d", n>>8, n&0xff)
		lis, err := net.Listen("tcp", net.JoinHostPort(addr, claimPort))
		if errors.Is(err, syscall.EADDRINUSE) {
			continue // another test holds it
		}
		if err != nil {
			return "", nil, fmt.Errorf("claim a loopback address: %w", err)
		}
		return addr, func() { _ = lis.Close() }, nil
	}
	return "", nil, fmt.Errorf("claim a loopback address: %d addresses tried, every one held", claimTries)
}
