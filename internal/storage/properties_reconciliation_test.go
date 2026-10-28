package storage_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// #1297: one properties read carries every property the owner has, declared,
// calculated, observed or told, and each row its reconciliation where one
// applies. A key that is only observed now appears (want null, no drift); a
// declared key a live observer disagrees with drifts, naming the instance; a
// key a command set shows told even when nothing declares or observes it; a
// key whose only observer is a retired endpoint is not a property the owner
// has; and a place's rows carry the block with nothing observed (ADR-0422).
func TestPropertiesCarryEveryKeyAndItsReconciliation(t *testing.T) {
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
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into endpoint (name, transport, component, node_name, params) values
		('telnet', 'telnet', $1::uuid, (select principal_id from node where name = 'edge'), '{"target":"10.0.0.1:23"}'::jsonb)`, comp.ID)
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	land := func(w storage.KeyWriter, key, instance, value string, at time.Duration) {
		t.Helper()
		if err := gw.InsertPropertySamples(ctx, w, []storage.PropertySampleWrite{{
			OwnerKind: "component", OwnerID: comp.ID, Key: key, Instance: instance, Value: value, Source: "test", TS: base.Add(at),
		}}); err != nil {
			t.Fatalf("land %s=%s on %s: %v", key, value, instance, err)
		}
	}

	// Declared and observed disagreeing: drift, naming the observer.
	if _, err := gw.SetProperty(ctx, "", "component", "panel-1", "firmware-version", "", json.RawMessage(`"1.0.0"`), all, all); err != nil {
		t.Fatalf("declare firmware-version: %v", err)
	}
	land(storage.NodeWriter(comp.ID, "panel-ip"), "firmware-version", "panel-ip", "2.0.0", time.Second)
	// Observed only: nothing declares serial-number.
	land(storage.NodeWriter(comp.ID, "panel-ip"), "serial-number", "panel-ip", "SN-42", 2*time.Second)
	// Observed only through an endpoint since deleted: a retired observer, so
	// the owner has no live value of mac-address and no row for it.
	land(storage.NodeWriter(comp.ID, "telnet"), "mac-address", "telnet", "00:11:22:33:44:55", 3*time.Second)
	if err := gw.DeleteEndpoint(ctx, "", comp.ID+"/telnet", all, all); err != nil {
		t.Fatalf("delete the telnet endpoint: %v", err)
	}

	props := byName(mustResolveOwner(t, gw, "component", "panel-1", all))

	fw, ok := props["firmware-version"]
	if !ok || fw.Reconciliation == nil {
		t.Fatalf("firmware-version: want a declared row with its reconciliation, got %+v", fw)
	}
	if r := fw.Reconciliation; string(r.Want) != `"1.0.0"` || string(r.Is) != `"2.0.0"` || r.IsInstance != "panel-ip" || !r.Drift || r.Told != nil {
		t.Fatalf("firmware-version: want want=1.0.0 is=2.0.0 from panel-ip, drift, no told; got %+v", r)
	}
	if string(fw.Value) != `"1.0.0"` || !fw.IsSet {
		t.Fatalf("firmware-version keeps its declared resolution: want value 1.0.0 set, got %+v", fw)
	}

	sn, ok := props["serial-number"]
	if !ok {
		t.Fatalf("serial-number, observed only, is missing from the properties: %+v", props)
	}
	if sn.IsSet || sn.FromContract || sn.Value != nil || sn.DataType == "" || sn.PropertyTypeID == "" {
		t.Fatalf("serial-number: want an undeclared catalog row with no declared value, got %+v", sn)
	}
	if r := sn.Reconciliation; r == nil || r.Want != nil || string(r.Is) != `"SN-42"` || r.IsInstance != "panel-ip" || r.Drift {
		t.Fatalf("serial-number: want want=null is=SN-42 from panel-ip, no drift; got %+v", r)
	}

	if mac, ok := props["mac-address"]; ok {
		t.Fatalf("mac-address, observed only by a retired endpoint, is listed: %+v", mac)
	}

	// Told only: a command sets video-input on a component that neither
	// declares nor observes it.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "switcher-1"}, all, all, all, all); err != nil {
		t.Fatalf("create switcher: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	if _, err := gw.IssueCommand(ctx, actor, "component", "switcher-1", "set-input", "", json.RawMessage(`"dp1"`), nil); err != nil {
		t.Fatalf("issue set-input: %v", err)
	}
	sw := byName(mustResolveOwner(t, gw, "component", "switcher-1", all))
	vi, ok := sw["video-input"]
	if !ok {
		t.Fatalf("video-input, told only, is missing from the properties: %+v", sw)
	}
	if r := vi.Reconciliation; r == nil || string(r.Told) != `"dp1"` || r.Want != nil || r.Is != nil || r.Drift {
		t.Fatalf("video-input: want told=dp1 and nothing declared or observed, got %+v", r)
	}

	// A place: its rows carry the block, never an observed value.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-campus", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create campus: %v", err)
	}
	if _, err := gw.SetProperty(ctx, "", "location", "hq-campus", "serial-number", "", json.RawMessage(`"L-1"`), all, all); err != nil {
		t.Fatalf("set on location: %v", err)
	}
	loc := byName(mustResolveOwner(t, gw, "location", "hq-campus", all))
	if r := loc["serial-number"].Reconciliation; r == nil || string(r.Want) != `"L-1"` || r.Is != nil || r.Told != nil || r.Drift {
		t.Fatalf("a place's row: want want=L-1 and nothing observed or told, got %+v", r)
	}

	// An out-of-scope owner is still the non-disclosing not-found.
	if _, err := gw.EffectiveProperties(ctx, "component", "panel-1", scope.Set{}); err == nil {
		t.Fatal("properties of an out-of-scope component: want not-found, got nil")
	}
}

// reconOf reads one key's reconciliation off the owner's properties read, the
// one read that carries it (#1297), failing when the key has no row.
func reconOf(t *testing.T, gw *storage.PG, kind, owner, key string) storage.PropertyReconciliation {
	t.Helper()
	for _, p := range mustResolveOwner(t, gw, kind, owner, scope.Set{All: true}) {
		if p.PropertyTypeName == key {
			if p.Reconciliation == nil {
				t.Fatalf("%s on %s carries no reconciliation: %+v", key, owner, p)
			}
			return *p.Reconciliation
		}
	}
	t.Fatalf("%s is not among %s's properties", key, owner)
	return storage.PropertyReconciliation{}
}
