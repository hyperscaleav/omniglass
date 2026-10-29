//go:build linux

package node_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A driver emit lands on any lane (#1145), end to end over the real wire:
// Postgres, the bus, a real node and the simulator. The shipped newtron-nvp
// driver lands one push on two lanes (the video-input property and an
// input-changed event carrying the input), a diagnostics line as a log line
// with its severity, and a push no listener claims whole as a log line, all
// on the component and none in the node's self-logs; the shipped kestrel-api
// driver lands a call starting and ending as call-started and call-ended
// carrying the call's id.

// waitFor polls cond until it holds or within passes.
func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s: not within %s", what, within)
}

// eventsOf is the component's recent events of one type.
func eventsOf(t *testing.T, gw *storage.PG, comp, key string) []storage.Event {
	t.Helper()
	evs, err := gw.ListComponentEvents(context.Background(), comp, time.Hour, 200)
	if err != nil {
		t.Fatal(err)
	}
	var out []storage.Event
	for _, e := range evs {
		if e.Key == key {
			out = append(out, e)
		}
	}
	return out
}

func payloadOf(t *testing.T, e storage.Event) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(e.Attributes, &m); err != nil {
		t.Fatalf("event %s payload %s: %v", e.Key, e.Attributes, err)
	}
	return m
}

func TestNodeLandsAnNVPPushOnEveryLane(t *testing.T) {
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

	panelIP := "127.89.17.2"
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
	operate := func(path, body string) {
		t.Helper()
		if res := panel.Operate(sim.Operation{Kind: "http", Method: "POST", Path: path, Body: body}); res.Status != 200 {
			t.Fatalf("POST %s %s on the panel = %+v", path, body, res)
		}
	}

	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-lanes"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "enroll-token-lanes"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-lanes", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-lanes"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	comp, drv, nodeName := "panel-lanes", "newtron-nvp", "edge-lanes"
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
		_, err := node.Run(runCtx, node.Config{ServerURL: apiSrv.URL, Name: nodeName, Token: token, HeartbeatEvery: time.Hour})
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
	logs := func() []storage.LogLine {
		l, err := gw.ListComponentLogs(ctx, comp, time.Hour, 200, storage.LogFilter{})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	lineWith := func(msg string) (storage.LogLine, bool) {
		for _, l := range logs() {
			if l.Message == msg {
				return l, true
			}
		}
		return storage.LogLine{}, false
	}

	waitFor(t, "the session up", 15*time.Second, func() bool { return latest("session-state") == "up" })

	// One push, two lanes: the property and the event carrying the input.
	if got, err := lx.Exchange(ctx, target, "SET INPUT hdmi2", 2*time.Second); err != nil || got != "INPUT hdmi2" {
		t.Fatalf("SET INPUT hdmi2 = %q, %v", got, err)
	}
	waitFor(t, "video-input hdmi2", 5*time.Second, func() bool { return latest("video-input") == "hdmi2" })
	waitFor(t, "an input-changed event", 5*time.Second, func() bool { return len(eventsOf(t, gw, comp, "input-changed")) == 1 })
	ev := eventsOf(t, gw, comp, "input-changed")[0]
	if ev.Message != "Video input changed" || payloadOf(t, ev)["input"] != "hdmi2" || ev.Origin != "caught" || ev.Instance != ep.Name {
		t.Fatalf("input-changed = %+v %s, want the input in its payload, caught, on the endpoint", ev, ev.Attributes)
	}

	// A diagnostics line: a log line on the component with its severity.
	operate("/api/diagnostics/log", `{"line":"WARN backlight driver reset"}`)
	waitFor(t, "the diagnostics log line", 5*time.Second, func() bool { _, ok := lineWith("backlight driver reset"); return ok })
	l, _ := lineWith("backlight driver reset")
	if l.Severity != "warning" || l.Facility != "panel" || l.Instance != ep.Name || l.Source != "tcp" {
		t.Fatalf("diagnostics line = %+v, want warning from the panel facility, collected by the endpoint", l)
	}

	// A push no listener claims lands whole, raw arrival.
	operate("/api/room/scene", `{"scene":"presentation"}`)
	waitFor(t, "the unclaimed scene line", 5*time.Second, func() bool { _, ok := lineWith("EVT SCENE presentation"); return ok })

	// None of the component's lines is the node's own.
	self, err := gw.ListNodeLogs(ctx, nodeName, time.Hour, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(self) == 0 {
		t.Fatal("the node shipped no self-logs")
	}
	for _, s := range self {
		if strings.Contains(s.Message, "backlight driver reset") || s.Message == "EVT SCENE presentation" {
			t.Fatalf("a component's line landed as the node's own: %+v", s)
		}
	}

	// A second input change is a second occurrence.
	if got, err := lx.Exchange(ctx, target, "SET INPUT hdmi3", 2*time.Second); err != nil || got != "INPUT hdmi3" {
		t.Fatalf("SET INPUT hdmi3 = %q, %v", got, err)
	}
	waitFor(t, "a second input-changed", 5*time.Second, func() bool { return len(eventsOf(t, gw, comp, "input-changed")) == 2 })
}

func TestNodeLandsKestrelCallEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var barProfile string
	s := newHTTPStack(t, "edge-calls", nil, func(store *profile.Store) []string {
		barProfile = driverProfile(t, store, "kestrel-api", "websocket") // the bar (only it has the control socket)
		return []string{barProfile}
	})
	bar := s.devices[barProfile]
	s.component("bar-calls")
	s.secret("bar-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	s.attachHTTP("bar-calls", "kestrel-api", "edge-calls", map[string]string{
		"host": s.ips[barProfile], "port": fmt.Sprint(s.httpPort(barProfile)), "login": "bar-login"})
	s.runNode("edge-calls")
	gw := s.gw

	waitFor(t, "the stream up", 20*time.Second, func() bool {
		v, err := gw.LatestProperty(s.ctx, "bar-calls", "session-state", "https")
		return err == nil && v != nil && v.Value == "up"
	})
	if res := bar.Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/call/dial", Body: `{"id":"c9001","participants":3}`}); res.Status != 200 {
		t.Fatalf("dial = %+v", res)
	}
	waitFor(t, "call-started", 5*time.Second, func() bool { return len(eventsOf(t, gw, "bar-calls", "call-started")) == 1 })
	started := eventsOf(t, gw, "bar-calls", "call-started")[0]
	if p := payloadOf(t, started); started.Message != "Call started" || p["call_id"] != "c9001" || p["participants"] != float64(3) {
		t.Fatalf("call-started = %+v %s, want the call id and a numeric participant count", started, started.Attributes)
	}
	if res := bar.Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/call/disconnect", Body: `{}`}); res.Status != 200 {
		t.Fatalf("disconnect = %+v", res)
	}
	waitFor(t, "call-ended", 5*time.Second, func() bool { return len(eventsOf(t, gw, "bar-calls", "call-ended")) == 1 })
	if p := payloadOf(t, eventsOf(t, gw, "bar-calls", "call-ended")[0]); p["call_id"] != "c9001" {
		t.Fatalf("call-ended payload = %v, want the id of the call that ended", p)
	}
}
