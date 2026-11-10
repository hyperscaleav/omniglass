//go:build linux

// The simulator serves on 127.x addresses beyond 127.0.0.1, which Linux
// treats as local without configuration.

package node_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestNodeHoldsASessionThroughADeviceOutage drives #816 end to end over the
// real wire: the shipped newtron-nvp driver attached to a component, the
// simulator's panel serving its line protocol, and the real node holding the
// session its standing listen task asks for. The session-state transitions
// land on the server as the component's property series (instance: the
// endpoint), the poll rides the held session, killing the panel's listener
// mid-session reads recovering and then up again once it returns, the
// reasons land in the node's own logs, and node shutdown tears the session
// down on the device's side.
func TestNodeHoldsASessionThroughADeviceOutage(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	// The device: the simulator's panel, its NVP listener on the port the
	// kernel handed it (unprivileged, and never another run's), which the
	// endpoint takes from the panel.
	panelIP := "127.89.16.1"
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	if err := engine.Sync([]sim.DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: panelIP,
		ProfileName: nvpProfile(t, store), Version: 1}}); err != nil {
		t.Fatalf("serve the panel: %v", err)
	}
	panel := engine.Devices()[0]
	nvpPort := panelPort(t, panel, "tcp")

	// The operator's configuration: a node, a component, the driver attach.
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-s"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "enroll-token-session"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-s", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	comp, drv, nodeName := "panel-1", "newtron-nvp", "edge-s"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &nodeName,
		Inputs:     map[string]string{"host": panelIP, "port": fmt.Sprint(nvpPort)},
		SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatalf("attach newtron-nvp: %v", err)
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()

	runCtx, stop := context.WithCancel(ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: apiSrv.URL, Name: "edge-s", Token: token, HeartbeatEvery: 200 * time.Millisecond})
		ran <- err
	}()
	stopped := false
	defer func() {
		if !stopped {
			stop()
			<-ran
		}
	}()

	latest := func(key string) string {
		v, err := gw.LatestProperty(ctx, comp, key, ep.Name)
		if err != nil || v == nil {
			return ""
		}
		return v.Value
	}
	waitValue := func(key, want string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if latest(key) == want {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("%s on %s[%s] = %q, want %q", key, comp, ep.Name, latest(key), want)
	}

	// The session comes up, and the poll lands its value over it.
	waitValue("session-state", "up")
	waitValue("video-input", "hdmi1")

	// Kill the panel's listener mid-session: recovering, then up again once
	// it returns.
	panel.StopTCPSession()
	waitValue("session-state", "recovering")
	if err := panel.StartTCPSession(nvpPort); err != nil {
		t.Fatalf("restart the panel's listener: %v", err)
	}
	waitValue("session-state", "up")

	// The series records each transition, oldest first: establishing, up,
	// recovering, up (a slow restart may add a down before the last up).
	series, err := gw.ListPropertySeries(ctx, "component", comp, "session-state", time.Hour, 50)
	if err != nil {
		t.Fatalf("session-state series: %v", err)
	}
	var story []string
	for i := len(series) - 1; i >= 0; i-- {
		if series[i].Instance == ep.Name {
			story = append(story, series[i].Value)
		}
	}
	got := strings.Join(story, ",")
	if got != "establishing,up,recovering,up" && got != "establishing,up,recovering,down,up" {
		t.Fatalf("session-state series = %s, want establishing,up,recovering,up", got)
	}

	// The reasons ride the node's own logs.
	deadline := time.Now().Add(5 * time.Second)
	for {
		logs, err := gw.ListNodeLogs(ctx, "edge-s", time.Hour, 200)
		if err != nil {
			t.Fatalf("node logs: %v", err)
		}
		found := false
		for _, l := range logs {
			if l.Message == "session recovering" && l.Facility == "session" {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no 'session recovering' self-log landed in node_log")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Node shutdown tears the session down: the panel holds no session after.
	stop()
	if err := <-ran; err != nil {
		t.Fatalf("node run: %v", err)
	}
	stopped = true
	deadline = time.Now().Add(3 * time.Second)
	for panel.TCPSession().SessionCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the panel still holds %d sessions after node shutdown", panel.TCPSession().SessionCount())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestNodeLandsPushedSamplesThroughARecover drives #817 end to end: the
// shipped newtron-nvp driver's EVT listener comes alive over the held
// session. The node's tick is an hour, so after its first poll nothing polls
// again: a value that lands later can only have been pushed. A change made on
// the panel lands as the component's video-input within a second; the
// panel's listener dies and returns, the session re-arms its subscription,
// and the next change lands within a second too.
func TestNodeLandsPushedSamplesThroughARecover(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	panelIP := "127.89.16.2"
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	if err := engine.Sync([]sim.DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: panelIP,
		ProfileName: nvpProfile(t, store), Version: 1}}); err != nil {
		t.Fatalf("serve the panel: %v", err)
	}
	panel := engine.Devices()[0]
	nvpPort := panelPort(t, panel, "tcp")
	target := net.JoinHostPort(panelIP, fmt.Sprint(nvpPort))
	lx := collection.NewLineExchanger()
	set := func(input string) time.Time {
		t.Helper()
		if got, err := lx.Exchange(ctx, target, "SET INPUT "+input, 2*time.Second); err != nil || got != "INPUT "+input {
			t.Fatalf("SET INPUT %s on the panel = %q, %v", input, got, err)
		}
		return time.Now()
	}

	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-l"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "enroll-token-listener"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-l", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-2"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	comp, drv, nodeName := "panel-2", "newtron-nvp", "edge-l"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &nodeName,
		Inputs:     map[string]string{"host": panelIP, "port": fmt.Sprint(nvpPort)},
		SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatalf("attach newtron-nvp: %v", err)
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()

	runCtx, stop := context.WithCancel(ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: apiSrv.URL, Name: "edge-l", Token: token, HeartbeatEvery: time.Hour})
		ran <- err
	}()
	defer func() { stop(); <-ran }()

	latest := func(key string) string {
		v, err := gw.LatestProperty(ctx, comp, key, ep.Name)
		if err != nil || v == nil {
			return ""
		}
		return v.Value
	}
	waitValue := func(key, want string, within time.Duration) {
		t.Helper()
		deadline := time.Now().Add(within)
		for time.Now().Before(deadline) {
			if latest(key) == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s on %s[%s] = %q, want %q within %s", key, comp, ep.Name, latest(key), want, within)
	}

	waitValue("session-state", "up", 15*time.Second)
	waitValue("video-input", "hdmi1", 15*time.Second) // the first tick's poll

	set("dp1")
	waitValue("video-input", "dp1", time.Second) // pushed: nothing polls for an hour

	panel.StopTCPSession()
	waitValue("session-state", "recovering", 15*time.Second)
	if err := panel.StartTCPSession(nvpPort); err != nil {
		t.Fatalf("restart the panel's listener: %v", err)
	}
	waitValue("session-state", "up", 15*time.Second)

	set("hdmi3")
	waitValue("video-input", "hdmi3", time.Second) // re-armed on recover
}

// panelPort is the port the panel serves a protocol on (tcp for the NVP line
// protocol, telnet, ssh), as its face reported binding it: the one face it
// serves over that protocol.
func panelPort(t *testing.T, panel *sim.Device, protocol string) int {
	t.Helper()
	var found []sim.Face
	for _, f := range panel.Faces() {
		proto := f.Protocol
		if proto == "tcp" && f.TLS {
			proto = "tls" // the line protocol behind TLS (#1035)
		}
		if proto == protocol {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the panel serves %d %s faces (%+v), want one", len(found), protocol, panel.Faces())
	}
	return int(found[0].Addr.Port())
}

// nvpProfile picks the simulator profile serving a product the shipped
// newtron-nvp driver reads, derived from both catalogs rather than named, so
// a catalog rename touches the YAML and nothing here (#804).
func nvpProfile(t *testing.T, store *profile.Store) string {
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
		if p.DriverID != "newtron-nvp" {
			continue
		}
		if name, _, ok := store.ForProduct(p.ID); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		t.Fatal("no simulator profile serves a newtron-nvp product")
	}
	sort.Strings(names)
	return names[0]
}

// fleetSession is one of the fifty held endpoints and the panel behind it.
type fleetSession struct {
	comp, ep, target string
	panel            *sim.Device
}

// faultLineDialer is the real held line dialer, counting the connects in
// flight at once and tripping a panic in the router of one target's first
// inbound line: the injected fault a driver's frame parse would be (#1636).
type faultLineDialer struct {
	inner  collection.StagedDialer
	victim string

	mu      sync.Mutex
	now     int
	peak    int
	armed   bool
	tripped bool
}

func (d *faultLineDialer) Dial(ctx context.Context, target string, timeout time.Duration, in collection.Inbound) (collection.HeldConn, error) {
	pc, err := d.Connect(ctx, target, timeout)
	if err != nil {
		return nil, err
	}
	return pc.Open(in, collection.ChannelOptions{})
}

func (d *faultLineDialer) Connect(ctx context.Context, target string, timeout time.Duration) (collection.PendingConn, error) {
	d.mu.Lock()
	d.now++
	d.peak = max(d.peak, d.now)
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.now--
		d.mu.Unlock()
	}()
	// Long enough that connects overlap if nothing bounds them.
	time.Sleep(20 * time.Millisecond)
	pc, err := d.inner.Connect(ctx, target, timeout)
	if err != nil || target != d.victim {
		return pc, err
	}
	return trippedConn{pc, d}, nil
}

func (d *faultLineDialer) peakInFlight() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.peak
}

// armTrip makes the victim's next inbound line panic, once.
func (d *faultLineDialer) armTrip() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.armed = true
}

// trip reports whether this call is the one that panics: the first line
// after armTrip.
func (d *faultLineDialer) trip() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.armed || d.tripped {
		return false
	}
	d.tripped = true
	return true
}

type trippedConn struct {
	collection.PendingConn
	d *faultLineDialer
}

func (c trippedConn) Open(in collection.Inbound, ch collection.ChannelOptions) (collection.HeldConn, error) {
	return c.PendingConn.Open(trippedInbound{in, c.d}, ch)
}

type trippedInbound struct {
	collection.Inbound
	d *faultLineDialer
}

func (w trippedInbound) Claim(msg driver.Message) bool {
	if w.d.trip() {
		panic("injected frame parse fault")
	}
	return w.Inbound.Claim(msg)
}

// TestNodeRestartHoldsFiftySessionsAndIsolatesAPanic drives #1636 over the
// real wire: fifty simulated panels, each a component with the shipped
// newtron-nvp driver attached, held by one node. A restarted node brings all
// fifty back through its session-dials slots (never more connects in flight
// than the bound), and a panic in one session's reader, injected on its first
// pushed line, recovers that one session through its ordinary failure path
// while the other forty-nine stay up on the connections they held.
func TestNodeRestartHoldsFiftySessionsAndIsolatesAPanic(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	const fleet, dials = 50, 4
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	nvp := nvpProfile(t, store)
	var snaps []sim.DeviceSnapshot
	for i := range fleet {
		snaps = append(snaps, sim.DeviceSnapshot{DeviceID: fmt.Sprintf("panel-%02d", i), Hostname: fmt.Sprintf("panel-%02d", i),
			LoopbackIP: fmt.Sprintf("127.89.19.%d", i+1), ProfileName: nvp, Version: 1})
	}
	if err := engine.Sync(snaps); err != nil {
		t.Fatalf("serve the panels: %v", err)
	}
	panels := map[string]*sim.Device{}
	for _, d := range engine.Devices() {
		panels[d.DeviceID] = d
	}

	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-f"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "enroll-token-fleet"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-f", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	var fleetHeld []fleetSession
	drv, nodeName := "newtron-nvp", "edge-f"
	for i := range fleet {
		host := fmt.Sprintf("panel-%02d", i)
		panel := panels[host]
		if panel == nil {
			t.Fatalf("the simulator serves no %s", host)
		}
		ip, port := fmt.Sprintf("127.89.19.%d", i+1), panelPort(t, panel, "tcp")
		comp := "fleet-" + host
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
			t.Fatalf("create component: %v", err)
		}
		ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
			Driver: &drv, Component: &comp, Node: &nodeName,
			Inputs:     map[string]string{"host": ip, "port": fmt.Sprint(port)},
			SecretRead: all, CanAdmin: true,
		}, all)
		if err != nil {
			t.Fatalf("attach newtron-nvp to %s: %v", comp, err)
		}
		fleetHeld = append(fleetHeld, fleetSession{comp, ep.Name, net.JoinHostPort(ip, fmt.Sprint(port)), panel})
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()

	// start runs the node until the returned stop; the heartbeat is an hour,
	// so every session it holds came up on the worklist's arrival.
	start := func(cfg node.Config) (stop func()) {
		cfg.ServerURL, cfg.Name, cfg.Token, cfg.HeartbeatEvery = apiSrv.URL, "edge-f", token, time.Hour
		runCtx, cancel := context.WithCancel(ctx)
		ran := make(chan error, 1)
		go func() {
			_, err := node.Run(runCtx, cfg)
			ran <- err
		}()
		return func() {
			cancel()
			if err := <-ran; err != nil {
				t.Errorf("node run: %v", err)
			}
		}
	}
	latest := func(h fleetSession) string {
		v, err := gw.LatestProperty(ctx, h.comp, "session-state", h.ep)
		if err != nil || v == nil {
			return ""
		}
		return v.Value
	}
	ups := func() map[string]int {
		n := map[string]int{}
		for _, h := range fleetHeld {
			n[h.comp] = countState(t, gw, h, "up")
		}
		return n
	}
	// allHeld waits until every panel holds exactly one session and the
	// server reads each up, come up since the counts in since.
	allHeld := func(since map[string]int, within time.Duration) {
		t.Helper()
		deadline := time.Now().Add(within)
		for {
			var waiting []string
			for _, h := range fleetHeld {
				if h.panel.TCPSession().SessionCount() != 1 || latest(h) != "up" || countState(t, gw, h, "up") <= since[h.comp] {
					waiting = append(waiting, h.comp)
				}
			}
			if len(waiting) == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d of %d sessions not up and held: %v", len(waiting), fleet, waiting)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	stop := start(node.Config{})
	allHeld(nil, 30*time.Second)
	stop()
	for _, h := range fleetHeld {
		waitPanel(t, h, 0)
	}

	// The restart: all fifty come back, never more than the bound dialing.
	victim := fleetHeld[17]
	faults := &faultLineDialer{inner: collection.NewLineSessionDialer(), victim: victim.target}
	upsAtRestart := ups()
	// The heartbeat is an hour; a short report window is the housekeeping
	// clock that publishes the node's self-logs.
	stop = start(node.Config{SessionDials: dials, LineSession: faults, ReportEvery: 200 * time.Millisecond})
	defer stop()
	allHeld(upsAtRestart, 30*time.Second)
	if p := faults.peakInFlight(); p < 1 || p > dials {
		t.Fatalf("peak connects in flight = %d, want between 1 and the %d session-dials", p, dials)
	}
	upsBefore := ups()

	// A pushed line on the victim trips the panic in its reader.
	faults.armTrip()
	lx := collection.NewLineExchanger()
	if got, err := lx.Exchange(ctx, victim.target, "SET INPUT dp1", 2*time.Second); err != nil || got != "INPUT dp1" {
		t.Fatalf("SET INPUT dp1 on %s = %q, %v", victim.comp, got, err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for countState(t, gw, victim, "recovering") == 0 || latest(victim) != "up" {
		if time.Now().After(deadline) {
			t.Fatalf("the victim's session-state = %q with %d recovering, want it recovered to up", latest(victim), countState(t, gw, victim, "recovering"))
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitNodeLog(t, gw, "edge-f", "session recovering", "panic: injected frame parse fault")

	// The other forty-nine never left up: no recovering, no new up, the
	// same one session held at their panels.
	for _, h := range fleetHeld {
		if h == victim {
			continue
		}
		if n := countState(t, gw, h, "recovering"); n != 0 {
			t.Errorf("%s read recovering %d times beside the victim's panic", h.comp, n)
		}
		if n := countState(t, gw, h, "up"); n != upsBefore[h.comp] {
			t.Errorf("%s came up %d times after the panic, was %d", h.comp, n, upsBefore[h.comp])
		}
		if n := h.panel.TCPSession().SessionCount(); n != 1 {
			t.Errorf("%s's panel holds %d sessions, want its one", h.comp, n)
		}
	}
}

// countState is how many times the endpoint's session-state series reads
// state in the last hour.
func countState(t *testing.T, gw *storage.PG, h fleetSession, state string) int {
	t.Helper()
	series, err := gw.ListPropertySeries(context.Background(), "component", h.comp, "session-state", time.Hour, 50)
	if err != nil {
		t.Fatalf("session-state series of %s: %v", h.comp, err)
	}
	n := 0
	for _, v := range series {
		if v.Instance == h.ep && v.Value == state {
			n++
		}
	}
	return n
}

// waitPanel waits until the panel holds n sessions.
func waitPanel(t *testing.T, h fleetSession, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.panel.TCPSession().SessionCount() != n {
		if time.Now().After(deadline) {
			t.Fatalf("%s's panel holds %d sessions, want %d", h.comp, h.panel.TCPSession().SessionCount(), n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitNodeLog waits for a self-log with this message whose reason attribute
// carries reason.
func waitNodeLog(t *testing.T, gw *storage.PG, nodeName, message, reason string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		logs, err := gw.ListNodeLogs(context.Background(), nodeName, time.Hour, 500)
		if err != nil {
			t.Fatalf("node logs: %v", err)
		}
		for _, l := range logs {
			var attrs struct {
				Reason string `json:"reason"`
			}
			if l.Message == message && json.Unmarshal(l.Attributes, &attrs) == nil && strings.Contains(attrs.Reason, reason) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q self-log with a reason carrying %q landed in node_log", message, reason)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
