package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// byName indexes a resolved set so a test can assert one property without
// depending on the result order beyond the resolver's own ordering guarantee.
func byName(props []storage.EffectiveProperty) map[string]storage.EffectiveProperty {
	m := make(map[string]storage.EffectiveProperty, len(props))
	for _, p := range props {
		m[p.PropertyName] = p
	}
	return m
}

// TestEffectiveProperties proves the fold: a component's declared properties are
// its product contract resolved against its own set values (default when unset,
// override when set), plus ad-hoc properties set directly on the component that the
// contract does not declare. A productless component has only the ad-hoc set.
func TestEffectiveProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	// A custom product with a two-property contract: one with a default, one
	// required with no default.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		ID: "acme-panel", DisplayName: "Acme Panel", Kind: "device",
	}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := gw.SetProductProperty(ctx, "", "acme-panel", storage.ProductPropertySpec{
		PropertyName: "firmware_version", DefaultValue: json.RawMessage(`"1.0.0"`),
	}); err != nil {
		t.Fatalf("set contract firmware_version: %v", err)
	}
	if _, err := gw.SetProductProperty(ctx, "", "acme-panel", storage.ProductPropertySpec{
		PropertyName: "serial_number", Required: true,
	}); err != nil {
		t.Fatalf("set contract serial_number: %v", err)
	}

	product := "acme-panel"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-1", ProductName: &product,
	}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	// Unset: the contract default resolves, nothing is marked set.
	got, err := gw.EffectiveProperties(ctx, "panel-1", all)
	if err != nil {
		t.Fatalf("effective properties: %v", err)
	}
	idx := byName(got)
	if len(got) != 2 {
		t.Fatalf("want the 2 contract properties, got %d: %+v", len(got), got)
	}
	fw := idx["firmware_version"]
	if string(fw.Value) != `"1.0.0"` || fw.IsSet || !fw.FromContract {
		t.Fatalf("firmware_version unset: want default 1.0.0, is_set=false, from_contract=true, got %+v", fw)
	}
	sn := idx["serial_number"]
	if !sn.Required || sn.IsSet || len(sn.Value) != 0 {
		t.Fatalf("serial_number unset: want required, unset, no value, got %+v", sn)
	}

	// Override a contract property: the set value wins and is marked.
	if _, err := gw.SetPropertyValue(ctx, "", "component", "panel-1", "firmware_version", "", json.RawMessage(`"2.5.1"`), all); err != nil {
		t.Fatalf("set firmware override: %v", err)
	}
	idx = byName(mustResolve(t, gw, "panel-1", all))
	fw = idx["firmware_version"]
	if string(fw.Value) != `"2.5.1"` || !fw.IsSet || fw.ValueID == "" || string(fw.DefaultValue) != `"1.0.0"` {
		t.Fatalf("firmware_version override: want 2.5.1 set over default 1.0.0, got %+v", fw)
	}

	// A repeat set of the same series updates in place (idempotent save), it does
	// not conflict or add a second row.
	if _, err := gw.SetPropertyValue(ctx, "", "component", "panel-1", "firmware_version", "", json.RawMessage(`"2.5.2"`), all); err != nil {
		t.Fatalf("re-set firmware override: %v", err)
	}
	idx = byName(mustResolve(t, gw, "panel-1", all))
	if string(idx["firmware_version"].Value) != `"2.5.2"` || len(mustResolve(t, gw, "panel-1", all)) != 2 {
		t.Fatalf("re-set: want a single updated row at 2.5.2, got %+v", idx["firmware_version"])
	}

	// An ad-hoc property the contract does not declare still resolves, flagged
	// off-contract.
	if _, err := gw.SetPropertyValue(ctx, "", "component", "panel-1", "mac_address", "", json.RawMessage(`"aa:bb:cc:dd:ee:ff"`), all); err != nil {
		t.Fatalf("set ad-hoc mac_address: %v", err)
	}
	idx = byName(mustResolve(t, gw, "panel-1", all))
	mac := idx["mac_address"]
	if !mac.IsSet || mac.FromContract || string(mac.Value) != `"aa:bb:cc:dd:ee:ff"` {
		t.Fatalf("ad-hoc mac_address: want set, from_contract=false, got %+v", mac)
	}
	if len(idx) != 3 {
		t.Fatalf("want 2 contract + 1 ad-hoc, got %d", len(idx))
	}

	// Clearing an override falls back to the contract default; clearing again is an
	// explicit miss.
	if err := gw.ClearPropertyValue(ctx, "", "component", "panel-1", "firmware_version", "", all); err != nil {
		t.Fatalf("clear firmware override: %v", err)
	}
	idx = byName(mustResolve(t, gw, "panel-1", all))
	if fw = idx["firmware_version"]; string(fw.Value) != `"1.0.0"` || fw.IsSet {
		t.Fatalf("after clear: want the default back and is_set=false, got %+v", fw)
	}
	if err := gw.ClearPropertyValue(ctx, "", "component", "panel-1", "firmware_version", "", all); !errors.Is(err, storage.ErrPropertyValueNotFound) {
		t.Fatalf("clear an unset property: want ErrPropertyValueNotFound, got %v", err)
	}

	// A productless component has no contract, only what it sets directly.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "loose-1"}, all); err != nil {
		t.Fatalf("create productless component: %v", err)
	}
	if _, err := gw.SetPropertyValue(ctx, "", "component", "loose-1", "serial_number", "", json.RawMessage(`"SN-9"`), all); err != nil {
		t.Fatalf("set on productless: %v", err)
	}
	loose := mustResolve(t, gw, "loose-1", all)
	if len(loose) != 1 || loose[0].PropertyName != "serial_number" || loose[0].FromContract {
		t.Fatalf("productless component: want a single ad-hoc serial_number, got %+v", loose)
	}

	// A value naming a property outside the catalog trips the property FK.
	if _, err := gw.SetPropertyValue(ctx, "", "component", "panel-1", "not.a.property", "", json.RawMessage(`1`), all); !errors.Is(err, storage.ErrPropertyRefNotFound) {
		t.Fatalf("unknown property: want ErrPropertyRefNotFound, got %v", err)
	}
	// An unknown component is the non-disclosing not-found, never an opaque FK error.
	if _, err := gw.SetPropertyValue(ctx, "", "component", "ghost", "serial_number", "", json.RawMessage(`"x"`), all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Fatalf("unknown component: want ErrComponentNotFound, got %v", err)
	}
}

func mustResolve(t *testing.T, gw *storage.PG, name string, s scope.Set) []storage.EffectiveProperty {
	t.Helper()
	got, err := gw.EffectiveProperties(context.Background(), name, s)
	if err != nil {
		t.Fatalf("effective properties %s: %v", name, err)
	}
	return got
}
