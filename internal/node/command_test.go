package node

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// pruneCommandMemory drops outcomes past the redelivery window so the map does
// not grow for the life of a long-running node, while keeping entries young
// enough to still be redelivered (idempotence must survive a redelivery).
func TestPruneCommandMemory(t *testing.T) {
	t.Parallel()
	now := time.Now()
	executed := map[int64]commandOutcome{
		1: {outcome: "", at: now.Add(-commandMemoryTTL - time.Minute)}, // stale
		2: {outcome: "boom", at: now.Add(-time.Minute)},                // fresh
		3: {outcome: "", at: now.Add(-commandMemoryTTL + time.Minute)}, // just inside the window
	}
	pruneCommandMemory(executed, now)
	if _, ok := executed[1]; ok {
		t.Fatalf("stale entry 1 survived the prune")
	}
	if _, ok := executed[2]; !ok {
		t.Fatalf("fresh entry 2 was pruned")
	}
	if _, ok := executed[3]; !ok {
		t.Fatalf("in-window entry 3 was pruned (still redeliverable)")
	}
}

// A node whose command pull fails says so once, at WARN with its name and
// the error, not on every tick it keeps failing, and says once at INFO when
// a pull succeeds again: a node that cannot reach its command queue is
// visible in its own log (#1630).
func TestACommandPullFailureIsLoggedOnTheTransition(t *testing.T) {
	// Serial: it swaps the slog default to count the pull's log lines.
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	t.Cleanup(ns.Shutdown)
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats-server not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	ctx := context.Background()
	state := newCommandState()
	pull := func() []slog.Record {
		return captureLogs(t, func() { runCommands(ctx, nc, "edge-7", nil, nil, state) })
	}
	at := func(recs []slog.Record, level slog.Level) []slog.Record {
		var out []slog.Record
		for _, r := range recs {
			if r.Level == level {
				out = append(out, r)
			}
		}
		return out
	}
	attr := func(r slog.Record, key string) string {
		var v string
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == key {
				v = a.Value.String()
			}
			return true
		})
		return v
	}

	// No command responder: the first failure is one WARN line.
	first := at(pull(), slog.LevelWarn)
	if len(first) != 1 || attr(first[0], "facility") != "command" || attr(first[0], "node") != "edge-7" || attr(first[0], "error") == "" {
		t.Fatalf("first failed pull logged %d WARN lines (%v), want one with facility=command, the node and the error", len(first), first)
	}
	// Still failing: nothing new.
	if again := pull(); len(at(again, slog.LevelWarn)) != 0 || len(at(again, slog.LevelInfo)) != 0 {
		t.Fatalf("second failed pull logged %v, want nothing", again)
	}

	// The queue answers again: one INFO line, then nothing on the next.
	sub, err := nc.Subscribe(collection.CommandSubject("edge-7"), func(m *nats.Msg) { _ = m.Respond([]byte(`{}`)) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	recovered := pull()
	if info := at(recovered, slog.LevelInfo); len(info) != 1 || attr(info[0], "node") != "edge-7" || len(at(recovered, slog.LevelWarn)) != 0 {
		t.Fatalf("recovered pull logged %v, want one INFO line naming the node", recovered)
	}
	if steady := pull(); len(steady) != 0 {
		t.Fatalf("steady pull logged %v, want nothing", steady)
	}
}
