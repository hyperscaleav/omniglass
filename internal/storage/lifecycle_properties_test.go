package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLifecycleProperties proves the six official lifecycle fields (#2366): the
// four dates are seeded property types and the cost and expected life are seeded
// metric types; a product's default is a component's effective value, a
// component's own value wins, and a malformed date is refused.
func TestLifecycleProperties(t *testing.T) {
	t.Parallel()
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, name := range []string{"install-date", "warranty-end", "end-of-support", "end-of-life"} {
		if _, err := gw.GetPropertyType(ctx, name); err != nil {
			t.Fatalf("property type %s after boot: %v", name, err)
		}
	}
	for _, c := range []struct{ name, unit string }{{"replacement-cost", "USD"}, {"expected-life-years", "years"}} {
		mt, err := gw.GetMetricType(ctx, c.name)
		if err != nil {
			t.Fatalf("metric type %s after boot: %v", c.name, err)
		}
		if mt.Unit == nil || *mt.Unit != c.unit {
			t.Fatalf("metric type %s unit = %v, want %s", c.name, mt.Unit, c.unit)
		}
	}

	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "lc-widget", Label: "LC Widget", Kind: "device"}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := gw.SetProductProperty(ctx, "", "lc-widget", storage.ProductPropertySpec{
		PropertyTypeName: "end-of-life", DefaultValue: json.RawMessage(`"2030-06-30"`),
	}); err != nil {
		t.Fatalf("product default: %v", err)
	}
	p := "lc-widget"
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "lc-a", ProductName: &p}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "lc-b", ProductName: &p}, all)

	if _, err := gw.SetProperty(ctx, "", "component", "lc-b", "end-of-life", "", json.RawMessage(`"2028-01-15"`), all, all); err != nil {
		t.Fatalf("component override: %v", err)
	}
	for comp, want := range map[string]string{"lc-a": `"2030-06-30"`, "lc-b": `"2028-01-15"`} {
		effs, err := gw.EffectiveProperties(ctx, "component", comp, all)
		if err != nil {
			t.Fatalf("effective %s: %v", comp, err)
		}
		got := byName(effs)["end-of-life"]
		if string(got.Value) != want {
			t.Fatalf("%s end-of-life = %s, want %s", comp, got.Value, want)
		}
	}

	for _, bad := range []string{`"next tuesday"`, `"2030-02-30"`, `"2030-13-45"`} {
		if _, err := gw.SetProperty(ctx, "", "component", "lc-a", "warranty-end", "", json.RawMessage(bad), all, all); !errors.Is(err, storage.ErrPropertyValueInvalid) {
			t.Fatalf("component warranty-end %s: err = %v, want ErrPropertyValueInvalid", bad, err)
		}
	}

	// A contract default is held to the same type as a declared value: every
	// component of the product would otherwise read the bad default as its own.
	if _, err := gw.SetProductProperty(ctx, "", "lc-widget", storage.ProductPropertySpec{
		PropertyTypeName: "end-of-life", DefaultValue: json.RawMessage(`"soon"`),
	}); !errors.Is(err, storage.ErrPropertyValueInvalid) {
		t.Fatalf("malformed product default: err = %v, want ErrPropertyValueInvalid", err)
	}
	if _, err := gw.SetProductProperty(ctx, "", "lc-widget", storage.ProductPropertySpec{PropertyTypeName: "install-date"}); err != nil {
		t.Fatalf("a contract line with no default: %v", err)
	}
}
