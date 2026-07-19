package collection_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// fakeICMP is the faked probe boundary: it returns a preset ping result so the
// Runner logic is exercised without a real ICMP socket.
type fakeICMP struct {
	res collection.PingResult
	err error
}

func (f fakeICMP) Ping(context.Context, string, int, time.Duration) (collection.PingResult, error) {
	return f.res, f.err
}

// TestCollectICMPReachable: an echoing target yields icmp.reachable=1 plus a
// present icmp.rtt_avg, and the reachable datapoint carries a responded reason.
func TestCollectICMPReachable(t *testing.T) {
	r := &collection.Runner{Ping: fakeICMP{res: collection.PingResult{
		Received: 1, AvgRTT: 3 * time.Millisecond, Reason: collection.Responded,
	}}}
	dps, err := r.CollectICMP(context.Background(), collection.ICMPTask{Target: "10.0.0.5", Count: 1, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := index(dps)
	reach, ok := got[collection.DatapointICMPReachable]
	if !ok || reach.Value != 1 {
		t.Fatalf("icmp.reachable: want present val=1, got %+v (present=%v)", reach, ok)
	}
	if reach.Labels[collection.ReasonLabel] != string(collection.Responded) {
		t.Fatalf("icmp.reachable reason: want responded, got %q", reach.Labels[collection.ReasonLabel])
	}
	rtt, ok := got[collection.DatapointICMPRTTAvg]
	if !ok || rtt.Value != 3 {
		t.Fatalf("icmp.rtt_avg: want present val=3, got %+v (present=%v)", rtt, ok)
	}
}

// TestCollectICMPUnreachable: a target that did not echo yields icmp.reachable=0
// with a down reason, and icmp.rtt_avg is ABSENT. A non-answer is DATA, not error.
func TestCollectICMPUnreachable(t *testing.T) {
	r := &collection.Runner{Ping: fakeICMP{res: collection.PingResult{Received: 0, Reason: collection.Timedout}}}
	dps, err := r.CollectICMP(context.Background(), collection.ICMPTask{Target: "10.0.0.9"})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := index(dps)
	reach, ok := got[collection.DatapointICMPReachable]
	if !ok || reach.Value != 0 {
		t.Fatalf("icmp.reachable: want present val=0, got %+v (present=%v)", reach, ok)
	}
	if reach.Labels[collection.ReasonLabel] != string(collection.Timedout) {
		t.Fatalf("icmp.reachable reason: want timeout, got %q", reach.Labels[collection.ReasonLabel])
	}
	if _, ok := got[collection.DatapointICMPRTTAvg]; ok {
		t.Fatal("icmp.rtt_avg must be absent when the target is unreachable")
	}
}

// TestCollectICMPInconclusive: a node that cannot attempt ICMP at all (err from
// the probe) is inconclusive, surfaced as an error so the caller records no
// false down.
func TestCollectICMPInconclusive(t *testing.T) {
	r := &collection.Runner{Ping: fakeICMP{err: context.DeadlineExceeded}}
	if _, err := r.CollectICMP(context.Background(), collection.ICMPTask{Target: "10.0.0.9"}); err == nil {
		t.Fatal("collect: want error on probe capability failure, got nil")
	}
}
