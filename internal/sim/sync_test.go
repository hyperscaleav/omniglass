//go:build linux

package sim

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// newCatalogEngine serves the embedded catalog on ephemeral ports, so a
// test reads each face's address from the device and never collides with
// another run on the host (#1066).
func newCatalogEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("loading catalog: %v", err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	return e
}

// device returns the device the engine serves under id.
func device(t *testing.T, e *Engine, id string) *Device {
	t.Helper()
	for _, d := range e.Devices() {
		if d.DeviceID == id {
			return d
		}
	}
	t.Fatalf("the engine serves no %s", id)
	return nil
}

func dspSnapshot(id, ip string) DeviceSnapshot {
	return DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: ip, ProfileName: "polaris-dsp-16", Version: 1}
}

// dspURL is a URL on the DSP's HTTP face, at the address it bound.
func dspURL(t *testing.T, d *Device, path string) string {
	t.Helper()
	return "http://" + faceAddr(t, d, "http-api") + path
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
	devA := device(t, e, "dsp-a")
	if code, body := dspDo(t, http.MethodPost, dspURL(t, devA, "/api/channels/gain"), `{"index": 0, "gain": -21}`); code != http.StatusOK {
		t.Fatalf("write to a: %d %s", code, body)
	}
	if n := len(e.Devices()); n != 1 {
		t.Fatalf("b served before it was synced in: %d devices", n)
	}

	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp-a", a), dspSnapshot("dsp-b", b)}); err != nil {
		t.Fatalf("sync [a b]: %v", err)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(t, device(t, e, "dsp-b"), "/api/status"), ""); code != http.StatusOK {
		t.Fatalf("b after sync: %d", code)
	}
	if device(t, e, "dsp-a") != devA {
		t.Fatal("a was rebuilt by a sync that left it unchanged")
	}
	if _, body := dspDo(t, http.MethodGet, dspURL(t, devA, "/api/channels"), ""); !strings.Contains(body, "-21") {
		t.Fatalf("a lost its state across a sync: %s", body)
	}

	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp-b", b)}); err != nil {
		t.Fatalf("sync [b]: %v", err)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(t, devA, "/api/status"), ""); code != 0 {
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
	old := device(t, e, "dsp")
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp", to)}); err != nil {
		t.Fatal(err)
	}
	moved := device(t, e, "dsp")
	if got := faceOf(t, moved, "http-api").Addr.Addr().String(); got != to {
		t.Fatalf("the moved device serves on %s, want %s", got, to)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(t, moved, "/api/status"), ""); code != http.StatusOK {
		t.Fatalf("new address: %d", code)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(t, old, "/api/status"), ""); code != 0 {
		t.Fatalf("old address still answers: %d", code)
	}
}

// squatHTTP holds a free port on ip and pins the engine's declared HTTP port
// 80 to it, every other face ephemeral: a DSP on ip then cannot bind its
// HTTP face. The squatter is the test's own, so no other run collides while
// it holds the port; a test that releases it to rebind takes ip from
// simtest.ClaimLoopback, so no other run can take the port in between.
func squatHTTP(t *testing.T, e *Engine, ip string) net.Listener {
	t.Helper()
	squat, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { squat.Close() })
	held := boundPort(squat.Addr())
	e.SetPortMap(func(declared int) int {
		if declared == 80 {
			return held
		}
		return 0
	})
	return squat
}

// One listener that cannot bind does not keep the rest of the fleet dark; the
// error names what failed.
func TestEngineSyncServesPastAFailedListener(t *testing.T) {
	e := newCatalogEngine(t)
	a, b := "127.88.2.1", "127.88.2.2"
	squatHTTP(t, e, a)

	// b is a video bar, which declares no port 80, so the pin is a's alone.
	bar := DeviceSnapshot{DeviceID: "bar-b", Hostname: "bar-b", LoopbackIP: b, ProfileName: "kestrel-vroom", Version: 1}
	err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp-a", a), bar})
	if err == nil || !strings.Contains(err.Error(), "dsp-a") {
		t.Fatalf("sync error = %v, want one naming dsp-a", err)
	}
	c, err := net.DialTimeout("tcp", faceAddr(t, device(t, e, "bar-b"), "http-api"), time.Second)
	if err != nil {
		t.Fatalf("b dark because a failed: %v", err)
	}
	c.Close()
	for _, d := range e.Devices() {
		if d.DeviceID == "dsp-a" {
			t.Fatal("dsp-a held as served after it failed to start")
		}
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
	dsp := device(t, e, "dsp")
	dspDo(t, http.MethodGet, dspURL(t, dsp, "/api/status"), "")
	// A reachability probe connects and closes without a request; that is a
	// hit too.
	probe, err := net.Dial("tcp", faceAddr(t, dsp, "qrc"))
	if err != nil {
		t.Fatal(err)
	}
	probe.Close()
	g := &gosnmp.GoSNMP{Target: ip, Port: uint16(facePort(t, dsp, "snmp")), Community: "public",
		Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1}
	if err := g.Connect(); err != nil {
		t.Fatal(err)
	}
	defer g.Conn.Close()
	if _, err := g.Get([]string{"1.3.6.1.4.1.55540.5.2.0"}); err != nil {
		t.Fatalf("snmp get: %v", err)
	}
	out, code, err := sshExec(faceAddr(t, dsp, "ssh-console"), "admin", "polaris", "show dsp")
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
	connects := map[int]bool{}
	for _, h := range hits {
		if h.Protocol == "tcp" && h.Method == "CONNECT" {
			connects[h.Port] = true
		}
	}
	for _, name := range []string{"http-api", "qrc", "ssh-console"} {
		if p := facePort(t, dsp, name); !connects[p] {
			t.Errorf("no CONNECT hit on %s's port %d; got %v", name, p, connects)
		}
	}
	if h := got["snmp"]; h.Port != facePort(t, dsp, "snmp") {
		t.Errorf("snmp hit on port %d, want the bound %d", h.Port, facePort(t, dsp, "snmp"))
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
		_, body := dspDo(t, http.MethodGet, dspURL(t, device(t, e, "dsp-stable"), "/api/status"), "")
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

// A device with two line-protocol interfaces serves each with its own
// session config, and a stop closes both listeners.
func TestEngineServesEachTCPInterfaceSeparately(t *testing.T) {
	e := newCatalogEngine(t)
	ip := "127.88.5.1"
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "census", Hostname: "census", LoopbackIP: ip,
		ProfileName: "tessellate-census-c1", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	census := device(t, e, "census")
	var lines []Face
	for _, f := range census.Faces() {
		if f.Protocol == "tcp" {
			lines = append(lines, f)
		}
	}
	if len(lines) != 2 || lines[0].Declared != 5000 || lines[1].Declared != 5001 || lines[0].Addr == lines[1].Addr {
		t.Fatalf("census line faces = %+v, want 5000 and 5001 each on an address of its own", lines)
	}
	c, err := net.DialTimeout("tcp", lines[0].Addr.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := c.Read(buf)
	c.Close()
	if !strings.Contains(string(buf[:n]), "FEED READY") {
		t.Fatalf("port 5000 greeted %q, want the ASCII band's welcome", buf[:n])
	}
	// The hex band answers its own poll on its own face, though both faces
	// asked the port map for the same port 0.
	hex, err := net.DialTimeout("tcp", lines[1].Addr.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer hex.Close()
	_ = hex.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := hex.Write([]byte("\x02\xAA\x01\x03\r")); err != nil {
		t.Fatal(err)
	}
	n, _ = hex.Read(buf)
	if got := string(buf[:n]); !strings.HasPrefix(got, "\x02\xAA\x81") {
		t.Fatalf("port 5001 answered the hex poll with %q, want the hex band's AA 81 reply", got)
	}

	e.StopAll()
	for _, f := range lines {
		if c, err := net.DialTimeout("tcp", f.Addr.String(), 300*time.Millisecond); err == nil {
			c.Close()
			t.Errorf("the %d face (%s) still accepts after StopAll", f.Declared, f.Addr)
		}
	}
}

// pinPorts fixes the engine's port map to the ports d's faces bound, so
// every device after binds those same ports: a test of who holds an address
// gets the contention of fixed ports. The kernel handed those ports out on
// d's address only, so a test binds them only on addresses it claimed
// (simtest.ClaimLoopback), where no other run binds anything.
func pinPorts(e *Engine, d *Device) {
	held := map[int]int{}
	for _, f := range d.Faces() {
		held[f.Declared] = int(f.Addr.Port())
	}
	e.SetPortMap(func(declared int) int { return held[declared] })
}

// A device that takes over an address another device just left serves on
// it, on the same ports: removals stop before additions start.
func TestEngineSyncHandsAnAddressToANewDevice(t *testing.T) {
	e := newCatalogEngine(t)
	ip := simtest.ClaimLoopback(t)
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("old", ip)}); err != nil {
		t.Fatal(err)
	}
	old := device(t, e, "old")
	pinPorts(e, old)
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("new", ip)}); err != nil {
		t.Fatalf("handover: %v", err)
	}
	handed := device(t, e, "new")
	if got, want := faceAddr(t, handed, "http-api"), faceAddr(t, old, "http-api"); got != want {
		t.Fatalf("the new device serves on %s, want the %s the old one left", got, want)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(t, handed, "/api/status"), ""); code != http.StatusOK {
		t.Fatalf("new device on a freed address: %d", code)
	}
	// Two devices swapping addresses in one sync both serve, each taking the
	// ports the other just left.
	a, b := simtest.ClaimLoopback(t), simtest.ClaimLoopback(t)
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("x", a), dspSnapshot("y", b)}); err != nil {
		t.Fatal(err)
	}
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("x", b), dspSnapshot("y", a)}); err != nil {
		t.Fatalf("swap: %v", err)
	}
	for id, ip := range map[string]string{"x": b, "y": a} {
		d := device(t, e, id)
		if got := faceOf(t, d, "http-api").Addr.Addr().String(); got != ip {
			t.Fatalf("after the swap %s serves on %s, want %s", id, got, ip)
		}
		if code, _ := dspDo(t, http.MethodGet, dspURL(t, d, "/api/status"), ""); code != http.StatusOK {
			t.Fatalf("after the swap %s answers %d", ip, code)
		}
	}
}

// A device that failed to start is not counted as served, and the next
// sync tries it again.
func TestEngineSyncRetriesADeviceThatFailedToStart(t *testing.T) {
	e := newCatalogEngine(t)
	ip := simtest.ClaimLoopback(t)
	squat := squatHTTP(t, e, ip)
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp", ip)}); err == nil {
		t.Fatal("sync with a squatted port reported no error")
	}
	if n := len(e.Devices()); n != 0 {
		t.Fatalf("a device that failed to start is held as served (%d devices)", n)
	}
	squat.Close()
	if err := e.Sync([]DeviceSnapshot{dspSnapshot("dsp", ip)}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	dsp := device(t, e, "dsp")
	if got, want := faceAddr(t, dsp, "http-api"), squat.Addr().String(); got != want {
		t.Fatalf("the retried device serves http on %s, want the freed %s", got, want)
	}
	if code, _ := dspDo(t, http.MethodGet, dspURL(t, dsp, "/api/status"), ""); code != http.StatusOK {
		t.Fatalf("retried device: %d", code)
	}
}

// A product served by its family's profile starts with its own identity.
func TestEngineSyncAppliesAProductsOverlay(t *testing.T) {
	e := newCatalogEngine(t)
	ip := "127.88.8.1"
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: ip,
		ProfileName: "boreal-edge-86", Product: "boreal-edge-55", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	model := e.Devices()[0].State()["configuration"].(map[string]any)["device"].(map[string]any)["model"]
	if model != "Edge 55" {
		t.Fatalf("model = %v, want the product's own", model)
	}
}

// The run mode's port shift lifts only the ports that need a privilege:
// ports at or above 1024 stay as the profile declares them.
func TestPrivilegedPortShift(t *testing.T) {
	shift := PrivilegedPortShift(10000)
	for in, want := range map[int]int{22: 10022, 161: 10161, 1023: 11023, 1024: 1024, 1710: 1710, 51325: 51325} {
		if got := shift(in); got != want {
			t.Errorf("shift(%d) = %d, want %d", in, got, want)
		}
	}
	if PrivilegedPortShift(0) != nil {
		t.Error("a zero shift should leave the declared ports (nil map)")
	}
}

// A device whose product changes within its family (one size of display to
// another) is rebuilt with the new product's identity.
func TestEngineSyncRebuildsADeviceWhoseProductChanged(t *testing.T) {
	e := newCatalogEngine(t)
	ip := "127.88.8.2"
	snap := func(product string) DeviceSnapshot {
		return DeviceSnapshot{DeviceID: "panel", Hostname: "panel", LoopbackIP: ip, ProfileName: "boreal-edge-86", Product: product, Version: 1}
	}
	if err := e.Sync([]DeviceSnapshot{snap("boreal-edge-55")}); err != nil {
		t.Fatal(err)
	}
	if err := e.Sync([]DeviceSnapshot{snap("boreal-edge-65")}); err != nil {
		t.Fatal(err)
	}
	model := e.Devices()[0].State()["configuration"].(map[string]any)["device"].(map[string]any)["model"]
	if model != "Edge 65" {
		t.Fatalf("model after the product changed = %v, want Edge 65", model)
	}
}

// A stopped HTTP listener has released its port by the time StopHTTP
// returns, even when the device is stopped before its serve goroutine has
// run: a rebuild binds the same address straight after (#946). One
// processor holds the serve goroutine off so the stop always lands first.
func TestStopHTTPReleasesThePortBeforeItReturns(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	ip := "127.88.8.3"
	port := 0 // the first round takes a port from the kernel; the rest rebind it
	for i := range 50 {
		dev := NewDevice("panel", "panel", net.ParseIP(ip), "p", 1, map[string]any{})
		if err := dev.StartHTTP(port, nil); err != nil {
			t.Fatalf("round %d: start: %v", i, err)
		}
		port = boundPort(dev.HTTPAddrs()[0])
		dev.StopHTTP()
		lis, err := net.Listen("tcp", net.JoinHostPort(ip, fmt.Sprint(port)))
		if err != nil {
			t.Fatalf("round %d: the stopped listener still holds the port: %v", i, err)
		}
		lis.Close()
	}
}
