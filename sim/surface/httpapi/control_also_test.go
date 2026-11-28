package httpapi

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// A control route may write more than the fields it was sent (#2833): a route
// that routes an input lands the signal on it in the same change, so the
// acknowledgment already reads it.

func routeControl() Control {
	return Control{
		Match:  "POST /route",
		Fields: []Field{{Path: "p", Name: "input", Kind: Choice, Values: []string{"a", "b"}}},
		Also: func(d spec.Device, set map[string]any) []surface.Change {
			return []surface.Change{{Path: "m", Value: "signal on " + set["p"].(string)}}
		},
		Ack: func(d spec.Device) any { return ack{true, spec.String(d, "m")} },
	}
}

func TestControlWritesWhatTheRouteDerivesInTheSameChange(t *testing.T) {
	d := newFake()
	res := routeControl().Route().Handle(d, spec.HTTPRequest{Body: map[string]any{"input": "b"}})
	if res.Status != 0 || string(res.Body.([]byte)) != "{\"ok\": true, \"power\": \"signal on b\"}\n" {
		t.Fatalf("response = %+v %s", res, res.Body)
	}
	if len(d.writes) != 1 || len(d.writes[0]) != 2 || d.state["p"] != "b" || d.state["m"] != "signal on b" {
		t.Fatalf("writes %+v, state %v: want one change of both paths", d.writes, d.state)
	}
}

func TestControlDerivesNothingFromARefusedRequest(t *testing.T) {
	d := newFake()
	if res := routeControl().Route().Handle(d, spec.HTTPRequest{Body: map[string]any{"input": "z"}}); res.Status != 400 || len(d.writes) != 0 {
		t.Fatalf("a refused request: %d, writes %+v", res.Status, d.writes)
	}
}
