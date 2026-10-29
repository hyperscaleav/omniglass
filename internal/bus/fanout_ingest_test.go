package bus

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

// countingStore counts the fan-out's reads, so a test can tell confinement
// dropped a batch before any identity was read.
type countingStore struct {
	*storage.PG
	resolves atomic.Int64
}

func (c *countingStore) ResolveIdentityBindings(ctx context.Context, conduit string, ids []storage.IdentityKey) (*storage.IdentityResolution, error) {
	c.resolves.Add(1)
	return c.PG.ResolveIdentityBindings(ctx, conduit, ids)
}

// fanIngest is a conduit (meetings-cloud) polled by node-a's task t-rooms
// over its http endpoint, two of its room ids bound to systems, and a second
// node holding a task of its own on another component.
type fanIngest struct {
	ctx     context.Context
	gw      *storage.PG
	store   *countingStore
	conn    *pgx.Conn
	srv     *Server
	conduit string
	actor   string
	seq     int
}

func newFanIngest(t *testing.T, opts ...storage.Option) *fanIngest {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, opts...)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	all := scope.Set{All: true}
	for _, n := range []string{"node-a", "node-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []string{"meetings-cloud", "other-cloud"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []string{"room-a", "room-b", "room-c"} {
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(ctx, `
		insert into endpoint (name, transport, component, node_name, params) values
			('http', 'http', (select id from component where name = 'meetings-cloud'), (select principal_id from node where name = 'node-a'), '{"target":"127.0.0.1:443"}'::jsonb),
			('http-b', 'http', (select id from component where name = 'other-cloud'), (select principal_id from node where name = 'node-b'), '{"target":"127.0.0.1:443"}'::jsonb);
		insert into task (id, mode, endpoint_id, enabled) values
			('t-rooms', 'poll', (select id from endpoint where name = 'http'), true),
			('t-other', 'poll', (select id from endpoint where name = 'http-b'), true)`); err != nil {
		t.Fatalf("endpoints and tasks: %v", err)
	}
	f := &fanIngest{ctx: ctx, gw: gw, conn: conn}
	if err := conn.QueryRow(ctx, `select id::text from component where name = 'meetings-cloud'`).Scan(&f.conduit); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&f.actor); err != nil {
		t.Fatal(err)
	}
	f.bind(t, "zr-a", "room-a")
	f.bind(t, "zr-b", "room-b")
	f.store = &countingStore{PG: gw}
	f.srv = &Server{store: f.store, orphans: newOrphanTally(orphanWindow)}
	return f
}

func (f *fanIngest) scopes() storage.IdentityBindingScopes {
	all := scope.Set{All: true}
	return storage.IdentityBindingScopes{ComponentRead: all, ComponentUpdate: all, SystemRead: all, SystemUpdate: all}
}

func (f *fanIngest) bind(t *testing.T, value, system string) {
	t.Helper()
	if _, err := f.gw.PutIdentityBinding(f.ctx, f.actor, "meetings-cloud", "ucc-room-id", value, storage.IdentityBindingOwner{Kind: "system", Name: system}, f.scopes()); err != nil {
		t.Fatalf("bind %s: %v", value, err)
	}
}

// deliver hands a batch to the real handler as JetStream would and fails
// unless it was acked.
func (f *fanIngest) deliver(t *testing.T, subject string, ev *ogv1.TelemetryBatch, id string) {
	t.Helper()
	data, err := proto.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		f.seq++
		id = fmt.Sprintf("fan-%d", f.seq)
	}
	m := &deliveredMsg{subject: subject, data: data, header: nats.Header{jetstream.MsgIDHeader: []string{id}}}
	f.srv.handleTelemetry(m)
	if !m.acked || m.termed || m.naked {
		t.Fatalf("batch %s: acked=%v termed=%v naked=%v", id, m.acked, m.termed, m.naked)
	}
}

// rooms is the room list as both arrivals carry it: each room's call state,
// named by its room id, and one unidentified sample about the conduit itself.
func rooms(ids ...string) *ogv1.TelemetryBatch {
	ev := &ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "participant-count", Value: 1}}}
	for i, id := range ids {
		state := "active"
		if i%2 == 1 {
			state = "idle"
		}
		ev.Properties = append(ev.Properties, &ogv1.PropertySample{Name: "call-state", ValueJson: `"` + state + `"`, Identity: room(id)})
		ev.Metrics = append(ev.Metrics, &ogv1.MetricSample{Name: "participant-count", Value: float64(i + 2), Identity: room(id)})
	}
	return ev
}

func (f *fanIngest) nodeLane(ev *ogv1.TelemetryBatch) (string, *ogv1.TelemetryBatch) {
	ev.TaskId, ev.NodeId = "t-rooms", "node-a"
	return collection.TelemetrySubject("node-a"), ev
}

func (f *fanIngest) webhookLane(ev *ogv1.TelemetryBatch) (string, *ogv1.TelemetryBatch) {
	ev.Owner, ev.Source = &ogv1.Owner{Kind: "component", Ref: f.conduit}, "http"
	return collection.APITelemetrySubject, ev
}

// landed is every observed row on a system, as owner key value instance
// source, sorted.
func (f *fanIngest) landed(t *testing.T) []string {
	t.Helper()
	rows, err := f.conn.Query(f.ctx, `
		select s.name, t.name, p.value #>> '{}', p.instance, p.source, p.provenance from property p
			join system s on s.id = p.system_id join property_type t on t.id = p.property_type_id where p.provenance = 'observed'
		union all
		select s.name, t.name, m.value::text, m.instance, m.source, m.provenance from metric m
			join system s on s.id = m.system_id join metric_type t on t.id = m.metric_type_id where m.provenance = 'observed'`)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var a, b, c, d, e, g string
		if err := rows.Scan(&a, &b, &c, &d, &e, &g); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join([]string{a, b, c, "[" + d + "]", e, g}, " "))
	}
	sort.Strings(out)
	return out
}

func (f *fanIngest) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.conn.QueryRow(f.ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// TestBothArrivalsFanOutTheSameRows is the epic's acceptance (#1282): a
// node polling the conduit's room list and the conduit posting the same
// list to its webhook land the same rows, each room's values on its bound
// system, observed, at the empty instance, naming the conduit and its
// endpoint in source; the unidentified sample stays on the conduit; the
// unbound room lands nowhere and waits in the conduit's inbox.
func TestBothArrivalsFanOutTheSameRows(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	got := map[string][]string{}
	for _, lane := range []string{"node", "webhook"} {
		f := newFanIngest(t)
		subject, ev := f.nodeLane(rooms("zr-a", "zr-b", "zr-x"))
		if lane == "webhook" {
			subject, ev = f.webhookLane(rooms("zr-a", "zr-b", "zr-x"))
		}
		f.deliver(t, subject, ev, "")
		got[lane] = f.landed(t)
		if n := f.count(t, `select count(*) from metric where component_id = $1`, f.conduit); n != 1 {
			t.Errorf("%s: the conduit holds %d metric rows, want its one unidentified sample", lane, n)
		}
		if n := f.count(t, `select count(*) from property where component_id = $1`, f.conduit); n != 0 {
			t.Errorf("%s: an identified value landed on the conduit (%d rows)", lane, n)
		}
		// The owner-level read shows it: an empty instance is the owner's
		// own series, never a retired observer.
		effs, err := f.gw.EffectiveProperties(f.ctx, "system", "room-a", scope.Set{All: true})
		if err != nil {
			t.Fatal(err)
		}
		shown := false
		for _, e := range effs {
			if e.PropertyTypeName == "call-state" && e.Reconciliation != nil && string(e.Reconciliation.Is) == `"active"` {
				shown = true
			}
		}
		if !shown {
			t.Errorf("%s: room-a's properties read %+v, want call-state is active", lane, effs)
		}
		inbox, err := f.gw.ListUnboundIdentities(f.ctx, "meetings-cloud", scope.Set{All: true})
		if err != nil || len(inbox) != 1 || inbox[0].Value != "zr-x" || inbox[0].Count != 2 {
			t.Errorf("%s: inbox = %+v (%v), want zr-x heard twice", lane, inbox, err)
		}
	}
	want := []string{
		"room-a call-state active [] meetings-cloud/http observed",
		"room-a participant-count 2 [] meetings-cloud/http observed",
		"room-b call-state idle [] meetings-cloud/http observed",
		"room-b participant-count 3 [] meetings-cloud/http observed",
	}
	for lane, rows := range got {
		if strings.Join(rows, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s lane landed:\n%s\nwant:\n%s", lane, strings.Join(rows, "\n"), strings.Join(want, "\n"))
		}
	}
}

// TestFanOutFollowsTheBindings: binding an unbound identity makes its next
// value land on the owner with no restart; deleting the binding makes the
// next one unbound again; a redelivery lands nothing twice; a node-probe key
// named under a bound identity is never written on the owner.
func TestFanOutFollowsTheBindings(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newFanIngest(t)
	subject, ev := f.nodeLane(rooms("zr-x"))
	f.deliver(t, subject, ev, "first")
	f.deliver(t, subject, ev, "first") // a redelivery
	inbox, _ := f.gw.ListUnboundIdentities(f.ctx, "meetings-cloud", scope.Set{All: true})
	if len(inbox) != 1 || inbox[0].Count != 2 {
		t.Fatalf("inbox = %+v, want zr-x heard twice once (the redelivery counted nothing)", inbox)
	}

	f.bind(t, "zr-x", "room-c")
	subject, ev = f.nodeLane(rooms("zr-x"))
	ev.Metrics = append(ev.Metrics, &ogv1.MetricSample{Name: "tcp-open", Value: 1, Identity: room("zr-x")})
	f.deliver(t, subject, ev, "")
	if n := f.count(t, `select count(*) from property p join system s on s.id = p.system_id where s.name = 'room-c' and p.provenance = 'observed'`); n != 1 {
		t.Errorf("after binding, room-c holds %d call-state rows, want one", n)
	}
	if n := f.count(t, `select count(*) from metric m join metric_type t on t.id = m.metric_type_id where t.name = 'tcp-open'`); n != 0 {
		t.Errorf("a node-probe key under a bound identity landed %d rows", n)
	}

	if err := f.gw.DeleteIdentityBinding(f.ctx, f.actor, "meetings-cloud", "ucc-room-id", "zr-x", f.scopes()); err != nil {
		t.Fatal(err)
	}
	subject, ev = f.nodeLane(rooms("zr-x"))
	f.deliver(t, subject, ev, "")
	if n := f.count(t, `select count(*) from metric m join system s on s.id = m.system_id where s.name = 'room-c'`); n != 1 {
		t.Errorf("after unbinding, room-c holds %d metric rows, want the one landed while bound", n)
	}
	inbox, _ = f.gw.ListUnboundIdentities(f.ctx, "meetings-cloud", scope.Set{All: true})
	if len(inbox) != 1 || inbox[0].Value != "zr-x" {
		t.Errorf("after unbinding, inbox = %+v, want zr-x again", inbox)
	}
}

// TestConfinementComesBeforeTheFanOut: a node cannot land a fanned value
// through another node's task: the batch is an orphan, dropped before any
// identity is read, and nothing lands on the bound owners.
func TestConfinementComesBeforeTheFanOut(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newFanIngest(t)
	ev := rooms("zr-a", "zr-b")
	ev.TaskId, ev.NodeId = "t-rooms", "node-b"
	f.deliver(t, collection.TelemetrySubject("node-b"), ev, "")
	if n := f.store.resolves.Load(); n != 0 {
		t.Errorf("an orphan batch's identities were read %d times, want none", n)
	}
	if rows := f.landed(t); len(rows) != 0 {
		t.Errorf("an orphan batch landed %v on the bound owners", rows)
	}
	// And a node's own task on another component cannot reach this
	// conduit's bindings: its identities resolve on its own conduit.
	ev = rooms("zr-a")
	ev.TaskId, ev.NodeId = "t-other", "node-b"
	f.deliver(t, collection.TelemetrySubject("node-b"), ev, "")
	if rows := f.landed(t); len(rows) != 0 {
		t.Errorf("another conduit's batch landed %v on meetings-cloud's owners", rows)
	}
}

// TestFanOutResolvesOwnersInOneRead: a batch of two hundred identified
// records, each about its own bound system, issues exactly the statements a
// batch of three does: the identities resolve in one read, the gateway's
// fence is one read, and each owner's arc comes with it (#1282). Measured on
// the metric lane, which lands in one statement however many rows; the
// property lane writes a row per statement, as it always has.
func TestFanOutResolvesOwnersInOneRead(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	counter := querycount.New()
	f := newFanIngest(t, storage.WithQueryTracer(counter))
	all := scope.Set{All: true}
	for i := range 200 {
		name := fmt.Sprintf("room-%03d", i)
		if _, err := f.gw.CreateSystem(f.ctx, "", storage.SystemSpec{Name: name}, all, all); err != nil {
			t.Fatal(err)
		}
		f.bind(t, fmt.Sprintf("zr-%03d", i), name)
	}
	statements := func(n int) int {
		ev := &ogv1.TelemetryBatch{}
		for i := range n {
			ev.Metrics = append(ev.Metrics, &ogv1.MetricSample{Name: "participant-count", Value: float64(i), Identity: room(fmt.Sprintf("zr-%03d", i))})
		}
		subject, ev := f.nodeLane(ev)
		f.deliver(t, subject, ev, "") // warm the registry and the first rows
		counter.Reset()
		subject, ev = f.nodeLane(ev)
		f.deliver(t, subject, ev, "")
		return counter.N()
	}
	three, many := statements(3), statements(200)
	if three != many {
		t.Errorf("a batch of 200 identified records issued %d statements, a batch of 3 issued %d: the fan-out reads per owner\n%s",
			many, three, strings.Join(counter.Summary(), "\n"))
	}
	if n := f.count(t, `select count(*) from metric m join system s on s.id = m.system_id`); n != 2*3+2*200 {
		t.Errorf("landed %d fanned metric rows, want %d", n, 2*3+2*200)
	}
}

// TestAnOwnersRuleJudgesAFannedValue: a fanned value is the owner's sample
// like any other, so an event rule over its key raises the alarm on the
// owner it fanned out to, never on the conduit. Owned here by a component:
// event rules do not yet judge a system's samples (#1002), so a system
// owner's rule waits on that issue.
func TestAnOwnersRuleJudgesAFannedValue(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newFanIngest(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateComponent(f.ctx, "", storage.ComponentSpec{Name: "bar-p"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.PutIdentityBinding(f.ctx, f.actor, "meetings-cloud", "ucc-room-id", "zr-p",
		storage.IdentityBindingOwner{Kind: "component", Name: "bar-p"}, f.scopes()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateEventRule(f.ctx, "", storage.EventRuleSpec{Name: "in-a-call", SampleKey: "call-state",
		Fire: rules.Criteria{Expr: `value == "active"`}, Severity: "high", FireEventType: "condition-started"}, all); err != nil {
		t.Fatal(err)
	}
	subject, ev := f.webhookLane(&ogv1.TelemetryBatch{Properties: []*ogv1.PropertySample{
		{Name: "call-state", ValueJson: `"active"`, Identity: room("zr-p")}}})
	f.deliver(t, subject, ev, "")
	got, err := f.gw.ListAlarms(f.ctx, "bar-p", storage.AlarmFilter{})
	if err != nil || len(got) != 1 {
		t.Fatalf("alarms on the owner = %+v (%v), want the rule's one alarm", got, err)
	}
	if conduit, _ := f.gw.ListAlarms(f.ctx, "meetings-cloud", storage.AlarmFilter{}); len(conduit) != 0 {
		t.Errorf("the conduit raised %d alarms for a value it fanned out", len(conduit))
	}
}
