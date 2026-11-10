package node

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The node answers a tally request on its own subject with what it counted,
// named for itself (#1639).
func TestSubscribeTallyAnswersWithTheNodesCounts(t *testing.T) {
	t.Parallel()
	nc := startTallyBus(t)
	tally := collection.NewRequestTally()
	tally.Begin(collection.TallyHTTP).End(collection.TallyAnswered)
	tally.BatchAcknowledged()
	sub, err := subscribeTally(nc, "edge", tally)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	msg, err := nc.Request(collection.RequestTallySubject("edge"), nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var snap collection.RequestTallySnapshot
	if err := json.Unmarshal(msg.Data, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Node != "edge" || snap.At.IsZero() || snap.BatchesAcknowledged != 1 {
		t.Fatalf("reply = %+v", snap)
	}
	if len(snap.Transports) != 1 || snap.Transports[0].Transport != "http" || snap.Transports[0].Answered != 1 {
		t.Fatalf("transports = %+v, want one answered http request", snap.Transports)
	}
}

// startTallyBus is a bare embedded nats server on a random port and a client.
func startTallyBus(t *testing.T) *nats.Conn {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("the nats server never came up")
	}
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}
