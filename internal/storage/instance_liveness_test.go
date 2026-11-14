package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// An endpoint that still exists but whose current driver spec no longer emits
// a key is a retired observer of it (#1217): nothing prunes a series' latest
// row, so its last reading would stand live for good beside an endpoint that
// no longer collects the key. A native probe key is the node's own, not a
// driver's emit, so it stays live on its endpoint whatever the spec emits.

// emitSpec is a tcp driver whose one poll emits the named property key.
func emitSpec(key string) []byte {
	return []byte(fmt.Sprintf(`{"version": 1, "transport": "tcp",
		"inputs": [{"name": "host", "kind": "string", "required": true}],
		"polls": [{"name": "status", "schedule": {"every": "30s"}, "request": {"line": "GET"},
			"emits": [{"name": %q, "extract": {"regex": "^(\\S+)$"}}]}]}`, key))
}

// livenessRig is a component with one tcp endpoint attached to a custom
// driver, whose spec a test then changes.
type livenessRig struct {
	ctx  context.Context
	gw   *storage.PG
	comp *storage.Component
	drv  *storage.Driver
	base time.Time
}

func newLivenessRig(t *testing.T) *livenessRig {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
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
	drv, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "emitter", Label: "Emitter", Version: "1.0.0", Spec: emitSpec("video-input")})
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	name, node, c := "emitter", "edge", "panel-1"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &name, Component: &c, Node: &node,
		Inputs: map[string]string{"host": "10.0.0.1"}}, all); err != nil {
		t.Fatalf("attach: %v", err)
	}
	return &livenessRig{ctx: ctx, gw: gw, comp: comp, drv: drv, base: time.Now().UTC().Truncate(time.Millisecond).Add(-10 * time.Minute)}
}

// emits changes the driver's spec to one that emits key.
func (r *livenessRig) emits(t *testing.T, key string) {
	t.Helper()
	if _, err := r.gw.UpdateDriver(r.ctx, "", r.drv.ID, storage.DriverPatch{Spec: emitSpec(key)}); err != nil {
		t.Fatalf("update the driver to emit %s: %v", key, err)
	}
}

// observe lands a video-input reading on the endpoint named instance, as the
// node lane stamps it.
func (r *livenessRig) observe(t *testing.T, instance, value string, at time.Duration) {
	t.Helper()
	if err := r.gw.InsertPropertySamples(r.ctx, storage.NodeWriter(r.comp.ID, "tcp"), []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: r.comp.ID, Key: "video-input", Instance: instance, Value: value, Source: "tcp", TS: r.base.Add(at),
	}}); err != nil {
		t.Fatalf("observe %s on %s: %v", value, instance, err)
	}
}

func TestReconciliationLeavesOutAnEndpointWhoseDriverStoppedEmitting(t *testing.T) {
	t.Parallel()
	r := newLivenessRig(t)
	all := scope.Set{All: true}
	if _, err := r.gw.SetProperty(r.ctx, "", "component", "panel-1", "video-input", "", json.RawMessage(`"hdmi1"`), all, all); err != nil {
		t.Fatalf("declare video-input: %v", err)
	}
	r.observe(t, "tcp", "hdmi4", time.Second)
	if rec := reconOf(t, r.gw, "component", "panel-1", "video-input"); string(rec.Is) != `"hdmi4"` || rec.IsInstance != "tcp" || !rec.Drift {
		t.Fatalf("a driver that emits the key: want is=hdmi4 from tcp, drift; got is=%s from %q drift=%v", rec.Is, rec.IsInstance, rec.Drift)
	}

	// The spec renames its emit: the endpoint is still there, but nothing
	// refreshes video-input on it, so its last reading is no longer current.
	r.emits(t, "power-state")
	if rec := reconOf(t, r.gw, "component", "panel-1", "video-input"); rec.Is != nil || rec.Drift {
		t.Fatalf("a driver that dropped the key: want no is and no drift; got is=%s from %q drift=%v", rec.Is, rec.IsInstance, rec.Drift)
	}

	// A spec that emits the key again makes the endpoint's reading live: the
	// rule is the spec's current word, so no false pruning is left behind.
	r.emits(t, "video-input")
	if rec := reconOf(t, r.gw, "component", "panel-1", "video-input"); string(rec.Is) != `"hdmi4"` || rec.IsInstance != "tcp" || !rec.Drift {
		t.Fatalf("a spec that emits the key again: want the frozen hdmi4 live from tcp, drift; got is=%s from %q drift=%v", rec.Is, rec.IsInstance, rec.Drift)
	}
	r.observe(t, "tcp", "hdmi1", 3*time.Second)
	if rec := reconOf(t, r.gw, "component", "panel-1", "video-input"); string(rec.Is) != `"hdmi1"` || rec.IsInstance != "tcp" || rec.Drift {
		t.Fatalf("a spec that emits the key again: want is=hdmi1 from tcp, no drift; got is=%s from %q drift=%v", rec.Is, rec.IsInstance, rec.Drift)
	}

	// The spec drops it again. A sender's own instance (no transport) is not the driver's to retire,
	// so the reduction is over what is live: the pushed one alone.
	if err := r.gw.InsertPropertySamples(r.ctx, storage.PushWriter(), []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: r.comp.ID, Key: "video-input", Instance: "rx-2", Value: "hdmi3", Source: "push", TS: r.base.Add(2 * time.Second),
	}}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if rec := reconOf(t, r.gw, "component", "panel-1", "video-input"); string(rec.Is) != `"hdmi3"` || rec.IsInstance != "rx-2" || !rec.Drift {
		t.Fatalf("a pushed instance beside a retired one: want is=hdmi3 from rx-2, drift; got is=%s from %q drift=%v", rec.Is, rec.IsInstance, rec.Drift)
	}
}

// A native probe key is no driver's emit: it stays live on its own endpoint
// whatever the spec emits, on the metric lane's reads as on the property's.
func TestANativeProbeKeyStaysLiveWhateverTheDriverEmits(t *testing.T) {
	t.Parallel()
	r := newLivenessRig(t)
	all := scope.Set{All: true}
	r.emits(t, "power-state")
	if err := r.gw.InsertMetricSamples(r.ctx, storage.NodeWriter(r.comp.ID, "tcp"), []storage.MetricSampleWrite{{
		OwnerKind: "component", OwnerID: r.comp.ID, Key: "tcp-connect-time", Instance: "tcp", Value: 4.8, Source: "tcp", TS: r.base,
	}}); err != nil {
		t.Fatalf("observe tcp-connect-time: %v", err)
	}
	m, ok := metricsByName(mustEffectiveMetrics(t, r.gw, "component", "panel-1", all))["tcp-connect-time"]
	if !ok || m.Instance != "tcp" || m.InstanceCount != 1 {
		t.Fatalf("a native probe key on a driver that does not emit it: want 4.8 from tcp, count 1; got %+v (present %v)", m, ok)
	}
}
