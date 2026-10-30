package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The one rule for which row a bare name resolves to (#1307, #1432): the search
// path. A name the operator's local rows hold resolves there, in whichever of
// the three catalogs; otherwise to the og row; one holder per tier, or nothing
// (a collision). A reserved key (the node's or the platform's own) is never
// shadowed, and a namespace other than local and og never answers a bare name.
func TestResolveNameLane(t *testing.T) {
	op := func(lane string) storage.NameHolder { return storage.NameHolder{Lane: lane, Namespace: "local"} }
	off := func(lane string) storage.NameHolder { return storage.NameHolder{Lane: lane, Namespace: "og"} }
	pub := func(lane string) storage.NameHolder { return storage.NameHolder{Lane: lane, Namespace: "acme"} }
	cases := []struct {
		what         string
		name         string
		holders      []storage.NameHolder
		want         storage.NameHolder
		shadowed, ok bool
	}{
		{"held nowhere", "x", nil, storage.NameHolder{}, false, false},
		{"held once, og", "x", []storage.NameHolder{off("metric")}, off("metric"), false, true},
		{"held once, an operator's", "x", []storage.NameHolder{op("event")}, op("event"), false, true},
		{"og metric, local metric", "x", []storage.NameHolder{off("metric"), op("metric")}, op("metric"), true, true},
		{"og metric, local property", "x", []storage.NameHolder{off("metric"), op("property")}, op("property"), true, true},
		{"og property, local metric", "x", []storage.NameHolder{op("metric"), off("property")}, op("metric"), true, true},
		{"two og rows over one local", "x", []storage.NameHolder{off("metric"), off("event"), op("property")}, op("property"), true, true},
		{"two og rows", "x", []storage.NameHolder{off("metric"), off("property")}, storage.NameHolder{}, false, false},
		{"two local rows", "x", []storage.NameHolder{op("metric"), op("property")}, storage.NameHolder{}, false, false},
		{"two local rows over an og one", "x", []storage.NameHolder{op("metric"), op("property"), off("event")}, storage.NameHolder{}, false, false},
		{"a publisher's row never answers a bare name", "x", []storage.NameHolder{pub("metric")}, storage.NameHolder{}, false, false},
		{"a publisher's row beside og", "x", []storage.NameHolder{pub("metric"), off("property")}, off("property"), false, true},
		{"a reserved key is never shadowed", "icmp-reachable", []storage.NameHolder{off("metric"), op("property")}, storage.NameHolder{}, false, false},
		{"a reserved key held once resolves", "icmp-reachable", []storage.NameHolder{off("metric")}, off("metric"), false, true},
	}
	for _, c := range cases {
		got, shadowed, ok := storage.ResolveNameLane(c.name, c.holders)
		if got != c.want || shadowed != c.shadowed || ok != c.ok {
			t.Errorf("%s: ResolveNameLane = %+v shadowed %v ok %v, want %+v %v %v", c.what, got, shadowed, ok, c.want, c.shadowed, c.ok)
		}
	}
}

// A qualified name resolves in its own namespace only, whatever the others
// hold: how the seed resolves a shipped row's references inside og.
func TestResolveNameIn(t *testing.T) {
	hs := []storage.NameHolder{{Lane: "metric", Namespace: "og"}, {Lane: "property", Namespace: "local"}}
	if got, ok := storage.ResolveNameIn("og", hs); !ok || got.Lane != "metric" {
		t.Errorf("og: %+v %v, want the og metric", got, ok)
	}
	if got, ok := storage.ResolveNameIn("acme", hs); ok {
		t.Errorf("acme: %+v, want nothing", got)
	}
	two := append(hs, storage.NameHolder{Lane: "event", Namespace: "og"})
	if got, ok := storage.ResolveNameIn("og", two); ok {
		t.Errorf("two og holders: %+v, want a collision", got)
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

// A role refuses ambiguity rather than shadow (#318 ruling): a name both local
// and og hold reaches neither, so its holders lose the role instead of gaining
// the operator's; a role in another namespace is never reached by a bare name.
func TestRolesInEffectFailClosedOnAnAmbiguousName(t *testing.T) {
	got := storage.RolesInEffect([]storage.Role{
		{Name: "admin", Namespace: "og"},
		{Name: "admin", Namespace: "local"},
		{Name: "viewer", Namespace: "og"},
		{Name: "noc", Namespace: "local"},
		{Name: "noc-lead", Namespace: "acme"},
	})
	var names []string
	for _, r := range got {
		names = append(names, r.Namespace+"/"+r.Name)
	}
	if len(names) != 2 || names[0] != "og/viewer" || names[1] != "local/noc" {
		t.Errorf("roles in effect = %v, want [og/viewer local/noc]", names)
	}
}
