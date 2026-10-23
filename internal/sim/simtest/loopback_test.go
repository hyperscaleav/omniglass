//go:build linux

package simtest

import (
	"math/rand/v2"
	"net"
	"testing"
)

// picks hands out start, start+1, ... and records how many it gave.
type picks struct{ next, given int }

func (p *picks) pick(int) int { p.given++; p.next++; return (p.next - 1) % (1 << 16) }

// A claim holds its address: a second claimant starting from the same
// address moves on to another, and the address is free again once the
// first claim is released.
func TestAClaimHoldsItsAddressUntilReleased(t *testing.T) {
	start := rand.IntN(1<<16 - claimTries)
	first, releaseFirst, err := claim((&picks{next: start}).pick)
	if err != nil {
		t.Fatal(err)
	}
	if net.ParseIP(first) == nil || !net.ParseIP(first).IsLoopback() {
		t.Fatalf("claimed %q, want a loopback address", first)
	}

	second := &picks{next: start}
	addr, releaseSecond, err := claim(second.pick)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSecond()
	if addr == first || second.given < 2 {
		t.Fatalf("a second claim took %s after %d picks, want another address than the held %s", addr, second.given, first)
	}

	releaseFirst()
	again, releaseAgain, err := claim(func(int) int { return start })
	if err != nil {
		t.Fatalf("the released %s could not be claimed again: %v", first, err)
	}
	defer releaseAgain()
	if again != first {
		t.Fatalf("reclaimed %s, want the released %s", again, first)
	}
}

// ClaimLoopback hands each claim in a run its own address.
func TestClaimLoopbackHandsOutDistinctAddresses(t *testing.T) {
	seen := map[string]bool{}
	for range 8 {
		addr := ClaimLoopback(t)
		if seen[addr] {
			t.Fatalf("%s claimed twice", addr)
		}
		seen[addr] = true
	}
}
