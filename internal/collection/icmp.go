package collection

import (
	"context"
	"fmt"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// NewICMPPinger returns the real Pinger: unprivileged ICMP via pro-bing
// (SOCK_DGRAM / IPPROTO_ICMP, which works where net.ipv4.ping_group_range admits
// the process gid, no raw-socket privilege). Reachability is DATA, never an
// error: a host that does not echo is Timedout, a firewall that rejects the echo
// (EPERM / admin-prohibited) is Prohibited, an unrouteable target is Unreachable.
// The error return is reserved for the one genuinely inconclusive case, a node
// that cannot do ICMP at all, so a per-target result would say nothing about the
// target.
//
// Telling "this node cannot ping anything" from "this target is blocked" matters
// because both surface as an error on the unprivileged socket. The pinger
// resolves it by self-checking its capability ONCE against loopback: if the
// loopback run succeeds, ICMP works here, so a subsequent per-target run error is
// a reachability verdict (down with a reason); if loopback itself fails, the node
// lacks the capability and every probe is inconclusive (no datapoint).
func NewICMPPinger() ICMPPinger { return ICMPPinger{cap: &icmpCapability{}} }

// ICMPPinger is the real Pinger. Its capability self-check is cached once across
// probes (a node's ICMP privilege does not change mid-run).
type ICMPPinger struct{ cap *icmpCapability }

type icmpCapability struct {
	once sync.Once
	ok   bool
}

// capable reports whether this node can do unprivileged ICMP at all, probed once
// against loopback and cached. A nil cap (a pinger built without the constructor)
// degrades to "capable" so a send error still classifies per-target rather than
// reporting a node-wide inconclusive.
func (p ICMPPinger) capable() bool {
	if p.cap == nil {
		return true
	}
	p.cap.once.Do(func() {
		lp, err := probing.NewPinger("127.0.0.1")
		if err != nil {
			return
		}
		lp.Count = 1
		lp.Timeout = time.Second
		lp.SetPrivileged(false)
		// Capability is whether the ICMP run SUCCEEDS (the socket opened and the
		// send went out), not whether loopback echoed back: a node where loopback
		// ICMP is filtered (Run returns nil, 0 packets) can still ping real targets,
		// so keying on PacketsRecv would falsely latch it "incapable" and suppress
		// all ping reporting. A genuine no-capability node fails Run (socket create /
		// permission), which is what we screen for.
		p.cap.ok = lp.Run() == nil
	})
	return p.cap.ok
}

// Ping runs one icmp probe against target. A non-answer is data (Received==0 with
// a down reason); an error is returned only when the node cannot do ICMP at all
// or the host is unresolvable.
func (p ICMPPinger) Ping(_ context.Context, target string, count int, timeout time.Duration) (PingResult, error) {
	pinger, err := probing.NewPinger(target)
	if err != nil {
		return PingResult{}, fmt.Errorf("collection: icmp new pinger %s: %w", target, err)
	}
	if count <= 0 {
		count = 1
	}
	pinger.Count = count
	pinger.Timeout = timeout
	pinger.SetPrivileged(false)
	if err := pinger.Run(); err != nil {
		// A run error is either "this node cannot ICMP at all" (inconclusive: only
		// when the loopback self-check also fails) or "this target's probe was
		// refused/blocked/unrouteable" (a reachability verdict: down + reason).
		if !p.capable() {
			return PingResult{}, fmt.Errorf("collection: icmp run %s: %w", target, err)
		}
		// Capable node: this target's probe was refused/blocked/unrouteable (or a
		// silent non-answer). Classify it as data (down + reason), never no-data.
		// The pinger resolved the target already (NewPinger), so a run error is a
		// reachability verdict; an unrecognized one reads as a silent timeout.
		reason, ok := Classify(err)
		if !ok {
			reason = Timedout
		}
		return PingResult{Reason: reason}, nil
	}
	st := pinger.Statistics()
	if st.PacketsRecv > 0 {
		return PingResult{Received: st.PacketsRecv, AvgRTT: st.AvgRtt, Reason: Responded}, nil
	}
	return PingResult{Reason: Timedout}, nil
}
