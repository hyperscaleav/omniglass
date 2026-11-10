package api_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/simhub"
)

// The ring brackets only nodes whose simulator pulled inside the connected
// window: a simulator that went quiet drops out, so its node warms up again
// when it comes back.
func TestSimLoopNodes(t *testing.T) {
	t.Parallel()
	hub := &fakeHub{Registry: simhub.New()}
	hub.Pulled("site-a", collection.SimCapabilities{}, nil)
	hub.Pulled("site-b", collection.SimCapabilities{}, nil)
	if got := api.SimLoopNodes(hub, time.Now)(); !slices.Equal(got, []string{"site-a", "site-b"}) {
		t.Fatalf("connected = %v, want both nodes", got)
	}
	later := func() time.Time { return time.Now().Add(5 * time.Minute) }
	if got := api.SimLoopNodes(hub, later)(); len(got) != 0 {
		t.Fatalf("quiet for 5m = %v, want none", got)
	}
}

// A node's device reads are every device its simulator was handed, with the
// hits it reported by protocol and whether the simulator impairs it; a
// device handed but never hit reads with no hits.
func TestSimLoopSourceObservesDevices(t *testing.T) {
	t.Parallel()
	hub := &fakeHub{Registry: simhub.New(), observation: map[string]collection.EndpointObservation{"site-a": {}}}
	hub.Pulled("site-a", collection.SimCapabilities{}, []string{"c-hit", "c-quiet"})
	hub.Report("site-a", collection.SimHitBatch{
		Hits: []collection.SimHit{
			{ComponentID: "c-hit", Protocol: "tcp"}, {ComponentID: "c-hit", Protocol: "tcp"}, {ComponentID: "c-hit", Protocol: "http"},
		},
		Impairments: &collection.SimImpairmentStatus{Available: true, Impaired: []string{"c-hit"}},
	})
	_, devices, err := api.NewSimLoopSource(hub, nil, closedloop.NodeCadence{}).Observe(context.Background(), "site-a")
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	hit, quiet := devices["c-hit"], devices["c-quiet"]
	if len(devices) != 2 || hit.ByProtocol["tcp"] != 2 || hit.ByProtocol["http"] != 1 || !hit.Impaired {
		t.Fatalf("devices = %+v, want c-hit impaired with 2 tcp and 1 http", devices)
	}
	if len(quiet.ByProtocol) != 0 || quiet.Impaired {
		t.Fatalf("c-quiet = %+v, want no hits and not impaired", quiet)
	}
}

// A node with no simulator answers the hub's error, so the ring says why.
func TestSimLoopSourceNoSimulator(t *testing.T) {
	t.Parallel()
	hub := &fakeHub{Registry: simhub.New()}
	if _, _, err := api.NewSimLoopSource(hub, nil, closedloop.NodeCadence{}).Observe(context.Background(), "site-z"); err == nil {
		t.Fatal("observe of a node with no simulator answered no error")
	}
}
