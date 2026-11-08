package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A node's worklist pull costs a number of statements that does not grow with
// the endpoints it carries (#1571). The pull ran a key-pin read per ssh or tls
// endpoint and a rejection read per component endpoint, so a 1,000-system
// corporate lab's headquarters node (806 components, 4,434 tasks) issued about
// 4,300 round trips per pull: past the node's 5 s pull timeout once the lab was
// loaded, a restart loop and a lab that never came up.

// worklistProbe is a node with `endpoints` tls endpoints, each on its own
// component with its own trusted key and its own rejection, and the worklist
// pulled once: the statements the pull issued and the worklist it returned.
func worklistProbe(t *testing.T, endpoints int) (int, storage.Worklist) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, counter := storagetest.NewCountingDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	node := "edge-w"
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: node}, all, all); err != nil {
		t.Fatal(err)
	}
	var writes []storage.PropertySampleWrite
	now := time.Now()
	for i := range endpoints {
		comp := fmt.Sprintf("panel-w%d", i)
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tls", Component: &comp, Node: &node,
			Params: []byte(fmt.Sprintf(`{"target":"10.0.0.%d:51326","tls-mode":"pin"}`, i+1))}, all); err != nil {
			t.Fatal(err)
		}
		writes = append(writes,
			storage.PropertySampleWrite{OwnerKind: "component", OwnerID: comp, Key: "tls-certificate", Instance: "tls", Value: key(string(rune('A' + i))), Source: "tls", TS: now},
			storage.PropertySampleWrite{OwnerKind: "component", OwnerID: comp, Key: "endpoint-rejection", Instance: "tls", Value: fmt.Sprintf("rejected %d", i), Source: "tls", TS: now},
		)
	}
	if err := insertProperties(ctx, gw.(sampleSink), writes); err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	wl, err := gw.NodeWorklist(ctx, node)
	if err != nil {
		t.Fatal(err)
	}
	return counter.N(), wl
}

func TestNodeWorklistStatementsDoNotGrowWithEndpoints(t *testing.T) {
	t.Parallel()
	small, _ := worklistProbe(t, 2)
	n, wl := worklistProbe(t, 9)
	if n != small {
		t.Errorf("a worklist of 9 tls endpoints cost %d statements, 2 cost %d: the pull must cost the same however many endpoints the node carries", n, small)
	}
	// The batched reads hand each endpoint its own pin and its own rejection.
	pins := map[string]bool{}
	for _, task := range wl.Tasks {
		if task.CertificatePin == "" || task.Rejection == "" {
			t.Errorf("task %s (%s) lost its pin %q or rejection %q", task.ID, task.EndpointName, task.CertificatePin, task.Rejection)
		}
		pins[task.CertificatePin+"|"+task.Rejection] = true
	}
	if len(pins) != 9 {
		t.Errorf("9 endpoints carried %d distinct pin and rejection pairs, want 9: the batch crossed endpoints", len(pins))
	}
}
