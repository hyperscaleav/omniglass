//go:build linux

package sim

import (
	"net/http"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Boreal Edge display's state over SNMP (#1372): the power and the
// selected input a display is monitored by, read off the Boreal arc and moved
// by the web admin's control routes, from one live state.

const (
	edgeOIDPower = "1.3.6.1.4.1.55520.1.1.1.0"
	edgeOIDInput = "1.3.6.1.4.1.55520.2.2.0"
)

func startEdge(t *testing.T) *Device {
	t.Helper()
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "display-1", Hostname: "display-1", LoopbackIP: ip, ProfileName: "boreal-edge-86", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return deviceByProfile(t, e, "boreal-edge-86")
}

func edgeOperate(t *testing.T, d *Device, path, body string, want int) {
	t.Helper()
	if res := d.Operate(Operation{Kind: "http", Method: http.MethodPost, Path: path, Body: body}); res.Status != want {
		t.Fatalf("POST %s %s: status %d, want %d (%+v)", path, body, res.Status, want, res)
	}
}

// TestBorealEdgeServesPowerAndInputOverSNMP: at rest the display reads on
// and hdmi1, and a power or input change made on the web admin reads back
// over SNMP.
func TestBorealEdgeServesPowerAndInputOverSNMP(t *testing.T) {
	d := startEdge(t)
	got := beamSNMP(t, d, edgeOIDPower, edgeOIDInput)
	if got[edgeOIDPower] != "on" || got[edgeOIDInput] != "hdmi1" {
		t.Fatalf("SNMP at rest = %v, want on, hdmi1", got)
	}
	edgeOperate(t, d, "/api/display/power", `{"power": "standby"}`, http.StatusOK)
	edgeOperate(t, d, "/api/display/input", `{"input": "usbc"}`, http.StatusOK)
	got = beamSNMP(t, d, edgeOIDPower, edgeOIDInput)
	if got[edgeOIDPower] != "standby" || got[edgeOIDInput] != "usbc" {
		t.Errorf("SNMP after the writes = %v, want standby, usbc", got)
	}
}

// TestBorealEdgeRefusesAnInputItDoesNotHave: an input outside the ones the
// panel carries is refused and the selection holds, so a collector never
// reads a value the property's enum rejects.
func TestBorealEdgeRefusesAnInputItDoesNotHave(t *testing.T) {
	d := startEdge(t)
	edgeOperate(t, d, "/api/display/input", `{"input": "vga"}`, http.StatusBadRequest)
	if v := beamSNMP(t, d, edgeOIDInput)[edgeOIDInput]; v != "hdmi1" {
		t.Errorf("SNMP input after a refused write = %q, want hdmi1", v)
	}
}

// TestBorealEdgeRebootRestoresItsInput: a reboot returns the selection to
// its default, the input switching a collector sees reversed.
func TestBorealEdgeRebootRestoresItsInput(t *testing.T) {
	d := startEdge(t)
	edgeOperate(t, d, "/api/display/input", `{"input": "dp1"}`, http.StatusOK)
	edgeOperate(t, d, "/api/admin/reboot", `{}`, http.StatusOK)
	if v := beamSNMP(t, d, edgeOIDInput)[edgeOIDInput]; v != "hdmi1" {
		t.Errorf("SNMP input after a reboot = %q, want hdmi1", v)
	}
}
