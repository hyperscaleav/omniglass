package bus_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
)

// The actuation loop end to end (#815): an issued settleable command reaches
// its node over the per-node queue, actuates a real device over a real socket,
// the execution report stamps once, and the settle loop closes with a real
// observed value. Node isolation covers the queue exactly as it covers the
// worklist: node B pulling node A's queue is a permissions violation.

// wireDriverSpec is the line-protocol driver the loop actuates through.
const wireDriverSpec = `{
	"version": 1,
	"transport": "tcp",
	"inputs": [
		{"name": "host", "kind": "string", "required": true},
		{"name": "port", "kind": "number", "required": true}
	],
	"polls": [{
		"name": "status",
		"schedule": {"every": "30s"},
		"request": {"line": "GET INPUT"},
		"emits": [{"name": "video-input", "extract": {"regex": "^INPUT (\\S+)$"}}]
	}],
	"commands": [
		{"command_type": "set-input-fast", "request": {"line": "SET INPUT ${value}"}}
	]
}`

func TestCommandWireActuates(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	// The device: a real line server recording what it is told.
	deviceLines := make(chan string, 4)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("device listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				line, err := bufio.NewReader(c).ReadString('\n')
				if err != nil {
					return
				}
				deviceLines <- strings.TrimRight(line, "\r\n")
				_, _ = c.Write([]byte("OK\r\n"))
			}(conn)
		}
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("device addr: %v", err)
	}

	// Two enrolled nodes, a component, and the driver attached with its
	// endpoint placed on node-a.
	for _, name := range []string{"node-a", "node-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	tokenA, tokenB := "secret-a", "secret-b"
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-a", hashHex(tokenA), all); err != nil {
		t.Fatalf("mint a: %v", err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-b", hashHex(tokenB), all); err != nil {
		t.Fatalf("mint b: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "switcher-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	// A short-window twin of set-input, so the loop can wait out the window
	// (settlement is pending WITHIN the window by design) in test time.
	if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
		Name: "set-input-fast", Label: "Set Input (fast)",
		SettleWindowSeconds: 1, TargetPropertyType: "video-input",
	}); err != nil {
		t.Fatalf("create command type: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "line-proto", Label: "Line Proto", Version: "1.0.0", Spec: []byte(wireDriverSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv, nodeA := "switcher-1", "line-proto", "node-a"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &nodeA,
		Inputs: map[string]string{"host": host, "port": port},
	}, all); err != nil {
		t.Fatalf("attach: %v", err)
	}

	// The issuing operator.
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}

	cmd, err := gw.IssueCommand(ctx, actor, "component", "switcher-1", "set-input-fast", "", json.RawMessage(`"hdmi-2"`), nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	url := srv.ClientURL()

	// Node A pulls its queue, actuates the device, reports the outcome:
	// exactly what the run loop does, hand-rolled so each stage asserts.
	ncA, err := nats.Connect(url,
		nats.UserInfo("node-a", tokenA),
		nats.CustomInboxPrefix(collection.InboxPrefix("node-a")),
	)
	if err != nil {
		t.Fatalf("node-a connect: %v", err)
	}
	defer ncA.Close()

	msg, err := ncA.Request(collection.CommandSubject("node-a"), nil, 3*time.Second)
	if err != nil {
		t.Fatalf("node-a command pull: %v", err)
	}
	var reply collection.CommandPullReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if len(reply.Commands) != 1 {
		t.Fatalf("queue = %+v, want the one command", reply.Commands)
	}
	d := reply.Commands[0]
	if d.ID != cmd.ID || d.Line != "SET INPUT hdmi-2" || d.Transport != "tcp" {
		t.Fatalf("delivery = %+v", d)
	}

	// Actuate over the real socket.
	exchanger := collection.NewLineExchanger()
	answer, err := exchanger.Exchange(ctx, d.Target, d.Line, 3*time.Second)
	if err != nil {
		t.Fatalf("actuate: %v", err)
	}
	if answer != "OK" {
		t.Fatalf("device answered %q", answer)
	}
	select {
	case got := <-deviceLines:
		if got != "SET INPUT hdmi-2" {
			t.Fatalf("device was told %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the device never heard the command")
	}

	// Report the execution; the row stamps once.
	st, _ := json.Marshal(collection.CommandStatus{ID: d.ID})
	if err := ncA.Publish(collection.CommandStatusSubject("node-a"), st); err != nil {
		t.Fatalf("report: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var executed *time.Time
		if err := conn.QueryRow(ctx, `select executed_at from command where id = $1`, cmd.ID).Scan(&executed); err != nil {
			t.Fatalf("read executed_at: %v", err)
		}
		if executed != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the execution report never stamped")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The device now reports the input it was told: the observed value lands
	// (the poll path's write, hand-driven here) and the command settles.
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "switcher-1", Key: "video-input",
		Value: "hdmi-2", Source: "tcp", TS: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("observed sample: %v", err)
	}
	// Past the window with the observed matching the intended, the loop closes.
	settleDeadline := time.Now().Add(5 * time.Second)
	for {
		verdict, err := gw.CommandSettlement(ctx, "component", "switcher-1", "set-input-fast", "")
		if err != nil {
			t.Fatalf("settlement: %v", err)
		}
		if string(verdict) == "settled" {
			break
		}
		if time.Now().After(settleDeadline) {
			t.Fatalf("verdict = %q, want settled", verdict)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Isolation: node B cannot pull node A's queue; the request dies inside
	// the denied publish, never reaching the responder.
	ncB, err := nats.Connect(url,
		nats.UserInfo("node-b", tokenB),
		nats.CustomInboxPrefix(collection.InboxPrefix("node-b")),
	)
	if err != nil {
		t.Fatalf("node-b connect: %v", err)
	}
	defer ncB.Close()
	if _, err := ncB.Request(collection.CommandSubject("node-a"), nil, 700*time.Millisecond); err == nil {
		t.Fatal("node-b pulled node-a's command queue")
	}
}
