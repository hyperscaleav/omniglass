package bus_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The server asks a node to run work now and gets the node's answer
// (#1163): a preview's tasks reach only the node named, with their secrets,
// and a node that is not listening is an answer of its own, never a hang. A
// node cannot take another node's runs. Skipped under -short.
func TestRunOnNode(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-a", hashHex("secret-a"), all); err != nil {
		t.Fatal(err)
	}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	task := storage.WorklistTask{ID: "preview-1", Mode: "poll", Transport: "http", EndpointParams: []byte(`{"target":"10.0.0.1:80"}`),
		Spec: []byte(`{"driver":"d","function":"identity","emits":[]}`), Secrets: map[string]map[string]string{"login": {"password": "pw"}}}
	if _, err := srv.RunPreview(ctx, "node-a", []storage.WorklistTask{task}, time.Second); !errors.Is(err, bus.ErrNodeAbsent) {
		t.Fatalf("with no node listening: err = %v, want ErrNodeAbsent", err)
	}

	permErrs := make(chan error, 16)
	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo("node-a", "secret-a"),
		nats.CustomInboxPrefix(collection.InboxPrefix("node-a")),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, e error) { permErrs <- e }))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	var got collection.RunRequest
	if _, err := nc.Subscribe(collection.RunSubject("node-a"), func(m *nats.Msg) {
		_ = json.Unmarshal(m.Data, &got)
		v := 42.0
		b, _ := json.Marshal(collection.RunReply{Results: []collection.RunResult{{TaskID: got.Tasks[0].ID, Function: "identity",
			Samples: []collection.RunSample{{Name: "uptime", Value: &v}}}}})
		_ = m.Respond(b)
	}); err != nil {
		t.Fatal(err)
	}
	_ = nc.Flush()

	reply, err := srv.RunPreview(ctx, "node-a", []storage.WorklistTask{task}, 5*time.Second)
	if err != nil {
		t.Fatalf("run preview: %v", err)
	}
	if got.Kind != collection.RunPreview || len(got.Tasks) != 1 || got.Tasks[0].Secrets["login"]["password"] != "pw" || got.TimeoutMs != 5000 {
		t.Fatalf("the node was asked %+v", got)
	}
	if len(reply.Results) != 1 || *reply.Results[0].Samples[0].Value != 42 {
		t.Fatalf("reply = %+v", reply)
	}

	if _, err := srv.RunTaskNow(ctx, "node-a", "no-such-task", time.Second); !errors.Is(err, bus.ErrTaskNotOnWorklist) {
		t.Fatalf("a task the node does not hold = %v, want ErrTaskNotOnWorklist", err)
	}

	if _, err := nc.Subscribe(collection.RunSubject("node-b"), func(*nats.Msg) {}); err != nil {
		t.Fatal(err)
	}
	_ = nc.Flush()
	if !awaitPermissionViolation(permErrs, 3*time.Second) {
		t.Fatal("node-a subscribed to node-b's runs without a permissions violation")
	}
}
