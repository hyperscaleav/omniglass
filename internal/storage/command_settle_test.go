package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	if err := r.gw.InsertPropertySamples(r.ctx, storage.PushWriter(), []storage.PropertySampleWrite{{
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
		if err := r.gw.InsertPropertySamples(r.ctx, storage.PushWriter(), []storage.PropertySampleWrite{s}); err != nil {
			t.Fatal(err)
		}
		if got := r.status(t, id); got != "issued" {
			t.Fatalf("after %s at %v: status = %q, want still issued", s.Value, s.TS, got)
		}
	}
}

// issueQueued issues set-input with no instance and leaves it queued: no node
// has taken it, so it names no endpoint it was dispatched over.
func (r *settleRig) issueQueued(t *testing.T, input string) int64 {
	t.Helper()
	cmd, err := r.gw.IssueCommand(r.ctx, r.actor, "component", "switcher", "set-input", "", json.RawMessage(`"`+input+`"`), nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return cmd.ID
}

// observeOn lands an observation of video-input on one instance: on the
// rig's endpoint (tcp) through the node lane, as the consumer does for a task
// on it; on any other instance as a push that names it.
func (r *settleRig) observeOn(t *testing.T, instance, value string) {
	t.Helper()
	w := storage.PushWriter()
	if instance == "tcp" {
		w = storage.NodeWriter("switcher", instance)
	}
	if err := r.gw.InsertPropertySamples(r.ctx, w, []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "switcher", Key: "video-input", Instance: instance, Value: value, Source: "tcp",
	}}); err != nil {
		t.Fatalf("observe %s on %s: %v", value, instance, err)
	}
}

// A queued un-instanced command (no node has taken it yet) is judged on the
// endpoint queued to carry it, the one whose driver binds its type, where
// that driver's observations of the target land (ADR-0456): another
// instance agreeing does not settle it, and its carrier agreeing does, the
// computed verdict agreeing with the recorded one.
func TestAQueuedCommandIsJudgedOnTheEndpointQueuedToCarryIt(t *testing.T) {
	r := newSettleRig(t, "")
	id := r.issueQueued(t, "hdmi3")
	r.observeOn(t, "aux", "hdmi3")
	if got := r.status(t, id); got != "issued" {
		t.Fatalf("after another instance agreed: status = %q, want issued", got)
	}
	r.observeOn(t, "tcp", "hdmi3")
	if got := r.status(t, id); got != "settled" {
		t.Fatalf("after its carrier agreed: status = %q, want settled", got)
	}
	verdict, err := r.gw.CommandSettlement(r.ctx, "component", "switcher", "set-input", "")
	if err != nil {
		t.Fatal(err)
	}
	if verdict != storage.SettlementSettled {
		t.Fatalf("computed verdict = %q, want settled, as recorded", verdict)
	}
}

// A command no node took never reached the device, so a reading of its
// target that disagrees is not the device's answer to it: past the window
// it times out, saying it stopped in the queue, whether its carrier had
// observed another value or nothing at all (ADR-0456, the commands page's
// "timed-out covers a command no node picked up").
func TestAQueuedCommandNoNodeTookTimesOut(t *testing.T) {
	r := newSettleRig(t, "")
	if _, err := r.conn.Exec(r.ctx, `update command_type set settle_window_seconds = 1 where name = 'set-input'`); err != nil {
		t.Fatal(err)
	}
	unanswered := r.issueQueued(t, "hdmi2")
	r.observeOn(t, "tcp", "hdmi1")
	answered := r.issueQueued(t, "hdmi3")
	time.Sleep(1500 * time.Millisecond)
	if err := r.gw.SweepCommandSettlements(r.ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{unanswered, answered} {
		var status, settledBy, reason string
		if err := r.conn.QueryRow(r.ctx, `select status, coalesce(settled_by, ''), coalesce(reason, '') from command where id = $1`, id).
			Scan(&status, &settledBy, &reason); err != nil {
			t.Fatal(err)
		}
		if status != "timed-out" || settledBy != "window" || !strings.Contains(reason, "queued for node edge") {
			t.Fatalf("command %d no node took: status %q by %q (%s), want timed-out by the window, queued for node edge", id, status, settledBy, reason)
		}
	}
}

// ownerLevelRig adds a display to the rig that no endpoint can command (no
// driver binds set-input on it), so an un-instanced set-input on it is an
// owner-level intent, judged on every observed instance of video-input.
func (r *settleRig) ownerLevelDisplay(t *testing.T) {
	t.Helper()
	all := scope.Set{All: true}
	if _, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "display"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := r.conn.Exec(r.ctx, `update command_type set settle_window_seconds = 1 where name = 'set-input'`); err != nil {
		t.Fatal(err)
	}
}

// pushOn lands an observation of video-input on the display, on an
// instance its sender names.
func (r *settleRig) pushOn(t *testing.T, instance, value string) {
	t.Helper()
	if err := r.gw.InsertPropertySamples(r.ctx, storage.PushWriter(), []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "display", Key: "video-input", Instance: instance, Value: value, Source: "push",
	}}); err != nil {
		t.Fatalf("push %s on %s: %v", value, instance, err)
	}
}

// issueOwnerLevel issues set-input on the display with no instance.
func (r *settleRig) issueOwnerLevel(t *testing.T, input string) int64 {
	t.Helper()
	cmd, err := r.gw.IssueCommand(r.ctx, r.actor, "component", "display", "set-input", "", json.RawMessage(`"`+input+`"`), nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return cmd.ID
}

// outcome reads a command's recorded status, what decided it, and the value
// it was decided on.
func (r *settleRig) outcome(t *testing.T, id int64) (status, settledBy, observed string) {
	t.Helper()
	var obs []byte
	if err := r.conn.QueryRow(r.ctx, `select status, coalesce(settled_by, ''), observed_value from command where id = $1`, id).
		Scan(&status, &settledBy, &obs); err != nil {
		t.Fatal(err)
	}
	return status, settledBy, string(obs)
}

// An owner-level command (one no endpoint can carry) is judged on every
// observed instance of its target (ADR-0456): an instance reporting another
// value leaves it pending, and the first instance that agrees settles it.
func TestAnOwnerLevelCommandSettlesWhenAnyInstanceAgrees(t *testing.T) {
	r := newSettleRig(t, "")
	r.ownerLevelDisplay(t)
	id := r.issueOwnerLevel(t, "hdmi3")
	r.pushOn(t, "rx-1", "hdmi1")
	if got := r.status(t, id); got != "issued" {
		t.Fatalf("after a disagreeing instance: status = %q, want issued", got)
	}
	r.pushOn(t, "rx-2", "hdmi3")
	if got := r.status(t, id); got != "settled" {
		t.Fatalf("after an agreeing instance: status = %q, want settled", got)
	}
	verdict, err := r.gw.CommandSettlement(r.ctx, "component", "display", "set-input", "")
	if err != nil {
		t.Fatal(err)
	}
	if verdict != storage.SettlementSettled {
		t.Fatalf("computed verdict = %q, want settled, as recorded", verdict)
	}
}

// Past the window, an owner-level command whose target was observed on some
// instance and agreed on none failed, decided by the observation; with
// nothing observed on any instance it timed out.
func TestAnOwnerLevelCommandFailsWhenNoInstanceAgrees(t *testing.T) {
	r := newSettleRig(t, "")
	r.ownerLevelDisplay(t)
	unanswered := r.issueOwnerLevel(t, "hdmi2")
	time.Sleep(1500 * time.Millisecond)
	if err := r.gw.SweepCommandSettlements(r.ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, unanswered); got != "timed-out" {
		t.Fatalf("nothing observed: status = %q, want timed-out", got)
	}
	r.pushOn(t, "rx-1", "hdmi1")
	id := r.issueOwnerLevel(t, "hdmi3")
	time.Sleep(1500 * time.Millisecond)
	if err := r.gw.SweepCommandSettlements(r.ctx); err != nil {
		t.Fatal(err)
	}
	if status, by, observed := r.outcome(t, id); status != "failed" || by != "observation" || observed != `"hdmi1"` {
		t.Fatalf("observed otherwise: status %q by %q observed %s, want failed by the observation of hdmi1", status, by, observed)
	}
}

// An endpoint deleted after it observed the target is a retired observer:
// its last reading is frozen (nothing prunes a series' latest row), so an
// owner-level judgment leaves it out rather than settling a command on what
// a removed connection once saw (ADR-0456).
func TestARetiredObserverDoesNotSettleAnOwnerLevelCommand(t *testing.T) {
	r := newSettleRig(t, "")
	r.ownerLevelDisplay(t)
	all := scope.Set{All: true}
	if _, err := r.conn.Exec(r.ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('telnet', 'telnet', (select id from component where name = 'display'), (select principal_id from node where name = 'edge'), '{"target":"10.20.4.61:23"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if err := r.gw.InsertPropertySamples(r.ctx, storage.NodeWriter("display", "telnet"), []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "display", Key: "video-input", Instance: "telnet", Value: "hdmi3", Source: "telnet",
	}}); err != nil {
		t.Fatalf("observe on telnet: %v", err)
	}
	if err := r.gw.DeleteEndpoint(r.ctx, r.actor, "display/telnet", all, all); err != nil {
		t.Fatalf("delete the telnet endpoint: %v", err)
	}
	r.pushOn(t, "rx-1", "hdmi1")
	id := r.issueOwnerLevel(t, "hdmi3")
	time.Sleep(1500 * time.Millisecond)
	if err := r.gw.SweepCommandSettlements(r.ctx); err != nil {
		t.Fatal(err)
	}
	if status, by, observed := r.outcome(t, id); status != "failed" || by != "observation" || observed != `"hdmi1"` {
		t.Fatalf("a retired observer agreed: status %q by %q observed %s, want failed on the live instance's hdmi1", status, by, observed)
	}
}

// A command dispatched over an endpoint is judged on that endpoint's series
// alone (#900, kept by ADR-0456): another instance agreeing does not settle
// it, and with nothing on its own endpoint it times out.
func TestADispatchedCommandIsJudgedOnItsEndpointAlone(t *testing.T) {
	r := newSettleRig(t, "")
	if _, err := r.conn.Exec(r.ctx, `update command_type set settle_window_seconds = 1 where name = 'set-input'`); err != nil {
		t.Fatal(err)
	}
	id := r.issueAndDispatch(t, "hdmi3")
	r.observeOn(t, "aux", "hdmi3")
	if got := r.status(t, id); got != "issued" {
		t.Fatalf("after another instance agreed: status = %q, want issued", got)
	}
	time.Sleep(1500 * time.Millisecond)
	if err := r.gw.SweepCommandSettlements(r.ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.status(t, id); got != "timed-out" {
		t.Fatalf("nothing on its own endpoint: status = %q, want timed-out", got)
	}
}
