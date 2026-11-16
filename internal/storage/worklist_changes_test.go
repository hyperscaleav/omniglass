package storage_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// A write that changes a node's worklist announces it on commit (#1945): the
// node it names arrives on WorklistChanges, so the bus can nudge that node to
// pull now instead of on its next scheduled pull. Skipped under -short.

// nextChange waits for one announced node name, failing on a quiet channel.
func nextChange(t *testing.T, ch <-chan string, what string) string {
	t.Helper()
	select {
	case n, ok := <-ch:
		if !ok {
			t.Fatalf("%s: the change channel closed", what)
		}
		return n
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no worklist change announced", what)
	}
	return ""
}

// expectChanges collects the names announced for one write and checks they are
// exactly want, in any order: a write announces each node it touched once.
func expectChanges(t *testing.T, ch <-chan string, what string, want ...string) {
	t.Helper()
	got := map[string]bool{}
	for range want {
		got[nextChange(t, ch, what)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Fatalf("%s: announced %v, want %v", what, got, want)
		}
	}
	select {
	case extra := <-ch:
		t.Fatalf("%s: an extra announcement %q beyond %v", what, extra, want)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWorklistChangesAnnounceTheNodesAWriteTouches(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	wrong := strings.Replace(lineProtoSpec, `"^INPUT (\\S+)$"`, `"^INPUT (\\d+)$"`, 1)
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "panel-proto", Label: "Panel", Version: "1.0.0", Spec: []byte(wrong)}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-n"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"edge-a", "edge-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
	}

	changes, err := gw.WorklistChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}

	comp, drv, a, b := "panel-n", "panel-proto", "edge-a", "edge-b"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &a,
		Inputs: map[string]string{"host": "10.20.4.70"}, SecretRead: all, SecretBind: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatal(err)
	}
	expectChanges(t, changes, "create", a)

	if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"host": "10.20.4.71"}}, all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, changes, "an address edit", a)

	if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Node: &b}, all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, changes, "a move between nodes", a, b)

	if _, err := gw.RetryEndpoint(ctx, "", ep.ID, storage.RetryPin{}, all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, changes, "a session retry", b)

	if _, err := gw.UpdateDriver(ctx, "", drv, storage.DriverPatch{Spec: []byte(lineProtoSpec)}); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, changes, "a driver spec edit", b)

	// A bare NOTIFY names no node: the listener skips it and stays up.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `notify og_worklist_changed`); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"host": "10.20.4.72"}}, all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, changes, "an edit after a bare notify", b)

	// A write that rolls back announces nothing: the notify rides the commit.
	if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"nope": "x"}}, all, all); err == nil {
		t.Fatal("an undeclared input was accepted")
	}
	select {
	case n := <-changes:
		t.Fatalf("a refused edit announced %q", n)
	case <-time.After(150 * time.Millisecond):
	}

	// The listener survives its connection dying: a later change still arrives.
	var killed int
	if err := conn.QueryRow(ctx, `
		select count(pg_terminate_backend(pid)) from pg_stat_activity
		where application_name = $1 and datname = current_database()`, storage.WorklistListenApp).Scan(&killed); err != nil {
		t.Fatal(err)
	}
	if killed == 0 {
		t.Fatal("no listener connection to terminate")
	}
	deadline := time.Now().Add(10 * time.Second)
	for i := 100; ; i++ {
		if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"host": "10.20.4." + strconv.Itoa(i)}}, all, all); err != nil {
			t.Fatal(err)
		}
		select {
		case n := <-changes:
			if n != b {
				t.Fatalf("after a reconnect: announced %q, want %q", n, b)
			}
		case <-time.After(300 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("the listener never came back after its connection died")
			}
			continue
		}
		break
	}

	if err := gw.DeleteEndpoint(ctx, "", ep.ID, all, all); err != nil {
		t.Fatal(err)
	}
	// A late announcement from the edit loop above names the same node, so
	// either way the next one names it.
	got := nextChange(t, changes, "delete")
	if got != b {
		t.Fatalf("delete: announced %q, want %q", got, b)
	}

	cancel()
	for range changes {
	}
}
