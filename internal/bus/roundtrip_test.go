package bus_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
)

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestNodeRoundTrip is the checkpoint-2 closing gate: a node enrolls and, over a
// real in-process nats-server on an ephemeral port, pulls its worklist and
// heartbeats, while per-node subject isolation is negatively proven (node A
// cannot publish or pull as node B) and a bad credential is rejected at connect.
func TestNodeRoundTrip(t *testing.T) {
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

	// Enroll node-a and node-b: create + mint token.
	for _, name := range []string{"node-a", "node-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all); err != nil {
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

	// Seed a component + interface + enabled task placed on node-a.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1", ComponentType: "display"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, node_name, params) values ('disp-1-icmp', 'icmp', 'disp-1', 'node-a', '{"target":"10.0.0.1"}'::jsonb)`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_id, node_name, spec, enabled) values ('t-icmp', 'poll', (select id from interface where name = 'disp-1-icmp'), 'node-a', '{"probe":"icmp"}'::jsonb, true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	conn.Close(ctx)

	// Start the embedded server on an ephemeral port.
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	url := srv.ClientURL()

	// Node A connects with its own credential.
	permErrs := make(chan error, 16)
	ncA, err := nats.Connect(url,
		nats.UserInfo("node-a", tokenA),
		nats.CustomInboxPrefix(collection.InboxPrefix("node-a")),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, e error) { permErrs <- e }),
	)
	if err != nil {
		t.Fatalf("node-a connect: %v", err)
	}
	defer ncA.Close()

	// Positive: node A pulls its worklist.
	msg, err := ncA.Request(collection.WorklistSubject("node-a"), nil, 3*time.Second)
	if err != nil {
		t.Fatalf("node-a worklist request: %v", err)
	}
	var reply collection.WorklistReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		t.Fatalf("decode worklist: %v", err)
	}
	if len(reply.Tasks) != 1 || reply.Tasks[0].ID != "t-icmp" || reply.Tasks[0].InterfaceType != "icmp" {
		t.Fatalf("worklist = %+v, want one t-icmp/icmp task", reply.Tasks)
	}
	if reply.ConfigGeneration == 0 {
		t.Fatalf("config_generation = 0, want the interface updated_at epoch")
	}

	// Positive: node A heartbeats; the server records last_heartbeat_at.
	publishHeartbeat(t, ncA, "node-a")
	waitHeartbeat(t, ctx, gw, "node-a", true)

	// Negative isolation: node A cannot publish on node B's heartbeat subject.
	// The publish is denied (a permissions violation), and node B's heartbeat is
	// never recorded.
	if err := ncA.Publish(collection.HeartbeatSubject("node-b"), heartbeatBytes(t, "node-b")); err != nil {
		t.Fatalf("publish (client-side) node-b: %v", err)
	}
	_ = ncA.Flush()
	if !awaitPermissionViolation(permErrs, 3*time.Second) {
		t.Fatalf("expected a permissions-violation error publishing to node-b's subject")
	}
	// Give any (impossible) delivery a moment, then assert node B stayed silent.
	time.Sleep(300 * time.Millisecond)
	waitHeartbeat(t, ctx, gw, "node-b", false)

	// Negative isolation: node A cannot pull node B's worklist (publish denied).
	if _, err := ncA.Request(collection.WorklistSubject("node-b"), nil, 1*time.Second); err == nil {
		t.Fatalf("node-a pulling node-b worklist: want error, got a reply")
	}

	// Auth negative: a wrong password is rejected at connect.
	bad, err := nats.Connect(url, nats.UserInfo("node-a", "wrong-token"), nats.MaxReconnects(0), nats.Timeout(2*time.Second))
	if err == nil {
		bad.Close()
		t.Fatalf("connect with wrong token: want rejection, got a connection")
	}
}

// TestWorklistReplyConfusedDeputy proves the worklist responder refuses to reply
// to any subject outside the requesting node's own inbox. The responder answers
// with the server's FULL-PERMISSION internal client, and msg.Reply is
// attacker-controlled, so an enrolled node that points the reply at another
// node's heartbeat subject would otherwise forge that node's liveness (and, once
// a stream exists, redirect to $JS/$SYS). node-a requests its own worklist
// subject (allowed by its grant) but sets the reply to node-b's heartbeat
// subject; node-b's last_heartbeat_at must stay nil, and node-a's legitimate
// pull (a real inbox reply) must still return its worklist.
func TestWorklistReplyConfusedDeputy(t *testing.T) {
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

	// node-a is enrolled and connects; node-b is enrolled but never heartbeats
	// and never connects (its liveness can only come from a forge).
	for _, name := range []string{"node-a", "node-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	tokenA := "secret-a"
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-a", hashHex(tokenA), all); err != nil {
		t.Fatalf("mint a: %v", err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-b", hashHex("secret-b"), all); err != nil {
		t.Fatalf("mint b: %v", err)
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	url := srv.ClientURL()

	ncA, err := nats.Connect(url,
		nats.UserInfo("node-a", tokenA),
		nats.CustomInboxPrefix(collection.InboxPrefix("node-a")),
	)
	if err != nil {
		t.Fatalf("node-a connect: %v", err)
	}
	defer ncA.Close()

	// The forge: request node-a's own worklist subject (permitted) but aim the
	// reply at node-b's heartbeat subject. If the responder honored msg.Reply, the
	// internal client would publish node-a's worklist to og.v1.heartbeat.node-b,
	// the heartbeat sink would consume it, and node-b's liveness would be forged.
	if err := ncA.PublishRequest(collection.WorklistSubject("node-a"), collection.HeartbeatSubject("node-b"), nil); err != nil {
		t.Fatalf("node-a forge publish: %v", err)
	}
	_ = ncA.Flush()

	// A legitimate pull round-trips through the same worklist handler, so once its
	// reply returns the forge request has already been fully handled (including any
	// Respond it would emit). The happy path must not regress.
	msg, err := ncA.Request(collection.WorklistSubject("node-a"), nil, 3*time.Second)
	if err != nil {
		t.Fatalf("node-a legitimate worklist pull: %v", err)
	}
	var reply collection.WorklistReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		t.Fatalf("decode worklist: %v", err)
	}

	// Let the heartbeat hop (the only remaining async step) land, then assert the
	// forge was blocked: node-b's last_heartbeat_at is still nil.
	time.Sleep(300 * time.Millisecond)
	waitHeartbeat(t, ctx, gw, "node-b", false)
}

func heartbeatBytes(t *testing.T, node string) []byte {
	t.Helper()
	b, err := json.Marshal(collection.Heartbeat{Node: node, At: time.Now().UTC()})
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	return b
}

func publishHeartbeat(t *testing.T, nc *nats.Conn, node string) {
	t.Helper()
	if err := nc.Publish(collection.HeartbeatSubject(node), heartbeatBytes(t, node)); err != nil {
		t.Fatalf("publish heartbeat %s: %v", node, err)
	}
	_ = nc.Flush()
}

// waitHeartbeat asserts last_heartbeat_at becomes set (want=true) within a
// window, or stays nil (want=false).
func waitHeartbeat(t *testing.T, ctx context.Context, gw storage.Gateway, node string, want bool) {
	t.Helper()
	all := scope.Set{All: true}
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := gw.GetNode(ctx, node, all)
		if err != nil {
			t.Fatalf("get node %s: %v", node, err)
		}
		got := n.LastHeartbeatAt != nil
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("node %s last_heartbeat_at set = %v, want %v", node, got, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func awaitPermissionViolation(errs <-chan error, within time.Duration) bool {
	deadline := time.After(within)
	for {
		select {
		case e := <-errs:
			if e != nil && strings.Contains(strings.ToLower(e.Error()), "permissions violation") {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
