package ingress_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/ingress"
)

// counts flattens a window's rows to endpoint -> (posts, refusals), failing on
// an endpoint carried twice.
func counts(t *testing.T, rows []ingress.PostCount) map[string][2]*int {
	t.Helper()
	out := map[string][2]*int{}
	for _, r := range rows {
		if _, dup := out[r.EndpointID]; dup {
			t.Fatalf("endpoint %s carried twice in one window", r.EndpointID)
		}
		out[r.EndpointID] = [2]*int{r.Posts, r.Refusals}
	}
	return out
}

func val(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// A window counts each endpoint's accepted posts and its refusals, the
// component and endpoint each row lands on carried with it. A refusal count
// is reported only when nonzero.
func TestAPostWindowCountsEachEndpointsPosts(t *testing.T) {
	w := ingress.NewPostWindow()
	w.Accepted(ingress.PostTarget{EndpointID: "ep-1", ComponentID: "c-1", Endpoint: "webhook"})
	w.Accepted(ingress.PostTarget{EndpointID: "ep-1", ComponentID: "c-1", Endpoint: "webhook"})
	w.Refused(ingress.PostTarget{EndpointID: "ep-1", ComponentID: "c-1", Endpoint: "webhook"})
	w.Refused(ingress.PostTarget{EndpointID: "ep-2", ComponentID: "c-2", Endpoint: "hook"})

	rows := w.Take()
	got := counts(t, rows)
	if val(got["ep-1"][0]) != 2 || val(got["ep-1"][1]) != 1 {
		t.Errorf("ep-1 = posts %v, refusals %v; want 2 and 1", val(got["ep-1"][0]), val(got["ep-1"][1]))
	}
	if val(got["ep-2"][0]) != nil || val(got["ep-2"][1]) != 1 {
		t.Errorf("ep-2 = posts %v, refusals %v; want no post count (none landed, none before) and 1 refusal", val(got["ep-2"][0]), val(got["ep-2"][1]))
	}
	for _, r := range rows {
		if r.EndpointID == "ep-1" && (r.ComponentID != "c-1" || r.Endpoint != "webhook") {
			t.Errorf("ep-1's row lands on %s[%s], want c-1[webhook]", r.ComponentID, r.Endpoint)
		}
	}
}

// A window with posts is followed by one window reporting zero, so the
// series shows the stream stopped; after that a quiet endpoint reports
// nothing (its last heard says how long it has been quiet), and the window
// has nothing pending, so the server holds no timer for it.
func TestAPostWindowReportsOneZeroAfterAQuietingStream(t *testing.T) {
	w := ingress.NewPostWindow()
	target := ingress.PostTarget{EndpointID: "ep-1", ComponentID: "c-1", Endpoint: "webhook"}
	w.Accepted(target)
	if got := counts(t, w.Take()); val(got["ep-1"][0]) != 1 {
		t.Fatalf("first window = %v, want 1 post", val(got["ep-1"][0]))
	}
	if !w.Pending() {
		t.Fatal("a window after posts has nothing pending: its zero would never be reported")
	}
	got := counts(t, w.Take())
	if val(got["ep-1"][0]) != 0 || val(got["ep-1"][1]) != nil {
		t.Fatalf("the quiet window = posts %v, refusals %v; want 0 and none", val(got["ep-1"][0]), val(got["ep-1"][1]))
	}
	if w.Pending() {
		t.Error("the window is still pending after its zero")
	}
	if rows := w.Take(); len(rows) != 0 {
		t.Errorf("a second quiet window reported %+v, want nothing", rows)
	}
}
