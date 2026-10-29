package storage_test

import (
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// fanFixture is a conduit (disp-1) with one identity bound to a system and a
// second system bound to nothing.
type fanFixture struct {
	writerFixture
	conduit, bound, unbound string
}

func newFanFixture(t *testing.T) fanFixture {
	t.Helper()
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	for _, name := range []string{"room-a", "room-b"} {
		if _, err := f.gw.CreateSystem(f.ctx, "", storage.SystemSpec{Name: name}, all, all); err != nil {
			t.Fatalf("create system %s: %v", name, err)
		}
	}
	sc := storage.IdentityBindingScopes{ComponentRead: all, ComponentUpdate: all, SystemRead: all, SystemUpdate: all}
	if _, err := f.gw.PutIdentityBinding(f.ctx, f.actor, "disp-1", "ucc-room-id", "zr-a", storage.IdentityBindingOwner{Kind: "system", Name: "room-a"}, sc); err != nil {
		t.Fatalf("bind: %v", err)
	}
	var ff fanFixture
	ff.writerFixture = f
	for name, dst := range map[string]*string{"disp-1": &ff.conduit} {
		if err := f.conn.QueryRow(f.ctx, `select id::text from component where name = $1`, name).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	for name, dst := range map[string]*string{"room-a": &ff.bound, "room-b": &ff.unbound} {
		if err := f.conn.QueryRow(f.ctx, `select id::text from system where name = $1`, name).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	return ff
}

// TestFanoutWriterIsHeldToTheConduitsBindings: the fan-out writer (ADR-1282
// point 6) lands a collected key on an owner the conduit's bindings name, and
// the gateway refuses, before anything lands, a row on an owner the conduit
// has not bound, and any key outside the collected class: a node-probe key,
// a platform key, a calc rule's output.
func TestFanoutWriterIsHeldToTheConduitsBindings(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newFanFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateMetricType(f.ctx, "", storage.MetricTypeSpec{Name: "avg-uptime", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateCalcRule(f.ctx, "", storage.CalcRuleSpec{Name: "uptime-avg", OwnerKind: "system",
		Inputs: []rules.CalcInput{{Alias: "u", Key: "uptime"}}, Reduce: rules.CalcReduce{Expr: "avg(u)"}, OutputKey: "avg-uptime"}, all); err != nil {
		t.Fatalf("calc rule: %v", err)
	}
	w := storage.FanoutWriter(f.conduit)
	metric := func(owner, key string) []storage.MetricSampleWrite {
		return []storage.MetricSampleWrite{{OwnerKind: "system", OwnerID: owner, Key: key, Value: 1, Source: "disp-1/http"}}
	}
	if err := f.gw.InsertMetricSamples(f.ctx, w, metric(f.bound, "participant-count")); err != nil {
		t.Fatalf("a collected key on a bound owner: %v", err)
	}
	if n := f.count(t, `select count(*) from metric where system_id = $1 and provenance = 'observed'`, f.bound); n != 1 {
		t.Fatalf("the fanned metric landed %d rows on its owner, want one observed row", n)
	}
	for _, c := range []struct {
		name string
		err  error
	}{
		{"an owner the conduit has not bound", f.gw.InsertMetricSamples(f.ctx, w, metric(f.unbound, "participant-count"))},
		{"a node-probe key", f.gw.InsertMetricSamples(f.ctx, w, metric(f.bound, "tcp-open"))},
		{"a calc rule's output", f.gw.InsertMetricSamples(f.ctx, w, metric(f.bound, "avg-uptime"))},
		{"a platform key", f.gw.InsertPropertySamples(f.ctx, w, []storage.PropertySampleWrite{{OwnerKind: "system", OwnerID: f.bound, Key: "health", Value: "ok"}})},
		{"another conduit's bound owner", f.gw.InsertMetricSamples(f.ctx, storage.FanoutWriter(f.unbound), metric(f.bound, "participant-count"))},
		{"the conduit itself", f.gw.InsertMetricSamples(f.ctx, w, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: f.conduit, Key: "participant-count", Value: 1}})},
	} {
		if !errors.Is(c.err, storage.ErrKeyWriterRefused) {
			t.Errorf("%s: err = %v, want ErrKeyWriterRefused", c.name, c.err)
		}
	}
	if n := f.count(t, `select count(*) from metric`); n != 1 {
		t.Errorf("a refused fan-out landed rows: %d metric rows, want the one admitted", n)
	}
}

// TestResolveIdentityBindingsInOneRead: the fan-out's read (system mode, the
// binding is the fence) maps each identity a batch names to its bound owner
// and leaves an unbound one out, naming the conduit.
func TestResolveIdentityBindingsInOneRead(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newFanFixture(t)
	res, err := f.gw.ResolveIdentityBindings(f.ctx, f.conduit, []storage.IdentityKey{{Key: "ucc-room-id", Value: "zr-a"}, {Key: "ucc-room-id", Value: "zr-z"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ConduitName != "disp-1" {
		t.Errorf("conduit name = %q", res.ConduitName)
	}
	if o, ok := res.Owners[storage.IdentityKey{Key: "ucc-room-id", Value: "zr-a"}]; !ok || o.Kind != "system" || o.ID != f.bound {
		t.Errorf("zr-a resolves to %+v (%v), want the bound system", o, ok)
	}
	if _, ok := res.Owners[storage.IdentityKey{Key: "ucc-room-id", Value: "zr-z"}]; ok {
		t.Error("an unbound identity resolved to an owner")
	}
}

// TestAStaleFanOutIsRedeliveredNotDropped: a binding deleted between the
// ingest's resolve and the batch's transaction makes its fanned rows stale.
// The batch is not refused (which would drop the conduit's own rows and
// every other owner's with it): it fails transiently, writes nothing, and the
// redelivery resolves the bindings afresh.
func TestAStaleFanOutIsRedeliveredNotDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newFanFixture(t)
	_, err := f.gw.LandBatch(f.ctx, storage.IngestBatch{BatchID: "stale", Writer: storage.PushWriter(),
		Metrics: []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: f.conduit, Key: "participant-count", Value: 1}},
		Fanned: &storage.FannedRows{Writer: storage.FanoutWriter(f.conduit), Metrics: []storage.MetricSampleWrite{
			{OwnerKind: "system", OwnerID: f.unbound, Key: "participant-count", Value: 2, Source: "disp-1/http"}}},
	})
	if err == nil || storage.IsRefusedWrite(err) || !errors.Is(err, storage.ErrFanoutStale) {
		t.Fatalf("a stale fan-out: err = %v (refused %v), want ErrFanoutStale, redelivered", err, storage.IsRefusedWrite(err))
	}
	if n := f.count(t, `select count(*) from metric`); n != 0 {
		t.Errorf("a stale batch landed %d rows", n)
	}
	if n := f.count(t, `select count(*) from telemetry_receipt where batch_id = 'stale'`); n != 0 {
		t.Error("a stale batch kept its receipt, so its redelivery would land nothing")
	}
}

// TestTheInboxNeverHoldsABoundIdentity: a batch that resolved an identity
// unbound and commits after the operator bound it does not put it back in
// the inbox.
func TestTheInboxNeverHoldsABoundIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newFanFixture(t)
	now := time.Now().UTC()
	if _, err := f.gw.LandBatch(f.ctx, storage.IngestBatch{BatchID: "late", Writer: storage.PushWriter(), Unbound: []storage.UnboundIdentityWrite{
		{ConduitID: f.conduit, Key: "ucc-room-id", Value: "zr-a", Count: 1, FirstAt: now, LastAt: now, Keys: []string{"call-state"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `select count(*) from unbound_identity where value = 'zr-a'`); n != 0 {
		t.Errorf("a bound identity was written into the inbox (%d rows)", n)
	}
}
