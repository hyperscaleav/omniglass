package bus_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
)

// TestNodeLaneLandsEveryNativeKey is the other half of #1080: the push lane
// refuses every key only a node may write, and the node itself still writes
// each one. A node publishes every native key (read from probekeys, so a key
// added there is covered here) on its own telemetry subject, for its own task,
// and every one lands under the task's component, instanced by the endpoint.
// The refusal lives in the push route alone; nothing on the node lane reads it.
func TestNodeLaneLandsEveryNativeKey(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	const token = "secret-a"
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-a", hashHex(token), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('ssh', 'ssh', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'node-a'), '{"target":"127.0.0.1:22"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-ssh', 'poll', (select id from endpoint where name = 'ssh'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	conn.Close(ctx)

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	nc, err := nats.Connect(srv.ClientURL(),
		nats.UserInfo("node-a", token),
		nats.CustomInboxPrefix(collection.InboxPrefix("node-a")),
	)
	if err != nil {
		t.Fatalf("node connect: %v", err)
	}
	defer nc.Close()

	// One batch carrying every native key on its seeded lane, with a value its
	// type accepts (a property's first enum member, else a plain string).
	metricTypes, err := gw.ListMetricTypes(ctx)
	if err != nil {
		t.Fatalf("metric types: %v", err)
	}
	propertyTypes, err := gw.ListPropertyTypes(ctx)
	if err != nil {
		t.Fatalf("property types: %v", err)
	}
	isMetric := map[string]bool{}
	for _, mt := range metricTypes {
		isMetric[mt.Name] = true
	}
	propertyValue := map[string]string{}
	for _, pt := range propertyTypes {
		v := `"from-the-node"`
		var schema struct {
			Enum []any `json:"enum"`
		}
		if len(pt.Validation) > 0 {
			if err := json.Unmarshal(pt.Validation, &schema); err != nil {
				t.Fatalf("property type %s validation: %v", pt.Name, err)
			}
		}
		if len(schema.Enum) > 0 {
			raw, _ := json.Marshal(schema.Enum[0])
			v = string(raw)
		}
		propertyValue[pt.Name] = v
	}
	batch := &ogv1.TelemetryBatch{TaskId: "t-ssh", NodeId: "node-a"}
	native := probekeys.Native()
	for _, key := range native {
		if isMetric[key] {
			batch.Metrics = append(batch.Metrics, &ogv1.MetricSample{Name: key, Value: 7})
			continue
		}
		v, ok := propertyValue[key]
		if !ok {
			t.Fatalf("native key %s is seeded on no lane", key)
		}
		batch.Properties = append(batch.Properties, &ogv1.PropertySample{Name: key, ValueJson: v})
	}
	publishEvent(t, nc, "node-a", batch)

	for _, key := range native {
		deadline := time.Now().Add(5 * time.Second)
		for {
			var landed bool
			if isMetric[key] {
				m, err := gw.LatestMetricInstance(ctx, "disp-1", key, "ssh")
				if err != nil {
					t.Fatalf("latest metric %s: %v", key, err)
				}
				landed = m != nil && m.Value == 7 && m.Provenance == "observed"
			} else {
				p, err := gw.LatestProperty(ctx, "disp-1", key, "ssh")
				if err != nil {
					t.Fatalf("latest property %s: %v", key, err)
				}
				var want string
				_ = json.Unmarshal([]byte(propertyValue[key]), &want)
				landed = p != nil && p.Value == want && p.Provenance == "observed"
			}
			if landed {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the node's own %s never landed", key)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}
