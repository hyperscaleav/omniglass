package bus

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// outcomeRegistry is the catalog the outcome cases derive against: a
// collected metric, a calc metric no node may write, a node-probe metric, a
// property with an enum, and an event with a payload schema.
func outcomeRegistry() collection.Registry {
	return collection.NewRegistry(
		[]storage.MetricType{
			{Name: "participant-count", WriterClass: writerclass.Collected},
			{Name: "rollup-avg", WriterClass: writerclass.Calc},
			{Name: "tcp-connect-time", WriterClass: writerclass.NodeProbe},
		},
		[]storage.PropertyType{{Name: "call-state", DataType: "string", Validation: []byte(`{"enum":["active","idle"]}`)}},
		[]storage.EventType{{Name: "call-started", PayloadSchema: []byte(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)}})
}

// only reports whether t counted exactly n items of kind k under o and
// nothing else.
func only(t delivery.Tally, k delivery.Kind, o delivery.Outcome, n uint64) bool {
	for _, kk := range delivery.Kinds {
		for _, oo := range delivery.Outcomes {
			want := uint64(0)
			if kk == k && oo == o {
				want = n
			}
			if t.Count(kk, oo) != want {
				return false
			}
		}
	}
	return true
}

// TestDeriveTalliesEachRefusal: each consumer-side refusal the derivations
// make counts its item under its own outcome and only that one (#1641), and a
// sample that derives counts nothing (the gateway counts it stored).
func TestDeriveTalliesEachRefusal(t *testing.T) {
	reg := outcomeRegistry()
	owner := storage.TaskOwner{ComponentID: "disp-1", EndpointName: "disp-1-tcp", Transport: "tcp"}
	w := storage.NodeWriter(owner.ComponentID, owner.EndpointName)
	arc := componentArc(owner)
	cases := []struct {
		name string
		ev   *ogv1.TelemetryBatch
		kind delivery.Kind
		want delivery.Outcome
	}{
		{"unregistered metric", &ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "no-such-key", Value: 1}}}, delivery.Metric, delivery.RefusedUnregistered},
		{"unregistered property", &ogv1.TelemetryBatch{Properties: []*ogv1.PropertySample{{Name: "no-such-key", ValueJson: `"x"`}}}, delivery.Property, delivery.RefusedUnregistered},
		{"unregistered event", &ogv1.TelemetryBatch{Events: []*ogv1.EventSample{{Name: "no-such-key", Message: "m"}}}, delivery.Event, delivery.RefusedUnregistered},
		{"writer class refuses the node", &ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "rollup-avg", Value: 1}}}, delivery.Metric, delivery.RefusedWriter},
		{"node-probe at another instance", &ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "tcp-connect-time", Value: 1, Instance: "other"}}}, delivery.Metric, delivery.RefusedInstance},
		{"property fails its validation", &ogv1.TelemetryBatch{Properties: []*ogv1.PropertySample{{Name: "call-state", ValueJson: `"sideways"`}}}, delivery.Property, delivery.RefusedInvalid},
		{"event payload fails its schema", &ogv1.TelemetryBatch{Events: []*ogv1.EventSample{{Name: "call-started", Payload: []byte(`{"m":1}`)}}}, delivery.Event, delivery.RefusedInvalid},
		{"event says nothing", &ogv1.TelemetryBatch{Events: []*ogv1.EventSample{{Name: "call-started"}}}, delivery.Event, delivery.RefusedEmpty},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var tally delivery.Tally
			m := deriveMetrics(c.ev, arc, owner, reg, w, &tally)
			p := deriveProperties(c.ev, arc, owner, reg, w, &tally)
			e := deriveEvents(c.ev, arc, owner, reg, w, &tally)
			if len(m)+len(p)+len(e) != 0 {
				t.Fatalf("derived %d/%d/%d rows, want the sample refused", len(m), len(p), len(e))
			}
			if !only(tally, c.kind, c.want, 1) {
				t.Fatalf("tally = %s, want one %s %s and nothing else", mustJSON(t, tally), c.kind, c.want)
			}
		})
	}

	var tally delivery.Tally
	ev := &ogv1.TelemetryBatch{
		Metrics:    []*ogv1.MetricSample{{Name: "participant-count", Value: 1}, {Name: "tcp-connect-time", Value: 1}},
		Properties: []*ogv1.PropertySample{{Name: "call-state", ValueJson: `"idle"`}},
		Events:     []*ogv1.EventSample{{Name: "call-started", Payload: []byte(`{"n":1}`)}},
	}
	m := deriveMetrics(ev, arc, owner, reg, w, &tally)
	p := deriveProperties(ev, arc, owner, reg, w, &tally)
	e := deriveEvents(ev, arc, owner, reg, w, &tally)
	if len(m) != 2 || len(p) != 1 || len(e) != 1 || !tally.Empty() {
		t.Fatalf("derived %d/%d/%d with tally %s, want every sample derived and nothing counted", len(m), len(p), len(e), mustJSON(t, tally))
	}
}

// TestFanOutTalliesUnboundAndUnidentified: an identified sample the fan-out
// cannot place counts unidentified (no conduit, nothing resolved, or no valid
// identity) or unbound (an identity no binding places), and a bound sample
// its lane refuses counts that refusal (#1641).
func TestFanOutTalliesUnboundAndUnidentified(t *testing.T) {
	reg := outcomeRegistry()
	fanned := func() *ogv1.TelemetryBatch {
		return &ogv1.TelemetryBatch{
			Metrics:    []*ogv1.MetricSample{{Name: "participant-count", Value: 3, Identity: room("zr-a")}},
			Properties: []*ogv1.PropertySample{{Name: "call-state", ValueJson: `"idle"`, Identity: room("zr-a")}},
			Events:     []*ogv1.EventSample{{Name: "call-started", Payload: []byte(`{"n":1}`), Identity: room("zr-a")}},
		}
	}
	s := &Server{}
	bound := ingestBinding{Conduit: "conduit-1", ConduitEndpoint: "webhook"}
	everyKind := func(t *testing.T, tally delivery.Tally, o delivery.Outcome) {
		t.Helper()
		for _, k := range []delivery.Kind{delivery.Metric, delivery.Property, delivery.Event} {
			if tally.Count(k, o) != 1 || tally.Items(k) != 1 {
				t.Errorf("%s: tally = %s, want its one sample %s", k, mustJSON(t, tally), o)
			}
		}
	}

	t.Run("no conduit", func(t *testing.T) {
		var tally delivery.Tally
		if rows, unbound := s.fanOut(fanned(), ingestBinding{}, reg, nil, &tally); rows != nil || unbound != nil {
			t.Fatalf("rows %+v, unbound %+v, want nothing", rows, unbound)
		}
		everyKind(t, tally, delivery.Unidentified)
	})
	t.Run("nothing resolved", func(t *testing.T) {
		var tally delivery.Tally
		s.fanOut(fanned(), bound, reg, nil, &tally)
		everyKind(t, tally, delivery.Unidentified)
	})
	t.Run("no valid identity", func(t *testing.T) {
		var tally delivery.Tally
		bad := fanned()
		for _, m := range bad.Metrics {
			m.Identity = &ogv1.Identity{Key: "ucc-room-id", Value: "bad\x00id"}
		}
		for _, p := range bad.Properties {
			p.Identity = &ogv1.Identity{Key: "ucc-room-id", Value: "bad\x00id"}
		}
		for _, e := range bad.Events {
			e.Identity = &ogv1.Identity{Key: "ucc-room-id", Value: "bad\x00id"}
		}
		s.fanOut(bad, bound, reg, &storage.IdentityResolution{ConduitName: "c"}, &tally)
		everyKind(t, tally, delivery.Unidentified)
	})
	t.Run("unbound", func(t *testing.T) {
		var tally delivery.Tally
		s.fanOut(fanned(), bound, reg, &storage.IdentityResolution{ConduitName: "c"}, &tally)
		everyKind(t, tally, delivery.Unbound)
	})
	t.Run("bound, refused by its lane", func(t *testing.T) {
		var tally delivery.Tally
		ev := &ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "no-such-key", Value: 1, Identity: room("zr-a")}}}
		res := &storage.IdentityResolution{ConduitName: "c", Owners: map[storage.IdentityKey]storage.IdentityOwner{
			{Key: "ucc-room-id", Value: "zr-a"}: {Kind: "system", ID: "sys-a"}}}
		if rows, _ := s.fanOut(ev, bound, reg, res, &tally); rows != nil {
			t.Fatalf("rows = %+v, want none", rows)
		}
		if !only(tally, delivery.Metric, delivery.RefusedUnregistered, 1) {
			t.Fatalf("tally = %s, want one metric refused-unregistered", mustJSON(t, tally))
		}
	})
}

// outcomeStore is a node lane's store with no database: it resolves the
// tasks it holds, serves outcomeRegistry, places no identity, and captures
// the batch it is asked to land.
type outcomeStore struct {
	fakeStore
	owners  map[string]storage.TaskOwner
	landed  *storage.IngestBatch
	landErr error                 // what LandBatch returns, nil to land
	termed  []storage.TermedBatch // every Term receipt recorded
	termErr error                 // what RecordTermedBatch returns
}

func (s *outcomeStore) ResolveTaskOwners(_ context.Context, _ string, ids []string) (map[string]storage.TaskOwner, error) {
	out := map[string]storage.TaskOwner{}
	for _, id := range ids {
		if o, ok := s.owners[id]; ok {
			out[id] = o
		}
	}
	return out, nil
}

func (s *outcomeStore) ListMetricTypes(context.Context) ([]storage.MetricType, error) {
	return []storage.MetricType{{Name: "participant-count", WriterClass: writerclass.Collected}}, nil
}

func (s *outcomeStore) ListPropertyTypes(context.Context) ([]storage.PropertyType, error) {
	return []storage.PropertyType{{Name: "call-state", DataType: "string"}}, nil
}

func (s *outcomeStore) ListEventTypes(context.Context) ([]storage.EventType, error) {
	return []storage.EventType{{Name: "call-started"}}, nil
}

func (s *outcomeStore) LandBatch(_ context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	s.landed = &b
	if s.landErr != nil {
		return storage.LandResult{}, s.landErr
	}
	return storage.LandResult{Landed: true}, nil
}

func (s *outcomeStore) RecordTermedBatch(_ context.Context, b storage.TermedBatch) error {
	if s.termErr != nil {
		return s.termErr
	}
	s.termed = append(s.termed, b)
	return nil
}

// TestLandNodeTalliesEachPath: a node tick's paths that land nothing count
// their items before the batch reaches the gateway (#1641): every item of an
// orphaned group is orphaned, a node-self block that names an identity is
// refused-invalid, and a ledger that declares more than the batch decodes to
// reads the shortfall as missing. The batch carries its node and ledger.
func TestLandNodeTalliesEachPath(t *testing.T) {
	ctx := context.Background()
	owner := storage.TaskOwner{ComponentID: "disp-1", EndpointID: "ep-1", EndpointName: "disp-1-tcp", Transport: "tcp"}

	t.Run("orphaned group", func(t *testing.T) {
		store := &outcomeStore{owners: map[string]storage.TaskOwner{"t-own": owner}}
		s := &Server{store: store, orphans: newOrphanTally(time.Minute)}
		ev := &ogv1.TelemetryBatch{Groups: []*ogv1.TaskGroup{
			{TaskId: "t-own", Metrics: []*ogv1.MetricSample{{Name: "participant-count", Value: 1}}},
			{TaskId: "t-gone",
				Metrics:    []*ogv1.MetricSample{{Name: "participant-count", Value: 1}, {Name: "participant-count", Value: 2}},
				Properties: []*ogv1.PropertySample{{Name: "call-state", ValueJson: `"idle"`}},
				Events:     []*ogv1.EventSample{{Name: "call-started", Message: "m"}},
				TaskLogs:   []*ogv1.LogLine{{Message: "a"}, {Message: "b"}, {Message: "c"}}},
		}}
		if err := s.landNode(ctx, "b1", "node-a", ev); err != nil {
			t.Fatal(err)
		}
		got := store.landed.Tally
		for k, n := range map[delivery.Kind]uint64{delivery.Metric: 2, delivery.Property: 1, delivery.Event: 1, delivery.Log: 3} {
			if got.Count(k, delivery.Orphaned) != n || got.Items(k) != n {
				t.Errorf("%s: tally = %s, want %d orphaned and nothing else", k, mustJSON(t, got), n)
			}
		}
		if store.landed.Node != "node-a" {
			t.Errorf("batch node = %q, want node-a", store.landed.Node)
		}
	})

	t.Run("node-self names an identity", func(t *testing.T) {
		store := &outcomeStore{}
		s := &Server{store: store, orphans: newOrphanTally(time.Minute)}
		ev := &ogv1.TelemetryBatch{NodeSelf: &ogv1.NodeSelf{Metrics: []*ogv1.MetricSample{
			{Name: "node-cpu-ratio", Value: 0.1}, {Name: "node-mem-ratio", Value: 0.2, Identity: room("zr-a")}}}}
		if err := s.landNode(ctx, "b1", "node-a", ev); err != nil {
			t.Fatal(err)
		}
		if !only(store.landed.Tally, delivery.Metric, delivery.RefusedInvalid, 2) {
			t.Fatalf("tally = %s, want both node-self metrics refused-invalid", mustJSON(t, store.landed.Tally))
		}
	})

	t.Run("declared above decoded", func(t *testing.T) {
		store := &outcomeStore{owners: map[string]storage.TaskOwner{"t-own": owner}}
		s := &Server{store: store, orphans: newOrphanTally(time.Minute)}
		stamp := delivery.Stamp{Boot: "boot-1", Seq: 4, Minted: time.Date(2026, 11, 9, 1, 0, 0, 0, time.UTC), Resolved: 3,
			Kinds: delivery.Declared{delivery.Metric: {Count: 3, Before: 10}, delivery.Log: {Count: 1, Before: 2}}}
		ev := &ogv1.TelemetryBatch{Ledger: stamp.Proto(), Groups: []*ogv1.TaskGroup{
			{TaskId: "t-own", Metrics: []*ogv1.MetricSample{{Name: "participant-count", Value: 1}},
				TaskLogs: []*ogv1.LogLine{{Message: "a"}, {Message: "b"}}},
		}}
		if err := s.landNode(ctx, "b1", "node-a", ev); err != nil {
			t.Fatal(err)
		}
		if !only(store.landed.Tally, delivery.Metric, delivery.Missing, 2) {
			t.Fatalf("tally = %s, want the two undecoded metrics missing; a log above its declared count is warned, not counted",
				mustJSON(t, store.landed.Tally))
		}
		l := store.landed.Ledger
		if l == nil || l.Boot != "boot-1" || l.Seq != 4 || l.Resolved != 3 || !l.Minted.Equal(stamp.Minted) || l.Kinds[delivery.Metric].Count != 3 {
			t.Fatalf("batch ledger = %+v, want the stamp the batch carried", l)
		}
	})

	t.Run("no ledger", func(t *testing.T) {
		store := &outcomeStore{owners: map[string]storage.TaskOwner{"t-own": owner}}
		s := &Server{store: store, orphans: newOrphanTally(time.Minute)}
		ev := &ogv1.TelemetryBatch{Groups: []*ogv1.TaskGroup{{TaskId: "t-own", Metrics: []*ogv1.MetricSample{{Name: "participant-count", Value: 1}}}}}
		if err := s.landNode(ctx, "b1", "node-a", ev); err != nil {
			t.Fatal(err)
		}
		if store.landed.Ledger != nil || !store.landed.Tally.Empty() {
			t.Fatalf("ledger %+v tally %s, want neither for a batch that carried no ledger", store.landed.Ledger, mustJSON(t, store.landed.Tally))
		}
	})
}

// TestTermRecordsItsCause: each path that Terms a node's batch whole
// records a receipt for it, its declared items under the path's own outcome,
// so the loss table names the cause instead of reading them missing (#1691).
// An undecodable batch has no readable ledger, so it records nothing and its
// items stay missing; a transient failure below the bound redelivers and
// records nothing; and a receipt that fails to record still Terms the batch.
func TestTermRecordsItsCause(t *testing.T) {
	stamp := delivery.Stamp{Boot: "boot-1", Seq: 7, Minted: time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC), Resolved: 6,
		Kinds: delivery.Declared{delivery.Log: {Count: 1, Before: 3}, delivery.Metric: {Before: 4}}}
	batch := func(edit func(*ogv1.TelemetryBatch)) []byte {
		ev := &ogv1.TelemetryBatch{Ledger: stamp.Proto(), Logs: []*ogv1.LogLine{{Message: "self"}}}
		if edit != nil {
			edit(ev)
		}
		b, err := proto.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	header := nats.Header{jetstream.MsgIDHeader: []string{"node-a-boot-1-7"}}
	const id = "og.v1.telemetry.node-a node-a-boot-1-7"
	transient := errors.New("database away")
	refused := fmt.Errorf("land: %w", &storage.KeyWriterError{Key: "k"})
	delivered := func(n uint64) *jetstream.MsgMetadata { return &jetstream.MsgMetadata{NumDelivered: n} }

	cases := []struct {
		name    string
		data    []byte
		landErr error
		meta    *jetstream.MsgMetadata
		metaErr error
		cause   delivery.Outcome // "" records nothing
		nak     bool
	}{
		{name: "undecodable", data: []byte{0xff, 0xff, 0xff}, meta: delivered(1)},
		{name: "owner asserted on the node lane", meta: delivered(1), cause: delivery.RefusedOwner,
			data: batch(func(ev *ogv1.TelemetryBatch) { ev.Owner = &ogv1.Owner{Kind: "component", Ref: "disp-1"} })},
		{name: "task rows outside the groups", meta: delivered(1), cause: delivery.RefusedTask,
			data: batch(func(ev *ogv1.TelemetryBatch) { ev.TaskId = "t-1" })},
		{name: "gateway refused the batch", data: batch(nil), landErr: refused, meta: delivered(1), cause: delivery.RefusedBatch},
		{name: "unreadable delivery metadata", data: batch(nil), landErr: transient, metaErr: errors.New("no metadata"), cause: delivery.RefusedMetadata},
		{name: "max deliveries reached", data: batch(nil), landErr: transient, meta: delivered(maxTelemetryDeliveries), cause: delivery.Abandoned},
		{name: "below the bound redelivers", data: batch(nil), landErr: transient, meta: delivered(1), nak: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &outcomeStore{landErr: c.landErr}
			s := &Server{store: store, orphans: newOrphanTally(time.Minute)}
			m := &fakeMsg{data: c.data, header: header, meta: c.meta, metaErr: c.metaErr}
			s.handleTelemetry(m)
			if m.naked != c.nak || m.termed == c.nak {
				t.Fatalf("naked=%v termed=%v, want naked=%v", m.naked, m.termed, c.nak)
			}
			if c.cause == "" {
				if len(store.termed) != 0 {
					t.Fatalf("recorded %+v, want no Term receipt", store.termed)
				}
				return
			}
			if len(store.termed) != 1 {
				t.Fatalf("recorded %d Term receipts, want 1", len(store.termed))
			}
			got := store.termed[0]
			if got.BatchID != id || got.Node != "node-a" || got.Ledger.Boot != "boot-1" || got.Ledger.Seq != 7 ||
				got.Ledger.Resolved != 6 || !got.Ledger.Minted.Equal(stamp.Minted) {
				t.Fatalf("receipt = %+v, want batch %q of node-a carrying its ledger", got, id)
			}
			if !only(got.Outcome, delivery.Log, c.cause, 1) {
				t.Fatalf("outcome = %s, want its one declared log %s and nothing else", mustJSON(t, got.Outcome), c.cause)
			}
		})
	}

	t.Run("a receipt that fails to record still Terms", func(t *testing.T) {
		store := &outcomeStore{termErr: transient}
		s := &Server{store: store, orphans: newOrphanTally(time.Minute)}
		m := &fakeMsg{data: batch(func(ev *ogv1.TelemetryBatch) { ev.TaskId = "t-1" }), header: header, meta: delivered(1)}
		s.handleTelemetry(m)
		if !m.termed || m.naked {
			t.Fatalf("naked=%v termed=%v, want the batch Termed whatever its receipt", m.naked, m.termed)
		}
	})
}

func mustJSON(t *testing.T, v delivery.Tally) string {
	t.Helper()
	b, err := v.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
