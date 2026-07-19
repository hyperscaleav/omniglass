package collection

import (
	"errors"
	"net"
	"syscall"
)

// Reachability is a liveness probe's classified outcome. It exists so the
// verdict can tell apart, and report distinctly, the ways a probe can fail: a
// host that is silent (timed out / off), one that actively refused the port, and
// one administratively blocked by a firewall. Without it all three collapse to a
// bare "down". Only Responded is up; the rest are down but each carries its own
// reason so an operator can distinguish "nothing answered" from "a firewall is in
// the way". The reason rides the datapoint as a label (see ReasonLabel), which is
// not persisted in checkpoint 3 (only the typed row) but is produced by the probe.
type Reachability string

const (
	// Responded is a positive answer: the host is up and reachable on this probe.
	Responded Reachability = "responded"
	// Timedout: the probe was sent but nothing came back inside the window (host
	// off, packet silently dropped).
	Timedout Reachability = "timeout"
	// Refused: the target actively refused (TCP RST). The host is up, the port is
	// closed.
	Refused Reachability = "refused"
	// Prohibited: administratively blocked (EPERM/EACCES, or an ICMP
	// admin-prohibited surfaced to connect()).
	Prohibited Reachability = "prohibited"
	// Unreachable: no route to the host or network (EHOSTUNREACH/ENETUNREACH).
	Unreachable Reachability = "unreachable"
)

// Up reports whether the outcome is a positive reachability answer (the only
// value that reads as open).
func (r Reachability) Up() bool { return r == Responded }

// ReasonLabel is the datapoint label the probe stamps with a Reachability reason,
// so a down signal carries WHY (a silent timeout vs an administrative block) and
// the two are distinguishable downstream.
const ReasonLabel = "reason"

// pingReason resolves the reason an icmp probe result carries. It prefers the
// reason the pinger classified; a fake that left Reason unset falls back to the
// received-count verdict (any echo is Responded, none is a silent Timedout) so
// the reachable datapoint always carries a non-empty reason.
func pingReason(res PingResult) Reachability {
	if res.Reason != "" {
		return res.Reason
	}
	if res.Received > 0 {
		return Responded
	}
	return Timedout
}

// Classify maps a connect error to the reachability reason it represents. ok is
// false when the error is NOT a reachability verdict, i.e. a resolve/setup
// failure (the node could not even form the probe), which the caller treats as
// inconclusive (no datapoint), never as down: an unresolvable endpoint says
// nothing about whether the target is up. A real failure to reach the target IS a
// verdict (down): refused / prohibited / unreachable get their own reason, and a
// deadline or unrecognized network error reads as a silent non-answer (Timedout)
// so a block never hides as no-data. A nil error is Responded.
func Classify(err error) (reason Reachability, ok bool) {
	if err == nil {
		return Responded, true
	}
	// A name-resolution failure is a setup problem, not a target verdict.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "", false
	}
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return Refused, true
	case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES):
		return Prohibited, true
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return Unreachable, true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return Timedout, true
	}
	// An unrecognized network error reached the target enough to fail: a silent
	// non-answer (down), not no-data, so a block never hides as missing.
	return Timedout, true
}
