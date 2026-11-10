package storage_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// commandQueueSpec is lineProtoSpec binding reboot too, so a fire-and-forget
// command (reboot settles at issue) has an endpoint to be delivered over.
var commandQueueSpec = strings.Replace(lineProtoSpec,
	`{"command_type": "set-input", "request": {"line": "SET INPUT ${value}", "reply": {"prefix": "INPUT "}}}`,
	`{"command_type": "set-input", "request": {"line": "SET INPUT ${value}", "reply": {"prefix": "INPUT "}}},
		{"command_type": "reboot", "request": {"line": "REBOOT"}}`, 1)

// commandQueueFixture is a fleet of nodes, each actuating one switcher
// (switcher-<node>) over the line-proto driver, with an owner to issue
// commands as and a raw connection to age the rows past the gateway.
type commandQueueFixture struct {
	gw       *storage.PG
	ctx      context.Context
	conn     *pgx.Conn
	actor    string
	platform map[string]string
	switcher map[string]string
}

func newCommandQueueFixture(t *testing.T, nodes ...string) commandQueueFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ctx, dsn := platformGatewayDSN(t)
	f := commandQueueFixture{gw: gw, ctx: ctx, platform: map[string]string{}, switcher: map[string]string{}}
	if commandQueueSpec == lineProtoSpec {
		t.Fatal("commandQueueSpec did not add the reboot binding")
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "line-proto", Label: "Line Proto", Version: "1.0.0", Spec: []byte(commandQueueSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	for i, n := range nodes {
		f.platform[n] = platformNode(t, ctx, gw, n).ID
		comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "switcher-" + n}, all, all, all, all)
		if err != nil {
			t.Fatalf("create component: %v", err)
		}
		f.switcher[n] = comp.ID
		name, drv, node := comp.Name, "line-proto", n
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
			Driver: &drv, Component: &name, Node: &node,
			Inputs: map[string]string{"host": "10.20.5." + string(rune('1'+i))},
		}, all); err != nil {
			t.Fatalf("attach %s: %v", n, err)
		}
	}
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	f.conn, err = pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { f.conn.Close(context.Background()) })
	if err := f.conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&f.actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	return f
}

// issue issues a command on node's switcher: set-input with an intent, or
// the fire-and-forget reboot.
func (f commandQueueFixture) issue(t *testing.T, node, commandType string) int64 {
	t.Helper()
	var intent json.RawMessage
	if commandType == "set-input" {
		intent = json.RawMessage(`"hdmi-2"`)
	}
	c, err := f.gw.IssueCommand(f.ctx, f.actor, "component", "switcher-"+node, commandType, "", intent, nil)
	if err != nil {
		t.Fatalf("issue %s on %s: %v", commandType, node, err)
	}
	return c.ID
}

func (f commandQueueFixture) pull(t *testing.T, node string) []storage.CommandDelivery {
	t.Helper()
	got, err := f.gw.PendingNodeCommands(f.ctx, node)
	if err != nil {
		t.Fatalf("pull %s: %v", node, err)
	}
	return got
}

func (f commandQueueFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.conn.Exec(f.ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// health reads the fleet's command queues for the window since, keyed by
// node.
func (f commandQueueFixture) health(t *testing.T, since time.Time) (storage.CommandQueueRead, map[string]storage.CommandQueue) {
	t.Helper()
	read, err := f.gw.CommandQueueHealth(f.ctx, since)
	if err != nil {
		t.Fatalf("command queue health: %v", err)
	}
	byNode := map[string]storage.CommandQueue{}
	for _, q := range read.Nodes {
		byNode[q.Node] = q
	}
	return read, byNode
}

// latencySamples counts key's samples on component, each observed and with
// no collector: the process wrote it, not a node's task.
func (f commandQueueFixture) latencySamples(t *testing.T, component, key string) []float64 {
	t.Helper()
	rows, err := f.conn.Query(f.ctx, `select m.value, m.provenance, m.node_id is null
		from `+storagetest.MetricRows+` m join metric_type t on t.id = m.metric_type_id
		where m.component_id = $1::uuid and t.name = $2`, component, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var v float64
		var provenance string
		var noCollector bool
		if err := rows.Scan(&v, &provenance, &noCollector); err != nil {
			t.Fatal(err)
		}
		if provenance != "observed" || !noCollector {
			t.Errorf("%s landed %s with a collector %v; want observed, written by the process", key, provenance, !noCollector)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestCommandRedeliveryKeepsTheFirstDispatch (#1626): a command redelivered
// after redeliverAfter keeps when it was first taken, advances when it was
// last taken, and counts both deliveries; the command read serves both.
func TestCommandRedeliveryKeepsTheFirstDispatch(t *testing.T) {
	t.Parallel()
	f := newCommandQueueFixture(t, "edge-r")
	id := f.issue(t, "edge-r", "set-input")
	if got := f.pull(t, "edge-r"); len(got) != 1 {
		t.Fatalf("first pull = %+v, want the command", got)
	}
	stamps := func() (first, last *time.Time, count int) {
		t.Helper()
		if err := f.conn.QueryRow(f.ctx, `select first_dispatched_at, dispatched_at, delivery_count from command where id = $1`, id).
			Scan(&first, &last, &count); err != nil {
			t.Fatalf("read stamps: %v", err)
		}
		return
	}
	first, last, count := stamps()
	if first == nil || last == nil || !first.Equal(*last) || count != 1 {
		t.Fatalf("after one delivery first=%v last=%v count=%d, want first = last and one delivery", first, last, count)
	}
	// The node went silent: age both stamps past redeliverAfter.
	f.exec(t, `update command set dispatched_at = dispatched_at - interval '31 seconds',
		first_dispatched_at = first_dispatched_at - interval '31 seconds' where id = $1`, id)
	firstTaken, _, _ := stamps()
	if got := f.pull(t, "edge-r"); len(got) != 1 || got[0].ID != id {
		t.Fatalf("pull after silence = %+v, want the command redelivered", got)
	}
	first, last, count = stamps()
	if first == nil || !first.Equal(*firstTaken) {
		t.Errorf("first_dispatched_at = %v, want it kept at %v", first, firstTaken)
	}
	if last == nil || !last.After(*first) {
		t.Errorf("dispatched_at = %v, want it advanced past the first %v", last, first)
	}
	if count != 2 {
		t.Errorf("delivery_count = %d, want 2", count)
	}

	rec, err := f.gw.GetComponentCommand(f.ctx, f.switcher["edge-r"], id)
	if err != nil {
		t.Fatalf("read the command: %v", err)
	}
	if rec.FirstDeliveredAt == nil || !rec.FirstDeliveredAt.Equal(*first) || rec.DeliveryCount != 2 {
		t.Errorf("read first delivered %v count %d, want %v and 2", rec.FirstDeliveredAt, rec.DeliveryCount, first)
	}
	if rec.DeliveredAt == nil || !rec.DeliveredAt.Equal(*last) {
		t.Errorf("read delivered %v, want the last taken %v", rec.DeliveredAt, last)
	}
}

// TestCommandPendingCountAgreesWithThePull (#1626): the health read and the
// pull resolve the same candidates, so each node's pending count is what its
// pull returns, and stays counted while the node owes its report. A command
// no endpoint binds is no node's.
func TestCommandPendingCountAgreesWithThePull(t *testing.T) {
	t.Parallel()
	f := newCommandQueueFixture(t, "edge-a", "edge-b", "edge-c")
	f.issue(t, "edge-a", "set-input")
	f.issue(t, "edge-a", "reboot")
	f.issue(t, "edge-b", "set-input")
	if _, err := f.gw.IssueCommand(f.ctx, f.actor, "component", "switcher-edge-c", "set-mute", "", json.RawMessage(`"true"`), nil); err != nil {
		t.Fatalf("issue an unbound command: %v", err)
	}

	read, before := f.health(t, time.Time{})
	if read.Now.IsZero() {
		t.Fatal("the health read carries no database time")
	}
	if len(before) != 3 {
		t.Fatalf("health read %+v, want every node with a platform component, an idle one at zero", read.Nodes)
	}
	pulled := map[string]int{}
	for _, n := range []string{"edge-a", "edge-b", "edge-c"} {
		pulled[n] = len(f.pull(t, n))
	}
	for n, want := range map[string]int{"edge-a": 2, "edge-b": 1, "edge-c": 0} {
		if pulled[n] != want {
			t.Errorf("%s pulled %d, want %d", n, pulled[n], want)
		}
		if before[n].Pending != pulled[n] {
			t.Errorf("%s pending %d before its pull, which returned %d", n, before[n].Pending, pulled[n])
		}
		if before[n].ComponentID != f.platform[n] {
			t.Errorf("%s component %s, want its platform component %s", n, before[n].ComponentID, f.platform[n])
		}
	}
	_, after := f.health(t, time.Time{})
	for _, n := range []string{"edge-a", "edge-b"} {
		if after[n].Pending != pulled[n] || after[n].Oldest == nil {
			t.Errorf("%s after its pull: %+v, want %d still awaiting a report, with an oldest", n, after[n], pulled[n])
		}
	}
	if after["edge-c"].Oldest != nil {
		t.Errorf("edge-c has an oldest %v with nothing pending", after["edge-c"].Oldest)
	}

	// The read maps to the gauges on each node's platform component: an
	// idle queue's age at zero, no windowed count on the first pass.
	metrics := storage.CommandQueueMetrics(read, time.Time{})
	got := map[string]map[string]float64{}
	for _, m := range metrics {
		if got[m.ComponentID] == nil {
			got[m.ComponentID] = map[string]float64{}
		}
		if !m.TS.Equal(read.Now) {
			t.Errorf("%s at %v, want the read's time %v", m.Key, m.TS, read.Now)
		}
		got[m.ComponentID][m.Key+"/"+m.Instance] = m.Value
	}
	if got[f.platform["edge-a"]][writerclass.CommandPendingCount+"/"] != 2 || got[f.platform["edge-c"]][writerclass.CommandPendingCount+"/"] != 0 {
		t.Errorf("pending gauges %v, want 2 on edge-a and 0 on edge-c", got)
	}
	if _, ok := got[f.platform["edge-a"]][writerclass.CommandPendingAge+"/"]; !ok {
		t.Errorf("edge-a carries no pending age: %v", got[f.platform["edge-a"]])
	}
	if age, ok := got[f.platform["edge-c"]][writerclass.CommandPendingAge+"/"]; !ok || age != 0 {
		t.Errorf("edge-c pending age = %v (written %v), want 0 with nothing pending", age, ok)
	}
	for _, m := range metrics {
		if m.Key == writerclass.CommandRedeliveryCount || m.Key == writerclass.CommandExpiredCount {
			t.Errorf("the first pass emits windowed %s", m.Key)
		}
	}
	if err := f.gw.RecordPlatformMetrics(f.ctx, metrics); err != nil {
		t.Fatalf("record the gauges: %v", err)
	}
	m, err := f.gw.LatestMetric(f.ctx, f.platform["edge-a"], writerclass.CommandPendingCount)
	if err != nil || m == nil || m.Value != 2 {
		t.Errorf("edge-a command-pending-count = %+v (%v), want 2", m, err)
	}
}

// TestCommandLatencySamplesLandOnThePlatformComponent (#1626): the first
// dispatch writes one dispatch-latency sample and the execution report one
// execution-latency sample, on the node's platform component, as the
// process. A redelivery writes no second dispatch sample.
func TestCommandLatencySamplesLandOnThePlatformComponent(t *testing.T) {
	t.Parallel()
	f := newCommandQueueFixture(t, "edge-l")
	id := f.issue(t, "edge-l", "set-input")
	// Issued a second ago, so the dispatch latency is measurable.
	f.exec(t, `update command set ts = ts - interval '1 second' where id = $1`, id)
	f.pull(t, "edge-l")
	f.exec(t, `update command set dispatched_at = dispatched_at - interval '31 seconds' where id = $1`, id)
	if got := f.pull(t, "edge-l"); len(got) != 1 {
		t.Fatalf("redelivery = %+v, want the command", got)
	}
	platform := f.platform["edge-l"]
	dispatch := f.latencySamples(t, platform, writerclass.CommandDispatchLatency)
	if len(dispatch) != 1 || dispatch[0] < 1000 || dispatch[0] > 60000 {
		t.Fatalf("dispatch latency samples %v, want one of about a second, in ms", dispatch)
	}
	if got := f.latencySamples(t, platform, writerclass.CommandExecutionLatency); len(got) != 0 {
		t.Fatalf("execution latency %v before any report", got)
	}
	if err := f.gw.RecordCommandExecution(f.ctx, "edge-l", id, "", "INPUT hdmi-2"); err != nil {
		t.Fatalf("record execution: %v", err)
	}
	execution := f.latencySamples(t, platform, writerclass.CommandExecutionLatency)
	if len(execution) != 1 || execution[0] < 0 {
		t.Fatalf("execution latency samples %v, want one, non-negative", execution)
	}
	var want float64
	if err := f.conn.QueryRow(f.ctx, `select extract(epoch from executed_at - first_dispatched_at) * 1000 from command where id = $1`, id).Scan(&want); err != nil {
		t.Fatal(err)
	}
	if diff := execution[0] - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("execution latency %v ms, want executed_at - first_dispatched_at = %v ms", execution[0], want)
	}
}

// TestCommandFirstTakesInOnePullEachKeepTheirSample: commands first taken
// in one pull are stamped in one statement at one now() (#1631), yet each
// writes its own dispatch-latency sample (#1626), all at the same instant on
// one series. A pull that wrote one sample for its batch would undercount the
// dispatches the delivery loss ratio divides by.
func TestCommandFirstTakesInOnePullEachKeepTheirSample(t *testing.T) {
	t.Parallel()
	f := newCommandQueueFixture(t, "edge-b")
	for range 3 {
		f.issue(t, "edge-b", "set-input")
	}
	if got := f.pull(t, "edge-b"); len(got) != 3 {
		t.Fatalf("pull = %+v, want the three commands", got)
	}
	if got := f.latencySamples(t, f.platform["edge-b"], writerclass.CommandDispatchLatency); len(got) != 3 {
		t.Fatalf("dispatch latency samples %v after three first takes in one pull, want three", got)
	}
}

// TestCommandStuckAndExpiredCounts (#1626): a command dispatched and silent
// past redeliverAfter is stuck; a command that crosses deliveryTTL
// unexecuted counts once, in the window it crossed, as undelivered (no node
// took it, a fire-and-forget reboot reading settled included) or unexecuted
// (taken, never reported). The count keys on executed_at and age, not status.
func TestCommandStuckAndExpiredCounts(t *testing.T) {
	t.Parallel()
	f := newCommandQueueFixture(t, "edge-s")
	taken := f.issue(t, "edge-s", "set-input")
	f.pull(t, "edge-s")
	f.exec(t, `update command set dispatched_at = dispatched_at - interval '31 seconds' where id = $1`, taken)
	_, q := f.health(t, time.Time{})
	if q["edge-s"].Stuck != 1 {
		t.Fatalf("stuck = %d, want the silent dispatched command", q["edge-s"].Stuck)
	}

	ignored := f.issue(t, "edge-s", "reboot")
	var status string
	if err := f.conn.QueryRow(f.ctx, `select status from command where id = $1`, ignored).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "settled" {
		t.Fatalf("the reboot reads %q, want the fire-and-forget settled at issue", status)
	}
	first, _ := f.health(t, time.Time{})
	// Both cross deliveryTTL a moment after the first pass.
	f.exec(t, `update command set ts = $2::timestamptz - interval '10 minutes' + interval '1 millisecond'
		where id = any($1)`, []int64{taken, ignored}, first.Now)
	second, q := f.health(t, first.Now)
	if got := q["edge-s"]; got.ExpiredUndelivered != 1 || got.ExpiredUnexecuted != 1 || got.Pending != 0 || got.Stuck != 0 {
		t.Fatalf("after the TTL: %+v, want one expired undelivered, one unexecuted, nothing pending or stuck", got)
	}
	metrics := storage.CommandQueueMetrics(second, first.Now)
	expired := map[string]float64{}
	for _, m := range metrics {
		if m.Key == writerclass.CommandExpiredCount {
			expired[m.Instance] = m.Value
		}
	}
	if expired[writerclass.CommandExpiredUndelivered] != 1 || expired[writerclass.CommandExpiredUnexecuted] != 1 {
		t.Errorf("expired metrics %v, want undelivered 1 and unexecuted 1", expired)
	}
	_, q = f.health(t, second.Now)
	if got := q["edge-s"]; got.ExpiredUndelivered != 0 || got.ExpiredUnexecuted != 0 {
		t.Errorf("the next window counts them again: %+v", got)
	}
}

// redeliveredOver reads node's windows back to back from start until their
// redeliveries sum to want or the deadline passes, then reads one window more,
// which must count none: each redelivery is counted in exactly one window.
func (f commandQueueFixture) redeliveredOver(t *testing.T, node string, start storage.CommandQueueRead, want int) {
	t.Helper()
	prev, total := start, 0
	deadline := time.Now().Add(20 * time.Second)
	for total < want && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		read, q := f.health(t, prev.Now)
		total += q[node].Redelivered
		prev = read
	}
	if total != want {
		t.Fatalf("%s's windows counted %d redeliveries, want %d", node, total, want)
	}
	time.Sleep(200 * time.Millisecond)
	if _, q := f.health(t, prev.Now); q[node].Redelivered != 0 {
		t.Errorf("a later window counted %d redeliveries again", q[node].Redelivered)
	}
}

// TestCommandRedeliveryCountsInItsWindow (#1626): a command taken again
// counts as redelivered in exactly one window, executed since or not; one
// taken once does not.
func TestCommandRedeliveryCountsInItsWindow(t *testing.T) {
	t.Parallel()
	f := newCommandQueueFixture(t, "edge-w")
	again := f.issue(t, "edge-w", "set-input")
	f.issue(t, "edge-w", "reboot")
	start, _ := f.health(t, time.Time{})
	f.pull(t, "edge-w")
	f.exec(t, `update command set dispatched_at = dispatched_at - interval '31 seconds' where id = $1`, again)
	f.pull(t, "edge-w")
	if err := f.gw.RecordCommandExecution(f.ctx, "edge-w", again, "", "INPUT hdmi-2"); err != nil {
		t.Fatalf("record execution: %v", err)
	}
	f.redeliveredOver(t, "edge-w", start, 1)
}

// TestCommandRedeliveryCommittedAfterAReadIsCounted (#1626): a redelivery's
// stamp is its transaction's start, so a health read that runs while that
// transaction is still open cannot see it. Its count must land in a later
// window, not fall between two.
func TestCommandRedeliveryCommittedAfterAReadIsCounted(t *testing.T) {
	t.Parallel()
	f := newCommandQueueFixture(t, "edge-r")
	id := f.issue(t, "edge-r", "set-input")
	f.pull(t, "edge-r")
	start, _ := f.health(t, time.Time{})

	tx, err := f.conn.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	// The pull's own stamp, held open across a read.
	if _, err := tx.Exec(f.ctx, `update command set dispatched_at = now(), delivery_count = delivery_count + 1 where id = $1`, id); err != nil {
		t.Fatalf("stamp the redelivery: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	during, q := f.health(t, start.Now)
	if q["edge-r"].Redelivered != 0 {
		t.Fatalf("a read saw the uncommitted redelivery: %+v", q["edge-r"])
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatalf("commit the redelivery: %v", err)
	}
	f.redeliveredOver(t, "edge-r", during, 1)
}

// TestCommandQueueKeysRefuseOtherWriters (#1626): the command queue keys are
// the process's. A push, a node's task (a driver's collection), a calc rule,
// an operator and a node's report of itself are each refused; the
// pull-failure count alone rides the node-self lane.
func TestCommandQueueKeysRefuseOtherWriters(t *testing.T) {
	t.Parallel()
	f := newCommandQueueFixture(t, "edge-k")
	platform := f.platform["edge-k"]
	keys := []string{writerclass.CommandPendingCount, writerclass.CommandPendingAge, writerclass.CommandDispatchLatency,
		writerclass.CommandExecutionLatency, writerclass.CommandRedeliveryCount, writerclass.CommandStuckCount,
		writerclass.CommandExpiredCount, writerclass.CommandPullFailureCount}
	for _, key := range keys {
		sample := []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: platform, Key: key, Value: 1, TS: time.Now()}}
		for name, write := range map[string]func() error{
			"push":     func() error { return f.gw.InsertMetricSamples(f.ctx, storage.PushWriter(), sample) },
			"driver":   func() error { return f.gw.InsertMetricSamples(f.ctx, storage.NodeWriter(platform, ""), sample) },
			"calc":     func() error { return f.gw.AdmitWrite(f.ctx, "metric", writerclass.CalcRule, key, "") },
			"operator": func() error { return f.gw.AdmitWrite(f.ctx, "metric", writerclass.Operator, key, "") },
		} {
			if err := write(); !refused(err) {
				t.Errorf("%s writing %s = %v, want ErrKeyWriterRefused", name, key, err)
			}
		}
		if err := f.gw.RecordPlatformMetrics(f.ctx, []storage.PlatformMetric{{ComponentID: platform, Key: key, Value: 1, TS: time.Now()}}); err != nil {
			t.Errorf("the process writing %s = %v, want it admitted", key, err)
		}
	}
	if n := len(f.latencySamples(t, platform, writerclass.CommandPendingCount)); n != 1 {
		t.Errorf("%d command-pending-count rows, want the process's one", n)
	}
}

// TestCommandHealthReadUsesTheUnexecutedIndex (#1626): the health read runs
// every timer pass over a command table that only grows, so its plan reaches
// the unexecuted rows through a partial index holding only them, never a scan
// of every executed command. Either such index serves: command_unexecuted_idx,
// or #1631's command_deliverable_idx, which holds the same rows keyed by
// component and which the planner may prefer.
func TestCommandHealthReadUsesTheUnexecutedIndex(t *testing.T) {
	t.Parallel()
	f := newCommandQueueFixture(t, "edge-x")
	id := f.issue(t, "edge-x", "set-input")
	f.exec(t, `insert into command (ts, owner_kind, component_id, command_type_id, instance, params, status, actor,
			dispatched_at, executed_at, first_dispatched_at, delivery_count, settled_at)
		select c.ts - (g * interval '1 second'), c.owner_kind, c.component_id, c.command_type_id, c.instance, c.params,
			'settled', c.actor, c.ts, c.ts, c.ts, 1, c.ts
		from command c, generate_series(1, 20000) g where c.id = $1`, id)
	f.exec(t, `analyze command`)
	rows, err := f.conn.Query(f.ctx, "explain (costs off) "+storage.ExportCommandHealthSQL(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(plan, "\n")
	if !strings.Contains(text, "command_unexecuted_idx") && !strings.Contains(text, "command_deliverable_idx") {
		t.Errorf("the health read reaches the unexecuted commands through neither unexecuted index:\n%s", text)
	}
	if strings.Contains(text, "Seq Scan on command") {
		t.Errorf("the health read scans the command table:\n%s", text)
	}
}
