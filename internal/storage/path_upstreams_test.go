package storage_test

import (
	"context"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestNodeWorklistCarriesPathUpstreams pins #1652's worklist end: an in-band
// endpoint of a component reached through another carries the upstream, by
// id and name, with the upstream's in-band endpoints on the same node. An
// out-of-band endpoint carries none, an upstream with no in-band endpoint on
// the node is left out, power and host edges are not path upstreams, and
// deleting the edge drops it.
func TestNodeWorklistCarriesPathUpstreams(t *testing.T) {
	t.Parallel()
	f := newDepFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	for _, c := range []string{"gw-p", "dev-p", "far-p", "pdu-p", "host-p"} {
		f.component(t, c)
	}
	if _, err := f.gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-far"}, all, all); err != nil {
		t.Fatalf("create far node: %v", err)
	}
	gwIn := f.endpoint(t, "gw-p", "icmp", false)
	f.endpoint(t, "gw-p", "http", true)
	devIn := f.endpoint(t, "dev-p", "icmp", false)
	devOOB := f.endpoint(t, "dev-p", "http", true)
	far, comp := "edge-far", "far-p"
	if _, err := f.gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "icmp", Component: &comp, Node: &far}, all); err != nil {
		t.Fatalf("place far endpoint: %v", err)
	}
	f.endpoint(t, "pdu-p", "icmp", false)
	f.endpoint(t, "host-p", "icmp", false)
	f.depend(t, "dev-p", "gw-p", storage.DependencyPath, "")
	f.depend(t, "dev-p", "far-p", storage.DependencyPath, "")
	f.depend(t, "dev-p", "pdu-p", storage.DependencyPower, "3")
	f.depend(t, "dev-p", "host-p", storage.DependencyHost, "")

	byEndpoint := func() map[string]storage.WorklistTask {
		t.Helper()
		wl, err := f.gw.NodeWorklist(ctx, f.node)
		if err != nil {
			t.Fatalf("worklist: %v", err)
		}
		out := map[string]storage.WorklistTask{}
		for _, task := range wl.Tasks {
			out[task.EndpointID] = task
		}
		return out
	}
	tasks := byEndpoint()
	gwTask, devTask, oobTask := tasks[gwIn], tasks[devIn], tasks[devOOB]
	if gwTask.ComponentID == "" || devTask.ComponentID == "" || devTask.ComponentID == gwTask.ComponentID {
		t.Fatalf("component ids gw %q dev %q; want each its own", gwTask.ComponentID, devTask.ComponentID)
	}
	want := []storage.PathUpstream{{ComponentID: gwTask.ComponentID, ComponentName: "gw-p", EndpointIDs: []string{gwIn}}}
	if !slices.EqualFunc(devTask.PathUpstreams, want, samePathUpstream) {
		t.Fatalf("dev path upstreams = %+v; want only the gateway's in-band endpoint on this node %+v", devTask.PathUpstreams, want)
	}
	if len(oobTask.PathUpstreams) != 0 {
		t.Fatalf("out-of-band endpoint carries %+v; want none", oobTask.PathUpstreams)
	}
	if len(gwTask.PathUpstreams) != 0 {
		t.Fatalf("the upstream carries %+v; want none", gwTask.PathUpstreams)
	}

	if err := f.gw.DeleteComponentDependency(ctx, f.actor, "dev-p", storage.DependencyPath, "gw-p", all, all); err != nil {
		t.Fatalf("delete path edge: %v", err)
	}
	if got := byEndpoint()[devIn].PathUpstreams; len(got) != 0 {
		t.Fatalf("dev path upstreams after the edge was deleted = %+v; want none", got)
	}
}

func samePathUpstream(a, b storage.PathUpstream) bool {
	return a.ComponentID == b.ComponentID && a.ComponentName == b.ComponentName && slices.Equal(a.EndpointIDs, b.EndpointIDs)
}
