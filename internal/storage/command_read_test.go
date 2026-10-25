package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A command's status is readable from issue to settlement (#1155): the read
// carries the state machine with a time per transition, where an in-flight
// command sits, the evidence that decided it (the reply, masked of the
// endpoint's credentials, or the observed sample), the reason for a failure
// or a timeout, and who issued it. Each terminal status writes its lifecycle
// event on the component's timeline.

// statusRig is a component with a line driver attached on a node, its
// set-input binding settling as settle declares, logging in with a
// basic-auth secret whose password is statusPassword.
type statusRig struct {
	ctx    context.Context
	dsn    string
	gw     *storage.PG
	conn   *pgx.Conn
	actor  string
	compID string
}

const statusPassword = "s3cr3t/Pa55!"

func newStatusRig(t *testing.T, settle string) *statusRig {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "switcher"}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: "switcher-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": statusPassword}}, all, true); err != nil {
		t.Fatal(err)
	}
	settleField := ""
	if settle != "" {
		settleField = `, "settle": ` + settle
	}
	spec := fmt.Sprintf(`{"version": 1, "transport": "tcp",
		"inputs": [{"name": "host", "kind": "string", "required": true}, {"name": "port", "kind": "number", "default": "4998"},
			{"name": "login", "kind": "secret", "secret_type": "basic-auth", "required": true}],
		"polls": [{"name": "status", "schedule": {"every": "30s"}, "request": {"line": "GET INPUT"},
			"emits": [{"name": "video-input", "extract": {"regex": "^INPUT (\\S+)$"}}]}],
		"commands": [{"command_type": "set-input", "request": {"line": "SET INPUT ${value}"}%s}]}`, settleField)
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "status-proto", Label: "Status Proto", Version: "1.0.0", Spec: []byte(spec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	name, drv, node := "switcher", "status-proto", "edge"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &drv, Component: &name, Node: &node,
		Inputs: map[string]string{"host": "10.20.4.60", "login": "switcher-login"}, SecretRead: all, CanAdmin: true}, all); err != nil {
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
	r := &statusRig{ctx: ctx, dsn: dsn, gw: gw, conn: conn, compID: comp.ID}
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&r.actor); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *statusRig) issue(t *testing.T, input string) int64 {
	t.Helper()
	cmd, err := r.gw.IssueCommand(r.ctx, r.actor, "component", r.compID, "set-input", "", json.RawMessage(`"`+input+`"`), nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return cmd.ID
}

func (r *statusRig) dispatch(t *testing.T, id int64) {
	t.Helper()
	got, err := r.gw.PendingNodeCommands(r.ctx, "edge")
	if err != nil || len(got) != 1 || got[0].ID != id {
		t.Fatalf("dispatch = %+v (%v), want command %d", got, err, id)
	}
}

func (r *statusRig) read(t *testing.T, id int64) *storage.CommandRecord {
	t.Helper()
	rec, err := r.gw.GetComponentCommand(r.ctx, r.compID, id)
	if err != nil {
		t.Fatalf("read command %d: %v", id, err)
	}
	return rec
}

// lifecycleEvent is the type and message of the event a command's terminal
// status wrote.
func (r *statusRig) lifecycleEvent(t *testing.T, rec *storage.CommandRecord) (string, string) {
	t.Helper()
	if rec.SettledEventID == nil {
		t.Fatalf("command %d (%s) wrote no lifecycle event", rec.ID, rec.Status)
	}
	var typ, msg string
	if err := r.conn.QueryRow(r.ctx, `select et.name, e.message from event e join event_type et on et.id = e.event_type_id
		where e.id = $1 and e.component_id = $2 and e.origin = 'caused'`, *rec.SettledEventID, r.compID).Scan(&typ, &msg); err != nil {
		t.Fatalf("lifecycle event of command %d: %v", rec.ID, err)
	}
	return typ, msg
}

// A reply-settled command reads its whole arc: queued for its node, then
// awaiting the reply once delivered, then settled with the reply as its
// evidence and a reason naming it, each transition stamped, the issuer and
// the endpoint, node and driver named, and command-settled on the timeline.
func TestCommandReadCarriesItsArc(t *testing.T) {
	r := newStatusRig(t, valueReply)
	id := r.issue(t, "hdmi3")

	rec := r.read(t, id)
	if rec.Status != "issued" || rec.Phase != storage.PhaseQueued {
		t.Fatalf("after issue: status %q phase %q, want issued and queued", rec.Status, rec.Phase)
	}
	if rec.CommandType != "set-input" || rec.Component != "switcher" || rec.ComponentID != r.compID {
		t.Fatalf("identity = %q on %q (%s)", rec.CommandType, rec.Component, rec.ComponentID)
	}
	if rec.SettleMode != storage.SettleModeReply || rec.TargetKind != "property" || rec.Target != "video-input" || string(rec.Intended) != `"hdmi3"` {
		t.Fatalf("settle mode %q target %s %s intended %s, want reply on property video-input to hdmi3", rec.SettleMode, rec.TargetKind, rec.Target, rec.Intended)
	}
	if rec.Endpoint != "tcp" || rec.EndpointID == "" || rec.Node != "edge" || rec.Driver != "status-proto" {
		t.Fatalf("carrier = endpoint %q (%q) node %q driver %q, want tcp on edge by status-proto", rec.Endpoint, rec.EndpointID, rec.Node, rec.Driver)
	}
	if rec.Issuer != "root" || rec.IssuerID != r.actor {
		t.Fatalf("issuer = %q (%s), want root", rec.Issuer, rec.IssuerID)
	}
	if rec.SettleWindowSeconds != 15 || !rec.Deadline.Equal(rec.IssuedAt.Add(15*time.Second)) {
		t.Fatalf("window %ds deadline %v, want 15s past %v", rec.SettleWindowSeconds, rec.Deadline, rec.IssuedAt)
	}
	if rec.DeliveredAt != nil || rec.ExecutedAt != nil || rec.SettledAt != nil || rec.Reply != nil || rec.Reason != "" {
		t.Fatalf("a queued command carries later facts: %+v", rec)
	}

	r.dispatch(t, id)
	rec = r.read(t, id)
	if rec.Phase != storage.PhaseAwaitingReply || rec.DeliveredAt == nil {
		t.Fatalf("after delivery: phase %q delivered %v, want awaiting-reply with its time", rec.Phase, rec.DeliveredAt)
	}

	if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", "INPUT hdmi3"); err != nil {
		t.Fatal(err)
	}
	rec = r.read(t, id)
	if rec.Status != "settled" || rec.Phase != "settled" || rec.SettledBy != storage.SettledByReply {
		t.Fatalf("after the reply: status %q phase %q by %q, want settled by the reply", rec.Status, rec.Phase, rec.SettledBy)
	}
	if rec.Reply == nil || *rec.Reply != "INPUT hdmi3" {
		t.Fatalf("reply = %v, want INPUT hdmi3", rec.Reply)
	}
	if !strings.Contains(rec.Reason, "hdmi3") {
		t.Fatalf("reason = %q, want it to name the value the reply carried", rec.Reason)
	}
	if rec.ExecutedAt == nil || rec.SettledAt == nil || rec.SettledAt.Before(rec.IssuedAt) || rec.ExecutedAt.Before(*rec.DeliveredAt) {
		t.Fatalf("transition times out of order: issued %v delivered %v executed %v settled %v", rec.IssuedAt, rec.DeliveredAt, rec.ExecutedAt, rec.SettledAt)
	}
	typ, msg := r.lifecycleEvent(t, rec)
	if typ != "command-settled" || !strings.Contains(msg, "set-input") {
		t.Fatalf("lifecycle event = %s %q, want command-settled naming set-input", typ, msg)
	}
}

// A failure says why: a reply carrying another value names both, an
// unconfirmed reply says so, and an execution error carries its story.
func TestCommandReadNamesTheFailure(t *testing.T) {
	r := newStatusRig(t, valueReply)
	for _, tc := range []struct {
		reply, execErr string
		reason         []string
	}{
		{"INPUT hdmi1", "", []string{"hdmi1", "hdmi3"}},
		{"-ERR busy", "", []string{"did not"}},
		{"", "collection: line dial: connection refused", []string{"connection refused"}},
	} {
		id := r.issue(t, "hdmi3")
		r.dispatch(t, id)
		if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, tc.execErr, tc.reply); err != nil {
			t.Fatal(err)
		}
		rec := r.read(t, id)
		if rec.Status != "failed" || rec.SettledBy != storage.SettledByReply {
			t.Errorf("reply %q err %q: status %q by %q, want failed by the reply", tc.reply, tc.execErr, rec.Status, rec.SettledBy)
		}
		for _, want := range tc.reason {
			if !strings.Contains(rec.Reason, want) {
				t.Errorf("reply %q err %q: reason %q, want it to mention %q", tc.reply, tc.execErr, rec.Reason, want)
			}
		}
		if tc.execErr != "" && rec.ExecError != tc.execErr {
			t.Errorf("exec error = %q, want %q", rec.ExecError, tc.execErr)
		}
		if typ, _ := r.lifecycleEvent(t, rec); typ != "command-failed" {
			t.Errorf("lifecycle event = %s, want command-failed", typ)
		}
	}
}

// A command nobody answers times out once its window passes, and the reason
// says where it stopped: delivered with no report, or never taken at all.
func TestNeverAnsweredCommandTimesOutWithAReason(t *testing.T) {
	r := newStatusRig(t, valueReply)
	if _, err := r.conn.Exec(r.ctx, `update command_type set settle_window_seconds = 1 where name = 'set-input'`); err != nil {
		t.Fatal(err)
	}
	delivered := r.issue(t, "hdmi3")
	r.dispatch(t, delivered)
	queued := r.issue(t, "hdmi2")
	time.Sleep(1500 * time.Millisecond)
	if err := r.gw.SweepCommandSettlements(r.ctx); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64][]string{
		delivered: {"edge", "no execution report", "1s"},
		queued:    {"edge", "did not take it", "1s"},
	} {
		rec := r.read(t, id)
		if rec.Status != "timed-out" || rec.Phase != "timed-out" || rec.SettledBy != storage.SettledByWindow {
			t.Fatalf("command %d: status %q phase %q by %q, want timed-out by the window", id, rec.Status, rec.Phase, rec.SettledBy)
		}
		for _, w := range want {
			if !strings.Contains(rec.Reason, w) {
				t.Errorf("command %d: reason %q, want it to mention %q", id, rec.Reason, w)
			}
		}
		// The event says what happened and which command; the evidence and
		// the reason stay on the command, behind command:read, rather than
		// on a timeline event:read reaches (#1155 review).
		if typ, msg := r.lifecycleEvent(t, rec); typ != "command-timed-out" || strings.Contains(msg, rec.Reason) || !strings.Contains(msg, fmt.Sprintf("id %d", id)) {
			t.Errorf("command %d: lifecycle event %s %q, want command-timed-out naming the command and not its reason", id, typ, msg)
		}
	}
}

// An observation-settled command reads delivered while its node has it,
// awaiting observation once the reply lands (the reply decides nothing), and
// settled by the sample that matched, which it carries with its time.
func TestObservationSettledCommandShowsItsSample(t *testing.T) {
	r := newStatusRig(t, "")
	id := r.issue(t, "hdmi3")
	if rec := r.read(t, id); rec.SettleMode != storage.SettleModeObservation || rec.Phase != storage.PhaseQueued {
		t.Fatalf("after issue: mode %q phase %q, want observation and queued", rec.SettleMode, rec.Phase)
	}
	r.dispatch(t, id)
	if rec := r.read(t, id); rec.Phase != storage.PhaseDelivered {
		t.Fatalf("after delivery: phase %q, want delivered", rec.Phase)
	}
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", "OK"); err != nil {
		t.Fatal(err)
	}
	if rec := r.read(t, id); rec.Status != "issued" || rec.Phase != storage.PhaseAwaitingObservation || rec.Reply == nil || *rec.Reply != "OK" {
		t.Fatalf("after the reply: status %q phase %q reply %v, want issued, awaiting observation, reply OK", rec.Status, rec.Phase, rec.Reply)
	}
	if err := r.gw.InsertPropertySamples(r.ctx, storage.PushWriter(), []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "switcher", Key: "video-input", Instance: "tcp", Value: "hdmi3", Source: "tcp",
	}}); err != nil {
		t.Fatal(err)
	}
	rec := r.read(t, id)
	if rec.Status != "settled" || rec.SettledBy != storage.SettledByObservation {
		t.Fatalf("after the observation: status %q by %q, want settled by observation", rec.Status, rec.SettledBy)
	}
	if string(rec.ObservedValue) != `"hdmi3"` || rec.ObservedAt == nil || rec.ObservedAt.Before(rec.IssuedAt) {
		t.Fatalf("observed sample = %s at %v, want hdmi3 after the issue", rec.ObservedValue, rec.ObservedAt)
	}
	if typ, _ := r.lifecycleEvent(t, rec); typ != "command-settled" {
		t.Fatalf("lifecycle event = %s, want command-settled", typ)
	}
}

// A component that echoes the credential its endpoint logs in with never
// has it stored or served: the reply and the execution error are masked of
// the endpoint's secret fields in every form, and of anything labeled a
// credential, before they are written.
func TestReplyIsStoredMaskedOfCredentials(t *testing.T) {
	r := newStatusRig(t, `{"from": "response", "success": {"regex": "OK"}}`)
	id := r.issue(t, "hdmi3")
	r.dispatch(t, id)
	echo := `OK login admin:` + statusPassword + ` url=` + url.QueryEscape(statusPassword) + ` token=abc123`
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", echo); err != nil {
		t.Fatal(err)
	}
	rec := r.read(t, id)
	if rec.Status != "settled" {
		t.Fatalf("status %q, want settled: masking is for what is stored, the judgment reads the reply whole", rec.Status)
	}
	if rec.Reply == nil || strings.Contains(*rec.Reply, "Pa55") || strings.Contains(*rec.Reply, "abc123") || !strings.Contains(*rec.Reply, "[redacted]") {
		t.Fatalf("stored reply = %v, want the credentials masked", rec.Reply)
	}
	var raw string
	if err := r.conn.QueryRow(r.ctx, `select coalesce(reply, '') || coalesce(reason, '') from command where id = $1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "Pa55") {
		t.Fatalf("the row holds the password: %q", raw)
	}

	failed := r.issue(t, "hdmi2")
	r.dispatch(t, failed)
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", failed, "line refused: auth "+statusPassword, ""); err != nil {
		t.Fatal(err)
	}
	rec = r.read(t, failed)
	if strings.Contains(rec.ExecError, "Pa55") || strings.Contains(rec.Reason, "Pa55") {
		t.Fatalf("exec error %q reason %q, want the password masked", rec.ExecError, rec.Reason)
	}
}

// Masking fails closed (#1155 review): a server that cannot unseal the
// endpoint's secrets (its key provider holds another key) cannot know what to
// mask, while the node that sent the reply may still hold them, so the reply,
// the error and any reason built from them are withheld rather than stored
// half-masked. The execution still lands and the command is still judged.
func TestReplyIsWithheldWhenTheEndpointsSecretsCannotBeRead(t *testing.T) {
	r := newStatusRig(t, `{"from": "response", "success": {"regex": "OK"}}`)
	other, err := storage.NewPG(r.ctx, r.dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x9}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	id := r.issue(t, "hdmi3")
	r.dispatch(t, id)
	if err := other.RecordCommandExecution(r.ctx, "edge", id, "", "OK "+statusPassword); err != nil {
		t.Fatal(err)
	}
	rec := r.read(t, id)
	if rec.Status != "settled" || rec.ExecutedAt == nil {
		t.Fatalf("status %q executed %v, want the execution recorded and judged on the whole reply", rec.Status, rec.ExecutedAt)
	}
	if rec.Reply == nil || strings.Contains(*rec.Reply, "Pa55") || !strings.Contains(*rec.Reply, "withheld") {
		t.Fatalf("stored reply = %v, want it withheld", rec.Reply)
	}

	failed := r.issue(t, "hdmi2")
	r.dispatch(t, failed)
	if err := other.RecordCommandExecution(r.ctx, "edge", failed, "refused: "+statusPassword, ""); err != nil {
		t.Fatal(err)
	}
	rec = r.read(t, failed)
	if strings.Contains(rec.ExecError, "Pa55") || strings.Contains(rec.Reason, "Pa55") || !strings.Contains(rec.ExecError, "withheld") {
		t.Fatalf("exec error %q reason %q, want both withheld", rec.ExecError, rec.Reason)
	}
}

// A report the node had no standing to make is refused before the server
// touches the endpoint's secrets, and a report it may make is masked by the
// endpoint the confining stamp names, never by a guess read before it.
func TestARefusedReportStoresNothing(t *testing.T) {
	r := newStatusRig(t, valueReply)
	id := r.issue(t, "hdmi3")
	r.dispatch(t, id)
	if _, err := r.gw.CreateNode(r.ctx, "", storage.NodeSpec{Name: "intruder"}, scope.Set{All: true}, scope.Set{All: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.gw.RecordCommandExecution(r.ctx, "intruder", id, "", "INPUT hdmi3 "+statusPassword); !errors.Is(err, storage.ErrCommandNotDispatchedHere) {
		t.Fatalf("a report from another node = %v, want ErrCommandNotDispatchedHere", err)
	}
	if rec := r.read(t, id); rec.Reply != nil || rec.ExecutedAt != nil || rec.Status != "issued" {
		t.Fatalf("a refused report left reply %v executed %v status %q", rec.Reply, rec.ExecutedAt, rec.Status)
	}
}

// A row stored before replies were masked (its execution error as the node
// reported it) still reads with what it labels a credential masked: the
// read applies the labeled-pair pass to every row, a no-op on one already
// masked.
func TestALegacyExecErrorReadsMasked(t *testing.T) {
	r := newStatusRig(t, valueReply)
	id := r.issue(t, "hdmi3")
	if _, err := r.conn.Exec(r.ctx, `update command set executed_at = now(), exec_error = 'login refused: password=hunter2' where id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if rec := r.read(t, id); strings.Contains(rec.ExecError, "hunter2") || !strings.Contains(rec.ExecError, "[redacted]") {
		t.Fatalf("legacy exec error reads %q, want its labeled credential masked", rec.ExecError)
	}
}

// A reply too long to be evidence is cut, and one that is not text is stored
// readable, so the execution is never lost to its reply.
func TestStoredReplyIsCappedAndReadable(t *testing.T) {
	r := newStatusRig(t, `{"from": "response", "success": {"regex": "OK"}}`)
	id := r.issue(t, "hdmi3")
	r.dispatch(t, id)
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", "OK "+strings.Repeat("x", 20000)+"\x00\xff"); err != nil {
		t.Fatal(err)
	}
	rec := r.read(t, id)
	if rec.Reply == nil || len(*rec.Reply) > storage.ReplyCap+64 || !strings.HasSuffix(*rec.Reply, "[truncated]") {
		t.Fatalf("stored reply is %d bytes, want it capped near %d and marked", len(*rec.Reply), storage.ReplyCap)
	}
}

// The list is a component's history, newest first, filtered by status and
// time, and never another component's.
func TestListComponentCommandsFilters(t *testing.T) {
	r := newStatusRig(t, valueReply)
	settled := r.issue(t, "hdmi3")
	r.dispatch(t, settled)
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", settled, "", "INPUT hdmi3"); err != nil {
		t.Fatal(err)
	}
	failed := r.issue(t, "hdmi3")
	r.dispatch(t, failed)
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", failed, "", "INPUT hdmi1"); err != nil {
		t.Fatal(err)
	}
	inFlight := r.issue(t, "hdmi4")
	var otherID string
	if err := r.conn.QueryRow(r.ctx, `select id::text from component where name = 'other'`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.gw.IssueCommand(r.ctx, r.actor, "component", otherID, "reboot", "", nil, nil); err != nil {
		t.Fatal(err)
	}

	ids := func(f storage.CommandFilter) []int64 {
		t.Helper()
		recs, err := r.gw.ListComponentCommands(r.ctx, r.compID, f)
		if err != nil {
			t.Fatalf("list %+v: %v", f, err)
		}
		var out []int64
		for _, c := range recs {
			out = append(out, c.ID)
		}
		return out
	}
	if got := ids(storage.CommandFilter{}); fmt.Sprint(got) != fmt.Sprint([]int64{inFlight, failed, settled}) {
		t.Fatalf("history = %v, want newest first %v", got, []int64{inFlight, failed, settled})
	}
	if got := ids(storage.CommandFilter{Status: "failed"}); fmt.Sprint(got) != fmt.Sprint([]int64{failed}) {
		t.Fatalf("failed = %v, want %d", got, failed)
	}
	if got := ids(storage.CommandFilter{Status: "issued"}); fmt.Sprint(got) != fmt.Sprint([]int64{inFlight}) {
		t.Fatalf("issued = %v, want %d", got, inFlight)
	}
	if got := ids(storage.CommandFilter{Limit: 2}); len(got) != 2 || got[0] != inFlight {
		t.Fatalf("limit 2 = %v, want the newest two", got)
	}
	var settledAt time.Time
	if err := r.conn.QueryRow(r.ctx, `select ts from command where id = $1`, failed).Scan(&settledAt); err != nil {
		t.Fatal(err)
	}
	since, until := settledAt, settledAt
	if got := ids(storage.CommandFilter{Since: &since, Until: &until}); fmt.Sprint(got) != fmt.Sprint([]int64{failed}) {
		t.Fatalf("at %v = %v, want %d", settledAt, got, failed)
	}
	future := time.Now().Add(time.Hour)
	if got := ids(storage.CommandFilter{Since: &future}); len(got) != 0 {
		t.Fatalf("since the future = %v, want none", got)
	}
	if _, err := r.gw.GetComponentCommand(r.ctx, otherID, settled); !errors.Is(err, storage.ErrCommandNotFound) {
		t.Fatalf("a command read under another component = %v, want ErrCommandNotFound", err)
	}
}

// A fire-and-forget command reads settled at its issue, decided by the
// issue, with nothing told to settle; a command no driver binds sits
// awaiting observation, since no node will ever take it.
func TestFireAndForgetAndUndeliverableReads(t *testing.T) {
	r := newStatusRig(t, valueReply)
	ff, err := r.gw.IssueCommand(r.ctx, r.actor, "component", r.compID, "reboot", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := r.read(t, ff.ID)
	if rec.Status != "settled" || rec.SettledBy != storage.SettledByIssue || rec.SettleMode != storage.SettleModeNone || rec.Intended != nil || rec.Reason == "" {
		t.Fatalf("fire-and-forget = status %q by %q mode %q intended %s reason %q", rec.Status, rec.SettledBy, rec.SettleMode, rec.Intended, rec.Reason)
	}
	cmd, err := r.gw.IssueCommand(r.ctx, r.actor, "component", r.compID, "set-volume", "", json.RawMessage(`40`), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = r.read(t, cmd.ID)
	if rec.Phase != storage.PhaseAwaitingObservation || rec.Endpoint != "" || rec.SettleMode != storage.SettleModeObservation || string(rec.Intended) != "40" || rec.TargetKind != "metric" {
		t.Fatalf("undeliverable = phase %q endpoint %q mode %q intended %s target %s", rec.Phase, rec.Endpoint, rec.SettleMode, rec.Intended, rec.TargetKind)
	}
}
