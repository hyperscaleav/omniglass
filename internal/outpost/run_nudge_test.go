//go:build linux

package node_test

import (
	"context"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// addrLine is the stateless line client, recording the address of every ask.
type addrLine struct {
	mu    sync.Mutex
	addrs []string
}

func (c *addrLine) Exchange(_ context.Context, addr, _ string, _ time.Duration) (string, error) {
	c.mu.Lock()
	c.addrs = append(c.addrs, addr)
	c.mu.Unlock()
	return "INPUT hdmi2", nil
}

// asked reports whether any ask went to an address on port.
func (c *addrLine) asked(port string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range c.addrs {
		if strings.HasSuffix(a, ":"+port) {
			return true
		}
	}
	return false
}

// acceptor is a local port that accepts and closes, so an endpoint's
// reachability probe answers at once; it returns the port.
func acceptor(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

// #1945 end to end: a node pulling its worklist once an hour still runs an
// endpoint the moment it is created or edited, because the commit nudges it
// to pull now; and a burst of edits costs a bounded number of pulls, because
// the node coalesces the nudges.
func TestAWorklistChangeReachesTheNodeAtOnce(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "panel-nudge", Label: "Panel", Version: "1.0.0", Spec: []byte(`{"version": 1, "transport": "tcp",
		"inputs": [{"name": "host", "kind": "string", "required": true}, {"name": "port", "kind": "number", "default": "4998"}],
		"polls": [{"name": "status", "schedule": {"every": "200ms"}, "request": {"line": "GET INPUT"},
			"emits": [{"name": "video-input", "extract": {"regex": "^INPUT (\\S+)$"}}]}]}`)}); err != nil {
		t.Fatal(err)
	}
	token := enrolledNode(t, ctx, gw, "edge-n")
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-n"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()

	line := &addrLine{}
	runCtx, stop := context.WithCancel(ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: apiSrv.URL, Name: "edge-n", Token: token,
			HeartbeatEvery: 50 * time.Millisecond, ProbeEvery: 50 * time.Millisecond,
			WorklistEvery: time.Hour, Line: line})
		ran <- err
	}()
	defer func() {
		stop()
		if err := <-ran; err != nil {
			t.Errorf("node run: %v", err)
		}
	}()
	pollUntil(t, 10*time.Second, "the node never took its first worklist", func() bool {
		return srv.WorklistCounts().Served >= 1
	})

	// A new endpoint runs within a second, not on the hourly pull.
	first, second := acceptor(t), acceptor(t)
	comp, drv, nodeName := "panel-n", "panel-nudge", "edge-n"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &nodeName,
		Inputs: map[string]string{"host": "127.0.0.1", "port": first}, SecretRead: all, SecretBind: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatal(err)
	}
	pollUntil(t, time.Second, "a created endpoint did not run within 1s", func() bool { return line.asked(first) })

	// An address edit moves the polls within a second.
	if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"port": second}}, all, all); err != nil {
		t.Fatal(err)
	}
	pollUntil(t, time.Second, "an edited endpoint did not move within 1s", func() bool { return line.asked(second) })

	// A burst of 100 changes: the node pulls at least once after it (the
	// last change is not lost), and at most once per nudge gap while it runs.
	before := srv.WorklistCounts().Served
	start := time.Now()
	for i := range 100 {
		label := "burst " + strconv.Itoa(i)
		if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Label: &label}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	pollUntil(t, 2*time.Second, "the burst nudged no pull", func() bool {
		return srv.WorklistCounts().Served > before
	})
	time.Sleep(3 * node.NudgeGap) // let any last coalesced pull land
	elapsed := time.Since(start)
	pulls := srv.WorklistCounts().Served - before
	bound := int64(elapsed/node.NudgeGap) + 3
	if pulls > bound {
		t.Fatalf("100 changes over %v cost %d pulls, want at most %d (one per %v gap)", elapsed, pulls, bound, node.NudgeGap)
	}
	t.Logf("100 changes over %v cost %d pulls (bound %d)", elapsed.Round(time.Millisecond), pulls, bound)
}
