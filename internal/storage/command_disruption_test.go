package storage_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// rebootProtoSpec is lineProtoSpec with a reboot binding beside set-input.
var rebootProtoSpec = strings.Replace(lineProtoSpec,
	`{"command_type": "set-input",`,
	`{"command_type": "reboot", "request": {"line": "REBOOT"}},
		{"command_type": "set-input",`, 1)

// TestDisruptiveDeliveryNamesItsHoldSet pins the storage side of #1651: a
// disruptive command's delivery says so and names the endpoints whose gates
// it closes, which is every endpoint of the component placed on the
// delivering node (the device behind them all is the one going down) and
// none placed on another node (that node's gates are its own). A
// non-disruptive delivery names none.
func TestDisruptiveDeliveryNamesItsHoldSet(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-4"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	for _, n := range []string{"edge-4", "edge-far"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatalf("create node %s: %v", n, err)
		}
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "reboot-proto", Label: "Reboot Proto", Version: "1.0.0", Spec: []byte(rebootProtoSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv, near, far := "codec-4", "reboot-proto", "edge-4", "edge-far"
	actuating, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &near, Inputs: map[string]string{"host": "10.20.4.60"},
	}, all)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	beside, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "icmp", Component: &comp, Node: &near}, all)
	if err != nil {
		t.Fatalf("create icmp endpoint: %v", err)
	}
	elsewhere, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "http", Component: &comp, Node: &far}, all)
	if err != nil {
		t.Fatalf("create far endpoint: %v", err)
	}

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
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	conn.Close(ctx)

	reboot, err := gw.IssueCommand(ctx, actor, "component", comp, "reboot", "", nil, nil)
	if err != nil {
		t.Fatalf("issue reboot: %v", err)
	}
	if _, err := gw.IssueCommand(ctx, actor, "component", comp, "set-input", "", json.RawMessage(`"hdmi-1"`), nil); err != nil {
		t.Fatalf("issue set-input: %v", err)
	}
	got, err := gw.PendingNodeCommands(ctx, near)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("queue = %+v, want the reboot and the set-input", got)
	}
	for _, d := range got {
		if d.EndpointID != actuating.ID {
			t.Errorf("%s dispatched to %s, want the binding endpoint %s", d.CommandType, d.EndpointID, actuating.ID)
		}
		switch d.CommandType {
		case "reboot":
			if d.ID != reboot.ID || !d.Disruptive || d.QuietSeconds != 15 || d.HoldSeconds != 300 {
				t.Errorf("reboot delivery = %+v, want disruptive with quiet 15 and hold 300", d)
			}
			want := []string{actuating.ID, beside.ID}
			slices.Sort(want)
			if !slices.Equal(d.HoldEndpoints, want) {
				t.Errorf("reboot hold set = %v, want both endpoints on %s %v and never %s on %s", d.HoldEndpoints, near, want, elsewhere.ID, far)
			}
		case "set-input":
			if d.Disruptive || d.QuietSeconds != 0 || d.HoldSeconds != 0 || len(d.HoldEndpoints) != 0 {
				t.Errorf("set-input delivery = %+v, want not disruptive with an empty hold set", d)
			}
		default:
			t.Errorf("unexpected delivery %+v", d)
		}
	}
}
