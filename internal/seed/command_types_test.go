package seed_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSeedCommandTypesShipTheOutletCommands pins #1653's seed side: outlet-off
// is the one shipped power command (disruptive, with its quiet and hold, and
// naming the param that carries its outlet), outlet-on disrupts nothing, no
// other shipped type names an outlet, and both refuse params that name no
// outlet in 1 to 4 at the issue door.
func TestSeedCommandTypesShipTheOutletCommands(t *testing.T) {
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
	types, err := gw.ListCommandTypes(ctx)
	if err != nil {
		t.Fatalf("list command types: %v", err)
	}
	byName := map[string]storage.CommandType{}
	for _, ct := range types {
		byName[ct.Name] = ct
		if ct.Name != "outlet-off" && ct.PowerOutletParam != "" {
			t.Errorf("seeded command type %q names power_outlet_param %q, want only outlet-off to", ct.Name, ct.PowerOutletParam)
		}
	}

	off, ok := byName["outlet-off"]
	if !ok {
		t.Fatal("the seed shipped no outlet-off")
	}
	if !off.Disruptive || off.QuietSeconds <= 0 || off.HoldSeconds <= off.QuietSeconds {
		t.Errorf("outlet-off disruptive=%v quiet=%d hold=%d, want disruptive with a quiet and a longer hold",
			off.Disruptive, off.QuietSeconds, off.HoldSeconds)
	}
	if off.PowerOutletParam != "outlet" {
		t.Errorf("outlet-off power_outlet_param = %q, want outlet", off.PowerOutletParam)
	}
	if off.TargetPropertyType != "" || off.TargetMetricType != "" || off.SettleWindowSeconds != 0 {
		t.Errorf("outlet-off = %+v, want fire-and-forget", off)
	}

	on, ok := byName["outlet-on"]
	if !ok {
		t.Fatal("the seed shipped no outlet-on")
	}
	if on.Disruptive || on.QuietSeconds != 0 || on.HoldSeconds != 0 || on.PowerOutletParam != "" {
		t.Errorf("outlet-on disruptive=%v quiet=%d hold=%d outlet param=%q, want it to disrupt nothing",
			on.Disruptive, on.QuietSeconds, on.HoldSeconds, on.PowerOutletParam)
	}
	if on.TargetPropertyType != "" || on.TargetMetricType != "" || on.SettleWindowSeconds != 0 {
		t.Errorf("outlet-on = %+v, want fire-and-forget", on)
	}

	all := scope.Set{All: true}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "pdu-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	for _, name := range []string{"outlet-off", "outlet-on"} {
		for _, params := range []string{``, `{}`, `{"outlet":0}`, `{"outlet":5}`, `{"outlet":2.5}`, `{"outlet":"two"}`} {
			_, err := gw.IssueCommand(ctx, actor, "component", "pdu-1", name, "", nil, json.RawMessage(params))
			if !errors.Is(err, storage.ErrCommandParamsInvalid) {
				t.Errorf("%s with params %q = %v, want ErrCommandParamsInvalid", name, params, err)
			}
		}
		if _, err := gw.IssueCommand(ctx, actor, "component", "pdu-1", name, "", nil, json.RawMessage(`{"outlet":2}`)); err != nil {
			t.Errorf("%s on outlet 2: %v", name, err)
		}
	}
}
