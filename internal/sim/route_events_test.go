package sim

import (
	"net"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// A route may declare named events (#1145): a codec reports a call starting
// and ending as named occurrences carrying the call's id (xAPI's
// CallSuccessful and CallDisconnect), not only as changes to its state, so a
// driver can map one occurrence to one event. Each fires once its route's
// mutations apply, rendered against the state they left, numbered in the
// same stream as the changes.
func TestARoutesNamedEventsStreamAfterItsMutations(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev := NewDevice("d", "codec", net.ParseIP("127.0.0.1"), "p", 1, map[string]any{"call": map[string]any{"active": false, "id": ""}})
	if err := dev.AddRouteFromProfile(&profile.Route{
		Match:     "POST /api/call/dial",
		Template:  "ack.json.gotempl",
		Mutations: []profile.Mutation{{Path: "call.active", Value: true}, {Path: "call.id", Value: "{{ .Request.Body.id }}"}},
		Events:    []profile.RouteEvent{{Name: "call-started", Data: `{"id":"{{ .Request.Body.id }}"}`}},
	}, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}
	if err := dev.AddRouteFromProfile(&profile.Route{
		Match:    "POST /api/call/disconnect",
		Template: "ack.json.gotempl",
		Mutate:   map[string]any{"call.active": false},
		Events:   []profile.RouteEvent{{Name: "call-ended", Data: `{"id":"{{ .State | get "call" | get "id" }}"}`}},
	}, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}
	dev.SetEvents(profile.Events{Path: "/api/events", Paths: []string{"call.active"}})
	if err := dev.StartHTTP(0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopHTTP)
	addr := dev.HTTPAddrs()[0].String()
	_, lines := openEvents(t, addr, "")

	post(t, addr, "/api/call/dial", `{"id":"c9001"}`)
	if ev := nextEvent(t, lines); strings.Join(ev, "|") != `id: 1|event: change|data: {"path":"call.active","value":true}` {
		t.Fatalf("first = %q, want the state change", ev)
	}
	if ev := nextEvent(t, lines); strings.Join(ev, "|") != `id: 2|event: call-started|data: {"id":"c9001"}` {
		t.Fatalf("second = %q, want the named event", ev)
	}
	if res := dev.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/call/disconnect"}); res.Status != 200 {
		t.Fatalf("disconnect = %+v", res)
	}
	nextEvent(t, lines) // the change
	if ev := nextEvent(t, lines); strings.Join(ev, "|") != `id: 4|event: call-ended|data: {"id":"c9001"}` {
		t.Fatalf("fourth = %q, want call-ended with the id the state still holds", ev)
	}
}
