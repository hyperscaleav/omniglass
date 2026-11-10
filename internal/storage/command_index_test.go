package storage_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/accesspath"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
	"github.com/jackc/pgx/v5"
)

// commandDeliverableVersion is the migration that adds the index a node's
// command pull reads by (#1631).
const commandDeliverableVersion = "20261109231119"

// commandPullFixture is one node with one driver-attached component, the
// gateway counting every statement it issues, and an actor to issue commands
// as: the smallest fleet a node's command pull has anything to deliver in.
type commandPullFixture struct {
	ctx     context.Context
	dsn     string
	gw      *storage.PG
	counter *querycount.Counter
	actor   string
}

func newCommandPullFixture(t *testing.T) *commandPullFixture {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	counter := querycount.New()
	gw, err := storage.NewPG(ctx, dsn, storage.WithQueryTracer(counter))
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	comp, drv, node := "pull-switcher", "line-proto", "pull-edge"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: node}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: drv, Label: "Line Proto", Version: "1.0.0", Spec: []byte(lineProtoSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &node, Inputs: map[string]string{"host": "10.20.4.50"},
	}, all); err != nil {
		t.Fatalf("attach: %v", err)
	}
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	conn := connectDSN(t, dsn)
	return &commandPullFixture{ctx: ctx, dsn: dsn, gw: gw, counter: counter,
		actor: scalar(t, conn, `select principal_id from human where username = 'root'`)}
}

// issue records n deliverable commands on the fixture's component.
func (f *commandPullFixture) issue(t *testing.T, n int) {
	t.Helper()
	for range n {
		if _, err := f.gw.IssueCommand(f.ctx, f.actor, "component", "pull-switcher", "set-input", "", json.RawMessage(`"hdmi-1"`), nil); err != nil {
			t.Fatalf("issue: %v", err)
		}
	}
}

// pull runs one command pull for the fixture's node with the counter reset, so
// the counter holds exactly the statements that one pull issued.
func (f *commandPullFixture) pull(t *testing.T) []storage.CommandDelivery {
	t.Helper()
	f.counter.Reset()
	got, err := f.gw.PendingNodeCommands(f.ctx, "pull-edge")
	if err != nil {
		t.Fatalf("PendingNodeCommands: %v", err)
	}
	return got
}

// TestCommandPullReachesTheDeliverableIndex holds a node's command pull to an
// access path keyed by its own components (#1631). Every node pulls once a
// heartbeat, so a pull that read the fleet's pending commands and filtered to
// the node afterwards cost every node the whole fleet's backlog. The proof is
// the accesspath instrument's usability question, not a preference: the scan
// of command reaches command_deliverable_idx with an index condition on
// component_id, so the rows the pull reads are found BY the node's own
// components rather than read and discarded. The statement is the one the
// gateway really issued, captured off the pool tracer.
//
// The pull reads only issued commands (#1630), so command_issued_component_idx
// (status = 'issued', keyed by component) is a usable path too, and on a
// fixture with no statistics the planner may take either. Which one it takes is
// a preference, which accesspath must never assert. So the test asks two
// usability questions: whatever index the planner picks, the command scan is
// searched by component; and with the settlement index out of the way inside a
// rolled-back transaction, command_deliverable_idx is still reachable, which is
// what breaks if its predicate stops being implied by the pull's clauses.
func TestCommandPullReachesTheDeliverableIndex(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newCommandPullFixture(t)
	f.issue(t, 1)
	if got := f.pull(t); len(got) != 1 {
		t.Fatalf("pull delivered %d commands, want the one issued", len(got))
	}
	call := oneCall(t, f.counter, "distinct on (c.id)")

	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(f.ctx) }()
	plan, err := accesspath.Explain(f.ctx, conn, call.SQL, call.Args...)
	if err != nil {
		t.Fatalf("explain the captured statement: %v", err)
	}
	scans := plan.Scans("command")
	if len(scans) == 0 {
		t.Fatalf("the plan never scans command:\n\t%s", plan)
	}
	for _, s := range scans {
		if s.Disabled || !strings.Contains(s.Cond, "component_id") {
			t.Errorf("the command scan is not searched by component, so a pull still reads other nodes' commands: %s", s)
		}
	}

	tx, err := conn.Begin(f.ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if _, err := tx.Exec(f.ctx, "drop index command_issued_component_idx"); err != nil {
		t.Fatalf("set the settlement index aside: %v", err)
	}
	plan, err = accesspath.Explain(f.ctx, conn, call.SQL, call.Args...)
	if err != nil {
		t.Fatalf("explain the captured statement: %v", err)
	}
	if ok, why := plan.MustReach("command", "command_deliverable_idx"); !ok {
		t.Fatalf("the command pull cannot reach the index it exists for: %s", why)
	}
}

// TestCommandPullStatementsDoNotGrowWithItsDeliveries holds a pull's round
// trips flat in the number of commands it delivers (#1631): the dispatched
// stamps land in one statement, not one per command. A command's FIRST take
// also writes its own dispatch-latency sample (#1626), one per command because
// delivery loss counts them, so the flat count is held on redeliveries, which
// stamp without sampling: a pull redelivering 21 commands issues exactly the
// statements of one redelivering 1, and a pull of 20 first takes stamps them
// all in the one statement.
func TestCommandPullStatementsDoNotGrowWithItsDeliveries(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newCommandPullFixture(t)
	conn := connectDSN(t, f.dsn)
	// overdue backdates every pending command's last take past redeliverAfter,
	// so the next pull redelivers it.
	overdue := func() {
		t.Helper()
		if _, err := conn.Exec(f.ctx, `update command set dispatched_at = dispatched_at - interval '31 seconds'
			where executed_at is null and dispatched_at is not null`); err != nil {
			t.Fatalf("backdate dispatch: %v", err)
		}
	}
	stamps := func() int {
		n := 0
		for _, c := range f.counter.Calls() {
			if strings.Contains(c.SQL, "from unnest($1::bigint[]") {
				n++
			}
		}
		return n
	}

	f.issue(t, 1)
	if got := f.pull(t); len(got) != 1 {
		t.Fatalf("first pull delivered %d commands, want 1", len(got))
	}
	overdue()
	if got := f.pull(t); len(got) != 1 {
		t.Fatalf("redelivering pull delivered %d commands, want 1", len(got))
	}
	one, oneSummary := f.counter.N(), f.counter.Summary()

	f.issue(t, 20)
	if got := f.pull(t); len(got) != 20 {
		t.Fatalf("pull of 20 first takes delivered %d commands, want 20", len(got))
	}
	if n := stamps(); n != 1 {
		t.Fatalf("a pull delivering 20 commands stamped them in %d statements, want 1:\n\t%s",
			n, strings.Join(f.counter.Summary(), "\n\t"))
	}

	overdue()
	if got := f.pull(t); len(got) != 21 {
		t.Fatalf("redelivering pull delivered %d commands, want 21", len(got))
	}
	if many := f.counter.N(); many != one {
		t.Fatalf("a pull redelivering 1 command issued %d statements and one redelivering 21 issued %d:\n\t1: %s\n\t21: %s",
			one, many, strings.Join(oneSummary, "\n\t   "), strings.Join(f.counter.Summary(), "\n\t    "))
	}
	// The batched stamp landed on every delivered row: an immediate re-pull
	// redelivers nothing.
	if again := f.pull(t); len(again) != 0 {
		t.Fatalf("immediate re-pull delivered %d commands, want none (all were stamped dispatched)", len(again))
	}
}

// TestCommandDeliverableIndexMigrationRoundTrips rolls the migration back,
// proves the index goes with it, and reapplies it (#1631).
func TestCommandDeliverableIndexMigrationRoundTrips(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	conn := connectDSN(t, dsn)
	const exists = `select count(*) from pg_indexes where indexname = 'command_deliverable_idx'`
	if count(t, conn, exists) != 1 {
		t.Fatal("command_deliverable_idx is missing after migrate up")
	}
	// The pull reads only issued commands (#1630), so the index holds only
	// those: a terminal command left unexecuted is no row a pull can deliver.
	var def string
	if err := conn.QueryRow(context.Background(), `select indexdef from pg_indexes where indexname = 'command_deliverable_idx'`).Scan(&def); err != nil {
		t.Fatalf("read the index definition: %v", err)
	}
	if !strings.Contains(def, "status") || !strings.Contains(def, "'issued'") {
		t.Errorf("command_deliverable_idx holds terminal commands too: %s", def)
	}
	if err := migrate.RollbackBelow(dsn, commandDeliverableVersion); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	if count(t, conn, exists) != 0 {
		t.Fatal("command_deliverable_idx survived its migration's rollback")
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("reapply: %v", err)
	}
	if count(t, conn, exists) != 1 {
		t.Fatal("command_deliverable_idx is missing after reapplying its migration")
	}
}
