//go:build linux

package node_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// http polls and commands end to end (#902): Postgres, the bus, a real
// node, and the simulator's http faces on ephemeral ports. The shipped
// kestrel-api driver reads the simulated bar over https with basic auth and
// actuates it; the shipped http-json driver reads the simulated sensor with
// its header key; and a DSP's namespaced XML and its HTML status page are
// read by drivers authored here, behind a bearer token and a query key. A
// refused credential is sent once per connection fingerprint, and a changed
// https key is held beneath the gate until an operator trusts it.

// httpStack is one test's server, simulated components and node.
type httpStack struct {
	*gateStack
	dsn     string
	devices map[string]*sim.Device // by profile
	ips     map[string]string      // by profile
}

// newHTTPStack serves each profile pick names on a claimed loopback
// address; shape adds variants to the catalog first. The profiles are
// derived from the catalogs (pick), never named, so a catalog rename touches
// the YAML and nothing here (#804).
func newHTTPStack(t *testing.T, nodeName string, shape func(*profile.Store), pick func(*profile.Store) []string) *httpStack {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	if shape != nil {
		shape(store)
	}
	names := pick(store)
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	ips := map[string]string{}
	var snaps []sim.DeviceSnapshot
	for _, name := range names {
		ip := simtest.ClaimLoopback(t)
		ips[name] = ip
		snaps = append(snaps, sim.DeviceSnapshot{DeviceID: name, Hostname: name, LoopbackIP: ip, ProfileName: name, Version: 1})
	}
	if err := engine.Sync(snaps); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: nodeName}, all, all); err != nil {
		t.Fatal(err)
	}
	token := "enroll-token-" + nodeName
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", nodeName, hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Shutdown)
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	t.Cleanup(apiSrv.Close)
	s := &httpStack{gateStack: &gateStack{t: t, ctx: ctx, gw: gw, all: all, apiURL: apiSrv.URL, token: token, panels: map[string]*sim.Device{}},
		dsn: dsn, devices: map[string]*sim.Device{}, ips: ips}
	for _, d := range engine.Devices() {
		s.devices[d.DeviceID] = d
	}
	t.Cleanup(func() {
		if s.stop != nil {
			s.stop()
		}
	})
	return s
}

// httpPort is the port a simulated component serves its one http face on.
func (s *httpStack) httpPort(name string) int {
	s.t.Helper()
	for _, f := range s.devices[name].Faces() {
		if f.Protocol == "http" {
			return int(f.Addr.Port())
		}
	}
	s.t.Fatalf("%s serves no http face: %+v", name, s.devices[name].Faces())
	return 0
}

func (s *httpStack) secret(name, typ string, fields map[string]string) *storage.Secret {
	s.t.Helper()
	sec, err := s.gw.CreateSecret(s.ctx, "", storage.SecretSpec{Name: name, SecretType: typ, OwnerKind: "platform", Fields: fields}, s.all, true)
	if err != nil {
		s.t.Fatal(err)
	}
	return sec
}

func (s *httpStack) attachHTTP(comp, drv, nodeName string, inputs map[string]string) *storage.Endpoint {
	s.t.Helper()
	ep, err := s.gw.CreateEndpoint(s.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &nodeName, Inputs: inputs,
		SecretRead: s.all, CanAdmin: true}, s.all)
	if err != nil {
		s.t.Fatalf("attach %s: %v", drv, err)
	}
	return ep
}

// driverProfile is the simulator profile serving a product the shipped
// driver reads, derived from both catalogs.
func driverProfile(t *testing.T, store *profile.Store, driver string) string {
	t.Helper()
	raw, err := seed.FactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var facts struct {
		Products []struct {
			ID       string `json:"id"`
			DriverID string `json:"driver_id"`
		} `json:"products"`
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range facts.Products {
		if p.DriverID != driver {
			continue
		}
		if name, _, ok := store.ForProduct(p.ID); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		t.Fatalf("no simulator profile serves a %s product", driver)
	}
	sort.Strings(names)
	return names[0]
}

// xmlProfile is the simulator profile whose http face serves its status as
// namespaced XML (GET /api/status.xml) beside an HTML dashboard.
func xmlProfile(t *testing.T, store *profile.Store) string {
	t.Helper()
	var names []string
	for name, d := range store.Devices {
		for _, iface := range d.Interfaces {
			for _, r := range iface.Routes {
				if iface.Protocol == "http" && r.Match == "GET /api/status.xml" {
					names = append(names, name)
				}
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("no simulator profile serves GET /api/status.xml")
	}
	sort.Strings(names)
	return names[0]
}

// commandStatus is the latest command of a type's recorded status.
func (s *httpStack) commandStatus(typ string) string {
	s.t.Helper()
	conn, err := pgx.Connect(s.ctx, s.dsn)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = conn.Close(s.ctx) }()
	var status string
	if err := conn.QueryRow(s.ctx, `select c.status from command c join command_type ct on ct.id = c.command_type_id
		where ct.name = $1 order by c.id desc limit 1`, typ).Scan(&status); err != nil {
		return ""
	}
	return status
}

// spkiOf renders a certificate's public key as the node pins it.
func spkiOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// TestNodeHTTPSKestrelPollsActuatesAndPinsItsKey: the shipped kestrel-api
// driver against the simulated bar's https face behind basic auth. The probe
// runs the handshake and pins the bar's key on first use; the polls land the
// bar's audio, call and system facts; a volume, a mute and a standby command
// actuate it (the standby settles from its reply); a replaced bar's key is
// refused as certificate, holds every request beneath the gate, and is
// trusted with :retry and certificate.
func TestNodeHTTPSKestrelPollsActuatesAndPinsItsKey(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var barProfile string
	s := newHTTPStack(t, "edge-https", nil, func(store *profile.Store) []string {
		barProfile = driverProfile(t, store, "kestrel-api")
		return []string{barProfile}
	})
	bar := s.devices[barProfile]
	s.component("bar-1")
	s.secret("bar-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	ep := s.attachHTTP("bar-1", "kestrel-api", "edge-https", map[string]string{
		"host": s.ips[barProfile], "port": fmt.Sprint(s.httpPort(barProfile)), "login": "bar-login"})
	if ep.Name != "https" {
		t.Fatalf("endpoint name %q, want https", ep.Name)
	}
	s.runNode("edge-https")

	s.waitFor("bar-1", ep.Name, "endpoint-responsive", is("up"), "up: the handshake answered")
	s.waitFor("bar-1", ep.Name, "tls-certificate", is(spkiOf(bar.HTTPSCertificate().RawSubjectPublicKeyInfo)), "the bar's key, trusted on first use")
	s.waitFor("bar-1", ep.Name, "endpoint-authenticated", is("yes"), "yes: the bar accepted the login")
	s.waitFor("bar-1", ep.Name, "model-number", is(fmt.Sprint(bar.ReadPath("configuration.device.model"))), "the bar's model, polled over https")
	s.waitFor("bar-1", ep.Name, "call-state", func(v string) bool { return v == "active" || v == "idle" }, "the call state, mapped")
	if v, ok := s.metric("bar-1", ep.Name, "volume"); !ok || fmt.Sprint(v) != fmt.Sprint(bar.ReadPath("status.audio.volume")) {
		t.Fatalf("volume = %v (%v), want the bar's %v", v, ok, bar.ReadPath("status.audio.volume"))
	}
	if ms, ok := s.metric("bar-1", ep.Name, "tls-handshake-time"); !ok || ms <= 0 {
		t.Fatalf("tls-handshake-time = %v (%v), want an observation", ms, ok)
	}
	if ms, ok := s.metric("bar-1", ep.Name, "http-response-time"); !ok || ms <= 0 {
		t.Fatalf("http-response-time = %v (%v), want an observation", ms, ok)
	}

	// The commands: a number, a text, and a standby that settles from the
	// bar's reply.
	owner := s.owner()
	for _, c := range []struct {
		typ, value, path, want string
	}{
		{"set-volume", `42`, "status.audio.volume", "42"},
		{"set-mute", `"true"`, "status.audio.mic_mute", "true"},
		{"set-power", `"standby"`, "status.device.power", "standby"},
	} {
		if _, err := s.gw.IssueCommand(s.ctx, owner, "component", "bar-1", c.typ, "", json.RawMessage(c.value), nil); err != nil {
			t.Fatalf("issue %s: %v", c.typ, err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for fmt.Sprint(bar.ReadPath(c.path)) != c.want && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
		}
		if got := fmt.Sprint(bar.ReadPath(c.path)); got != c.want {
			t.Fatalf("%s: the bar shows %s = %s, want %s", c.typ, c.path, got, c.want)
		}
	}
	s.waitFor("bar-1", ep.Name, "power-state", is("standby"), "standby, settled from the bar's reply")
	settled := func(typ string) bool { return s.commandStatus(typ) == "settled" }
	deadline := time.Now().Add(10 * time.Second)
	for !(settled("set-volume") && settled("set-mute") && settled("set-power")) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	for _, typ := range []string{"set-volume", "set-mute", "set-power"} {
		if got := s.commandStatus(typ); got != "settled" {
			t.Fatalf("%s = %q, want settled from its reply", typ, got)
		}
	}

	// The bar is replaced: a new key, refused at the transport rung, and
	// beneath the gate no request reaches it (the probe connects only).
	pinned := spkiOf(bar.HTTPSCertificate().RawSubjectPublicKeyInfo)
	if err := bar.RotateHTTPSKey(); err != nil {
		t.Fatal(err)
	}
	offered := spkiOf(bar.HTTPSCertificate().RawSubjectPublicKeyInfo)
	s.waitFor("bar-1", ep.Name, "endpoint-rejection", func(v string) bool {
		return strings.HasPrefix(v, "responsive certificate ") && strings.HasSuffix(v, pinned+" "+offered)
	}, "the certificate gate naming both keys")
	s.waitFor("bar-1", ep.Name, "endpoint-rejection-reason", func(v string) bool {
		return strings.HasPrefix(v, "certificate: the certificate's public key changed")
	}, "the certificate gate's reason, for the notice")
	requests := bar.HTTPRequests
	s.settles("requests beneath the certificate gate", requests, requests())
	// A restarted node honours the gate it adopts: still nothing.
	s.runNode("edge-https")
	s.settles("requests after a node restart", requests, requests())

	// The operator trusts the offered key: the gate lifts and the polls run.
	if _, err := s.gw.RetryEndpoint(s.ctx, "", ep.ID, storage.RetryPin{Certificate: offered}, s.all, s.all); err != nil {
		t.Fatalf("retry with the offered key: %v", err)
	}
	s.waitFor("bar-1", ep.Name, "endpoint-rejection", is("none"), "none, the key trusted")
	before := requests()
	deadline = time.Now().Add(20 * time.Second)
	for requests() == before && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if requests() == before {
		t.Fatal("no request reached the trusted key")
	}

	// verify, edited in place: no root vouches for the bar's self-signed
	// certificate, so it is refused as certificate, never a timeout; skip,
	// the operator's explicit choice, connects.
	for _, m := range []struct{ mode, want string }{{"verify", "responsive certificate "}, {"skip", "none"}} {
		if _, err := s.gw.UpdateEndpoint(s.ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"tls-mode": m.mode}, SecretRead: s.all, CanAdmin: true}, s.all, s.all); err != nil {
			t.Fatalf("edit tls-mode to %s: %v", m.mode, err)
		}
		s.waitFor("bar-1", ep.Name, "endpoint-rejection", func(v string) bool { return strings.HasPrefix(v, m.want) }, m.mode+": "+m.want)
	}
	s.waitFor("bar-1", ep.Name, "endpoint-responsive", is("up"), "up under skip")
}

// TestNodeHTTPAuthVariantsAndTheRefusedCredential: an api key in a header
// (the shipped http-json driver on the simulated sensor), a bearer token over
// namespaced XML, and an api key in a query parameter over an HTML status
// page (drivers authored here, on a DSP's faces). A wrong key is sent once
// per connection fingerprint, across ticks and a node restart, while the
// credential-free probe keeps reporting; the key rotated in the store brings
// the endpoint up within a tick.
func TestNodeHTTPAuthVariantsAndTheRefusedCredential(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	variant := func(store *profile.Store, name string, auth profile.Auth) {
		base := *store.Devices[xmlProfile(t, store)]
		base.Name = name
		base.Interfaces = append([]profile.Interface(nil), base.Interfaces...)
		for i := range base.Interfaces {
			if base.Interfaces[i].Protocol == "http" {
				base.Interfaces[i].Auth = auth
			}
		}
		store.Devices[name] = &base
	}
	var sensorProfile string
	s := newHTTPStack(t, "edge-http", func(store *profile.Store) {
		variant(store, "dsp-bearer", profile.Auth{Type: "bearer", APIKey: "dsp-token", Enforce: true})
		variant(store, "dsp-query", profile.Auth{Type: "query", Query: "key", APIKey: "dsp-key", Enforce: true})
	}, func(store *profile.Store) []string {
		sensorProfile = driverProfile(t, store, "http-json")
		return []string{sensorProfile, "dsp-bearer", "dsp-query"}
	})

	// The header key: the shipped http-json driver.
	s.component("sensor-1")
	census := s.secret("census-key", "api-key", map[string]string{"key": "wrong-key"})
	sensor := s.devices[sensorProfile]
	sep := s.attachHTTP("sensor-1", "http-json", "edge-http", map[string]string{
		"host": s.ips[sensorProfile], "port": fmt.Sprint(s.httpPort(sensorProfile)), "key": "census-key"})

	// A bearer token over namespaced XML, and a query key over HTML.
	s.secret("dsp-token", "bearer-token", map[string]string{"token": "dsp-token"})
	s.secret("dsp-key", "api-key", map[string]string{"key": "dsp-key"})
	for _, d := range []struct{ name, spec string }{
		{"dsp-xml", `{"version":1,"transport":"http",
			"inputs":[{"name":"host","kind":"string","required":true},{"name":"port","kind":"number","default":"80"},{"name":"token","kind":"secret","secret_type":"bearer-token","required":true}],
			"session":{"credential":"token","scheme":"bearer"},
			"polls":[{"name":"status","schedule":{"every":"30s"},"request":{"path":"/api/status.xml","format":"xml","namespaces":{"d":"urn:polaris:dsp:1","sys":"urn:polaris:system:1"}},
				"emits":[{"name":"firmware-version","extract":{"xpath":"/d:status/sys:device/sys:firmware"}},
					{"name":"model-number","extract":{"xpath":"/d:status/sys:device/@model"}}]}]}`},
		{"dsp-html", `{"version":1,"transport":"http",
			"inputs":[{"name":"host","kind":"string","required":true},{"name":"port","kind":"number","default":"80"},{"name":"key","kind":"secret","secret_type":"api-key","required":true}],
			"session":{"credential":"key","scheme":"api-key","api_key":{"in":"query","name":"key"}},
			"polls":[{"name":"page","schedule":{"every":"30s"},"request":{"path":"/","format":"html"},
				"emits":[{"name":"firmware-version","extract":{"xpath":"//div[@class='row'][span[@class='k']='firmware']/span[@class='v']"}},
					{"name":"serial-number","extract":{"regex":"<span class=\"k\">serial</span><span class=\"v\">([^<]+)</span>"}}]}]}`},
	} {
		if _, err := s.gw.CreateDriver(s.ctx, "", storage.Driver{Name: d.name, Label: d.name, Version: "1.0.0", Spec: []byte(d.spec)}); err != nil {
			t.Fatalf("author %s: %v", d.name, err)
		}
	}
	s.component("dsp-1")
	s.component("dsp-2")
	xep := s.attachHTTP("dsp-1", "dsp-xml", "edge-http", map[string]string{"host": s.ips["dsp-bearer"], "port": fmt.Sprint(s.httpPort("dsp-bearer")), "token": "dsp-token"})
	hep := s.attachHTTP("dsp-2", "dsp-html", "edge-http", map[string]string{"host": s.ips["dsp-query"], "port": fmt.Sprint(s.httpPort("dsp-query")), "key": "dsp-key"})
	s.runNode("edge-http")

	firmware := fmt.Sprint(s.devices["dsp-bearer"].ReadPath("configuration.firmware.version"))
	s.waitFor("dsp-1", xep.Name, "firmware-version", is(firmware), "the firmware read by namespaced XPath")
	s.waitFor("dsp-1", xep.Name, "model-number", is(fmt.Sprint(s.devices["dsp-bearer"].ReadPath("configuration.device.model"))), "the model, an attribute")
	s.waitFor("dsp-1", xep.Name, "endpoint-authenticated", is("yes"), "yes: the token accepted")
	s.waitFor("dsp-2", hep.Name, "firmware-version", is(firmware), "the firmware read off the status page")
	s.waitFor("dsp-2", hep.Name, "serial-number", is(fmt.Sprint(s.devices["dsp-query"].ReadPath("configuration.device.serial"))), "the serial, by regex")
	s.waitFor("dsp-2", hep.Name, "endpoint-authenticated", is("yes"), "yes: the query key accepted")

	// The wrong header key: refused once, held at the session rung.
	s.waitFor("sensor-1", sep.Name, "endpoint-authenticated", is("no"), "no: the sensor refused the key")
	s.waitFor("sensor-1", sep.Name, "endpoint-rejection", func(v string) bool { return strings.HasPrefix(v, "authenticated auth ") }, "the auth gate")
	s.waitFor("sensor-1", sep.Name, "endpoint-rejection-reason", is("auth: the component refused the credential (401)"), "the gate's reason, for the notice")
	s.waitFor("sensor-1", sep.Name, "endpoint-responsive", is("up"), "up: the credential-free probe still reports")
	refusals := sensor.HTTPRefusals
	s.settles("refused requests", refusals, 1)
	s.runNode("edge-http")
	s.settles("refused requests after a node restart", refusals, 1)

	// The key rotated in the store: the fingerprint moves, the gate lifts,
	// and the polls land within a tick.
	if _, err := s.gw.UpdateSecret(s.ctx, "", census.ID, storage.SecretPatch{Fields: map[string]string{"key": "census-lab-key"}}, s.all, s.all, true, true); err != nil {
		t.Fatal(err)
	}
	s.waitFor("sensor-1", sep.Name, "endpoint-authenticated", is("yes"), "yes after the rotation")
	s.waitFor("sensor-1", sep.Name, "occupancy-state", func(v string) bool { return v == "occupied" || v == "vacant" }, "the occupancy, polled")
	s.waitFor("sensor-1", sep.Name, "endpoint-rejection", is("none"), "none, the gate lifted")
	if got := refusals(); got != 1 {
		t.Fatalf("refused requests = %d after the rotation, want still 1", got)
	}
}
