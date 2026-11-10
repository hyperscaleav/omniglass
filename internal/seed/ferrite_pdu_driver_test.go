package seed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// TestTheShippedFerritePDUAPIDriverSwitchesAnOutlet (#1653): ferrite-pdu-api
// validates, and each outlet binding renders POST /api/outlets/state with the
// command's outlet as a number and its fixed state. Sent to the simulated
// PDU's http-api face behind its login, the rendered outlet-off switches that
// outlet and no other, and outlet-on switches it back.
func TestTheShippedFerritePDUAPIDriverSwitchesAnOutlet(t *testing.T) {
	sp := shippedSpec(t, "ferrite-pdu-api")
	if err := sp.Validate(nil); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if sp.Transport != "http" || sp.Session == nil || sp.Session.Scheme != driver.SchemeBasic {
		t.Fatalf("transport %q session %+v, want http behind basic auth", sp.Transport, sp.Session)
	}
	binding := func(ct string) driver.CommandBinding {
		t.Helper()
		for _, c := range sp.Commands {
			if c.CommandType == ct {
				return c
			}
		}
		t.Fatalf("ferrite-pdu-api binds no %s", ct)
		return driver.CommandBinding{}
	}
	render := func(ct string) driver.Request {
		t.Helper()
		req, err := driver.RenderRequest(binding(ct).Request, "", map[string]any{"outlet": json.Number("2")})
		if err != nil {
			t.Fatalf("render %s: %v", ct, err)
		}
		if req.Method != http.MethodPost || req.Path != "/api/outlets/state" {
			t.Fatalf("%s renders %s %s, want POST /api/outlets/state", ct, req.Method, req.Path)
		}
		return req
	}
	off := render("outlet-off")
	var body map[string]any
	if err := json.Unmarshal(off.Body, &body); err != nil {
		t.Fatalf("outlet-off body %s: %v", off.Body, err)
	}
	if body["outlet"] != float64(2) || body["state"] != "off" || len(body) != 2 {
		t.Fatalf("outlet-off body = %s, want {\"outlet\": 2, \"state\": \"off\"}", off.Body)
	}
	if _, err := driver.RenderRequest(binding("outlet-off").Request, "", nil); err == nil {
		t.Fatal("outlet-off rendered with no outlet param, want a refusal")
	}

	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := sim.NewEngine(store, profiles.Templates())
	if err := e.LoadDevices([]sim.DeviceSnapshot{{DeviceID: "pdu", Hostname: "pdu", LoopbackIP: "127.0.0.1", ProfileName: "ferrite-pdu-4c", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	dev := e.Devices()[0]
	send := func(req driver.Request) {
		t.Helper()
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(req.Method, req.Path, bytes.NewReader(req.Body))
		r.Header.Set("Content-Type", "application/json")
		r.SetBasicAuth("ferrite", "ferrite")
		dev.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", req.Method, req.Path, rec.Code, rec.Body)
		}
	}
	state := func(outlet int) string {
		return fmt.Sprint(dev.ReadPath(fmt.Sprintf("status.outlets.%d.state", outlet-1)))
	}

	send(off)
	if got := []string{state(1), state(2), state(3)}; got[0] != "on" || got[1] != "off" || got[2] != "on" {
		t.Fatalf("outlets 1 to 3 after outlet-off on 2 = %v, want [on off on]", got)
	}
	send(render("outlet-on"))
	if got := state(2); got != "on" {
		t.Fatalf("outlet 2 after outlet-on = %q, want on", got)
	}

	// Its one poll, the login's carrier to the node, reads the document's
	// agent block.
	poll := sp.Polls[0]
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, poll.Request.Path, nil)
	r.SetBasicAuth("ferrite", "ferrite")
	dev.ServeHTTP(rec, r)
	got, faults := bake(poll.Name, poll.Records, poll.Identity, poll.Emits).Interpret(driver.HTTPPayload(rec.Body.Bytes(), &poll.Request), time.Now())
	if len(faults) > 0 {
		t.Fatalf("identity poll faulted: %v", faults)
	}
	read := map[string]string{}
	for _, smp := range got {
		read[smp.Name] = smp.Text
	}
	for key, path := range map[string]string{"model-number": "configuration.device.model", "serial-number": "configuration.device.serial", "firmware-version": "configuration.firmware.version"} {
		if want := fmt.Sprint(dev.ReadPath(path)); read[key] != want {
			t.Errorf("%s = %q, want %q", key, read[key], want)
		}
	}
}
