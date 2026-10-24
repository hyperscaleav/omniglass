//go:build linux

package node_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

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

// TestNodeSSHSessionPinsTheHostKeyAndRefusesAChange drives #1032 end to end:
// the shipped newtron-nvp-ssh driver attached to the simulator's panel. The
// node's ssh session authenticates, trusts the panel's host key on first use
// (ssh-host-key lands on the server, which delivers it back as the pin), and
// polls over the shell. The panel is replaced (a new key at the same
// address): the session reads rejected, class host-key, naming both keys,
// and dials no more. The operator retries with the new key: the session
// redials onto it and reads up.
func TestNodeSSHSessionPinsTheHostKeyAndRefusesAChange(t *testing.T) {
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

	panelIP := "127.89.16.4"
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	if err := engine.Sync([]sim.DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: panelIP,
		ProfileName: nvpProfile(t, store), Version: 1}}); err != nil {
		t.Fatal(err)
	}
	panel := engine.Devices()[0]
	sshPort := panelPort(t, panel, "ssh")

	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-h"}, all, all); err != nil {
		t.Fatal(err)
	}
	token := "enroll-token-ssh"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-h", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "panel-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": "newtron"},
	}, all, true); err != nil {
		t.Fatal(err)
	}
	comp, drv, nodeName := "panel-1", "newtron-nvp-ssh", "edge-h"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &nodeName,
		Inputs:     map[string]string{"host": panelIP, "port": fmt.Sprint(sshPort), "login": "panel-login"},
		SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatalf("attach newtron-nvp-ssh: %v", err)
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()
	runCtx, stop := context.WithCancel(ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: apiSrv.URL, Name: "edge-h", Token: token, HeartbeatEvery: 100 * time.Millisecond})
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
	waitFor := func(key string, ok func(string) bool, what string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if ok(latest(key)) {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("%s on %s[%s] = %q, want %s", key, comp, ep.Name, latest(key), what)
	}
	is := func(want string) func(string) bool { return func(v string) bool { return v == want } }

	first := ssh.FingerprintSHA256(panel.SSHHostKey())
	waitFor("session-state", is("up"), "up")
	waitFor("ssh-host-key", is(first), "the panel's key, trusted on first use")
	waitFor("video-input", is("hdmi1"), "hdmi1")
	// The server delivers the trusted key back as the pin.
	deadline := time.Now().Add(5 * time.Second)
	for {
		wl, err := gw.NodeWorklist(ctx, "edge-h")
		if err != nil {
			t.Fatal(err)
		}
		pinned := ""
		for _, task := range wl.Tasks {
			if task.HostKey != "" {
				pinned = task.HostKey
			}
		}
		if pinned == first {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worklist pin = %q, want %q", pinned, first)
		}
		time.Sleep(25 * time.Millisecond)
	}

	// The panel is replaced: a new key at the same address.
	panel.StopSSH()
	if err := panel.RotateSSHHostKey(); err != nil {
		t.Fatal(err)
	}
	if err := panel.StartSSH(sshPort, "admin", "newtron", false); err != nil {
		t.Fatal(err)
	}
	second := ssh.FingerprintSHA256(panel.SSHHostKey())
	waitFor("session-state", is("rejected"), "rejected")
	waitFor("session-reason", func(v string) bool {
		return strings.HasPrefix(v, "host-key: ") && strings.Contains(v, first) && strings.Contains(v, second)
	}, "host-key naming both keys")
	if got := latest("ssh-host-key"); got != first {
		t.Fatalf("a refused key replaced the pin: %q", got)
	}

	// A malformed key is refused at the door; the offered one is accepted.
	if _, err := gw.RetryEndpoint(ctx, "", ep.ID, storage.RetryPin{HostKey: "SHA256:nope"}, all, all); err == nil {
		t.Fatal("a malformed host key was accepted")
	}
	if _, err := gw.RetryEndpoint(ctx, "", ep.ID, storage.RetryPin{HostKey: second}, all, all); err != nil {
		t.Fatalf("retry with the new key: %v", err)
	}
	waitFor("session-state", is("up"), "up on the accepted key")
	waitFor("ssh-host-key", is(second), "the accepted key")
}
