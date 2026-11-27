package sim

import (
	"bufio"
	"bytes"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/surface/nvp"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
)

// The Go surface helpers (sim/surface, #2831) are held to the YAML they
// replace by one method: the same protocol server serves the profile's YAML
// and a minimal Go declaration, and the bytes each puts on the wire are
// compared, for every command, OID or document the surface defines. The YAML
// is the catalog's own, read from profiles.FS, so a change to either side
// fails here.

func parityStore(t *testing.T) *profile.Store {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("loading catalog: %v", err)
	}
	return store
}

func ifaceNamed(t *testing.T, store *profile.Store, prof, name string) profile.Interface {
	t.Helper()
	p, ok := store.Devices[prof]
	if !ok {
		t.Fatalf("no profile %s", prof)
	}
	for _, i := range p.Interfaces {
		if i.Name == name {
			return i
		}
	}
	t.Fatalf("%s has no interface %s", prof, name)
	return profile.Interface{}
}

// ---- NVP ----

// roomState is the Go family's typed state behind the NVP surface.
type roomState struct {
	mu           sync.Mutex
	input, scene string
}

func (r *roomState) room() nvp.Room {
	return nvp.Room{
		Inputs: nvp.DefaultInputs,
		Scenes: nvp.DefaultScenes,
		Input: nvp.Field[string]{
			Get: func() string { r.mu.Lock(); defer r.mu.Unlock(); return r.input },
			Set: func(v string) { r.mu.Lock(); r.input = v; r.mu.Unlock() },
		},
		Scene: nvp.Field[string]{
			Get: func() string { r.mu.Lock(); defer r.mu.Unlock(); return r.scene },
			Set: func(v string) { r.mu.Lock(); r.scene = v; r.mu.Unlock() },
		},
	}
}

func nvpState() map[string]any {
	return map[string]any{"status": map[string]any{
		"video":       map[string]any{"input": "hdmi1"},
		"room":        map[string]any{"scene": "idle"},
		"diagnostics": map[string]any{"log": ""},
	}}
}

// serveNVP starts the interface's raw TCP face on the device and returns its
// address.
func serveNVP(t *testing.T, dev *Device, iface profile.Interface) string {
	t.Helper()
	cfg, err := CompileSessionConfig(&iface)
	if err != nil {
		t.Fatal(err)
	}
	dev.AttachTCPSession(NewSessionRegistry(dev, cfg, nil))
	if err := dev.StartTCPSession(0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopTCPSession)
	return dev.TCPSessionAddrs()[0].String()
}

// nvpPair is the YAML-backed panel (the catalog's own nvp face, the
// nvp-room surface expanded into it) and a Go-declared one, with the Go
// state behind the second.
func nvpPair(t *testing.T) (yamlDev *Device, yamlAddr string, goDev *Device, goAddr string, matches []string) {
	t.Helper()
	iface := ifaceNamed(t, parityStore(t), "newtron-panel-7", "nvp")

	yamlDev = NewDevice("yaml", "yaml", net.ParseIP("127.0.0.1"), "newtron-panel-7", 1, nvpState())
	for i := range iface.Commands {
		cmd := &iface.Commands[i]
		matches = append(matches, cmd.Match)
		if err := yamlDev.AddCommandFromProfile(cmd, cmd.Response); err != nil {
			t.Fatal(err)
		}
	}
	yamlAddr = serveNVP(t, yamlDev, iface)

	goIface := iface
	goIface.Commands = nil
	goIface.Subjects = nil
	for _, s := range nvp.Subjects() {
		goIface.Subjects = append(goIface.Subjects, profile.Subject{
			Match: s.Match, Path: s.Path, PublishToken: s.Token, Push: s.Push})
	}
	st := &roomState{input: "hdmi1", scene: "idle"}
	goDev = NewDevice("go", "go", net.ParseIP("127.0.0.1"), "newtron-panel-7", 1, nvpState())
	goDev.AttachCommands(st.room())
	goAddr = serveNVP(t, goDev, goIface)
	return yamlDev, yamlAddr, goDev, goAddr, matches
}

// wire is one client of a line protocol, recording every byte it reads.
type wire struct {
	t    *testing.T
	conn net.Conn
	rd   *bufio.Reader
	log  bytes.Buffer
}

func dialWire(t *testing.T, addr string) *wire {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &wire{t: t, conn: c, rd: bufio.NewReader(c)}
}

// send writes one CRLF line.
func (w *wire) send(line string) {
	w.t.Helper()
	if _, err := w.conn.Write([]byte(line + "\r\n")); err != nil {
		w.t.Fatal(err)
	}
}

// read takes n lines, raw, into the record.
func (w *wire) read(n int) {
	w.t.Helper()
	for i := 0; i < n; i++ {
		_ = w.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := w.rd.ReadBytes('\n')
		if err != nil {
			w.t.Fatalf("reading line %d of %d (so far %q): %v", i+1, n, w.log.String(), err)
		}
		w.log.Write(line)
	}
}

// ask sends a line and records the one line it is answered with.
func (w *wire) ask(line string) { w.t.Helper(); w.send(line); w.read(1) }

// quiet records that nothing more arrives soon.
func (w *wire) quiet() {
	w.t.Helper()
	_ = w.conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	b, err := w.rd.ReadBytes('\n')
	if err == nil {
		w.t.Fatalf("expected silence, read %q", b)
	}
	w.log.WriteString("<quiet>")
}

// nvpTranscript drives every command the surface defines, the lines it must
// refuse, and the pushes its subjects send, and returns what the wire carried.
func nvpTranscript(t *testing.T, dev *Device, addr string, commands []string) string {
	t.Helper()
	a := dialWire(t, addr)
	for _, c := range commands {
		a.log.WriteString("> " + c + "\n")
		a.ask(c)
	}
	// Lines that are none of the surface's: each is refused the same way.
	for _, bad := range []string{"SET INPUT bogus", "SET INPUT", "SET SCENE", "GET  INPUT", "get input", "UNSUBSCRIBE LOG", "UNSUBSCRIBE SCENE", "NOPE"} {
		a.log.WriteString("> " + bad + "\n")
		a.ask(bad)
	}
	// Subscribed, the session hears each subject's change as its line, after
	// the reply of a command it sent itself.
	for _, s := range []string{"SUBSCRIBE INPUT", "SUBSCRIBE LOG", "SUBSCRIBE SCENE"} {
		a.ask(s)
	}
	a.send("SET INPUT dp1")
	a.read(2)
	a.send("SET SCENE vc")
	a.read(2)
	b := dialWire(t, addr)
	b.ask("SET INPUT usbc")
	a.read(1)
	b.ask("SET SCENE presentation")
	a.read(1)
	if err := dev.applyExternalDiffs(map[string]any{"status.diagnostics.log": "WARN backlight driver reset"}, dev.Version+1); err != nil {
		t.Fatal(err)
	}
	a.read(1)
	a.ask("UNSUBSCRIBE INPUT")
	b.ask("SET INPUT hdmi2")
	a.quiet()
	return a.log.String() + "\n--\n" + b.log.String()
}

func TestParityNVPRoomSurface(t *testing.T) {
	yamlDev, yamlAddr, goDev, goAddr, yamlMatches := nvpPair(t)

	// The Go surface defines exactly the commands the YAML surface does, in
	// its order.
	st := &roomState{}
	if got := st.room().Commands(); !equalStrings(got, yamlMatches) {
		t.Fatalf("commands differ\n go:   %q\n yaml: %q", got, yamlMatches)
	}
	if got := goDev.CommandList(); !equalStrings(got, yamlDev.CommandList()) {
		t.Fatalf("help lists differ\n go:   %q\n yaml: %q", got, yamlDev.CommandList())
	}

	want := nvpTranscript(t, yamlDev, yamlAddr, yamlMatches)
	got := nvpTranscript(t, goDev, goAddr, yamlMatches)
	if got != want {
		t.Fatalf("wire bytes differ\n go:\n%q\n yaml:\n%q", got, want)
	}
	for _, must := range []string{"INPUT hdmi1\r\n", "SCENE idle\r\n", "OK\r\n", "EVT INPUT usbc\r\n", "EVT SCENE presentation\r\n", "LOG WARN backlight driver reset\r\n", "-ERR unknown command: NOPE\r\n"} {
		if !strings.Contains(got, must) {
			t.Errorf("transcript lacks %q: %q", must, got)
		}
	}
}

// The Go declaration's subscriptions and the YAML's are the same set: the
// panel's SSH, TLS and Telnet faces each declare the input subject alone.
func TestParityNVPInputSubject(t *testing.T) {
	iface := ifaceNamed(t, parityStore(t), "newtron-panel-7", "nvp-tls")
	if len(iface.Subjects) != 1 {
		t.Fatalf("nvp-tls declares %d subjects", len(iface.Subjects))
	}
	y, g := iface.Subjects[0], nvp.InputSubject()
	if y.Match != g.Match || y.Path != g.Path || y.PublishToken != g.Token {
		t.Fatalf("subject differs: yaml %+v, go %+v", y, g)
	}
	for _, v := range []any{"hdmi2", "", 7, nil} {
		cfg := &SessionConfig{LineEnding: "\r\n"}
		ys, err := compileSessionSubject(&y)
		if err != nil {
			t.Fatal(err)
		}
		sess := &Session{registry: &SessionRegistry{device: NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 1, nil), cfg: cfg}}
		yb, err := sess.renderPush(&Subscription{subject: y.Match, path: y.Path, token: y.PublishToken, def: ys}, v)
		if err != nil {
			t.Fatal(err)
		}
		gs, err := compileSessionSubject(&profile.Subject{Match: g.Match, Path: g.Path, PublishToken: g.Token, Push: g.Push})
		if err != nil {
			t.Fatal(err)
		}
		gb, err := sess.renderPush(&Subscription{subject: g.Match, path: g.Path, token: g.Token, def: gs}, v)
		if err != nil {
			t.Fatal(err)
		}
		if string(yb) != string(gb) {
			t.Errorf("push of %#v: go %q, yaml %q", v, gb, yb)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- SNMP ----

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// snmpScalars is the covered paths of an oid_map: the system group and the
// vendor identity arc.
var snmpCovered = map[string]bool{
	"configuration.device.model":     true,
	"status.device.uptime":           true,
	"configuration.firmware.version": true,
	"configuration.device.serial":    true,
}

// snmpExchange sends one request datagram and returns the raw reply.
func snmpExchange(t *testing.T, addr string, pdu gosnmp.PDUType, name string) []byte {
	t.Helper()
	req := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c, Community: "public", PDUType: pdu, RequestID: 4242,
		Variables: []gosnmp.SnmpPDU{{Name: "." + name, Type: gosnmp.Null}},
	}
	raw, err := req.MarshalMsg()
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write(raw); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 65535)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("snmp %v %s: %v", pdu, name, err)
	}
	return buf[:n]
}

func decodeSNMP(t *testing.T, raw []byte) *gosnmp.SnmpPacket {
	t.Helper()
	pkt, err := (&gosnmp.GoSNMP{}).SnmpDecodePacket(append([]byte(nil), raw...))
	if err != nil {
		t.Fatal(err)
	}
	return pkt
}

// walkBytes is a GETNEXT walk of the whole tree from its root: the raw reply
// at each step, and the names it visited.
func walkBytes(t *testing.T, addr string) (replies [][]byte, names []string) {
	t.Helper()
	cur := "1.3.6.1"
	for i := 0; i < 64; i++ {
		raw := snmpExchange(t, addr, gosnmp.GetNextRequest, cur)
		replies = append(replies, raw)
		v := decodeSNMP(t, raw).Variables
		if len(v) != 1 || v[0].Type == gosnmp.EndOfMibView || v[0].Type == gosnmp.NoSuchObject {
			return replies, names
		}
		cur = strings.TrimPrefix(v[0].Name, ".")
		names = append(names, cur)
	}
	t.Fatal("walk did not end")
	return nil, nil
}

func TestParitySNMPSystemAndIdentity(t *testing.T) {
	store := parityStore(t)
	names := make([]string, 0, len(store.Devices))
	for n := range store.Devices {
		names = append(names, n)
	}
	sort.Strings(names)

	identities := 0
	for _, name := range names {
		var iface *profile.Interface
		for i := range store.Devices[name].Interfaces {
			if store.Devices[name].Interfaces[i].Protocol == "snmp" {
				iface = &store.Devices[name].Interfaces[i]
			}
		}
		if iface == nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			// What the profile maps of the covered scalars.
			covered := map[string]string{} // state path to oid
			for path, oid := range iface.OIDMap {
				if snmpCovered[path] {
					covered[path] = oid
				}
			}
			if covered["configuration.device.model"] != snmp.OIDSysDescr || covered["status.device.uptime"] != snmp.OIDSysUpTime {
				t.Fatalf("%s does not map the system group: %v", name, covered)
			}
			fw, serial := covered["configuration.firmware.version"], covered["configuration.device.serial"]
			hasIdentity := fw != "" || serial != ""

			clock := &fakeClock{t: time.Unix(1_800_000_000, 0)}
			boot := clock.Now().Unix()
			state := map[string]any{
				"configuration": map[string]any{
					"device":   map[string]any{"model": "Test Model 9", "serial": "SN-0042"},
					"firmware": map[string]any{"version": "3.1.4"},
				},
				"status": map[string]any{"device": map[string]any{"boot_epoch": 0}},
			}

			// The YAML-backed agent: the profile's own oid_map, trimmed to the
			// scalars the helpers cover, over an elapsed uptime.
			yamlDev := NewDevice("y", "y", net.ParseIP("127.0.0.1"), name, 1, deepCopy(state))
			yamlDev.SetClock(clock.Now)
			yamlDev.OIDMap = map[string]string{}
			for path, oid := range covered {
				yamlDev.OIDMap[oid] = path
			}
			if err := yamlDev.AddComputedFromProfile(&profile.Computed{Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch"}); err != nil {
				t.Fatal(err)
			}

			// The Go-declared agent: the same scalars from typed state.
			goDev := NewDevice("g", "g", net.ParseIP("127.0.0.1"), name, 1, nil)
			goDev.SetClock(clock.Now)
			goDev.AttachOIDs(snmp.System{
				Descr:         func() string { return "Test Model 9" },
				UptimeSeconds: func() int64 { return clock.Now().Unix() - boot },
			}.OIDs()...)
			if hasIdentity {
				identities++
				id := snmp.Identity{}
				arc := fw
				if fw != "" {
					if !strings.HasSuffix(fw, ".5.1.0") {
						t.Fatalf("firmware is not at .5.1.0: %s", fw)
					}
					id.Firmware = func() string { return "3.1.4" }
				}
				if serial != "" {
					if !strings.HasSuffix(serial, ".5.2.0") {
						t.Fatalf("serial is not at .5.2.0: %s", serial)
					}
					id.Serial = func() string { return "SN-0042" }
					arc = strings.TrimSuffix(serial, ".2.0") + ".1.0"
				}
				ent, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(arc, "1.3.6.1.4.1."), ".5.1.0"))
				if err != nil {
					t.Fatalf("enterprise of %s: %v", arc, err)
				}
				id.Enterprise = ent
				goDev.AttachOIDs(id.OIDs()...)
			}

			ya, err := yamlDev.startSNMP(0, "public")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(yamlDev.StopSNMP)
			ga, err := goDev.startSNMP(0, "public")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(goDev.StopSNMP)
			time.Sleep(50 * time.Millisecond)

			for _, uptime := range []time.Duration{0, 4242 * time.Second} {
				clock.Advance(uptime)
				yReplies, yNames := walkBytes(t, ya.String())
				gReplies, gNames := walkBytes(t, ga.String())
				want := len(covered)
				if len(yNames) != want || strings.Join(yNames, " ") != strings.Join(gNames, " ") {
					t.Fatalf("walk visited go %v, yaml %v (want %d scalars)", gNames, yNames, want)
				}
				if len(yReplies) != len(gReplies) {
					t.Fatalf("walk length differs: go %d, yaml %d", len(gReplies), len(yReplies))
				}
				for i := range yReplies {
					if !bytes.Equal(yReplies[i], gReplies[i]) {
						t.Fatalf("GETNEXT step %d differs\n go:   %x\n yaml: %x", i, gReplies[i], yReplies[i])
					}
				}
				for _, oid := range yNames {
					yb := snmpExchange(t, ya.String(), gosnmp.GetRequest, oid)
					gb := snmpExchange(t, ga.String(), gosnmp.GetRequest, oid)
					if !bytes.Equal(yb, gb) {
						t.Fatalf("GET %s differs\n go:   %x\n yaml: %x", oid, gb, yb)
					}
				}
			}
			// An OID the surface does not define is refused as the YAML does.
			if y, g := snmpExchange(t, ya.String(), gosnmp.GetRequest, "1.3.6.1.2.1.1.2.0"), snmpExchange(t, ga.String(), gosnmp.GetRequest, "1.3.6.1.2.1.1.2.0"); !bytes.Equal(y, g) {
				t.Fatalf("an undefined OID differs\n go:   %x\n yaml: %x", g, y)
			}
		})
	}
	// Nine profiles repeated the identity arc before the Boreal displays and
	// projector became Go families (#2832); seven remain in YAML.
	if identities < 7 {
		t.Fatalf("only %d profiles map the identity arc; the helper's reason to exist is gone", identities)
	}
}

// ---- HTTP ----

func serveGET(t *testing.T, dev *Device, path string) (int, string, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	dev.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Header().Get("Content-Type"), rec.Body.Bytes()
}

type sortedMic struct {
	LevelDB int  `json:"level_db"`
	Mute    bool `json:"mute"`
}
type extender struct {
	Link   string `json:"link"`
	Signal bool   `json:"signal"`
}
type switcher struct {
	Input  string `json:"input"`
	Signal bool   `json:"signal"`
}
type dspBlock struct {
	CPULoad        int `json:"cpu_load"`
	ActiveChannels int `json:"active_channels"`
	Temperature    int `json:"temperature"`
	FanRPM         int `json:"fan_rpm"`
}
type outputBlock struct {
	Mute    bool `json:"mute"`
	LevelDB int  `json:"level_db"`
}
type ampBlock struct {
	Power         string `json:"power"`
	Protect       string `json:"protect"`
	SignalPresent bool   `json:"signal_present"`
	Temperature   int    `json:"temperature"`
	FanRPM        int    `json:"fan_rpm"`
}
type channel struct {
	Clip     bool `json:"clip"`
	OutputDB int  `json:"output_db"`
}
type occupancyBlock struct {
	LastEvent string `json:"last_event"`
	Vacant    bool   `json:"vacant"`
}
type webhookBlock struct {
	URL string `json:"url"`
}

func TestParityHTTPStatusDocument(t *testing.T) {
	ident := httpapi.Identity{Model: "Test Model 9", Serial: "SN-0042", Firmware: "3.1.4", Uptime: 4242}
	cfgState := map[string]any{
		"device":   map[string]any{"model": ident.Model, "serial": ident.Serial},
		"firmware": map[string]any{"version": ident.Firmware},
	}
	identState := map[string]any{"uptime": ident.Uptime}

	cases := []struct {
		profile string // the catalog template the YAML side serves
		order   httpapi.Order
		status  map[string]any // what the template reads beside the identity
		config  map[string]any
		blocks  []httpapi.Section
	}{
		{"lyra-mic", httpapi.ModelFirst, map[string]any{"mic": map[string]any{"mute": true, "level_db": -31}}, nil,
			[]httpapi.Section{httpapi.Inline("mic", sortedMic{LevelDB: -31, Mute: true})}},
		{"meridian-extender", httpapi.ModelFirst, map[string]any{"extender": map[string]any{"link": "up", "signal": true}}, nil,
			[]httpapi.Section{httpapi.Inline("extender", extender{Link: "up", Signal: true})}},
		{"meridian-switcher", httpapi.ModelFirst, map[string]any{"switcher": map[string]any{"input": "hdmi2", "signal": false}}, nil,
			[]httpapi.Section{httpapi.Inline("switcher", switcher{Input: "hdmi2"})}},
		{"polaris-dsp", httpapi.UptimeFirst, map[string]any{
			"dsp":    map[string]any{"cpu_load": 12, "active_channels": 8, "temperature": 41, "fan_rpm": 2100},
			"output": map[string]any{"mute": false, "level_db": -12}}, nil,
			[]httpapi.Section{
				httpapi.Block("dsp", dspBlock{CPULoad: 12, ActiveChannels: 8, Temperature: 41, FanRPM: 2100}),
				httpapi.Block("output", outputBlock{LevelDB: -12})}},
		{"polaris-amp", httpapi.UptimeFirst, map[string]any{
			"amp":      map[string]any{"power": "on", "protect": "ok", "signal_present": true, "temperature": 38, "fan_rpm": 1800},
			"channels": []any{map[string]any{"clip": false, "output_db": -6}, map[string]any{"clip": true, "output_db": 0}}}, nil,
			[]httpapi.Section{
				httpapi.Block("amp", ampBlock{Power: "on", Protect: "ok", SignalPresent: true, Temperature: 38, FanRPM: 1800}),
				httpapi.Inline("channels", []channel{{Clip: false, OutputDB: -6}, {Clip: true, OutputDB: 0}})}},
		{"tessellate-census", httpapi.UptimeFirst, map[string]any{
			"occupancy": map[string]any{"last_event": "entered", "vacant": false}},
			map[string]any{"webhook": map[string]any{"url": "https://hooks.example/x?a=1&b=<2>"}},
			[]httpapi.Section{
				httpapi.Block("occupancy", occupancyBlock{LastEvent: "entered"}),
				httpapi.Block("webhook", webhookBlock{URL: "https://hooks.example/x?a=1&b=<2>"})}},
	}
	orders := map[httpapi.Order]int{}
	for _, c := range cases {
		orders[c.order]++
		t.Run(c.profile, func(t *testing.T) {
			tmpl, err := fs.ReadFile(profiles.Templates(), "api/"+c.profile+"/status.json.gotempl")
			if err != nil {
				t.Fatal(err)
			}
			status := map[string]any{"device": identState}
			for k, v := range c.status {
				status[k] = v
			}
			cfg := map[string]any{}
			for k, v := range cfgState {
				cfg[k] = v
			}
			for k, v := range c.config {
				cfg[k] = v
			}
			yamlDev := NewDevice("y", "y", net.ParseIP("127.0.0.1"), c.profile, 1,
				map[string]any{"configuration": cfg, "status": status})
			if err := yamlDev.AddRoute("GET /api/status", "status.json.gotempl", string(tmpl), nil); err != nil {
				t.Fatal(err)
			}

			goDev := NewDevice("g", "g", net.ParseIP("127.0.0.1"), c.profile, 1, nil)
			goDev.AttachRoute(httpapi.StatusRoute(c.order, func() (httpapi.Identity, []httpapi.Section) { return ident, c.blocks }))

			ycode, ytype, ybody := serveGET(t, yamlDev, "/api/status")
			gcode, gtype, gbody := serveGET(t, goDev, "/api/status")
			if ycode != 200 {
				t.Fatalf("the YAML device answered %d: %s", ycode, ybody)
			}
			if gcode != ycode || gtype != ytype || !bytes.Equal(gbody, ybody) {
				t.Fatalf("status document differs\n go:   %d %s %q\n yaml: %d %s %q", gcode, gtype, gbody, ycode, ytype, ybody)
			}
		})
	}
	if orders[httpapi.ModelFirst] < 3 || orders[httpapi.UptimeFirst] < 3 {
		t.Fatalf("each key order needs three profiles behind it: %v", orders)
	}
}

// A Go route that leaves its status unset answers 200, as a handler that
// writes a body does; net/http panics on a zero status.
func TestGoRouteWithNoStatusAnswersOK(t *testing.T) {
	dev := NewDevice("g", "g", net.ParseIP("127.0.0.1"), "p", 1, nil)
	dev.AttachRoute(surface.Route{Method: "GET", Path: "/x", Serve: func() surface.Response {
		return surface.Response{Body: []byte("hi")}
	}})
	code, _, body := serveGET(t, dev, "/x")
	if code != 200 || string(body) != "hi" {
		t.Fatalf("got %d %q", code, body)
	}
}
