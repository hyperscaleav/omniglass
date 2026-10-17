package sim

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

const syncPortShift = 21000

func newCatalogEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("loading catalog: %v", err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(func(p int) int { return p + syncPortShift })
	t.Cleanup(e.StopAll)
	return e
}

func dspSnapshot(id, ip string) DeviceSnapshot {
	return DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: ip, ProfileName: "polaris-dsp-16", Version: 1}
}

func dspURL(ip, path string) string {
	return fmt.Sprintf("http://%s%s", net.JoinHostPort(ip, fmt.Sprint(80+syncPortShift)), path)
}

func dspDo(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("X-Polaris-Token", "polaris-lab-token")
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// Sync is how a running simulator follows the server: devices added start
// serving, devices removed stop, and a device present on both sides keeps its
// live state rather than resetting to defaults.
func TestEngineSyncAddsAndRemovesDevicesKeepingState(t *testing.T) {
	e := newCatalogEngine(t)
	a, b := "127.88.0.1", "127.88.0.2"

	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp-a", a)}); err != nil {
		t.Fatalf("sync [a]: %v", err)
	}
	if code, body := dspDo(t, http.MethodPost, dspURL(a, "/api/channels/gain"), `{"index": 0, "gain": -21}`); code != http.StatusOK {
		t.Fatalf("write to a: %d %s", code, body)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(b, "/api/status"), ""); code != 0 {
		t.Fatalf("b answered before it was synced in: %d", code)
	}

	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp-a", a), dspSnapshot("dsp-b", b)}); err != nil {
		t.Fatalf("sync [a b]: %v", err)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(b, "/api/status"), ""); code != http.StatusOK {
		t.Fatalf("b after sync: %d", code)
	}
	if _, body := dspDo(t, http.MethodGet, dspURL(a, "/api/channels"), ""); !strings.Contains(body, "-21") {
		t.Fatalf("a lost its state across a sync: %s", body)
	}

	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp-b", b)}); err != nil {
		t.Fatalf("sync [b]: %v", err)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(a, "/api/status"), ""); code != 0 {
		t.Fatalf("a still answers after removal: %d", code)
	}
	if n := len(e.Devices()); n != 1 {
		t.Fatalf("engine holds %d devices, want 1", n)
	}
}

// A device whose address moves is restarted on the new address.
func TestEngineSyncRestartsAMovedDevice(t *testing.T) {
	e := newCatalogEngine(t)
	from, to := "127.88.1.1", "127.88.1.2"
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp", from)}); err != nil {
		t.Fatal(err)
	}
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp", to)}); err != nil {
		t.Fatal(err)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(to, "/api/status"), ""); code != http.StatusOK {
		t.Fatalf("new address: %d", code)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(from, "/api/status"), ""); code != 0 {
		t.Fatalf("old address still answers: %d", code)
	}
}

// One listener that cannot bind does not keep the rest of the fleet dark; the
// error names what failed.
func TestEngineSyncServesPastAFailedListener(t *testing.T) {
	e := newCatalogEngine(t)
	a, b := "127.88.2.1", "127.88.2.2"
	squat, err := net.Listen("tcp", net.JoinHostPort(a, fmt.Sprint(80+syncPortShift)))
	if err != nil {
		t.Fatal(err)
	}
	defer squat.Close()

	err = e.Sync([]DeviceSnapshot{dspSnapshot("dsp-a", a), dspSnapshot("dsp-b", b)})
	if err == nil || !strings.Contains(err.Error(), "dsp-a") {
		t.Fatalf("sync error = %v, want one naming dsp-a", err)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(b, "/api/status"), ""); code != http.StatusOK {
		t.Fatalf("b dark because a failed: %d", code)
	}
}

// Every protocol's hits arrive on one stream, for devices added before or
// after the reporter was set.
func TestEngineReportsHitsOnOneStream(t *testing.T) {
	e := newCatalogEngine(t)
	var mu sync.Mutex
	var hits []Hit
	e.SetHitReporter(func(h Hit) {
		mu.Lock()
		hits = append(hits, h)
		mu.Unlock()
	})
	ip := "127.88.3.1"
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp", ip)}); err != nil {
		t.Fatal(err)
	}
	dspDo(t, http.MethodGet, dspURL(ip, "/api/status"), "")
	g := &gosnmp.GoSNMP{Target: ip, Port: uint16(161 + syncPortShift), Community: "public",
		Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1}
	if err := g.Connect(); err != nil {
		t.Fatal(err)
	}
	defer g.Conn.Close()
	if _, err := g.Get([]string{"1.3.6.1.4.1.55540.5.2.0"}); err != nil {
		t.Fatalf("snmp get: %v", err)
	}
	out, code, err := sshExec(net.JoinHostPort(ip, fmt.Sprint(22+syncPortShift)), "admin", "polaris", "show dsp")
	if err != nil || code != 0 {
		t.Fatalf("ssh: %v %d %q", err, code, out)
	}

	mu.Lock()
	defer mu.Unlock()
	got := map[string]Hit{}
	for _, h := range hits {
		got[h.Protocol] = h
	}
	for _, p := range []string{"http", "snmp", "ssh"} {
		h, ok := got[p]
		if !ok {
			t.Errorf("no %s hit; got %v", p, hits)
			continue
		}
		if h.DeviceID != "dsp" || h.At.IsZero() {
			t.Errorf("%s hit = %+v, want device dsp and a time", p, h)
		}
	}
	if h := got["http"]; h.Path != "/api/status" || h.Status != http.StatusOK {
		t.Errorf("http hit = %+v", h)
	}
}

// A device the server lists carries no state; it starts from its profile's
// defaults with an identity derived from its id, so its serial is stable
// across restarts.
func TestEngineSyncDerivesStateForAListedDevice(t *testing.T) {
	serial := func(ip string) string {
		e := newCatalogEngine(t)
		if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp-stable", ip)}); err != nil {
			t.Fatal(err)
		}
		defer e.StopAll()
		_, body := dspDo(t, http.MethodGet, dspURL(ip, "/api/status"), "")
		i := strings.Index(body, `"serial": "`)
		if i < 0 {
			t.Fatalf("status has no serial: %s", body)
		}
		rest := body[i+len(`"serial": "`):]
		return rest[:strings.Index(rest, `"`)]
	}
	first, second := serial("127.88.4.1"), serial("127.88.4.2")
	if first == "" || first != second {
		t.Fatalf("serials %q and %q, want one non-empty serial derived from the id", first, second)
	}
}
