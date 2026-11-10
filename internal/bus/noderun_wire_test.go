package bus

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A task carries its component on the wire only beside its path upstreams
// (#1652): the node reads an upstream's component off the edge that names it,
// so the thousands of tasks reached through nothing pay no bytes for it.
func TestOnlyATaskReachedThroughAnUpstreamCarriesItsComponent(t *testing.T) {
	t.Parallel()
	alone := taskSpecOf(storage.WorklistTask{EndpointID: "gw-1", ComponentID: "comp-gw"})
	if alone.ComponentID != "" || alone.PathUpstreams != nil {
		t.Fatalf("a task reached through nothing carried %q and %+v", alone.ComponentID, alone.PathUpstreams)
	}
	behind := taskSpecOf(storage.WorklistTask{EndpointID: "dev-1", ComponentID: "comp-dev",
		PathUpstreams: []storage.PathUpstream{{ComponentID: "comp-gw", ComponentName: "gateway", EndpointIDs: []string{"gw-1"}}}})
	if behind.ComponentID != "comp-dev" || len(behind.PathUpstreams) != 1 || behind.PathUpstreams[0].ComponentID != "comp-gw" ||
		behind.PathUpstreams[0].ComponentName != "gateway" || len(behind.PathUpstreams[0].EndpointIDs) != 1 {
		t.Fatalf("a dependent task = %q, %+v; want comp-dev reached through comp-gw", behind.ComponentID, behind.PathUpstreams)
	}
}
