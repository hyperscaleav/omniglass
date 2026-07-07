package collection_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// fakeTCP is the faked probe boundary: it returns a preset verdict so the Runner
// logic is exercised without a real socket.
type fakeTCP struct {
	ms    float64
	reach collection.Reachability
	err   error
}

func (f fakeTCP) Dial(context.Context, string, time.Duration) (float64, collection.Reachability, error) {
	return f.ms, f.reach, f.err
}

func index(dps []collection.Datapoint) map[string]collection.Datapoint {
	m := make(map[string]collection.Datapoint, len(dps))
	for _, d := range dps {
		m[d.Name] = d
	}
	return m
}

// TestCollectTCPOpen: an open port yields tcp.open=1 plus a present connect_time.
func TestCollectTCPOpen(t *testing.T) {
	r := &collection.Runner{TCP: fakeTCP{ms: 4, reach: collection.Responded}}
	dps, err := r.CollectTCP(context.Background(), collection.TCPTask{Target: "10.0.0.5:22", Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := index(dps)
	open, ok := got[collection.DatapointTCPOpen]
	if !ok || open.Value != 1 {
		t.Fatalf("tcp.open: want present val=1, got %+v", open)
	}
	if open.Labels[collection.ReasonLabel] != string(collection.Responded) {
		t.Fatalf("tcp.open reason label: want responded, got %q", open.Labels[collection.ReasonLabel])
	}
	ct, ok := got[collection.DatapointTCPConnectTime]
	if !ok || ct.Value != 4 {
		t.Fatalf("tcp.connect_time: want present val=4, got %+v (present=%v)", ct, ok)
	}
}

// TestCollectTCPClosed: a refused port yields tcp.open=0 with a reason, and
// tcp.connect_time is ABSENT.
func TestCollectTCPClosed(t *testing.T) {
	r := &collection.Runner{TCP: fakeTCP{reach: collection.Refused}}
	dps, err := r.CollectTCP(context.Background(), collection.TCPTask{Target: "10.0.0.5:22"})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := index(dps)
	open, ok := got[collection.DatapointTCPOpen]
	if !ok || open.Value != 0 {
		t.Fatalf("tcp.open: want present val=0, got %+v (present=%v)", open, ok)
	}
	if open.Labels[collection.ReasonLabel] != string(collection.Refused) {
		t.Fatalf("tcp.open reason: want refused, got %q", open.Labels[collection.ReasonLabel])
	}
	if _, ok := got[collection.DatapointTCPConnectTime]; ok {
		t.Fatal("tcp.connect_time must be absent when the port is closed")
	}
}

// TestCollectTCPSetupError: an unresolved target (err from the probe) is
// inconclusive, surfaced as an error so the caller records no false down.
func TestCollectTCPSetupError(t *testing.T) {
	r := &collection.Runner{TCP: fakeTCP{err: context.DeadlineExceeded}}
	if _, err := r.CollectTCP(context.Background(), collection.TCPTask{Target: "bad:1"}); err == nil {
		t.Fatal("collect: want error on probe setup failure, got nil")
	}
}
