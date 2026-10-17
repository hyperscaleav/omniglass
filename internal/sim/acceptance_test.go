//go:build linux

// The fleet serves on 127.x addresses beyond 127.0.0.1, which Linux treats
// as local without configuration.

package sim

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/sim/fleet"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// acceptancePortShift lifts every profile port above 1024 so the whole
// fleet serves unprivileged. Each device owns a distinct 127.x address, so
// a fixed shift cannot collide between devices.
const acceptancePortShift = 20000

// startFleet compiles an embedded fleet and serves every device in it.
func startFleet(t *testing.T, name string) (*Engine, *profile.Store) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("loading catalog: %v", err)
	}
	m, err := fleet.ParseManifestFS(profiles.Fleets(), name)
	if err != nil {
		t.Fatalf("parsing fleet: %v", err)
	}
	fl, err := fleet.Compile(m, store)
	if err != nil {
		t.Fatalf("compiling fleet: %v", err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(func(p int) int { return p + acceptancePortShift })
	if err := e.LoadDevices(Snapshots(fl)); err != nil {
		t.Fatalf("loading devices: %v", err)
	}
	if err := e.StartAll(); err != nil {
		e.StopAll()
		t.Fatalf("starting devices: %v", err)
	}
	t.Cleanup(e.StopAll)
	return e, store
}

func deviceByProfile(t *testing.T, e *Engine, name string) *Device {
	t.Helper()
	for _, d := range e.Devices() {
		if d.ProfileName == name {
			return d
		}
	}
	t.Fatalf("fleet has no %s device", name)
	return nil
}

// TestFleetServesSharedStateAcrossProtocols is the slice's acceptance
// behavior: one compiled fleet served in-process, and a simulated device
// answering HTTP, SNMP, SSH and a line-protocol subscription from one live
// state, so a write over HTTP shows in the other protocols.
func TestFleetServesSharedStateAcrossProtocols(t *testing.T) {
	e, _ := startFleet(t, "training-default.yaml")
	if n := len(e.Devices()); n < 2 {
		t.Fatalf("fleet served %d devices, want the whole fleet", n)
	}
	dsp := deviceByProfile(t, e, "polaris-dsp-16")
	ip := dsp.LoopbackIP.String()
	at := func(port int) string { return net.JoinHostPort(ip, fmt.Sprint(port+acceptancePortShift)) }

	var hitsMu sync.Mutex
	hits := map[string]bool{}
	var sessionPorts []int
	e.SetHitReporter(func(h Hit) {
		if h.DeviceID != dsp.DeviceID {
			return
		}
		hitsMu.Lock()
		hits[h.Protocol] = true
		hitsMu.Unlock()
	})
	e.SetSessionPublisher(func(ev SessionEvent) {
		if ev.DeviceID != dsp.DeviceID {
			return
		}
		hitsMu.Lock()
		sessionPorts = append(sessionPorts, ev.Port)
		hitsMu.Unlock()
	})

	// Line protocol: subscribe to channel 0 gain before the write.
	conn, err := net.DialTimeout("tcp", at(1710), 2*time.Second)
	if err != nil {
		t.Fatalf("dialing line protocol: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	lines := bufio.NewReader(conn)
	readFrame := func() string {
		t.Helper()
		frame, err := lines.ReadString(0)
		if err != nil {
			t.Fatalf("reading line-protocol frame: %v", err)
		}
		return strings.TrimSuffix(frame, "\x00")
	}
	if welcome := readFrame(); !strings.Contains(welcome, "EngineStatus") {
		t.Fatalf("welcome = %q, want EngineStatus", welcome)
	}
	if _, err := io.WriteString(conn, `{"jsonrpc":"2.0","method":"ChangeGroup.AddControl","params":{"Id":"sub1","Controls":["Mixer1.gain"]},"id":3}`+"\x00"); err != nil {
		t.Fatalf("subscribing: %v", err)
	}
	if ack := readFrame(); !strings.Contains(ack, `"id":3`) {
		t.Fatalf("subscribe ack = %q", ack)
	}

	// HTTP: the write.
	req, _ := http.NewRequest(http.MethodPost, "http://"+at(80)+"/api/channels/gain",
		bytes.NewBufferString(`{"index": 0, "gain": -17}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Polaris-Token", "polaris-lab-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP write: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP write status = %d", resp.StatusCode)
	}

	// Line protocol: the push carries the HTTP write.
	for {
		frame := readFrame()
		if strings.Contains(frame, "ChangeGroup.Poll") {
			if !strings.Contains(frame, `"Value":-17`) {
				t.Fatalf("push = %q, want Value -17", frame)
			}
			break
		}
	}

	// SSH: the CLI renders the same state.
	out, code, err := sshExec(at(22), "admin", "polaris", "show channels")
	if err != nil || code != 0 {
		t.Fatalf("SSH exec: code=%d err=%v out=%q", code, err, out)
	}
	if !strings.Contains(out, "-17 dB") {
		t.Errorf("show channels = %q, want channel 0 at -17 dB", out)
	}

	// SNMP and HTTP read one serial from the same state.
	resp, err = authedGet("http://"+at(80)+"/api/status", "polaris-lab-token")
	if err != nil {
		t.Fatalf("HTTP status: %v", err)
	}
	var status struct {
		Device struct {
			Serial string `json:"serial"`
		} `json:"device"`
	}
	err = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if err != nil || status.Device.Serial == "" {
		t.Fatalf("decoding status serial: %v (%q)", err, status.Device.Serial)
	}
	g := &gosnmp.GoSNMP{
		Target: ip, Port: uint16(161 + acceptancePortShift), Community: "public",
		Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1,
	}
	if err := g.Connect(); err != nil {
		t.Fatalf("SNMP connect: %v", err)
	}
	defer g.Conn.Close()
	pkt, err := g.Get([]string{"1.3.6.1.4.1.55540.5.2.0"})
	if err != nil {
		t.Fatalf("SNMP GET: %v", err)
	}
	if got := fmt.Sprint(pkt.Variables[0].Value); got != status.Device.Serial && string(asBytes(pkt.Variables[0].Value)) != status.Device.Serial {
		t.Errorf("SNMP serial = %v, HTTP serial = %q", pkt.Variables[0].Value, status.Device.Serial)
	}

	// SNMP reads live state too: a display powered off over HTTP reads off
	// over SNMP.
	display := deviceByProfile(t, e, "boreal-edge-86")
	dip := display.LoopbackIP.String()
	preq, _ := http.NewRequest(http.MethodPost,
		"http://"+net.JoinHostPort(dip, fmt.Sprint(80+acceptancePortShift))+"/api/display/power",
		bytes.NewBufferString(`{"power": "off"}`))
	preq.Header.Set("Content-Type", "application/json")
	preq.SetBasicAuth("admin", "boreal")
	presp, err := http.DefaultClient.Do(preq)
	if err != nil {
		t.Fatalf("display power write: %v", err)
	}
	presp.Body.Close()
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("display power write status = %d", presp.StatusCode)
	}
	dg := &gosnmp.GoSNMP{
		Target: dip, Port: uint16(161 + acceptancePortShift), Community: "public",
		Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1,
	}
	if err := dg.Connect(); err != nil {
		t.Fatalf("display SNMP connect: %v", err)
	}
	defer dg.Conn.Close()
	ppkt, err := dg.Get([]string{"1.3.6.1.4.1.55520.1.1.1.0"})
	if err != nil {
		t.Fatalf("display SNMP GET: %v", err)
	}
	if got := string(asBytes(ppkt.Variables[0].Value)); got != "off" {
		t.Errorf("SNMP power = %v after an HTTP power-off, want off", ppkt.Variables[0].Value)
	}

	// Every request-response hit was reported for the device, and session
	// events carry the bound port, as the hits do.
	hitsMu.Lock()
	defer hitsMu.Unlock()
	if len(sessionPorts) == 0 {
		t.Error("no session event for the line-protocol session")
	}
	for _, p := range sessionPorts {
		if p != 1710+acceptancePortShift {
			t.Errorf("session event port = %d, want the bound %d", p, 1710+acceptancePortShift)
		}
	}
	for _, p := range []string{"http", "ssh", "snmp"} {
		if !hits[p] {
			t.Errorf("no %s hit reported for %s; saw %v", p, dsp.Hostname, hits)
		}
	}
}

func authedGet(url, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Polaris-Token", token)
	return http.DefaultClient.Do(req)
}

func asBytes(v any) []byte {
	b, _ := v.([]byte)
	return b
}
