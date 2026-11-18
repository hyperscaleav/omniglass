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

// TestOutpostRoundTrip is the checkpoint-2 closing gate: an outpost enrolls and, over a
// real in-process nats-server on an ephemeral port, pulls its worklist and
// heartbeats, while per-outpost subject isolation is negatively proven (outpost A
// cannot publish or pull as outpost B) and a bad credential is rejected at connect.
func TestOutpostRoundTrip(t *testing.T) {
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

	// Enroll outpost-a and outpost-b: create + mint token.
	for _, name := range []string{"outpost-a", "outpost-b"} {
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: name}, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	tokenA, tokenB := "secret-a", "secret-b"
	if _, err := gw.SetEnrollmentToken(ctx, "", "outpost-a", hashHex(tokenA), all); err != nil {
		t.Fatalf("mint a: %v", err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "outpost-b", hashHex(tokenB), all); err != nil {
		t.Fatalf("mint b: %v", err)
	}

	// Seed a component + interface + enabled task placed on outpost-a.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, outpost_id, params) values ('disp-1-icmp', 'icmp', (select id from component where name = 'disp-1'), (select user_id from outpost where name = 'outpost-a'), '{"target":"10.0.0.1"}'::jsonb)`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, spec, enabled) values ('t-icmp', 'poll', (select id from endpoint where name = 'disp-1-icmp'), '{"probe":"icmp"}'::jsonb, true)`); err != nil {
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

	// Outpost A connects with its own credential.
	permErrs := make(chan error, 16)
	ncA, err := nats.Connect(url,
		nats.UserInfo("outpost-a", tokenA),
		nats.CustomInboxPrefix(collection.InboxPrefix("outpost-a")),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, e error) { permErrs <- e }),
	)
	if err != nil {
		t.Fatalf("outpost-a connect: %v", err)
	}
	defer ncA.Close()

	// Positive: outpost A pulls its worklist.
	msg, err := ncA.Request(collection.WorklistSubject("outpost-a"), nil, 3*time.Second)
	if err != nil {
		t.Fatalf("outpost-a worklist request: %v", err)
	}
	var reply collection.WorklistReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		t.Fatalf("decode worklist: %v", err)
	}
	if len(reply.Tasks) != 1 || reply.Tasks[0].ID != "t-icmp" || reply.Tasks[0].Transport != "icmp" {
		t.Fatalf("worklist = %+v, want one t-icmp/icmp task", reply.Tasks)
	}
	if reply.Tasks[0].EndpointID == "" {
		t.Fatalf("worklist task carries no endpoint id; a held session is keyed by it (#816)")
	}
	if reply.ConfigGeneration == 0 {
		t.Fatalf("config_generation = 0, want the interface updated_at epoch")
	}

	// Positive: outpost A heartbeats; the server records last_heartbeat_at.
	publishHeartbeat(t, ncA, "outpost-a")
	waitHeartbeat(t, ctx, gw, "outpost-a", true)

	// Negative isolation: outpost A cannot publish on outpost B's heartbeat subject.
	// The publish is denied (a permissions violation), and outpost B's heartbeat is
	// never recorded.
	if err := ncA.Publish(collection.HeartbeatSubject("outpost-b"), heartbeatBytes(t, "outpost-b")); err != nil {
		t.Fatalf("publish (client-side) outpost-b: %v", err)
	}
	_ = ncA.Flush()
	if !awaitPermissionViolation(permErrs, 3*time.Second) {
		t.Fatalf("expected a permissions-violation error publishing to outpost-b's subject")
	}
	// Give any (impossible) delivery a moment, then assert outpost B stayed silent.
	time.Sleep(300 * time.Millisecond)
	waitHeartbeat(t, ctx, gw, "outpost-b", false)

	// Negative isolation: outpost A cannot pull outpost B's worklist (publish denied).
	if _, err := ncA.Request(collection.WorklistSubject("outpost-b"), nil, 1*time.Second); err == nil {
		t.Fatalf("outpost-a pulling outpost-b worklist: want error, got a reply")
	}

	// Auth negative: a wrong password is rejected at connect.
	bad, err := nats.Connect(url, nats.UserInfo("outpost-a", "wrong-token"), nats.MaxReconnects(0), nats.Timeout(2*time.Second))
	if err == nil {
		bad.Close()
		t.Fatalf("connect with wrong token: want rejection, got a connection")
	}
}

// TestWorklistReplyConfusedDeputy proves the worklist responder refuses to reply
// to any subject outside the requesting outpost's own inbox. The responder answers
// with the server's FULL-PERMISSION internal client, and msg.Reply is
// attacker-controlled, so an enrolled outpost that points the reply at another
// outpost's heartbeat subject would otherwise forge that outpost's liveness (and, once
// a stream exists, redirect to $JS/$SYS). outpost-a requests its own worklist
// subject (allowed by its grant) but sets the reply to outpost-b's heartbeat
// subject; outpost-b's last_heartbeat_at must stay nil, and outpost-a's legitimate
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

	// outpost-a is enrolled and connects; outpost-b is enrolled but never heartbeats
	// and never connects (its liveness can only come from a forge).
	for _, name := range []string{"outpost-a", "outpost-b"} {
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: name}, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	tokenA := "secret-a"
	if _, err := gw.SetEnrollmentToken(ctx, "", "outpost-a", hashHex(tokenA), all); err != nil {
		t.Fatalf("mint a: %v", err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "outpost-b", hashHex("secret-b"), all); err != nil {
		t.Fatalf("mint b: %v", err)
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	url := srv.ClientURL()

	ncA, err := nats.Connect(url,
		nats.UserInfo("outpost-a", tokenA),
		nats.CustomInboxPrefix(collection.InboxPrefix("outpost-a")),
	)
	if err != nil {
		t.Fatalf("outpost-a connect: %v", err)
	}
	defer ncA.Close()

	// The forge: request outpost-a's own worklist subject (permitted) but aim the
	// reply at outpost-b's heartbeat subject. If the responder honored msg.Reply, the
	// internal client would publish outpost-a's worklist to og.v1.heartbeat.outpost-b,
	// the heartbeat sink would consume it, and outpost-b's liveness would be forged.
	if err := ncA.PublishRequest(collection.WorklistSubject("outpost-a"), collection.HeartbeatSubject("outpost-b"), nil); err != nil {
		t.Fatalf("outpost-a forge publish: %v", err)
	}
	_ = ncA.Flush()

	// A legitimate pull round-trips through the same worklist handler, so once its
	// reply returns the forge request has already been fully handled (including any
	// Respond it would emit). The happy path must not regress.
	msg, err := ncA.Request(collection.WorklistSubject("outpost-a"), nil, 3*time.Second)
	if err != nil {
		t.Fatalf("outpost-a legitimate worklist pull: %v", err)
	}
	var reply collection.WorklistReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		t.Fatalf("decode worklist: %v", err)
	}

	// Let the heartbeat hop (the only remaining async step) land, then assert the
	// forge was blocked: outpost-b's last_heartbeat_at is still nil.
	time.Sleep(300 * time.Millisecond)
	waitHeartbeat(t, ctx, gw, "outpost-b", false)
}

func heartbeatBytes(t *testing.T, outpost string) []byte {
	t.Helper()
	b, err := json.Marshal(collection.Heartbeat{Outpost: outpost, At: time.Now().UTC()})
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	return b
}

func publishHeartbeat(t *testing.T, nc *nats.Conn, outpost string) {
	t.Helper()
	if err := nc.Publish(collection.HeartbeatSubject(outpost), heartbeatBytes(t, outpost)); err != nil {
		t.Fatalf("publish heartbeat %s: %v", outpost, err)
	}
	_ = nc.Flush()
}

// waitHeartbeat asserts last_heartbeat_at becomes set (want=true) within a
// window, or stays nil (want=false).
func waitHeartbeat(t *testing.T, ctx context.Context, gw storage.Gateway, outpost string, want bool) {
	t.Helper()
	all := scope.Set{All: true}
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := gw.GetOutpost(ctx, outpost, all)
		if err != nil {
			t.Fatalf("get outpost %s: %v", outpost, err)
		}
		got := n.LastHeartbeatAt != nil
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("outpost %s last_heartbeat_at set = %v, want %v", outpost, got, want)
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
