package bus_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSimRoundTrip proves the simulator's wire over a real in-process NATS
// server: a simulator holding a node's credential pulls the devices behind
// that node (stating its capabilities), its hits reach the server's
// registry, it cannot pull another node's list, and a hit on a component it
// was not handed is not recorded.
func TestSimRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	for _, n := range []string{"node-a", "node-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-a", hashHex("secret-a"), all); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"dsp-1", "dsp-2"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`insert into endpoint (name, transport, component, node_name, params) values ('dsp-1-tcp', 'tcp', (select id from component where name = 'dsp-1'), (select principal_id from node where name = 'node-a'), '{"target":"127.21.0.5:80"}'::jsonb)`,
		`insert into endpoint (name, transport, component, node_name, params) values ('dsp-2-tcp', 'tcp', (select id from component where name = 'dsp-2'), (select principal_id from node where name = 'node-b'), '{"target":"127.21.0.6:80"}'::jsonb)`,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	var dsp2 string
	if err := conn.QueryRow(ctx, `select id from component where name = 'dsp-2'`).Scan(&dsp2); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()
	permErrs := make(chan error, 16)
	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo("node-a", "secret-a"),
		nats.CustomInboxPrefix(collection.InboxPrefix("node-a")),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, e error) { permErrs <- e }))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	req, _ := json.Marshal(collection.SimlistRequest{Capabilities: collection.SimCapabilities{NetAdmin: true, PortOffset: 20000}})
	msg, err := nc.Request(collection.SimlistSubject("node-a"), req, 3*time.Second)
	if err != nil {
		t.Fatalf("simlist request: %v", err)
	}
	var reply collection.SimlistReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Devices) != 1 || reply.Devices[0].Name != "dsp-1" || reply.Devices[0].Address != "127.21.0.5" {
		t.Fatalf("simlist = %+v, want dsp-1 at 127.21.0.5", reply.Devices)
	}
	dsp1 := reply.Devices[0].ComponentID

	batch, _ := json.Marshal(collection.SimHitBatch{
		Serving: []string{dsp1},
		Hits: []collection.SimHit{
			{ComponentID: dsp1, Protocol: "http", Path: "/api/status", Status: 200, At: time.Now()},
			{ComponentID: dsp2, Protocol: "http", At: time.Now()}, // node-b's component
		},
	})
	if err := nc.Publish(collection.SimHitSubject("node-a"), batch); err != nil {
		t.Fatal(err)
	}
	_ = nc.Flush()

	deadline := time.Now().Add(3 * time.Second)
	var st bus.SimStatus
	for time.Now().Before(deadline) {
		var ok bool
		if st, ok = srv.SimStatus("node-a"); ok && st.Devices[dsp1].Count > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Devices[dsp1].Count != 1 || st.Devices[dsp1].ByProtocol["http"] != 1 {
		t.Fatalf("dsp-1 hits = %+v, want one http hit", st.Devices[dsp1])
	}
	if _, ok := st.Devices[dsp2]; ok {
		t.Fatal("a hit on another node's component was recorded")
	}
	if !st.Capabilities.NetAdmin || st.Capabilities.PortOffset != 20000 || !st.Serving[dsp1] {
		t.Fatalf("status = %+v, want the stated capabilities and dsp-1 serving", st)
	}

	// Isolation: node-a's credential is refused node-b's subjects by the
	// grant itself (a permissions violation), not merely left unanswered.
	for _, subj := range []string{collection.SimlistSubject("node-b"), collection.SimHitSubject("node-b")} {
		if err := nc.Publish(subj, req); err != nil {
			t.Fatal(err)
		}
		_ = nc.Flush()
		if !awaitPermissionViolation(permErrs, 3*time.Second) {
			t.Fatalf("no permissions violation publishing on %s", subj)
		}
	}
}
