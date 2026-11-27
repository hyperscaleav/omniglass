//go:build linux

package sim

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Wire parity of the Boreal families with the YAML profiles they replaced
// (#2832). The golden transcripts were recorded from the profiles; each test
// here replays the same script against the Go family and holds its bytes,
// statuses, headers, state, SNMP types and values, drift sequences and room-day
// behavior to them. This file is the worked example of how a family proves it
// reads identically to the profile it replaces.

func canon(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("not JSON: %v: %s", err, raw)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// The two places the YAML dashboard named "Edge 86" whatever the size: the
// title and the bar. A product reports its own model in both now (#2832).
const (
	yamlTitle = ", Boreal Edge 86</title>"
	yamlBar   = `<div class="model">Edge 86</div>`
)

// asOwnModel is the golden dashboard with the model the product reports.
func asOwnModel(t *testing.T, product, golden string) string {
	t.Helper()
	if product == "boreal-edge-86" || !strings.Contains(golden, "<!DOCTYPE html>") {
		return golden
	}
	if !strings.Contains(golden, yamlTitle) || !strings.Contains(golden, yamlBar) {
		t.Fatalf("%s: the golden dashboard no longer names Edge 86 in its title and bar", product)
	}
	model := "Edge " + strings.TrimPrefix(product, "boreal-edge-")
	golden = strings.Replace(golden, yamlTitle, ", Boreal "+model+"</title>", 1)
	return strings.Replace(golden, yamlBar, `<div class="model">`+model+`</div>`, 1)
}

func replayWire(t *testing.T, product string, script, writes []wireStep, oids []string, ticks []tickPlan, walks []string) {
	t.Helper()
	g := readGolden(t, product)

	d := wireDevice(t, shippedEngine(t), product)
	if got, want := canon(t, stateJSON(d)), canon(t, g.State); got != want {
		t.Errorf("initial state:\n got %s\nwant %s", got, want)
	}
	if oids != nil {
		if got := wireSNMPGet(t, d, oids); !reflect.DeepEqual(got, g.SNMP["rest"]) {
			t.Errorf("SNMP at rest:\n got %+v\nwant %+v", got, g.SNMP["rest"])
		}
		for _, w := range walks {
			if got := wireSNMPWalk(t, d, w); !reflect.DeepEqual(got, g.Walks[w+"@rest"]) {
				t.Errorf("SNMP walk of %s at rest:\n got %+v\nwant %+v", w, got, g.Walks[w+"@rest"])
			}
		}
		runWireHTTP(t, d, writes)
		if got := wireSNMPGet(t, d, oids); !reflect.DeepEqual(got, g.SNMP["written"]) {
			t.Errorf("SNMP after the writes:\n got %+v\nwant %+v", got, g.SNMP["written"])
		}
		for _, w := range walks {
			if got := wireSNMPWalk(t, d, w); !reflect.DeepEqual(got, g.Walks[w+"@written"]) {
				t.Errorf("SNMP walk of %s after the writes:\n got %+v\nwant %+v", w, got, g.Walks[w+"@written"])
			}
		}
		d = wireDevice(t, shippedEngine(t), product)
	}

	got := runWireHTTP(t, d, script)
	if len(got) != len(g.HTTP) {
		t.Fatalf("%d steps replayed, %d recorded", len(got), len(g.HTTP))
	}
	for i, want := range g.HTTP {
		have := got[i]
		name := have.Name
		if want.Quirk != "" {
			// An accident of the YAML templates the family refuses instead: the
			// request is a 400 and changes nothing.
			if have.Status != 400 || have.StateChange {
				t.Errorf("%s: %s: status %d, state changed %v; want a refusal that changes nothing (the profile: %s)", product, name, have.Status, have.StateChange, want.Quirk)
			}
			continue
		}
		if have.Status != want.Status {
			t.Errorf("%s: %s: status %d, want %d", product, name, have.Status, want.Status)
		}
		if !reflect.DeepEqual(have.Headers, want.Headers) {
			t.Errorf("%s: %s: headers %v, want %v", product, name, have.Headers, want.Headers)
		}
		if wantBody := asOwnModel(t, product, want.Response); have.Response != wantBody {
			t.Errorf("%s: %s: body differs from the recorded profile's:\n got %q\nwant %q", product, name, firstDiff(have.Response, wantBody), firstDiff(wantBody, have.Response))
		}
		if have.StateChange != want.StateChange {
			t.Errorf("%s: %s: state changed %v, recorded %v", product, name, have.StateChange, want.StateChange)
		}
		if want.State != nil && canon(t, have.State) != canon(t, want.State) {
			t.Errorf("%s: %s: state after:\n got %s\nwant %s", product, name, canon(t, have.State), canon(t, want.State))
		}
	}

	gotTicks := recordTicks(wireDevice(t, shippedEngine(t), product), ticks)
	if !sameJSON(t, gotTicks, g.Ticks) {
		t.Errorf("%s: drift sequences differ from the profile's seeded run:\n got %v\nwant %v", product, gotTicks, g.Ticks)
	}
	if days := roomDayFor(t, newShippedEngine(t), product); !reflect.DeepEqual(canonChanges(t, days), canonChanges(t, g.RoomDays)) {
		for key := range g.RoomDays {
			if !reflect.DeepEqual(canonChanges(t, days)[key], canonChanges(t, g.RoomDays)[key]) {
				t.Errorf("%s: the %s room day differs from the profile's:\n got %v\nwant %v", product, key, firstChanges(days[key]), firstChanges(g.RoomDays[key]))
			}
		}
	}
}

func canonChanges(t *testing.T, m map[string][]wireChange) map[string][]string {
	out := map[string][]string{}
	for k, v := range m {
		for _, c := range v {
			b, _ := json.Marshal(c)
			out[k] = append(out[k], string(b))
		}
	}
	return out
}

func firstChanges(c []wireChange) []wireChange {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// firstDiff is the text of a from where it first differs from b, to a few
// lines on.
func firstDiff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	start := max(i-40, 0)
	return a[start:min(i+80, len(a))]
}

func TestBorealEdgeFamilyReadsAsTheProfileDidAtEverySize(t *testing.T) {
	for _, p := range borealEdgeProducts {
		t.Run(p, func(t *testing.T) {
			replayWire(t, p, edgeScript(), edgeWrites(), edgeOIDs, edgeTicks(), []string{"1.3.6.1.4.1.55520", "1.3.6.1.2.1.1"})
		})
	}
}

func TestBorealBeamFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, borealBeamProduct, beamScript(), beamWrites(), beamOIDs, beamTicks(), []string{"1.3.6.1.4.1.55520", "1.3.6.1.2.1.1"})
}

func TestBorealCastFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, borealCastProduct, castScript(), nil, nil, castTicks(), nil)
}
