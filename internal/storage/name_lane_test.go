package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The one rule for which catalog a bare name resolves on (#1307): a name one
// catalog holds resolves there; a name an official row holds in one catalog and
// exactly one operator's row holds in another resolves to the operator's (private
// shadows official); any other name held twice resolves to nothing. A reserved
// key (the node's or the platform's own) is never shadowed.
func TestResolveNameLane(t *testing.T) {
	op := func(lane string) storage.NameHolder { return storage.NameHolder{Lane: lane} }
	off := func(lane string) storage.NameHolder { return storage.NameHolder{Lane: lane, Official: true} }
	cases := []struct {
		what         string
		name         string
		holders      []storage.NameHolder
		lane         string
		shadowed, ok bool
	}{
		{"held nowhere", "x", nil, "", false, false},
		{"held once, official", "x", []storage.NameHolder{off("metric")}, "metric", false, true},
		{"held once, an operator's", "x", []storage.NameHolder{op("event")}, "event", false, true},
		{"official metric, operator property", "x", []storage.NameHolder{off("metric"), op("property")}, "property", true, true},
		{"official property, operator metric", "x", []storage.NameHolder{op("metric"), off("property")}, "metric", true, true},
		{"two official rows over one operator's", "x", []storage.NameHolder{off("metric"), off("event"), op("property")}, "property", true, true},
		{"two official rows", "x", []storage.NameHolder{off("metric"), off("property")}, "", false, false},
		{"two operator rows", "x", []storage.NameHolder{op("metric"), op("property")}, "", false, false},
		{"two operator rows over an official one", "x", []storage.NameHolder{op("metric"), op("property"), off("event")}, "", false, false},
		{"a reserved key is never shadowed", "icmp-reachable", []storage.NameHolder{off("metric"), op("property")}, "", false, false},
		{"a reserved key held once resolves", "icmp-reachable", []storage.NameHolder{off("metric")}, "metric", false, true},
	}
	for _, c := range cases {
		lane, shadowed, ok := storage.ResolveNameLane(c.name, c.holders)
		if lane != c.lane || shadowed != c.shadowed || ok != c.ok {
			t.Errorf("%s: ResolveNameLane = %q shadowed %v ok %v, want %q %v %v", c.what, lane, shadowed, ok, c.lane, c.shadowed, c.ok)
		}
	}
}

// Every reader of a bare name agrees with the registry on which catalog a
// shadowed name is in, or a rule, a calc rule or a shipped driver would read the
// platform's row while ingest writes the operator's. An operator's row of the
// name first, then the release that ships it, in each direction.
func TestEveryNameResolverAgreesWithTheShadow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)

	// shadow-prop: the operator's property, then an official metric.
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "shadow-prop", DataType: "string"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.UpsertMetricType(ctx, storage.MetricType{Name: "shadow-prop", DataType: "float", Official: true}); err != nil {
		t.Fatal(err)
	}
	// shadow-metric: the operator's metric, then an official property.
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "shadow-metric", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.UpsertPropertyType(ctx, storage.PropertyType{Name: "shadow-metric", DataType: "string", Official: true}); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]rules.Lane{"shadow-prop": rules.LaneProperty, "shadow-metric": rules.LaneMetric} {
		if lane, _, err := gw.SampleKeyType(ctx, name); err != nil || lane != want {
			t.Errorf("an event rule's sample key %q reads lane %q (%v), want the operator's %q", name, lane, err, want)
		}
		if k, err := gw.ResolveSampleKey(ctx, name); err != nil || k.Lane != want {
			t.Errorf("a calc rule's key %q resolves to lane %q (%v), want the operator's %q", name, k.Lane, err, want)
		}
	}

	// A driver's emit is validated against the operator's lane: the catalog a
	// spec is checked against names the lane the registry resolves to.
	lanes, err := gw.DriverCatalogLanes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"shadow-prop": "property", "shadow-metric": "metric"} {
		if lanes[name] != want {
			t.Errorf("the driver catalog puts %q on lane %q, want the operator's %q", name, lanes[name], want)
		}
	}
}
