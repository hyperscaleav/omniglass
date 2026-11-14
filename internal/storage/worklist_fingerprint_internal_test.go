package storage

import (
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/fingerprint"
)

// A node's fingerprints are computed once per endpoint, however many tasks it
// runs, and every task of the endpoint carries its endpoint's (#1583).
func TestAnEndpointIsFingerprintedOnce(t *testing.T) {
	t.Parallel()
	var tasks []WorklistTask
	for e := range 3 {
		for m := range 5 {
			tasks = append(tasks, WorklistTask{ID: fmt.Sprintf("%d-%d", e, m), EndpointID: fmt.Sprintf("ep-%d", e)})
		}
	}
	calls := 0
	inputsOf := func(idx int) fingerprint.Inputs { return fingerprint.Inputs{Transport: tasks[idx].EndpointID} }
	of := func(in fingerprint.Inputs) string {
		calls++
		return "fp-" + in.Transport
	}
	fingerprintEndpoints(tasks, inputsOf, of)
	if calls != 3 {
		t.Errorf("fingerprinted %d times for 3 endpoints of 15 tasks, want 3", calls)
	}
	for _, task := range tasks {
		if task.Fingerprint != "fp-"+task.EndpointID {
			t.Errorf("task %s carries %q, want its endpoint's", task.ID, task.Fingerprint)
		}
	}
}
