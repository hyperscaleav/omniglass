package node_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http/httptest"
	"strings"
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

// #823 end to end: fixing a regex in a driver changes what an attached
// endpoint emits on the next poll. A real line server answers GET INPUT; the
// driver's first spec captures digits only, so the poll faults and lands
// nothing; the fix re-derives the endpoint's poll, and the next poll lands
// the value.
func TestDriverFixReachesTheNextPoll(t *testing.T) {
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

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimSpace(line) == "GET INPUT" {
						_, _ = c.Write([]byte("INPUT hdmi2\r\n"))
					}
				}
			}(c)
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())

	spec := func(regex string) []byte {
		return []byte(`{"version": 1, "transport": "tcp",
			"inputs": [{"name": "host", "kind": "string", "required": true}, {"name": "port", "kind": "number"}],
			"polls": [{"name": "status", "schedule": {"every": "30s"}, "request": {"line": "GET INPUT"},
				"emits": [{"name": "video-input", "extract": {"regex": "` + regex + `"}}]}]}`)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "panel-fix", Label: "Panel", Version: "1.0.0", Spec: spec(`^INPUT (\\d+)$`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-f"}, all, all); err != nil {
		t.Fatal(err)
	}
	token := "enroll-token-fix"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-f", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-f"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	comp, drv, nodeName := "panel-f", "panel-fix", "edge-f"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &nodeName,
		Inputs: map[string]string{"host": host, "port": port}, SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()
	runOnce := func() {
		t.Helper()
		if _, err := node.Run(ctx, node.Config{ServerURL: apiSrv.URL, Name: "edge-f", Token: token, Once: true}); err != nil {
			t.Fatalf("node run: %v", err)
		}
	}
	latest := func() string {
		v, err := gw.LatestProperty(ctx, comp, "video-input", ep.Name)
		if err != nil || v == nil {
			return ""
		}
		return v.Value
	}

	runOnce()
	time.Sleep(500 * time.Millisecond) // let ingest drain
	if got := latest(); got != "" {
		t.Fatalf("the broken regex landed %q", got)
	}

	if _, err := gw.UpdateDriver(ctx, "", "panel-fix", storage.DriverPatch{Spec: spec(`^INPUT (\\S+)$`)}); err != nil {
		t.Fatal(err)
	}
	runOnce()
	deadline := time.Now().Add(5 * time.Second)
	for latest() != "hdmi2" {
		if time.Now().After(deadline) {
			t.Fatalf("after the fix the next poll landed %q, want hdmi2", latest())
		}
		time.Sleep(25 * time.Millisecond)
	}
}
