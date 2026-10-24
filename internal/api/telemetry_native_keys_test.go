package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestTelemetryPushRefusesNativeKeys holds the push lane to #1080: a key a
// native probe writes is the node's own observation (a reachability verdict or
// one of its rungs, a probe metric or timing, the node's account of its held
// session, the gate and host key that steer it), which the console reads as
// the node's. So a push is refused each one BY NAME: on its own lane and on
// every other, whatever instance it names, alone or in a batch beside rows
// that land. The keys come from probekeys, the one declaration the probes emit
// from, so a key added there is covered here without editing this test.
func TestTelemetryPushRefusesNativeKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw, c, tok := pushHarness(t, ctx)

	// Each native key's seeded lane, and a value its type accepts, so a key the
	// lane failed to refuse would land rather than fail validation.
	metricTypes, err := gw.ListMetricTypes(ctx)
	if err != nil {
		t.Fatalf("metric types: %v", err)
	}
	propertyTypes, err := gw.ListPropertyTypes(ctx)
	if err != nil {
		t.Fatalf("property types: %v", err)
	}
	type laned struct {
		lane  string
		value any
	}
	seeded := map[string]laned{}
	for _, mt := range metricTypes {
		seeded[mt.Name] = laned{"metrics", 1}
	}
	for _, pt := range propertyTypes {
		seeded[pt.Name] = laned{"properties", validPropertyValue(t, pt)}
	}
	item := func(lane, name, instance string, value any) map[string]any {
		switch lane {
		case "metrics":
			return map[string]any{"name": name, "instance": instance, "value": 1}
		case "properties":
			return map[string]any{"name": name, "instance": instance, "value": value}
		default:
			return map[string]any{"name": name, "instance": instance, "message": "pushed"}
		}
	}
	push := func(body map[string]any) pushResp {
		t.Helper()
		body["owner"] = map[string]any{"kind": "component", "ref": "bar-1"}
		var r pushResp
		if err := json.Unmarshal(c.do(tok, http.MethodPost, "/telemetry:push", body, http.StatusAccepted), &r); err != nil {
			t.Fatalf("decode push response: %v", err)
		}
		return r
	}
	refusedByName := func(r pushResp, key string) bool {
		for _, rej := range r.Rejected {
			if rej.Name == key && strings.Contains(rej.Reason, key) && strings.Contains(rej.Reason, "only the node") {
				return true
			}
		}
		return false
	}

	// The node-only keys, and the native keys ruled pushable (each with its
	// reason in probekeys), which a push may write on their own lane.
	var native []string
	for _, key := range probekeys.Native() {
		s, ok := seeded[key]
		if !ok {
			t.Fatalf("native key %s is seeded on no lane", key)
		}
		if !probekeys.IsNodeOnly(key) {
			r := push(map[string]any{s.lane: []map[string]any{item(s.lane, key, "ssh", s.value)}})
			if r.Accepted.Metrics+r.Accepted.Properties != 1 || len(r.Rejected) != 0 {
				t.Errorf("push of pushable native key %s = %+v, want it accepted", key, r)
			}
			continue
		}
		native = append(native, key)
	}
	if len(native) == 0 {
		t.Fatal("probekeys rules no native key node-only")
	}
	for _, key := range native {
		s := seeded[key]
		// Its own lane, then every other: a node-only name is refused as such
		// wherever it rides, not merely as unregistered on a lane it is not.
		for _, lane := range []string{"metrics", "properties", "events"} {
			for _, instance := range []string{"", "ssh", "bar-1-tcp"} {
				r := push(map[string]any{lane: []map[string]any{item(lane, key, instance, s.value)}})
				if r.Accepted.Metrics+r.Accepted.Properties+r.Accepted.Events != 0 || !refusedByName(r, key) {
					t.Errorf("push of native key %s on %s (instance %q) = %+v, want it refused by name", key, lane, instance, r)
				}
			}
		}
	}

	// A batch: every node-only key on its seeded lane, one of them twice,
	// beside one metric and one property a push may write. The two land; each
	// node-only entry is reported by name and none is published.
	var metrics, properties []map[string]any
	for _, key := range append(slices.Clone(native), native[0]) {
		s := seeded[key]
		if s.lane == "metrics" {
			metrics = append(metrics, item("metrics", key, "ssh", nil))
		} else {
			properties = append(properties, item("properties", key, "ssh", s.value))
		}
	}
	metrics = append(metrics, map[string]any{"name": "uptime", "instance": "ssh", "value": 4242})
	properties = append(properties, map[string]any{"name": "video-input", "instance": "ssh", "value": "hdmi2"})
	r := push(map[string]any{"metrics": metrics, "properties": properties})
	if r.Accepted.Metrics != 1 || r.Accepted.Properties != 1 {
		t.Fatalf("batch accepted = %+v, want only uptime and video-input", r.Accepted)
	}
	var rejected []string
	for _, rej := range r.Rejected {
		rejected = append(rejected, rej.Name)
	}
	slices.Sort(rejected)
	want := slices.Sorted(slices.Values(append(slices.Clone(native), native[0])))
	if !slices.Equal(rejected, want) {
		t.Fatalf("batch rejected %v, want every node-only entry %v", rejected, want)
	}

	// Casing and spacing variants of a node-only key never reach a node's
	// series: each is refused (as node-only, before the registry's exact,
	// lowercase lookup would refuse it as unregistered).
	for _, variant := range []string{"TCP-Open", "Endpoint-Reachable", " session-state", "endpoint-rejection ", "SSH-HOST-KEY"} {
		r := push(map[string]any{
			"metrics":    []map[string]any{{"name": variant, "instance": "ssh", "value": 1}},
			"properties": []map[string]any{{"name": variant, "instance": "ssh", "value": "up"}},
		})
		if r.Accepted.Metrics+r.Accepted.Properties != 0 || len(r.Rejected) != 2 ||
			!strings.Contains(r.Rejected[0].Reason, "only the node") || !strings.Contains(r.Rejected[1].Reason, "only the node") {
			t.Errorf("push of variant %q = %+v, want both refused as node-only", variant, r)
		}
	}

	// The batch's accepted rows landing is the watermark: the batch was
	// processed, so a native key absent now was never written.
	waitFor(t, "the pushed uptime", func() bool {
		m, err := gw.LatestMetricInstance(ctx, "bar-1", "uptime", "ssh")
		return err == nil && m != nil && m.Value == 4242
	})
	for _, key := range native {
		if seeded[key].lane == "metrics" {
			if m, err := gw.LatestMetric(ctx, "bar-1", key); err != nil || m != nil {
				t.Errorf("a pushed native metric %s landed: %+v (err %v)", key, m, err)
			}
			continue
		}
		for _, instance := range []string{"", "ssh", "bar-1-tcp"} {
			if p, err := gw.LatestProperty(ctx, "bar-1", key, instance); err != nil || p != nil {
				t.Errorf("a pushed native property %s[%s] landed: %+v (err %v)", key, instance, p, err)
			}
		}
	}
}

// pushHarness stands up the push lane end to end: a seeded gateway, an owner,
// the component bar-1, a real bus, and the API publishing onto it.
func pushHarness(t *testing.T, ctx context.Context) (storage.Gateway, *apiClient, string) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	t.Cleanup(busSrv.Shutdown)
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv)))
	t.Cleanup(srv.Close)
	return gw, &apiClient{t: t, ctx: ctx, base: srv.URL}, tok
}

// validPropertyValue picks a value pt's validation accepts: its first enum
// member when it declares one, else a plain string.
func validPropertyValue(t *testing.T, pt storage.PropertyType) any {
	t.Helper()
	if len(pt.Validation) == 0 {
		return "pushed"
	}
	var v struct {
		Enum []any `json:"enum"`
	}
	if err := json.Unmarshal(pt.Validation, &v); err != nil {
		t.Fatalf("property type %s validation: %v", pt.Name, err)
	}
	if len(v.Enum) > 0 {
		return v.Enum[0]
	}
	return "pushed"
}
