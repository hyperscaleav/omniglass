package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// settleRig is a component with a line-protocol driver attached on a node,
// whose set-input binding settles as settle declares.
type settleRig struct {
	ctx   context.Context
	gw    *storage.PG
	conn  *pgx.Conn
	actor string
}

func newSettleRig(t *testing.T, settle string) *settleRig {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "switcher"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatal(err)
	}
	settleField := ""
	if settle != "" {
		settleField = `, "settle": ` + settle
	}
	spec := fmt.Sprintf(`{"version": 1, "transport": "tcp",
		"inputs": [{"name": "host", "kind": "string", "required": true}, {"name": "port", "kind": "number", "default": "4998"}],
		"polls": [{"name": "status", "schedule": {"every": "30s"}, "request": {"line": "GET INPUT"},
			"emits": [{"name": "video-input", "extract": {"regex": "^INPUT (\\S+)$"}}]}],
		"commands": [{"command_type": "set-input", "request": {"line": "SET INPUT ${value}"}%s}]}`, settleField)
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "settle-proto", Label: "Settle Proto", Version: "1.0.0", Spec: []byte(spec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv, node := "switcher", "settle-proto", "edge"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &node,
		Inputs: map[string]string{"host": "10.20.4.60"}}, all); err != nil {
		t.Fatalf("attach: %v", err)
	}
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	r := &settleRig{ctx: ctx, gw: gw, conn: conn}
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&r.actor); err != nil {
		t.Fatal(err)
	}
	return r
}

// issueAndDispatch issues set-input and has the node pull it.
func (r *settleRig) issueAndDispatch(t *testing.T, input string) int64 {
	t.Helper()
	cmd, err := r.gw.IssueCommand(r.ctx, r.actor, "component", "switcher", "set-input", "", json.RawMessage(`"`+input+`"`), nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	got, err := r.gw.PendingNodeCommands(r.ctx, "edge")
	if err != nil || len(got) != 1 || got[0].ID != cmd.ID {
		t.Fatalf("dispatch = %+v (%v), want command %d", got, err, cmd.ID)
	}
	return cmd.ID
}

func (r *settleRig) status(t *testing.T, id int64) string {
	t.Helper()
	var s string
	if err := r.conn.QueryRow(r.ctx, `select status from command where id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

const valueReply = `{"from": "response", "extract": {"regex": "^INPUT (\\S+)$"}}`

// A binding that reads its reply's value settles the command on the reply,
// and the reply is recorded as an observation of the target, on the
// endpoint it came through.
func TestCommandSettlesFromItsResponse(t *testing.T) {
	r := newSettleRig(t, valueReply)
	id := r.issueAndDispatch(t, "hdmi3")
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", "INPUT hdmi3"); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, id); got != "settled" {
		t.Fatalf("status = %q, want settled from the reply", got)
	}
	latest, err := r.gw.LatestProperty(r.ctx, "switcher", "video-input", "tcp")
	if err != nil || latest == nil || latest.Value != "hdmi3" || latest.Provenance != "observed" {
		t.Fatalf("observed video-input on the endpoint = %+v (%v), want hdmi3", latest, err)
	}
}

// A reply carrying another value, no value, or an execution error fails the
// command at once.
func TestCommandFailsOnAContraryResponse(t *testing.T) {
	r := newSettleRig(t, valueReply)
	for _, tc := range []struct{ reply, execErr string }{
		{"INPUT hdmi1", ""},
		{"-ERR busy", ""},
		{"", "collection: line dial: connection refused"},
	} {
		id := r.issueAndDispatch(t, "hdmi3")
		if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, tc.execErr, tc.reply); err != nil {
			t.Fatal(err)
		}
		if got := r.status(t, id); got != "failed" {
			t.Errorf("reply %q err %q: status = %q, want failed", tc.reply, tc.execErr, got)
		}
	}
}

// A binding whose reply only confirms settles on a matching reply and fails
// on any other.
func TestCommandSettlesFromAConfirmation(t *testing.T) {
	r := newSettleRig(t, `{"from": "response", "success": {"regex": "^OK$"}}`)
	ok := r.issueAndDispatch(t, "hdmi2")
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", ok, "", "OK"); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, ok); got != "settled" {
		t.Fatalf("confirmed: status = %q, want settled", got)
	}
	nak := r.issueAndDispatch(t, "hdmi3")
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", nak, "", "NAK"); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, nak); got != "failed" {
		t.Fatalf("refused: status = %q, want failed", got)
	}
}

// A binding that settles on observation leaves the reply undecided; the
// next observation of the target on the dispatched endpoint settles it,
// though the command was issued with no instance of its own.
func TestCommandSettlesOnTheNextObservation(t *testing.T) {
	r := newSettleRig(t, "")
	id := r.issueAndDispatch(t, "hdmi3")
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", "OK"); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, id); got != "issued" {
		t.Fatalf("status after the reply = %q, want issued (observe decides)", got)
	}
	if err := r.gw.InsertPropertySamples(r.ctx, []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "switcher", Key: "video-input", Instance: "tcp", Value: "hdmi3", Source: "tcp",
	}}); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, id); got != "settled" {
		t.Fatalf("status after the observation = %q, want settled", got)
	}
}

// A command nothing settles is swept to timed-out once its window passes,
// without anyone issuing another.
func TestSweepTimesOutAnUnsettledCommand(t *testing.T) {
	r := newSettleRig(t, "")
	if _, err := r.conn.Exec(r.ctx, `update command_type set settle_window_seconds = 1 where name = 'set-input'`); err != nil {
		t.Fatal(err)
	}
	id := r.issueAndDispatch(t, "hdmi3")
	if err := r.gw.SweepCommandSettlements(r.ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, id); got != "issued" {
		t.Fatalf("status inside the window = %q, want issued", got)
	}
	time.Sleep(1500 * time.Millisecond)
	if err := r.gw.SweepCommandSettlements(r.ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, id); got != "timed-out" {
		t.Fatalf("status past the window = %q, want timed-out", got)
	}
}

func (r *settleRig) observedRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := r.conn.QueryRow(r.ctx, `select count(*) from property p join property_type pt on pt.id = p.property_type_id
		where pt.name = 'video-input' and p.provenance = 'observed'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A reply's value is held to the target property's validation like any
// telemetry: a value outside it fails the command and records nothing.
func TestAReplyOutsideThePropertysValuesRecordsNothing(t *testing.T) {
	r := newSettleRig(t, valueReply)
	id := r.issueAndDispatch(t, "hdmi3")
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", "INPUT <img/src=x>"); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, id); got != "failed" {
		t.Fatalf("status = %q, want failed", got)
	}
	if n := r.observedRows(t); n != 0 {
		t.Fatalf("%d observed rows written from an invalid reply, want none", n)
	}
}

// A reply repeating the current value settles its command without adding a
// row: the observed series records transitions.
func TestARepeatedReplyValueIsOneTransition(t *testing.T) {
	r := newSettleRig(t, valueReply)
	for i := 0; i < 2; i++ {
		id := r.issueAndDispatch(t, "hdmi3")
		if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", "INPUT hdmi3"); err != nil {
			t.Fatal(err)
		}
		if got := r.status(t, id); got != "settled" {
			t.Fatalf("command %d status = %q", i, got)
		}
	}
	if n := r.observedRows(t); n != 1 {
		t.Fatalf("%d observed rows for one value, want 1", n)
	}
}

// A reply that cannot be stored (a NUL byte) fails the command, and the
// execution is still recorded, so the command is not redelivered.
func TestAnUnstorableReplyStillRecordsTheExecution(t *testing.T) {
	r := newSettleRig(t, valueReply)
	id := r.issueAndDispatch(t, "hdmi3")
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", "INPUT hd\x00mi3"); err != nil {
		t.Fatal(err)
	}
	var executed bool
	if err := r.conn.QueryRow(r.ctx, `select executed_at is not null from command where id = $1`, id).Scan(&executed); err != nil {
		t.Fatal(err)
	}
	if !executed || r.status(t, id) != "failed" {
		t.Fatalf("executed %v status %q, want the execution recorded and the command failed", executed, r.status(t, id))
	}
}

// Inside the window, an observation that does not match, or one taken
// before the intent, leaves the command pending.
func TestNonEvidenceLeavesACommandPending(t *testing.T) {
	r := newSettleRig(t, "")
	id := r.issueAndDispatch(t, "hdmi3")
	var issuedAt time.Time
	if err := r.conn.QueryRow(r.ctx, `select ts from command where id = $1`, id).Scan(&issuedAt); err != nil {
		t.Fatal(err)
	}
	for _, s := range []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "switcher", Key: "video-input", Instance: "tcp", Value: "hdmi1", Source: "tcp"},
		{OwnerKind: "component", OwnerID: "switcher", Key: "video-input", Instance: "tcp", Value: "hdmi3", Source: "tcp", TS: issuedAt.Add(-time.Minute)},
	} {
		if err := r.gw.InsertPropertySamples(r.ctx, []storage.PropertySampleWrite{s}); err != nil {
			t.Fatal(err)
		}
		if got := r.status(t, id); got != "issued" {
			t.Fatalf("after %s at %v: status = %q, want still issued", s.Value, s.TS, got)
		}
	}
}
