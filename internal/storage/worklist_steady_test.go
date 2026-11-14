package storage_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// httpsPollsSpec is a driver over https with three polls, so its endpoint
// carries several tasks beside the transport probe, as a soak device does.
const httpsPollsSpec = `{"version":1,"transport":"https",
	"inputs":[{"name":"host","kind":"string","required":true}],
	"polls":[
		{"name":"status","schedule":{"every":"30s"},"request":{"path":"/api/status"},"emits":[{"name":"power-state","extract":{"jsonpath":"device.power"}}]},
		{"name":"audio","schedule":{"every":"30s"},"request":{"path":"/api/audio"},"emits":[{"name":"volume","extract":{"jsonpath":"audio.volume"}}]},
		{"name":"device","schedule":{"every":"60s"},"request":{"path":"/api/device"},"emits":[{"name":"model-number","extract":{"jsonpath":"device.model"}}]}
	]}`

// An unchanged node's worklist is the same bytes on every build (#1872): it
// changes only when the node learns something, here the key it trusted on
// first use and the endpoint's rejection, each riding every task of its
// endpoint, and then holds again. The bus encodes the reply from this value
// alone, so steady bytes here are a steady reply.
func TestAnUnchangedNodesWorklistBuildsTheSameBytes(t *testing.T) {
	t.Parallel()
	f := newTLSFixture(t)
	drv := "panel-https"
	if _, err := f.gw.CreateDriver(f.ctx, "", storage.Driver{Name: drv, Label: "Panel HTTPS", Version: "1.0.0", Spec: []byte(httpsPollsSpec)}); err != nil {
		t.Fatal(err)
	}
	ep, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &f.component, Node: &f.node,
		Inputs: map[string]string{"host": "10.0.0.9"}}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	build := func() (storage.Worklist, []byte) {
		t.Helper()
		wl, err := f.gw.NodeWorklist(f.ctx, f.node)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(wl)
		if err != nil {
			t.Fatal(err)
		}
		return wl, b
	}
	steady := func(stage string) (storage.Worklist, []byte) {
		t.Helper()
		wl, first := build()
		if _, again := build(); string(first) != string(again) {
			t.Fatalf("%s: two builds differ:\n%s\n%s", stage, first, again)
		}
		return wl, first
	}
	tasksOf := func(wl storage.Worklist) []storage.WorklistTask {
		var out []storage.WorklistTask
		for _, task := range wl.Tasks {
			if task.EndpointID == ep.ID {
				out = append(out, task)
			}
		}
		return out
	}

	fresh, freshBytes := steady("unchanged")
	if n := len(tasksOf(fresh)); n < 3 {
		t.Fatalf("the endpoint carries %d tasks, want several so the order and every task's pin are held", n)
	}

	learn := func(k, v string) {
		t.Helper()
		if err := insertProperties(f.ctx, f.gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: f.component,
			Key: k, Instance: ep.Name, Value: v, Source: "https", TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	learn("tls-certificate", key("A"))
	pinned, pinnedBytes := steady("pin learned")
	if len(pinnedBytes) <= len(freshBytes) {
		t.Errorf("a learned pin left the worklist at %d bytes, want it larger than %d", len(pinnedBytes), len(freshBytes))
	}
	for _, task := range tasksOf(pinned) {
		if task.CertificatePin != key("A") {
			t.Errorf("task %s carries pin %q, want the learned one on every task of its endpoint", task.ID, task.CertificatePin)
		}
	}

	learn("endpoint-rejection", "responsive certificate "+key("B"))
	rejected, rejectedBytes := steady("rejection recorded")
	if len(rejectedBytes) <= len(pinnedBytes) {
		t.Errorf("a recorded rejection left the worklist at %d bytes, want it larger than %d", len(rejectedBytes), len(pinnedBytes))
	}
	for _, task := range tasksOf(rejected) {
		if task.Rejection == "" {
			t.Errorf("task %s carries no rejection, want its endpoint's on every task", task.ID)
		}
	}
}
