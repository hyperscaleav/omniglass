//go:build linux

package node_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestNodeTelnetSessionRejectsThenRecoversOnTheFix drives #1049 and #1033 end
// to end over the real wire: the shipped newtron-nvp-telnet driver attached
// to the simulator's panel with a wrong password. The node's session runs the
// login as its auth step and is refused: it reads rejected with the panel's
// own text and endpoint-authenticated no, logs the rejection once, and dials
// no more across many ticks. An operator's :retry dials exactly once more.
// Fixing the credential reconnects within a tick: up, authenticated, and the
// poll lands its value over the telnet session.
func TestNodeTelnetSessionRejectsThenRecoversOnTheFix(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	panelIP := "127.89.16.3"
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
	telnetPort := panelPort(t, engine.Devices()[0], "telnet")

	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-t"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "enroll-token-telnet"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-t", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	login, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "panel-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": "wrong"},
	}, all, true)
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}
	comp, drv, nodeName := "panel-1", "newtron-nvp-telnet", "edge-t"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &nodeName,
		Inputs:     map[string]string{"host": panelIP, "port": fmt.Sprint(telnetPort), "login": "panel-login"},
		SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatalf("attach newtron-nvp-telnet: %v", err)
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
		_, err := node.Run(runCtx, node.Config{ServerURL: apiSrv.URL, Name: "edge-t", Token: token, HeartbeatEvery: 100 * time.Millisecond})
		ran <- err
	}()
	defer func() {
		stop()
		<-ran
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
	countLogs := func(message string) int {
		logs, err := gw.ListNodeLogs(ctx, "edge-t", time.Hour, 500)
		if err != nil {
			t.Fatalf("node logs: %v", err)
		}
		n := 0
		for _, l := range logs {
			if l.Message == message && l.Facility == "session" {
				n++
			}
		}
		return n
	}
	waitLogs := func(message string, want int) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for countLogs(message) < want && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if got := countLogs(message); got != want {
			t.Fatalf("%q self-logs = %d, want %d", message, got, want)
		}
	}

	// The wrong password: rejected with the panel's text, not authenticated.
	waitValue("session-state", "rejected")
	waitValue("session-reason", "auth: auth rejected")
	waitValue("endpoint-authenticated", "no")
	waitLogs("session rejected", 1)
	// Twenty ticks later the node has not dialed again: still one rejection.
	time.Sleep(2 * time.Second)
	waitLogs("session rejected", 1)

	// An operator's retry dials exactly once more, and is refused again.
	if _, err := gw.RetryEndpoint(ctx, "", ep.ID, storage.RetryPin{}, all, all); err != nil {
		t.Fatalf("retry: %v", err)
	}
	waitLogs("session rejected", 2)
	time.Sleep(time.Second)
	waitLogs("session rejected", 2)

	// The fix: the credential's edit recycles the session, which logs in.
	if _, err := gw.UpdateSecret(ctx, "", login.ID, storage.SecretPatch{Fields: map[string]string{"username": "admin", "password": "newtron"}}, all, all, true, true); err != nil {
		t.Fatalf("fix the credential: %v", err)
	}
	waitValue("session-state", "up")
	waitValue("endpoint-authenticated", "yes")
	waitValue("video-input", "hdmi1")
}
