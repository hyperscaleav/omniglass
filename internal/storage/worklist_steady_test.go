package storage_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// An unchanged node's worklist is the same bytes on every build (#1872): it
// grows only when the node learns something, here the key it trusted on first
// use, which rides every task of its endpoint, and then holds again.
func TestAnUnchangedNodesWorklistBuildsTheSameBytes(t *testing.T) {
	t.Parallel()
	f := newTLSFixture(t)
	ep, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Transport: "https", Component: &f.component, Node: &f.node,
		Params: []byte(`{"target":"10.0.0.9:443","tls-mode":"pin"}`)}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	build := func() []byte {
		t.Helper()
		wl, err := f.gw.NodeWorklist(f.ctx, f.node)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(wl)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	first, again := build(), build()
	if string(first) != string(again) {
		t.Fatalf("two builds of an unchanged worklist differ:\n%s\n%s", first, again)
	}

	if err := insertProperties(f.ctx, f.gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: f.component,
		Key: "tls-certificate", Instance: ep.Name, Value: key("A"), Source: "https", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	learned, held := build(), build()
	if len(learned) <= len(first) {
		t.Errorf("a learned pin left the worklist at %d bytes, want it larger than %d", len(learned), len(first))
	}
	if string(learned) != string(held) {
		t.Fatalf("two builds after the pin was learned differ:\n%s\n%s", learned, held)
	}
	wl, err := f.gw.NodeWorklist(f.ctx, f.node)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range wl.Tasks {
		if task.EndpointID == ep.ID && task.CertificatePin != key("A") {
			t.Errorf("task %s of the endpoint carries pin %q, want the learned one on every task", task.ID, task.CertificatePin)
		}
	}
}
